package appinit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/n-r-w/team-mcp/internal/adapters/filesystem"
	"github.com/n-r-w/team-mcp/internal/adapters/queue"
	"github.com/n-r-w/team-mcp/internal/adapters/runstate"
	"github.com/n-r-w/team-mcp/internal/config"
	"github.com/n-r-w/team-mcp/internal/server"
	"github.com/n-r-w/team-mcp/internal/usecase"
)

// Service composes application dependencies and runs MCP server lifecycle.
type Service struct {
	runServer          func(ctx context.Context) error
	runCleanup         lifecycleRunner
	runShutdown        lifecycleRunner
	cleanupStopTimeout time.Duration
	shutdownTimeout    time.Duration
}

// New wires all components required to run the MCP server.
func New(cfg *config.Config, version string) (*Service, error) {
	if err := buildLogger(cfg); err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	logStartupConfig(cfg)

	runStateAdapter := runstate.New(cfg.MaxActiveRuns)
	queueAdapter := queue.New(cfg.MaxBufferedMessages)
	messageStoreAdapter, messageStoreErr := filesystem.New(cfg.MessageDir)
	if messageStoreErr != nil {
		return nil, fmt.Errorf("build message store adapter: %w", messageStoreErr)
	}

	usecaseService := usecase.New(
		runStateAdapter,
		messageStoreAdapter,
		queueAdapter,
		usecase.Options{SessionTTL: cfg.SessionTTL, MaxTitleLength: cfg.MaxTitleLength},
	)

	mcpSrv := server.New(version, cfg.MaxTitleLength, usecaseService)

	return &Service{
		runServer: mcpSrv.Run,
		runCleanup: func(ctx context.Context) error {
			return usecaseService.RunLifecycleCollector(ctx, cfg.LifecycleCollectInterval)
		},
		runShutdown:        usecaseService.CleanupAllRuns,
		cleanupStopTimeout: defaultCleanupStopTimeout,
		shutdownTimeout:    defaultShutdownTimeout,
	}, nil
}

// logStartupConfig writes effective startup configuration values required for operational visibility.
func logStartupConfig(cfg *config.Config) {
	slog.Info(
		"startup configuration",
		"TEAM_MCP_MESSAGE_DIR",
		cfg.MessageDir,
		"TEAM_MCP_SESSION_TTL",
		cfg.SessionTTL.String(),
		"TEAM_MCP_MAX_BUFFERED_MESSAGES",
		cfg.MaxBufferedMessages,
		"TEAM_MCP_MAX_ACTIVE_RUNS",
		cfg.MaxActiveRuns,
		"TEAM_MCP_MAX_TITLE_LENGTH",
		cfg.MaxTitleLength,
		"TEAM_MCP_LIFECYCLE_COLLECT_INTERVAL",
		cfg.LifecycleCollectInterval.String(),
	)
}

// Run starts lifecycle collector and MCP stdio server.
func (s *Service) Run(ctx context.Context) error {
	cleanupCtx, cancelCleanup := context.WithCancel(ctx)
	defer cancelCleanup()

	done := make(chan struct{})
	go func() {
		defer close(done)

		if err := s.runCleanup(cleanupCtx); err != nil && !errors.Is(err, context.Canceled) {
			slog.ErrorContext(cleanupCtx, "lifecycle collector stopped", "error", err)
		}
	}()

	runErr := s.runServer(ctx)
	cancelCleanup()
	s.waitCleanupStop(cleanupCtx, done)

	shutdownErr := s.runShutdownWithTimeout(ctx)
	if shutdownErr != nil {
		slog.ErrorContext(ctx, "shutdown cleanup failed", "error", shutdownErr)
	}

	if runErr != nil {
		if shutdownErr != nil {
			return errors.Join(fmt.Errorf("run mcp server: %w", runErr), fmt.Errorf("shutdown cleanup: %w", shutdownErr))
		}

		return fmt.Errorf("run mcp server: %w", runErr)
	}

	if shutdownErr != nil {
		return fmt.Errorf("shutdown cleanup: %w", shutdownErr)
	}

	return nil
}

// waitCleanupStop bounds wait time for lifecycle collector shutdown to avoid indefinite stop hangs.
func (s *Service) waitCleanupStop(ctx context.Context, done <-chan struct{}) {
	if s.cleanupStopTimeout <= 0 {
		<-done

		return
	}

	cleanupWaitTimer := time.NewTimer(s.cleanupStopTimeout)
	defer cleanupWaitTimer.Stop()

	select {
	case <-done:
		return
	case <-cleanupWaitTimer.C:
		slog.WarnContext(
			context.WithoutCancel(ctx),
			"lifecycle collector did not stop before timeout",
			"timeout",
			s.cleanupStopTimeout.String(),
		)

		return
	}
}

// runShutdownWithTimeout bounds shutdown cleanup duration and returns timeout error on deadline exceed.
func (s *Service) runShutdownWithTimeout(ctx context.Context) error {
	if s.runShutdown == nil {
		return nil
	}

	shutdownCtx := context.WithoutCancel(ctx)
	if s.shutdownTimeout <= 0 {
		return s.runShutdown(shutdownCtx)
	}

	boundedShutdownCtx, cancelShutdown := context.WithTimeout(shutdownCtx, s.shutdownTimeout)
	defer cancelShutdown()

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- s.runShutdown(boundedShutdownCtx)
	}()

	select {
	case err := <-shutdownDone:
		return err
	case <-boundedShutdownCtx.Done():
		return boundedShutdownCtx.Err()
	}
}

// buildLogger initializes slog logger from configuration.
func buildLogger(cfg *config.Config) error {
	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return err
	}

	handlerOptions := &slog.HandlerOptions{ //nolint:exhaustruct // external SDK
		Level: level,
	}
	handler := slog.NewJSONHandler(os.Stderr, handlerOptions)

	logger := slog.New(handler)
	slog.SetDefault(logger)

	return nil
}

// parseLogLevel maps config string to slog level value.
func parseLogLevel(level string) (slog.Level, error) {
	switch level {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unsupported log level %q", level)
	}
}
