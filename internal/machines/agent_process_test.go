package machines

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestRemoteAgentArgumentsUseSelectedModelAndReasoning(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		p := AgentProcessRequest{Kind: kind, Model: "selected/model", Reasoning: "high", Prompt: "private task\n$(never a shell argument)"}
		args, err := remoteAgentArgs(p, "--print --output-format --effort low medium high exec --config")
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(args, "selected/model") {
			t.Fatalf("model missing: %v", args)
		}
		if kind == "claude" && !slices.Contains(args, "--effort") {
			t.Fatalf("Claude effort missing: %v", args)
		}
		if kind == "codex" && !slices.Contains(args, "model_reasoning_effort=high") {
			t.Fatalf("Codex effort missing: %v", args)
		}
		if strings.Contains(strings.Join(args, " "), p.Prompt) {
			t.Fatal("task leaked into remote command line")
		}
	}
	if _, err := remoteAgentArgs(AgentProcessRequest{Kind: "custom"}, "anything"); err == nil {
		t.Fatal("arbitrary remote command adapter accepted")
	}
	if _, err := remoteAgentArgs(AgentProcessRequest{Kind: "claude", Reasoning: "high"}, "--print --output-format"); err == nil {
		t.Fatal("unadvertised effort accepted")
	}
}

func TestRemoteAgentCommandQuotesWorkingDirectoryAndNativeArgv(t *testing.T) {
	dir := "/approved/a'b; $(touch marker)"
	command, err := agentCommand("posix", dir, "/usr/bin/claude", []string{"--model", "model'; bad"})
	if err != nil {
		t.Fatal(err)
	}
	if command != "cd "+quotePOSIX(dir)+" && exec '/usr/bin/claude' '--model' "+quotePOSIX("model'; bad") {
		t.Fatalf("unsafe POSIX command: %s", command)
	}
	if _, err := agentCommand("powershell", "/C:/approved", "C:\\bin\\codex.cmd", nil); err == nil {
		t.Fatal("Windows batch shim accepted")
	}
	command, err = agentCommand("powershell", "/C:/approved/a'b", "C:\\bin\\claude.exe", []string{"--model", `a\"b; $(bad)`})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(command, "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand "))
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	script := string(utf16.Decode(u))
	if !strings.Contains(script, "$p.StartInfo.WorkingDirectory='C:/approved/a''b'") || !strings.Contains(script, "$p.StartInfo.Arguments="+quotePowerShell(quoteWindowsArg("--model")+" "+quoteWindowsArg(`a\"b; $(bad)`))) || !strings.Contains(script, "RedirectStandardInput=$true") {
		t.Fatalf("unsafe native launcher: %s", script)
	}
}

func TestAgentDiscoveryOnlyAcceptsFixedHarnessProbes(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		for _, backend := range []string{"posix", "powershell"} {
			if command, err := agentProbe(backend, kind); err != nil || command == "" {
				t.Fatalf("fixed probe missing: %v", err)
			}
		}
	}
	for _, kind := range []string{"custom", "claude; touch marker", "codex\nrm"} {
		if _, err := agentProbe("posix", kind); !errors.Is(err, ErrInvalid) {
			t.Fatalf("frontend command escaped probe allowlist: %q %v", kind, err)
		}
	}
}

func TestAgentStartRevocationCancelsPendingDiscovery(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local", AllowCommands: true}); err != nil {
		t.Fatal(err)
	}
	gate := newApprovalGate(t, "machines.agent.options", true)
	m.policy = gate
	done := make(chan error, 1)
	go func() {
		_, err := m.StartAgentProcess(ctx, ctx, "user", AgentProcessRequest{Machine: "local", Kind: "claude", Dir: "/approved", Prompt: "task"}, io.Discard, io.Discard)
		done <- err
	}()
	receive(t, gate.entered)
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local"}); err != nil {
		t.Fatal(err)
	}
	receive(t, gate.canceled)
	gate.approve()
	if err := receive(t, done); !errors.Is(err, ErrAccessChanged) {
		t.Fatalf("stale operation not revoked: %v", err)
	}
}

func TestRemoteAgentAPINeverExecutesNativeFallback(t *testing.T) {
	m, _ := testManager(t)
	ctx := context.Background()
	if _, err := m.SetAccess(ctx, "user", Access{ID: "local", AllowCommands: true}); err != nil {
		t.Fatal(err)
	}
	_, err := m.StartAgentProcess(ctx, ctx, "user", AgentProcessRequest{Machine: "local", Kind: "claude", Dir: t.TempDir(), Prompt: "task"}, io.Discard, io.Discard)
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("remote API used native fallback: %v", err)
	}
}
