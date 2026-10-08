// Package git is a thin, safe wrapper over the `git` binary found on PATH.
//
// Every call runs git directly (no shell) with a timeout, a sanitized environment
// (allow-list; prompts disabled; repo-controlled command hooks such as fsmonitor and
// external diff drivers turned off) and validated arguments. Refs and branch names are
// validated before use and positional arguments are always preceded by `--`, so
// untrusted names can never be parsed by git as options.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"
)

// Timeouts for operations; a caller context with an earlier deadline wins.
const (
	DefaultTimeout = 30 * time.Second
	CloneTimeout   = 10 * time.Minute
	maxOutput      = 16 << 20
)

// ErrInvalidRef is returned for ref or branch names that fail validation.
var ErrInvalidRef = errors.New("git: invalid ref name")

// ErrNotRepo is returned when a directory is not inside a git work tree.
var ErrNotRepo = errors.New("git: not a git repository")

// Error is a failed git invocation.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", e.Args[0], msg)
}
func (e *Error) Unwrap() error { return e.Err }

var envAllow = map[string]bool{
	"PATH": true, "SYSTEMROOT": true, "SYSTEMDRIVE": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true,
	"USERPROFILE": true, "HOME": true, "HOMEDRIVE": true, "HOMEPATH": true, "APPDATA": true, "LOCALAPPDATA": true,
	"PROGRAMDATA": true, "PROGRAMFILES": true, "PROGRAMFILES(X86)": true, "PROGRAMW6432": true,
	"TEMP": true, "TMP": true, "TMPDIR": true, "USERNAME": true, "USERDOMAIN": true,
	"SSH_AUTH_SOCK": true, "GIT_SSH": true, "GIT_SSH_COMMAND": true, "HTTPS_PROXY": true, "HTTP_PROXY": true, "NO_PROXY": true,
}

func sanitizedEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if envAllow[strings.ToUpper(k)] {
			env = append(env, kv)
		}
	}
	return append(env,
		"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_PAGER=cat", "GIT_EDITOR=true",
		"GCM_INTERACTIVE=never", "LC_ALL=C", "LANG=C",
		"GIT_ALLOW_PROTOCOL=https:ssh:git:file",
	)
}

// safeConfig neutralizes repository-controlled command execution.
var safeConfig = []string{"--no-pager", "--no-replace-objects", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=" + os.DevNull, "-c", "core.quotepath=false", "-c", "core.pager=cat", "-c", "protocol.ext.allow=never", "-c", "maintenance.auto=false", "-c", "gc.auto=0"}

type limitBuffer struct {
	buf       bytes.Buffer
	max       int
	truncated bool
	cancel    context.CancelFunc
}

func (l *limitBuffer) Write(p []byte) (int, error) {
	if l.buf.Len()+len(p) > l.max {
		l.truncated = true
		room := l.max - l.buf.Len()
		if room > 0 {
			l.buf.Write(p[:room])
		}
		l.cancel()
		return len(p), nil
	}
	return l.buf.Write(p)
}

// run executes git in dir (empty = current directory) and returns stdout.
// truncated reports that output exceeded the cap and the process was stopped.
func run(ctx context.Context, timeout time.Duration, dir string, args ...string) (out []byte, truncated bool, err error) {
	return runInput(ctx, timeout, dir, nil, nil, args...)
}

func runInput(ctx context.Context, timeout time.Duration, dir string, env []string, stdin io.Reader, args ...string) (out []byte, truncated bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append(append([]string{}, safeConfig...), args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(sanitizedEnv(), env...)
	cmd.Stdin = stdin
	stdout := &limitBuffer{max: maxOutput, cancel: cancel}
	var stderr limitBuffer
	stderr.max, stderr.cancel = 1<<20, func() {}
	cmd.Stdout, cmd.Stderr = stdout, &stderr
	hideWindow(cmd)
	runErr := cmd.Run()
	if stdout.truncated {
		return stdout.buf.Bytes(), true, nil
	}
	if runErr != nil {
		if errors.Is(runErr, exec.ErrNotFound) {
			return nil, false, fmt.Errorf("git executable not found on PATH: %w", runErr)
		}
		if ctx.Err() != nil {
			return nil, false, fmt.Errorf("git %s: %w", args[0], ctx.Err())
		}
		return nil, false, &Error{Args: args, Stderr: stderr.buf.String(), Err: runErr}
	}
	return stdout.buf.Bytes(), false, nil
}

func runDefault(ctx context.Context, dir string, args ...string) ([]byte, error) {
	out, _, err := run(ctx, DefaultTimeout, dir, args...)
	return out, err
}

// ValidRef reports whether s is acceptable as a branch, tag or revision argument.
// It is stricter than git: it rejects anything starting with "-", control characters,
// whitespace and revision-expression syntax other than plain names, SHAs and HEAD.
func ValidRef(s string) bool {
	if s == "" || len(s) > 255 || s[0] == '-' || s[0] == '/' || s[0] == '.' {
		return false
	}
	if strings.HasSuffix(s, "/") || strings.HasSuffix(s, ".") || strings.HasSuffix(s, ".lock") || s == "@" {
		return false
	}
	for _, bad := range []string{"..", "//", "@{", "/.", ".lock/"} {
		if strings.Contains(s, bad) {
			return false
		}
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) || strings.ContainsRune("~^:?*[\\", r) {
			return false
		}
	}
	return true
}

// ValidBranchName is ValidRef plus: not HEAD and double-checked by `git check-ref-format --branch`.
func ValidBranchName(ctx context.Context, name string) error {
	if !ValidRef(name) || name == "HEAD" {
		return fmt.Errorf("%w: %q", ErrInvalidRef, name)
	}
	if _, err := runDefault(ctx, "", "check-ref-format", "--branch", name); err != nil {
		return fmt.Errorf("%w: %q", ErrInvalidRef, name)
	}
	return nil
}

// Toplevel returns the root of the work tree containing dir.
func Toplevel(ctx context.Context, dir string) (string, error) {
	out, err := runDefault(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		var ge *Error
		if errors.As(err, &ge) && strings.Contains(ge.Stderr, "not a git repository") {
			return "", ErrNotRepo
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// ResolveCommit returns the full commit id for ref, or an error if it does not resolve.
func ResolveCommit(ctx context.Context, dir, ref string) (string, error) {
	if !ValidRev(ref) {
		return "", fmt.Errorf("%w: %q", ErrInvalidRef, ref)
	}
	out, err := runDefault(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("ref %q does not resolve to a commit", ref)
	}
	return strings.TrimSpace(string(out)), nil
}

// RemoteURL returns the URL of the "origin" remote (credentials stripped), or "" if none.
func RemoteURL(ctx context.Context, dir string) string {
	out, err := runDefault(ctx, dir, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return stripCredentials(strings.TrimSpace(string(out)))
}

func stripCredentials(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return u
	}
	rest := u[i+3:]
	if at := strings.LastIndex(strings.SplitN(rest, "/", 2)[0], "@"); at >= 0 {
		return u[:i+3] + rest[at+1:]
	}
	return u
}

// ValidRev is ValidRef plus optional trailing ancestry suffixes (~N, ^, ^N), e.g. HEAD~2.
// Used for revision arguments to read-only commands; branch names use ValidRef.
func ValidRev(s string) bool {
	base := s
	for {
		i := strings.LastIndexAny(base, "~^")
		if i < 0 {
			break
		}
		digits := base[i+1:]
		if strings.Trim(digits, "0123456789") != "" {
			return false
		}
		base = base[:i]
	}
	return ValidRef(base)
}
