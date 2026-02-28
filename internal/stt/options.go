package stt

import "strings"

// PreVADConfig controls server-side pre-transcription VAD gating.
type PreVADConfig struct {
	Enabled     bool
	ThresholdDB float64
	MinSpeechMS int
	FrameMS     int
}

// TranscribeOptions configures language handling and optional pre-VAD.
type TranscribeOptions struct {
	AllowedLanguages []string
	FallbackLanguage string
	InitialPrompt    string
	Translate        bool
	PreVAD           PreVADConfig
}

func normalizeLanguageCode(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return ""
	}
	if i := strings.Index(v, "_"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if i := strings.Index(v, "-"); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	if i := strings.Index(v, "."); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

func normalizeLanguageList(languages []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(languages))
	for _, raw := range languages {
		lang := normalizeLanguageCode(raw)
		if lang == "" || lang == "auto" {
			continue
		}
		if _, ok := seen[lang]; ok {
			continue
		}
		seen[lang] = struct{}{}
		out = append(out, lang)
	}
	return out
}
