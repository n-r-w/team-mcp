package filesystem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/n-r-w/team-mcp/internal/usecase"
)

var _ usecase.IDeskStore = (*Service)(nil)

// deskMeta persists desk creation timestamp for startup expiry collection.
type deskMeta struct {
	CreatedAt int64 `json:"created_at"`
}

// Service persists payloads into filesystem as markdown files and resolves them by message IDs.
type Service struct {
	rootDir string
}

// New constructs filesystem adapter with configured root directory.
func New(rootDir string) (*Service, error) {
	cleanRootDir := filepath.Clean(strings.TrimSpace(rootDir))
	if cleanRootDir == "." {
		return nil, errors.New("invalid root directory: must not be empty")
	}

	if !filepath.IsAbs(cleanRootDir) {
		return nil, errors.New("invalid root directory: must be absolute path")
	}

	return &Service{rootDir: cleanRootDir}, nil
}

func fileForMessageID(messageID string) string {
	return messageID + markdownExtension
}

// EnsureDesk creates desk directory and metadata file used by cleanup lifecycle.
func (s *Service) EnsureDesk(_ context.Context, deskID string, createdAt time.Time) error {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return err
	}

	deskDir := filepath.Join(s.rootDir, deskID)
	if mkdirErr := os.MkdirAll(deskDir, directoryPermission); mkdirErr != nil {
		return mkdirErr
	}

	metaPayload, marshalErr := json.Marshal(deskMeta{CreatedAt: createdAt.UTC().UnixNano()})
	if marshalErr != nil {
		return marshalErr
	}

	metaFile := filepath.Clean(filepath.Join(deskID, metaFileName))
	if !filepath.IsLocal(metaFile) {
		return errors.New("desk ID points outside message directory")
	}

	if err := s.withOpenedRoot(func(root *os.Root) error {
		return root.WriteFile(metaFile, metaPayload, filePermission)
	}); err != nil {
		return err
	}

	return nil
}

// PersistMessage stores payload under desk directory using provided message ID.
func (s *Service) PersistMessage(_ context.Context, deskID, messageID, payload string) error {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return err
	}

	if err := validateLocalID("message ID", messageID); err != nil {
		return err
	}

	metaFile := filepath.Clean(filepath.Join(deskID, metaFileName))
	if !filepath.IsLocal(metaFile) {
		return errors.New("desk ID points outside message directory")
	}

	messageFile := filepath.Clean(filepath.Join(deskID, fileForMessageID(messageID)))
	if !filepath.IsLocal(messageFile) {
		return errors.New("message ID points outside message directory")
	}

	if err := s.withOpenedRoot(func(root *os.Root) error {
		if _, readMetaErr := root.ReadFile(metaFile); readMetaErr != nil {
			return readMetaErr
		}

		return root.WriteFile(messageFile, []byte(payload), filePermission)
	}); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("desk storage not initialized: %w", err)
		}

		return err
	}

	return nil
}

// ResolveMessage loads message payload by desk and message IDs.
func (s *Service) ResolveMessage(_ context.Context, deskID, messageID string) (string, error) {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return "", err
	}

	if err := validateLocalID("message ID", messageID); err != nil {
		return "", err
	}

	messageFile := filepath.Clean(filepath.Join(deskID, fileForMessageID(messageID)))
	if !filepath.IsLocal(messageFile) {
		return "", errors.New("message ID points outside message directory")
	}

	var payload []byte
	if err := s.withOpenedRoot(func(root *os.Root) error {
		readPayload, readErr := root.ReadFile(messageFile)
		if readErr != nil {
			return readErr
		}

		payload = readPayload

		return nil
	}); err != nil {
		return "", err
	}

	return string(payload), nil
}

// DeleteMessage removes payload file by desk and message IDs.
func (s *Service) DeleteMessage(_ context.Context, deskID, messageID string) error {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return err
	}

	if err := validateLocalID("message ID", messageID); err != nil {
		return err
	}

	messageFile := filepath.Clean(filepath.Join(deskID, fileForMessageID(messageID)))
	if !filepath.IsLocal(messageFile) {
		return errors.New("message ID points outside message directory")
	}

	if err := s.withOpenedRoot(func(root *os.Root) error {
		return root.Remove(messageFile)
	}); err != nil {
		return err
	}

	return nil
}

// DeleteDesk removes all desk-linked payload and metadata files.
func (s *Service) DeleteDesk(_ context.Context, deskID string) error {
	if err := validateLocalID("desk ID", deskID); err != nil {
		return err
	}

	return os.RemoveAll(filepath.Join(s.rootDir, deskID))
}

// CollectExpiredDeskIDs scans persisted desk metadata and returns expired desk IDs.
func (s *Service) CollectExpiredDeskIDs(_ context.Context, now time.Time, ttl time.Duration) ([]string, error) {
	entries, err := os.ReadDir(s.rootDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}

		return nil, err
	}

	nowUTC := now.UTC()
	expiredDeskIDs := make([]string, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		deskID := entry.Name()
		metaFile := filepath.Clean(filepath.Join(deskID, metaFileName))
		if !filepath.IsLocal(metaFile) {
			continue
		}

		var metaPayload []byte
		if readErr := s.withOpenedRoot(func(root *os.Root) error {
			payload, readMetaErr := root.ReadFile(metaFile)
			if readMetaErr != nil {
				return readMetaErr
			}

			metaPayload = payload

			return nil
		}); readErr != nil {
			if errors.Is(readErr, os.ErrNotExist) {
				continue
			}

			return nil, readErr
		}

		var meta deskMeta
		if unmarshalErr := json.Unmarshal(metaPayload, &meta); unmarshalErr != nil {
			slog.Warn(
				"desk metadata is invalid, marking desk as expired",
				"desk_id",
				deskID,
				"error",
				unmarshalErr,
			)
			expiredDeskIDs = append(expiredDeskIDs, deskID)

			continue
		}

		createdAt := time.Unix(0, meta.CreatedAt).UTC()
		if createdAt.Add(ttl).After(nowUTC) {
			continue
		}

		expiredDeskIDs = append(expiredDeskIDs, deskID)
	}

	return expiredDeskIDs, nil
}

// validateLocalID verifies required non-empty path-local IDs for secure filesystem paths.
func validateLocalID(field, value string) error {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return fmt.Errorf("%s is empty", field)
	}

	if !filepath.IsLocal(trimmedValue) {
		return fmt.Errorf("%s points outside message directory", field)
	}

	return nil
}

// withOpenedRoot runs operation against root and preserves operation-before-close error precedence.
func (s *Service) withOpenedRoot(operation func(root *os.Root) error) error {
	root, openErr := os.OpenRoot(s.rootDir)
	if openErr != nil {
		return openErr
	}

	operationErr := operation(root)
	closeErr := root.Close()
	if operationErr != nil {
		return operationErr
	}

	if closeErr != nil {
		return closeErr
	}

	return nil
}
