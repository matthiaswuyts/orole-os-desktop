package machines

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/pkg/sftp"
	"github.com/rayorole/orole-os-desktop/internal/policy"
	"golang.org/x/crypto/ssh"
)

// AgentExecutable is discovered on the pinned machine, never on the daemon host.
type AgentExecutable struct {
	Kind, Path, Version, Help string
}

type AgentDirectory struct {
	Path string
	Git  bool
}

type AgentEnvironment struct {
	Executables []AgentExecutable
	Directories []AgentDirectory
}

func encodedPowerShell(script string) string {
	runes := utf16.Encode([]rune(script))
	b := make([]byte, len(runes)*2)
	for i, v := range runes {
		binary.LittleEndian.PutUint16(b[i*2:], v)
	}
	return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(b)
}

// Only these reviewed probes can run during preview. No frontend command is accepted.
func agentProbe(backend, kind string) (string, error) {
	if kind != "claude" && kind != "codex" {
		return "", ErrInvalid
	}
	switch backend {
	case "posix":
		return "p=$(command -v " + kind + ") || exit 0; case \"$p\" in /*) ;; *) exit 0;; esac; printf '%s\\n' \"$p\"; \"$p\" --version; printf '\\nOROLE_AGENT_HELP\\n'; \"$p\" --help", nil
	case "powershell":
		return encodedPowerShell("$ErrorActionPreference='Stop'; $c=Get-Command " + kind + " -CommandType Application -ErrorAction SilentlyContinue | Select-Object -First 1; if ($null -eq $c) { exit 0 }; $p=$c.Source; [Console]::WriteLine($p); & $p --version; [Console]::WriteLine('OROLE_AGENT_HELP'); & $p --help"), nil
	default:
		return "", ErrInvalid
	}
}

func probeAgent(op *accessOperation, client *ssh.Client, kind string) (AgentExecutable, error) {
	out := AgentExecutable{Kind: kind}
	command, err := agentProbe(op.machine.Backend, kind)
	if err != nil {
		return out, err
	}
	s, err := client.NewSession()
	if err != nil {
		return out, err
	}
	defer s.Close()
	var stdout, stderr limitedOutput
	s.Stdout, s.Stderr = &stdout, &stderr
	unlock, err := op.guard()
	if err != nil {
		return out, err
	}
	err = s.Start(command)
	unlock()
	if err == nil {
		err = s.Wait()
	}
	if err != nil {
		return out, op.result(err)
	}
	if stdout.truncated || stderr.truncated {
		return out, errors.New("agent discovery exceeded its output limit")
	}
	text := strings.ReplaceAll(stdout.b.String(), "\r\n", "\n")
	parts := strings.SplitN(text, "\nOROLE_AGENT_HELP\n", 2)
	if len(parts) != 2 {
		return out, nil
	}
	lines := strings.SplitN(strings.TrimSpace(parts[0]), "\n", 2)
	if len(lines) != 2 || !approvalText(lines[0]) {
		return out, ErrInvalid
	}
	out.Path, out.Version, out.Help = lines[0], strings.TrimSpace(lines[1]), parts[1]
	if _, _, err := prepareCommand(op.machine, Command{Program: out.Path}); err != nil {
		return out, err
	}
	return out, nil
}

// AgentOptions reads only approved directories and fixed CLI help/version probes.
func (m *Manager) AgentOptions(ctx context.Context, actor, id string) (out AgentEnvironment, err error) {
	out.Executables, out.Directories = []AgentExecutable{}, []AgentDirectory{}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	op, err := m.beginAccess(ctx, id)
	if err != nil {
		return out, err
	}
	defer op.finish()
	defer func() { err = op.result(err) }()
	if !op.machine.AllowCommands {
		return out, ErrDenied
	}
	if err = m.authorize(op.ctx, actor, "agent.options", id, policy.EffectRead, "Read approved directory metadata and fixed Claude/Codex executable help on the pinned box"); err != nil {
		return out, err
	}
	client, err := m.connect(op.ctx, op.machine)
	if err != nil {
		return out, err
	}
	defer client.Close()
	for _, kind := range []string{"claude", "codex"} {
		x, e := probeAgent(op, client, kind)
		if e != nil {
			return out, e
		}
		out.Executables = append(out.Executables, x)
	}
	fs, err := sftp.NewClient(client)
	if err != nil {
		return out, err
	}
	defer fs.Close()
	add := func(p string) error {
		checked, e := checkedRemote(fs, op.machine, p, false)
		if e != nil {
			return e
		}
		st, e := fs.Stat(checked)
		if e != nil {
			return e
		}
		if !st.IsDir() {
			return nil
		}
		_, gitErr := fs.Lstat(path.Join(checked, ".git"))
		out.Directories = append(out.Directories, AgentDirectory{Path: checked, Git: gitErr == nil})
		return nil
	}
	for _, root := range op.machine.Roots {
		if err = add(root); err != nil {
			return out, err
		}
		entries, e := fs.ReadDirContext(op.ctx, root)
		if e != nil {
			return out, e
		}
		for _, entry := range entries {
			if len(out.Directories) >= 256 {
				break
			}
			if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
				// Symlinks and paths outside the current root grant are not options.
				_ = add(path.Join(root, entry.Name()))
			}
		}
	}
	unlock, err := op.guard()
	if err != nil {
		return out, err
	}
	unlock()
	return out, nil
}

