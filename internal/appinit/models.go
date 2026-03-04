package appinit

import "context"

// lifecycleRunner executes periodic cleanup loops.
type lifecycleRunner func(ctx context.Context) error
