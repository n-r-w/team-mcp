package config

import (
	"testing"
	"time"

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
		LogLevel:                 "info",
		LifecycleCollectInterval: time.Second,
	}
}
