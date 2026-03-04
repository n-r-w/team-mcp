package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/n-r-w/team-mcp/internal/domain"
)

//go:generate mockgen -destination=interfaces_mock.go -package=server -source=interfaces.go

// ICoordination defines application consumed by MCP handlers.
type ICoordination interface {
	// DeskCreate creates desk and returns desk identifier.
	DeskCreate(ctx context.Context) (domain.DeskCreateResult, error)
	// DeskRemove synchronously removes desk-linked data from memory and disk.
	DeskRemove(ctx context.Context, request domain.DeskRemoveRequest) (domain.DeskRemoveResult, error)
	// TopicCreate creates topic in desk and is idempotent by (desk_id,title).
	TopicCreate(ctx context.Context, request domain.TopicCreateRequest) (domain.TopicCreateResult, error)
	// TopicList returns ordered topic headers for desk.
	TopicList(ctx context.Context, request domain.TopicListRequest) (domain.TopicListResult, error)
	// MessageCreate creates message under topic with duplicate-title business semantics.
	MessageCreate(ctx context.Context, request domain.MessageCreateRequest) (domain.MessageCreateResult, error)
	// MessageList returns ordered message headers for topic.
	MessageList(ctx context.Context, request domain.MessageListRequest) (domain.MessageListResult, error)
	// MessageGet returns full message payload by message identifier.
	MessageGet(ctx context.Context, request domain.MessageGetRequest) (domain.MessageGetResult, error)
}

// IMCPRuntime defines MCP runtime consumed by server adapter Run method.
type IMCPRuntime interface {
	// Run starts MCP server over provided transport and blocks until completion.
	Run(ctx context.Context, transport mcp.Transport) error
}
