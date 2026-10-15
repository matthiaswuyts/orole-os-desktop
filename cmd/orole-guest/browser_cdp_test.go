package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/sandbox/guestagent"
)

func TestPageTargetPicksTheFirstPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/json/list" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`[{"id":"sw1","type":"service_worker","url":"https://x"},{"id":"P1","type":"page","url":"https://example.com"},{"id":"P2","type":"page","url":"about:blank"}]`))
	}))
	defer srv.Close()
	id, err := pageTarget(context.Background(), srv.Client(), srv.URL)
	if err != nil || id != "P1" {
		t.Fatalf("pageTarget = %q %v", id, err)
	}
}

func TestPageTargetWithoutChromiumIsNoBrowser(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more
	if _, err := pageTarget(context.Background(), http.DefaultClient, url); !errors.Is(err, guestagent.ErrNoBrowser) {
		t.Fatalf("err = %v", err)
	}
}

func TestResolveScriptQuotesTheTarget(t *testing.T) {
	hostile := `"); document.body.innerHTML = ""; ("`
	script := resolveScript("m", hostile)
	quoted, _ := json.Marshal(hostile)
	if !strings.HasSuffix(script, ", "+string(quoted)+", false)") {
		t.Fatalf("target must be passed as a JSON string literal: %s", script[len(script)-120:])
	}
	if s := resolveScript("m", "text=Sign in"); !strings.HasSuffix(s, `, "Sign in", true)`) {
		t.Fatalf("text= targets match by visible text: %s", s[len(s)-40:])
	}
}

func TestResolveScriptAgreesWithValidationOnThePrefix(t *testing.T) {
	// Leading whitespace before text= still means text; the prefix is case-sensitive.
	if s := resolveScript("m", "  text= Sign in "); !strings.HasSuffix(s, `, "Sign in", true)`) {
		t.Fatalf("padded text= target: %s", s[len(s)-40:])
	}
	if s := resolveScript("m", "TEXT=Sign in"); !strings.HasSuffix(s, `, "TEXT=Sign in", false)`) {
		t.Fatalf("upper-case prefix is a selector: %s", s[len(s)-40:])
	}
}

func TestNeedAttach(t *testing.T) {
	tests := []struct {
		name     string
		attached string
		alive    bool
		current  string
		want     bool
	}{
		{"first use", "", false, "P1", true},
		{"same live tab", "P1", true, "P1", false},
		{"visible tab changed", "P1", true, "P2", true},
		{"attached tab closed, another remains", "P1", true, "P3", true},
		{"no page left", "P1", true, "", true},
		{"context died", "P1", false, "P1", true},
	}
	for _, tt := range tests {
		if got := needAttach(tt.attached, tt.alive, tt.current); got != tt.want {
			t.Errorf("%s: needAttach = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPageTargetFollowsTheVisibleTab(t *testing.T) {
	body := `[{"id":"P1","type":"page"}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer srv.Close()
	attached, _ := pageTarget(context.Background(), srv.Client(), srv.URL)
	if needAttach(attached, true, attached) {
		t.Fatal("same tab must not re-attach")
	}
	body = `[{"id":"P2","type":"page"},{"id":"P1","type":"page"}]` // a new tab opened in front
	cur, _ := pageTarget(context.Background(), srv.Client(), srv.URL)
	if !needAttach(attached, true, cur) {
		t.Fatalf("new visible tab %q must re-attach", cur)
	}
}
