package gemini

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientCompleteUsesGoogleSearchGrounding(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := strings.TrimSpace(r.URL.Path); got != "/v1beta/models/gemini-3-flash-preview:generateContent" {
			t.Fatalf("path = %q, want generateContent path", got)
		}
		if got := strings.TrimSpace(r.Header.Get("x-goog-api-key")); got != "token-gemini" {
			t.Fatalf("x-goog-api-key = %q, want token-gemini", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": "Kernel 6.20 landed changes."},
						},
					},
					"groundingMetadata": map[string]any{
						"webSearchQueries": []string{"linux kernel latest changes"},
						"groundingChunks": []map[string]any{
							{
								"web": map[string]any{
									"uri":   "https://example.com/kernel",
									"title": "Kernel Notes",
								},
							},
						},
						"groundingSupports": []map[string]any{
							{
								"segment": map[string]any{
									"text":       "Kernel 6.20 landed changes.",
									"startIndex": 0,
									"endIndex":   28,
								},
								"groundingChunkIndices": []int{0},
							},
						},
					},
				},
			},
			"usageMetadata": map[string]any{
				"totalTokenCount": 73,
			},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-gemini", DefaultModel)
	client.HTTPClient = server.Client()

	resp, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{
			{Role: "system", Content: "Answer with grounded links only."},
			{Role: "user", Content: "What changed in Linux?"},
		},
		MaxTokens:             321,
		Temperature:           0.2,
		EnableSearchGrounding: true,
	})
	if err != nil {
		t.Fatalf("Complete() error: %v", err)
	}
	if got := strings.TrimSpace(resp.Text); got != "Kernel 6.20 landed changes." {
		t.Fatalf("Text = %q, want grounded response", got)
	}
	if resp.GroundingMetadata == nil {
		t.Fatal("GroundingMetadata = nil, want grounded metadata")
	}
	if got := resp.GroundingMetadata.SearchQueries; len(got) != 1 || got[0] != "linux kernel latest changes" {
		t.Fatalf("SearchQueries = %#v, want query", got)
	}
	if got := resp.GroundingMetadata.Sources; len(got) != 1 || got[0].URI != "https://example.com/kernel" {
		t.Fatalf("Sources = %#v, want source URI", got)
	}
	if got := intFromAny(payload["generationConfig"].(map[string]any)["maxOutputTokens"]); got != 321 {
		t.Fatalf("maxOutputTokens = %d, want 321", got)
	}
	tools, ok := payload["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools = %#v, want google_search tool", payload["tools"])
	}
	systemInstruction, ok := payload["system_instruction"].(map[string]any)
	if !ok {
		t.Fatalf("system_instruction = %#v, want object", payload["system_instruction"])
	}
	parts, ok := systemInstruction["parts"].([]any)
	if !ok || len(parts) != 1 {
		t.Fatalf("system_instruction.parts = %#v, want single part", systemInstruction["parts"])
	}
}

func TestClientCompleteBacksOffAfterServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream failure"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "token-gemini", DefaultModel)
	client.HTTPClient = server.Client()
	client.UnavailableAfter = time.Minute

	_, err := client.Complete(context.Background(), CompletionRequest{
		Messages: []Message{{Role: "user", Content: "latest news"}},
	})
	if err == nil {
		t.Fatal("Complete() error = nil, want unavailable error")
	}
	if err != ErrUnavailable {
		t.Fatalf("Complete() error = %v, want ErrUnavailable", err)
	}
	if client.IsAvailable() {
		t.Fatal("IsAvailable() = true, want false during backoff")
	}
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	default:
		return 0
	}
}
