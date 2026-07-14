package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/team-mcp/internal/domain"
)

const testServerVersion = "test-version"

// newTestOptions builds baseline constructor options used across server tests.
func newTestOptions(coordination ICoordination) Options {
	return Options{
		Version:             testServerVersion,
		MaxTitleLength:      200,
		CoordinationUseCase: coordination,
		ToolDescriptions: ToolDescriptions{
			DeskCreate:        "",
			TopicCreate:       "",
			TopicList:         "",
			MessageCreate:     "",
			MessageList:       "",
			MessageGet:        "",
			SaveMessageToFile: "",
		},
		SystemPrompt: "",
	}
}

// TestRunSuccess verifies Run returns nil when MCP runtime completes successfully.
func TestRunSuccess(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	runtime := NewMockIMCPRuntime(controller)

	service := New(newTestOptions(coordination))
	service.mcpRuntime = runtime

	ctx := t.Context()
	runtime.EXPECT().Run(ctx, gomock.AssignableToTypeOf(&mcp.StdioTransport{})).Return(nil)

	require.NoError(t, service.Run(ctx))
}

// TestRunErrorWrap verifies Run wraps runtime errors with operation context.
func TestRunErrorWrap(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	runtime := NewMockIMCPRuntime(controller)

	service := New(newTestOptions(coordination))
	service.mcpRuntime = runtime

	ctx := t.Context()
	sentinelErr := errors.New("run failed")
	runtime.EXPECT().Run(ctx, gomock.AssignableToTypeOf(&mcp.StdioTransport{})).Return(sentinelErr)

	err := service.Run(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "server run failed:")
	require.ErrorIs(t, err, sentinelErr)
}

// TestNewRegistersSevenTools verifies constructor registers only the supported seven tools.
func TestNewRegistersSevenTools(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))

	runtimeServer, ok := service.mcpRuntime.(*mcp.Server)
	require.True(t, ok)

	registeredTools, err := listRegisteredToolDescriptions(t, t.Context(), runtimeServer)
	require.NoError(t, err)
	require.Len(t, registeredTools, 7)
	require.Contains(t, registeredTools, toolDeskCreateName)
	require.Contains(t, registeredTools, toolTopicCreateName)
	require.Contains(t, registeredTools, toolTopicListName)
	require.Contains(t, registeredTools, toolMessageCreateName)
	require.Contains(t, registeredTools, toolMessageListName)
	require.Contains(t, registeredTools, toolMessageGetName)
	require.Contains(t, registeredTools, toolSaveMessageToFileName)
}

// TestSaveMessageToFileSchemaRequiresAllInputs verifies MCP schema marks every input field as required.
func TestSaveMessageToFileSchemaRequiresAllInputs(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))

	runtimeServer, ok := service.mcpRuntime.(*mcp.Server)
	require.True(t, ok)

	registeredTools, err := listRegisteredToolDefinitions(t, t.Context(), runtimeServer)
	require.NoError(t, err)

	tool, found := registeredTools[toolSaveMessageToFileName]
	require.True(t, found)

	schemaPayload, err := json.Marshal(tool.InputSchema)
	require.NoError(t, err)

	var schema struct {
		Required []string `json:"required"`
	}
	require.NoError(t, json.Unmarshal(schemaPayload, &schema))
	require.ElementsMatch(t, []string{"message_id", "file_path", "mode"}, schema.Required)
}

// TestTopicCreateNotFound verifies topic_create maps business not_found status into output status.
func TestTopicCreateNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))

	ctx := t.Context()
	coordination.EXPECT().TopicCreate(ctx, domain.TopicCreateRequest{DeskID: "desk-1", Title: "Topic"}).Return(
		domain.TopicCreateResult{Status: domain.BusinessStatusNotFound, TopicID: ""},
		nil,
	)

	_, output, err := service.topicCreateTool(ctx, nil, topicCreateInput{DeskID: "desk-1", Title: "Topic"})
	require.NoError(t, err)
	require.Equal(t, string(domain.BusinessStatusNotFound), output.Status)
	require.Empty(t, output.TopicID)
}

// TestMessageCreateContentSource verifies direct content reaches the coordination use case unchanged.
func TestMessageCreateContentSource(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))
	content := "  first line\nsecond line\n"

	coordination.EXPECT().MessageCreate(t.Context(), domain.MessageCreateRequest{
		TopicID: "topic-1",
		Title:   "Title",
		Content: content,
	}).Return(domain.MessageCreateResult{
		Status:            domain.BusinessStatusOK,
		MessageID:         "message-1",
		ExistingMessageID: "",
		StatusMessage:     "",
	}, nil)

	_, output, err := service.messageCreateTool(
		t.Context(),
		nil,
		messageCreateInput{TopicID: "topic-1", Title: "Title", Content: new(content), FilePath: nil},
	)
	require.NoError(t, err)
	require.Equal(t, "message-1", output.MessageID)
}

