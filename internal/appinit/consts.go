package appinit

import "time"

const (
	// defaultCleanupStopTimeout caps wait time for lifecycle collector stop during shutdown.
	defaultCleanupStopTimeout = 2 * time.Second
	// defaultShutdownTimeout caps shutdown cleanup execution to prevent indefinite blocking.
	defaultShutdownTimeout = 5 * time.Second
)
