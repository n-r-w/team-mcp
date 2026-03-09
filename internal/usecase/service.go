package usecase

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/n-r-w/team-mcp/internal/server"
)

var _ server.ICoordination = (*Service)(nil)

// Service coordinates desk/topic/message workflows across outbound adapters.
type Service struct {
	runRegistry IRunRegistry
	deskStore   IDeskStore
	headerQueue IHeaderQueue
	options     Options
}

// New builds coordination use case service with explicit dependencies.
func New(
	runRegistry IRunRegistry,
	deskStore IDeskStore,
	headerQueue IHeaderQueue,
	options Options,
) *Service {
	return &Service{
		runRegistry: runRegistry,
		deskStore:   deskStore,
		headerQueue: headerQueue,
		options:     options,
	}
}

// DeskCreate creates desk and initializes persistent storage metadata.
func (s *Service) DeskCreate(ctx context.Context) (domain.DeskCreateResult, error) {
	createdAt := time.Now().UTC()
	deskID, err := s.runRegistry.CreateDesk(ctx, createdAt)
	if err != nil {
		return domain.DeskCreateResult{}, fmt.Errorf("create desk: %w", err)
	}

	ensureDeskErr := s.deskStore.EnsureDesk(ctx, deskID, createdAt)
	if ensureDeskErr != nil {
		rollbackErr := s.runRegistry.DeleteDesk(ctx, deskID)
		if rollbackErr != nil {
			return domain.DeskCreateResult{}, errors.Join(
				fmt.Errorf("initialize desk storage: %w", ensureDeskErr),
				fmt.Errorf("rollback desk metadata: %w", rollbackErr),
			)
		}

		return domain.DeskCreateResult{}, fmt.Errorf("initialize desk storage: %w", ensureDeskErr)
	}

	slog.InfoContext(ctx, "desk created", logFieldEvent, eventDeskCreate, logFieldDeskID, deskID)

	return domain.DeskCreateResult{DeskID: deskID}, nil
}

// DeskRemove synchronously removes all desk-linked in-memory and disk data.
func (s *Service) DeskRemove(ctx context.Context, request domain.DeskRemoveRequest) (domain.DeskRemoveResult, error) {
	snapshot, found, err := s.runRegistry.GetDeskSnapshot(ctx, request.DeskID)
	if err != nil {
		return domain.DeskRemoveResult{}, fmt.Errorf("resolve desk snapshot: %w", err)
	}

	if !found {
		result := domain.DeskRemoveResult{Status: domain.BusinessStatusNotFound}
		slog.InfoContext(
			ctx,
			"desk remove completed",
			logFieldEvent,
			eventDeskRemove,
			logFieldDeskID,
			request.DeskID,
			logFieldStatus,
			result.Status,
		)

		return result, nil
	}

	removeDeskErr := s.removeDeskCascade(ctx, snapshot, false)
	if removeDeskErr != nil {
		return domain.DeskRemoveResult{}, fmt.Errorf("cascade desk remove: %w", removeDeskErr)
	}

	result := domain.DeskRemoveResult{Status: domain.BusinessStatusOK}
	slog.InfoContext(
		ctx,
		"desk remove completed",
		logFieldEvent,
		eventDeskRemove,
		logFieldDeskID,
		request.DeskID,
		logFieldStatus,
		result.Status,
	)

	return result, nil
}

// TopicCreate creates topic in desk or returns existing topic identifier for idempotent calls.
func (s *Service) TopicCreate(
	ctx context.Context,
	request domain.TopicCreateRequest,
) (domain.TopicCreateResult, error) {
	if err := s.validateTitle(request.Title); err != nil {
		return domain.TopicCreateResult{}, err
	}

	header, status, _, err := s.runRegistry.CreateTopic(ctx, request.DeskID, request.Title)
	if err != nil {
		return domain.TopicCreateResult{}, fmt.Errorf("create topic: %w", err)
	}

	if status == domain.BusinessStatusNotFound {
		return domain.TopicCreateResult{Status: domain.BusinessStatusNotFound, TopicID: ""}, nil
	}

	ensureTopicErr := s.headerQueue.EnsureTopic(ctx, request.DeskID, header)
	if ensureTopicErr != nil {
		return domain.TopicCreateResult{}, fmt.Errorf("ensure topic order: %w", ensureTopicErr)
	}

	return domain.TopicCreateResult{Status: domain.BusinessStatusOK, TopicID: header.TopicID}, nil
}

