package agents

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// lookBin resolves bin (a PATH name or an explicit path).
func lookBin(bin string) (string, error) {
	if filepath.IsAbs(bin) || strings.ContainsAny(bin, `/\`) {
		return bin, nil
	}
	p, err := exec.LookPath(bin)
	if err != nil {
		return "", fmt.Errorf("%s is not installed or not on PATH", bin)
	}
	return p, nil
}

// terminalCLI is an interactive agent CLI run in a ConPTY. These CLIs expose no structured
// status feed we can rely on, so the dashboard only knows "running" and "exited" for them
// (Detection.StatusFeed == "process"); questions and approvals are answered in the terminal.
type terminalCLI struct {
	kind, name, bin string
	// args builds the CLI arguments for a model and (already safe) prompt argument.
	args func(model, prompt string) []string
}

func (t terminalCLI) Kind() string  { return t.kind }
func (t terminalCLI) Name() string  { return t.name }
func (terminalCLI) Modes() []Mode   { return []Mode{ModeTerminal} }
func (terminalCLI) CanResume() bool { return false }

func (t terminalCLI) Detect(ctx context.Context) Detection {
	d := detectBinary(ctx, t.kind, t.name, t.bin)
	d.Modes = []Mode{ModeTerminal}
	d.StatusFeed = "process"
	return d
}

func (t terminalCLI) Start(ctx context.Context, h *Host, spec LaunchSpec) (Process, error) {
	if !localMachine(spec.Machine) {
		return nil, errors.New("local adapter cannot launch on a remote machine")
	}
	if spec.Mode != "" && spec.Mode != ModeTerminal {
		return nil, fmt.Errorf("%s only runs in terminal mode", t.name)
	}
	if spec.Resume != "" {
		return nil, fmt.Errorf("%s cannot resume a session by id; restart it from its task", t.name)
	}
	if err := checkToken("model", spec.Model); err != nil {
		return nil, err
	}
	path, err := lookBin(t.bin)
	if err != nil {
		return nil, err
	}
	pa, err := promptArg(h, spec, path)
	if err != nil {
		return nil, err
	}
	args := t.args(spec.Model, pa)
	if err := validateReasoning(t.kind, spec.Reasoning); err != nil {
		return nil, err
	}
	if spec.Reasoning != "" {
		args = append([]string{"-c", "model_reasoning_effort=" + spec.Reasoning}, args...)
	}
	line, err := commandLine(path, args...)
	if err != nil {
		return nil, err
	}
	return startPty(h, spec.SessionID, spec.Dir, line, t.name)
}

// Codex runs `codex [-m model] [prompt]`.
func Codex() Adapter {
	return terminalCLI{kind: "codex", name: "Codex", bin: "codex", args: func(model, prompt string) []string {
		var a []string
		if model != "" {
			a = append(a, "-m", model)
		}
		if prompt != "" {
			a = append(a, "--", prompt)
		}
		return a
	}}
}

// OpenCode runs `opencode [--model m] [--prompt p]`.
func OpenCode() Adapter {
	return terminalCLI{kind: "opencode", name: "OpenCode", bin: "opencode", args: func(model, prompt string) []string {
		var a []string
		if model != "" {
			a = append(a, "--model", model)
		}
		if prompt != "" {
			a = append(a, "--prompt", prompt)
		}
		return a
	}}
}

// Gemini runs `gemini [-m model] [-i prompt]` (-i keeps the session interactive).
func Gemini() Adapter {
	return terminalCLI{kind: "gemini", name: "Gemini CLI", bin: "gemini", args: func(model, prompt string) []string {
		var a []string
		if model != "" {
			a = append(a, "-m", model)
		}
		if prompt != "" {
			a = append(a, "-i", prompt)
		}
		return a
	}}
}

// Custom runs a user-supplied command line under ConPTY. The task prompt is written to a
// file; `{prompt_file}` in the command is replaced with its path (never the prompt text).
type Custom struct{}

func (Custom) Kind() string    { return "custom" }
func (Custom) Name() string    { return "Custom CLI" }
func (Custom) Modes() []Mode   { return []Mode{ModeTerminal} }
func (Custom) CanResume() bool { return false }

func (Custom) Detect(context.Context) Detection {
	return Detection{Kind: "custom", Name: "Custom CLI", Installed: true, Modes: []Mode{ModeTerminal}, StatusFeed: "process"}
}

func (Custom) Start(ctx context.Context, h *Host, spec LaunchSpec) (Process, error) {
	if !localMachine(spec.Machine) {
		return nil, errors.New("custom commands cannot launch through the local adapter on a remote machine")
	}
	cmd := strings.TrimSpace(spec.Command)
	if cmd == "" {
		return nil, errors.New("custom agent needs a command")
	}
	if spec.Resume != "" {
		return nil, errors.New("custom CLIs cannot be resumed; restart from the task")
	}
	if spec.Prompt != "" {
		p, err := writePromptFile(h, spec)
		if err != nil {
			return nil, err
		}
		cmd = strings.ReplaceAll(cmd, "{prompt_file}", escapeArg(p))
	}
	title := cmd
	if f := strings.Fields(cmd); len(f) > 0 {
		title = filepath.Base(f[0])
	}
	return startPty(h, spec.SessionID, spec.Dir, shellWrap(cmd), title)
}
