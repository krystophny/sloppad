package web

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sloppy-org/slopshell/internal/store"
)

func decodeBrainCanvasEdgeResponse(t *testing.T, body []byte) brainCanvasEdgeResponse {
	t.Helper()
	var resp brainCanvasEdgeResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode brain canvas edge response: %v\nbody=%s", err, string(body))
	}
	return resp
}

func writeBrainCanvasNote(t *testing.T, brainRoot, rel, body string) string {
	t.Helper()
	path := filepath.Join(brainRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir note parent: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	return path
}

func createBrainCanvasNoteCard(t *testing.T, app *App, workspace store.Workspace, rel string) brainCanvasCardView {
	t.Helper()
	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, brainCanvasURL(workspace.ID, "/cards"),
		brainCanvasCardCreateRequest{Binding: brainCanvasBinding{Kind: "note", Path: rel}})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create note card %q = %d: %s", rel, rr.Code, rr.Body.String())
	}
	return decodeBrainCanvasCard(t, rr.Body.Bytes())
}

func loadBrainCanvasTestDocument(t *testing.T, brainRoot string) brainCanvasDocument {
	t.Helper()
	path := filepath.Join(brainRoot, "canvas", "default.canvas")
	t.Logf("canvas artifact: %s", path)
	doc, err := loadBrainCanvasDocument(path)
	if err != nil {
		t.Fatalf("load canvas document: %v", err)
	}
	return doc
}

func TestBrainCanvasVisualEdgeStaysCanvasLocal(t *testing.T) {
	app, workspace, brainRoot := newBrainCanvasTestApp(t)
	sourcePath := writeBrainCanvasNote(t, brainRoot, "topics/source.md", "# Source\n")
	targetPath := writeBrainCanvasNote(t, brainRoot, "topics/target.md", "# Target\n")
	source := createBrainCanvasNoteCard(t, app, workspace, "topics/source.md")
	target := createBrainCanvasNoteCard(t, app, workspace, "topics/target.md")

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, brainCanvasURL(workspace.ID, "/edges"), map[string]any{
		"from_node": source.ID,
		"to_node":   target.ID,
		"label":     "sketched relation",
		"mode":      "visual",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create visual edge = %d: %s", rr.Code, rr.Body.String())
	}
	resp := decodeBrainCanvasEdgeResponse(t, rr.Body.Bytes())
	if resp.ProposalItemID != 0 || resp.ProposalArtifactID != 0 {
		t.Fatalf("visual edge created proposal: %+v", resp)
	}

	doc := loadBrainCanvasTestDocument(t, brainRoot)
	if len(doc.Edges) != 1 || doc.Edges[0].Mode != brainCanvasEdgeModeVisual {
		t.Fatalf("visual edge not persisted as canvas state: %+v", doc.Edges)
	}
	assertFileContent(t, sourcePath, "# Source\n")
	assertFileContent(t, targetPath, "# Target\n")
}

