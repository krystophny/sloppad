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

func TestProjectWelcomeIncludesStartAgentCardForLinkedWorkspaces(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	linkedRoot := filepath.Join(vaultRoot, "project", "path")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir brain topics: %v", err)
	}
	if err := os.MkdirAll(linkedRoot, 0o755); err != nil {
		t.Fatalf("mkdir linked root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte("active"), 0o644); err != nil {
		t.Fatalf("write active note: %v", err)
	}
	app := newAuthedTestApp(t)
	brain, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(brain) error: %v", err)
	}
	linked, _, err := app.createWorkspace2(runtimeWorkspaceCreateRequest{
		Name:              "Linked source",
		Kind:              "linked",
		Path:              linkedRoot,
		SourceWorkspaceID: workspaceIDStr(brain.ID),
		SourcePath:        "topics/active.md",
	})
	if err != nil {
		t.Fatalf("create linked workspace: %v", err)
	}
	workspaceID := workspaceIDStr(linked.ID)
	rrWelcome := doAuthedJSONRequest(t, app.Router(), http.MethodGet, "/api/runtime/workspaces/"+workspaceID+"/welcome", nil)
	if rrWelcome.Code != http.StatusOK {
		t.Fatalf("expected linked welcome 200, got %d: %s", rrWelcome.Code, rrWelcome.Body.String())
	}
	var welcome workspaceWelcomeResponse
	if err := json.Unmarshal(rrWelcome.Body.Bytes(), &welcome); err != nil {
		t.Fatalf("decode welcome response: %v", err)
	}
	if welcome.Project.SourceWorkspaceID != workspaceIDStr(brain.ID) {
		t.Fatalf("welcome source workspace id = %q, want %q", welcome.Project.SourceWorkspaceID, workspaceIDStr(brain.ID))
	}
	if welcome.Project.SourcePath != "topics/active.md" {
		t.Fatalf("welcome source path = %q, want topics/active.md", welcome.Project.SourcePath)
	}
	var agentCard *workspaceWelcomeCard
	var originCard *workspaceWelcomeCard
	for i := range welcome.Sections {
		section := welcome.Sections[i]
		for j := range section.Cards {
			card := &section.Cards[j]
			switch card.ID {
			case "start-agent-here":
				agentCard = card
			case "return-to-source-note":
				originCard = card
			}
		}
	}
	if agentCard == nil {
		t.Fatalf("welcome missing start-agent card: %#v", welcome.Sections)
	}
	if !strings.Contains(agentCard.Description, "Use the nearest AGENTS.md or CLAUDE.md from this source folder.") {
		t.Fatalf("welcome missing nearest-instructions guidance: %s", agentCard.Description)
	}
	if originCard == nil {
		t.Fatalf("welcome missing origin card: %#v", welcome.Sections)
	}
	if originCard.Action.Type != "switch_workspace_and_open_file" {
		t.Fatalf("origin action type = %q, want switch_workspace_and_open_file", originCard.Action.Type)
	}
	if originCard.Action.WorkspaceID != workspaceIDStr(brain.ID) {
		t.Fatalf("origin workspace id = %q, want %q", originCard.Action.WorkspaceID, workspaceIDStr(brain.ID))
	}
	if originCard.Action.Path != "topics/active.md" {
		t.Fatalf("origin path = %q, want topics/active.md", originCard.Action.Path)
	}
}

func TestLinkedWorkspaceCreationCopiesSourceSettings(t *testing.T) {
	vaultRoot, _ := configureWorkPersonalGuardrail(t)
	brainRoot := filepath.Join(vaultRoot, "brain")
	linkedRoot := filepath.Join(vaultRoot, "project", "path")
	if err := os.MkdirAll(filepath.Join(brainRoot, "topics"), 0o755); err != nil {
		t.Fatalf("mkdir brain topics: %v", err)
	}
	if err := os.MkdirAll(linkedRoot, 0o755); err != nil {
		t.Fatalf("mkdir linked root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainRoot, "topics", "active.md"), []byte("active"), 0o644); err != nil {
		t.Fatalf("write active note: %v", err)
	}
	app := newAuthedTestApp(t)
	source, err := app.store.CreateWorkspace("Work brain", brainRoot, store.SphereWork)
	if err != nil {
		t.Fatalf("CreateWorkspace(source) error: %v", err)
	}
	if err := app.store.UpdateEnrichedWorkspaceChatModel(workspaceIDStr(source.ID), "gpt"); err != nil {
		t.Fatalf("UpdateWorkspaceChatModel() error: %v", err)
	}
	if err := app.store.UpdateEnrichedWorkspaceChatModelReasoningEffort(workspaceIDStr(source.ID), "xhigh"); err != nil {
		t.Fatalf("UpdateWorkspaceChatModelReasoningEffort() error: %v", err)
	}
	if err := app.store.UpdateEnrichedWorkspaceCompanionConfig(workspaceIDStr(source.ID), `{"companion_enabled":true,"idle_surface":"black"}`); err != nil {
		t.Fatalf("UpdateWorkspaceCompanionConfig() error: %v", err)
	}

	linked, created, err := app.createWorkspace2(runtimeWorkspaceCreateRequest{
		Name:              "Linked source",
		Kind:              "linked",
		Path:              linkedRoot,
		SourceWorkspaceID: workspaceIDStr(source.ID),
		SourcePath:        "topics/active.md",
	})
	if err != nil {
		t.Fatalf("create linked workspace: %v", err)
	}
	if !created {
		t.Fatal("expected linked workspace to be created")
	}
	if linked.Kind != "linked" {
		t.Fatalf("linked kind = %q, want linked", linked.Kind)
	}
	if linked.ChatModel != "gpt" {
		t.Fatalf("linked chat model = %q, want gpt", linked.ChatModel)
	}
	if linked.ChatModelReasoningEffort != "xhigh" {
		t.Fatalf("linked reasoning effort = %q, want xhigh", linked.ChatModelReasoningEffort)
	}
	if got := strings.TrimSpace(linked.CompanionConfigJSON); got != `{"companion_enabled":true,"idle_surface":"black"}` {
		t.Fatalf("linked companion config = %q", got)
	}
	if linked.SourceWorkspaceID != workspaceIDStr(source.ID) {
		t.Fatalf("linked source workspace id = %q, want %q", linked.SourceWorkspaceID, workspaceIDStr(source.ID))
	}
	if linked.SourcePath != "topics/active.md" {
		t.Fatalf("linked source path = %q, want topics/active.md", linked.SourcePath)
	}
}
