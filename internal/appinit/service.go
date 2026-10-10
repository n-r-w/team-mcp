// Package appinit composes application dependencies and process lifecycle.
package appinit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/n-r-w/team-mcp/internal/adapters/filesystem"
	"github.com/n-r-w/team-mcp/internal/adapters/messagefile"
	"github.com/n-r-w/team-mcp/internal/config"
	"github.com/n-r-w/team-mcp/internal/server"
	"github.com/n-r-w/team-mcp/internal/usecase"
)

const (
	// defaultCleanupStopTimeout caps wait time for lifecycle collector stop during shutdown.
	defaultCleanupStopTimeout = 2 * time.Second
	// unixTempDir is Unix temporary directory that agents use in message file paths.
	unixTempDir = "/tmp"
)

// lifecycleRunner executes periodic cleanup loops.
type lifecycleRunner func(ctx context.Context) error

// Service composes application dependencies and runs MCP server lifecycle.
type Service struct {
	runServer          func(ctx context.Context) error
	runCleanup         lifecycleRunner
	cleanupStopTimeout time.Duration
}

// New wires all components required to run the MCP server.
func New(cfg *config.Config, version string) (*Service, error) {
	if err := buildLogger(cfg); err != nil {
		return nil, fmt.Errorf("build logger: %w", err)
	}

	if err := filesystem.ValidateRuntimeMessageDir(cfg.MessageDir); err != nil {
		return nil, fmt.Errorf("validate message directory handoff: %w", err)
	}

	logStartupConfig(cfg)

	boardStore, boardStoreErr := filesystem.NewBoardStore(cfg.MessageDir)
	if boardStoreErr != nil {
		return nil, fmt.Errorf("build authoritative board store: %w", boardStoreErr)
	}

	homeDir, homeDirErr := os.UserHomeDir()
	if homeDirErr != nil {
		return nil, fmt.Errorf("resolve user home directory: %w", homeDirErr)
	}

	messageFileService := messagefile.New(messageFileDirs(homeDir))
	usecaseService := usecase.New(
		boardStore,
		messageFileService,
		usecase.Options{SessionTTL: cfg.SessionTTL, MaxTitleLength: cfg.MaxTitleLength},
	)

	mcpSrv := server.New(server.Options{
		Version:             version,
		MaxTitleLength:      cfg.MaxTitleLength,
		CoordinationUseCase: usecaseService,
		MessageFileReader:   messageFileService,
		ToolDescriptions: server.ToolDescriptions{
			DeskCreate:        cfg.ToolDeskCreateDesc,
			TopicCreate:       cfg.ToolTopicCreateDesc,
			TopicList:         cfg.ToolTopicListDesc,
			MessageCreate:     cfg.ToolMessageCreateDesc,
			MessageList:       cfg.ToolMessageListDesc,
			MessageGet:        cfg.ToolMessageGetDesc,
			SaveMessageToFile: cfg.ToolSaveMessageToFileDesc,
		},
		SystemPrompt: cfg.SystemPrompt,
	})

	return &Service{
		runServer: mcpSrv.Run,
		runCleanup: func(ctx context.Context) error {
			return usecaseService.RunLifecycleCollector(ctx, cfg.LifecycleCollectInterval)
		},
		cleanupStopTimeout: defaultCleanupStopTimeout,
	}, nil
}

// messageFileDirs lists directories where agents read and write message files.
func messageFileDirs(homeDir string) []string {
	tempDir := os.TempDir()
	dirs := []string{homeDir, tempDir}
	if runtime.GOOS != "windows" && tempDir != unixTempDir {
		dirs = append(dirs, unixTempDir)
	}

	return dirs
}

// logStartupConfig writes effective startup configuration values required for operational visibility.
func logStartupConfig(cfg *config.Config) {
	slog.Info(
		"startup configuration",
		"TEAM_MCP_MESSAGE_DIR",
		cfg.MessageDir,
		"TEAM_MCP_SESSION_TTL",
		cfg.SessionTTL.String(),
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

	if runErr != nil {
		return fmt.Errorf("run mcp server: %w", runErr)
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

// buildLogger initializes slog logger from configuration.
func buildLogger(cfg *config.Config) error {
	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return err
	}

	handlerOptions := &slog.HandlerOptions{ //nolint:exhaustruct_v5 // external SDK
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
