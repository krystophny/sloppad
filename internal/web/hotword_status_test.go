package web

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckHotwordStatusRequiresKeywordDataSidecar(t *testing.T) {
	root := t.TempDir()
	vendorDir := hotwordVendorDir(root)
	if err := os.MkdirAll(vendorDir, 0o755); err != nil {
		t.Fatalf("mkdir vendor dir: %v", err)
	}
	for _, name := range []string{"melspectrogram.onnx", "embedding_model.onnx", hotwordModelFileName} {
		if err := os.WriteFile(filepath.Join(vendorDir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	status := checkHotwordStatus(root)
	if ready, _ := status["ready"].(bool); ready {
		t.Fatalf("ready = true, want false when %s is missing", hotwordModelFileName+".data")
	}

	missing, ok := status["missing"].([]string)
	if !ok {
		t.Fatalf("missing type = %T, want []string", status["missing"])
	}
	wantMissing := hotwordModelFileName + ".data"
	found := false
	for _, item := range missing {
		if item == wantMissing {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("missing = %v, want %q", missing, wantMissing)
	}

	if err := os.WriteFile(filepath.Join(vendorDir, wantMissing), []byte("y"), 0o644); err != nil {
		t.Fatalf("write %s: %v", wantMissing, err)
	}

	status = checkHotwordStatus(root)
	if ready, _ := status["ready"].(bool); !ready {
		t.Fatalf("ready = false, want true when sidecar exists: %#v", status)
	}
}
