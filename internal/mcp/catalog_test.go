package mcp

import (
	"context"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
)

func TestResourcesTemplatesPromptsCached(t *testing.T) {
	h := newHarness(t)
	srv := mcptest.New(t, mcptest.Options{OpenMCP: true, Setup: func(s *sdk.Server) {
		mcptest.DefaultTools(s)
		read := func(ctx context.Context, r *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: r.Params.URI, Text: "fixture", MIMEType: "text/plain"}}}, nil
		}
		s.AddResource(&sdk.Resource{Name: "Notes", URI: "notes://all", MIMEType: "text/plain"}, read)
		s.AddResourceTemplate(&sdk.ResourceTemplate{Name: "Note", URITemplate: "notes://{id}", MIMEType: "text/plain"}, read)
		s.AddPrompt(&sdk.Prompt{Name: "summarize", Description: "Summarize the selected note"}, func(context.Context, *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
			return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{Text: "Summarize this note."}}}}, nil
		})
	}})
	st := h.add(t, srv, AddParams{AuthKind: AuthNone})
	h.wait(t, st.ID, StatusConnected)
	cat, err := h.m.RefreshCatalog(context.Background(), st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Resources) != 2 || len(cat.Prompts) != 1 {
		t.Fatalf("catalog: %+v", cat)
	}
	cached, err := h.m.loadCatalog(st.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cached.Resources) != 2 || !cached.Resources[1].Template || cached.Prompts[0].Name != "summarize" {
		t.Fatalf("cached: %+v", cached)
	}
}
