package web

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/krystophny/tabura/internal/gemini"
)

func TestNewLoadsGeminiAPIKeyFromSecretFile(t *testing.T) {
	home := t.TempDir()
	secretDir := filepath.Join(home, ".config", "tabura", "secrets")
	if err := os.MkdirAll(secretDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(secretDir): %v", err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, DefaultGeminiSecretFile), []byte("secret-from-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(secret): %v", err)
	}

	t.Setenv("HOME", home)
	t.Setenv("TABURA_GEMINI_URL", "https://gemini.example")
	t.Setenv("TABURA_GEMINI_API_KEY", "")
	t.Setenv("TABURA_GEMINI_MODEL", "")

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	if app.geminiClient == nil {
		t.Fatal("geminiClient = nil, want configured client")
	}
	if got := app.geminiClient.APIKey; got != "secret-from-file" {
		t.Fatalf("APIKey = %q, want secret-from-file", got)
	}
	if got := app.geminiClient.Model; got != gemini.DefaultModel {
		t.Fatalf("Model = %q, want %q", got, gemini.DefaultModel)
	}
}

func TestNewLoadsGeminiAPIKeyFromEnv(t *testing.T) {
	t.Setenv("TABURA_GEMINI_URL", "https://gemini.example")
	t.Setenv("TABURA_GEMINI_API_KEY", "secret-from-env")
	t.Setenv("TABURA_GEMINI_MODEL", "gemini-2.5-flash")

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	if app.geminiClient == nil {
		t.Fatal("geminiClient = nil, want configured client")
	}
	if got := app.geminiClient.APIKey; got != "secret-from-env" {
		t.Fatalf("APIKey = %q, want secret-from-env", got)
	}
	if got := app.geminiClient.Model; got != "gemini-2.5-flash" {
		t.Fatalf("Model = %q, want gemini-2.5-flash", got)
	}
}

func TestNewDisablesGeminiWhenURLIsOff(t *testing.T) {
	t.Setenv("TABURA_GEMINI_URL", "off")
	t.Setenv("TABURA_GEMINI_API_KEY", "ignored")

	app, err := New(t.TempDir(), "", "", "", "", "", "", false)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer func() {
		_ = app.Shutdown(context.Background())
	}()

	if app.geminiClient != nil {
		t.Fatal("geminiClient != nil, want nil when TABURA_GEMINI_URL=off")
	}
}