// TopicList returns deterministic ordered topic headers for desk.
func (s *Service) TopicList(
	ctx context.Context,
	request domain.TopicListRequest,
) (domain.TopicListResult, error) {
	return listOrderedResult(
		ctx,
		request.DeskID,
		s.runRegistry.DeskExists,
		s.headerQueue.ListTopics,
		"check desk existence",
		"list topics",
		topicListResult,
	)
}

// MessageCreate creates message metadata and payload while preserving duplicate-title semantics.
func (s *Service) MessageCreate(
	ctx context.Context,
	request domain.MessageCreateRequest,
) (domain.MessageCreateResult, error) {
	if err := s.validateTitle(request.Title); err != nil {
		return domain.MessageCreateResult{}, err
	}

	meta, status, existingMessageID, err := s.createMessageMetadata(ctx, request)
	if err != nil {
		return domain.MessageCreateResult{}, err
	}

	if result, done := buildMessageCreateBusinessResult(status, existingMessageID); done {
		s.logMessageCreateResult(ctx, request.TopicID, result)

		return result, nil
	}

	persistErr := s.persistMessagePayload(ctx, request.TopicID, meta, request.Content)
	if persistErr != nil {
		return domain.MessageCreateResult{}, persistErr
	}

	appendHeaderErr := s.appendMessageHeader(ctx, meta, request.Title)
	if appendHeaderErr != nil {
		return domain.MessageCreateResult{}, appendHeaderErr
	}

	result := domain.MessageCreateResult{
		Status:            domain.BusinessStatusOK,
		MessageID:         meta.MessageID,
		ExistingMessageID: "",
		StatusMessage:     "",
	}
	s.logMessageCreateResult(ctx, meta.TopicID, result)

	return result, nil
}

func (s *Service) createMessageMetadata(
	ctx context.Context,
	request domain.MessageCreateRequest,
) (domain.MessageMeta, domain.BusinessStatus, string, error) {
	normalizedTitle := normalizeMessageTitle(request.Title)
	meta, status, existingMessageID, err := s.runRegistry.CreateMessage(
		ctx,
		request.TopicID,
		request.Title,
		normalizedTitle,
	)
	if err != nil {
		slog.ErrorContext(
			ctx,
			"message create failed",
			logFieldEvent,
			eventMessageCreate,
			logFieldTopicID,
			request.TopicID,
			logFieldError,
			err,
		)

		return domain.MessageMeta{}, "", "", fmt.Errorf("create message metadata: %w", err)
	}

	return meta, status, existingMessageID, nil
}

func buildMessageCreateBusinessResult(
	status domain.BusinessStatus,
	existingMessageID string,
) (domain.MessageCreateResult, bool) {
	if status == domain.BusinessStatusNotFound {
		return domain.MessageCreateResult{
			Status:            domain.BusinessStatusNotFound,
			MessageID:         "",
			ExistingMessageID: "",
			StatusMessage:     "",
		}, true
	}

	if status == domain.BusinessStatusDuplicateTitle {
		return domain.MessageCreateResult{
			Status:            domain.BusinessStatusDuplicateTitle,
			MessageID:         "",
			ExistingMessageID: existingMessageID,
			StatusMessage:     "message with the same normalized title already exists",
		}, true
	}

	return domain.MessageCreateResult{
		Status:            "",
		MessageID:         "",
		ExistingMessageID: "",
		StatusMessage:     "",
	}, false
}

