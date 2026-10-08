package agents_test

import (
	"context"
	"testing"

	"github.com/rayorole/orole-os-desktop/internal/agents"
)

type gatewayCapture struct{ specs []agents.LaunchSpec }

func (*gatewayCapture) Kind() string { return "claude" }
func (*gatewayCapture) Name() string { return "Capture" }
func (*gatewayCapture) Modes() []agents.Mode {
	return []agents.Mode{agents.ModeStructured, agents.ModeTerminal}
}
func (*gatewayCapture) CanResume() bool { return false }
func (*gatewayCapture) Detect(context.Context) agents.Detection {
	return agents.Detection{Kind: "claude", Name: "Capture", Modes: []agents.Mode{agents.ModeStructured, agents.ModeTerminal}}
}
func (a *gatewayCapture) Start(_ context.Context, _ *agents.Host, spec agents.LaunchSpec) (agents.Process, error) {
	a.specs = append(a.specs, spec)
	ch := make(chan agents.Event, 1)
	ch <- agents.Event{Kind: agents.EvExit, Data: []byte(`{"code":0}`)}
	close(ch)
	return captureProcess{ch}, nil
}

func TestStructuredLaunchGetsGatewayAndSkillsPrompt(t *testing.T) {
	cases := []struct {
		name        string
		mode        agents.Mode
		gatewayExe  string
		wantGateway bool
	}{
		{"structured local", agents.ModeStructured, `C:\Orole\orole-daemon.exe`, true},
		{"terminal gets none", agents.ModeTerminal, `C:\Orole\orole-daemon.exe`, false},
		{"no gateway exe", agents.ModeStructured, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			a := &gatewayCapture{}
			m := agents.NewManager(agents.Config{
				Store: e.st, Adapters: []agents.Adapter{a},
				Host:         &agents.Host{DataDir: `C:\data\orole`, GatewayExe: c.gatewayExe},
				SkillsPrompt: func() string { return "## Orole skills\n- x: y\n" },
				Authorize:    func(context.Context, string, string, string) error { return nil },
			})
			t.Cleanup(m.Shutdown)
			s, err := m.Start(ctx10(t), agents.StartSpec{Kind: "claude", Dir: t.TempDir(), Mode: c.mode})
			if err != nil {
				t.Fatal(err)
			}
			if len(a.specs) != 1 {
				t.Fatalf("launches = %d", len(a.specs))
			}
			spec := a.specs[0]
			if !c.wantGateway {
				if spec.Gateway != nil || spec.SkillsPrompt != "" {
					t.Fatalf("unexpected gateway: %+v / %q", spec.Gateway, spec.SkillsPrompt)
				}
				return
			}
			g := spec.Gateway
			if g == nil || g.Command != c.gatewayExe || len(g.Args) != 1 || g.Args[0] != "mcp-gateway" ||
				g.Env["OROLE_HOME"] != `C:\data\orole` || g.Env["OROLE_GATEWAY_SESSION"] != s.ID || len(g.Env) != 2 {
				t.Fatalf("gateway = %+v (session %s)", g, s.ID)
			}
			if spec.SkillsPrompt == "" {
				t.Fatal("skills prompt missing")
			}
		})
	}
}
