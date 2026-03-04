package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/n-r-w/team-mcp/internal/domain"
)

// Service registers MCP tools and maps DTOs to coordination use-cases.
type Service struct {
	mcpRuntime          IMCPRuntime
	coordinationUseCase ICoordination
	maxTitleLength      int
}

// New constructs MCP inbound adapter service.
func New(version string, maxTitleLength int, coordinationUseCase ICoordination) *Service {
	mcpServer := mcp.NewServer(
		&mcp.Implementation{ //nolint:exhaustruct // external SDK
			Name:    serverName,
			Version: version,
			Title:   serverTitle,
		},
		&mcp.ServerOptions{ //nolint:exhaustruct // external SDK
			Instructions: systemPrompt,
		},
	)

	src := &Service{
		mcpRuntime:          mcpServer,
		coordinationUseCase: coordinationUseCase,
		maxTitleLength:      maxTitleLength,
	}
	src.register(mcpServer)

	return src
}

// Run starts the MCP server with stdio transport.
func (s *Service) Run(ctx context.Context) error {
	if err := s.mcpRuntime.Run(ctx, &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("server run failed: %w", err)
	}

	return nil
}

// emptyObjectInputSchema returns a reusable empty object schema for tools without input parameters.
func emptyObjectInputSchema() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

// Register adds desk/topic/message tools to the MCP server.
func (s *Service) register(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct // external SDK
		Name:        toolDeskCreateName,
		Description: toolDeskCreateDesc,
		InputSchema: emptyObjectInputSchema(),
	}, s.deskCreateTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct // external SDK
		Name:        toolDeskRemoveName,
		Description: toolDeskRemoveDesc,
	}, s.deskRemoveTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct // external SDK
		Name:        toolTopicCreateName,
		Description: toolTopicCreateDesc,
	}, s.topicCreateTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct // external SDK
		Name:        toolTopicListName,
		Description: toolTopicListDesc,
	}, s.topicListTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct // external SDK
		Name:        toolMessageCreateName,
		Description: toolMessageCreateDesc,
	}, s.messageCreateTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct // external SDK
		Name:        toolMessageListName,
		Description: toolMessageListDesc,
	}, s.messageListTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct // external SDK
		Name:        toolMessageGetName,
		Description: toolMessageGetDesc,
	}, s.messageGetTool)
}

// deskCreateTool handles desk creation and ID publication.
func (s *Service) deskCreateTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ deskCreateInput,
) (*mcp.CallToolResult, deskCreateOutput, error) {
	result, err := s.coordinationUseCase.DeskCreate(ctx)
	if err != nil {
		return nil, deskCreateOutput{}, fmt.Errorf("desk_create failed: %w", err)
	}

	return nil, deskCreateOutput{DeskID: result.DeskID}, nil
}

// deskRemoveTool handles desk removal with strict synchronous cleanup semantics.
func (s *Service) deskRemoveTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input deskRemoveInput,
) (*mcp.CallToolResult, deskRemoveOutput, error) {
	deskID := strings.TrimSpace(input.DeskID)
	if err := validateRequiredID("desk_id", deskID); err != nil {
		return nil, deskRemoveOutput{}, err
	}

	result, err := s.coordinationUseCase.DeskRemove(ctx, domain.DeskRemoveRequest{DeskID: deskID})
	if err != nil {
		return nil, deskRemoveOutput{}, fmt.Errorf("desk_remove failed: %w", err)
	}

	return nil, deskRemoveOutput{Status: string(result.Status)}, nil
}

// topicCreateTool handles topic creation and idempotent resolution.
func (s *Service) topicCreateTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input topicCreateInput,
) (*mcp.CallToolResult, topicCreateOutput, error) {
	deskID := strings.TrimSpace(input.DeskID)
	title := strings.TrimSpace(input.Title)
	if err := validateRequiredID("desk_id", deskID); err != nil {
		return nil, topicCreateOutput{}, err
	}

	if err := validateTitle(title, s.maxTitleLength); err != nil {
		return nil, topicCreateOutput{}, err
	}

	result, err := s.coordinationUseCase.TopicCreate(ctx, domain.TopicCreateRequest{DeskID: deskID, Title: title})
	if err != nil {
		return nil, topicCreateOutput{}, fmt.Errorf("topic_create failed: %w", err)
	}

	if result.Status == domain.BusinessStatusNotFound {
		return nil, topicCreateOutput{Status: string(result.Status), TopicID: ""}, nil
	}

	return nil, topicCreateOutput{Status: "", TopicID: result.TopicID}, nil
}

// topicListTool handles topic header listing for desk.
func (s *Service) topicListTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input topicListInput,
) (*mcp.CallToolResult, topicListOutput, error) {
	deskID := strings.TrimSpace(input.DeskID)
	if err := validateRequiredID("desk_id", deskID); err != nil {
		return nil, topicListOutput{}, err
	}

	result, err := s.coordinationUseCase.TopicList(ctx, domain.TopicListRequest{DeskID: deskID})
	if err != nil {
		return nil, topicListOutput{}, fmt.Errorf("topic_list failed: %w", err)
	}

	if result.Status == domain.BusinessStatusNotFound {
		return nil, topicListOutput{Status: string(result.Status), Topics: nil}, nil
	}

	return nil, topicListOutput{Status: "", Topics: toTopicHeaderDTOs(result.Topics)}, nil
}