// TestMessageCreateFileSource verifies supported text files reach the coordination use case byte-for-byte.
func TestMessageCreateFileSource(t *testing.T) {
	t.Parallel()

	testCases := map[string]string{
		"txt":          "message.txt",
		"uppercase md": "message.MD",
	}

	for name, fileName := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			content := "  first line\n" + strings.Repeat("payload line\n", 5_000)
			filePath := filepath.Join(t.TempDir(), fileName)
			require.NoError(t, os.WriteFile(filePath, []byte(content), 0o600))

			controller := gomock.NewController(t)
			coordination := NewMockICoordination(controller)
			service := New(newTestOptions(coordination))
			coordination.EXPECT().MessageCreate(t.Context(), domain.MessageCreateRequest{
				TopicID: "topic-1",
				Title:   "Title",
				Content: content,
			}).Return(domain.MessageCreateResult{
				Status:            domain.BusinessStatusOK,
				MessageID:         "message-1",
				ExistingMessageID: "",
				StatusMessage:     "",
			}, nil)

			_, output, err := service.messageCreateTool(
				t.Context(),
				nil,
				messageCreateInput{TopicID: "topic-1", Title: "Title", Content: nil, FilePath: new(filePath)},
			)
			require.NoError(t, err)
			require.Equal(t, "message-1", output.MessageID)
		})
	}
}

// TestMessageCreateSourceValidation verifies exactly one non-blank message source is required.
func TestMessageCreateSourceValidation(t *testing.T) {
	t.Parallel()

	content := "content"
	blankContent := "   "
	filePath := "/tmp/message.md"
	testCases := map[string]struct {
		input       messageCreateInput
		expectedErr string
	}{
		"neither source": {
			input: messageCreateInput{
				TopicID:  "topic-1",
				Title:    "Title",
				Content:  nil,
				FilePath: nil,
			},
			expectedErr: "exactly one of content or file_path is required",
		},
		"both sources": {
			input: messageCreateInput{
				TopicID:  "topic-1",
				Title:    "Title",
				Content:  new(content),
				FilePath: new(filePath),
			},
			expectedErr: "content and file_path are mutually exclusive",
		},
		"blank content": {
			input: messageCreateInput{
				TopicID:  "topic-1",
				Title:    "Title",
				Content:  new(blankContent),
				FilePath: nil,
			},
			expectedErr: "content is required",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			controller := gomock.NewController(t)
			service := New(newTestOptions(NewMockICoordination(controller)))

			_, _, err := service.messageCreateTool(t.Context(), nil, testCase.input)
			require.EqualError(t, err, testCase.expectedErr)
		})
	}
}

// TestMessageCreateFileValidation verifies file paths and payloads satisfy the file-source contract.
func TestMessageCreateFileValidation(t *testing.T) {
	t.Parallel()

	testCases := map[string]struct {
		preparePath func(t *testing.T) string
		expectedErr string
	}{
		"relative path": {
			preparePath: func(_ *testing.T) string { return "message.md" },
			expectedErr: "file_path must be absolute",
		},
		"unsupported extension": {
			preparePath: func(t *testing.T) string { return filepath.Join(t.TempDir(), "message.json") },
			expectedErr: "file_path must have a .txt or .md extension",
		},
		"missing file": {
			preparePath: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing.md") },
			expectedErr: "read file_path",
		},
		"directory": {
			preparePath: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "directory.md")
				require.NoError(t, os.Mkdir(path, 0o700))

				return path
			},
			expectedErr: "file_path must reference a regular file",
		},
		"blank file": {
			preparePath: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "blank.txt")
				require.NoError(t, os.WriteFile(path, []byte(" \n\t"), 0o600))

				return path
			},
			expectedErr: "file content is required",
		},
		"invalid UTF-8": {
			preparePath: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "invalid.md")
				require.NoError(t, os.WriteFile(path, []byte{0xff, 'a'}, 0o600))

				return path
			},
			expectedErr: "file content must be valid UTF-8",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			filePath := testCase.preparePath(t)
			controller := gomock.NewController(t)
			service := New(newTestOptions(NewMockICoordination(controller)))

			_, _, err := service.messageCreateTool(
				t.Context(),
				nil,
				messageCreateInput{TopicID: "topic-1", Title: "Title", Content: nil, FilePath: new(filePath)},
			)
			require.ErrorContains(t, err, testCase.expectedErr)
		})
	}
}

// TestMessageGetNotFound verifies message_get maps business not_found status into output status.
func TestMessageGetNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))

	ctx := t.Context()
	coordination.EXPECT().MessageGet(ctx, domain.MessageGetRequest{MessageID: "msg-1"}).Return(
		domain.MessageGetResult{Status: domain.BusinessStatusNotFound, Title: "", Content: ""},
		nil,
	)

	_, output, err := service.messageGetTool(ctx, nil, messageGetInput{MessageID: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, string(domain.BusinessStatusNotFound), output.Status)
}

