package appinit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/team-mcp/internal/config"
)

// serviceSuite validates startup wiring and logger-config fail-fast behavior.
type serviceSuite struct {
	suite.Suite
}

// TestServiceSuite runs app initialization service tests.
func TestServiceSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(serviceSuite))
}

// TestNewServiceBuildsForValidConfig verifies startup wiring succeeds for valid runtime configuration.
func (s *serviceSuite) TestNewServiceBuildsForValidConfig() {
	cfg := s.validConfig()

	service, err := New(cfg, "test-version")
	s.Require().NoError(err)
	s.NotNil(service)
}

// TestNewServiceLogsStartupConfig verifies startup emits effective runtime env-equivalent values.
func (s *serviceSuite) TestNewServiceLogsStartupConfig() {
	cfg := s.validConfig()

	originalStderr := os.Stderr
	stderrReader, stderrWriter, stderrErr := os.Pipe()
	s.Require().NoError(stderrErr)
	os.Stderr = stderrWriter

	s.T().Cleanup(func() {
		os.Stderr = originalStderr
		s.Require().NoError(stderrReader.Close())
	})

	service, buildErr := New(cfg, "test-version")
	s.Require().NoError(buildErr)
	s.NotNil(service)

	s.Require().NoError(stderrWriter.Close())
	stderrPayload, readErr := io.ReadAll(stderrReader)
	s.Require().NoError(readErr)

	logOutput := string(stderrPayload)
	s.Contains(logOutput, "\"msg\":\"startup configuration\"")
	s.Contains(logOutput, "\"TEAM_MCP_MESSAGE_DIR\":")
	s.Contains(logOutput, "\"TEAM_MCP_SESSION_TTL\":")
	s.Contains(logOutput, "\"TEAM_MCP_MAX_BUFFERED_MESSAGES\":")
	s.Contains(logOutput, "\"TEAM_MCP_MAX_ACTIVE_RUNS\":")
	s.Contains(logOutput, "\"TEAM_MCP_MAX_TITLE_LENGTH\":")
	s.Contains(logOutput, "\"TEAM_MCP_LIFECYCLE_COLLECT_INTERVAL\":")
}

// TestRunStopsCleanupWhenServerReturns verifies Run cancels cleanup routine when server exits with active parent context.
func (s *serviceSuite) TestRunStopsCleanupWhenServerReturns() {
	cleanupDone := make(chan struct{})

	service := &Service{
		runServer: func(context.Context) error {
			return nil
		},
		runCleanup: func(ctx context.Context) error {
			<-ctx.Done()
			close(cleanupDone)

			return ctx.Err()
		},
		runShutdown:        nil,
		cleanupStopTimeout: defaultCleanupStopTimeout,
		shutdownTimeout:    defaultShutdownTimeout,
	}

	err := service.Run(s.T().Context())
	s.Require().NoError(err)

	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		s.FailNow("cleanup goroutine did not stop in time")
	}
}

// TestRunWrapsServerError verifies Run propagates server run failures with context.
func (s *serviceSuite) TestRunWrapsServerError() {
	expectedErr := errors.New("server failed")

	service := &Service{
		runServer: func(context.Context) error {
			return expectedErr
		},
		runCleanup: func(ctx context.Context) error {
			<-ctx.Done()

			return ctx.Err()
		},
		runShutdown:        nil,
		cleanupStopTimeout: defaultCleanupStopTimeout,
		shutdownTimeout:    defaultShutdownTimeout,
	}

	err := service.Run(s.T().Context())
	s.Require().Error(err)
	s.Require().ErrorContains(err, "run mcp server")
	s.Require().ErrorIs(err, expectedErr)
}

// TestRunCallsShutdownCleanup verifies Run invokes shutdown cleanup after server stops.
func (s *serviceSuite) TestRunCallsShutdownCleanup() {
	cleanupDone := make(chan struct{})
	shutdownDone := make(chan struct{}, 1)

	service := &Service{
		runServer: func(context.Context) error {
			return nil
		},
		runCleanup: func(ctx context.Context) error {
			<-ctx.Done()
			close(cleanupDone)

			return ctx.Err()
		},
		runShutdown: func(context.Context) error {
			shutdownDone <- struct{}{}

			return nil
		},
		cleanupStopTimeout: defaultCleanupStopTimeout,
		shutdownTimeout:    defaultShutdownTimeout,
	}

	err := service.Run(s.T().Context())
	s.Require().NoError(err)

	select {
	case <-cleanupDone:
	case <-time.After(time.Second):
		s.FailNow("cleanup goroutine did not stop in time")
	}

	select {
	case <-shutdownDone:
	case <-time.After(time.Second):
		s.FailNow("shutdown cleanup was not called")
	}
}