// messageCreateTool handles message creation and duplicate-title business status mapping.
func (s *Service) messageCreateTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input messageCreateInput,
) (*mcp.CallToolResult, messageCreateOutput, error) {
	topicID := strings.TrimSpace(input.TopicID)
	title := strings.TrimSpace(input.Title)
	if err := validateRequiredID("topic_id", topicID); err != nil {
		return nil, messageCreateOutput{}, err
	}

	if err := validateTitle(title, s.maxTitleLength); err != nil {
		return nil, messageCreateOutput{}, err
	}

	if strings.TrimSpace(input.Content) == "" {
		return nil, messageCreateOutput{}, errors.New("content is required")
	}

	result, err := s.coordinationUseCase.MessageCreate(ctx, domain.MessageCreateRequest{
		TopicID: topicID,
		Title:   title,
		Content: input.Content,
	})
	if err != nil {
		return nil, messageCreateOutput{}, fmt.Errorf("message_create failed: %w", err)
	}

	if result.Status == domain.BusinessStatusNotFound {
		return nil, messageCreateOutput{
			Status:            string(result.Status),
			MessageID:         "",
			ExistingMessageID: "",
			StatusMessage:     "",
		}, nil
	}

	if result.Status == domain.BusinessStatusDuplicateTitle {
		return nil, messageCreateOutput{
			Status:            string(result.Status),
			MessageID:         "",
			ExistingMessageID: result.ExistingMessageID,
			StatusMessage:     result.StatusMessage,
		}, nil
	}

	return nil, messageCreateOutput{
		Status:            "",
		MessageID:         result.MessageID,
		ExistingMessageID: "",
		StatusMessage:     "",
	}, nil
}

// messageListTool handles message header listing for topic.
func (s *Service) messageListTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input messageListInput,
) (*mcp.CallToolResult, messageListOutput, error) {
	topicID := strings.TrimSpace(input.TopicID)
	if err := validateRequiredID("topic_id", topicID); err != nil {
		return nil, messageListOutput{}, err
	}

	result, err := s.coordinationUseCase.MessageList(ctx, domain.MessageListRequest{TopicID: topicID})
	if err != nil {
		return nil, messageListOutput{}, fmt.Errorf("message_list failed: %w", err)
	}

	if result.Status == domain.BusinessStatusNotFound {
		return nil, messageListOutput{Status: string(result.Status), Messages: nil}, nil
	}

	output := messageListOutput{Status: "", Messages: toMessageHeaderDTOs(result.Messages)}

	return nil, output, nil
}

// messageGetTool resolves full message payload by message ID.
func (s *Service) messageGetTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input messageGetInput,
) (*mcp.CallToolResult, messageGetOutput, error) {
	messageID := strings.TrimSpace(input.MessageID)
	if err := validateRequiredID("message_id", messageID); err != nil {
		return nil, messageGetOutput{}, err
	}

	result, err := s.coordinationUseCase.MessageGet(ctx, domain.MessageGetRequest{MessageID: messageID})
	if err != nil {
		return nil, messageGetOutput{}, fmt.Errorf("message_get failed: %w", err)
	}

	if result.Status == domain.BusinessStatusNotFound {
		return nil, messageGetOutput{Status: string(result.Status), Title: "", Content: ""}, nil
	}

	return nil, messageGetOutput{Status: "", Title: result.Title, Content: result.Content}, nil
}

// validateRequiredID validates required identifier input.
func validateRequiredID(field string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}

	return nil
}

// validateTitle validates required title and configured max length.
func validateTitle(title string, maxTitleLength int) error {
	if strings.TrimSpace(title) == "" {
		return errors.New("title is required")
	}

	if len([]rune(title)) > maxTitleLength {
		return fmt.Errorf("title length exceeds limit %d", maxTitleLength)
	}

	return nil
}

// toTopicHeaderDTOs maps domain topic headers to MCP DTO list items.
func toTopicHeaderDTOs(topics []domain.TopicHeader) []topicHeaderDTO {
	dtos := make([]topicHeaderDTO, 0, len(topics))
	for _, topic := range topics {
		dtos = append(dtos, topicHeaderDTO{TopicID: topic.TopicID, Title: topic.Title})
	}

	return dtos
}

// toMessageHeaderDTOs maps domain message headers to MCP DTO list items.
func toMessageHeaderDTOs(messages []domain.MessageHeader) []messageHeaderDTO {
	dtos := make([]messageHeaderDTO, 0, len(messages))
	for _, message := range messages {
		dtos = append(dtos, messageHeaderDTO{MessageID: message.MessageID, Title: message.Title})
	}

	return dtos
}
