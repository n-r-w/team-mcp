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

type lockedBuffer struct {
	mu   sync.Mutex
	data bytes.Buffer
}

func (b *lockedBuffer) Write(payload []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.data.Write(payload)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.data.String()
}

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

// TestDeskCreateSuccess verifies desk_create creates in-memory and persistent desk state.
func TestDeskCreateSuccess(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().CreateDesk(ctx, gomock.Any()).Return("desk-1", nil)
	deskStore.EXPECT().EnsureDesk(ctx, "desk-1", gomock.Any()).Return(nil)

	result, err := service.DeskCreate(ctx)
	require.NoError(t, err)
	require.Equal(t, "desk-1", result.DeskID)
}

// TestDeskCreateEnsureDeskFailureIncludesRollbackError verifies rollback errors are returned with primary ensure-desk failure.
func TestDeskCreateEnsureDeskFailureIncludesRollbackError(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().CreateDesk(ctx, gomock.Any()).Return("desk-1", nil)
	deskStore.EXPECT().EnsureDesk(ctx, "desk-1", gomock.Any()).Return(errors.New("ensure failed"))
	runRegistry.EXPECT().DeleteDesk(ctx, "desk-1").Return(errors.New("rollback failed"))

	_, err := service.DeskCreate(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "initialize desk storage")
	require.ErrorContains(t, err, "rollback failed")
}

