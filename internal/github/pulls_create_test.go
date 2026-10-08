package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestCreatePullAuthorizationAndDraft(t *testing.T) {
	calls := 0
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodPost || r.URL.Path != "/repos/owner/repo/pulls" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var in struct {
			Title string `json:"title"`
			Body  string `json:"body"`
			Head  string `json:"head"`
			Base  string `json:"base"`
			Draft bool   `json:"draft"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			t.Error(err)
		}
		if !in.Draft || in.Title != "Reviewed title" || in.Body != "Untrusted issue context" || in.Head != "orole/issue-54" || in.Base != "main" {
			t.Errorf("unexpected payload: %+v", in)
		}
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, `{"number":54,"draft":true,"html_url":"https://github.com/owner/repo/pull/54"}`)
	}))
	create := func() (PullRequest, error) {
		return c.CreatePull(context.Background(), "owner/repo", "Reviewed title", "Untrusted issue context", "orole/issue-54", "main")
	}
	if _, err := create(); !errors.Is(err, ErrNoAuthorizer) || calls != 0 {
		t.Fatalf("missing authorizer: %v, calls=%d", err, calls)
	}
	denied := errors.New("denied")
	c.Authorize = func(context.Context, string, string) error { return denied }
	if _, err := create(); !errors.Is(err, denied) || calls != 0 {
		t.Fatalf("denied authorizer: %v, calls=%d", err, calls)
	}
	c.Authorize = func(_ context.Context, action, detail string) error {
		if action != "github.pull.create" || detail != "owner/repo" {
			t.Errorf("authorization context: %q %q", action, detail)
		}
		return nil
	}
	pr, err := create()
	if err != nil || pr.Number != 54 || !pr.Draft || calls != 1 {
		t.Fatalf("create: %+v, %v, calls=%d", pr, err, calls)
	}
}

func TestPullPublicationRejectsInvalidInputBeforeAuthorization(t *testing.T) {
	c := NewClient(StaticSource{T: testToken})
	c.Authorize = func(context.Context, string, string) error {
		t.Fatal("invalid input reached authorizer")
		return nil
	}
	for _, tc := range []struct{ repo, title, head, base string }{
		{"https://evil.test/a/b", "title", "topic", "main"},
		{"a/b", " ", "topic", "main"},
		{"a/b", "title", "--exec=x", "main"},
		{"a/b", "title", "owner:topic", "main"},
		{"a/b", "title", "topic", "HEAD"},
		{"a/b", "title", "topic", "topic"},
	} {
		if _, err := c.CreatePull(context.Background(), tc.repo, tc.title, "", tc.head, tc.base); err == nil {
			t.Errorf("accepted invalid create: %+v", tc)
		}
		if strings.TrimSpace(tc.title) != "" {
			if _, _, err := c.FindPullByHead(context.Background(), tc.repo, tc.head, tc.base); err == nil {
				t.Errorf("accepted invalid lookup: %+v", tc)
			}
		}
	}
}

func TestCreatePullAmbiguousFailureDoesNotRetryPost(t *testing.T) {
	posts, gets := 0, 0
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"message":"response lost after creation"}`)
			return
		}
		gets++
		if r.URL.Query().Get("state") != "all" || r.URL.Query().Get("head") != "owner:orole/issue-54" || r.URL.Query().Get("base") != "main" {
			t.Errorf("retry query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `[{"number":54,"state":"closed","merged":true,"head":{"ref":"orole/issue-54","repo":{"full_name":"owner/repo"}},"base":{"ref":"main","repo":{"full_name":"owner/repo"}}}]`)
	}))
	c.Authorize = allowAll
	if _, err := c.CreatePull(context.Background(), "owner/repo", "title", "", "orole/issue-54", "main"); err == nil || posts != 1 {
		t.Fatalf("ambiguous create: %v, posts=%d", err, posts)
	}
	// Lookup remains available without a write authorizer and finds merged PRs.
	c.Authorize = nil
	pr, found, err := c.FindPullByHead(context.Background(), "owner/repo", "orole/issue-54", "main")
	if err != nil || !found || !pr.Merged || pr.Number != 54 || posts != 1 || gets != 1 {
		t.Fatalf("retry lookup: %+v found=%v err=%v posts=%d gets=%d", pr, found, err, posts, gets)
	}
}