func (s *Service) persistMessagePayload(
	ctx context.Context,
	topicID string,
	meta domain.MessageMeta,
	content string,
) error {
	if err := s.deskStore.PersistMessage(ctx, meta.DeskID, meta.MessageID, content); err != nil {
		rollbackErr := s.runRegistry.DeleteMessage(ctx, meta.MessageID)

		errorPayload := err
		if rollbackErr != nil {
			errorPayload = errors.Join(err, rollbackErr)
		}

		slog.ErrorContext(
			ctx,
			"message create failed",
			logFieldEvent,
			eventMessageCreate,
			logFieldTopicID,
			topicID,
			logFieldDeskID,
			meta.DeskID,
			logFieldMessageID,
			meta.MessageID,
			logFieldError,
			errorPayload,
		)

		if rollbackErr != nil {
			return errors.Join(
				fmt.Errorf("persist message payload: %w", err),
				fmt.Errorf("rollback message metadata: %w", rollbackErr),
			)
		}

		return fmt.Errorf("persist message payload: %w", err)
	}

	return nil
}

func (s *Service) appendMessageHeader(ctx context.Context, meta domain.MessageMeta, title string) error {
	header := domain.MessageHeader{MessageID: meta.MessageID, Title: title}
	if err := s.headerQueue.AppendMessage(ctx, meta.TopicID, header); err != nil {
		rollbackErr := errors.Join(
			s.runRegistry.DeleteMessage(ctx, meta.MessageID),
			s.deskStore.DeleteMessage(ctx, meta.DeskID, meta.MessageID),
		)

		errorPayload := err
		if rollbackErr != nil {
			errorPayload = errors.Join(err, rollbackErr)
		}

		slog.ErrorContext(
			ctx,
			"message create failed",
			logFieldEvent,
			eventMessageCreate,
			logFieldTopicID,
			meta.TopicID,
			logFieldDeskID,
			meta.DeskID,
			logFieldMessageID,
			meta.MessageID,
			logFieldError,
			errorPayload,
		)

		if rollbackErr != nil {
			return errors.Join(fmt.Errorf("append message header: %w", err), rollbackErr)
		}

		return fmt.Errorf("append message header: %w", err)
	}

	return nil
}

// MessageList returns deterministic ordered message headers for topic.
func (s *Service) MessageList(
	ctx context.Context,
	request domain.MessageListRequest,
) (domain.MessageListResult, error) {
	return listOrderedResult(
		ctx,
		request.TopicID,
		s.runRegistry.TopicExists,
		s.headerQueue.ListMessages,
		"check topic existence",
		"list message headers",
		messageListResult,
	)
}

// listOrderedResult resolves parent existence first and maps ordered headers into the target business result.
func listOrderedResult[T any, R any](
	ctx context.Context,
	id string,
	existsFn func(context.Context, string) (bool, error),
	listFn func(context.Context, string) ([]T, bool, error),
	existsErrMessage string,
	listErrMessage string,
	buildResult func(domain.BusinessStatus, []T) R,
) (R, error) {
	exists, err := existsFn(ctx, id)
	if err != nil {
		var zeroResult R

		return zeroResult, fmt.Errorf("%s: %w", existsErrMessage, err)
	}

	if !exists {
		return buildResult(domain.BusinessStatusNotFound, nil), nil
	}

	headers, found, err := listFn(ctx, id)
	if err != nil {
		var zeroResult R

		return zeroResult, fmt.Errorf("%s: %w", listErrMessage, err)
	}

	if !found {
		headers = []T{}
	}

	return buildResult(domain.BusinessStatusOK, headers), nil
}

// topicListResult preserves business not_found and empty-slice semantics for topic listing.
func topicListResult(status domain.BusinessStatus, topics []domain.TopicHeader) domain.TopicListResult {
	if status == domain.BusinessStatusNotFound {
		return domain.TopicListResult{Status: status, Topics: nil}
	}

	return domain.TopicListResult{Status: status, Topics: topics}
}

// messageListResult preserves business not_found and empty-slice semantics for message listing.
func messageListResult(status domain.BusinessStatus, messages []domain.MessageHeader) domain.MessageListResult {
	if status == domain.BusinessStatusNotFound {
		return domain.MessageListResult{Status: status, Messages: nil}
	}

	return domain.MessageListResult{Status: status, Messages: messages}
}

