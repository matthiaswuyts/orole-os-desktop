package automations

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/policy"
)

// Explicit opt-in for contract tests that exercise execution rather than policy.
func trustedTestAuthorizer(context.Context, policy.Request) error { return nil }

// The child process leaves a real filesystem effect; denied dispatch must never
// reach it. Running the test binary avoids shell and external-tool dependencies.
func TestAutomationPolicyProcess(t *testing.T) {
	if os.Getenv("OROLE_AUTOMATION_POLICY_HELPER") != "1" {
		return
	}
	if err := os.WriteFile(os.Args[len(os.Args)-1], []byte("executed"), 0600); err != nil {
		os.Exit(2)
	}
	os.Exit(0)
}

func policyCommandDefinition(t *testing.T, kind, marker string) Definition {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	params, err := json.Marshal(map[string]any{
		"executable": executable, "dir": filepath.Dir(marker),
		"args": []string{"-test.run=^TestAutomationPolicyProcess$", "--", "{{ run.automationId }}", marker},
		"env":  map[string]string{"OROLE_AUTOMATION_POLICY_HELPER": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return Definition{Name: "policy command", AllowedExecutables: []string{executable}, Steps: []Step{
		{ID: "effect", Type: kind, Strategy: "repeatable", Retries: 2, BackoffMs: 1, Params: params},
	}}
}

func openPolicyEngine(t *testing.T, path string) (*Engine, *policy.Engine) {
	t.Helper()
	var shared *policy.Engine
	e := openEngine(t, path, Options{Authorize: func(ctx context.Context, request policy.Request) error {
		return shared.Require(ctx, request)
	}})
	shared = policy.New(e.db, nil, e.log)
	return e, shared
}

func awaitPolicyApproval(t *testing.T, shared *policy.Engine) policy.Pending {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if pending := shared.Pending(); len(pending) == 1 {
			return pending[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("expected shared-policy approval")
	return policy.Pending{}
}

func assertNoCommandEffect(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unauthorized process effect: %v", err)
	}
}

func TestCommandSharedPolicyDispatch(t *testing.T) {
	for _, kind := range []string{"command.run", "test.run"} {
		for _, decision := range []string{"missing", "rule-deny", "reject", "allow"} {
			t.Run(kind+"/"+decision, func(t *testing.T) {
				marker := filepath.Join(t.TempDir(), "effect")
				var e *Engine
				var shared *policy.Engine
				path := filepath.Join(t.TempDir(), "db")
				if decision == "missing" {
					e = openEngine(t, path, Options{})
				} else {
					e, shared = openPolicyEngine(t, path)
				}
				d := saveDef(t, e, policyCommandDefinition(t, kind, marker))
				if decision == "rule-deny" {
					if _, err := shared.AddRule(context.Background(), policy.Rule{Actor: "automation:" + d.ID, Action: kind, Resource: "*", Decision: policy.Deny}); err != nil {
						t.Fatal(err)
					}
				}
				r, err := e.Run(context.Background(), d.ID)
				if err != nil {
					t.Fatal(err)
				}
				if decision == "reject" || decision == "allow" {
					pending := awaitPolicyApproval(t, shared)
					if pending.Actor != "automation:"+d.ID || pending.Action != kind || pending.Effect != policy.EffectDestructive {
						t.Fatalf("wrong authority: %+v", pending)
					}
					var detail struct {
						Executable string            `json:"executable"`
						Argv       []string          `json:"argv"`
						Dir        string            `json:"dir"`
						Env        map[string]string `json:"env"`
					}
					if err := json.Unmarshal([]byte(pending.Detail), &detail); err != nil || detail.Executable != d.AllowedExecutables[0] || detail.Dir != filepath.Dir(marker) || len(detail.Argv) != 4 || detail.Argv[2] != d.ID || detail.Argv[3] != marker || detail.Env["OROLE_AUTOMATION_POLICY_HELPER"] != "1" {
						t.Fatalf("approval omitted concrete command: %s (%v)", pending.Detail, err)
					}
					assertNoCommandEffect(t, marker)
					if err := shared.Resolve(context.Background(), pending.ID, policy.Resolution{Allow: decision == "allow"}); err != nil {
						t.Fatal(err)
					}
				}
				status := "failed"
				if decision == "allow" {
					status = "succeeded"
				}
				finished := awaitStatus(t, e, r.ID, status)
				if len(finished.Steps) != 1 || finished.Steps[0].Attempt != 1 || finished.Steps[0].Status != status {
					t.Fatalf("authorization was retried or journal is inconsistent: %+v", finished.Steps)
				}
				if decision == "allow" {
					body, err := os.ReadFile(marker)
					if err != nil || string(body) != "executed" {
						t.Fatalf("authorized process did not execute: %q %v", body, err)
					}
				} else {
					assertNoCommandEffect(t, marker)
				}
			})
		}
	}
}

func TestHTTPSharedPolicyDispatch(t *testing.T) {
	for _, decision := range []string{"missing", "rule-deny", "reject", "allow"} {
		t.Run(decision, func(t *testing.T) {
			var effects atomic.Int32
			httpClient, dials := httpTestClient(t, func(w http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != "/effect" {
					t.Errorf("wrong concrete HTTP dispatch: %s %s", request.Method, request.URL)
				}
				effects.Add(1)
				w.WriteHeader(http.StatusCreated)
			})
			var e *Engine
			var shared *policy.Engine
			path := filepath.Join(t.TempDir(), "db")
			if decision == "missing" {
				e = openEngine(t, path, Options{})
			} else {
				e, shared = openPolicyEngine(t, path)
			}
			e.http = *httpClient
			d := saveDef(t, e, Definition{Name: "policy HTTP", AllowedHosts: []string{"public.example"}, AllowHTTP: true, Steps: []Step{
				{ID: "effect", Type: "http.request", Strategy: "key", Retries: 2, BackoffMs: 1, Params: json.RawMessage(`{"method":"POST","url":"http://public.example/effect","body":"create"}`)},
			}})
			if decision == "rule-deny" {
				if _, err := shared.AddRule(context.Background(), policy.Rule{Actor: "automation:" + d.ID, Action: "http.request", Resource: "*", Decision: policy.Deny}); err != nil {
					t.Fatal(err)
				}
			}
			r, err := e.Run(context.Background(), d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if decision == "reject" || decision == "allow" {
				pending := awaitPolicyApproval(t, shared)
				if pending.Actor != "automation:"+d.ID || pending.Resource != "http/POST http://public.example/effect" || pending.Effect != policy.EffectDestructive || !strings.Contains(pending.Detail, "http://public.example/effect") {
					t.Fatalf("wrong HTTP authority: %+v", pending)
				}
				if dials.Load() != 0 || effects.Load() != 0 {
					t.Fatal("HTTP dispatched before authorization")
				}
				if err := shared.Resolve(context.Background(), pending.ID, policy.Resolution{Allow: decision == "allow"}); err != nil {
					t.Fatal(err)
				}
			}
			status, want := "failed", int32(0)
			if decision == "allow" {
				status, want = "succeeded", 1
			}
			finished := awaitStatus(t, e, r.ID, status)
			if effects.Load() != want || dials.Load() != want || finished.Steps[0].Attempt != 1 {
				t.Fatalf("HTTP effect/retry mismatch: effects=%d dials=%d journal=%+v", effects.Load(), dials.Load(), finished.Steps)
			}
		})
	}
}

func TestHTTPRedirectRequiresConcreteAuthorization(t *testing.T) {
	var targetEffects atomic.Int32
	httpClient, dials := httpTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(w, request, "/forbidden", http.StatusFound)
			return
		}
		targetEffects.Add(1)
	})
	e, shared := openPolicyEngine(t, filepath.Join(t.TempDir(), "db"))
	e.http = *httpClient
	if _, err := shared.AddRule(context.Background(), policy.Rule{Actor: "*", Action: "http.request", Resource: "http/GET http://public.example/forbidden", Decision: policy.Deny}); err != nil {
		t.Fatal(err)
	}
	r := runDef(t, e, Definition{Name: "redirect", AllowedHosts: []string{"public.example"}, AllowHTTP: true, Steps: []Step{
		{ID: "effect", Type: "http.request", Params: json.RawMessage(`{"method":"GET","url":"http://public.example/start"}`)},
	}})
	awaitStatus(t, e, r.ID, "failed")
	if dials.Load() != 1 || targetEffects.Load() != 0 {
		t.Fatalf("redirect bypassed policy: dials=%d effects=%d", dials.Load(), targetEffects.Load())
	}
}

func TestAuthorizationDetailFailsClosed(t *testing.T) {
	for _, field := range []string{"long-argument", "invisible-argument", "secret-argument", "secret-environment", "secret-target", "escaped-secret-target"} {
		t.Run(field, func(t *testing.T) {
			var called bool
			e := &Engine{authorize: func(context.Context, policy.Request) error { called = true; return nil }}
			e.RegisterSecret("private-value")
			command := CommandRequest{Executable: "tool", Args: []string{"safe"}, Dir: "work"}
			switch field {
			case "long-argument":
				command.Args = []string{strings.Repeat("x", 2001)}
			case "invisible-argument":
				command.Args = []string{"safe\u202eevil"}
			case "secret-argument":
				command.Args = []string{"private-value"}
			case "secret-environment":
				command.Env = map[string]string{"TOKEN": "opaque-value"}
			}
			var err error
			if field == "secret-target" || field == "escaped-secret-target" {
				target := "https://public.example/private-value"
				if field == "escaped-secret-target" {
					target = "https://public.example/%70rivate-value"
				}
				err = e.authorizeHTTP(context.Background(), "automation:test", "GET", target)
			} else {
				err = e.authorizeCommand(context.Background(), "automation:test", "command.run", command)
			}
			if err == nil || called || strings.Contains(err.Error(), "private-value") || strings.Contains(err.Error(), "opaque-value") {
				t.Fatalf("unsafe approval detail escaped: called=%v error=%v", called, err)
			}
		})
	}
}

func TestPendingPolicyCancellationAndRestart(t *testing.T) {
	for _, mode := range []string{"cancel", "restart"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")
			marker := filepath.Join(t.TempDir(), "effect")
			e, shared := openPolicyEngine(t, path)
			d := policyCommandDefinition(t, "command.run", marker)
			d.Steps[0].Strategy, d.Steps[0].Retries = "at-most-once", 0
			r := runDef(t, e, d)
			awaitPolicyApproval(t, shared)
			detail, err := e.Detail(context.Background(), r.ID)
			if err != nil || len(detail.Steps) != 1 || detail.Steps[0].Status != "authorizing" {
				t.Fatalf("approval must precede dispatch journal: %+v %v", detail.Steps, err)
			}
			if mode == "cancel" {
				if err := e.Cancel(context.Background(), r.ID); err != nil {
					t.Fatal(err)
				}
				e.Close() // Join the worker, not merely the run's early canceled status.
				detail = awaitStatus(t, e, r.ID, "canceled")
				// Shutdown may retain the authorizing row, but the terminal run must
				// prevent it from ever dispatching again on restart.
				if detail.Steps[0].Status != "canceled" && detail.Steps[0].Status != "authorizing" {
					t.Fatalf("unexpected cancellation journal: %+v", detail.Steps)
				}
				assertNoCommandEffect(t, marker)
				e2 := openEngine(t, path, Options{Authorize: trustedTestAuthorizer})
				e2.Close()
				awaitStatus(t, e2, r.ID, "canceled")
				assertNoCommandEffect(t, marker)
				return
			}
			e.Close()
			assertNoCommandEffect(t, marker)
			// Install policy before Start can resume queued work.
			e2 := openEngine(t, path, Options{Authorize: shared.Require})
			pending := awaitPolicyApproval(t, shared)
			if err := shared.Resolve(context.Background(), pending.ID, policy.Resolution{Allow: true}); err != nil {
				t.Fatal(err)
			}
			finished := awaitStatus(t, e2, r.ID, "succeeded")
			if finished.Steps[0].Key != detail.Steps[0].Key {
				t.Fatal("idempotency key changed across pending-approval recovery")
			}
			if body, err := os.ReadFile(marker); err != nil || string(body) != "executed" {
				t.Fatalf("approved resumed command did not execute: %q %v", body, err)
			}
		})
	}
}

