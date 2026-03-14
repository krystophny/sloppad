package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleDialogueDiagnosticsRequiresAuth(t *testing.T) {
	app := newAuthedTestApp(t)

	req := httptest.NewRequest(http.MethodPost, "/api/dialogue/diagnostics", strings.NewReader(`{"kind":"voice_capture_begin"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	app.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestHandleDialogueDiagnosticsLogsAndReturnsOK(t *testing.T) {
	app := newAuthedTestApp(t)

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/dialogue/diagnostics", map[string]any{
		"session_id": "chat-test",
		"kind":       "voice_capture_begin",
		"payload": map[string]any{
			"trigger_source": "dialogue_listen",
			"firefox_linux":  true,
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONResponse(t, rr)
	if payload["ok"] != true {
		t.Fatalf("ok = %#v, want true", payload["ok"])
	}
}