// MessageGet returns full message body for existing message identifier.
func (s *Service) MessageGet(ctx context.Context, request domain.MessageGetRequest) (domain.MessageGetResult, error) {
	meta, found, err := s.runRegistry.GetMessageMeta(ctx, request.MessageID)
	if err != nil {
		return domain.MessageGetResult{}, fmt.Errorf("resolve message metadata: %w", err)
	}

	if !found {
		return domain.MessageGetResult{Status: domain.BusinessStatusNotFound, Title: "", Content: ""}, nil
	}

	content, err := s.deskStore.ResolveMessage(ctx, meta.DeskID, meta.MessageID)
	if err != nil {
		return domain.MessageGetResult{}, fmt.Errorf("resolve message payload: %w", err)
	}

	return domain.MessageGetResult{
		Status:  domain.BusinessStatusOK,
		Title:   meta.Title,
		Content: content,
	}, nil
}

// CleanupAllRuns removes all active desks from memory and disk on shutdown.
func (s *Service) CleanupAllRuns(ctx context.Context) error {
	deskIDs, err := s.runRegistry.ListDeskIDs(ctx)
	if err != nil {
		slog.ErrorContext(
			ctx,
			"shutdown cleanup failed",
			logFieldEvent,
			eventShutdownCleanup,
			logFieldResult,
			logResultError,
			logFieldError,
			err,
		)

		return fmt.Errorf("list active desks: %w", err)
	}

	var cleanupErr error
	cleanedDesks := 0
	failedDesks := 0
	for _, deskID := range deskIDs {
		snapshot, found, snapshotErr := s.runRegistry.GetDeskSnapshot(ctx, deskID)
		if snapshotErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("resolve desk snapshot %s: %w", deskID, snapshotErr))
			failedDesks++

			continue
		}

		if !found {
			continue
		}

		removeDeskErr := s.removeDeskCascade(ctx, snapshot, true)
		if removeDeskErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup desk %s: %w", deskID, removeDeskErr))
			failedDesks++

			continue
		}

		cleanedDesks++
	}

	if cleanupErr != nil {
		slog.ErrorContext(
			ctx,
			"shutdown cleanup completed with errors",
			logFieldEvent,
			eventShutdownCleanup,
			logFieldResult,
			logResultError,
			logFieldDeskCount,
			len(deskIDs),
			logFieldCleanedDesks,
			cleanedDesks,
			logFieldFailedDesks,
			failedDesks,
			logFieldError,
			cleanupErr,
		)

		return cleanupErr
	}

	slog.InfoContext(
		ctx,
		"shutdown cleanup completed",
		logFieldEvent,
		eventShutdownCleanup,
		logFieldResult,
		logResultOK,
		logFieldDeskCount,
		len(deskIDs),
		logFieldCleanedDesks,
		cleanedDesks,
		logFieldFailedDesks,
		failedDesks,
	)

	return nil
}

// RunLifecycleCollector executes startup and periodic cleanup for expired desks.
func (s *Service) RunLifecycleCollector(ctx context.Context, collectInterval time.Duration) error {
	if collectInterval <= 0 {
		return errors.New("collect interval must be greater than 0")
	}

	now := time.Now().UTC()
	if err := s.cleanupExpiredOnDisk(ctx, now); err != nil {
		slog.ErrorContext(
			ctx,
			"startup desk cleanup failed",
			logFieldEvent,
			eventStartupGC,
			logFieldResult,
			logResultError,
			logFieldError,
			err,
		)
	} else {
		slog.InfoContext(ctx, "startup desk cleanup completed", logFieldEvent, eventStartupGC, logFieldResult, logResultOK)
	}

	ticker := time.NewTicker(collectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case currentTime := <-ticker.C:
			inMemoryErr := s.cleanupExpiredInMemory(ctx, currentTime.UTC())
			onDiskErr := s.cleanupExpiredOnDisk(ctx, currentTime.UTC())
			runtimeCleanupErr := errors.Join(inMemoryErr, onDiskErr)
			if runtimeCleanupErr != nil {
				slog.ErrorContext(
					ctx,
					"runtime desk cleanup failed",
					logFieldEvent,
					eventRuntimeGC,
					logFieldResult,
					logResultError,
					logFieldError,
					runtimeCleanupErr,
				)

				continue
			}

			slog.DebugContext(ctx, "runtime desk cleanup completed", logFieldEvent, eventRuntimeGC, logFieldResult, logResultOK)
		}
	}
}

