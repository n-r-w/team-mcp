package usecase

import (
	"context"
	"time"

	"github.com/n-r-w/team-mcp/internal/domain"
)

//go:generate mockgen -destination=interfaces_mock.go -package=usecase -source=interfaces.go

// IBoardStore defines the authoritative desk/topic/message persistence boundary for runtime operations.
type IBoardStore interface {
	// CreateDesk creates desk metadata record and returns desk identifier.
	CreateDesk(ctx context.Context, createdAt time.Time) (string, error)
	// CreateTopic creates topic under desk or returns existing topic in idempotent case.
	CreateTopic(ctx context.Context, deskID string, title string) (domain.TopicHeader, domain.BusinessStatus, bool, error)
	// ListTopics returns ordered topic headers for desk or reports that the desk is missing.
	ListTopics(ctx context.Context, deskID string) ([]domain.TopicHeader, bool, error)
	// CreateMessage creates message metadata and payload or returns duplicate/not-found business status.
	CreateMessage(
		ctx context.Context,
		topicID string,
		title string,
		normalizedTitle string,
		payload string,
	) (domain.MessageMeta, domain.BusinessStatus, string, error)
	// ListMessages returns ordered message headers for topic or reports that the topic is missing.
	ListMessages(ctx context.Context, topicID string) ([]domain.MessageHeader, bool, error)
	// GetMessage resolves persisted message metadata and payload by message identifier.
	GetMessage(ctx context.Context, messageID string) (domain.MessageMeta, string, bool, error)
	// DeleteDesk removes all desk-linked state.
	DeleteDesk(ctx context.Context, deskID string) error
	// CollectExpiredDeskIDs returns desks whose created_at + ttl is expired by provided timestamp.
	CollectExpiredDeskIDs(ctx context.Context, now time.Time, ttl time.Duration) ([]string, error)
}
