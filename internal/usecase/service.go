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
	boardStore IBoardStore
	options    Options
}

// New builds coordination use case service with explicit dependencies.
func New(boardStore IBoardStore, options Options) *Service {
	return &Service{
		boardStore: boardStore,
		options:    options,
	}
}

// DeskCreate creates desk and initializes persistent storage metadata.
func (s *Service) DeskCreate(ctx context.Context) (domain.DeskCreateResult, error) {
	createdAt := time.Now().UTC()
	deskID, err := s.boardStore.CreateDesk(ctx, createdAt)
	if err != nil {
		return domain.DeskCreateResult{}, fmt.Errorf("create desk: %w", err)
	}

	slog.InfoContext(ctx, "desk created", logFieldEvent, eventDeskCreate, logFieldDeskID, deskID)

	return domain.DeskCreateResult{DeskID: deskID}, nil
}

// TopicCreate creates topic in desk or returns existing topic identifier for idempotent calls.
func (s *Service) TopicCreate(
	ctx context.Context,
	request domain.TopicCreateRequest,
) (domain.TopicCreateResult, error) {
	if err := s.validateTitle(request.Title); err != nil {
		return domain.TopicCreateResult{}, err
	}

	header, status, _, err := s.boardStore.CreateTopic(ctx, request.DeskID, request.Title)
	if err != nil {
		return domain.TopicCreateResult{}, fmt.Errorf("create topic: %w", err)
	}

	if status == domain.BusinessStatusNotFound {
		return domain.TopicCreateResult{Status: domain.BusinessStatusNotFound, TopicID: ""}, nil
	}

	return domain.TopicCreateResult{Status: domain.BusinessStatusOK, TopicID: header.TopicID}, nil
}

// TopicList returns deterministic ordered topic headers for desk.
func (s *Service) TopicList(
	ctx context.Context,
	request domain.TopicListRequest,
) (domain.TopicListResult, error) {
	topics, found, err := s.boardStore.ListTopics(ctx, request.DeskID)
	if err != nil {
		return domain.TopicListResult{}, fmt.Errorf("list topics: %w", err)
	}

	if !found {
		return domain.TopicListResult{Status: domain.BusinessStatusNotFound, Topics: nil}, nil
	}

	return domain.TopicListResult{Status: domain.BusinessStatusOK, Topics: topics}, nil
}

// MessageCreate creates message metadata and payload while preserving duplicate-title semantics.
func (s *Service) MessageCreate(
	ctx context.Context,
	request domain.MessageCreateRequest,
) (domain.MessageCreateResult, error) {
	if err := s.validateTitle(request.Title); err != nil {
		return domain.MessageCreateResult{}, err
	}

	normalizedTitle := normalizeMessageTitle(request.Title)
	meta, status, existingMessageID, err := s.boardStore.CreateMessage(
		ctx,
		request.TopicID,
		request.Title,
		normalizedTitle,
		request.Content,
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

		return domain.MessageCreateResult{}, fmt.Errorf("create message: %w", err)
	}

	if result, done := buildMessageCreateBusinessResult(status, existingMessageID); done {
		s.logMessageCreateResult(ctx, request.TopicID, result)

		return result, nil
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

// MessageList returns deterministic ordered message headers for topic.
func (s *Service) MessageList(
	ctx context.Context,
	request domain.MessageListRequest,
) (domain.MessageListResult, error) {
	messages, found, err := s.boardStore.ListMessages(ctx, request.TopicID)
	if err != nil {
		return domain.MessageListResult{}, fmt.Errorf("list message headers: %w", err)
	}

	if !found {
		return domain.MessageListResult{Status: domain.BusinessStatusNotFound, Messages: nil}, nil
	}

	return domain.MessageListResult{Status: domain.BusinessStatusOK, Messages: messages}, nil
}

// MessageGet returns full message body for existing message identifier.
func (s *Service) MessageGet(ctx context.Context, request domain.MessageGetRequest) (domain.MessageGetResult, error) {
	meta, content, found, err := s.boardStore.GetMessage(ctx, request.MessageID)
	if err != nil {
		return domain.MessageGetResult{}, fmt.Errorf("resolve message: %w", err)
	}

	if !found {
		return domain.MessageGetResult{Status: domain.BusinessStatusNotFound, Title: "", Content: ""}, nil
	}

	return domain.MessageGetResult{
		Status:  domain.BusinessStatusOK,
		Title:   meta.Title,
		Content: content,
	}, nil
}

// RunLifecycleCollector executes startup and periodic cleanup for expired desks.
func (s *Service) RunLifecycleCollector(ctx context.Context, collectInterval time.Duration) error {
	if collectInterval <= 0 {
		return errors.New("collect interval must be greater than 0")
	}

	now := time.Now().UTC()
	if err := s.cleanupExpiredDesks(ctx, now); err != nil {
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
			runtimeCleanupErr := s.cleanupExpiredDesks(ctx, currentTime.UTC())
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

// cleanupExpiredDesks removes expired desk state through the single authoritative store.
func (s *Service) cleanupExpiredDesks(ctx context.Context, now time.Time) error {
	expiredDeskIDs, err := s.boardStore.CollectExpiredDeskIDs(ctx, now, s.options.SessionTTL)
	if err != nil {
		return fmt.Errorf("collect expired desks: %w", err)
	}

	var cleanupErr error
	for _, deskID := range expiredDeskIDs {
		deleteErr := s.boardStore.DeleteDesk(ctx, deskID)
		if deleteErr == nil || errors.Is(deleteErr, os.ErrNotExist) {
			continue
		}

		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("delete expired desk %s: %w", deskID, deleteErr))
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
