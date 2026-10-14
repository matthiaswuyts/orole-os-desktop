package dictation

import (
	"runtime"
	"sort"
)

// File is one download, pinned by size and SHA-256 (hex).
type File struct {
	Name   string
	URL    string
	Size   int64
	SHA256 string
}

// Kind tells the engine how to load a model.
type Kind string

const (
	KindTransducer Kind = "nemo_transducer" // encoder, decoder, joiner, tokens
	KindWhisper    Kind = "whisper"         // encoder, decoder, tokens
)

// Model is a speech model the user can download.
type Model struct {
	ID          string
	Name        string
	Description string
	License     string
	Kind        Kind
	// LanguageChoice: the spoken language can be set (Whisper). Otherwise the model finds it itself.
	LanguageChoice bool
	// ChunkSeconds > 0 splits longer recordings into pieces of at most this length (Whisper reads 30 s windows).
	ChunkSeconds int
	// Encoder, Decoder, Joiner and Tokens name files in Files ("" = not used by this Kind).
	Encoder, Decoder, Joiner, Tokens string
	Files                            []File
}

// SizeBytes is the model's download and disk size.
func (m Model) SizeBytes() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// Model IDs.
const (
	ModelParakeet     = "parakeet-tdt-0.6b-v3"
	ModelWhisperTurbo = "whisper-large-v3-turbo"
)

const (
	parakeetBase = "https://huggingface.co/csukuangfj/sherpa-onnx-nemo-parakeet-tdt-0.6b-v3-int8/resolve/2bda32ec70b097a55adaa07d9a7173915b43cc78/"
	whisperBase  = "https://huggingface.co/csukuangfj/sherpa-onnx-whisper-turbo/resolve/2ca6ff69fc878651b770880507669577ac41c2ff/"
)

// Catalog lists the models in the order the settings page shows them. URLs are pinned to a repository
// revision and every file to its size and SHA-256 (the hashes Hugging Face publishes for its LFS files).
var Catalog = []Model{
	{
		ID: ModelParakeet, Name: "Parakeet TDT 0.6B v3",
		Description: "Fast. Finds the spoken language itself, in 25 European languages.",
		License:     "NVIDIA Parakeet TDT 0.6B v3, CC BY 4.0",
		Kind:        KindTransducer,
		Encoder:     "encoder.int8.onnx", Decoder: "decoder.int8.onnx", Joiner: "joiner.int8.onnx", Tokens: "tokens.txt",
		Files: []File{
			{"encoder.int8.onnx", parakeetBase + "encoder.int8.onnx", 652184281, "acfc2b4456377e15d04f0243af540b7fe7c992f8d898d751cf134c3a55fd2247"},
			{"decoder.int8.onnx", parakeetBase + "decoder.int8.onnx", 11845275, "179e50c43d1a9de79c8a24149a2f9bac6eb5981823f2a2ed88d655b24248db4e"},
			{"joiner.int8.onnx", parakeetBase + "joiner.int8.onnx", 6355277, "3164c13fc2821009440d20fcb5fdc78bff28b4db2f8d0f0b329101719c0948b3"},
			{"tokens.txt", parakeetBase + "tokens.txt", 93939, "d58544679ea4bc6ac563d1f545eb7d474bd6cfa467f0a6e2c1dc1c7d37e3c35d"},
		},
	},
	{
		ID: ModelWhisperTurbo, Name: "Whisper large-v3 turbo",
		Description:    "Slower. About 100 languages, and you can set the spoken language.",
		License:        "OpenAI Whisper large-v3 turbo, MIT",
		Kind:           KindWhisper,
		LanguageChoice: true,
		ChunkSeconds:   28,
		Encoder:        "turbo-encoder.int8.onnx", Decoder: "turbo-decoder.int8.onnx", Tokens: "turbo-tokens.txt",
		Files: []File{
			{"turbo-encoder.int8.onnx", whisperBase + "turbo-encoder.int8.onnx", 674716297, "b02dcdf54f348741e93fe732b67d933c8dcb6735655f710640143081db38878b"},
			{"turbo-decoder.int8.onnx", whisperBase + "turbo-decoder.int8.onnx", 361080764, "20accd02388482eb3a46bd615631adfdc85e1eb2c7db9ea3f02a40ffe6b81547"},
			{"turbo-tokens.txt", whisperBase + "turbo-tokens.txt", 816730, "b34b360dbb493e781e479794586d661700670d65564001f23024971d1f2fa126"},
		},
	},
}

