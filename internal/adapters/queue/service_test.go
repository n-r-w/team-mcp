package queue

import (
	"testing"

	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/stretchr/testify/require"
)

// TestTopicOrdering verifies topic headers preserve insertion order.
func TestTopicOrdering(t *testing.T) {
	t.Parallel()

	adapter := New(10)
	ctx := t.Context()
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t1", Title: "A"}))
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t2", Title: "B"}))

	topics, found, err := adapter.ListTopics(ctx, "desk-1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.TopicHeader{{TopicID: "t1", Title: "A"}, {TopicID: "t2", Title: "B"}}, topics)
}

// TestTopicOrderingIdempotentEnsure verifies duplicate EnsureTopic does not duplicate or reorder headers.
func TestTopicOrderingIdempotentEnsure(t *testing.T) {
	t.Parallel()

	adapter := New(10)
	ctx := t.Context()
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t1", Title: "A"}))
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t2", Title: "B"}))
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t1", Title: "A"}))

	topics, found, err := adapter.ListTopics(ctx, "desk-1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.TopicHeader{{TopicID: "t1", Title: "A"}, {TopicID: "t2", Title: "B"}}, topics)
}

// TestMessageOrdering verifies message headers preserve insertion order.
func TestMessageOrdering(t *testing.T) {
	t.Parallel()

	adapter := New(10)
	ctx := t.Context()
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t1", Title: "A"}))
	require.NoError(t, adapter.AppendMessage(ctx, "t1", domain.MessageHeader{MessageID: "m1", Title: "1"}))
	require.NoError(t, adapter.AppendMessage(ctx, "t1", domain.MessageHeader{MessageID: "m2", Title: "2"}))

	messages, found, err := adapter.ListMessages(ctx, "t1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.MessageHeader{{MessageID: "m1", Title: "1"}, {MessageID: "m2", Title: "2"}}, messages)
}

// TestAppendMessageRespectsCapacity verifies per-topic message header capacity is enforced.
func TestAppendMessageRespectsCapacity(t *testing.T) {
	t.Parallel()

	adapter := New(1)
	ctx := t.Context()
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t1", Title: "A"}))
	require.NoError(t, adapter.AppendMessage(ctx, "t1", domain.MessageHeader{MessageID: "m1", Title: "1"}))

	err := adapter.AppendMessage(ctx, "t1", domain.MessageHeader{MessageID: "m2", Title: "2"})
	require.Error(t, err)

	var domainErr domain.Error
	require.ErrorAs(t, err, &domainErr)
	require.Equal(t, domain.ErrorCodeCapacityExceeded, domainErr.Code())
}

// TestRemoveMessage verifies message headers can be removed without affecting other items.
func TestRemoveMessage(t *testing.T) {
	t.Parallel()

	adapter := New(10)
	ctx := t.Context()
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t1", Title: "A"}))
	require.NoError(t, adapter.AppendMessage(ctx, "t1", domain.MessageHeader{MessageID: "m1", Title: "1"}))
	require.NoError(t, adapter.AppendMessage(ctx, "t1", domain.MessageHeader{MessageID: "m2", Title: "2"}))

	require.NoError(t, adapter.RemoveMessage(ctx, "t1", "m1"))
	messages, found, err := adapter.ListMessages(ctx, "t1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []domain.MessageHeader{{MessageID: "m2", Title: "2"}}, messages)
}

// TestDeleteDesk verifies desk cleanup removes desk and linked topics.
func TestDeleteDesk(t *testing.T) {
	t.Parallel()

	adapter := New(10)
	ctx := t.Context()
	require.NoError(t, adapter.EnsureTopic(ctx, "desk-1", domain.TopicHeader{TopicID: "t1", Title: "A"}))
	require.NoError(t, adapter.DeleteDesk(ctx, "desk-1", []string{"t1"}))

	_, deskFound, err := adapter.ListTopics(ctx, "desk-1")
	require.NoError(t, err)
	require.False(t, deskFound)

	_, topicFound, err := adapter.ListMessages(ctx, "t1")
	require.NoError(t, err)
	require.False(t, topicFound)
}
