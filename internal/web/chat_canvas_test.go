package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseCanvasActions_NoMarkers(t *testing.T) {
	actions, cleaned := parseCanvasActions("Hello world, no markers here.")
	if len(actions) != 0 {
		t.Fatalf("expected 0 actions, got %d", len(actions))
	}
	if cleaned != "Hello world, no markers here." {
		t.Fatalf("cleaned text should be unchanged, got %q", cleaned)
	}
}

func TestParseCanvasActions_SingleTextBlock(t *testing.T) {
	input := `Here is some code:

:::canvas_show{title="hello.go" kind="code"}
package main

func main() {
	println("hello")
}
:::

Let me know if you need changes.`

	actions, cleaned := parseCanvasActions(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Title != "hello.go" {
		t.Errorf("title = %q, want %q", actions[0].Title, "hello.go")
	}
	if actions[0].Kind != "code" {
		t.Errorf("kind = %q, want %q", actions[0].Kind, "code")
	}
	if actions[0].Content == "" {
		t.Error("content should not be empty")
	}
	if cleaned == input {
		t.Error("cleaned should differ from input (markers stripped)")
	}
	if len(cleaned) == 0 {
		t.Error("cleaned should not be empty")
	}
}

func TestParseCanvasActions_DefaultKindIsText(t *testing.T) {
	input := `:::canvas_show{title="Analysis"}
Some analysis content.
:::`
	actions, _ := parseCanvasActions(input)
	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Kind != "text" {
		t.Errorf("kind = %q, want %q", actions[0].Kind, "text")
	}
}

func TestParseCanvasActions_MultipleBlocks(t *testing.T) {
	input := `First block:

:::canvas_show{title="Part 1"}
Content A
:::

Second block:

:::canvas_show{title="Part 2" kind="code"}
Content B
:::

Done.`

	actions, cleaned := parseCanvasActions(input)
	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
	if actions[0].Title != "Part 1" {
		t.Errorf("actions[0].Title = %q, want %q", actions[0].Title, "Part 1")
	}
	if actions[1].Title != "Part 2" {
		t.Errorf("actions[1].Title = %q, want %q", actions[1].Title, "Part 2")
	}
	if actions[1].Kind != "code" {
		t.Errorf("actions[1].Kind = %q, want %q", actions[1].Kind, "code")
	}
	// Cleaned text should contain references
	if cleaned == "" {
		t.Error("cleaned should not be empty")
	}
}

func TestParseCanvasActions_CleanedContainsReference(t *testing.T) {
	input := `:::canvas_show{title="Report"}
Full report here.
:::`
	_, cleaned := parseCanvasActions(input)
	want := "[canvas: Report]"
	if cleaned != want {
		t.Errorf("cleaned = %q, want %q", cleaned, want)
	}
}

func TestBuildPromptFromHistory_IncludesSystemPrompt(t *testing.T) {
	prompt := buildPromptFromHistory("chat", nil, nil)
	if prompt == "" {
		t.Fatal("prompt should not be empty")
	}
	if !containsSubstring(prompt, "You are Tabura") {
		t.Error("prompt should contain system identity")
	}
	if !containsSubstring(prompt, "canvas_show") {
		t.Error("prompt should mention canvas_show markers")
	}
	if !containsSubstring(prompt, "Reply as ASSISTANT.") {
		t.Error("prompt should end with Reply as ASSISTANT")
	}
}

func TestBuildPromptFromHistory_PlanMode(t *testing.T) {
	prompt := buildPromptFromHistory("plan", nil, nil)
	if !containsSubstring(prompt, "plan mode") {
		t.Error("prompt should mention plan mode")
	}
}

func TestBuildPromptFromHistory_WithCanvasContext(t *testing.T) {
	ctx := &canvasContext{HasArtifact: true, ArtifactTitle: "Report.md", ArtifactKind: "text_artifact"}
	prompt := buildPromptFromHistory("chat", nil, ctx)
	if !containsSubstring(prompt, "Report.md") {
		t.Error("prompt should include artifact title")
	}
}

func TestResolveArtifactFilePath_AbsoluteExists(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "main.go")
	if err := os.WriteFile(f, []byte("package main"), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveArtifactFilePath(tmp, f)
	if got != f {
		t.Fatalf("expected %q, got %q", f, got)
	}
}

func TestResolveArtifactFilePath_RelativeExists(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "src", "app.go")
	if err := os.MkdirAll(filepath.Join(tmp, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f, []byte("package app"), 0644); err != nil {
		t.Fatal(err)
	}
	got := resolveArtifactFilePath(tmp, "src/app.go")
	if got != f {
		t.Fatalf("expected %q, got %q", f, got)
	}
}

func TestResolveArtifactFilePath_NoExtension(t *testing.T) {
	got := resolveArtifactFilePath("/tmp", "Meeting Notes")
	if got != "" {
		t.Fatalf("expected empty for non-file title, got %q", got)
	}
}

func TestResolveArtifactFilePath_MissingFile(t *testing.T) {
	got := resolveArtifactFilePath("/tmp", "nonexistent.go")
	if got != "" {
		t.Fatalf("expected empty for missing file, got %q", got)
	}
}

func TestResolveArtifactFilePath_EmptyTitle(t *testing.T) {
	got := resolveArtifactFilePath("/tmp", "")
	if got != "" {
		t.Fatalf("expected empty for empty title, got %q", got)
	}
}

func TestResolveArtifactFilePath_Directory(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "subdir.d")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	got := resolveArtifactFilePath(tmp, "subdir.d")
	if got != "" {
		t.Fatalf("expected empty for directory, got %q", got)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