// TestSaveMessageToFileValidation verifies invalid transport inputs do not reach the use case.
func TestSaveMessageToFileValidation(t *testing.T) {
	t.Parallel()

	validPath := filepath.Join(t.TempDir(), "message.md")
	testCases := map[string]struct {
		input       saveMessageToFileInput
		expectedErr string
	}{
		"missing message identifier": {
			input:       saveMessageToFileInput{MessageID: " ", FilePath: validPath, Mode: string(domain.MessageFileModeCreate)},
			expectedErr: "message_id is required",
		},
		"missing file path": {
			input:       saveMessageToFileInput{MessageID: "msg-1", FilePath: " ", Mode: string(domain.MessageFileModeCreate)},
			expectedErr: "file_path is required",
		},
		"missing mode": {
			input:       saveMessageToFileInput{MessageID: "msg-1", FilePath: validPath, Mode: ""},
			expectedErr: "mode is required",
		},
		"unsupported mode": {
			input:       saveMessageToFileInput{MessageID: "msg-1", FilePath: validPath, Mode: "append"},
			expectedErr: "mode must be create or overwrite",
		},
		"relative file path": {
			input:       saveMessageToFileInput{MessageID: "msg-1", FilePath: "message.md", Mode: string(domain.MessageFileModeCreate)},
			expectedErr: "file_path must be absolute",
		},
		"unsupported extension": {
			input:       saveMessageToFileInput{MessageID: "msg-1", FilePath: filepath.Join(t.TempDir(), "message.MD"), Mode: string(domain.MessageFileModeCreate)},
			expectedErr: "file_path extension must be .txt or .md",
		},
	}

	for name, testCase := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			controller := gomock.NewController(t)
			service := New(newTestOptions(NewMockICoordination(controller)))

			_, _, err := service.saveMessageToFileTool(t.Context(), nil, testCase.input)
			require.ErrorContains(t, err, testCase.expectedErr)
		})
	}
}

// TestSaveMessageToFileNotFound verifies the handler maps missing messages without a destination path.
func TestSaveMessageToFileNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))
	filePath := filepath.Join(t.TempDir(), "message.txt")
	request := domain.SaveMessageToFileRequest{
		MessageID: "missing",
		FilePath:  filePath,
		Mode:      domain.MessageFileModeCreate,
	}

	coordination.EXPECT().SaveMessageToFile(t.Context(), request).Return(
		domain.SaveMessageToFileResult{Status: domain.BusinessStatusNotFound},
		nil,
	)

	_, output, err := service.saveMessageToFileTool(t.Context(), nil, saveMessageToFileInput{
		MessageID: request.MessageID,
		FilePath:  request.FilePath,
		Mode:      string(request.Mode),
	})
	require.NoError(t, err)
	require.Equal(t, saveMessageToFileOutput{Status: string(domain.BusinessStatusNotFound)}, output)
}

// TestSaveMessageToFileError verifies the handler returns use-case failures without redundant tool context.
func TestSaveMessageToFileError(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))
	filePath := filepath.Join(t.TempDir(), "message.txt")
	request := domain.SaveMessageToFileRequest{
		MessageID: "msg-1",
		FilePath:  filePath,
		Mode:      domain.MessageFileModeOverwrite,
	}
	writeErr := errors.New("write failed")

	coordination.EXPECT().SaveMessageToFile(t.Context(), request).Return(
		domain.SaveMessageToFileResult{Status: ""},
		writeErr,
	)

	_, _, err := service.saveMessageToFileTool(t.Context(), nil, saveMessageToFileInput{
		MessageID: request.MessageID,
		FilePath:  request.FilePath,
		Mode:      string(request.Mode),
	})
	require.ErrorIs(t, err, writeErr)
	require.EqualError(t, err, writeErr.Error())
}

// TestSaveMessageToFileMCPOutput verifies clients receive an empty object in structured and text success results.
func TestSaveMessageToFileMCPOutput(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))
	filePath := filepath.Join(t.TempDir(), "message.md")
	request := domain.SaveMessageToFileRequest{
		MessageID: "msg-1",
		FilePath:  filePath,
		Mode:      domain.MessageFileModeCreate,
	}
	coordination.EXPECT().SaveMessageToFile(gomock.Any(), request).Return(
		domain.SaveMessageToFileResult{Status: domain.BusinessStatusOK},
		nil,
	)

	runtimeServer, ok := service.mcpRuntime.(*mcp.Server)
	require.True(t, ok)
	clientSession, err := connectTestClient(t, t.Context(), runtimeServer)
	require.NoError(t, err)

	result, err := clientSession.CallTool(t.Context(), &mcp.CallToolParams{
		Meta: nil,
		Name: toolSaveMessageToFileName,
		Arguments: map[string]any{
			"message_id": request.MessageID,
			"file_path":  request.FilePath,
			"mode":       request.Mode,
		},
	})
	require.NoError(t, err)
	require.False(t, result.IsError)
	require.Equal(t, map[string]any{}, result.StructuredContent)
	require.Len(t, result.Content, 1)

	textContent, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	require.JSONEq(t, `{}`, textContent.Text)
}

