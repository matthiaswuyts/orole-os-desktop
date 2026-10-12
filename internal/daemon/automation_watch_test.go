package daemon

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
)

func keys(items []automations.WatchItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Key
	}
	return out
}

func TestDeploymentItemsFromIntegrationResults(t *testing.T) {
	cases := []struct {
		name   string
		res    *mcp.CallResult
		states []string
		want   []string
	}{
		{"vercel-style JSON text with a preamble",
			&mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: `Deployments: {"deployments":[{"uid":"dpl_2","state":"READY","url":"b.vercel.app"},{"uid":"dpl_1","state":"ERROR"}],"pagination":{}}`}}},
			nil, []string{"dpl_2:READY", "dpl_1:ERROR"}},
		{"state filter is case-insensitive",
			&mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: `[{"id":"a","status":"SUCCESS"},{"id":"b","status":"FAILED"}]`}}},
			[]string{"failed"}, []string{"b:FAILED"}},
		{"graphql edges in structured content",
			&mcp.CallResult{Structured: json.RawMessage(`{"deployments":{"edges":[{"node":{"id":"r1","status":"BUILDING"}},{"node":{"id":"r0","status":"SUCCESS"}}]}}`)},
			nil, []string{"r1:BUILDING", "r0:SUCCESS"}},
		{"items without id or state are ignored",
			&mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: `[{"id":"x"},{"state":"READY"},{"id":"y","state":"READY"}]`}}},
			nil, []string{"y:READY"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			items, err := deploymentItems(automations.Watch{Server: "vercel", Tool: "list_deployments", States: c.states}, c.res)
			if err != nil || !slices.Equal(keys(items), c.want) {
				t.Fatalf("got %v %v, want %v", keys(items), err, c.want)
			}
		})
	}
	if _, err := deploymentItems(automations.Watch{Tool: "t"}, &mcp.CallResult{Content: []mcp.ContentBlock{{Type: "text", Text: "3 deployments, all ready"}}}); err == nil {
		t.Fatal("prose accepted as deployments")
	}
}
