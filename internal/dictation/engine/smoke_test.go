package engine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// TestSmoke runs the real speech engine (this test binary started as "voice-engine", see TestMain) on real
// recordings with both catalog models and logs transcripts and timings. It is skipped unless OROLE_VOICE_SMOKE
// names a directory laid out like <DataDir>/voice:
//
//	engine/              the DLLs of dictation.EngineBuild
//	models/<model id>/   the files of each catalog model (a missing model is skipped)
//	wavs/*.wav           16 kHz mono 16-bit PCM recordings, each transcribed on its own (e.g. the Parakeet
//	                     model repository's test_wavs, resampled: ffmpeg -i in.wav -ar 16000 -ac 1 out.wav)
//	wavs/long/*.wav      optional: joined in name order with 0.5 s pauses into one recording, which should be
//	                     longer than Whisper's window so dictation.Chunks has to cut it; it is transcribed with
//	                     automatic and English language. Use one voice, as in dictation: sherpa-onnx v1.13.8's
//	                     Parakeet front end drops whole sentences when recordings of different speakers and
//	                     levels are spliced together (upstream sherpa-onnx PR #4024).
//
// Run (go test runs in the package directory, so give an absolute path):
// OROLE_VOICE_SMOKE=<absolute dir> go test -run TestSmoke -v -timeout 30m ./internal/dictation/engine
func TestSmoke(t *testing.T) {
	dir := os.Getenv("OROLE_VOICE_SMOKE")
	if dir == "" {
		t.Skip("OROLE_VOICE_SMOKE not set")
	}
	if !dictation.EngineSupported() {
		t.Skip("no engine build for this computer")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	loadWavs := func(pattern string) (names []string, samples [][]float32) {
		paths, _ := filepath.Glob(filepath.Join(dir, pattern))
		sort.Strings(paths)
		for _, path := range paths {
			s, err := readWav(path)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			names, samples = append(names, filepath.Base(path)), append(samples, s)
		}
		return names, samples
	}
	names, wavs := loadWavs(filepath.Join("wavs", "*.wav"))
	if len(wavs) == 0 {
		t.Fatal("no wavs/*.wav")
	}
	longNames, longParts := loadWavs(filepath.Join("wavs", "long", "*.wav"))
	var long []float32
	for _, s := range longParts {
		long = append(long, s...)
		long = append(long, make([]float32, dictation.SampleRate/2)...)
	}

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := New(Config{Exe: exe, Args: []string{"voice-engine"}})
	defer p.Close()
	ctx := context.Background()

	for _, m := range dictation.Catalog {
		mdir := filepath.Join(dir, "models", m.ID)
		if _, err := os.Stat(mdir); err != nil {
			t.Logf("%s: not downloaded, skipped", m.ID)
			continue
		}
		file := func(name string) string {
			if name == "" {
				return ""
			}
			return filepath.Join(mdir, name)
		}
		spec := dictation.Spec{
			EngineDir: filepath.Join(dir, "engine"), Kind: m.Kind,
			Encoder: file(m.Encoder), Decoder: file(m.Decoder), Joiner: file(m.Joiner), Tokens: file(m.Tokens),
		}
		start := time.Now()
		if err := p.Load(ctx, spec); err != nil {
			t.Fatalf("%s: load: %v", m.ID, err)
		}
		t.Logf("%s: loaded in %.2f s", m.ID, time.Since(start).Seconds())

		run := func(label string, spec dictation.Spec, samples []float32) {
			start := time.Now()
			chunks := dictation.Chunks(samples, dictation.SampleRate, m.ChunkSeconds)
			var parts []string
			for i, chunk := range chunks {
				cs := time.Now()
				text, err := p.Recognize(ctx, spec, chunk)
				if err != nil {
					t.Fatalf("%s %s: %v", m.ID, label, err)
				}
				if len(chunks) > 1 {
					t.Logf("  chunk %d (%.2f s audio): %.2f s: %q", i, float64(len(chunk))/dictation.SampleRate, time.Since(cs).Seconds(), text)
				}
				if text = strings.TrimSpace(text); text != "" {
					parts = append(parts, text)
				}
			}
			text := strings.Join(parts, " ")
			t.Logf("%s %s (%.1f s audio, %d chunks): %.2f s: %q", m.ID, label,
				float64(len(samples))/dictation.SampleRate, len(chunks), time.Since(start).Seconds(), text)
			if text == "" {
				t.Errorf("%s %s: empty transcript", m.ID, label)
			}
		}
		for i, name := range names {
			run(name, spec, wavs[i])
		}
		if len(long) == 0 {
			continue
		}
		langs := []string{""}
		if m.LanguageChoice {
			langs = append(langs, "en")
		}
		for _, lang := range langs {
			s := spec
			s.Language = lang
			label := "auto"
			if lang != "" {
				label = lang
			}
			run(fmt.Sprintf("long %v language=%s", longNames, label), s, long)
		}
	}
}

// readWav reads a 16 kHz mono 16-bit PCM WAV file as float32 samples.
func readWav(path string) ([]float32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var format bool
	for off := 12; off+8 <= len(b); {
		id, size := string(b[off:off+4]), int(binary.LittleEndian.Uint32(b[off+4:]))
		body := b[off+8 : min(off+8+size, len(b))]
		switch id {
		case "fmt ":
			if len(body) < 16 || binary.LittleEndian.Uint16(body) != 1 || binary.LittleEndian.Uint16(body[2:]) != 1 ||
				binary.LittleEndian.Uint32(body[4:]) != dictation.SampleRate || binary.LittleEndian.Uint16(body[14:]) != 16 {
				return nil, errors.New("not 16 kHz mono 16-bit PCM")
			}
			format = true
		case "data":
			if !format {
				return nil, errors.New("data before fmt")
			}
			out := make([]float32, len(body)/2)
			for i := range out {
				out[i] = float32(int16(binary.LittleEndian.Uint16(body[2*i:]))) / 32768
			}
			return out, nil
		}
		off += 8 + size + size%2
	}
	return nil, errors.New("no data chunk")
}