// TestRunDoesNotBlockOnStuckCleanup verifies Run returns within bounded time even if cleanup goroutine ignores cancellation.
func (s *serviceSuite) TestRunDoesNotBlockOnStuckCleanup() {
	cleanupRelease := make(chan struct{})

	service := &Service{
		runServer: func(context.Context) error {
			return nil
		},
		runCleanup: func(context.Context) error {
			<-cleanupRelease

			return nil
		},
		runShutdown:        nil,
		cleanupStopTimeout: 20 * time.Millisecond,
		shutdownTimeout:    defaultShutdownTimeout,
	}

	done := make(chan error, 1)
	go func() {
		done <- service.Run(s.T().Context())
	}()

	select {
	case err := <-done:
		s.Require().NoError(err)
	case <-time.After(time.Second):
		s.FailNow("run did not return in bounded time")
	}

	close(cleanupRelease)
}

// TestRunDoesNotBlockOnStuckShutdown verifies Run returns when shutdown callback ignores cancellation.
func (s *serviceSuite) TestRunDoesNotBlockOnStuckShutdown() {
	shutdownRelease := make(chan struct{})

	service := &Service{
		runServer: func(context.Context) error {
			return nil
		},
		runCleanup: func(ctx context.Context) error {
			<-ctx.Done()

			return ctx.Err()
		},
		runShutdown: func(context.Context) error {
			<-shutdownRelease

			return nil
		},
		cleanupStopTimeout: defaultCleanupStopTimeout,
		shutdownTimeout:    20 * time.Millisecond,
	}

	done := make(chan error, 1)
	go func() {
		done <- service.Run(s.T().Context())
	}()

	select {
	case err := <-done:
		s.Require().Error(err)
		s.Require().ErrorContains(err, "shutdown cleanup")
	case <-time.After(time.Second):
		s.FailNow("run did not return in bounded time")
	}

	close(shutdownRelease)
}

// TestBuildLoggerWritesToStderrOnly verifies stdio MCP transport remains parseable because logs avoid stdout.
func (s *serviceSuite) TestBuildLoggerWritesToStderrOnly() {
	cfg := s.validConfig()

	originalStdout := os.Stdout
	originalStderr := os.Stderr

	stdoutReader, stdoutWriter, stdoutErr := os.Pipe()
	s.Require().NoError(stdoutErr)
	stderrReader, stderrWriter, stderrErr := os.Pipe()
	s.Require().NoError(stderrErr)

	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	s.T().Cleanup(func() {
		os.Stdout = originalStdout
		os.Stderr = originalStderr
		s.Require().NoError(stdoutReader.Close())
		s.Require().NoError(stderrReader.Close())
	})

	buildErr := buildLogger(cfg)
	s.Require().NoError(buildErr)

	slog.Info("stderr-only-log-check")

	s.Require().NoError(stdoutWriter.Close())
	s.Require().NoError(stderrWriter.Close())

	stdoutPayload, readStdoutErr := io.ReadAll(stdoutReader)
	s.Require().NoError(readStdoutErr)
	stderrPayload, readStderrErr := io.ReadAll(stderrReader)
	s.Require().NoError(readStderrErr)

	s.Empty(string(stdoutPayload))
	s.Contains(string(stderrPayload), "\"msg\":\"stderr-only-log-check\"")
}

// validConfig builds one complete valid startup config baseline.
func (s *serviceSuite) validConfig() *config.Config {
	return &config.Config{
		MessageDir:               filepath.Join(s.T().TempDir(), "messages"),
		SessionTTL:               10 * time.Minute,
		MaxBufferedMessages:      64,
		MaxActiveRuns:            16,
		MaxTitleLength:           200,
		ToolDeskCreateDesc:       "",
		ToolDeskRemoveDesc:       "",
		ToolTopicCreateDesc:      "",
		ToolTopicListDesc:        "",
		ToolMessageCreateDesc:    "",
		ToolMessageListDesc:      "",
		ToolMessageGetDesc:       "",
		SystemPrompt:             "",
		LogLevel:                 "info",
		LifecycleCollectInterval: time.Second,
	}
}
