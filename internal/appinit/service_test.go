package appinit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/n-r-w/team-mcp/internal/adapters/filesystem"
	"github.com/n-r-w/team-mcp/internal/config"
)

var appinitLoggerGlobalsMu sync.Mutex

// serviceSuite validates startup wiring and logger-config fail-fast behavior.
type serviceSuite struct {
	suite.Suite
}

// TestServiceSuite runs app initialization service tests.
func TestServiceSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(serviceSuite))
}

// lockAppinitLoggerGlobals serializes tests that read or mutate process-global logger and stdio state.
func lockAppinitLoggerGlobals(t testing.TB) func() {
	t.Helper()

	appinitLoggerGlobalsMu.Lock()

	return func() {
		appinitLoggerGlobalsMu.Unlock()
	}
}

// TestNewServiceBuildsForValidConfig verifies startup wiring succeeds for valid runtime configuration.
func (s *serviceSuite) TestNewServiceBuildsForValidConfig() {
	s.T().Cleanup(lockAppinitLoggerGlobals(s.T()))

	cfg := s.validConfig()

	service, err := New(cfg, "test-version")
	s.Require().NoError(err)
	s.NotNil(service)
}

// TestNewServiceBuildsForExistingRuntimeStore verifies startup accepts a message directory populated by a previous Team MCP run.
func (s *serviceSuite) TestNewServiceBuildsForExistingRuntimeStore() {
	s.T().Cleanup(lockAppinitLoggerGlobals(s.T()))

	cfg := s.validConfig()
	store, err := filesystem.NewBoardStore(cfg.MessageDir)
	s.Require().NoError(err)

	_, err = store.CreateDesk(s.T().Context(), time.Now().UTC())
	s.Require().NoError(err)

	service, err := New(cfg, "test-version")
	s.Require().NoError(err)
	s.NotNil(service)
}

// TestNewServiceLogsStartupConfig verifies startup emits effective runtime env-equivalent values.
func (s *serviceSuite) TestNewServiceLogsStartupConfig() {
	s.T().Cleanup(lockAppinitLoggerGlobals(s.T()))

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
		cleanupStopTimeout: defaultCleanupStopTimeout,
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
		cleanupStopTimeout: defaultCleanupStopTimeout,
	}

	err := service.Run(s.T().Context())
	s.Require().Error(err)
	s.Require().ErrorContains(err, "run mcp server")
	s.Require().ErrorIs(err, expectedErr)
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
		cleanupStopTimeout: 20 * time.Millisecond,
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

// TestBuildLoggerWritesToStderrOnly verifies stdio MCP transport remains parseable because logs avoid stdout.
func (s *serviceSuite) TestBuildLoggerWritesToStderrOnly() {
	s.T().Cleanup(lockAppinitLoggerGlobals(s.T()))

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
		MaxTitleLength:           200,
		ToolDeskCreateDesc:       "",
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
