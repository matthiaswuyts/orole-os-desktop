// Package engine runs speech recognition in a helper process: the daemon executable started as
// `orole-daemon voice-engine` (Main), which loads sherpa-onnx's C API DLL without cgo. A native fault in the
// engine ends the helper, never the daemon, and unloading a model is simply ending the helper.
//
// Process is the daemon's side. It starts the helper on first use, sends it one request at a time, restarts it
// after a crash and stops it when dictation has been idle for a while.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/cliruntime"
	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// Config configures a Process.
type Config struct {
	// Exe is the helper executable ("" = this executable).
	Exe string
	// Args are the helper's arguments (nil = {"voice-engine"}).
	Args []string
	// IdleUnload stops the helper this long after the last call (0 = dictation.IdleUnload).
	IdleUnload time.Duration
	// Log receives the helper's failures and the tail of its standard error (nil = discard).
	Log *slog.Logger
}

const (
	// stopGrace is how long Close and idle unloading wait for the helper to exit before killing it.
	stopGrace = 2 * time.Second
	// stderrTail is how much of the helper's standard error is kept for the log.
	stderrTail = 4 << 10
)

// Process runs the speech engine helper. It implements dictation.Recognizer; it is safe for concurrent use and
// runs one request at a time.
type Process struct {
	cfg Config
	log *slog.Logger

	req sync.Mutex // held for a whole request

	mu    sync.Mutex // guards the fields below
	h     *helper
	timer *time.Timer
	gen   uint64 // bumped whenever the idle timer is re-armed or disarmed, so a stale timer does nothing
}

var _ dictation.Recognizer = (*Process)(nil)

// New returns a Process; the helper starts on the first call.
func New(cfg Config) *Process {
	if cfg.Args == nil {
		cfg.Args = []string{"voice-engine"}
	}
	if cfg.IdleUnload <= 0 {
		cfg.IdleUnload = dictation.IdleUnload
	}
	log := cfg.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Process{cfg: cfg, log: log}
}

// Load starts the helper if needed and loads spec's model there. It is a no-op when spec is already loaded.
func (p *Process) Load(ctx context.Context, spec dictation.Spec) error {
	p.req.Lock()
	defer p.req.Unlock()
	h, err := p.acquire(spec)
	if err != nil {
		return err
	}
	defer p.release()
	if h.loaded && h.spec == spec {
		return nil
	}
	_, err = p.call(ctx, h, request{Op: opLoad, Spec: spec}, nil)
	return err
}

// Recognize returns the text spoken in samples, loading spec first when another model (or language) is loaded.
func (p *Process) Recognize(ctx context.Context, spec dictation.Spec, samples []float32) (string, error) {
	if len(samples) > maxSamples {
		return "", dictation.Fail(dictation.CodeTooLong, fmt.Sprintf("Recordings can be at most %d seconds long.", dictation.MaxSeconds))
	}
	p.req.Lock()
	defer p.req.Unlock()
	h, err := p.acquire(spec)
	if err != nil {
		return "", err
	}
	defer p.release()
	return p.call(ctx, h, request{Op: opRecognize, Spec: spec, Samples: len(samples)}, samples)
}

// Close stops the helper, freeing the model's memory. A request in progress fails. A later call starts the
// helper again.
func (p *Process) Close() error {
	p.mu.Lock()
	h := p.h
	p.h = nil
	p.disarm()
	p.mu.Unlock()
	if h != nil {
		h.stop(p.log, true)
	}
	return nil
}

// acquire returns a running helper for spec, starting one if needed, and disarms the idle timer. A helper that
// loaded its engine from another directory is replaced: a process loads the DLLs once.
func (p *Process) acquire(spec dictation.Spec) (*helper, error) {
	p.mu.Lock()
	p.disarm()
	h := p.h
	if h != nil && h.engineDir != "" && h.engineDir != spec.EngineDir {
		p.h = nil
		p.mu.Unlock()
		h.stop(p.log, true)
		p.mu.Lock()
		h = nil
	}
	if h == nil {
		var err error
		if h, err = p.start(); err != nil {
			p.mu.Unlock()
			p.log.Error("speech engine did not start", "err", err)
			return nil, dictation.Fail(dictation.CodeEngine, "The speech engine could not start.")
		}
		p.h = h
	}
	p.mu.Unlock()
	return h, nil
}

// release arms the idle timer after a request.
func (p *Process) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.h == nil {
		return
	}
	p.gen++
	gen := p.gen
	p.timer = time.AfterFunc(p.cfg.IdleUnload, func() { p.unloadIdle(gen) })
}

// disarm stops the idle timer. p.mu must be held.
func (p *Process) disarm() {
	p.gen++
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
}

func (p *Process) unloadIdle(gen uint64) {
	p.mu.Lock()
	if gen != p.gen || p.h == nil {
		p.mu.Unlock()
		return
	}
	h := p.h
	p.h = nil
	p.timer = nil
	p.mu.Unlock()
	p.log.Debug("speech engine idle, stopping")
	h.stop(p.log, true)
}

// drop forgets h after it failed and makes sure it is gone.
func (p *Process) drop(h *helper) {
	p.mu.Lock()
	if p.h == h {
		p.h = nil
	}
	p.mu.Unlock()
	h.stop(p.log, false)
}

