// Package messagefile writes stored message payloads to user-selected files under one home directory.
package messagefile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/n-r-w/team-mcp/internal/usecase"
)

var _ usecase.IMessageFileWriter = (*Service)(nil)

// Service writes message payloads within one configured home directory.
type Service struct {
	homeDir string
}

// New constructs a message file writer rooted at homeDir.
func New(homeDir string) *Service {
	return &Service{homeDir: filepath.Clean(homeDir)}
}

// WriteMessage writes exact message content to the requested destination.
func (s *Service) WriteMessage(
	_ context.Context,
	filePath string,
	mode domain.MessageFileMode,
	content string,
) (resultErr error) {
	relativePath, err := s.resolveDestination(filePath)
	if err != nil {
		return err
	}

	flags, err := writeFlags(mode)
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(s.homeDir)
	if err != nil {
		return fmt.Errorf("open user home directory: %w", err)
	}
	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("close user home directory: %w", closeErr))
		}
	}()

	// Parent creation is rooted so existing symbolic links cannot redirect writes outside home.
	if mkdirErr := root.MkdirAll(filepath.Dir(relativePath), directoryPermission); mkdirErr != nil {
		return fmt.Errorf("create destination directories: %w", mkdirErr)
	}

	file, err := root.OpenFile(relativePath, flags, filePermission)
	if err != nil {
		return fmt.Errorf("open destination file: %w", err)
	}

	return writeAndClose(file, content)
}

// resolveDestination validates the public path contract and derives a root-local name.
func (s *Service) resolveDestination(filePath string) (string, error) {
	if strings.TrimSpace(filePath) == "" {
		return "", errFilePathRequired
	}
	if !filepath.IsAbs(filePath) {
		return "", errFilePathAbsolute
	}

	cleanPath := filepath.Clean(filePath)
	extension := filepath.Ext(cleanPath)
	if extension != textExtension && extension != markdownExtension {
		return "", errFilePathExtension
	}

	relativePath, err := filepath.Rel(s.homeDir, cleanPath)
	if err != nil {
		return "", fmt.Errorf("resolve destination relative to user home: %w", err)
	}
	if !filepath.IsLocal(relativePath) {
		return "", errFilePathOutsideHome
	}

	return relativePath, nil
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
