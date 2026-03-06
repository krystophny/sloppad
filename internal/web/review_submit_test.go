package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewSubmitWritesMarkdownArtifact(t *testing.T) {
	app := newAuthedTestApp(t)

	rrProjects := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/projects", nil)
	if rrProjects.Code != http.StatusOK {
		t.Fatalf("projects status=%d body=%s", rrProjects.Code, rrProjects.Body.String())
	}
	var listPayload projectsListResponse
	if err := json.Unmarshal(rrProjects.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode projects response: %v", err)
	}
	projectID := ""
	for _, project := range listPayload.Projects {
		if project.Kind != "hub" {
			projectID = project.ID
			break
		}
	}
	if projectID == "" {
		t.Fatalf("expected non-hub project")
	}

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, "/api/review/submit", map[string]any{
		"project_id":     projectID,
		"artifact_kind":  "text",
		"artifact_title": "README.md",
		"artifact_path":  "README.md",
		"comments": []map[string]any{
			{
				"text": "Tighten this explanation.",
				"anchor": map[string]any{
					"title":         "README.md",
					"line":          12,
					"selected_text": "Current text",
				},
			},
		},
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("review submit status=%d body=%s", rr.Code, rr.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode review submit response: %v", err)
	}
	reviewPath := strFromAny(payload["review_markdown_path"])
	if reviewPath == "" {
		t.Fatalf("expected review_markdown_path in response")
	}

	project, err := app.store.GetProject(projectID)
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(project.RootPath, filepath.FromSlash(reviewPath)))
	if err != nil {
		t.Fatalf("read review artifact: %v", err)
	}
	text := string(content)
	if !strings.Contains(text, "Tighten this explanation.") {
		t.Fatalf("review artifact missing comment: %s", text)
	}
	if !strings.Contains(text, "Line: `12`") {
		t.Fatalf("review artifact missing line anchor: %s", text)
	}
}