// AgentProcessRequest is daemon-internal. The prompt travels on this process's stdin
// once, then EOF; it is never typed into an interactive shell or unrelated terminal.
type AgentProcessRequest struct {
	Machine, Kind, Dir, Model, Reasoning, PermissionMode, Prompt string
}

type AgentProcessResult struct {
	Code      int
	Error     string
	Uncertain bool // transport lost: remote descendants may still be running
}

type AgentProcess struct {
	session *ssh.Session
	client  *ssh.Client
	op      *accessOperation
	done    chan AgentProcessResult
	once    sync.Once
}

func (p *AgentProcess) Done() <-chan AgentProcessResult { return p.done }

// Stop revokes the transport and asks the SSH server to terminate the process.
// SSH cannot guarantee termination of detached descendants; completion says so.
func (p *AgentProcess) Stop() error {
	p.once.Do(func() {
		go func() { _ = p.session.Signal(ssh.SIGTERM) }()
		p.op.cancel(context.Canceled)
		_ = p.client.Close()
	})
	return nil
}

func remoteAgentArgs(p AgentProcessRequest, help string) ([]string, error) {
	var args []string
	switch p.Kind {
	case "claude":
		if !strings.Contains(help, "--print") || !strings.Contains(help, "--output-format") {
			return nil, errors.New("remote Claude does not advertise print/text mode")
		}
		args = []string{"--print", "--output-format", "text"}
		if p.PermissionMode != "" && p.PermissionMode != "default" {
			args = append(args, "--permission-mode", p.PermissionMode)
		}
		if p.Reasoning != "" {
			if !strings.Contains(help, "--effort") || !strings.Contains(help, p.Reasoning) {
				return nil, errors.New("remote Claude does not advertise this effort level")
			}
			args = append(args, "--effort", p.Reasoning)
		}
	case "codex":
		if !strings.Contains(help, "exec") || !strings.Contains(help, "--config") {
			return nil, errors.New("remote Codex does not advertise exec/config support")
		}
		// Keep sandboxing; non-interactive execution cannot grant escalation.
		args = []string{"exec", "--sandbox", "workspace-write", "--skip-git-repo-check", "--color", "never"}
		if p.Reasoning != "" {
			args = append(args, "-c", "model_reasoning_effort="+p.Reasoning)
		}
	default:
		return nil, errors.New("this harness has no remote process adapter")
	}
	if p.Model != "" {
		args = append(args, "--model", p.Model)
	}
	if p.Kind == "codex" {
		args = append(args, "-")
	}
	return args, nil
}

func agentCommand(backend, dir, program string, args []string) (string, error) {
	if backend == "posix" {
		command, err := remoteCommand(backend, program, args)
		return "cd " + quotePOSIX(dir) + " && " + command, err
	}
	if backend != "powershell" {
		return "", ErrInvalid
	}
	// Use native argv quoting rather than PowerShell 5's legacy argument binder.
	// Batch shims are refused rather than interpolated through cmd.exe.
	if strings.HasSuffix(strings.ToLower(program), ".cmd") || strings.HasSuffix(strings.ToLower(program), ".bat") {
		return "", errors.New("remote Windows requires a native CLI executable, not a cmd/bat shim")
	}
	words := make([]string, len(args))
	for i, arg := range args {
		words[i] = quoteWindowsArg(arg)
	}
	script := "$ErrorActionPreference='Stop'; $p=New-Object System.Diagnostics.Process; $p.StartInfo.FileName=" + quotePowerShell(program) + "; $p.StartInfo.WorkingDirectory=" + quotePowerShell(strings.TrimPrefix(dir, "/")) + "; $p.StartInfo.Arguments=" + quotePowerShell(strings.Join(words, " ")) + "; $p.StartInfo.UseShellExecute=$false; $p.StartInfo.RedirectStandardInput=$true; [void]$p.Start(); $p.StandardInput.Write([Console]::In.ReadToEnd()); $p.StandardInput.Close(); $p.WaitForExit(); exit $p.ExitCode"
	return encodedPowerShell(script), nil
}

// ErrAgentDispatchUncertain means SSH did not acknowledge whether launch completed.
var ErrAgentDispatchUncertain = errors.New("SSH launch acknowledgement lost; remote process may be running")