// call sends one request to h and reads its answer. Cancelling ctx kills the helper.
func (p *Process) call(ctx context.Context, h *helper, req request, samples []float32) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", ctxFailure(err)
	}
	killed := make(chan struct{})
	stopWatch := context.AfterFunc(ctx, func() {
		h.kill()
		close(killed)
	})
	resp, err := h.roundTrip(req, samples)
	if !stopWatch() {
		<-killed
		p.drop(h)
		return "", ctxFailure(ctx.Err())
	}
	if err != nil {
		stopped := h.stopping() // Close or idle unloading ended it under us
		p.drop(h)
		if stopped {
			return "", dictation.Fail(dictation.CodeCanceled, "Dictation was stopped.")
		}
		p.log.Warn("speech engine stopped unexpectedly", "err", err, "exit", h.exitErr(), "stderr", h.stderr.String())
		return "", dictation.Fail(dictation.CodeEngine, "The speech engine stopped unexpectedly. Try again.")
	}
	h.spec, h.loaded = req.Spec, true
	h.engineDir = req.Spec.EngineDir
	if resp.Error != "" {
		// The helper is still usable; a failed load leaves no model loaded.
		if tail := h.stderr.take(); tail != "" {
			p.log.Warn("speech engine error", "code", resp.Code, "stderr", tail)
		}
		h.loaded = false
		code := resp.Code
		if code == "" {
			code = dictation.CodeEngine
		}
		return "", dictation.Fail(code, resp.Error)
	}
	return resp.Text, nil
}

func ctxFailure(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return dictation.Fail(dictation.CodeTimeout, "Speech recognition took too long.")
	}
	return dictation.Fail(dictation.CodeCanceled, "Speech recognition was canceled.")
}

// helper is one running helper process.
type helper struct {
	cmd     *exec.Cmd
	stdin   *os.File
	stdout  *bufio.Reader
	out     *os.File
	stderr  *tail
	done    chan struct{} // closed once the process has exited and its stderr is drained
	waitErr error         // valid after done

	// Owned by the request holding Process.req.
	spec      dictation.Spec
	loaded    bool
	engineDir string // the directory the helper loaded its engine from ("" = none yet)

	stopOnce sync.Once
	stopMu   sync.Mutex
	closing  bool
}

func (p *Process) start() (*helper, error) {
	exe := p.cfg.Exe
	if exe == "" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return nil, err
		}
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, err
	}
	cmd := exec.Command(exe, p.cfg.Args...)
	cliruntime.HideWindow(cmd)
	cmd.Stdin, cmd.Stdout = inR, outW
	h := &helper{cmd: cmd, stdin: inW, out: outR, stdout: bufio.NewReaderSize(outR, maxLine), stderr: &tail{max: stderrTail}, done: make(chan struct{})}
	cmd.Stderr = h.stderr
	err = cmd.Start()
	inR.Close()
	outW.Close()
	if err != nil {
		inW.Close()
		outR.Close()
		return nil, err
	}
	go func() {
		h.waitErr = cmd.Wait()
		close(h.done)
	}()
	return h, nil
}

func (h *helper) roundTrip(req request, samples []float32) (response, error) {
	line, err := json.Marshal(req)
	if err != nil {
		return response{}, err
	}
	if _, err := h.stdin.Write(append(line, '\n')); err != nil {
		return response{}, err
	}
	if len(samples) > 0 {
		if _, err := h.stdin.Write(floatBytes(samples)); err != nil {
			return response{}, err
		}
	}
	b, err := h.stdout.ReadSlice('\n')
	if err != nil {
		return response{}, err
	}
	var resp response
	if err := json.Unmarshal(b, &resp); err != nil {
		return response{}, fmt.Errorf("decoding response: %w", err)
	}
	return resp, nil
}

func (h *helper) kill() { _ = h.cmd.Process.Kill() }

func (h *helper) stopping() bool {
	h.stopMu.Lock()
	defer h.stopMu.Unlock()
	return h.closing
}

func (h *helper) exitErr() error {
	select {
	case <-h.done:
		return h.waitErr
	default:
		return nil
	}
}

// stop ends the helper: gracefully (close its stdin, wait stopGrace, then kill) or at once. A graceful stop logs
// the rest of the helper's stderr; after a forced one the caller decides.
func (h *helper) stop(log *slog.Logger, graceful bool) {
	h.stopOnce.Do(func() {
		h.stopMu.Lock()
		h.closing = true
		h.stopMu.Unlock()
		h.stdin.Close()
		if graceful {
			select {
			case <-h.done:
			case <-time.After(stopGrace):
				log.Warn("speech engine did not exit, killing it")
				h.kill()
			}
		} else {
			h.kill()
		}
		<-h.done
		h.out.Close()
		if t := h.stderr.String(); graceful && t != "" {
			log.Debug("speech engine stderr", "stderr", t)
		}
	})
}

// tail keeps the last max bytes written to it.
type tail struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	n := len(b)
	if len(b) >= t.max {
		t.buf = append(t.buf[:0], b[len(b)-t.max:]...)
		return n, nil
	}
	if over := len(t.buf) + len(b) - t.max; over > 0 {
		t.buf = append(t.buf[:0], t.buf[over:]...)
	}
	t.buf = append(t.buf, b...)
	return n, nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}

// take returns the kept bytes and forgets them.
func (t *tail) take() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := string(t.buf)
	t.buf = t.buf[:0]
	return s
}

var _ io.Writer = (*tail)(nil)
