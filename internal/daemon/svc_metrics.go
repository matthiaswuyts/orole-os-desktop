package daemon

import (
	"context"

	"github.com/rayorole/orole-os-desktop/internal/systemmetrics"
)

// registerMetrics owns demand-based host sampling for this daemon's data volume.
// The sampler has no background work or handles that need lifecycle shutdown.
func (d *Daemon) registerMetrics() {
	d.Metrics = systemmetrics.NewSampler(d.DataDir)
	handle(d, "system.metrics", func(ctx context.Context, _ none) (systemmetrics.HostMetrics, error) {
		return d.Metrics.Snapshot(ctx)
	})
}
