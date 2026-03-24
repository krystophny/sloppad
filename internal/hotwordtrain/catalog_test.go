package hotwordtrain

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCatalogDownloadInstallsCommunityModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official.py":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(`MODELS = {
    "computer": {
        "download_url": "` + serverURLPlaceholder + `/downloads/computer_v0.1.tflite"
    }
}
model_class_mappings = {}`))
		case "/community.json":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tree":[{"path":"en/computer/computer_v2.onnx","type":"blob"}]}`))
		case "/downloads/computer_v0.1.onnx":
			_, _ = w.Write([]byte("official"))
		case "/raw/en/computer/computer_v2.onnx":
			_, _ = w.Write([]byte("community"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	dataDir := t.TempDir()
	projectRoot := t.TempDir()
	manager := New(dataDir, projectRoot)
	manager.SetCatalogSources(catalogSources{
		OfficialModelsURL: strings.ReplaceAll(server.URL+"/official.py", serverURLPlaceholder, server.URL),
		CommunityTreeURL:  server.URL + "/community.json",
		CommunityRawBase:  server.URL + "/raw/",
	})
	manager.SetHTTPClient(server.Client())

	entries, err := manager.ListCatalog(context.Background())
	if err != nil {
		t.Fatalf("ListCatalog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("catalog entries = %d, want 2", len(entries))
	}

	model, err := manager.DownloadCatalogModel(context.Background(), "home-assistant-community:en/computer/computer_v2.onnx")
	if err != nil {
		t.Fatalf("DownloadCatalogModel: %v", err)
	}
	if model.DisplayName != "Computer V2" {
		t.Fatalf("display name = %q, want %q", model.DisplayName, "Computer V2")
	}
	if model.CatalogKey != "home-assistant-community:en/computer/computer_v2.onnx" {
		t.Fatalf("catalog key = %q", model.CatalogKey)
	}
	if !strings.Contains(filepath.Base(model.Path), "home-assistant-community-computer-v2-") {
		t.Fatalf("path = %q, want dated downloaded filename", model.Path)
	}
	if _, err := os.Stat(model.Path); err != nil {
		t.Fatalf("downloaded model missing: %v", err)
	}
	meta, err := readModelMetadata(model.Path)
	if err != nil {
		t.Fatalf("readModelMetadata: %v", err)
	}
	if meta.CatalogKey != model.CatalogKey {
		t.Fatalf("metadata catalog key = %q, want %q", meta.CatalogKey, model.CatalogKey)
	}

	entries, err = manager.ListCatalog(context.Background())
	if err != nil {
		t.Fatalf("ListCatalog second pass: %v", err)
	}
	var installed *CatalogEntry
	for i := range entries {
		if entries[i].Key == model.CatalogKey {
			installed = &entries[i]
			break
		}
	}
	if installed == nil || !installed.Installed || installed.InstalledModel == nil {
		t.Fatalf("installed entry missing after download: %#v", installed)
	}
}

const serverURLPlaceholder = "SERVER_URL"
