package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// The test binary doubles as the helper: Process starts it with fakeArg (a scripted backend behind the real
// protocol server) or "voice-engine" (the real Main, for the smoke test).
const fakeArg = "voice-engine-fake"

func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case fakeArg:
			os.Exit(serve(os.Stdin, os.Stdout, os.Stderr, fakeOpen))
		case "voice-engine":
			os.Exit(Main(os.Args[2:], os.Stdin, os.Stdout, os.Stderr))
		}
	}
	os.Exit(m.Run())
}

// Scripted samples: a recording starting with one of these makes the fake helper misbehave.
const (
	sampleCrash = -1 // exit at once, as a native fault would
	sampleHang  = 0.75
)

var fakeLoads int

func fakeOpen(spec dictation.Spec, log io.Writer) (backend, error) {
	if spec.Encoder == "bad" {
		fmt.Fprintln(log, "fake: bad model")
		return nil, dictation.Fail(dictation.CodeEngine, "bad model")
	}
	fakeLoads++
	return &fakeBackend{spec: spec, load: fakeLoads}, nil
}

type fakeBackend struct {
	spec dictation.Spec
	load int
}

// fakeResult is what the fake helper "recognizes": enough to tell which process, model and audio it saw.
type fakeResult struct {
	PID      int
	Load     int
	Encoder  string
	Language string
	N        int
	Sum      float64
}

func (f *fakeBackend) recognize(samples []float32) (string, error) {
	if len(samples) > 0 {
		switch samples[0] {
		case sampleCrash:
			os.Exit(3)
		case sampleHang:
			time.Sleep(time.Hour)
		}
	}
	r := fakeResult{PID: os.Getpid(), Load: f.load, Encoder: f.spec.Encoder, Language: f.spec.Language, N: len(samples)}
	for _, s := range samples {
		r.Sum += float64(s)
	}
	b, err := json.Marshal(r)
	return string(b), err
}

func (f *fakeBackend) close() {}

func newFake(t *testing.T, idle time.Duration) *Process {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := New(Config{Exe: exe, Args: []string{fakeArg}, IdleUnload: idle})
	t.Cleanup(func() { p.Close() })
	return p
}

func spec(encoder, language string) dictation.Spec {
	return dictation.Spec{EngineDir: "engine", Kind: dictation.KindWhisper, Encoder: encoder, Decoder: "d", Tokens: "t", Language: language}
}

func recognize(t *testing.T, p *Process, s dictation.Spec, samples []float32) fakeResult {
	t.Helper()
	text, err := p.Recognize(context.Background(), s, samples)
	if err != nil {
		t.Fatalf("Recognize: %v", err)
	}
	var r fakeResult
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		t.Fatalf("result %q: %v", text, err)
	}
	return r
}

func wantCode(t *testing.T, err error, code dictation.Code) *dictation.Error {
	t.Helper()
	var de *dictation.Error
	if !errors.As(err, &de) || de.Code != code {
		t.Fatalf("err = %#v, want code %q", err, code)
	}
	return de
}

func running(p *Process) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.h != nil
}

func TestRoundTrip(t *testing.T) {
	p := newFake(t, time.Hour)
	samples := make([]float32, 3*dictation.SampleRate)
	var sum float64
	for i := range samples {
		samples[i] = float32(math.Sin(float64(i) / 7))
		sum += float64(samples[i])
	}
	s := spec("modèle ✓", "de")
	r := recognize(t, p, s, samples)
	if r.N != len(samples) || math.Abs(r.Sum-sum) > 1e-6 || r.Encoder != s.Encoder || r.Language != "de" {
		t.Fatalf("helper saw %+v, sent n=%d sum=%g", r, len(samples), sum)
	}
	if r := recognize(t, p, s, nil); r.N != 0 {
		t.Fatalf("empty recording: helper saw %d samples", r.N)
	}
}

