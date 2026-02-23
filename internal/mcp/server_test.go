package mcp

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/krystophny/tabura/internal/canvas"
)

func TestCanvasImportHandoffFileText(t *testing.T) {
	content := []byte("hello from handoff")
	sum := sha256.Sum256(content)

	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var structured map[string]interface{}
		switch name {
		case "handoff.peek":
			structured = map[string]interface{}{"handoff_id": "h1", "kind": "file"}
		case "handoff.consume":
			structured = map[string]interface{}{
				"spec_version": "handoff.v1",
				"handoff_id":   "h1",
				"kind":         "file",
				"meta": map[string]interface{}{
					"filename":   "note.txt",
					"mime_type":  "text/plain",
					"size_bytes": len(content),
					"sha256":     stringHex(sum[:]),
				},
				"payload": map[string]interface{}{
					"content_base64": base64.StdEncoding.EncodeToString(content),
				},
			}
		default:
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": structured,
			},
		})
	}))
	defer producer.Close()

	projectDir := t.TempDir()
	adapter := canvas.NewAdapter(projectDir, nil)
	s := NewServer(adapter)
	got, err := s.callTool("canvas_import_handoff", map[string]interface{}{
		"session_id":       "s1",
		"handoff_id":       "h1",
		"producer_mcp_url": producer.URL,
		"title":            "Imported File",
	})
	if err != nil {
		t.Fatalf("canvas_import_handoff failed: %v", err)
	}
	if got["kind"] != "file" {
		t.Fatalf("expected kind=file, got %#v", got["kind"])
	}
	if got["artifact_id"] == nil {
		t.Fatalf("missing artifact_id: %#v", got)
	}

	matches, err := filepath.Glob(filepath.Join(projectDir, ".tabura", "artifacts", "imports", "h1-*.txt"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one imported file, found %d", len(matches))
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read imported file: %v", err)
	}
	if string(data) != string(content) {
		t.Fatalf("imported content mismatch")
	}
}

func TestCanvasImportHandoffUnsupportedKind(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		structured := map[string]interface{}{"handoff_id": "h1", "kind": "nope"}
		if name == "handoff.consume" {
			structured["payload"] = map[string]interface{}{}
			structured["meta"] = map[string]interface{}{}
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": structured,
			},
		})
	}))
	defer producer.Close()

	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter)
	_, err := s.callTool("canvas_import_handoff", map[string]interface{}{
		"session_id":       "s1",
		"handoff_id":       "h1",
		"producer_mcp_url": producer.URL,
	})
	if err == nil {
		t.Fatalf("expected unsupported kind error")
	}
}

func TestCanvasImportHandoffMailHeadersCarriesMessageTriageMeta(t *testing.T) {
	producer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var req map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&req)
		params, _ := req["params"].(map[string]interface{})
		name, _ := params["name"].(string)
		var structured map[string]interface{}
		switch name {
		case "handoff.peek":
			structured = map[string]interface{}{"handoff_id": "mail-1", "kind": "mail_headers"}
		case "handoff.consume":
			structured = map[string]interface{}{
				"spec_version": "handoff.v1",
				"handoff_id":   "mail-1",
				"kind":         "mail_headers",
				"meta": map[string]interface{}{
					"provider": "gmail",
					"folder":   "INBOX",
					"count":    1,
				},
				"payload": map[string]interface{}{
					"headers": []map[string]interface{}{
						{
							"id":      "m1",
							"date":    "2026-02-20T12:30:00Z",
							"sender":  "a@example.com",
							"subject": "Hello",
						},
					},
				},
			}
		default:
			t.Fatalf("unexpected tool call: %s", name)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]interface{}{
				"isError":           false,
				"structuredContent": structured,
			},
		})
	}))
	defer producer.Close()

	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter)
	if _, err := s.callTool("canvas_import_handoff", map[string]interface{}{
		"session_id":       "s1",
		"handoff_id":       "mail-1",
		"producer_mcp_url": producer.URL,
	}); err != nil {
		t.Fatalf("canvas_import_handoff failed: %v", err)
	}

	status := adapter.CanvasStatus("s1")
	active, _ := status["active_artifact"].(map[string]interface{})
	if active == nil {
		t.Fatalf("expected active artifact")
	}
	meta, _ := active["meta"].(map[string]interface{})
	if meta == nil {
		t.Fatalf("expected active artifact meta, got %#v", active)
	}
	if got := meta["handoff_kind"]; got != "mail_headers" {
		t.Fatalf("expected handoff_kind=mail_headers, got %#v", got)
	}
	if got := meta["producer_mcp_url"]; got != producer.URL {
		t.Fatalf("expected producer_mcp_url=%s, got %#v", producer.URL, got)
	}
	triage, _ := meta["message_triage_v1"].(map[string]interface{})
	if triage == nil {
		t.Fatalf("expected message_triage_v1 metadata, got %#v", meta)
	}
	if got := triage["provider"]; got != "gmail" {
		t.Fatalf("expected provider=gmail, got %#v", got)
	}
	headers, _ := triage["headers"].([]interface{})
	if len(headers) != 1 {
		t.Fatalf("expected one header, got %#v", triage["headers"])
	}
	first, _ := headers[0].(map[string]interface{})
	if first["id"] != "m1" {
		t.Fatalf("expected header id m1, got %#v", first["id"])
	}
}

