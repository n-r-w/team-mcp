package usecase

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/team-mcp/internal/domain"
)

var slogTestMu sync.Mutex

// lockedBuffer serializes concurrent slog writes so log assertions stay deterministic.
type lockedBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

// Write keeps concurrent slog output ordered for tests that inspect JSON logs.
func (b *lockedBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.data.Write(payload)
}

// String returns the buffered log payload after all writes complete.
func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.data.String()
}

// captureSlogJSON redirects the default logger to one in-memory JSON buffer for assertions.
func captureSlogJSON(t *testing.T) (*lockedBuffer, func()) {
	t.Helper()

	slogTestMu.Lock()

	output := &lockedBuffer{mu: sync.Mutex{}, data: bytes.Buffer{}}
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{
		AddSource:   false,
		Level:       slog.LevelDebug,
		ReplaceAttr: nil,
	})))

	restore := func() {
		slog.SetDefault(originalLogger)
		slogTestMu.Unlock()
	}

	return output, restore
}

// TestDeskCreateSuccess verifies desk_create delegates to the authoritative board store.
func TestDeskCreateSuccess(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().CreateDesk(ctx, gomock.Any()).Return("desk-1", nil)

	result, err := service.DeskCreate(ctx)
	require.NoError(t, err)
	require.Equal(t, "desk-1", result.DeskID)
}

// TestDeskCreateStoreFailureWrapsError verifies desk_create returns authoritative store errors with context.
func TestDeskCreateStoreFailureWrapsError(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().CreateDesk(ctx, gomock.Any()).Return("", errors.New("create failed"))

	_, err := service.DeskCreate(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "create desk")
	require.ErrorContains(t, err, "create failed")
}

