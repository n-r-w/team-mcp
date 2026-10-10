// Package messagefile reads and writes message files inside allowed directories.
package messagefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/n-r-w/team-mcp/internal/server"
	"github.com/n-r-w/team-mcp/internal/usecase"
)

// Service reads and writes message files inside allowed directories.
type Service struct {
	// allowedDirs lists directories that hold readable and writable message files.
	allowedDirs []string
}

var (
	_ usecase.IMessageFileWriter = (*Service)(nil)
	_ server.IMessageFileReader  = (*Service)(nil)
)

// New constructs a message file service for allowedDirs.
func New(allowedDirs []string) *Service {
	return &Service{allowedDirs: allowedDirs}
}

// WriteMessage writes exact message content to the requested destination.
func (s *Service) WriteMessage(
	_ context.Context,
	filePath string,
	mode domain.MessageFileMode,
	content string,
) (resultErr error) {
	place, err := s.resolveDestination(filePath)
	if err != nil {
		return err
	}

	flags, err := writeFlags(mode)
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(place.dir)
	if err != nil {
		return fmt.Errorf("open allowed directory: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close allowed directory: %w", closeErr))
		}
	}()

	// Parent creation is rooted so existing symbolic links cannot redirect writes outside allowed directory.
	if mkdirErr := root.MkdirAll(filepath.Dir(place.relativePath), directoryPermission); mkdirErr != nil {
		return fmt.Errorf("create destination directories: %w", mkdirErr)
	}

	file, err := root.OpenFile(place.relativePath, flags, filePermission)
	if err != nil {
		return fmt.Errorf("open destination file: %w", err)
	}

	return writeAndClose(file, content)
}

// ReadMessage returns UTF-8 text of message source file.
func (s *Service) ReadMessage(_ context.Context, filePath string) (_ string, resultErr error) {
	if !filepath.IsAbs(filePath) {
		return "", errFilePathAbsolute
	}

	cleanPath := filepath.Clean(filePath)
	extension := strings.ToLower(filepath.Ext(cleanPath))
	if extension != textExtension && extension != markdownExtension {
		return "", errSourceExtension
	}

	place, err := s.locate(cleanPath)
	if err != nil {
		return "", err
	}

	root, err := os.OpenRoot(place.dir)
	if err != nil {
		return "", fmt.Errorf("open allowed directory: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close allowed directory: %w", closeErr))
		}
	}()

	// Stat before read keeps special files such as FIFO from blocking the read.
	fileInfo, err := root.Stat(place.relativePath)
	if err != nil {
		return "", fmt.Errorf("read file_path: %w", err)
	}
	if !fileInfo.Mode().IsRegular() {
		return "", errSourceNotRegular
	}

	payload, err := root.ReadFile(place.relativePath)
	if err != nil {
		return "", fmt.Errorf("read file_path: %w", err)
	}
	if !utf8.Valid(payload) {
		return "", errSourceNotUTF8
	}

	content := string(payload)
	if strings.TrimSpace(content) == "" {
		return "", errSourceBlank
	}

	return content, nil
}

// resolveDestination validates the public destination contract and finds its allowed directory.
func (s *Service) resolveDestination(filePath string) (filePlace, error) {
	if strings.TrimSpace(filePath) == "" {
		return filePlace{dir: "", relativePath: ""}, errFilePathRequired
	}
	if !filepath.IsAbs(filePath) {
		return filePlace{dir: "", relativePath: ""}, errFilePathAbsolute
	}

	cleanPath := filepath.Clean(filePath)
	extension := filepath.Ext(cleanPath)
	if extension != textExtension && extension != markdownExtension {
		return filePlace{dir: "", relativePath: ""}, errFilePathExtension
	}

	return s.locate(cleanPath)
}

// locate finds allowed directory that holds cleanPath.
func (s *Service) locate(cleanPath string) (filePlace, error) {
	for _, dir := range s.allowedDirs {
		// Rel fails when path and directory have different volumes, so such path is outside directory.
		relativePath, err := filepath.Rel(dir, cleanPath)
		if err == nil && filepath.IsLocal(relativePath) {
			return filePlace{dir: dir, relativePath: relativePath}, nil
		}
	}

	return filePlace{dir: "", relativePath: ""}, fmt.Errorf(
		"%w: %s",
		errFilePathOutsideAllowedDirs,
		strings.Join(s.allowedDirs, ", "),
	)
}

// writeFlags maps the closed domain mode to operating-system open semantics.
func writeFlags(mode domain.MessageFileMode) (int, error) {
	switch mode {
	case domain.MessageFileModeCreate:
		return os.O_WRONLY | os.O_CREATE | os.O_EXCL, nil
	case domain.MessageFileModeOverwrite:
		return os.O_WRONLY | os.O_CREATE | os.O_TRUNC, nil
	default:
		return 0, errMode
	}
}

// writeAndClose preserves write and close failures without attempting rollback.
func writeAndClose(file *os.File, content string) error {
	written, writeErr := file.WriteString(content)
	if writeErr == nil && written != len(content) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		writeErr = fmt.Errorf("write destination file: %w", writeErr)
	}

	closeErr := file.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close destination file: %w", closeErr)
	}

	return errors.Join(writeErr, closeErr)
}
