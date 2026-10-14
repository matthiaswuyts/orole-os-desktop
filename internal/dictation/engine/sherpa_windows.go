//go:build amd64

package engine

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/rayorole/orole-os-desktop/internal/dictation"
)

// The structs below mirror sherpa-onnx v1.13.8's c-api.h (dictation.EngineBuild.Version) field for field:
// int32_t -> int32, float -> float32, const char * -> *byte. The library refuses any other version before a
// struct is touched, and sherpa_layout_windows_amd64_test.go pins every size and offset to the C compiler's.

type sherpaFeatureConfig struct {
	sampleRate int32
	featureDim int32
}

type sherpaHomophoneReplacerConfig struct {
	dictDir  *byte
	lexicon  *byte
	ruleFsts *byte
}

type sherpaOfflineTransducerModelConfig struct {
	encoder *byte
	decoder *byte
	joiner  *byte
}

type sherpaOfflineParaformerModelConfig struct{ model *byte }

type sherpaOfflineNemoEncDecCtcModelConfig struct{ model *byte }

type sherpaOfflineWhisperModelConfig struct {
	encoder                 *byte
	decoder                 *byte
	language                *byte
	task                    *byte
	tailPaddings            int32
	enableTokenTimestamps   int32
	enableSegmentTimestamps int32
}

type sherpaOfflineCanaryModelConfig struct {
	encoder *byte
	decoder *byte
	srcLang *byte
	tgtLang *byte
	usePnc  int32
}

type sherpaOfflineCohereTranscribeModelConfig struct {
	encoder  *byte
	decoder  *byte
	language *byte
	usePunct int32
	useItn   int32
}

type sherpaOfflineFireRedAsrModelConfig struct {
	encoder *byte
	decoder *byte
}

type sherpaOfflineFireRedAsrCtcModelConfig struct{ model *byte }

type sherpaOfflineMoonshineModelConfig struct {
	preprocessor    *byte
	encoder         *byte
	uncachedDecoder *byte
	cachedDecoder   *byte
	mergedDecoder   *byte
}

type sherpaOfflineTdnnModelConfig struct{ model *byte }

type sherpaOfflineLMConfig struct {
	model *byte
	scale float32
}

type sherpaOfflineSenseVoiceModelConfig struct {
	model    *byte
	language *byte
	useItn   int32
}

type sherpaOfflineDolphinModelConfig struct{ model *byte }

type sherpaOfflineZipformerCtcModelConfig struct{ model *byte }

type sherpaOfflineWenetCtcModelConfig struct{ model *byte }

type sherpaOfflineOmnilingualAsrCtcModelConfig struct{ model *byte }

type sherpaOfflineFunASRNanoModelConfig struct {
	encoderAdaptor *byte
	llm            *byte
	embedding      *byte
	tokenizer      *byte
	systemPrompt   *byte
	userPrompt     *byte
	maxNewTokens   int32
	temperature    float32
	topP           float32
	seed           int32
	language       *byte
	itn            int32
	hotwords       *byte
}

type sherpaOfflineQwen3ASRModelConfig struct {
	convFrontend *byte
	encoder      *byte
	decoder      *byte
	tokenizer    *byte
	maxTotalLen  int32
	maxNewTokens int32
	temperature  float32
	topP         float32
	seed         int32
	hotwords     *byte
}

type sherpaOfflineMedAsrCtcModelConfig struct{ model *byte }

type sherpaOfflineModelConfig struct {
	transducer       sherpaOfflineTransducerModelConfig
	paraformer       sherpaOfflineParaformerModelConfig
	nemoCtc          sherpaOfflineNemoEncDecCtcModelConfig
	whisper          sherpaOfflineWhisperModelConfig
	tdnn             sherpaOfflineTdnnModelConfig
	tokens           *byte
	numThreads       int32
	debug            int32
	provider         *byte
	modelType        *byte
	modelingUnit     *byte
	bpeVocab         *byte
	telespeechCtc    *byte
	senseVoice       sherpaOfflineSenseVoiceModelConfig
	moonshine        sherpaOfflineMoonshineModelConfig
	fireRedAsr       sherpaOfflineFireRedAsrModelConfig
	dolphin          sherpaOfflineDolphinModelConfig
	zipformerCtc     sherpaOfflineZipformerCtcModelConfig
	canary           sherpaOfflineCanaryModelConfig
	wenetCtc         sherpaOfflineWenetCtcModelConfig
	omnilingual      sherpaOfflineOmnilingualAsrCtcModelConfig
	medasr           sherpaOfflineMedAsrCtcModelConfig
	funasrNano       sherpaOfflineFunASRNanoModelConfig
	fireRedAsrCtc    sherpaOfflineFireRedAsrCtcModelConfig
	qwen3Asr         sherpaOfflineQwen3ASRModelConfig
	cohereTranscribe sherpaOfflineCohereTranscribeModelConfig
}