func TestFindPullByHeadRejectsForeignRefsAndPaginates(t *testing.T) {
	var baseURL string
	c, srv, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "2" {
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/pulls?page=2>; rel="next"`, baseURL))
			fmt.Fprint(w, `[{"number":1,"head":{"ref":"topic","repo":{"full_name":"fork/r"}},"base":{"ref":"main","repo":{"full_name":"o/r"}}},{"number":2,"head":{"ref":"other","repo":{"full_name":"o/r"}},"base":{"ref":"main","repo":{"full_name":"o/r"}}}]`)
			return
		}
		fmt.Fprint(w, `[{"number":3,"head":{"ref":"topic","repo":{"full_name":"o/r"}},"base":{"ref":"main","repo":{"full_name":"o/r"}}}]`)
	}))
	baseURL = srv.URL
	pr, found, err := c.FindPullByHead(context.Background(), "o/r", "topic", "main")
	if err != nil || !found || pr.Number != 3 {
		t.Fatalf("lookup: %+v %v %v", pr, found, err)
	}
}

func TestFindPullByHeadDistinguishesMissingFromFailure(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				if status == http.StatusOK {
					fmt.Fprint(w, `[]`)
				} else {
					fmt.Fprint(w, `{"message":"permission denied"}`)
				}
			}))
			_, found, err := c.FindPullByHead(context.Background(), "o/r", "topic", "main")
			if found || (err == nil) != (status == http.StatusOK) {
				t.Fatalf("found=%v err=%v status=%d", found, err, status)
			}
		})
	}
}

func TestBranchHeadReturnsExactCommitWithEscapedBranch(t *testing.T) {
	sha := strings.Repeat("a1", 20)
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/repos/owner/repo/git/ref/heads/release%2Fstable" {
			t.Errorf("unsafe branch request: %s %s", r.Method, r.URL.EscapedPath())
		}
		fmt.Fprintf(w, `{"ref":"refs/heads/release/stable","object":{"type":"commit","sha":%q}}`, sha)
	}))
	got, err := c.BranchHead(context.Background(), "owner/repo", "release/stable")
	if err != nil || got != sha {
		t.Fatalf("head=%q err=%v", got, err)
	}
}

func TestBranchHeadRejectsInvalidReferencesAndResponses(t *testing.T) {
	calls := 0
	response := `{"ref":"refs/heads/main","object":{"type":"tag","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, response)
	}))
	for _, branch := range []string{"", "HEAD", "../main", "main?x", "main#fragment\n", "main~1", "--exec"} {
		if _, err := c.BranchHead(context.Background(), "owner/repo", branch); err == nil {
			t.Errorf("accepted invalid branch %q", branch)
		}
	}
	if calls != 0 {
		t.Fatalf("invalid branches reached network: %d", calls)
	}
	for _, body := range []string{
		response,
		`{"ref":"refs/heads/main","object":{"type":"commit","sha":"short"}}`,
		`{"ref":"refs/heads/other","object":{"type":"commit","sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`,
	} {
		response = body
		if _, err := c.BranchHead(context.Background(), "owner/repo", "main"); err == nil {
			t.Errorf("accepted noncommit or mismatched ref: %s", body)
		}
	}
}

func TestBranchHeadMissingIsNotAnEmptySuccess(t *testing.T) {
	c, _, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"message":"Not Found"}`)
	}))
	head, err := c.BranchHead(context.Background(), "owner/repo", "missing")
	if err == nil || head != "" {
		t.Fatalf("head=%q err=%v", head, err)
	}
}
