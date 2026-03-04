package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caarlos0/env/v11"
)

// Config contains validated application settings loaded from environment.
type Config struct {
	MessageDir               string
	SessionTTL               time.Duration
	MaxBufferedMessages      int
	MaxActiveRuns            int
	MaxTitleLength           int
	LogLevel                 string
	LifecycleCollectInterval time.Duration
}

// envConfig is raw env schema for caarlos0/env parsing.
type envConfig struct {
	MessageDir               string        `env:"TEAM_MCP_MESSAGE_DIR"`
	SessionTTL               time.Duration `env:"TEAM_MCP_SESSION_TTL" envDefault:"24h"`
	MaxBufferedMessages      int           `env:"TEAM_MCP_MAX_BUFFERED_MESSAGES" envDefault:"10000"`
	MaxActiveRuns            int           `env:"TEAM_MCP_MAX_ACTIVE_RUNS" envDefault:"1000"`
	MaxTitleLength           int           `env:"TEAM_MCP_MAX_TITLE_LENGTH" envDefault:"200"`
	LogLevel                 string        `env:"TEAM_MCP_LOG_LEVEL" envDefault:"info"`
	LifecycleCollectInterval time.Duration `env:"TEAM_MCP_LIFECYCLE_COLLECT_INTERVAL" envDefault:"60s"`
}

// Load parses environment variables once and validates lifecycle/config invariants.
func Load() (*Config, error) {
	var parsed envConfig
	if err := env.Parse(&parsed); err != nil {
		return nil, fmt.Errorf("parse env config: %w", err)
	}

	messageDir := parsed.MessageDir
	if messageDir == "" {
		messageDir = filepath.Join(os.TempDir(), messageDirectoryName)
	}

	cfg := &Config{
		MessageDir:               messageDir,
		SessionTTL:               parsed.SessionTTL,
		MaxBufferedMessages:      parsed.MaxBufferedMessages,
		MaxActiveRuns:            parsed.MaxActiveRuns,
		MaxTitleLength:           parsed.MaxTitleLength,
		LogLevel:                 parsed.LogLevel,
		LifecycleCollectInterval: parsed.LifecycleCollectInterval,
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// validate enforces configuration ranges and lifecycle invariants.
func validate(cfg *Config) error {
	if cfg.SessionTTL < minimumSessionTTL {
		return newValidationError("TEAM_MCP_SESSION_TTL", fmt.Sprintf("must be >= %s", minimumSessionTTL))
	}

	if cfg.MaxBufferedMessages < minimumPositiveInteger {
		return newValidationError("TEAM_MCP_MAX_BUFFERED_MESSAGES", "must be >= 1")
	}

	if cfg.MaxActiveRuns < minimumPositiveInteger {
		return newValidationError("TEAM_MCP_MAX_ACTIVE_RUNS", "must be >= 1")
	}

	if cfg.MaxTitleLength < minimumPositiveInteger {
		return newValidationError("TEAM_MCP_MAX_TITLE_LENGTH", "must be >= 1")
	}

	if cfg.LifecycleCollectInterval <= 0 {
		return newValidationError("TEAM_MCP_LIFECYCLE_COLLECT_INTERVAL", "must be greater than 0")
	}

	if !isSupportedLogLevel(cfg.LogLevel) {
		return newValidationError("TEAM_MCP_LOG_LEVEL", "must be debug, info, warn, or error")
	}

	return nil
}

// isSupportedLogLevel verifies accepted slog level value.
func isSupportedLogLevel(level string) bool {
	switch level {
	case logLevelDebug, logLevelInfo, logLevelWarn, logLevelError:
		return true
	default:
		return false
	}
}