// ModelByID finds a catalog model.
func ModelByID(id string) (Model, bool) {
	for _, m := range Catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// Engine is the speech engine build: sherpa-onnx's C API with ONNX Runtime, CPU only, static C runtime
// (no Visual C++ redistributable needed). The archive is GitHub's release asset; its SHA-256 is the digest
// GitHub publishes for it.
type Engine struct {
	Version string
	Archive File
	// Prefix is the archive directory the DLLs live in; DLLs are the files extracted from it.
	Prefix string
	DLLs   []string
	// API is the DLL the engine loads; it resolves the others from the same directory.
	API string
}

// EngineBuild is the pinned engine. The helper refuses a DLL that reports another version: the C structs it
// fills are laid out for exactly this one.
var EngineBuild = Engine{
	Version: "1.13.8",
	Archive: File{
		Name:   "sherpa-onnx-v1.13.8-win-x64-shared-MT-Release-no-tts-lib.tar.bz2",
		URL:    "https://github.com/k2-fsa/sherpa-onnx/releases/download/v1.13.8/sherpa-onnx-v1.13.8-win-x64-shared-MT-Release-no-tts-lib.tar.bz2",
		Size:   7536080,
		SHA256: "a1253e665c4f236119c443c8932a8acfca32a546c78c05962d483c9a0eae21b7",
	},
	Prefix: "sherpa-onnx-v1.13.8-win-x64-shared-MT-Release-no-tts-lib/lib/",
	DLLs:   []string{"sherpa-onnx-c-api.dll", "onnxruntime.dll", "onnxruntime_providers_shared.dll"},
	API:    "sherpa-onnx-c-api.dll",
}

// EngineSupported reports whether this computer can run EngineBuild (Windows on x64 only).
func EngineSupported() bool { return runtime.GOOS == "windows" && runtime.GOARCH == "amd64" }

// whisperLanguages are the languages Whisper large-v3 turbo can be told to expect (its tokenizer's list).
var whisperLanguages = map[string]string{
	"af": "Afrikaans", "am": "Amharic", "ar": "Arabic", "as": "Assamese", "az": "Azerbaijani", "ba": "Bashkir",
	"be": "Belarusian", "bg": "Bulgarian", "bn": "Bengali", "bo": "Tibetan", "br": "Breton", "bs": "Bosnian",
	"ca": "Catalan", "cs": "Czech", "cy": "Welsh", "da": "Danish", "de": "German", "el": "Greek", "en": "English",
	"es": "Spanish", "et": "Estonian", "eu": "Basque", "fa": "Persian", "fi": "Finnish", "fo": "Faroese",
	"fr": "French", "gl": "Galician", "gu": "Gujarati", "ha": "Hausa", "haw": "Hawaiian", "he": "Hebrew",
	"hi": "Hindi", "hr": "Croatian", "ht": "Haitian Creole", "hu": "Hungarian", "hy": "Armenian", "id": "Indonesian",
	"is": "Icelandic", "it": "Italian", "ja": "Japanese", "jw": "Javanese", "ka": "Georgian", "kk": "Kazakh",
	"km": "Khmer", "kn": "Kannada", "ko": "Korean", "la": "Latin", "lb": "Luxembourgish", "ln": "Lingala",
	"lo": "Lao", "lt": "Lithuanian", "lv": "Latvian", "mg": "Malagasy", "mi": "Maori", "mk": "Macedonian",
	"ml": "Malayalam", "mn": "Mongolian", "mr": "Marathi", "ms": "Malay", "mt": "Maltese", "my": "Burmese",
	"ne": "Nepali", "nl": "Dutch", "nn": "Norwegian Nynorsk", "no": "Norwegian", "oc": "Occitan", "pa": "Punjabi",
	"pl": "Polish", "ps": "Pashto", "pt": "Portuguese", "ro": "Romanian", "ru": "Russian", "sa": "Sanskrit",
	"sd": "Sindhi", "si": "Sinhala", "sk": "Slovak", "sl": "Slovenian", "sn": "Shona", "so": "Somali",
	"sq": "Albanian", "sr": "Serbian", "su": "Sundanese", "sv": "Swedish", "sw": "Swahili", "ta": "Tamil",
	"te": "Telugu", "tg": "Tajik", "th": "Thai", "tk": "Turkmen", "tl": "Tagalog", "tr": "Turkish", "tt": "Tatar",
	"uk": "Ukrainian", "ur": "Urdu", "uz": "Uzbek", "vi": "Vietnamese", "yi": "Yiddish", "yo": "Yoruba",
	"yue": "Cantonese", "zh": "Chinese",
}

// Languages lists whisperLanguages by name.
func Languages() []Language {
	out := make([]Language, 0, len(whisperLanguages))
	for code, name := range whisperLanguages {
		out = append(out, Language{Code: code, Name: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ValidLanguage reports whether code may be set ("" = automatic).
func ValidLanguage(code string) bool {
	_, ok := whisperLanguages[code]
	return code == "" || ok
}
