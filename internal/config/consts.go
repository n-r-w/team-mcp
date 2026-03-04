package config

import "time"

const (
	// minimumSessionTTL protects lifecycle cleanup from pathological tight TTL values.
	minimumSessionTTL = time.Minute
	// minimumPositiveInteger is used for integer config values that must be > 0.
	minimumPositiveInteger = 1
	// logLevelDebug enables debug-level log verbosity.
	logLevelDebug = "debug"
	// logLevelInfo enables info-level log verbosity.
	logLevelInfo = "info"
	// logLevelWarn enables warn-level log verbosity.
	logLevelWarn = "warn"
	// logLevelError enables error-level log verbosity.
	logLevelError = "error"
	// messageDirectoryName defines default relative message storage location.
	messageDirectoryName = "team-mcp/messages"
)
