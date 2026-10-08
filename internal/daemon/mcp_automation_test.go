package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/credentials"
	"github.com/rayorole/orole-os-desktop/internal/database"
	"github.com/rayorole/orole-os-desktop/internal/mcp"
	"github.com/rayorole/orole-os-desktop/internal/mcp/mcptest"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"github.com/rayorole/orole-os-desktop/migrations"
)

func TestAutomationMCPScrubsProductionCredentialsBeforeJournal(t *testing.T) {
	for _, lifecycle := range []string{"set", "loaded", "rotated"} {
		t.Run(lifecycle, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			db, err := database.Open(filepath.Join(t.TempDir(), "automation.db"), migrations.FS)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			creds := credentials.NewMemory()
			pol := policy.New(db.DB, nil, nil)
			oldToken, newToken := "opaque-credential-alpha-51", "opaque-credential-beta-52"
			srv := mcptest.New(t, mcptest.Options{Setup: func(s *sdk.Server) {
				mcptest.AddJSONTool(s, "diagnostic", "Diagnostic", mcptest.ReadOnly(), nil, nil, func(map[string]any) (any, error) {
					return map[string]any{"message": oldToken + " " + newToken}, nil
				})
				mcptest.AddJSONTool(s, "failure", "Failure", mcptest.ReadOnly(), nil, nil, func(map[string]any) (any, error) {
					return nil, errors.New("diagnostic " + oldToken + " " + newToken)
				})
			}})
			srv.AddStaticToken(oldToken)
			srv.AddStaticToken(newToken)
			newManager := func() *mcp.Manager {
				m := mcp.New(mcp.Options{DB: db.DB, Creds: creds, Policy: pol})
				if err := m.Start(ctx); err != nil {
					t.Fatal(err)
				}
				return m
			}
			manager := newManager()
			defer func() { manager.Close() }()
			st, err := manager.Add(ctx, mcp.AddParams{Name: "echoing service", URL: srv.MCPURL, AuthKind: mcp.AuthBearer, BearerToken: oldToken})
			if err != nil {
				t.Fatal(err)
			}
			if lifecycle == "loaded" {
				manager.Close()
				manager = newManager() // credentials must be learned from the real store
			}
			if lifecycle == "rotated" {
				if _, err := manager.Update(ctx, st.ID, mcp.UpdateParams{BearerToken: &newToken}); err != nil {
					t.Fatal(err)
				}
			}
			for {
				state, err := manager.Get(ctx, st.ID)
				if err != nil {
					t.Fatal(err)
				}
				if state.Status == mcp.StatusConnected {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("MCP did not connect: %+v", state)
				case <-time.After(10 * time.Millisecond):
				}
			}
			d := &Daemon{MCP: manager}
			engine := automations.New(automations.Options{DB: db.DB, Credentials: creds})
			engine.SetToolInvoker(automationMCP{d: d})
			if err := engine.Start(ctx); err != nil {
				t.Fatal(err)
			}
			defer engine.Close()
			for _, tool := range []string{"diagnostic", "failure"} {
				if err := manager.SetToolPermission(ctx, st.ID, tool, "allow"); err != nil {
					t.Fatal(err)
				}
				params, _ := json.Marshal(map[string]any{"server": st.ID, "tool": tool})
				saved, err := engine.Save(ctx, automations.Definition{Name: "credential echo", Steps: []automations.Step{{ID: "call", Type: "mcp.tool", Params: params}}})
				if err != nil || !saved.Saved {
					t.Fatalf("save: %v %+v", err, saved.Issues)
				}
				run, err := engine.Run(ctx, saved.Definition.ID)
				if err != nil {
					t.Fatal(err)
				}
				for {
					detail, err := engine.Detail(ctx, run.ID)
					if err != nil {
						t.Fatal(err)
					}
					if detail.Run.Status == "succeeded" || detail.Run.Status == "failed" {
						want := "succeeded"
						if tool == "failure" {
							want = "failed"
						}
						if detail.Run.Status != want || len(detail.Steps) != 1 {
							t.Fatalf("unexpected execution: %+v", detail)
						}
						serialized, _ := json.Marshal(detail)
						if bytes.Contains(serialized, []byte(oldToken)) || (lifecycle == "rotated" && bytes.Contains(serialized, []byte(newToken))) {
							t.Fatal("MCP credential persisted in automation detail")
						}
						if !bytes.Contains(detail.Steps[0].Output, []byte("[REDACTED]")) {
							t.Fatal("expected redacted tool content in persisted journal")
						}
						break
					}
					select {
					case <-ctx.Done():
						t.Fatalf("automation did not finish: %+v", detail)
					case <-time.After(10 * time.Millisecond):
					}
				}
			}
		})
	}
}
