package server

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/n-r-w/team-mcp/internal/domain"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

const testServerVersion = "test-version"

// TestRunSuccess verifies Run returns nil when MCP runtime completes successfully.
func TestRunSuccess(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	runtime := NewMockIMCPRuntime(controller)

	service := New(testServerVersion, 200, coordination)
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

	service := New(testServerVersion, 200, coordination)
	service.mcpRuntime = runtime

	ctx := t.Context()
	sentinelErr := errors.New("run failed")
	runtime.EXPECT().Run(ctx, gomock.AssignableToTypeOf(&mcp.StdioTransport{})).Return(sentinelErr)

	err := service.Run(ctx)
	require.Error(t, err)
	require.ErrorContains(t, err, "server run failed:")
	require.ErrorIs(t, err, sentinelErr)
}

// TestNewRegistersSevenTools verifies constructor registers only target 7 tools.
func TestNewRegistersSevenTools(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(testServerVersion, 200, coordination)

	runtimeServer, ok := service.mcpRuntime.(*mcp.Server)
	require.True(t, ok)

	registeredTools, err := listRegisteredToolsModern(t, t.Context(), runtimeServer)
	require.NoError(t, err)
	require.Len(t, registeredTools, 7)
	require.Contains(t, registeredTools, toolDeskCreateName)
	require.Contains(t, registeredTools, toolDeskRemoveName)
	require.Contains(t, registeredTools, toolTopicCreateName)
	require.Contains(t, registeredTools, toolTopicListName)
	require.Contains(t, registeredTools, toolMessageCreateName)
	require.Contains(t, registeredTools, toolMessageListName)
	require.Contains(t, registeredTools, toolMessageGetName)
}

// TestTopicCreateNotFound verifies topic_create maps business not_found status into output status.
func TestTopicCreateNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(testServerVersion, 200, coordination)

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

// TestMessageCreateValidation verifies message_create requires non-empty content.
func TestMessageCreateValidation(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	service := New(testServerVersion, 200, NewMockICoordination(controller))

	_, _, err := service.messageCreateTool(
		t.Context(),
		nil,
		messageCreateInput{TopicID: "topic-1", Title: "Title", Content: "   "},
	)
	require.EqualError(t, err, "content is required")
}

// TestMessageGetNotFound verifies message_get maps business not_found status into output status.
func TestMessageGetNotFound(t *testing.T) {
	t.Parallel()

	controller := gomock.NewController(t)
	coordination := NewMockICoordination(controller)
	service := New(testServerVersion, 200, coordination)

	ctx := t.Context()
	coordination.EXPECT().MessageGet(ctx, domain.MessageGetRequest{MessageID: "msg-1"}).Return(
		domain.MessageGetResult{Status: domain.BusinessStatusNotFound, Title: "", Content: ""},
		nil,
	)

	_, output, err := service.messageGetTool(ctx, nil, messageGetInput{MessageID: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, string(domain.BusinessStatusNotFound), output.Status)
}

// listRegisteredToolsModern reads MCP server tool registry into name->description mapping.
func listRegisteredToolsModern(t testing.TB, ctx context.Context, server *mcp.Server) (map[string]string, error) {
	client := mcp.NewClient(
		&mcp.Implementation{ //nolint:exhaustruct // external SDK
			Name:    "test-client",
			Version: testServerVersion,
		},
		nil,
	)

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverTransport, nil); err != nil {
		return nil, err
	}

	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = session.Close() })

	registered := make(map[string]string)
	for tool, err := range session.Tools(ctx, nil) {
		if err != nil {
			return nil, err
		}

		registered[tool.Name] = tool.Description
	}

	return registered, nil
}
