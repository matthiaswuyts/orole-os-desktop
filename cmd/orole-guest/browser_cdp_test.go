package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestPlanTabs(t *testing.T) {
	tests := []struct {
		name    string
		tracked []string
		pages   []string
		max     int
		want    tabPlan
	}{
		{"nothing yet", nil, []string{"P1"}, 8, tabPlan{Front: "P1", Attach: true}},
		{"reuse the front tab", []string{"P1"}, []string{"P1"}, 8, tabPlan{Front: "P1"}},
		{"new tab in front, old still open", []string{"P1"}, []string{"P2", "P1"}, 8, tabPlan{Front: "P2", Attach: true}},
		{"switch back to a tracked tab", []string{"P1", "P2"}, []string{"P1", "P2"}, 8, tabPlan{Front: "P1"}},
		{"attached tab closed", []string{"P1"}, []string{"P3"}, 8, tabPlan{Front: "P3", Cancel: []string{"P1"}, Attach: true}},
		{"closed tab cancelled, front reused", []string{"P1", "P2"}, []string{"P2"}, 8, tabPlan{Front: "P2", Cancel: []string{"P1"}}},
		{"no page left", []string{"P1"}, nil, 8, tabPlan{Cancel: []string{"P1"}, Attach: true}},
		{"too many live tabs", []string{"A", "B"}, []string{"C", "A", "B"}, 2, tabPlan{Front: "C", Attach: true, TooMany: true}},
		{"closed tabs make room", []string{"A", "B"}, []string{"C", "A"}, 2, tabPlan{Front: "C", Cancel: []string{"B"}, Attach: true}},
	}
	for _, tt := range tests {
		got := planTabs(tt.tracked, tt.pages, tt.max)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("%s: planTabs = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}
