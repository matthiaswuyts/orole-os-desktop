package plugins

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rayorole/orole-os-desktop/internal/mcp"
)

func TestRunReturnsSanitizedPlainTextOnly(t *testing.T) {
	m, _ := fixture(t)
	record := enable(t, m, saveFixture(t, m))
	m.invoker = invokeFunc(func(ctx context.Context, _ string, _ string, _ map[string]any, opts mcp.CallOptions) (*mcp.CallResult, error) {
		if err := opts.BeforeCall(ctx); err != nil {
			return nil, err
		}
		return &mcp.CallResult{
			IsError: true,
			Content: []mcp.ContentBlock{
				{Type: "text", Text: "Visible\x00\u202e\u200b\u2066\ufeff\x1b\n\ttext"},
				{Type: "resource", Text: "<script>not executed</script>", URI: "private://do-not-forward"},
				{Type: "image", Text: "do-not-forward-image", Bytes: 10},
			},
			Structured: json.RawMessage(`{"token":"do-not-forward-secret"}`),
		}, nil
	})
	out, err := m.Run(context.Background(), runRequest(record))
	if err != nil || out.Text != "Visible\n\ttext\n<script>not executed</script>" || !out.IsError || out.Truncated {
		t.Fatalf("unsafe/wrong output: %+v, %v", out, err)
	}
}

func TestPlainTextLimitIsByteBoundedAndRuneSafe(t *testing.T) {
	cases := []struct {
		name      string
		blocks    []mcp.ContentBlock
		truncated bool
	}{
		{"exact bound", []mcp.ContentBlock{{Type: "text", Text: strings.Repeat("a", maxResultBytes)}}, false},
		{"oversized block", []mcp.ContentBlock{{Type: "text", Text: strings.Repeat("a", maxResultBytes+1)}}, true},
		{"multibyte boundary", []mcp.ContentBlock{{Type: "text", Text: strings.Repeat("a", maxResultBytes-1) + "界"}}, true},
		{"multiple blocks", []mcp.ContentBlock{{Type: "text", Text: strings.Repeat("a", maxResultBytes-2)}, {Type: "text", Text: "界"}}, true},
		{"inherited truncation", []mcp.ContentBlock{{Type: "text", Text: "short", Truncated: true}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := plainResult(&mcp.CallResult{Content: tc.blocks})
			if len(out.Text) > maxResultBytes || !utf8.ValidString(out.Text) || out.Truncated != tc.truncated {
				t.Fatalf("result exceeded boundary: bytes=%d, utf8=%v, truncated=%v", len(out.Text), utf8.ValidString(out.Text), out.Truncated)
			}
		})
	}
}
