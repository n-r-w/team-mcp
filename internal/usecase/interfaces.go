package usecase

import (
	"context"
	"time"

	"github.com/n-r-w/team-mcp/internal/domain"
)

//go:generate mockgen -destination=interfaces_mock.go -package=usecase -source=interfaces.go

// IRunRegistry defines in-memory desk/topic/message registry operations.
type IRunRegistry interface {
	// CreateDesk creates desk metadata record and returns desk identifier.
	CreateDesk(ctx context.Context, createdAt time.Time) (string, error)
	// DeskExists reports whether desk exists in in-memory registry.
	DeskExists(ctx context.Context, deskID string) (bool, error)
	// TopicExists reports whether topic exists in in-memory registry.
	TopicExists(ctx context.Context, topicID string) (bool, error)
	// CreateTopic creates topic under desk or returns existing topic in idempotent case.
	CreateTopic(ctx context.Context, deskID string, title string) (domain.TopicHeader, domain.BusinessStatus, bool, error)
	// CreateMessage creates message metadata or returns duplicate/not-found business status.
	CreateMessage(
		ctx context.Context,
		topicID string,
		title string,
		normalizedTitle string,
	) (domain.MessageMeta, domain.BusinessStatus, string, error)
	// DeleteMessage removes message metadata from in-memory indexes.
	DeleteMessage(ctx context.Context, messageID string) error
	// GetMessageMeta resolves message metadata from in-memory indexes.
	GetMessageMeta(ctx context.Context, messageID string) (domain.MessageMeta, bool, error)
	// GetDeskSnapshot resolves desk-linked topic/message IDs for cascade cleanup.
	GetDeskSnapshot(ctx context.Context, deskID string) (domain.DeskSnapshot, bool, error)
	// DeleteDesk removes desk metadata and all linked in-memory indexes.
	DeleteDesk(ctx context.Context, deskID string) error
	// ListDeskIDs returns all active desk IDs.
	ListDeskIDs(ctx context.Context) ([]string, error)
	// CollectExpiredDeskIDs returns desks whose created_at + ttl is expired by provided timestamp.
	CollectExpiredDeskIDs(ctx context.Context, now time.Time, ttl time.Duration) ([]string, error)
}

// IDeskStore defines desk-scoped durable persistence operations.
type IDeskStore interface {
	// EnsureDesk creates desk storage directory and metadata marker.
	EnsureDesk(ctx context.Context, deskID string, createdAt time.Time) error
	// PersistMessage stores message body by desk and message identifiers.
	PersistMessage(ctx context.Context, deskID string, messageID string, payload string) error
	// ResolveMessage retrieves message body by desk and message identifiers.
	ResolveMessage(ctx context.Context, deskID string, messageID string) (string, error)
	// DeleteMessage deletes message body by desk and message identifiers.
	DeleteMessage(ctx context.Context, deskID string, messageID string) error
	// DeleteDesk deletes all desk-linked files.
	DeleteDesk(ctx context.Context, deskID string) error
	// CollectExpiredDeskIDs scans persisted metadata for expired desks.
	CollectExpiredDeskIDs(ctx context.Context, now time.Time, ttl time.Duration) ([]string, error)
}

// IHeaderQueue defines deterministic ordered lists of topic/message headers.
type IHeaderQueue interface {
	// EnsureTopic registers a topic header in desk-ordered list.
	EnsureTopic(ctx context.Context, deskID string, header domain.TopicHeader) error
	// ListTopics returns topic headers in first successful topic registration order from read-model queue.
	ListTopics(ctx context.Context, deskID string) ([]domain.TopicHeader, bool, error)
	// AppendMessage appends message header to topic list.
	AppendMessage(ctx context.Context, topicID string, header domain.MessageHeader) error
	// RemoveMessage removes message header from topic list.
	RemoveMessage(ctx context.Context, topicID string, messageID string) error
	// ListMessages returns ordered message headers for topic.
	ListMessages(ctx context.Context, topicID string) ([]domain.MessageHeader, bool, error)
	// DeleteDesk removes all desk-linked topic/message headers.
	DeleteDesk(ctx context.Context, deskID string, topicIDs []string) error
}