// logMessageCreateResult records business outcomes for message_create.
// It distinguishes business statuses from tool errors in logs.
func (s *Service) logMessageCreateResult(ctx context.Context, topicID string, result domain.MessageCreateResult) {
	attributes := []any{
		logFieldEvent,
		eventMessageCreate,
		logFieldTopicID,
		topicID,
		logFieldStatus,
		result.Status,
	}

	if result.MessageID != "" {
		attributes = append(attributes, logFieldMessageID, result.MessageID)
	}

	if result.ExistingMessageID != "" {
		attributes = append(attributes, logFieldExistingMessageID, result.ExistingMessageID)
	}

	slog.InfoContext(ctx, "message create completed", attributes...)
}

// cleanupExpiredInMemory removes expired desk state derived from created_at + ttl.
func (s *Service) cleanupExpiredInMemory(ctx context.Context, now time.Time) error {
	expiredDeskIDs, err := s.runRegistry.CollectExpiredDeskIDs(ctx, now, s.options.SessionTTL)
	if err != nil {
		return fmt.Errorf("collect expired desks: %w", err)
	}

	var cleanupErr error
	for _, deskID := range expiredDeskIDs {
		snapshot, found, snapshotErr := s.runRegistry.GetDeskSnapshot(ctx, deskID)
		if snapshotErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("resolve expired desk snapshot %s: %w", deskID, snapshotErr))

			continue
		}

		if !found {
			continue
		}

		removeDeskErr := s.removeDeskCascade(ctx, snapshot, true)
		if removeDeskErr != nil {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("cleanup expired desk %s: %w", deskID, removeDeskErr))
		}
	}

	return cleanupErr
}

// cleanupExpiredOnDisk removes expired desk directories and ignores concurrent delete races.
func (s *Service) cleanupExpiredOnDisk(ctx context.Context, now time.Time) error {
	expiredDeskIDs, err := s.deskStore.CollectExpiredDeskIDs(ctx, now, s.options.SessionTTL)
	if err != nil {
		return fmt.Errorf("collect expired desks on disk: %w", err)
	}

	var cleanupErr error
	for _, deskID := range expiredDeskIDs {
		deleteErr := s.deskStore.DeleteDesk(ctx, deskID)
		if deleteErr == nil || errors.Is(deleteErr, os.ErrNotExist) {
			continue
		}

		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete expired desk on disk %s: %w", deskID, deleteErr))
	}

	return cleanupErr
}

// removeDeskCascade removes desk-linked entries from queue, registry, and filesystem.
func (s *Service) removeDeskCascade(ctx context.Context, snapshot domain.DeskSnapshot, ignoreMissingDisk bool) error {
	var cleanupErr error

	if err := s.headerQueue.DeleteDesk(ctx, snapshot.DeskID, snapshot.TopicIDs); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete desk headers: %w", err))
	}

	if err := s.runRegistry.DeleteDesk(ctx, snapshot.DeskID); err != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete desk registry: %w", err))
	}

	if err := s.deskStore.DeleteDesk(ctx, snapshot.DeskID); err != nil {
		if !ignoreMissingDisk || !errors.Is(err, os.ErrNotExist) {
			cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete desk payload: %w", err))
		}
	}

	return cleanupErr
}

// validateTitle enforces required and max-length title constraints.
func (s *Service) validateTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("title is required")
	}

	if utf8Len(title) > s.options.MaxTitleLength {
		return fmt.Errorf("title length exceeds limit %d", s.options.MaxTitleLength)
	}

	return nil
}

// utf8Len counts rune length for title-size validation.
func utf8Len(value string) int {
	return len([]rune(value))
}

// normalizeMessageTitle lowercases title and strips all whitespace for duplicate detection.
func normalizeMessageTitle(title string) string {
	lowerTitle := strings.ToLower(title)

	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}

		return r
	}, lowerTitle)
}