type sherpaOfflineRecognizerConfig struct {
	featConfig     sherpaFeatureConfig
	modelConfig    sherpaOfflineModelConfig
	lmConfig       sherpaOfflineLMConfig
	decodingMethod *byte
	maxActivePaths int32
	hotwordsFile   *byte
	hotwordsScore  float32
	ruleFsts       *byte
	ruleFars       *byte
	blankPenalty   float32
	hr             sherpaHomophoneReplacerConfig
}

// sherpaOfflineRecognizerResult is the leading part of SherpaOnnxOfflineRecognizerResult; only text is read.
type sherpaOfflineRecognizerResult struct {
	text *byte
}

// sherpaLib is the loaded C API. A process loads it once, from one directory.
type sherpaLib struct {
	dir string

	createRecognizer  uintptr
	destroyRecognizer uintptr
	createStream      uintptr
	destroyStream     uintptr
	acceptWaveform    uintptr
	decodeStream      uintptr
	getResult         uintptr
	destroyResult     uintptr
}

var (
	libMu sync.Mutex
	lib   *sherpaLib
)

func engineDamaged() error {
	return dictation.Fail(dictation.CodeEngine, "The speech engine files are damaged. Remove the model and download it again.")
}

// loadSherpa loads dictation.EngineBuild.API from dir (the other DLLs resolve from the same directory) and checks
// its version before anything else.
func loadSherpa(dir string, log io.Writer) (*sherpaLib, error) {
	libMu.Lock()
	defer libMu.Unlock()
	if lib != nil {
		if lib.dir != dir {
			fmt.Fprintf(log, "voice-engine: engine already loaded from %s, asked for %s\n", lib.dir, dir)
			return nil, dictation.Fail(dictation.CodeEngine, "The speech engine changed. Try again.")
		}
		return lib, nil
	}
	if !filepath.IsAbs(dir) {
		fmt.Fprintf(log, "voice-engine: engine directory %q is not absolute\n", dir)
		return nil, dictation.Fail(dictation.CodeNotReady, "The speech engine is not installed.")
	}
	api := filepath.Join(dir, dictation.EngineBuild.API)
	if _, err := os.Stat(api); err != nil {
		fmt.Fprintln(log, "voice-engine:", err)
		return nil, dictation.Fail(dictation.CodeNotReady, "The speech engine is not installed. Download the model again.")
	}

	// Native code logs with printf too. Point this process's standard output at standard error before the DLLs'
	// C runtimes read it, so nothing but protocol frames reaches the pipe (os.Stdout keeps the original handle).
	if h, err := windows.GetStdHandle(windows.STD_ERROR_HANDLE); err == nil && h != 0 && h != windows.InvalidHandle {
		_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, h)
	}
	if err := windows.SetDllDirectory(dir); err != nil {
		fmt.Fprintln(log, "voice-engine: SetDllDirectory:", err)
		return nil, engineDamaged()
	}
	mod, err := windows.LoadLibraryEx(api, 0, windows.LOAD_WITH_ALTERED_SEARCH_PATH)
	if err != nil {
		fmt.Fprintf(log, "voice-engine: loading %s: %v\n", api, err)
		return nil, engineDamaged()
	}
	proc := func(name string) uintptr {
		if err != nil {
			return 0
		}
		var p uintptr
		if p, err = windows.GetProcAddress(mod, name); err != nil {
			err = fmt.Errorf("%s: %w", name, err)
		}
		return p
	}
	getVersion := proc("SherpaOnnxGetVersionStr")
	if err != nil {
		fmt.Fprintln(log, "voice-engine:", err)
		_ = windows.FreeLibrary(mod)
		return nil, engineDamaged()
	}
	r, _, _ := syscall.SyscallN(getVersion)
	if v := cString(r); v != dictation.EngineBuild.Version {
		fmt.Fprintf(log, "voice-engine: engine version %q, want %q\n", v, dictation.EngineBuild.Version)
		_ = windows.FreeLibrary(mod)
		return nil, dictation.Fail(dictation.CodeEngine, "The speech engine on this computer is a different version. Remove the model and download it again.")
	}
	l := &sherpaLib{
		dir:               dir,
		createRecognizer:  proc("SherpaOnnxCreateOfflineRecognizer"),
		destroyRecognizer: proc("SherpaOnnxDestroyOfflineRecognizer"),
		createStream:      proc("SherpaOnnxCreateOfflineStream"),
		destroyStream:     proc("SherpaOnnxDestroyOfflineStream"),
		acceptWaveform:    proc("SherpaOnnxAcceptWaveformOffline"),
		decodeStream:      proc("SherpaOnnxDecodeOfflineStream"),
		getResult:         proc("SherpaOnnxGetOfflineStreamResult"),
		destroyResult:     proc("SherpaOnnxDestroyOfflineRecognizerResult"),
	}
	if err != nil {
		fmt.Fprintln(log, "voice-engine:", err)
		_ = windows.FreeLibrary(mod)
		return nil, engineDamaged()
	}
	lib = l
	return l, nil
}

