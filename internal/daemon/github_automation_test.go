package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/automations"
	"github.com/rayorole/orole-os-desktop/internal/github"
)

func TestGitHubAutomationChecksSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/commits/main/check-runs" {
			t.Errorf("unexpected query path: %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"check_runs":[{"conclusion":"failure"},{"conclusion":"timed_out"},{"conclusion":"cancelled"},{"conclusion":"action_required"},{"conclusion":"success"},{"conclusion":"skipped"},{"status":"queued"}]}`)
	}))
	defer srv.Close()
	c := github.NewClient(github.StaticSource{T: "test-only"})
	c.BaseURL = srv.URL
	g := &ghService{client: c}
	result, err := g.QueryGitHub(context.Background(), automations.QueryRequest{Kind: "checks", Repo: "o/r", Query: "main"})
	if err != nil {
		t.Fatal(err)
	}
	out := result.(map[string]any)
	if out["failed"] != 4 || out["repo"] != "o/r" || out["ref"] != "main" || len(out["checks"].([]github.CheckRun)) != 7 {
		t.Fatalf("incorrect normalized checks: %+v", out)
	}
}