func TestHTTPPendingAuthorizationCancellation(t *testing.T) {
	var effects atomic.Int32
	httpClient, dials := httpTestClient(t, func(w http.ResponseWriter, request *http.Request) {
		effects.Add(1)
		w.WriteHeader(http.StatusCreated)
	})
	e, shared := openPolicyEngine(t, filepath.Join(t.TempDir(), "db"))
	e.http = *httpClient
	r := runDef(t, e, Definition{Name: "cancel pending HTTP", AllowedHosts: []string{"public.example"}, AllowHTTP: true, Steps: []Step{
		{ID: "effect", Type: "http.request", Strategy: "key", Retries: 2, Params: json.RawMessage(`{"method":"POST","url":"http://public.example/effect"}`)},
	}})
	awaitPolicyApproval(t, shared)
	if err := e.Cancel(context.Background(), r.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		detail, err := e.Detail(context.Background(), r.ID)
		if err == nil && len(detail.Steps) == 1 && detail.Steps[0].Status == "canceled" {
			if detail.Run.Status != "canceled" || detail.Steps[0].Attempt != 1 || dials.Load() != 0 || effects.Load() != 0 || len(shared.Pending()) != 0 {
				t.Fatalf("cancellation dispatched or retried HTTP: %+v dials=%d effects=%d", detail, dials.Load(), effects.Load())
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("pending HTTP authorization did not finish its canceled journal")
}

func TestAuthorizationCancellationAfterAllowPreventsDispatch(t *testing.T) {
	for _, kind := range []string{"command.run", "http.request"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			e := &Engine{commands: ExecRunner{}, authorize: func(context.Context, policy.Request) error {
				cancel()
				return nil
			}}
			var effects atomic.Int32
			httpClient, dials := httpTestClient(t, func(w http.ResponseWriter, request *http.Request) {
				effects.Add(1)
				w.WriteHeader(http.StatusCreated)
			})
			e.http = *httpClient
			marker := filepath.Join(t.TempDir(), "effect")
			d := policyCommandDefinition(t, "command.run", marker)
			d.AllowHTTP, d.AllowedHosts = true, []string{"public.example"}
			var params map[string]any
			if err := json.Unmarshal(d.Steps[0].Params, &params); err != nil {
				t.Fatal(err)
			}
			if kind == "http.request" {
				params = map[string]any{"method": "POST", "url": "http://public.example/effect"}
			}
			dispatched := false
			_, err := e.invoke(ctx, Run{AutomationID: "cancel-race", Definition: d}, Step{Type: kind}, params, "key", nil, func() error {
				dispatched = true
				return nil
			})
			if !errors.Is(err, context.Canceled) || dispatched || effects.Load() != 0 || dials.Load() != 0 {
				t.Fatalf("authorization cancellation lost: error=%v dispatched=%v effects=%d", err, dispatched, effects.Load())
			}
			assertNoCommandEffect(t, marker)
		})
	}
}
