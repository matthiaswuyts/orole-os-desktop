package policy

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRequireOnceCannotRememberOrReuseWildcardAllow(t *testing.T) {
	e, _, _ := newEngine(t)
	ctx := context.Background()
	if _, err := e.AddRule(ctx, Rule{Actor: "*", Action: "*", Resource: "*", Decision: Allow}); err != nil {
		t.Fatal(err)
	}
	req := Request{Actor: "user", Action: "drive.share", Resource: "drive-snapshot/exact"}
	for _, allow := range []bool{false, true, true} {
		done := make(chan error, 1)
		go func() { done <- e.RequireOnce(ctx, req) }()
		p := waitPending(t, e)
		if p.CanRemember {
			t.Fatal("one-shot disclosure can be remembered")
		}
		if err := e.Resolve(ctx, p.ID, Resolution{Allow: true, Remember: true}); !errors.Is(err, ErrNoRemember) {
			t.Fatalf("remember: %v", err)
		}
		if err := e.Resolve(ctx, p.ID, Resolution{Allow: allow}); err != nil {
			t.Fatal(err)
		}
		if err := <-done; (allow && err != nil) || (!allow && !errors.Is(err, ErrDenied)) {
			t.Fatalf("allow=%v: %v", allow, err)
		}
		if err := e.Resolve(ctx, p.ID, Resolution{Allow: true}); !errors.Is(err, ErrNotFound) {
			t.Fatalf("approval replay: %v", err)
		}
	}
}

func TestRequireOnceDenialAndExpiry(t *testing.T) {
	e, _, _ := newEngine(t)
	req := Request{Actor: "user", Action: "drive.share", Resource: "drive-snapshot/exact", Timeout: time.Millisecond}
	if err := e.RequireOnce(context.Background(), req); !errors.Is(err, ErrApprovalTimeout) {
		t.Fatalf("expiry: %v", err)
	}
	if len(e.Pending()) != 0 {
		t.Fatal("expired approval retained")
	}
	if _, err := e.AddRule(context.Background(), Rule{Actor: "user", Action: req.Action, Resource: "*", Decision: Deny}); err != nil {
		t.Fatal(err)
	}
	if err := e.RequireOnce(context.Background(), req); !errors.Is(err, ErrDenied) {
		t.Fatalf("deny: %v", err)
	}
	if len(e.Pending()) != 0 {
		t.Fatal("denied request queued")
	}
}
