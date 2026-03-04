package filesystem

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	invalidMessageID = "../outside"
)

// TestPersistResolveByDesk verifies message persistence and read by desk/message IDs.
func TestPersistResolveByDesk(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	messageDir := filepath.Join(t.TempDir(), "messages")
	adapter, err := New(messageDir)
	require.NoError(t, err)

	require.NoError(t, adapter.EnsureDesk(ctx, "desk-1", time.Now().UTC()))
	require.NoError(t, adapter.PersistMessage(ctx, "desk-1", "msg-1", "# payload"))

	payload, err := adapter.ResolveMessage(ctx, "desk-1", "msg-1")
	require.NoError(t, err)
	require.Equal(t, "# payload", payload)
}

// TestDeleteMessage verifies message file removal by desk/message IDs.
func TestDeleteMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	messageDir := filepath.Join(t.TempDir(), "messages")
	adapter, err := New(messageDir)
	require.NoError(t, err)

	require.NoError(t, adapter.EnsureDesk(ctx, "desk-1", time.Now().UTC()))
	require.NoError(t, adapter.PersistMessage(ctx, "desk-1", "msg-1", "# payload"))
	require.NoError(t, adapter.DeleteMessage(ctx, "desk-1", "msg-1"))

	_, err = adapter.ResolveMessage(ctx, "desk-1", "msg-1")
	require.Error(t, err)
}

// TestPersistMessageRejectsDeskWithoutMetadata verifies payload write is denied when desk metadata marker is absent.
func TestPersistMessageRejectsDeskWithoutMetadata(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	messageDir := filepath.Join(t.TempDir(), "messages")
	adapter, err := New(messageDir)
	require.NoError(t, err)

	deskDir := filepath.Join(messageDir, "desk-1")
	require.NoError(t, os.MkdirAll(deskDir, 0o755))

	err = adapter.PersistMessage(ctx, "desk-1", "msg-1", "# payload")
	require.Error(t, err)
	require.ErrorIs(t, err, os.ErrNotExist)

	entries, readDirErr := os.ReadDir(deskDir)
	require.NoError(t, readDirErr)
	require.Empty(t, entries)
}

// TestCollectExpiredDeskIDs verifies metadata-driven expiration scanning.
func TestCollectExpiredDeskIDs(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	messageDir := filepath.Join(t.TempDir(), "messages")
	adapter, err := New(messageDir)
	require.NoError(t, err)

	require.NoError(t, adapter.EnsureDesk(ctx, "desk-expired", time.Now().UTC().Add(-2*time.Hour)))
	require.NoError(t, adapter.EnsureDesk(ctx, "desk-active", time.Now().UTC()))

	expired, err := adapter.CollectExpiredDeskIDs(ctx, time.Now().UTC(), time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{"desk-expired"}, expired)
}

// TestCollectExpiredDeskIDsTreatsCorruptedMetadataAsExpired verifies malformed metadata does not silently bypass cleanup.
func TestCollectExpiredDeskIDsTreatsCorruptedMetadataAsExpired(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	messageDir := filepath.Join(t.TempDir(), "messages")
	adapter, err := New(messageDir)
	require.NoError(t, err)

	require.NoError(t, adapter.EnsureDesk(ctx, "desk-expired", time.Now().UTC().Add(-2*time.Hour)))
	require.NoError(t, adapter.EnsureDesk(ctx, "desk-corrupted", time.Now().UTC()))
	require.NoError(t, adapter.EnsureDesk(ctx, "desk-active", time.Now().UTC()))

	corruptedMetaPath := filepath.Join(messageDir, "desk-corrupted", metaFileName)
	require.NoError(t, os.WriteFile(corruptedMetaPath, []byte("{broken-json"), 0o640))

	expired, err := adapter.CollectExpiredDeskIDs(ctx, time.Now().UTC(), time.Hour)
	require.NoError(t, err)
	require.Contains(t, expired, "desk-expired")
	require.Contains(t, expired, "desk-corrupted")
	require.NotContains(t, expired, "desk-active")
}

// TestDeleteDesk verifies full desk directory deletion.
func TestDeleteDesk(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	messageDir := filepath.Join(t.TempDir(), "messages")
	adapter, err := New(messageDir)
	require.NoError(t, err)

	require.NoError(t, adapter.EnsureDesk(ctx, "desk-1", time.Now().UTC()))
	require.NoError(t, adapter.PersistMessage(ctx, "desk-1", "msg-1", "# payload"))
	require.NoError(t, adapter.DeleteDesk(ctx, "desk-1"))

	_, err = adapter.ResolveMessage(ctx, "desk-1", "msg-1")
	require.Error(t, err)
}

// TestResolveRejectsParentTraversal verifies parent-directory message IDs are denied.
func TestResolveRejectsParentTraversal(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	messageDir := filepath.Join(t.TempDir(), "messages")
	adapter, err := New(messageDir)
	require.NoError(t, err)

	_, err = adapter.ResolveMessage(ctx, "desk-1", invalidMessageID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside message directory")
}

// TestNewServiceRejectsRelativeRootDir verifies constructor validation for relative rootDir.
func TestNewServiceRejectsRelativeRootDir(t *testing.T) {
	t.Parallel()

	adapter, err := New("messages")
	require.Nil(t, adapter)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid root directory")
}