func stringHex(b []byte) string {
	const hextable = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hextable[v>>4]
		out[i*2+1] = hextable[v&0x0f]
	}
	return string(out)
}

func TestResolveModelAlias(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"codex", "gpt-5.3-codex"},
		{"CODEX", "gpt-5.3-codex"},
		{"spark", "gpt-5.3-codex-spark"},
		{"gpt", "gpt-5.2"},
		{"", "gpt-5.3-codex"},
		{"  ", "gpt-5.3-codex"},
		{"gpt-5.2", "gpt-5.2"},
		{"some-custom-model", "some-custom-model"},
	}
	for _, tc := range tests {
		got := resolveModelAlias(tc.input)
		if got != tc.want {
			t.Errorf("resolveModelAlias(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestAssembleDelegatePrompt(t *testing.T) {
	t.Run("all fields", func(t *testing.T) {
		got := assembleDelegatePrompt("Be thorough.", "User asked about X.", "Analyze the code.")
		if !strings.Contains(got, "## Instructions") {
			t.Error("missing Instructions section")
		}
		if !strings.Contains(got, "Be thorough.") {
			t.Error("missing system_prompt content")
		}
		if !strings.Contains(got, "## Context") {
			t.Error("missing Context section")
		}
		if !strings.Contains(got, "User asked about X.") {
			t.Error("missing context content")
		}
		if !strings.Contains(got, "## Task") {
			t.Error("missing Task section")
		}
		if !strings.Contains(got, "Analyze the code.") {
			t.Error("missing prompt content")
		}
	})

	t.Run("prompt only uses default system prompt", func(t *testing.T) {
		got := assembleDelegatePrompt("", "", "Do something.")
		if !strings.Contains(got, "## Instructions") {
			t.Error("should have Instructions section with default preamble")
		}
		if !strings.Contains(got, "Edit files directly") {
			t.Error("default preamble should tell delegate to edit files directly")
		}
		if !strings.Contains(got, "Do NOT output patches") {
			t.Error("default preamble should tell delegate not to output patches")
		}
		if strings.Contains(got, "## Context") {
			t.Error("should not have Context section when empty")
		}
		if !strings.Contains(got, "## Task") {
			t.Error("missing Task section")
		}
		if !strings.Contains(got, "Do something.") {
			t.Error("missing prompt content")
		}
	})

	t.Run("explicit system_prompt overrides default", func(t *testing.T) {
		got := assembleDelegatePrompt("Custom instructions.", "", "Do something.")
		if !strings.Contains(got, "Custom instructions.") {
			t.Error("explicit system_prompt should be used")
		}
		if strings.Contains(got, "Edit files directly") {
			t.Error("default preamble should not appear when explicit system_prompt provided")
		}
	})
}

func TestToolDefinitionsEmitsProperties(t *testing.T) {
	defs := toolDefinitions()
	var delegateDef map[string]interface{}
	for _, d := range defs {
		if d["name"] == "delegate_to_model" {
			delegateDef = d
			break
		}
	}
	if delegateDef == nil {
		t.Fatal("delegate_to_model not found in tool definitions")
	}
	schema, _ := delegateDef["inputSchema"].(map[string]interface{})
	if schema == nil {
		t.Fatal("missing inputSchema")
	}
	props, _ := schema["properties"].(map[string]interface{})
	if props == nil {
		t.Fatal("missing properties in inputSchema")
	}
	for _, key := range []string{"prompt", "model", "context", "system_prompt", "cwd", "timeout_seconds"} {
		if props[key] == nil {
			t.Errorf("missing property %q", key)
		}
	}
	modelProp, _ := props["model"].(map[string]interface{})
	if modelProp == nil {
		t.Fatal("model property is not a map")
	}
	if modelProp["enum"] == nil {
		t.Error("model property missing enum")
	}
}

func TestDelegateToModelRequiresAppServer(t *testing.T) {
	adapter := canvas.NewAdapter(t.TempDir(), nil)
	s := NewServer(adapter)
	_, err := s.callTool("delegate_to_model", map[string]interface{}{"prompt": "test"})
	if err == nil {
		t.Fatal("expected error for missing app-server client")
	}
	if !strings.Contains(err.Error(), "app-server client is not configured") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDelegateToModelDefaultsToCodex(t *testing.T) {
	got := resolveModelAlias("")
	if got != "gpt-5.3-codex" {
		t.Errorf("empty model should default to codex, got %q", got)
	}
}