// TestTopicCreateSuccess verifies topic_create delegates to the authoritative board store.
func TestTopicCreateSuccess(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	header := domain.TopicHeader{TopicID: "topic-1", Title: "Topic"}
	boardStore.EXPECT().CreateTopic(ctx, "desk-1", "Topic").Return(header, domain.BusinessStatusOK, true, nil)

	result, err := service.TopicCreate(ctx, domain.TopicCreateRequest{DeskID: "desk-1", Title: "Topic"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, result.Status)
	require.Equal(t, "topic-1", result.TopicID)
}

// TestTopicListNotFound verifies topic_list preserves not_found semantics for missing desks.
func TestTopicListNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().ListTopics(ctx, "desk-1").Return(nil, false, nil)

	result, err := service.TopicList(ctx, domain.TopicListRequest{DeskID: "desk-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, result.Status)
	require.Nil(t, result.Topics)
}

// TestTopicListReturnsEmptySlice verifies topic_list preserves empty-slice semantics for an existing empty desk.
func TestTopicListReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().ListTopics(ctx, "desk-1").Return([]domain.TopicHeader{}, true, nil)

	result, err := service.TopicList(ctx, domain.TopicListRequest{DeskID: "desk-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, result.Status)
	require.NotNil(t, result.Topics)
	require.Empty(t, result.Topics)
}

// TestMessageCreateDuplicateTitleLogsOutcome verifies duplicate-title business outcomes stay visible in logs.
func TestMessageCreateDuplicateTitleLogsOutcome(t *testing.T) {
	t.Parallel()

	result, logs, err := runMessageCreateDuplicateTitleScenario(t)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusDuplicateTitle, result.Status)
	require.Equal(t, "msg-existing", result.ExistingMessageID)
	require.Contains(t, logs, `"event":"message_create"`)
	require.Contains(t, logs, `"status":"duplicate_title"`)
	require.Contains(t, logs, `"topic_id":"topic-1"`)
}

// TestMessageCreateStoreFailureWrapsError verifies message_create surfaces authoritative store failures as tool errors.
func TestMessageCreateStoreFailureWrapsError(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().CreateMessage(ctx, "topic-1", "Title", "title", "Body").Return(
		domain.MessageMeta{MessageID: "", TopicID: "", DeskID: "", Title: ""},
		domain.BusinessStatus(""),
		"",
		errors.New("store failed"),
	)

	_, err := service.MessageCreate(ctx, domain.MessageCreateRequest{TopicID: "topic-1", Title: "Title", Content: "Body"})
	require.Error(t, err)
	require.ErrorContains(t, err, "create message")
	require.ErrorContains(t, err, "store failed")
}

// TestMessageListNotFound verifies message_list preserves not_found semantics for missing topics.
func TestMessageListNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().ListMessages(ctx, "topic-1").Return(nil, false, nil)

	result, err := service.MessageList(ctx, domain.MessageListRequest{TopicID: "topic-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, result.Status)
	require.Nil(t, result.Messages)
}

// TestMessageListReturnsEmptySlice verifies message_list preserves empty-slice semantics for an existing empty topic.
func TestMessageListReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().ListMessages(ctx, "topic-1").Return([]domain.MessageHeader{}, true, nil)

	result, err := service.MessageList(ctx, domain.MessageListRequest{TopicID: "topic-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, result.Status)
	require.NotNil(t, result.Messages)
	require.Empty(t, result.Messages)
}

// TestMessageGetNotFound verifies message_get returns not_found business status for missing message IDs.
func TestMessageGetNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().GetMessage(ctx, "msg-1").Return(
		domain.MessageMeta{MessageID: "", TopicID: "", DeskID: "", Title: ""},
		"",
		false,
		nil,
	)

	result, err := service.MessageGet(ctx, domain.MessageGetRequest{MessageID: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, result.Status)
}

// TestRunLifecycleCollectorStartupCleanup verifies startup cleanup scans and removes expired desks from the authoritative store.
func TestRunLifecycleCollectorStartupCleanup(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx, cancel := context.WithCancel(t.Context())
	boardStore.EXPECT().CollectExpiredDeskIDs(gomock.Any(), gomock.Any(), time.Hour).Return([]string{"desk-expired"}, nil)
	boardStore.EXPECT().DeleteDesk(gomock.Any(), "desk-expired").DoAndReturn(func(context.Context, string) error {
		cancel()

		return nil
	})

	err := service.RunLifecycleCollector(ctx, time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
}

// TestRunLifecycleCollectorRejectsNonPositiveInterval verifies collector fails fast for non-positive intervals.
func TestRunLifecycleCollectorRejectsNonPositiveInterval(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	err := service.RunLifecycleCollector(t.Context(), 0)
	require.ErrorContains(t, err, "collect interval must be greater than 0")
}

// TestRunLifecycleCollectorLogsSuccessResults verifies startup and runtime cleanup success results are logged.
func TestRunLifecycleCollectorLogsSuccessResults(t *testing.T) {
	t.Parallel()

	logOutput, restoreLogger := captureSlogJSON(t)
	t.Cleanup(restoreLogger)

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx, cancel := context.WithCancel(t.Context())
	collectCallCount := 0
	boardStore.EXPECT().CollectExpiredDeskIDs(gomock.Any(), gomock.Any(), time.Hour).DoAndReturn(
		func(context.Context, time.Time, time.Duration) ([]string, error) {
			collectCallCount++
			if collectCallCount >= 2 {
				cancel()
			}

			return []string{}, nil
		},
	).AnyTimes()

	done := make(chan error, 1)
	go func() {
		done <- service.RunLifecycleCollector(ctx, time.Millisecond)
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("collector did not stop in time")
	}

	logs := logOutput.String()
	require.Contains(t, logs, `"event":"startup_gc"`)
	require.Contains(t, logs, `"event":"runtime_gc"`)
	require.Contains(t, logs, `"msg":"startup desk cleanup completed"`)
	require.Contains(t, logs, `"msg":"runtime desk cleanup completed"`)
	require.Contains(t, logs, `"level":"INFO"`)
	require.Contains(t, logs, `"level":"DEBUG"`)
	require.GreaterOrEqual(t, strings.Count(logs, `"result":"ok"`), 2)
}

// runMessageCreateDuplicateTitleScenario exercises the duplicate-title branch while capturing logs.
func runMessageCreateDuplicateTitleScenario(t *testing.T) (domain.MessageCreateResult, string, error) {
	t.Helper()

	logOutput, restoreLogger := captureSlogJSON(t)
	t.Cleanup(restoreLogger)

	controller := gomock.NewController(t)
	boardStore := NewMockIBoardStore(controller)
	service := New(boardStore, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	boardStore.EXPECT().CreateMessage(
		ctx,
		"topic-1",
		"Title",
		"title",
		"Body",
	).Return(
		domain.MessageMeta{MessageID: "", TopicID: "", DeskID: "", Title: ""},
		domain.BusinessStatusDuplicateTitle,
		"msg-existing",
		nil,
	)

	result, err := service.MessageCreate(ctx, domain.MessageCreateRequest{TopicID: "topic-1", Title: "Title", Content: "Body"})

	return result, logOutput.String(), err
}
