package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHotwordCatalogDownloadAndDeploy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/official.py":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte(`MODELS = {
    "computer": {
        "download_url": "` + mockCatalogPlaceholder + `/downloads/computer_v0.1.tflite"
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

	app := newAuthedTestApp(t)
	root := t.TempDir()
	app.localProjectDir = root
	app.hotwordTrainer = app.hotwordTrainerForTest(root)
	app.hotwordTrainer.SetCatalogSourceURLs(
		strings.ReplaceAll(server.URL+"/official.py", mockCatalogPlaceholder, server.URL),
		server.URL+"/community.json",
		server.URL+"/raw/",
	)
	app.hotwordTrainer.SetHTTPClient(server.Client())

	catalogRR := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/hotword/catalog", nil)
	catalogPayload := decodeJSONBody(t, catalogRR.Body.String())
	catalog, ok := catalogPayload["catalog"].([]interface{})
	if !ok || len(catalog) != 2 {
		t.Fatalf("catalog payload = %#v", catalogPayload["catalog"])
	}

	downloadRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/hotword/catalog/download", map[string]any{
		"key": "home-assistant-community:en/computer/computer_v2.onnx",
	})
	downloadPayload := decodeJSONBody(t, downloadRR.Body.String())
	modelPayload, ok := downloadPayload["model"].(map[string]any)
	if !ok {
		t.Fatalf("download payload missing model: %#v", downloadPayload)
	}
	fileName, _ := modelPayload["file_name"].(string)
	if strings.TrimSpace(fileName) == "" {
		t.Fatalf("downloaded model file_name missing: %#v", modelPayload)
	}

	deployRR := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/hotword/train/deploy", map[string]any{
		"model": fileName,
	})
	deployPayload := decodeJSONBody(t, deployRR.Body.String())
	hotwordStatus, ok := deployPayload["hotword_status"].(map[string]any)
	if !ok {
		t.Fatalf("deploy payload missing hotword_status: %#v", deployPayload)
	}
	modelStatus, ok := hotwordStatus["model"].(map[string]any)
	if !ok {
		t.Fatalf("hotword status missing model: %#v", hotwordStatus)
	}
	if got := strings.TrimSpace(strFromAny(modelStatus["display_name"])); got != "Computer V2" {
		t.Fatalf("display_name = %q, want %q", got, "Computer V2")
	}
	if got := strings.TrimSpace(strFromAny(modelStatus["source"])); got != "Home Assistant Community" {
		t.Fatalf("source = %q, want %q", got, "Home Assistant Community")
	}
}

const mockCatalogPlaceholder = "MOCK_CATALOG_SERVER"
