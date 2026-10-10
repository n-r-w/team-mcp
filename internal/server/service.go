package server

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/n-r-w/team-mcp/internal/domain"
)

// Service registers MCP tools and maps DTOs to coordination use-cases.
type Service struct {
	mcpRuntime IMCPRuntime
	options    Options
}

// New constructs MCP inbound adapter service.
func New(options Options) *Service {
	applyDefaultMetadata(&options)

	//nolint:exhaustruct_v5 // external SDK
	mcpServer := mcp.NewServer(
		&mcp.Implementation{
			Name:    serverName,
			Version: options.Version,
			Title:   serverTitle,
		},
		&mcp.ServerOptions{
			Instructions: options.SystemPrompt,
		},
	)

	src := &Service{
		mcpRuntime: mcpServer,
		options:    options,
	}
	src.register(mcpServer)

	return src
}

// applyDefaultMetadata fills optional tool descriptions with built-in defaults.
func applyDefaultMetadata(options *Options) {
	if strings.TrimSpace(options.ToolDescriptions.DeskCreate) == "" {
		options.ToolDescriptions.DeskCreate = toolDeskCreateDesc
	}

	if strings.TrimSpace(options.ToolDescriptions.TopicCreate) == "" {
		options.ToolDescriptions.TopicCreate = toolTopicCreateDesc
	}

	if strings.TrimSpace(options.ToolDescriptions.TopicList) == "" {
		options.ToolDescriptions.TopicList = toolTopicListDesc
	}

	if strings.TrimSpace(options.ToolDescriptions.MessageCreate) == "" {
		options.ToolDescriptions.MessageCreate = toolMessageCreateDesc
	}

	if strings.TrimSpace(options.ToolDescriptions.MessageList) == "" {
		options.ToolDescriptions.MessageList = toolMessageListDesc
	}

	if strings.TrimSpace(options.ToolDescriptions.MessageGet) == "" {
		options.ToolDescriptions.MessageGet = toolMessageGetDesc
	}

	if strings.TrimSpace(options.ToolDescriptions.SaveMessageToFile) == "" {
		options.ToolDescriptions.SaveMessageToFile = toolSaveMessageToFileDesc
	}
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

// register adds desk/topic/message tools to the MCP server.
func (s *Service) register(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct_v5 // external SDK
		Name:        toolDeskCreateName,
		Description: s.options.ToolDescriptions.DeskCreate,
		InputSchema: emptyObjectInputSchema(),
	}, s.deskCreateTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct_v5 // external SDK
		Name:        toolTopicCreateName,
		Description: s.options.ToolDescriptions.TopicCreate,
	}, s.topicCreateTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct_v5 // external SDK
		Name:        toolTopicListName,
		Description: s.options.ToolDescriptions.TopicList,
	}, s.topicListTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct_v5 // external SDK
		Name:        toolMessageCreateName,
		Description: s.options.ToolDescriptions.MessageCreate,
	}, s.messageCreateTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct_v5 // external SDK
		Name:        toolMessageListName,
		Description: s.options.ToolDescriptions.MessageList,
	}, s.messageListTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct_v5 // external SDK
		Name:        toolMessageGetName,
		Description: s.options.ToolDescriptions.MessageGet,
	}, s.messageGetTool)
	mcp.AddTool(server, &mcp.Tool{ //nolint:exhaustruct_v5 // external SDK
		Name:        toolSaveMessageToFileName,
		Description: s.options.ToolDescriptions.SaveMessageToFile,
	}, s.saveMessageToFileTool)
}