func TestBrainCanvasPromoteEdgeCreatesReviewProposalOnly(t *testing.T) {
	app, workspace, brainRoot := newBrainCanvasTestApp(t)
	sourcePath := writeBrainCanvasNote(t, brainRoot, "topics/source.md", "# Source\n")
	targetPath := writeBrainCanvasNote(t, brainRoot, "topics/target.md", "# Target\n")
	source := createBrainCanvasNoteCard(t, app, workspace, "topics/source.md")
	target := createBrainCanvasNoteCard(t, app, workspace, "topics/target.md")

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, brainCanvasURL(workspace.ID, "/edges"), map[string]any{
		"from_node": source.ID,
		"to_node":   target.ID,
		"label":     "supports",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("create edge = %d: %s", rr.Code, rr.Body.String())
	}
	created := decodeBrainCanvasEdgeResponse(t, rr.Body.Bytes())

	promoteURL := brainCanvasURL(workspace.ID, "/edges/"+created.Edge.ID+"/promote")
	rr = doAuthedJSONRequest(t, app.Router(), http.MethodPost, promoteURL, map[string]any{
		"relation": "supports",
	})
	if rr.Code != http.StatusCreated {
		t.Fatalf("promote edge = %d: %s", rr.Code, rr.Body.String())
	}
	promoted := decodeBrainCanvasEdgeResponse(t, rr.Body.Bytes())
	if promoted.ProposalItemID == 0 || promoted.ProposalArtifactID == 0 {
		t.Fatalf("semantic edge did not create proposal: %+v", promoted)
	}
	t.Logf("proposal artifact: %s", filepath.Join(brainRoot, promoted.ProposalArtifactPath))
	assertBrainCanvasProposalItem(t, app, promoted)
	assertBrainCanvasProposalFile(t, brainRoot, promoted.ProposalArtifactPath)

	doc := loadBrainCanvasTestDocument(t, brainRoot)
	if len(doc.Edges) != 1 || doc.Edges[0].ProposalItemID != promoted.ProposalItemID {
		t.Fatalf("semantic edge proposal IDs not stored on canvas edge: %+v", doc.Edges)
	}
	assertFileContent(t, sourcePath, "# Source\n")
	assertFileContent(t, targetPath, "# Target\n")
}

func assertBrainCanvasProposalItem(t *testing.T, app *App, promoted brainCanvasEdgeResponse) {
	t.Helper()
	item, err := app.store.GetItem(promoted.ProposalItemID)
	if err != nil {
		t.Fatalf("GetItem(proposal): %v", err)
	}
	if item.State != store.ItemStateReview || item.SourceRef == nil || *item.SourceRef != promoted.Edge.ID {
		t.Fatalf("proposal item not reviewable relation proposal: %+v", item)
	}
	if item.ArtifactID == nil || *item.ArtifactID != promoted.ProposalArtifactID {
		t.Fatalf("proposal item artifact link = %v, want %d", item.ArtifactID, promoted.ProposalArtifactID)
	}
}

func assertBrainCanvasProposalFile(t *testing.T, brainRoot, relPath string) {
	t.Helper()
	proposalBytes, err := os.ReadFile(filepath.Join(brainRoot, filepath.FromSlash(relPath)))
	if err != nil {
		t.Fatalf("read proposal artifact: %v", err)
	}
	proposal := string(proposalBytes)
	for _, want := range []string{"# Canvas relation proposal", "Relation: `supports`", "[[topics/target.md]]", "Review this proposal before editing canonical notes."} {
		if !strings.Contains(proposal, want) {
			t.Fatalf("proposal artifact missing %q:\n%s", want, proposal)
		}
	}
}

func TestBrainCanvasSemanticEdgeValidationFailureHasNoSideEffects(t *testing.T) {
	app, workspace, brainRoot := newBrainCanvasTestApp(t)
	sourcePath := writeBrainCanvasNote(t, brainRoot, "topics/source.md", "# Source\n")
	targetPath := writeBrainCanvasNote(t, brainRoot, "topics/target.md", "# Target\n")
	source := createBrainCanvasNoteCard(t, app, workspace, "topics/source.md")
	target := createBrainCanvasNoteCard(t, app, workspace, "topics/target.md")

	rr := doAuthedJSONRequest(t, app.Router(), http.MethodPost, brainCanvasURL(workspace.ID, "/edges"), map[string]any{
		"from_node": source.ID,
		"to_node":   target.ID,
		"relation":  "not a slug",
		"mode":      "semantic",
	})
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid semantic edge = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}

	doc := loadBrainCanvasTestDocument(t, brainRoot)
	if len(doc.Edges) != 0 {
		t.Fatalf("invalid semantic edge changed canvas state: %+v", doc.Edges)
	}
	proposalDir := filepath.Join(brainRoot, brainCanvasProposalDir)
	if _, err := os.Stat(proposalDir); !os.IsNotExist(err) {
		t.Fatalf("invalid semantic edge created proposal dir; stat err=%v", err)
	}
	assertFileContent(t, sourcePath, "# Source\n")
	assertFileContent(t, targetPath, "# Target\n")
}

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, string(got), want)
	}
}