// TestDeskRemoveNotFound verifies desk_remove maps missing desk to business not_found.
func TestDeskRemoveNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().GetDeskSnapshot(ctx, "desk-1").Return(
		domain.DeskSnapshot{DeskID: "", CreatedAt: time.Time{}, TopicIDs: nil, MessageIDs: nil},
		false,
		nil,
	)

	result, err := service.DeskRemove(ctx, domain.DeskRemoveRequest{DeskID: "desk-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, result.Status)
}

// TestDeskRemoveSuccessLogsStatus verifies successful desk_remove emits status log.
func TestDeskRemoveSuccessLogsStatus(t *testing.T) {
	t.Parallel()

	logOutput, restoreLogger := captureSlogJSON(t)
	t.Cleanup(restoreLogger)

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().GetDeskSnapshot(ctx, "desk-1").Return(
		domain.DeskSnapshot{DeskID: "desk-1", CreatedAt: time.Time{}, TopicIDs: []string{"topic-1"}, MessageIDs: nil},
		true,
		nil,
	)
	headerQueue.EXPECT().DeleteDesk(ctx, "desk-1", []string{"topic-1"}).Return(nil)
	runRegistry.EXPECT().DeleteDesk(ctx, "desk-1").Return(nil)
	deskStore.EXPECT().DeleteDesk(ctx, "desk-1").Return(nil)

	result, err := service.DeskRemove(ctx, domain.DeskRemoveRequest{DeskID: "desk-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, result.Status)

	logs := logOutput.String()
	require.Contains(t, logs, `"event":"desk_remove"`)
	require.Contains(t, logs, `"status":"ok"`)
	require.Contains(t, logs, `"desk_id":"desk-1"`)
}

// TestTopicCreateSuccess verifies topic_create ensures deterministic topic ordering state.
func TestTopicCreateSuccess(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	header := domain.TopicHeader{TopicID: "topic-1", Title: "Topic"}
	runRegistry.EXPECT().CreateTopic(ctx, "desk-1", "Topic").Return(header, domain.BusinessStatusOK, true, nil)
	headerQueue.EXPECT().EnsureTopic(ctx, "desk-1", header).Return(nil)

	result, err := service.TopicCreate(ctx, domain.TopicCreateRequest{DeskID: "desk-1", Title: "Topic"})
	require.NoError(t, err)
	require.Equal(t, "topic-1", result.TopicID)
}

// TestTopicListNotFound verifies topic_list preserves not_found semantics for missing desks.
func TestTopicListNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().DeskExists(ctx, "desk-1").Return(false, nil)

	result, err := service.TopicList(ctx, domain.TopicListRequest{DeskID: "desk-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, result.Status)
	require.Nil(t, result.Topics)
}

// TestTopicListReturnsEmptySliceWhenHeaderQueueMissing verifies topic_list keeps ok status with empty slice when order state is absent.
func TestTopicListReturnsEmptySliceWhenHeaderQueueMissing(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().DeskExists(ctx, "desk-1").Return(true, nil)
	headerQueue.EXPECT().ListTopics(ctx, "desk-1").Return(nil, false, nil)

	result, err := service.TopicList(ctx, domain.TopicListRequest{DeskID: "desk-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, result.Status)
	require.NotNil(t, result.Topics)
	require.Empty(t, result.Topics)
}

// TestMessageCreateDuplicateTitle verifies duplicate normalized title maps to business duplicate_title payload.
func TestMessageCreateDuplicateTitle(t *testing.T) {
	t.Parallel()

	result, _, err := runMessageCreateDuplicateTitleScenario(t, false)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusDuplicateTitle, result.Status)
	require.Equal(t, "msg-existing", result.ExistingMessageID)
}

// TestMessageCreateDuplicateTitleLogsOutcome verifies duplicate-title business outcome is explicitly logged.
func TestMessageCreateDuplicateTitleLogsOutcome(t *testing.T) {
	t.Parallel()

	result, logs, err := runMessageCreateDuplicateTitleScenario(t, true)
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusDuplicateTitle, result.Status)
	require.Contains(t, logs, `"event":"message_create"`)
	require.Contains(t, logs, `"status":"duplicate_title"`)
	require.Contains(t, logs, `"topic_id":"topic-1"`)
}

// TestMessageListNotFound verifies message_list preserves not_found semantics for missing topics.
func TestMessageListNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().TopicExists(ctx, "topic-1").Return(false, nil)

	result, err := service.MessageList(ctx, domain.MessageListRequest{TopicID: "topic-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, result.Status)
	require.Nil(t, result.Messages)
}

// TestMessageListReturnsEmptySliceWhenHeaderQueueMissing verifies message_list keeps ok status with empty slice when order state is absent.
func TestMessageListReturnsEmptySliceWhenHeaderQueueMissing(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().TopicExists(ctx, "topic-1").Return(true, nil)
	headerQueue.EXPECT().ListMessages(ctx, "topic-1").Return(nil, false, nil)

	result, err := service.MessageList(ctx, domain.MessageListRequest{TopicID: "topic-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusOK, result.Status)
	require.NotNil(t, result.Messages)
	require.Empty(t, result.Messages)
}

func runMessageCreateDuplicateTitleScenario(t *testing.T, captureLogs bool) (domain.MessageCreateResult, string, error) {
	t.Helper()

	logs := ""
	var logOutput *lockedBuffer
	if captureLogs {
		capturedOutput, restoreLogger := captureSlogJSON(t)
		t.Cleanup(restoreLogger)
		logOutput = capturedOutput
	}

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().CreateMessage(
		ctx,
		"topic-1",
		"Title",
		"title",
	).Return(
		domain.MessageMeta{MessageID: "", TopicID: "", DeskID: "", Title: ""},
		domain.BusinessStatusDuplicateTitle,
		"msg-existing",
		nil,
	)

	result, err := service.MessageCreate(ctx, domain.MessageCreateRequest{TopicID: "topic-1", Title: "Title", Content: "Body"})
	if logOutput != nil {
		logs = logOutput.String()
	}

	return result, logs, err
}

// TestMessageCreatePersistFailureRollsBack verifies metadata rollback when payload persistence fails.
func TestMessageCreatePersistFailureRollsBack(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	meta := domain.MessageMeta{MessageID: "msg-1", TopicID: "topic-1", DeskID: "desk-1", Title: "Title"}
	runRegistry.EXPECT().CreateMessage(ctx, "topic-1", "Title", "title").Return(meta, domain.BusinessStatusOK, "", nil)
	deskStore.EXPECT().PersistMessage(ctx, "desk-1", "msg-1", "Body").Return(errors.New("persist failed"))
	runRegistry.EXPECT().DeleteMessage(ctx, "msg-1").Return(nil)

	_, err := service.MessageCreate(ctx, domain.MessageCreateRequest{TopicID: "topic-1", Title: "Title", Content: "Body"})
	require.Error(t, err)
	require.ErrorContains(t, err, "persist message payload")
}

// TestMessageCreatePersistFailureIncludesRollbackError verifies persist failure returns rollback metadata error when rollback fails.
func TestMessageCreatePersistFailureIncludesRollbackError(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	meta := domain.MessageMeta{MessageID: "msg-1", TopicID: "topic-1", DeskID: "desk-1", Title: "Title"}
	runRegistry.EXPECT().CreateMessage(ctx, "topic-1", "Title", "title").Return(meta, domain.BusinessStatusOK, "", nil)
	deskStore.EXPECT().PersistMessage(ctx, "desk-1", "msg-1", "Body").Return(errors.New("persist failed"))
	runRegistry.EXPECT().DeleteMessage(ctx, "msg-1").Return(errors.New("rollback failed"))

	_, err := service.MessageCreate(ctx, domain.MessageCreateRequest{TopicID: "topic-1", Title: "Title", Content: "Body"})
	require.Error(t, err)
	require.ErrorContains(t, err, "persist message payload")
	require.ErrorContains(t, err, "rollback failed")
}

// TestMessageCreateAppendFailureIncludesRollbackErrors verifies append failure preserves joined rollback errors.
func TestMessageCreateAppendFailureIncludesRollbackErrors(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	meta := domain.MessageMeta{MessageID: "msg-1", TopicID: "topic-1", DeskID: "desk-1", Title: "Title"}
	runRegistry.EXPECT().CreateMessage(ctx, "topic-1", "Title", "title").Return(meta, domain.BusinessStatusOK, "", nil)
	deskStore.EXPECT().PersistMessage(ctx, "desk-1", "msg-1", "Body").Return(nil)
	headerQueue.EXPECT().AppendMessage(ctx, "topic-1", domain.MessageHeader{MessageID: "msg-1", Title: "Title"}).
		Return(errors.New("append failed"))
	runRegistry.EXPECT().DeleteMessage(ctx, "msg-1").Return(errors.New("rollback metadata failed"))
	deskStore.EXPECT().DeleteMessage(ctx, "desk-1", "msg-1").Return(errors.New("rollback payload failed"))

	_, err := service.MessageCreate(ctx, domain.MessageCreateRequest{TopicID: "topic-1", Title: "Title", Content: "Body"})
	require.Error(t, err)
	require.ErrorContains(t, err, "append message header")
	require.ErrorContains(t, err, "rollback metadata failed")
	require.ErrorContains(t, err, "rollback payload failed")
}

// TestMessageGetNotFound verifies message_get returns not_found business status for missing message IDs.
func TestMessageGetNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().GetMessageMeta(ctx, "msg-1").Return(
		domain.MessageMeta{MessageID: "", TopicID: "", DeskID: "", Title: ""},
		false,
		nil,
	)

	result, err := service.MessageGet(ctx, domain.MessageGetRequest{MessageID: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, domain.BusinessStatusNotFound, result.Status)
}

// TestRunLifecycleCollectorStartupDiskCleanup verifies startup cleanup scans and removes expired desk directories.
func TestRunLifecycleCollectorStartupDiskCleanup(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx, cancel := context.WithCancel(t.Context())
	deskStore.EXPECT().CollectExpiredDeskIDs(gomock.Any(), gomock.Any(), time.Hour).Return([]string{"desk-expired"}, nil)
	deskStore.EXPECT().DeleteDesk(gomock.Any(), "desk-expired").DoAndReturn(func(context.Context, string) error {
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
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	err := service.RunLifecycleCollector(t.Context(), 0)
	require.ErrorContains(t, err, "collect interval must be greater than 0")
}

// TestRunLifecycleCollectorLogsSuccessResults verifies startup and runtime cleanup success results are logged.
func TestRunLifecycleCollectorLogsSuccessResults(t *testing.T) {
	t.Parallel()

	logOutput, restoreLogger := captureSlogJSON(t)
	t.Cleanup(restoreLogger)

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx, cancel := context.WithCancel(t.Context())
	deskStore.EXPECT().CollectExpiredDeskIDs(gomock.Any(), gomock.Any(), time.Hour).Return([]string{}, nil).AnyTimes()
	runRegistry.EXPECT().CollectExpiredDeskIDs(gomock.Any(), gomock.Any(), time.Hour).DoAndReturn(
		func(context.Context, time.Time, time.Duration) ([]string, error) {
			cancel()

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

// TestCleanupAllRunsRemovesActiveDesks verifies shutdown cleanup removes all known active desks.
func TestCleanupAllRunsRemovesActiveDesks(t *testing.T) {
	t.Parallel()

	_, err := runCleanupAllRunsSuccessScenario(t, false)
	require.NoError(t, err)
}

// TestCleanupAllRunsLogsSummary verifies shutdown cleanup summary result is logged.
func TestCleanupAllRunsLogsSummary(t *testing.T) {
	t.Parallel()

	logs, err := runCleanupAllRunsSuccessScenario(t, true)
	require.NoError(t, err)
	require.Contains(t, logs, `"event":"shutdown_cleanup"`)
	require.Contains(t, logs, `"result":"ok"`)
}

func runCleanupAllRunsSuccessScenario(t *testing.T, captureLogs bool) (string, error) {
	t.Helper()

	logs := ""
	var logOutput *lockedBuffer
	if captureLogs {
		capturedOutput, restoreLogger := captureSlogJSON(t)
		t.Cleanup(restoreLogger)
		logOutput = capturedOutput
	}

	controller := gomock.NewController(t)
	runRegistry := NewMockIRunRegistry(controller)
	deskStore := NewMockIDeskStore(controller)
	headerQueue := NewMockIHeaderQueue(controller)

	service := New(runRegistry, deskStore, headerQueue, Options{SessionTTL: time.Hour, MaxTitleLength: 200})

	ctx := t.Context()
	runRegistry.EXPECT().ListDeskIDs(ctx).Return([]string{"desk-1"}, nil)
	runRegistry.EXPECT().GetDeskSnapshot(ctx, "desk-1").Return(
		domain.DeskSnapshot{DeskID: "desk-1", CreatedAt: time.Time{}, TopicIDs: []string{"topic-1"}, MessageIDs: nil},
		true,
		nil,
	)
	headerQueue.EXPECT().DeleteDesk(ctx, "desk-1", []string{"topic-1"}).Return(nil)
	runRegistry.EXPECT().DeleteDesk(ctx, "desk-1").Return(nil)
	deskStore.EXPECT().DeleteDesk(ctx, "desk-1").Return(nil)

	err := service.CleanupAllRuns(ctx)
	if logOutput != nil {
		logs = logOutput.String()
	}

	return logs, err
}
