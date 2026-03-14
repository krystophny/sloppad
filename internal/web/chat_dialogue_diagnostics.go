package web

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

const dialogueDiagnosticsMaxBytes = 16 * 1024

type dialogueDiagnosticRequest struct {
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	Payload   map[string]any `json:"payload"`
}

func (a *App) handleDialogueDiagnostics(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	defer r.Body.Close()
	body := http.MaxBytesReader(w, r.Body, dialogueDiagnosticsMaxBytes)
	defer body.Close()

	var req dialogueDiagnosticRequest
	if err := json.NewDecoder(body).Decode(&req); err != nil {
		if err == io.EOF {
			writeAPIError(w, http.StatusBadRequest, "missing diagnostic payload")
			return
		}
		writeAPIError(w, http.StatusBadRequest, "invalid diagnostic payload")
		return
	}

	kind := strings.TrimSpace(req.Kind)
	if kind == "" {
		writeAPIError(w, http.StatusBadRequest, "missing diagnostic kind")
		return
	}
	sessionID := strings.TrimSpace(req.SessionID)
	payloadText := compactDialogueDiagnosticPayload(req.Payload)
	log.Printf("dialogue diagnostic session=%s kind=%s payload=%s", sessionID, kind, payloadText)
	writeAPIData(w, http.StatusOK, map[string]any{"ok": true})
}

func compactDialogueDiagnosticPayload(payload map[string]any) string {
	if len(payload) == 0 {
		return "{}"
	}
	buf, err := json.Marshal(payload)
	if err != nil {
		return "{\"error\":\"payload_marshal_failed\"}"
	}
	if len(buf) <= 1500 {
		return string(buf)
	}
	return string(buf[:1500]) + "...(truncated)"
}
