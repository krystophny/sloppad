package web

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/hotwordtrain"
)

const (
	hotwordModelFileName = "keyword.onnx"
)

var hotwordSharedAssetFiles = []string{
	"melspectrogram.onnx",
	"embedding_model.onnx",
}

func (a *App) hotwordProjectRoot() string {
	root := strings.TrimSpace(a.localProjectDir)
	if root == "" {
		root = "."
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return root
	}
	return abs
}

func hotwordVendorDir(root string) string {
	return filepath.Join(root, "internal", "web", "static", "vendor", "openwakeword")
}

func hotwordRuntimeDir(dataDir string) string {
	return filepath.Join(strings.TrimSpace(dataDir), "hotword-runtime")
}

func hotwordRuntimeModelPath(dataDir string) string {
	return filepath.Join(hotwordRuntimeDir(dataDir), hotwordModelFileName)
}

func hotwordModelDataPath(path string) string {
	return path + ".data"
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func checkHotwordStatus(projectRoot string, dataDir string) map[string]interface{} {
	vendorDir := hotwordVendorDir(projectRoot)
	missing := make([]string, 0, len(hotwordSharedAssetFiles)+1)
	for _, file := range hotwordSharedAssetFiles {
		if !fileExists(filepath.Join(vendorDir, file)) {
			missing = append(missing, file)
		}
	}
	modelPath := hotwordRuntimeModelPath(dataDir)
	if !fileExists(modelPath) {
		missing = append(missing, hotwordModelFileName)
	}
	ready := len(missing) == 0
	model := map[string]interface{}{
		"exists":   false,
		"file":     hotwordModelFileName,
		"revision": "",
	}
	if info, err := os.Stat(modelPath); err == nil && !info.IsDir() {
		modifiedAt := info.ModTime().UTC().Format(time.RFC3339)
		sizeBytes := info.Size()
		if dataInfo, err := os.Stat(hotwordModelDataPath(modelPath)); err == nil && !dataInfo.IsDir() {
			sizeBytes += dataInfo.Size()
		}
		model["exists"] = true
		model["modified_at"] = modifiedAt
		model["size_bytes"] = sizeBytes
		model["revision"] = fmt.Sprintf("%s:%d", modifiedAt, sizeBytes)
	}
	return map[string]interface{}{
		"ok":                   true,
		"model":                model,
		"ready":                ready,
		"missing":              missing,
		"training_in_progress": false,
	}
}

func (a *App) hotwordStatusPayload() map[string]interface{} {
	status := checkHotwordStatus(a.hotwordProjectRoot(), a.dataDir)
	if a.hotwordTrainer == nil {
		return status
	}
	active, err := a.hotwordTrainer.ActiveModel()
	if err != nil {
		return status
	}
	model, _ := status["model"].(map[string]interface{})
	if model == nil {
		model = map[string]interface{}{}
		status["model"] = model
	}
	if strings.TrimSpace(active.DisplayName) != "" {
		model["display_name"] = active.DisplayName
	}
	if strings.TrimSpace(active.Phrase) != "" {
		model["phrase"] = active.Phrase
	}
	if strings.TrimSpace(active.Source) != "" {
		model["source"] = active.Source
	}
	if strings.TrimSpace(active.SourceURL) != "" {
		model["source_url"] = active.SourceURL
	}
	if strings.TrimSpace(active.CatalogKey) != "" {
		model["catalog_key"] = active.CatalogKey
	}
	hasExternalData := a.hotwordTrainer.ActiveModelHasExternalData()
	model["has_external_data"] = hasExternalData
	if hasExternalData && !fileExists(hotwordModelDataPath(hotwordRuntimeModelPath(a.dataDir))) {
		missing, _ := status["missing"].([]string)
		missing = append(missing, hotwordModelFileName+".data")
		status["missing"] = missing
		status["ready"] = false
	}
	return status
}

func (a *App) handleHotwordStatus(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	status := a.hotwordStatusPayload()
	if a.hotwordTrainer != nil {
		training := a.hotwordTrainer.TrainingStatus()
		status["training_in_progress"] = training.State == "running"
		status["training_status"] = training
		feedback, err := a.hotwordTrainer.ListFeedback()
		if err == nil {
			status["feedback_summary"] = hotwordtrain.SummarizeFeedback(feedback)
		}
	}
	writeJSON(w, status)
}