// TestNewAppliesCustomDescriptions verifies constructor options override default MCP tool descriptions.
func TestNewAppliesCustomDescriptions(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(Options{
		Version:             testServerVersion,
		MaxTitleLength:      200,
		CoordinationUseCase: coordination,
		ToolDescriptions: ToolDescriptions{
			DeskCreate:        "custom desk_create",
			TopicCreate:       "custom topic_create",
			TopicList:         "custom topic_list",
			MessageCreate:     "custom message_create",
			MessageList:       "custom message_list",
			MessageGet:        "custom message_get",
			SaveMessageToFile: "custom save_message_to_file",
		},
		SystemPrompt: "custom system prompt",
	})

	runtimeServer, ok := service.mcpRuntime.(*mcp.Server)
	require.True(t, ok)

	registeredTools, err := listRegisteredToolDescriptions(t, t.Context(), runtimeServer)
	require.NoError(t, err)
	require.Len(t, registeredTools, 7)
	require.Equal(t, "custom desk_create", registeredTools[toolDeskCreateName])
	require.Equal(t, "custom topic_create", registeredTools[toolTopicCreateName])
	require.Equal(t, "custom topic_list", registeredTools[toolTopicListName])
	require.Equal(t, "custom message_create", registeredTools[toolMessageCreateName])
	require.Equal(t, "custom message_list", registeredTools[toolMessageListName])
	require.Equal(t, "custom message_get", registeredTools[toolMessageGetName])
	require.Equal(t, "custom save_message_to_file", registeredTools[toolSaveMessageToFileName])
	require.Equal(t, "custom system prompt", service.options.SystemPrompt)
}

// TestNewFallsBackToDefaultDescriptions verifies empty overrides preserve default descriptions and prompt.
func TestNewFallsBackToDefaultDescriptions(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(newTestOptions(coordination))

	runtimeServer, ok := service.mcpRuntime.(*mcp.Server)
	require.True(t, ok)

	registeredTools, err := listRegisteredToolDescriptions(t, t.Context(), runtimeServer)
	require.NoError(t, err)
	require.Len(t, registeredTools, 7)
	require.Equal(t, toolDeskCreateDesc, registeredTools[toolDeskCreateName])
	require.Equal(t, toolTopicCreateDesc, registeredTools[toolTopicCreateName])
	require.Equal(t, toolTopicListDesc, registeredTools[toolTopicListName])
	require.Equal(t, toolMessageCreateDesc, registeredTools[toolMessageCreateName])
	require.Equal(t, toolMessageListDesc, registeredTools[toolMessageListName])
	require.Equal(t, toolMessageGetDesc, registeredTools[toolMessageGetName])
	require.Equal(t, toolSaveMessageToFileDesc, registeredTools[toolSaveMessageToFileName])
	require.Equal(t, systemPrompt, service.options.SystemPrompt)
}

// listRegisteredToolDescriptions reads MCP server tool registry into name-to-description mapping.
func listRegisteredToolDescriptions(t testing.TB, ctx context.Context, server *mcp.Server) (map[string]string, error) {
	definitions, err := listRegisteredToolDefinitions(t, ctx, server)
	if err != nil {
		return nil, err
	}

	registered := make(map[string]string, len(definitions))
	for name, tool := range definitions {
		registered[name] = tool.Description
	}

	return registered, nil
}

// listRegisteredToolDefinitions reads complete MCP tool definitions from an in-memory client session.
func listRegisteredToolDefinitions(t testing.TB, ctx context.Context, server *mcp.Server) (map[string]*mcp.Tool, error) {
	session, err := connectTestClient(t, ctx, server)
	if err != nil {
		return nil, err
	}

	registered := make(map[string]*mcp.Tool)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}

		registered[tool.Name] = tool
	}

	return registered, nil
}

// connectTestClient connects an in-memory MCP client and registers both sessions for cleanup.
func connectTestClient(t testing.TB, ctx context.Context, server *mcp.Server) (*mcp.ClientSession, error) {
	client := mcp.NewClient(
		&mcp.Implementation{ //nolint:exhaustruct // external SDK
			Name:    "test-client",
			Version: testServerVersion,
		},
		nil,
	)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return clientSession, nil
}
