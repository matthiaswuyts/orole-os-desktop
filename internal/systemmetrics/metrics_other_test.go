//go:build !windows

package systemmetrics

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedHostDoesNotReturnFakeMetrics(t *testing.T) {
	got, err := NewSampler(t.TempDir()).Snapshot(context.Background())
	if !errors.Is(err, ErrUnsupported) || got != (HostMetrics{}) {
		t.Fatalf("unsupported host returned fabricated metrics: %+v, %v", got, err)
	}
}
