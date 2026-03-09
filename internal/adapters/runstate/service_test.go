package runstate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/team-mcp/internal/domain"
)

// TestCreateDeskCapacityExceeded verifies active desk capacity bound enforcement.
func TestCreateDeskCapacityExceeded(t *testing.T) {
	t.Parallel()

	adapter := New(1)
	ctx := t.Context()
	_, err := adapter.CreateDesk(ctx, time.Now().UTC())
	require.NoError(t, err)

	_, err = adapter.CreateDesk(ctx, time.Now().UTC())
	require.Error(t, err)

	var domainErr domain.Error
	require.ErrorAs(t, err, &domainErr)
	require.Equal(t, domain.ErrorCodeCapacityExceeded, domainErr.Code())
}

// TestCreateTopicIdempotent verifies topic creation is idempotent by (desk_id,title).
func TestCreateTopicIdempotent(t *testing.T) {
	t.Parallel()

	adapter := New(4)
	ctx := t.Context()
	deskID, err := adapter.CreateDesk(ctx, time.Now().UTC())
	require.NoError(t, err)

	firstHeader, firstStatus, firstCreated, err := adapter.CreateTopic(ctx, deskID, "topic")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, firstStatus)
	require.True(t, firstCreated)

	secondHeader, secondStatus, secondCreated, err := adapter.CreateTopic(ctx, deskID, "topic")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, secondStatus)
	require.False(t, secondCreated)
	require.Equal(t, firstHeader.TopicID, secondHeader.TopicID)
}

// TestCreateMessageDuplicateNormalizedTitle verifies duplicate detection by normalized title.
func TestCreateMessageDuplicateNormalizedTitle(t *testing.T) {
	t.Parallel()

	adapter := New(4)
	ctx := t.Context()
	deskID, err := adapter.CreateDesk(ctx, time.Now().UTC())
	require.NoError(t, err)

	topicHeader, status, _, err := adapter.CreateTopic(ctx, deskID, "topic")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, status)

	firstMeta, firstStatus, firstExisting, err := adapter.CreateMessage(ctx, topicHeader.TopicID, "Title", "title")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, firstStatus)
	require.Empty(t, firstExisting)

	_, duplicateStatus, duplicateExisting, err := adapter.CreateMessage(ctx, topicHeader.TopicID, "TITLE", "title")
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusDuplicateTitle, duplicateStatus)
	require.Equal(t, firstMeta.MessageID, duplicateExisting)
}

// TestCollectExpiredDeskIDs verifies expiration derives from created_at + ttl.
func TestCollectExpiredDeskIDs(t *testing.T) {
	t.Parallel()

	adapter := New(4)
	ctx := t.Context()
	createdAt := time.Now().UTC().Add(-2 * time.Hour)
	deskID, err := adapter.CreateDesk(ctx, createdAt)
	require.NoError(t, err)

	expired, err := adapter.CollectExpiredDeskIDs(ctx, time.Now().UTC(), time.Hour)
	require.NoError(t, err)
	require.Equal(t, []string{deskID}, expired)
}

// TestGetDeskSnapshotIncludesMessageIDs verifies cascade snapshot contains linked topics and messages.
func TestGetDeskSnapshotIncludesMessageIDs(t *testing.T) {
	t.Parallel()

	adapter := New(4)
	ctx := t.Context()
	deskID, err := adapter.CreateDesk(ctx, time.Now().UTC())
	require.NoError(t, err)

	topicHeader, _, _, err := adapter.CreateTopic(ctx, deskID, "topic")
	require.NoError(t, err)

	messageMeta, _, _, err := adapter.CreateMessage(ctx, topicHeader.TopicID, "Title", "title")
	require.NoError(t, err)

	snapshot, found, err := adapter.GetDeskSnapshot(ctx, deskID)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []string{topicHeader.TopicID}, snapshot.TopicIDs)
	require.Equal(t, []string{messageMeta.MessageID}, snapshot.MessageIDs)
}
