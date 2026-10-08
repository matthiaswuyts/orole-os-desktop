package agents

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestClaudeModelCatalog(t *testing.T) {
	input := `{"type":"system","subtype":"ui_invalidate"}
{"type":"control_response","response":{"request_id":"another-request","subtype":"success"}}
{"type":"control_response","response":{"request_id":"orole-models","subtype":"success","response":{"models":[
{"value":"default","displayName":"Default (recommended)","resolvedModel":"claude-opus-5-5","supportedEffortLevels":["low","high","max"]},
{"value":"opus","displayName":"Opus 5.5","resolvedModel":"claude-opus-5-5","supportedEffortLevels":["low","high","high","unknown",""]},
{"value":"haiku","displayName":"Haiku","resolvedModel":"claude-haiku-4-5-20251001"},
{"value":"unversioned","displayName":"Authoritative SDK label"},
{"value":"provider-model-1"},
{"value":"opus[1m]","displayName":"Opus (1M context)","resolvedModel":"claude-opus-5-5[1m]"},
{"value":"hidden","disabled":true},
{"value":""},
{"value":"bad\nselector"}
]}}}`
	// SDK messages are JSON lines; collapse the multiline fixture's catalog line.
	parts := strings.SplitN(input, "\n", 3)
	input = parts[0] + "\n" + parts[1] + "\n" + strings.ReplaceAll(parts[2], "\n", "")
	got, err := readClaudeModels(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	want := []ModelOption{
		{ID: "default", Name: "Default (recommended) (claude-opus-5-5)", Reasoning: []string{"low", "high", "max"}, Default: true},
		{ID: "opus", Name: "Opus 5.5", Reasoning: []string{"low", "high"}},
		{ID: "haiku", Name: "Haiku (claude-haiku-4-5-20251001)", Reasoning: []string{}},
		{ID: "unversioned", Name: "Authoritative SDK label", Reasoning: []string{}},
		{ID: "provider-model-1", Name: "provider-model-1", Reasoning: []string{}},
		{ID: "opus[1m]", Name: "Opus (1M context) (claude-opus-5-5[1m])", Reasoning: []string{}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("catalog = %#v; want %#v", got, want)
	}
}

func TestClaudeModelCatalogErrors(t *testing.T) {
	for _, input := range []string{
		"",
		"not JSON",
		`{"type":"system","subtype":"init"}`,
		`{"type":"control_response","response":{"request_id":"orole-models","subtype":"error","error":"unsupported"}}`,
	} {
		if _, err := readClaudeModels(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted missing or failed catalog: %q", input)
		}
	}
	models, err := readClaudeModels(strings.NewReader(`{"type":"control_response","response":{"request_id":"orole-models","subtype":"success","response":{"models":[]}}}`))
	if err != nil || models == nil || len(models) != 0 {
		t.Fatalf("empty catalog = %v, %v", models, err)
	}
}

func TestClaudeModelsDiscovery(t *testing.T) {
	name := "fakeclaudemodels"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	bin := filepath.Join(t.TempDir(), name)
	if out, err := exec.Command("go", "build", "-o", bin, "./testdata/fakeclaudemodels").CombinedOutput(); err != nil {
		t.Fatalf("building fake Claude: %v\n%s", err, out)
	}
	logPath := filepath.Join(t.TempDir(), "discovery.log")
	t.Setenv("FAKE_CLAUDE_MODELS_LOG", logPath)
	t.Setenv("FAKE_CLAUDE_MODELS_MODE", "")
	originalConfig := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", originalConfig)
	m := NewManager(Config{Adapters: []Adapter{Claude{Bin: bin}}})
	defer m.Shutdown()
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		opts, err := m.Options(ctx, "local")
		cancel()
		if err != nil || len(opts.Harnesses) != 1 {
			t.Fatalf("options: %#v, %v", opts, err)
		}
		h := opts.Harnesses[0]
		if len(h.Models) != 3 || h.Models[1].ID != "opus" || h.Models[1].Name != "Opus (claude-opus-5-5)" || len(h.Models[2].Reasoning) != 0 || !reflect.DeepEqual(h.Reasoning, []string{"low", "high"}) {
			t.Fatalf("Claude options did not use per-model SDK metadata: %#v", h)
		}
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	dirs := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(dirs) != 1 {
		t.Fatalf("cached options started discovery %d times", len(dirs))
	}
	if _, err := os.Stat(strings.TrimSpace(dirs[0])); !os.IsNotExist(err) {
		t.Fatalf("discovery configuration was not removed: %v", err)
	}
	entries, err := os.ReadDir(originalConfig)
	if err != nil || len(entries) != 0 {
		t.Fatalf("native Claude configuration was touched: %v, %v", entries, err)
	}
	for _, mode := range []string{"error", "eof", "hang"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("FAKE_CLAUDE_MODELS_MODE", mode)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := listClaudeModels(ctx, bin)
			if err == nil {
				t.Fatal("failed initialization produced a catalog")
			}
			if mode == "hang" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("timeout = %v", err)
			}
		})
	}
}
