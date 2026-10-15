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
	script := resolveScript(hostile)
	quoted, _ := json.Marshal(hostile)
	if !strings.HasSuffix(script, string(quoted)+", false)") {
		t.Fatalf("target must be passed as a JSON string literal: %s", script[len(script)-120:])
	}
	if s := resolveScript("text=Sign in"); !strings.HasSuffix(s, `("Sign in", true)`) {
		t.Fatalf("text= targets match by visible text: %s", s[len(s)-40:])
	}
}

func TestResolveScriptAgreesWithValidationOnThePrefix(t *testing.T) {
	// Leading whitespace before text= still means text; the prefix is case-sensitive.
	if s := resolveScript("  text= Sign in "); !strings.HasSuffix(s, `("Sign in", true)`) {
		t.Fatalf("padded text= target: %s", s[len(s)-40:])
	}
	if s := resolveScript("TEXT=Sign in"); !strings.HasSuffix(s, `("TEXT=Sign in", false)`) {
		t.Fatalf("upper-case prefix is a selector: %s", s[len(s)-40:])
	}
}
