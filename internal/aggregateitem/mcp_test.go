package aggregateitem

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestBindUsesSloptoolsSourceBindingsOverMCP(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp" {
			t.Fatalf("path = %q, want /mcp", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeMCPResult(t, w, map[string]any{
			"sphere":        "work",
			"winner_path":   "brain/commitments/winner.md",
			"binding_count": float64(3),
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	result, err := client.Bind(context.Background(), BindRequest{
		Sphere:     "work",
		WinnerPath: "brain/commitments/winner.md",
		Paths:      []string{"brain/commitments/winner.md", "brain/commitments/mail.md"},
		Outcome:    "Reply to reviewer",
		SourceBindings: []map[string]any{
			{
				"provider":          "github",
				"ref":               "sloppy-org/slopshell#725",
				"location":          map[string]any{"path": "internal/aggregateitem/mcp.go", "anchor": "L1"},
				"url":               "https://github.com/sloppy-org/slopshell/issues/725",
				"writeable":         true,
				"authoritative_for": []string{"title", "status"},
				"summary":           "review feedback",
			},
			{
				"provider":          "todoist",
				"ref":               "task-1",
				"location":          map[string]any{"path": "brain/commitments/winner.md"},
				"writeable":         false,
				"authoritative_for": []string{"status"},
			},
			{
				"provider":          "mail",
				"ref":               "AAMk-msg",
				"location":          map[string]any{"path": "mail/inbox/AAMk-msg"},
				"writeable":         true,
				"authoritative_for": []string{"title", "status"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Bind() error: %v", err)
	}
	if result["binding_count"] != float64(3) {
		t.Fatalf("binding_count = %#v, want 3", result["binding_count"])
	}

	params := objectAt(t, got, "params")
	if params["name"] != toolGTDBind {
		t.Fatalf("tool name = %q, want %q", params["name"], toolGTDBind)
	}
	args := objectAt(t, params, "arguments")
	if _, ok := args["bindings"]; ok {
		t.Fatalf("arguments used Slopshell bindings key: %#v", args)
	}
	bindings := arrayAt(t, args, "source_bindings")
	first := bindings[0].(map[string]any)
	for _, key := range []string{"location", "writeable", "authoritative_for"} {
		if _, ok := first[key]; !ok {
			t.Fatalf("first binding missing %s: %#v", key, first)
		}
	}
	if _, ok := first["authority"]; ok {
		t.Fatalf("binding carried local authority field: %#v", first)
	}
	if got := stringSliceAt(t, first, "authoritative_for"); !reflect.DeepEqual(got, []string{"title", "status"}) {
		t.Fatalf("authoritative_for = %#v", got)
	}
}

func TestSetStatusUsesSloptoolsLocalOverlayTool(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writeMCPResult(t, w, map[string]any{
			"sphere": "work",
			"path":   "brain/commitments/winner.md",
			"status": "closed",
		})
	}))
	defer server.Close()

	client, err := NewClient(server.URL, server.Client())
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	_, err = client.SetStatus(context.Background(), SetStatusRequest{
		Sphere:    "work",
		Path:      "brain/commitments/winner.md",
		Status:    "closed",
		ClosedAt:  "2026-04-29T18:00:00Z",
		ClosedVia: "slopshell",
	})
	if err != nil {
		t.Fatalf("SetStatus() error: %v", err)
	}

	params := objectAt(t, got, "params")
	if params["name"] != toolGTDSetStatus {
		t.Fatalf("tool name = %q, want %q", params["name"], toolGTDSetStatus)
	}
	args := objectAt(t, params, "arguments")
	if args["status"] != "closed" || args["closed_via"] != "slopshell" {
		t.Fatalf("status arguments = %#v", args)
	}
	if _, ok := args["local_overlay"]; ok {
		t.Fatalf("arguments should use sloptools overlay tool, got local_overlay: %#v", args)
	}
}

func TestBindRequiresProviderAndRef(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	_, err = client.Bind(context.Background(), BindRequest{
		Sphere:         "work",
		WinnerPath:     "brain/commitments/winner.md",
		SourceBindings: []map[string]any{{"provider": "github"}},
	})
	if err == nil {
		t.Fatal("Bind() error = nil, want missing ref error")
	}
}

func TestBindRequiresExtendedSourceBindingSchema(t *testing.T) {
	client, err := NewClient("http://127.0.0.1:1", nil)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	_, err = client.Bind(context.Background(), BindRequest{
		Sphere:     "work",
		WinnerPath: "brain/commitments/winner.md",
		SourceBindings: []map[string]any{{
			"provider":  "github",
			"ref":       "sloppy-org/slopshell#725",
			"location":  map[string]any{"path": "brain/commitments/winner.md"},
			"writeable": false,
		}},
	})
	if err == nil {
		t.Fatal("Bind() error = nil, want missing authoritative_for error")
	}
}

func writeMCPResult(t *testing.T, w http.ResponseWriter, structured map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"result": map[string]any{
			"structuredContent": structured,
		},
	})
	if err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func objectAt(t *testing.T, values map[string]any, key string) map[string]any {
	t.Helper()
	got, ok := values[key].(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", key, values[key])
	}
	return got
}

func arrayAt(t *testing.T, values map[string]any, key string) []any {
	t.Helper()
	got, ok := values[key].([]any)
	if !ok {
		t.Fatalf("%s = %#v, want array", key, values[key])
	}
	return got
}

func stringSliceAt(t *testing.T, values map[string]any, key string) []string {
	t.Helper()
	raw := arrayAt(t, values, key)
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		out = append(out, value.(string))
	}
	return out
}
