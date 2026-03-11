package web

import (
	"net/http"
	"testing"
)

func TestContextListAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	root, err := app.store.CreateContext("Work", nil)
	if err != nil {
		t.Fatalf("CreateContext(root) error: %v", err)
	}
	if _, err := app.store.CreateContext("W7x", &root.ID); err != nil {
		t.Fatalf("CreateContext(child) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/contexts", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("context list status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONDataResponse(t, rr)
	contexts, ok := payload["contexts"].([]any)
	if !ok || len(contexts) != 2 {
		t.Fatalf("context list payload = %#v", payload)
	}
	first, ok := contexts[0].(map[string]any)
	if !ok {
		t.Fatalf("first context row = %#v", contexts[0])
	}
	if got := strFromAny(first["name"]); got != "Work" {
		t.Fatalf("first context name = %q, want %q", got, "Work")
	}
}
