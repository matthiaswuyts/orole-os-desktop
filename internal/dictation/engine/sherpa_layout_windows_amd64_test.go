package engine

import (
	"testing"
	"unsafe"
)

// TestSherpaLayout pins the Go mirrors of sherpa-onnx v1.13.8's C structs to the C compiler's layout. The numbers
// come from a throwaway C program (not committed) built with MinGW-w64 gcc 14.2 (x86_64) against that release's
// sherpa-onnx/c-api/c-api.h, printing sizeof/offsetof for each struct and field below. Regenerate them the same
// way when dictation.EngineBuild moves to another version.
func TestSherpaLayout(t *testing.T) {
	var (
		feat  sherpaFeatureConfig
		tr    sherpaOfflineTransducerModelConfig
		wh    sherpaOfflineWhisperModelConfig
		model sherpaOfflineModelConfig
		rec   sherpaOfflineRecognizerConfig
		res   sherpaOfflineRecognizerResult
	)
	cases := []struct {
		name      string
		got, want uintptr
	}{
		{"sizeof(SherpaOnnxFeatureConfig)", unsafe.Sizeof(feat), 8},
		{"FeatureConfig.sample_rate", unsafe.Offsetof(feat.sampleRate), 0},
		{"FeatureConfig.feature_dim", unsafe.Offsetof(feat.featureDim), 4},

		{"sizeof(SherpaOnnxOfflineTransducerModelConfig)", unsafe.Sizeof(tr), 24},
		{"OfflineTransducerModelConfig.joiner", unsafe.Offsetof(tr.joiner), 16},

		{"sizeof(SherpaOnnxOfflineWhisperModelConfig)", unsafe.Sizeof(wh), 48},
		{"OfflineWhisperModelConfig.language", unsafe.Offsetof(wh.language), 16},
		{"OfflineWhisperModelConfig.task", unsafe.Offsetof(wh.task), 24},
		{"OfflineWhisperModelConfig.tail_paddings", unsafe.Offsetof(wh.tailPaddings), 32},
		{"OfflineWhisperModelConfig.enable_segment_timestamps", unsafe.Offsetof(wh.enableSegmentTimestamps), 40},

		{"sizeof(SherpaOnnxOfflineCanaryModelConfig)", unsafe.Sizeof(sherpaOfflineCanaryModelConfig{}), 40},
		{"sizeof(SherpaOnnxOfflineCohereTranscribeModelConfig)", unsafe.Sizeof(sherpaOfflineCohereTranscribeModelConfig{}), 32},
		{"sizeof(SherpaOnnxOfflineMoonshineModelConfig)", unsafe.Sizeof(sherpaOfflineMoonshineModelConfig{}), 40},
		{"sizeof(SherpaOnnxOfflineLMConfig)", unsafe.Sizeof(sherpaOfflineLMConfig{}), 16},
		{"sizeof(SherpaOnnxOfflineSenseVoiceModelConfig)", unsafe.Sizeof(sherpaOfflineSenseVoiceModelConfig{}), 24},
		{"sizeof(SherpaOnnxOfflineFunASRNanoModelConfig)", unsafe.Sizeof(sherpaOfflineFunASRNanoModelConfig{}), 88},
		{"sizeof(SherpaOnnxOfflineQwen3ASRModelConfig)", unsafe.Sizeof(sherpaOfflineQwen3ASRModelConfig{}), 64},
		{"sizeof(SherpaOnnxHomophoneReplacerConfig)", unsafe.Sizeof(sherpaHomophoneReplacerConfig{}), 24},
		{"sizeof(SherpaOnnxOfflineFireRedAsrModelConfig)", unsafe.Sizeof(sherpaOfflineFireRedAsrModelConfig{}), 16},

		{"sizeof(SherpaOnnxOfflineModelConfig)", unsafe.Sizeof(model), 504},
		{"OfflineModelConfig.transducer", unsafe.Offsetof(model.transducer), 0},
		{"OfflineModelConfig.paraformer", unsafe.Offsetof(model.paraformer), 24},
		{"OfflineModelConfig.nemo_ctc", unsafe.Offsetof(model.nemoCtc), 32},
		{"OfflineModelConfig.whisper", unsafe.Offsetof(model.whisper), 40},
		{"OfflineModelConfig.tdnn", unsafe.Offsetof(model.tdnn), 88},
		{"OfflineModelConfig.tokens", unsafe.Offsetof(model.tokens), 96},
		{"OfflineModelConfig.num_threads", unsafe.Offsetof(model.numThreads), 104},
		{"OfflineModelConfig.debug", unsafe.Offsetof(model.debug), 108},
		{"OfflineModelConfig.provider", unsafe.Offsetof(model.provider), 112},
		{"OfflineModelConfig.model_type", unsafe.Offsetof(model.modelType), 120},
		{"OfflineModelConfig.modeling_unit", unsafe.Offsetof(model.modelingUnit), 128},
		{"OfflineModelConfig.bpe_vocab", unsafe.Offsetof(model.bpeVocab), 136},
		{"OfflineModelConfig.telespeech_ctc", unsafe.Offsetof(model.telespeechCtc), 144},
		{"OfflineModelConfig.sense_voice", unsafe.Offsetof(model.senseVoice), 152},
		{"OfflineModelConfig.moonshine", unsafe.Offsetof(model.moonshine), 176},
		{"OfflineModelConfig.fire_red_asr", unsafe.Offsetof(model.fireRedAsr), 216},
		{"OfflineModelConfig.dolphin", unsafe.Offsetof(model.dolphin), 232},
		{"OfflineModelConfig.zipformer_ctc", unsafe.Offsetof(model.zipformerCtc), 240},
		{"OfflineModelConfig.canary", unsafe.Offsetof(model.canary), 248},
		{"OfflineModelConfig.wenet_ctc", unsafe.Offsetof(model.wenetCtc), 288},
		{"OfflineModelConfig.omnilingual", unsafe.Offsetof(model.omnilingual), 296},
		{"OfflineModelConfig.medasr", unsafe.Offsetof(model.medasr), 304},
		{"OfflineModelConfig.funasr_nano", unsafe.Offsetof(model.funasrNano), 312},
		{"OfflineModelConfig.fire_red_asr_ctc", unsafe.Offsetof(model.fireRedAsrCtc), 400},
		{"OfflineModelConfig.qwen3_asr", unsafe.Offsetof(model.qwen3Asr), 408},
		{"OfflineModelConfig.cohere_transcribe", unsafe.Offsetof(model.cohereTranscribe), 472},

		{"sizeof(SherpaOnnxOfflineRecognizerConfig)", unsafe.Sizeof(rec), 608},
		{"OfflineRecognizerConfig.feat_config", unsafe.Offsetof(rec.featConfig), 0},
		{"OfflineRecognizerConfig.model_config", unsafe.Offsetof(rec.modelConfig), 8},
		{"OfflineRecognizerConfig.lm_config", unsafe.Offsetof(rec.lmConfig), 512},
		{"OfflineRecognizerConfig.decoding_method", unsafe.Offsetof(rec.decodingMethod), 528},
		{"OfflineRecognizerConfig.max_active_paths", unsafe.Offsetof(rec.maxActivePaths), 536},
		{"OfflineRecognizerConfig.hotwords_file", unsafe.Offsetof(rec.hotwordsFile), 544},
		{"OfflineRecognizerConfig.hotwords_score", unsafe.Offsetof(rec.hotwordsScore), 552},
		{"OfflineRecognizerConfig.rule_fsts", unsafe.Offsetof(rec.ruleFsts), 560},
		{"OfflineRecognizerConfig.rule_fars", unsafe.Offsetof(rec.ruleFars), 568},
		{"OfflineRecognizerConfig.blank_penalty", unsafe.Offsetof(rec.blankPenalty), 576},
		{"OfflineRecognizerConfig.hr", unsafe.Offsetof(rec.hr), 584},

		{"OfflineRecognizerResult.text", unsafe.Offsetof(res.text), 0},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s = %d, C has %d", c.name, c.got, c.want)
		}
	}
}
