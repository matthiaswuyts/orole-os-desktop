package engine

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unsafe"

	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// The helper speaks a framed protocol on its standard input and output, one request at a time:
//
//	request:  one JSON line (request), then for "recognize" Samples float32 values in the machine's byte order
//	          (both ends are the same executable on the same computer)
//	response: one JSON line (response)
//
// Standard error carries diagnostics only (never audio or text); the client keeps its tail for the daemon log.
// The helper exits with 0 when its standard input ends.

const (
	opLoad      = "load"
	opRecognize = "recognize"

	// maxSamples bounds one recognize request: a whole recording at most.
	maxSamples = dictation.MaxPCMBytes / 2
	// maxLine bounds one JSON line in either direction.
	maxLine = 64 << 10
)

type request struct {
	Op      string         `json:"op"`
	Spec    dictation.Spec `json:"spec"`
	Samples int            `json:"samples,omitempty"`
}

type response struct {
	Text  string         `json:"text,omitempty"`
	Code  dictation.Code `json:"code,omitempty"`
	Error string         `json:"error,omitempty"`
}

// backend is one loaded model.
type backend interface {
	recognize(samples []float32) (string, error)
	close()
}

// opener loads spec's model. Errors are *dictation.Error with a user-ready message; details go to the log.
type opener func(spec dictation.Spec, log io.Writer) (backend, error)

// Main is the helper's entry point (`orole-daemon voice-engine`). It returns the process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 {
		fmt.Fprintln(stderr, "voice-engine: no arguments expected")
		return 2
	}
	return serve(stdin, stdout, stderr, openNative)
}

// serve answers requests until stdin ends. It keeps one model loaded and replaces it when a request names
// another spec (model files, kind or language).
func serve(stdin io.Reader, stdout, stderr io.Writer, open opener) int {
	in := bufio.NewReaderSize(stdin, maxLine)
	out := bufio.NewWriter(stdout)
	var (
		cur     backend
		curSpec dictation.Spec
	)
	defer func() {
		if cur != nil {
			cur.close()
		}
	}()
	var samples []float32
	for {
		req, err := readRequest(in)
		if errors.Is(err, io.EOF) {
			return 0
		}
		if err != nil {
			fmt.Fprintln(stderr, "voice-engine:", err)
			return 1
		}
		if req.Op == opRecognize {
			if cap(samples) < req.Samples {
				samples = make([]float32, req.Samples)
			}
			samples = samples[:req.Samples]
			if _, err := io.ReadFull(in, floatBytes(samples)); err != nil {
				fmt.Fprintln(stderr, "voice-engine: reading samples:", err)
				return 1
			}
		}

		var resp response
		if cur == nil || curSpec != req.Spec {
			if cur != nil {
				cur.close() // free the old model before loading the next one
				cur = nil
			}
			b, err := openChecked(open, req.Spec, stderr)
			if err != nil {
				resp = failure(err, stderr)
			} else {
				cur, curSpec = b, req.Spec
			}
		}
		if cur != nil && req.Op == opRecognize {
			text, err := cur.recognize(samples)
			if err != nil {
				resp = failure(err, stderr)
			} else {
				resp.Text = text
			}
		}
		if err := writeResponse(out, resp); err != nil {
			fmt.Fprintln(stderr, "voice-engine: writing response:", err)
			return 1
		}
	}
}

// failure turns err into a response; anything but a *dictation.Error is logged and reported generically.
func failure(err error, log io.Writer) response {
	var de *dictation.Error
	if errors.As(err, &de) {
		return response{Code: de.Code, Error: de.Message}
	}
	fmt.Fprintln(log, "voice-engine:", err)
	return response{Code: dictation.CodeEngine, Error: "The speech engine failed. Try again."}
}

func readRequest(in *bufio.Reader) (request, error) {
	line, err := in.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			if len(line) == 0 {
				return request{}, io.EOF
			}
			err = io.ErrUnexpectedEOF
		}
		return request{}, fmt.Errorf("reading request: %w", err)
	}
	var req request
	if err := json.Unmarshal(line, &req); err != nil {
		return request{}, fmt.Errorf("decoding request: %w", err)
	}
	switch req.Op {
	case opLoad:
		if req.Samples != 0 {
			return request{}, errors.New("load request carries samples")
		}
	case opRecognize:
		if req.Samples < 0 || req.Samples > maxSamples {
			return request{}, fmt.Errorf("recognize request with %d samples", req.Samples)
		}
	default:
		return request{}, fmt.Errorf("unknown op %q", req.Op)
	}
	return req, nil
}

func writeResponse(out *bufio.Writer, resp response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := out.Write(append(b, '\n')); err != nil {
		return err
	}
	return out.Flush()
}

// floatBytes views samples as bytes without copying.
func floatBytes(samples []float32) []byte {
	if len(samples) == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&samples[0])), len(samples)*4)
}

// openChecked refuses specs the engine must never see (sherpa-onnx exits the process on some bad values).
func openChecked(open opener, spec dictation.Spec, log io.Writer) (backend, error) {
	switch spec.Kind {
	case dictation.KindTransducer:
		if spec.Language != "" {
			return nil, dictation.Fail(dictation.CodeUnknown, "This model finds the spoken language itself.")
		}
	case dictation.KindWhisper:
		if !dictation.ValidLanguage(spec.Language) {
			return nil, dictation.Fail(dictation.CodeUnknown, "This language is not available for this model.")
		}
	default:
		return nil, dictation.Fail(dictation.CodeUnknown, "This speech model is not supported.")
	}
	return open(spec, log)
}
