package automations

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type commandFunc func(context.Context, CommandRequest) (CommandResult, error)

func (f commandFunc) RunCommand(ctx context.Context, r CommandRequest) (CommandResult, error) {
	return f(ctx, r)
}
func TestCommandArgvAndTestExitSemantics(t *testing.T) {
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Skip("go executable unavailable")
	}
	dir := t.TempDir()
	arg := "one argument; $(do-not-execute) & two"
	var got CommandRequest
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{Authorize: trustedTestAuthorizer, Commands: commandFunc(func(_ context.Context, r CommandRequest) (CommandResult, error) {
		got = r
		return CommandResult{ExitCode: 7, Stdout: "failed tests"}, nil
	})})
	params, _ := json.Marshal(map[string]any{"executable": goExe, "dir": dir, "args": []string{arg}})
	r := runDef(t, e, Definition{Name: "test semantics", AllowedExecutables: []string{goExe}, Steps: []Step{{ID: "test", Type: "test.run", Params: params}}})
	d := awaitStatus(t, e, r.ID, "failed")
	if len(got.Args) != 1 || got.Args[0] != arg || got.Dir != dir {
		t.Fatalf("argv changed: %+v", got)
	}
	if !strings.Contains(string(d.Steps[0].Output), `"passed":false`) {
		t.Fatal(string(d.Steps[0].Output))
	}
}
func TestExecRunnerRealGoVersion(t *testing.T) {
	goExe, err := exec.LookPath("go")
	if err != nil {
		t.Skip(err)
	}
	r, err := (ExecRunner{}).RunCommand(context.Background(), CommandRequest{Executable: goExe, Args: []string{"version"}, Dir: t.TempDir()})
	if err != nil || !r.Passed || !strings.HasPrefix(r.Stdout, "go version ") {
		t.Fatalf("%+v %v", r, err)
	}
	t.Log(strings.TrimSpace(r.Stdout))
}
func TestSaveRejectsRecognizedSecret(t *testing.T) {
	e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
	e.RegisterSecret("known-secret-value")
	s := toolStep("tool")
	s.Params = json.RawMessage(`{"server":"fake","tool":"test","args":{"value":"known-secret-value"}}`)
	r, err := e.Save(context.Background(), Definition{Name: "secret", Steps: []Step{s}})
	if err != nil || r.Saved || len(r.Issues) == 0 {
		t.Fatalf("secret persisted %+v %v", r, err)
	}
	list, err := e.List(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatal("secret-bearing definition saved")
	}
}
func TestRunTimeout(t *testing.T) {
	t.Run("deadline", func(t *testing.T) {
		e := openEngine(t, filepath.Join(t.TempDir(), "db"), Options{})
		e.SetToolInvoker(toolFunc(func(ctx context.Context, _ ToolRequest) (any, error) { <-ctx.Done(); return nil, ctx.Err() }))
		r := runDef(t, e, Definition{Name: "run timeout", TimeoutSec: 1, Steps: []Step{toolStep("wait")}})
		awaitStatus(t, e, r.ID, "failed")
	})
}