// StartAgentProcess retains the access snapshot for the whole process lifetime.
// The request context gates dispatch; owner controls the daemon-owned lifetime.
func (m *Manager) StartAgentProcess(request, owner context.Context, actor string, p AgentProcessRequest, stdout, stderr io.Writer) (out *AgentProcess, err error) {
	if len(p.Prompt) == 0 || len(p.Prompt) > 100<<10 || strings.ContainsRune(p.Prompt, 0) {
		return nil, ErrInvalid
	}
	lifetime, cancel := context.WithTimeout(owner, 24*time.Hour)
	op, err := m.beginAccess(lifetime, p.Machine)
	if err != nil {
		cancel()
		return nil, err
	}
	stopRequest := context.AfterFunc(request, func() { op.cancel(request.Err()) })
	defer stopRequest()
	var client *ssh.Client
	defer func() {
		if err != nil {
			if cause := op.result(nil); cause != nil {
				err = errors.Join(err, cause)
			}
			if client != nil {
				_ = client.Close()
			}
			op.finish()
			cancel()
		}
	}()
	if !op.machine.AllowCommands {
		return nil, ErrDenied
	}
	if err = m.authorize(op.ctx, actor, "agent.options", p.Machine, policy.EffectRead, "Read approved directory metadata and fixed Claude/Codex executable help on the pinned box"); err != nil {
		return nil, err
	}
	// Bound pre-dispatch network work independently from the long process lifetime.
	timer := time.AfterFunc(30*time.Second, func() { op.cancel(context.DeadlineExceeded) })
	defer timer.Stop()
	client, err = m.connect(op.ctx, op.machine)
	if err != nil {
		return nil, err
	}
	executable, err := probeAgent(op, client, p.Kind)
	if err != nil {
		return nil, err
	}
	if executable.Path == "" {
		return nil, errors.New("selected harness is not installed on this box")
	}
	args, err := remoteAgentArgs(p, executable.Help)
	if err != nil {
		return nil, err
	}
	cmd, _, err := prepareCommand(op.machine, Command{Program: executable.Path, Args: args})
	if err != nil {
		return nil, err
	}
	fs, err := sftp.NewClient(client)
	if err != nil {
		return nil, err
	}
	defer fs.Close()
	dir, err := checkedRemote(fs, op.machine, p.Dir, false)
	if err != nil {
		return nil, err
	}
	st, err := fs.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, errors.New("agent working directory is not a directory")
	}
	identity, err := machineIdentity(op.machine)
	if err != nil {
		return nil, err
	}
	detail, err := approvalDetail(struct {
		Target       approvalIdentity `json:"target"`
		Dir          string           `json:"dir"`
		Program      string           `json:"program"`
		Args         []string         `json:"args"`
		PromptSHA256 string           `json:"promptSHA256"`
		PromptBytes  int              `json:"promptBytes"`
		Scope        string           `json:"scope"`
	}{identity, dir, cmd.Program, cmd.Args, fmt.Sprintf("%x", sha256.Sum256([]byte(p.Prompt))), len(p.Prompt), "Full remote account permissions; approved roots select cwd, not a sandbox. SSH disconnect cannot guarantee termination of remote children."})
	if err != nil {
		return nil, err
	}
	// Human approval may take longer than the network preparation deadline.
	timer.Stop()
	if err = m.authorize(op.ctx, actor, "agent.start", p.Machine, policy.EffectDestructive, detail); err != nil {
		return nil, err
	}
	timer.Reset(30 * time.Second)
	command, err := agentCommand(op.machine.Backend, dir, cmd.Program, cmd.Args)
	if err != nil {
		return nil, err
	}
	s, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	s.Stdin = strings.NewReader(p.Prompt)
	s.Stdout, s.Stderr = stdout, stderr
	// Recheck directory after consent, then serialize dispatch with grant changes.
	if _, err = checkedRemote(fs, op.machine, dir, false); err != nil {
		_ = s.Close()
		return nil, err
	}
	unlock, err := op.guard()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	if err = request.Err(); err == nil {
		if startErr := s.Start(command); startErr != nil {
			err = fmt.Errorf("%w: %v", ErrAgentDispatchUncertain, startErr)
		}
	}
	unlock()
	if err != nil {
		_ = s.Close()
		return nil, err
	}
	stopRequest()
	timer.Stop()
	out = &AgentProcess{session: s, client: client, op: op, done: make(chan AgentProcessResult, 1)}
	go func(p *AgentProcess) {
		defer close(p.done)
		defer cancel()
		defer op.finish()
		defer client.Close()
		defer s.Close()
		waitErr := s.Wait()
		result := AgentProcessResult{Code: 0}
		var exit *ssh.ExitError
		switch {
		case context.Cause(op.ctx) != nil:
			result.Code, result.Uncertain, result.Error = -1, true, "SSH process control ended: "+context.Cause(op.ctx).Error()+". Remote process termination is unconfirmed; check the box before restarting."
		case errors.As(waitErr, &exit):
			result.Code, result.Error = exit.ExitStatus(), waitErr.Error()
		case waitErr != nil:
			result.Code, result.Uncertain, result.Error = -1, true, "SSH connection lost; remote process termination is unconfirmed: "+waitErr.Error()
		}
		p.done <- result
	}(out)
	return out, nil
}
