package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// validateSuite validates startup configuration invariants.
type validateSuite struct {
	suite.Suite
}

// TestValidateSuite runs configuration validation scenarios.
func TestValidateSuite(t *testing.T) {
	t.Parallel()

	suite.Run(t, new(validateSuite))
}

// TestLoadReadsMCPMetadataOverrides verifies env parsing populates MCP tool description and system prompt overrides.
func TestLoadReadsMCPMetadataOverrides(t *testing.T) {
	t.Setenv("TEAM_MCP_TOOL_DESK_CREATE_DESC", "desk create override")
	t.Setenv("TEAM_MCP_TOOL_DESK_REMOVE_DESC", "desk remove override")
	t.Setenv("TEAM_MCP_TOOL_TOPIC_CREATE_DESC", "topic create override")
	t.Setenv("TEAM_MCP_TOOL_TOPIC_LIST_DESC", "topic list override")
	t.Setenv("TEAM_MCP_TOOL_MESSAGE_CREATE_DESC", "message create override")
	t.Setenv("TEAM_MCP_TOOL_MESSAGE_LIST_DESC", "message list override")
	t.Setenv("TEAM_MCP_TOOL_MESSAGE_GET_DESC", "message get override")
	t.Setenv("TEAM_MCP_SYSTEM_PROMPT", "system prompt override")

	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "desk create override", cfg.ToolDeskCreateDesc)
	require.Equal(t, "desk remove override", cfg.ToolDeskRemoveDesc)
	require.Equal(t, "topic create override", cfg.ToolTopicCreateDesc)
	require.Equal(t, "topic list override", cfg.ToolTopicListDesc)
	require.Equal(t, "message create override", cfg.ToolMessageCreateDesc)
	require.Equal(t, "message list override", cfg.ToolMessageListDesc)
	require.Equal(t, "message get override", cfg.ToolMessageGetDesc)
	require.Equal(t, "system prompt override", cfg.SystemPrompt)
}

// TestValidateAcceptsConsistentConfig verifies a fully valid config passes startup validation.
func (s *validateSuite) TestValidateAcceptsConsistentConfig() {
	err := validate(s.validConfig())
	s.Require().NoError(err)
}

// TestValidateRejectsSessionTTLBelowMinimum verifies session TTL lower bound.
func (s *validateSuite) TestValidateRejectsSessionTTLBelowMinimum() {
	cfg := s.validConfig()
	cfg.SessionTTL = time.Second

	err := validate(cfg)
	s.Require().Error(err)

	var validationErr validationError
	s.Require().ErrorAs(err, &validationErr)
	s.Equal("TEAM_MCP_SESSION_TTL", validationErr.field)
}

// TestValidateRejectsTitleLengthBelowMinimum verifies title-length lower bound.
func (s *validateSuite) TestValidateRejectsTitleLengthBelowMinimum() {
	cfg := s.validConfig()
	cfg.MaxTitleLength = 0

	err := validate(cfg)
	s.Require().Error(err)

	var validationErr validationError
	s.Require().ErrorAs(err, &validationErr)
	s.Equal("TEAM_MCP_MAX_TITLE_LENGTH", validationErr.field)
}

// TestValidateRejectsUnsupportedLogLevel verifies startup fails fast on invalid log level values.
func (s *validateSuite) TestValidateRejectsUnsupportedLogLevel() {
	cfg := s.validConfig()
	cfg.LogLevel = "trace"

	err := validate(cfg)
	s.Require().Error(err)

	var validationErr validationError
	s.Require().ErrorAs(err, &validationErr)
	s.Equal("TEAM_MCP_LOG_LEVEL", validationErr.field)
}

// validConfig builds one complete valid startup config baseline for invariant testing.
func (s *validateSuite) validConfig() *Config {
	return &Config{
		MessageDir:               "messages",
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
