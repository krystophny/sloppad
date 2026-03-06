package web

import (
	"net/http"
	"strings"
)

type runtimeYoloRequest struct {
	Enabled bool `json:"enabled"`
}

type runtimeDisclaimerAckRequest struct {
	Version string `json:"version"`
}

type runtimePreferencesRequest struct {
	SilentMode      *bool  `json:"silent_mode"`
	InputMode       string `json:"input_mode"`
	StartupBehavior string `json:"startup_behavior"`
}

func normalizeRuntimeInputMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "typing", "type", "text":
		return "typing"
	default:
		return "voice"
	}
}

func normalizeRuntimeStartupBehavior(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "hub_first":
		return "hub_first"
	default:
		return "hub_first"
	}
}

func (a *App) silentModeEnabled() bool {
	if a == nil || a.store == nil {
		return false
	}
	value, err := a.store.AppState(appStateSilentModeKey)
	if err != nil {
		return false
	}
	return parseBoolString(value, false)
}

func (a *App) runtimeInputMode() string {
	if a == nil || a.store == nil {
		return "voice"
	}
	value, err := a.store.AppState(appStateInputModeKey)
	if err != nil {
		return "voice"
	}
	return normalizeRuntimeInputMode(value)
}

func (a *App) runtimeStartupBehavior() string {
	if a == nil || a.store == nil {
		return "hub_first"
	}
	value, err := a.store.AppState(appStateStartupBehaviorKey)
	if err != nil {
		return "hub_first"
	}
	return normalizeRuntimeStartupBehavior(value)
}

func (a *App) setSilentModeEnabled(enabled bool) error {
	if a == nil || a.store == nil {
		return nil
	}
	if enabled {
		return a.store.SetAppState(appStateSilentModeKey, "true")
	}
	return a.store.SetAppState(appStateSilentModeKey, "false")
}

func (a *App) setRuntimeInputMode(mode string) error {
	if a == nil || a.store == nil {
		return nil
	}
	return a.store.SetAppState(appStateInputModeKey, normalizeRuntimeInputMode(mode))
}

func (a *App) setRuntimeStartupBehavior(behavior string) error {
	if a == nil || a.store == nil {
		return nil
	}
	return a.store.SetAppState(appStateStartupBehaviorKey, normalizeRuntimeStartupBehavior(behavior))
}

func (a *App) handleRuntimePreferencesUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req runtimePreferencesRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.SilentMode != nil {
		if err := a.setSilentModeEnabled(*req.SilentMode); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if strings.TrimSpace(req.InputMode) != "" {
		if err := a.setRuntimeInputMode(req.InputMode); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if strings.TrimSpace(req.StartupBehavior) != "" {
		if err := a.setRuntimeStartupBehavior(req.StartupBehavior); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]interface{}{
		"ok":               true,
		"silent_mode":      a.silentModeEnabled(),
		"input_mode":       a.runtimeInputMode(),
		"startup_behavior": a.runtimeStartupBehavior(),
	})
}

func (a *App) handleRuntimeYoloModeUpdate(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req runtimeYoloRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if err := a.setYoloModeEnabled(req.Enabled); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":      true,
		"enabled": req.Enabled,
	})
}

func (a *App) handleRuntimeDisclaimerAck(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req runtimeDisclaimerAckRequest
	if r.ContentLength > 0 {
		if err := decodeJSON(r, &req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}
	version := strings.TrimSpace(req.Version)
	if version == "" {
		version = disclaimerVersionCurrent
	}
	if err := a.setDisclaimerAckVersion(version); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{
		"ok":                     true,
		"disclaimer_ack_version": version,
	})
}
