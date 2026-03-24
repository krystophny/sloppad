package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckHotwordStatusDoesNotRequireDataSidecarByDefault(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	vendorDir := hotwordVendorDir(root)
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatalf("mkdir vendor dir: %v", err)
	}
	for _, name := range hotwordSharedAssetFiles {
		if err := os.WriteFile(filepath.Join(vendorDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(hotwordRuntimeDir(dataDir), 0o755); err != nil {
		t.Fatalf("mkdir runtime dir: %v", err)
	}
	if err := os.WriteFile(hotwordRuntimeModelPath(dataDir), []byte("x"), 0o644); err != nil {
		t.Fatalf("write active model: %v", err)
	}

	status := checkHotwordStatus(root, dataDir)
	if ready, _ := status["ready"].(bool); !ready {
		t.Fatalf("ready = false, want true for single-file model: %#v", status)
	}
}

func TestHotwordStatusPayloadRequiresDataSidecarWhenActiveModelUsesExternalData(t *testing.T) {
	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root
	app.hotwordTrainer = app.hotwordTrainerForTest(root)

	vendorDir := filepath.Join(root, "internal", "web", "static", "vendor", "openwakeword")
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatalf("mkdir vendor dir: %v", err)
	}
	for _, name := range []string{"melspectrogram.onnx", "embedding_model.onnx"} {
		if err := os.WriteFile(filepath.Join(vendorDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	modelsDir := filepath.Join(app.dataDir, "hotword-train", "models")
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		t.Fatalf("mkdir models dir: %v", err)
	}
	sourcePath := filepath.Join(modelsDir, "candidate.onnx")
	if err := os.WriteFile(sourcePath, []byte("model"), 0o644); err != nil {
		t.Fatalf("write source model: %v", err)
	}
	if err := os.WriteFile(sourcePath+".data", []byte("data"), 0o644); err != nil {
		t.Fatalf("write source model data: %v", err)
	}
	if _, err := app.hotwordTrainer.DeployModel("candidate.onnx"); err != nil {
		t.Fatalf("DeployModel: %v", err)
	}
	if err := os.Remove(hotwordRuntimeModelPath(app.dataDir) + ".data"); err != nil {
		t.Fatalf("remove deployed data: %v", err)
	}

	status := app.hotwordStatusPayload()
	if ready, _ := status["ready"].(bool); ready {
		t.Fatalf("ready = true, want false when deployed sidecar is missing")
	}
	missing, _ := status["missing"].([]string)
	found := false
	for _, item := range missing {
		if item == hotwordModelFileName+".data" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing = %v, want %q", missing, hotwordModelFileName+".data")
	}
}
