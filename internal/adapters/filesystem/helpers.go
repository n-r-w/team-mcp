package filesystem

import (
	"fmt"
	"path/filepath"
	"strings"
)

// fileForMessageID keeps payload naming consistent across board-store reads and writes.
func fileForMessageID(messageID string) string {
	return messageID + markdownExtension
}

// validateLocalID keeps desk, topic, and message identifiers scoped to the storage root.
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
