package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSTTConfigGetRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/stt/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("authed GET /api/stt/config status = %d, want 200", rr.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/stt/config", nil)
	unauth := httptest.NewRecorder()
	app.Router().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauth GET /api/stt/config status = %d, want 401", unauth.Code)
	}
}

func TestSTTConfigDefaultAndRoundTrip(t *testing.T) {
	t.Setenv("TABURA_STT_ALLOWED_LANGUAGES", "en,de")
	t.Setenv("TABURA_STT_PREVAD_ENABLED", "true")
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/stt/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	var cfg sttConfigSnapshot
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if len(cfg.AllowedLanguages) < 2 {
		t.Fatalf("allowed languages=%v, want at least [en de]", cfg.AllowedLanguages)
	}
	if !cfg.PreVADEnabled {
		t.Fatal("pre_vad_enabled=false, want true")
	}

	payload := map[string]any{
		"allowed_languages":     []string{"de", "en"},
		"fallback_language":     "de",
		"initial_prompt":        "Use en/de terminology.",
		"pre_vad_enabled":       false,
		"pre_vad_threshold_db":  -52.0,
		"pre_vad_min_speech_ms": 240,
	}
	rr = doAuthedJSONRequest(t, app.Router(), http.MethodPut, "/api/stt/config", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, want 200", rr.Code)
	}

	rr = doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/stt/config", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", rr.Code)
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config after PUT: %v", err)
	}
	if cfg.FallbackLanguage != "de" {
		t.Fatalf("fallback_language=%q, want de", cfg.FallbackLanguage)
	}
	if cfg.InitialPrompt != "Use en/de terminology." {
		t.Fatalf("initial_prompt=%q", cfg.InitialPrompt)
	}
	if cfg.PreVADEnabled {
		t.Fatal("pre_vad_enabled=true, want false")
	}
	if cfg.PreVADThreshold != -52.0 {
		t.Fatalf("pre_vad_threshold_db=%v, want -52", cfg.PreVADThreshold)
	}
	if cfg.PreVADMinSpeech != 240 {
		t.Fatalf("pre_vad_min_speech_ms=%d, want 240", cfg.PreVADMinSpeech)
	}

	opts := app.sttTranscribeOptions()
	if len(opts.AllowedLanguages) != 2 || opts.FallbackLanguage != "de" {
		t.Fatalf("stt options mismatch: %+v", opts)
	}
	if opts.InitialPrompt != "Use en/de terminology." {
		t.Fatalf("opts.InitialPrompt=%q", opts.InitialPrompt)
	}
	if opts.PreVAD.Enabled {
		t.Fatal("opts.PreVAD.Enabled=true, want false")
	}
}