// deskCreateTool handles desk creation and ID publication.
func (s *Service) deskCreateTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	_ deskCreateInput,
) (*mcp.CallToolResult, deskCreateOutput, error) {
	result, err := s.options.CoordinationUseCase.DeskCreate(ctx)
	if err != nil {
		return nil, deskCreateOutput{}, fmt.Errorf("desk_create failed: %w", err)
	}

	return nil, deskCreateOutput{DeskID: result.DeskID}, nil
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

	if err := validateTitle(title, s.options.MaxTitleLength); err != nil {
		return nil, topicCreateOutput{}, err
	}

	result, err := s.options.CoordinationUseCase.TopicCreate(ctx, domain.TopicCreateRequest{DeskID: deskID, Title: title})
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

	result, err := s.options.CoordinationUseCase.TopicList(ctx, domain.TopicListRequest{DeskID: deskID})
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

	if err := validateTitle(title, s.options.MaxTitleLength); err != nil {
		return nil, messageCreateOutput{}, err
	}

	content, err := s.resolveMessageContent(ctx, input)
	if err != nil {
		return nil, messageCreateOutput{}, err
	}

	result, err := s.options.CoordinationUseCase.MessageCreate(ctx, domain.MessageCreateRequest{
		TopicID: topicID,
		Title:   title,
		Content: content,
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

// resolveMessageContent enforces one message source and preserves the selected text unchanged.
func (s *Service) resolveMessageContent(ctx context.Context, input messageCreateInput) (string, error) {
	if input.Content != nil && input.FilePath != nil {
		return "", errors.New("content and file_path are mutually exclusive")
	}

	if input.Content == nil && input.FilePath == nil {
		return "", errors.New("exactly one of content or file_path is required")
	}

	if input.Content != nil {
		if strings.TrimSpace(*input.Content) == "" {
			return "", errors.New("content is required")
		}

		return *input.Content, nil
	}

	content, err := s.options.MessageFileReader.ReadMessage(ctx, *input.FilePath)
	if err != nil {
		return "", fmt.Errorf("read message file: %w", err)
	}

	return content, nil
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

	result, err := s.options.CoordinationUseCase.MessageList(ctx, domain.MessageListRequest{TopicID: topicID})
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

	result, err := s.options.CoordinationUseCase.MessageGet(ctx, domain.MessageGetRequest{MessageID: messageID})
	if err != nil {
		return nil, messageGetOutput{}, fmt.Errorf("message_get failed: %w", err)
	}

	if result.Status == domain.BusinessStatusNotFound {
		return nil, messageGetOutput{
			Status:  string(result.Status),
			Title:   "",
			Content: "",
		}, nil
	}

	return nil, messageGetOutput{
		Status:  "",
		Title:   result.Title,
		Content: result.Content,
	}, nil
}

// saveMessageToFileTool validates the transport contract and delegates ordered export orchestration.
func (s *Service) saveMessageToFileTool(
	ctx context.Context,
	_ *mcp.CallToolRequest,
	input saveMessageToFileInput,
) (*mcp.CallToolResult, saveMessageToFileOutput, error) {
	messageID := strings.TrimSpace(input.MessageID)
	if err := validateRequiredID("message_id", messageID); err != nil {
		return nil, saveMessageToFileOutput{}, err
	}
	if strings.TrimSpace(input.FilePath) == "" {
		return nil, saveMessageToFileOutput{}, errors.New("file_path is required")
	}
	if !filepath.IsAbs(input.FilePath) {
		return nil, saveMessageToFileOutput{}, errors.New("file_path must be absolute")
	}
	extension := filepath.Ext(filepath.Clean(input.FilePath))
	if extension != ".txt" && extension != ".md" {
		return nil, saveMessageToFileOutput{}, errors.New("file_path extension must be .txt or .md")
	}

	mode, err := parseMessageFileMode(input.Mode)
	if err != nil {
		return nil, saveMessageToFileOutput{}, err
	}

	result, err := s.options.CoordinationUseCase.SaveMessageToFile(ctx, domain.SaveMessageToFileRequest{
		MessageID: messageID,
		FilePath:  input.FilePath,
		Mode:      mode,
	})
	if err != nil {
		return nil, saveMessageToFileOutput{}, err
	}
	if result.Status == domain.BusinessStatusNotFound {
		return nil, saveMessageToFileOutput{Status: string(result.Status)}, nil
	}

	return nil, saveMessageToFileOutput{Status: ""}, nil
}

// parseMessageFileMode validates and converts the closed transport mode enum.
func parseMessageFileMode(mode string) (domain.MessageFileMode, error) {
	if mode == "" {
		return "", errors.New("mode is required")
	}

	switch domain.MessageFileMode(mode) {
	case domain.MessageFileModeCreate:
		return domain.MessageFileModeCreate, nil
	case domain.MessageFileModeOverwrite:
		return domain.MessageFileModeOverwrite, nil
	default:
		return "", errors.New("mode must be create or overwrite")
	}
}

// validateRequiredID validates required identifier input.
func validateRequiredID(field, value string) error {
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