// sherpaRecognizer is one loaded model.
type sherpaRecognizer struct {
	lib *sherpaLib
	rec uintptr
	log io.Writer
}

func openNative(spec dictation.Spec, log io.Writer) (backend, error) {
	files := []string{spec.Encoder, spec.Decoder, spec.Tokens}
	if spec.Kind == dictation.KindTransducer {
		files = append(files, spec.Joiner)
	}
	for _, f := range files {
		if _, err := os.Stat(f); f == "" || err != nil {
			fmt.Fprintf(log, "voice-engine: model file %q: %v\n", f, err)
			return nil, dictation.Fail(dictation.CodeNotReady, "The speech model's files are missing. Download the model again.")
		}
	}
	l, err := loadSherpa(spec.EngineDir, log)
	if err != nil {
		return nil, err
	}

	var cfg sherpaOfflineRecognizerConfig
	// Both models read their feature size from their own metadata (sherpa-onnx v1.13.8: the NeMo transducer's
	// PostInit sets feature_dim from the model, Whisper's streams use the model's n_mels), so 80 is only the
	// documented default and is overridden; the rate is what the recording is resampled to.
	cfg.featConfig = sherpaFeatureConfig{sampleRate: dictation.SampleRate, featureDim: 80}
	m := &cfg.modelConfig
	switch spec.Kind {
	case dictation.KindTransducer:
		m.transducer = sherpaOfflineTransducerModelConfig{encoder: cStr(spec.Encoder), decoder: cStr(spec.Decoder), joiner: cStr(spec.Joiner)}
		m.modelType = cStr("nemo_transducer")
	case dictation.KindWhisper:
		m.whisper = sherpaOfflineWhisperModelConfig{
			encoder: cStr(spec.Encoder), decoder: cStr(spec.Decoder),
			language: cStr(spec.Language), // NULL = detect the language
			task:     cStr("transcribe"),
		}
	default:
		return nil, dictation.Fail(dictation.CodeUnknown, "This speech model is not supported.")
	}
	m.tokens = cStr(spec.Tokens)
	m.numThreads = int32(min(max(runtime.NumCPU()/2, 1), 8))
	m.provider = cStr("cpu")
	cfg.decodingMethod = cStr("greedy_search")

	r, _, _ := syscall.SyscallN(l.createRecognizer, uintptr(unsafe.Pointer(&cfg)))
	runtime.KeepAlive(&cfg) // and with it every string it points to
	if r == 0 {
		fmt.Fprintln(log, "voice-engine: SherpaOnnxCreateOfflineRecognizer returned NULL")
		return nil, dictation.Fail(dictation.CodeEngine, "The speech model could not be loaded. Remove it and download it again.")
	}
	return &sherpaRecognizer{lib: l, rec: r, log: log}, nil
}

func (s *sherpaRecognizer) recognize(samples []float32) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}
	l := s.lib
	stream, _, _ := syscall.SyscallN(l.createStream, s.rec)
	if stream == 0 {
		fmt.Fprintln(s.log, "voice-engine: SherpaOnnxCreateOfflineStream returned NULL")
		return "", dictation.Fail(dictation.CodeEngine, "The speech engine could not process the recording.")
	}
	defer syscall.SyscallN(l.destroyStream, stream)
	syscall.SyscallN(l.acceptWaveform, stream, uintptr(dictation.SampleRate), uintptr(unsafe.Pointer(&samples[0])), uintptr(int32(len(samples))))
	runtime.KeepAlive(samples)
	syscall.SyscallN(l.decodeStream, s.rec, stream)
	res, _, _ := syscall.SyscallN(l.getResult, stream)
	if res == 0 {
		fmt.Fprintln(s.log, "voice-engine: SherpaOnnxGetOfflineStreamResult returned NULL")
		return "", dictation.Fail(dictation.CodeEngine, "The speech engine could not process the recording.")
	}
	defer syscall.SyscallN(l.destroyResult, res)
	result := *(**sherpaOfflineRecognizerResult)(unsafe.Pointer(&res))
	return windows.BytePtrToString(result.text), nil
}

func (s *sherpaRecognizer) close() {
	if s.rec != 0 {
		syscall.SyscallN(s.lib.destroyRecognizer, s.rec)
		s.rec = 0
	}
}

// cStr returns s as a NUL-terminated C string, or NULL for "" (the C API's "use the default").
func cStr(s string) *byte {
	if s == "" {
		return nil
	}
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}

// cString copies the C string at p (owned by the library).
func cString(p uintptr) string {
	if p == 0 {
		return ""
	}
	return windows.BytePtrToString(*(**byte)(unsafe.Pointer(&p)))
}
