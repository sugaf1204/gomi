package dns

import "context"

// Controller is implemented by DNS integrations that can be run by the GOMI
// runtime and refreshed from current stores.
type Controller interface {
	Start(ctx context.Context) error
	Sync(ctx context.Context) error
}