func TestSpecChangeReloads(t *testing.T) {
	p := newFake(t, time.Hour)
	ctx := context.Background()
	a, b := spec("a", ""), spec("a", "en")
	for range 2 {
		if err := p.Load(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	steps := []struct {
		spec dictation.Spec
		load int
	}{{a, 1}, {a, 1}, {b, 2}, {b, 2}, {a, 3}}
	first := recognize(t, p, a, []float32{0.1})
	for i, st := range steps {
		r := recognize(t, p, st.spec, []float32{0.1})
		if r.Load != st.load || r.PID != first.PID {
			t.Fatalf("step %d: load %d in pid %d, want load %d in pid %d", i, r.Load, r.PID, st.load, first.PID)
		}
	}
	// Another engine directory needs a fresh process: a process loads the DLLs once.
	c := a
	c.EngineDir = "other"
	if r := recognize(t, p, c, nil); r.PID == first.PID || r.Load != 1 {
		t.Fatalf("engine dir change: %+v, first pid %d", r, first.PID)
	}
}

func TestHelperErrorsKeepHelper(t *testing.T) {
	p := newFake(t, time.Hour)
	ctx := context.Background()
	before := recognize(t, p, spec("a", ""), nil)

	de := wantCode(t, p.Load(ctx, spec("bad", "")), dictation.CodeEngine)
	if de.Message != "bad model" {
		t.Fatalf("message %q", de.Message)
	}
	_, err := p.Recognize(ctx, spec("a", "xx"), []float32{0})
	wantCode(t, err, dictation.CodeUnknown)
	tr := spec("a", "en")
	tr.Kind = dictation.KindTransducer
	wantCode(t, p.Load(ctx, tr), dictation.CodeUnknown)

	after := recognize(t, p, spec("a", ""), nil)
	if after.PID != before.PID || after.Load != 2 {
		t.Fatalf("after errors: %+v, before %+v", after, before)
	}
}

func TestRestartAfterCrash(t *testing.T) {
	p := newFake(t, time.Hour)
	before := recognize(t, p, spec("a", ""), nil)
	_, err := p.Recognize(context.Background(), spec("a", ""), []float32{sampleCrash})
	de := wantCode(t, err, dictation.CodeEngine)
	if de.Message != "The speech engine stopped unexpectedly. Try again." {
		t.Fatalf("message %q", de.Message)
	}
	if running(p) {
		t.Fatal("crashed helper still referenced")
	}
	after := recognize(t, p, spec("a", ""), nil)
	if after.PID == before.PID || after.Load != 1 {
		t.Fatalf("after crash: %+v, before %+v", after, before)
	}
}

func TestCancelKillsHelper(t *testing.T) {
	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		code dictation.Code
	}{
		{"timeout", func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 300*time.Millisecond)
		}, dictation.CodeTimeout},
		{"cancel", func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(300*time.Millisecond, cancel)
			return ctx, cancel
		}, dictation.CodeCanceled},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newFake(t, time.Hour)
			before := recognize(t, p, spec("a", ""), nil)
			ctx, cancel := c.ctx()
			defer cancel()
			start := time.Now()
			_, err := p.Recognize(ctx, spec("a", ""), []float32{sampleHang})
			wantCode(t, err, c.code)
			if d := time.Since(start); d > 10*time.Second {
				t.Fatalf("returned after %v", d)
			}
			if running(p) {
				t.Fatal("killed helper still referenced")
			}
			if after := recognize(t, p, spec("a", ""), nil); after.PID == before.PID {
				t.Fatal("hung helper was reused")
			}
		})
	}
}

func TestIdleUnloadAndClose(t *testing.T) {
	p := newFake(t, 100*time.Millisecond)
	first := recognize(t, p, spec("a", ""), nil)
	deadline := time.Now().Add(10 * time.Second)
	for running(p) {
		if time.Now().After(deadline) {
			t.Fatal("helper not unloaded after idling")
		}
		time.Sleep(20 * time.Millisecond)
	}
	second := recognize(t, p, spec("a", ""), nil)
	if second.PID == first.PID {
		t.Fatal("idle helper was not stopped")
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if running(p) {
		t.Fatal("helper running after Close")
	}
	if third := recognize(t, p, spec("a", ""), nil); third.PID == second.PID {
		t.Fatal("closed helper was reused")
	}
}

func TestTooLongIsRefusedUpFront(t *testing.T) {
	p := New(Config{Exe: "does-not-exist"})
	_, err := p.Recognize(context.Background(), spec("a", ""), make([]float32, maxSamples+1))
	wantCode(t, err, dictation.CodeTooLong)
}

func TestServeProtocolErrors(t *testing.T) {
	load := func(s dictation.Spec) string {
		b, _ := json.Marshal(request{Op: opLoad, Spec: s})
		return string(b) + "\n"
	}
	cases := []struct {
		name  string
		input string
		code  int
		lines int
	}{
		{"eof", "", 0, 0},
		{"load then eof", load(spec("a", "")), 0, 1},
		{"garbage", "not json\n", 1, 0},
		{"unknown op", `{"op":"dance"}` + "\n", 1, 0},
		{"too many samples", fmt.Sprintf(`{"op":"recognize","samples":%d}`+"\n", maxSamples+1), 1, 0},
		{"truncated samples", `{"op":"recognize","samples":4}` + "\n" + "\x00\x00", 1, 0},
		{"unterminated", load(spec("a", ""))[:10], 1, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if code := serve(strings.NewReader(c.input), &out, &errOut, fakeOpen); code != c.code {
				t.Fatalf("exit %d, want %d (stderr %q)", code, c.code, errOut.String())
			}
			lines := 0
			for sc := bufio.NewScanner(&out); sc.Scan(); lines++ {
			}
			if lines != c.lines {
				t.Fatalf("%d responses, want %d", lines, c.lines)
			}
		})
	}
}
