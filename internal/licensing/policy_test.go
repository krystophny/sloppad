package licensing

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestThirdPartyLicenseInventoryIncludesRequiredComponents(t *testing.T) {
	t.Parallel()

	content := readRepoFile(t, "THIRD_PARTY_LICENSES.md")
	requireContainsAll(t, content,
		"# Third-Party Licenses",
		"| Piper TTS Python runtime | GPL |",
		"| Piper voice models | Per-model",
		"| ffmpeg | GPL/LGPL",
		"| voxtype | MIT |",
		"not linked into the Go binary",
	)
}

func TestModelDownloadPolicyDefinesTierRulesAndCurrentDownloads(t *testing.T) {
	t.Parallel()

	content := readRepoFile(t, "docs", "model-download-policy.md")
	requireContainsAll(t, content,
		"# Model Download Policy",
		"### Tier 1: Silent",
		"### Tier 2: Notice + Opt-Out",
		"| Piper TTS runtime | PyPI (`piper-tts`) | GPL | Tier 2 |",
		"| Piper voice models | Hugging Face (`rhasspy/piper-voices`) | Per-model | Tier 2 |",
		"Never statically or dynamically link Piper runtime libraries into the Go",
		"Display model/runtime notice text before Tier-2 downloads in setup scripts.",
	)
}

func TestPiperSetupScriptContainsTier2NoticeAndOptOutFlow(t *testing.T) {
	t.Parallel()

	content := readRepoFile(t, "scripts", "setup-tabura-piper-tts.sh")
	requireContainsAll(t, content,
		"=== Piper TTS Tier-2 Notice ===",
		"Runtime license: GPL",
		"confirm_default_yes",
		"TABURA_ASSUME_YES",
		"Continue with Piper TTS setup?",
		"Skipping model download:",
	)
}

func TestNoKnownGPLSidecarDependenciesAreLinkedIntoGoBinary(t *testing.T) {
	t.Parallel()

	cmd := exec.Command("go", "list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}{{end}}", "./cmd/tabura")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list deps failed: %v\n%s", err, string(out))
	}

	forbidden := []string{"piper", "ffmpeg", "voxtype", "openwakeword", "llama.cpp"}
	for _, line := range strings.Split(string(out), "\n") {
		module := strings.TrimSpace(line)
		if module == "" {
			continue
		}
		lowered := strings.ToLower(module)
		for _, token := range forbidden {
			if strings.Contains(lowered, token) {
				t.Fatalf("forbidden sidecar dependency token %q found in module path %q", token, module)
			}
		}
	}
}

func readRepoFile(t *testing.T, pathParts ...string) string {
	t.Helper()

	path := filepath.Join(append([]string{repoRoot(t)}, pathParts...)...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(content)
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found from current working directory")
		}
		dir = parent
	}
}

func requireContainsAll(t *testing.T, content string, required ...string) {
	t.Helper()

	for _, marker := range required {
		if !strings.Contains(content, marker) {
			t.Fatalf("missing required marker %q", marker)
		}
	}
}
