package automations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ExecRunner executes argv directly, never a shell command string. Only a small
// environment allowlist is inherited; user-specified values are explicit inputs.
type ExecRunner struct{}
type cappedBuffer struct {
	mu        sync.Mutex
	b         []byte
	truncated bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	room := (64 << 10) - len(b.b)
	if len(p) > room {
		p = p[:room]
		b.truncated = true
	}
	b.b = append(b.b, p...)
	return n, nil
}
func (ExecRunner) RunCommand(ctx context.Context, r CommandRequest) (CommandResult, error) {
	if !filepath.IsAbs(r.Dir) {
		return CommandResult{}, fmt.Errorf("explicit absolute working directory required")
	}
	cmd := exec.CommandContext(ctx, r.Executable, r.Args...)
	cmd.Dir = r.Dir
	cmd.WaitDelay = 2 * time.Second
	allowed := map[string]bool{"PATH": true, "PATHEXT": true, "SYSTEMROOT": true, "WINDIR": true, "TEMP": true, "TMP": true, "HOME": true, "USERPROFILE": true, "LOCALAPPDATA": true, "APPDATA": true, "GOPATH": true, "GOROOT": true, "GOCACHE": true, "GOMODCACHE": true, "LANG": true, "TZ": true}
	env := map[string]string{}
	for _, entry := range os.Environ() {
		k, v, ok := strings.Cut(entry, "=")
		if ok && allowed[strings.ToUpper(k)] {
			env[k] = v
		}
	}
	for k, v := range r.Env {
		if k == "" || strings.ContainsAny(k, "=\x00") || strings.ContainsRune(v, 0) {
			return CommandResult{}, fmt.Errorf("invalid environment entry")
		}
		env[k] = v
	}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var out, stderr cappedBuffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := CommandResult{Stdout: string(out.b), Stderr: string(stderr.b), Truncated: out.truncated || stderr.truncated, Passed: err == nil}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			result.ExitCode = exit.ExitCode()
		} else {
			result.ExitCode = -1
		}
	}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	return result, err
}
func resolveCommand(executable string, allow []string) (string, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".cmd" || ext == ".bat" {
		return "", fmt.Errorf("batch scripts are not supported; choose an executable")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	for _, item := range allow {
		p, err := exec.LookPath(item)
		if err != nil {
			continue
		}
		other, err := os.Stat(p)
		if err == nil && os.SameFile(info, other) {
			return path, nil
		}
	}
	return "", fmt.Errorf("executable %q is not allowlisted", executable)
}
