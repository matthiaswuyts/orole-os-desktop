package machines

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/rayorole/orole-os-desktop/internal/policy"
	"golang.org/x/crypto/ssh"
)

type Command struct {
	ID             string   `json:"id"`
	OperationID    string   `json:"operationId"`
	Program        string   `json:"program"`
	Args           []string `json:"args"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
}
type CommandResult struct {
	Stdout    string `json:"stdout"`
	Stderr    string `json:"stderr"`
	ExitCode  int    `json:"exitCode"`
	Truncated bool   `json:"truncated"`
}
type limitedOutput struct {
	mu        sync.Mutex
	b         bytes.Buffer
	truncated bool
}

func (b *limitedOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	left := (512 << 10) - b.b.Len()
	if len(p) > left {
		p = p[:left]
		b.truncated = true
	}
	_, _ = b.b.Write(p)
	return n, nil
}
func quotePOSIX(s string) string      { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func quotePowerShell(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// quoteWindowsArg implements CommandLineToArgvW/MS C-runtime quoting. PowerShell
// 5's legacy native-argument binder is deliberately not involved.
func quoteWindowsArg(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range s {
		if r == '\\' {
			slashes++
			continue
		}
		if r == '"' {
			b.WriteString(strings.Repeat("\\", slashes*2+1))
			b.WriteRune(r)
		} else {
			b.WriteString(strings.Repeat("\\", slashes))
			b.WriteRune(r)
		}
		slashes = 0
	}
	b.WriteString(strings.Repeat("\\", slashes*2))
	b.WriteByte('"')
	return b.String()
}
func remoteCommand(backend, program string, args []string) (string, error) {
	if program == "" || strings.HasPrefix(program, "-") || strings.ContainsRune(program, 0) {
		return "", ErrInvalid
	}
	for _, arg := range args {
		if strings.ContainsRune(arg, 0) {
			return "", ErrInvalid
		}
	}
	switch backend {
	case "posix":
		words := []string{quotePOSIX(program)}
		for _, a := range args {
			words = append(words, quotePOSIX(a))
		}
		return "exec " + strings.Join(words, " "), nil
	case "powershell":
		words := make([]string, len(args))
		for i, a := range args {
			words[i] = quoteWindowsArg(a)
		}
		script := "$ErrorActionPreference = 'Stop'; $p = New-Object System.Diagnostics.Process; $p.StartInfo.FileName = " + quotePowerShell(program) + "; $p.StartInfo.Arguments = " + quotePowerShell(strings.Join(words, " ")) + "; $p.StartInfo.UseShellExecute = $false; [void]$p.Start(); $p.WaitForExit(); exit $p.ExitCode"
		runes := utf16.Encode([]rune(script))
		encoded := make([]byte, len(runes)*2)
		for i, v := range runes {
			binary.LittleEndian.PutUint16(encoded[i*2:], v)
		}
		return "powershell.exe -NoLogo -NoProfile -NonInteractive -EncodedCommand " + base64.StdEncoding.EncodeToString(encoded), nil
	default:
		return "", ErrInvalid
	}
}
func prepareCommand(v Machine, p Command) (Command, os.FileInfo, error) {
	if p.Program == "" || !approvalText(p.Program) || len(p.Args) > 256 {
		return p, nil, ErrInvalid
	}
	total := len(p.Program)
	for _, arg := range p.Args {
		total += len(arg)
		if !approvalText(arg) || total > maxApprovalDetail {
			return p, nil, ErrInvalid
		}
	}
	p.Args = slices.Clone(p.Args)
	if p.Args == nil {
		p.Args = []string{}
	}
	if v.Transport == "native" && v.ID == "local" {
		program, err := exec.LookPath(p.Program)
		if err != nil {
			return p, nil, fmt.Errorf("%w: executable is unavailable", ErrInvalid)
		}
		program, err = filepath.Abs(program)
		if err != nil {
			return p, nil, err
		}
		program, err = filepath.EvalSymlinks(program)
		if err != nil {
			return p, nil, err
		}
		info, err := os.Stat(program)
		if err != nil || !info.Mode().IsRegular() {
			return p, nil, fmt.Errorf("%w: executable must be a regular file", ErrInvalid)
		}
		p.Program = program
		return p, info, nil
	}
	// Remote PATH lookup cannot bind approval to an exact executable. Require
	// an absolute path; the pinned remote account still controls its contents.
	switch v.Backend {
	case "posix":
		if !path.IsAbs(p.Program) || path.Clean(p.Program) != p.Program {
			return p, nil, fmt.Errorf("%w: remote executable must be an absolute clean path", ErrInvalid)
		}
	case "powershell":
		if len(p.Program) < 3 || p.Program[1] != ':' || (p.Program[2] != '/' && p.Program[2] != '\\') {
			return p, nil, fmt.Errorf("%w: remote executable must be an absolute drive path", ErrInvalid)
		}
		if clean := strings.ReplaceAll(p.Program, "\\", "/"); path.Clean(clean) != clean {
			return p, nil, ErrInvalid
		}
	default:
		return p, nil, ErrInvalid
	}
	return p, nil, nil
}

func commandDetail(v Machine, p Command) (string, error) {
	identity, err := machineIdentity(v)
	if err != nil {
		return "", err
	}
	if !approvalText(p.Program) {
		return "", ErrInvalid
	}
	return approvalDetail(struct {
		Target  approvalIdentity `json:"target"`
		Program string           `json:"program"`
		Args    []string         `json:"args"`
		Scope   string           `json:"scope"`
	}{identity, p.Program, p.Args, "Full account OS permissions; file roots do not sandbox execution"})
}

func (m *Manager) Execute(ctx context.Context, actor string, p Command) (result CommandResult, err error) {
	op, err := m.beginAccess(ctx, p.ID)
	if err != nil {
		return result, err
	}
	defer op.finish()
	defer func() { err = op.result(err) }()
	v := op.machine
	if !v.AllowCommands {
		return result, ErrDenied
	}
	p, executable, err := prepareCommand(v, p)
	if err != nil {
		return result, err
	}
	detail, err := commandDetail(v, p)
	if err != nil {
		return result, err
	}
	ctx, finish, err := m.track(op.ctx, p.OperationID)
	if err != nil {
		return result, err
	}
	defer finish()
	if err = m.authorize(ctx, actor, "execute", v.ID, policy.EffectDestructive, detail); err != nil {
		return result, err
	}
	if err = ctx.Err(); err != nil {
		return result, err
	}
	ctx, cancel := operationContext(ctx, p.TimeoutSeconds)
	defer cancel()
	var stdout, stderr limitedOutput
	if v.Transport == "native" && v.ID == "local" {
		cmd := exec.CommandContext(ctx, p.Program, p.Args...)
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		cmd.WaitDelay = 2 * time.Second
		unlock, e := op.guard()
		if e != nil {
			return result, e
		}
		checked, e := filepath.EvalSymlinks(p.Program)
		current, statErr := os.Stat(p.Program)
		if e != nil || statErr != nil || checked != p.Program || !os.SameFile(executable, current) {
			unlock()
			return result, ErrAccessChanged
		}
		err = cmd.Start()
		unlock()
		if err == nil {
			err = cmd.Wait()
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
			err = nil
		}
	} else {
		command, e := remoteCommand(v.Backend, p.Program, p.Args)
		if e != nil {
			return result, e
		}
		client, e := m.connect(ctx, v)
		if e != nil {
			return result, e
		}
		defer client.Close()
		session, e := client.NewSession()
		if e != nil {
			return result, e
		}
		defer session.Close()
		session.Stdout = &stdout
		session.Stderr = &stderr
		unlock, e := op.guard()
		if e != nil {
			return result, e
		}
		if err = ctx.Err(); err == nil {
			err = session.Start(command)
		}
		unlock()
		if err == nil {
			err = session.Wait()
		}
		var exit *ssh.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitStatus()
			err = nil
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	result.Stdout = stdout.b.String()
	result.Stderr = stderr.b.String()
	result.Truncated = stdout.truncated || stderr.truncated
	return result, err
}

type MachineHealth struct {
	ID                   string `json:"id"`
	Reachable            bool   `json:"reachable"`
	CheckedAt            int64  `json:"checkedAt"`
	LatencyMS            int64  `json:"latencyMs"`
	Hostname             string `json:"hostname"`
	OS                   string `json:"os"`
	Architecture         string `json:"architecture"`
	LogicalCPUs          int    `json:"logicalCpus"`
	DaemonUptimeSeconds  int64  `json:"daemonUptimeSeconds"`
	DaemonHeapBytes      uint64 `json:"daemonHeapBytes"`
	Detail               string `json:"detail"`
	MemoryTotalBytes     uint64 `json:"memoryTotalBytes"`
	MemoryAvailableBytes uint64 `json:"memoryAvailableBytes"`
	MemoryReady          bool   `json:"memoryReady"`
	MetricError          string `json:"metricError"`
}

func (m *Manager) Health(ctx context.Context, actor, id string) (h MachineHealth, err error) {
	ctx, cancel := operationContext(ctx, 10)
	defer cancel()
	op, err := m.beginAccess(ctx, id)
	if err != nil {
		return h, err
	}
	defer op.finish()
	defer func() { err = op.result(err) }()
	v := op.machine
	// Background probes must never open interactive approval requests.
	reader, ok := m.policy.(interface {
		Decide(context.Context, policy.Request) (policy.Evaluation, error)
	})
	if !ok {
		return h, ErrDenied
	}
	decision, err := reader.Decide(op.ctx, policy.Request{Actor: actor, Action: "machines.health", Resource: "machine/" + id, Effect: policy.EffectRead, Detail: "Read host identity, connectivity and physical memory"})
	if err != nil {
		return h, err
	}
	if decision.Decision != policy.Allow {
		return h, ErrDenied
	}
	start := time.Now()
	h = MachineHealth{ID: id, OS: v.OS, CheckedAt: start.UnixMilli()}
	if id == "local" {
		var mem runtime.MemStats
		runtime.ReadMemStats(&mem)
		h.Hostname, err = os.Hostname()
		if err != nil {
			return h, err
		}
		h.Architecture = runtime.GOARCH
		h.LogicalCPUs = runtime.NumCPU()
		h.DaemonHeapBytes = mem.HeapAlloc
		h.DaemonUptimeSeconds = int64(time.Since(m.started).Seconds())
		h.Detail = "Native host available. Memory describes physical host memory; daemon uptime and heap are separate process metrics."
		if m.hostMetrics == nil {
			h.MetricError = "Host memory sampler is unavailable"
		} else if metrics, sampleErr := m.hostMetrics.Snapshot(op.ctx); sampleErr != nil {
			h.MetricError = sampleErr.Error()
		} else {
			h.MemoryTotalBytes = metrics.MemoryTotalBytes
			h.MemoryAvailableBytes = metrics.MemoryAvailableBytes
			h.MemoryReady = h.MemoryTotalBytes > 0 && h.MemoryAvailableBytes <= h.MemoryTotalBytes
			if !h.MemoryReady {
				h.MetricError = "Native sampler returned invalid physical memory counters"
			}
		}
	} else {
		client, connectErr := m.connect(op.ctx, v)
		if connectErr != nil {
			if isHostOffline(connectErr) {
				h.Detail = "Host unreachable: " + connectErr.Error()
				h.LatencyMS = time.Since(start).Milliseconds()
				return h, nil
			}
			return h, fmt.Errorf("machine health: %w", connectErr)
		}
		defer client.Close()
		h.Detail = "Pinned SSH identity and key authentication verified. Host memory is collected by a fixed read-only probe."
		if sampleErr := readRemoteHostMemory(op, client, &h); sampleErr != nil {
			h.MetricError = sampleErr.Error()
		}
	}
	unlock, err := op.guard()
	if err != nil {
		return MachineHealth{}, err
	}
	unlock()
	h.Reachable = true
	h.LatencyMS = time.Since(start).Milliseconds()
	return h, nil
}
