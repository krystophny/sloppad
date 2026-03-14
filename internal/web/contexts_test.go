package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestLabelListAPI(t *testing.T) {
	app := newAuthedTestApp(t)

	root, err := app.store.CreateLabel("Work", nil)
	if err != nil {
		t.Fatalf("CreateLabel(root) error: %v", err)
	}
	if _, err := app.store.CreateLabel("W7x", &root.ID); err != nil {
		t.Fatalf("CreateLabel(child) error: %v", err)
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/labels", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("label list status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	payload := decodeJSONDataResponse(t, rr)
	labels, ok := payload["labels"].([]any)
	if !ok || len(labels) < 3 {
		t.Fatalf("label list payload = %#v", payload)
	}

	byName := map[string]map[string]any{}
	for _, entry := range labels {
		row, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("label row = %#v", entry)
		}
		byName[strings.ToLower(strFromAny(row["name"]))] = row
	}
	work, ok := byName["work"]
	if !ok {
		t.Fatalf("label list missing work root: %#v", payload)
	}
	private, ok := byName["private"]
	if !ok {
		t.Fatalf("label list missing private root: %#v", payload)
	}
	if got := int64FromAny(private["parent_id"]); got != 0 {
		t.Fatalf("private parent_id = %d, want 0", got)
	}
	child, ok := byName["w7x"]
	if !ok {
		t.Fatalf("label list missing child label: %#v", payload)
	}
	if got := int64FromAny(child["parent_id"]); got != root.ID {
		t.Fatalf("child parent_id = %d, want %d", got, root.ID)
	}
	if got := int64FromAny(work["id"]); got != root.ID {
		t.Fatalf("work id = %d, want %d", got, root.ID)
	}
}
