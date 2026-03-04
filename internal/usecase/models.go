package usecase

import "time"

// Options contains runtime settings for coordination use cases.
type Options struct {
	SessionTTL     time.Duration
	MaxTitleLength int
}
