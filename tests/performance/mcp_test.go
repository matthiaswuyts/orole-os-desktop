package performance

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
)

// BenchmarkMCPLoopbackCallTool measures the adapter-facing manager primitive over
// real Streamable HTTP to a local SDK fixture, not a Mail/Drive service.
func BenchmarkMCPLoopbackCallTool(b *testing.B) {
	server := sdk.NewServer(&sdk.Implementation{Name: "latency-loopback-fixture", Version: "1.0.0"}, nil)
	server.AddTool(&sdk.Tool{
		Name:        "echo",
		Description: "Echo a local benchmark payload; no external service is contacted.",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
			"required":   []string{"text"},
		},
	}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var args struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("decode echo arguments: %w", err)
		}
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: args.Text}}}, nil
	})
	handler := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return server }, nil)
	fixture := httptest.NewServer(handler)
	b.Cleanup(func() {
		fixture.CloseClientConnections() // Also release long-lived SSE streams on failure.
		fixture.Close()
	})

	db := openDB(b)
	manager := mcp.New(mcp.Options{
		DB:             db.DB,
		Creds:          credentials.NewMemory(),
		HTTP:           fixture.Client(),
		Log:            slog.New(slog.NewTextHandler(io.Discard, nil)),
		ConnectTimeout: 5 * time.Second,
		CallTimeout:    5 * time.Second,
	})
	b.Cleanup(manager.Close)
	ctx := context.Background()
	if err := manager.Start(ctx); err != nil {
		b.Fatal(err)
	}
	state, err := manager.Add(ctx, mcp.AddParams{
		Name: "latency-loopback-fixture", URL: fixture.URL, AuthKind: mcp.AuthNone,
	})
	if err != nil {
		b.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		state, err = manager.Get(ctx, state.ID)
		if err != nil {
			b.Fatal(err)
		}
		if state.Status == mcp.StatusConnected {
			break
		}
		if time.Now().After(deadline) {
			b.Fatalf("MCP fixture did not connect: %s (%s)", state.Status, state.LastError)
		}
		time.Sleep(10 * time.Millisecond)
	}

	validate := func(result *mcp.CallResult, want string) {
		b.Helper()
		if result == nil || result.IsError || !result.Untrusted || len(result.Content) != 1 {
			b.Fatalf("invalid MCP echo result: %+v", result)
		}
		if block := result.Content[0]; block.Type != "text" || block.Text != want || block.Truncated {
			b.Fatalf("MCP echo content = %+v; want text %q", block, want)
		}
	}
	// Exercise distinct payloads before timing so a constant canned result cannot
	// pass validation. Initialization, catalog discovery and warmup are excluded.
	for _, text := range []string{"warmup-one", "warmup-two"} {
		result, err := manager.CallTool(ctx, state.ID, "echo", map[string]any{"text": text}, mcp.CallOptions{})
		if err != nil {
			b.Fatal(err)
		}
		validate(result, text)
	}
	const payload = "orole MCP loopback latency payload"
	args := map[string]any{"text": payload}
	var result *mcp.CallResult
	b.ReportAllocs()
	for b.Loop() {
		result, err = manager.CallTool(ctx, state.ID, "echo", args, mcp.CallOptions{})
		if err != nil {
			b.Fatal(err)
		}
		if result == nil || result.IsError {
			b.Fatalf("MCP echo failed: %+v", result)
		}
	}
	// b.Loop stops the timer before this content validation and all cleanup.
	validate(result, payload)
}
