package machines

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/policy"
)

func TestParsePhysicalMemory(t *testing.T) {
	for _, tc := range []struct{ name, backend, input string }{
		{"Linux", "posix", "my-host\nx86_64\nMemTotal: 8388608 kB\nMemFree: 42 kB\nMemAvailable: 2097152 kB\n"},
		{"Windows", "powershell", `{"hostname":"my-host","architecture":"64-bit","totalKiB":8388608,"availableKiB":2097152}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var h MachineHealth
			if err := parseHostMemory(tc.backend, tc.input, &h); err != nil {
				t.Fatal(err)
			}
			if !h.MemoryReady || h.MemoryTotalBytes != 8<<30 || h.MemoryAvailableBytes != 2<<30 || h.Hostname != "my-host" {
				t.Fatalf("unexpected observation: %+v", h)
			}
		})
	}
}

func TestRejectInvalidPhysicalMemory(t *testing.T) {
	for _, input := range []string{
		"host\nx86_64\nMemTotal: 100 kB\nMemFree: 1 kB",
		"host\nx86_64\nMemTotal: 100 MB\nMemAvailable: 1 kB",
		"host\nx86_64\nMemTotal: 100 kB\nMemAvailable: 101 kB",
		"host\nx86_64\nMemTotal: 18446744073709551615 kB\nMemAvailable: 1 kB",
		"host\nx86_64\nMemTotal: 100 kB\nMemAvailable: 1 kB\nMemAvailable: 2 kB",
	} {
		var h MachineHealth
		if err := parseHostMemory("posix", input, &h); err == nil || h.MemoryReady {
			t.Fatalf("accepted invalid metrics: %q", input)
		}
	}
	for _, input := range []string{`{}`, `{"hostname":"host","totalKiB":2,"availableKiB":3}`, `{"hostname":"host","totalKiB":18446744073709551615,"availableKiB":1}`} {
		var h MachineHealth
		if err := parseHostMemory("powershell", input, &h); err == nil || h.MemoryReady {
			t.Fatalf("accepted invalid metrics: %q", input)
		}
	}
}

func TestProbeOutputIsBounded(t *testing.T) {
	var output probeOutput
	if _, ok := any(&output).(io.ReaderFrom); ok {
		t.Fatal("ReaderFrom would bypass the output limit")
	}
	if _, err := io.Copy(&output, strings.NewReader(strings.Repeat("x", 32769))); err == nil {
		t.Fatal("accepted oversized probe output")
	}
	if output.buffer.Len() > 32768 {
		t.Fatal("retained oversized output")
	}
}

type noninteractiveHealthPolicy struct{ decision policy.Decision }

func (p noninteractiveHealthPolicy) Check(context.Context, policy.Request) (policy.Decision, error) {
	panic("health must not request interactive approval")
}
func (p noninteractiveHealthPolicy) Decide(context.Context, policy.Request) (policy.Evaluation, error) {
	return policy.Evaluation{Decision: p.decision}, nil
}

func TestHealthDoesNotAskForApproval(t *testing.T) {
	m, _ := testManager(t)
	m.policy = noninteractiveHealthPolicy{policy.Ask}
	if _, err := m.Health(context.Background(), "user", "local"); !errors.Is(err, ErrDenied) {
		t.Fatalf("ask: %v", err)
	}
	m.policy = noninteractiveHealthPolicy{policy.Allow}
	h, err := m.Health(context.Background(), "user", "local")
	if err != nil || !h.Reachable || h.MemoryReady || h.MetricError == "" {
		t.Fatalf("missing sampler must be explicit, not daemon heap: %+v %v", h, err)
	}
}

func TestLocalCustomNamePersistsAndDoesNotGrantAccess(t *testing.T) {
	m, p := testManager(t)
	ctx := context.Background()
	before, err := m.Get(ctx, "local")
	if err != nil {
		t.Fatal(err)
	}
	renamed, err := m.Rename(ctx, "user", "local", "  Personal workstation  ")
	if err != nil || renamed.Name != "Personal workstation" {
		t.Fatalf("rename: %+v %v", renamed, err)
	}
	for range 2 {
		machines, err := m.List(ctx)
		if err != nil || len(machines) != 1 || machines[0].Name != "Personal workstation" {
			t.Fatalf("list reset custom name: %+v %v", machines, err)
		}
	}
	after, err := m.Get(ctx, "local")
	if err != nil || after.AllowCommands != before.AllowCommands || after.AllowWrite != before.AllowWrite || after.accessRevision != before.accessRevision {
		t.Fatalf("rename changed access: %+v %v", after, err)
	}
	p.decision = policy.Deny
	if _, err := m.Rename(ctx, "user", "local", "Denied"); !errors.Is(err, ErrDenied) {
		t.Fatalf("rename bypassed policy: %v", err)
	}
	for _, name := range []string{"", " ", "bad\nname", strings.Repeat("x", 121)} {
		if _, err := m.Rename(ctx, "user", "local", name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("accepted name %q: %v", name, err)
		}
	}
}
