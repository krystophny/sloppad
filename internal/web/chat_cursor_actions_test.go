package web

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/krystophny/tabura/internal/store"
)

func TestParseInlineCursorIntent_ItemAndWorkspaceTargets(t *testing.T) {
	tests := []struct {
		name       string
		text       string
		cursor     *chatCursorContext
		wantAction string
		wantTriage string
		wantPath   string
	}{
		{
			name: "item delete",
			text: "delete this",
			cursor: &chatCursorContext{
				View:      "inbox",
				ItemID:    42,
				ItemTitle: "Fix login bug",
				ItemState: store.ItemStateInbox,
			},
			wantAction: "cursor_triage_item",
			wantTriage: "delete",
		},
		{
			name: "item waiting",
			text: "move this to waiting",
			cursor: &chatCursorContext{
				View:      "inbox",
				ItemID:    42,
				ItemTitle: "Fix login bug",
				ItemState: store.ItemStateInbox,
			},
			wantAction: "cursor_triage_item",
			wantTriage: "waiting",
		},
		{
			name: "workspace path",
			text: "open this",
			cursor: &chatCursorContext{
				View:          "workspace_browser",
				WorkspaceID:   7,
				WorkspaceName: "tabura",
				Path:          "docs",
				IsDir:         true,
			},
			wantAction: "cursor_open_path",
			wantPath:   "docs",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			action := parseInlineCursorIntent(tc.text, tc.cursor)
			if action == nil {
				t.Fatal("expected cursor action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if tc.wantTriage != "" && systemActionCursorTriage(action.Params) != tc.wantTriage {
				t.Fatalf("triage_action = %q, want %q", systemActionCursorTriage(action.Params), tc.wantTriage)
			}
			if tc.wantPath != "" && systemActionStringParam(action.Params, "path") != tc.wantPath {
				t.Fatalf("path = %q, want %q", systemActionStringParam(action.Params, "path"), tc.wantPath)
			}
		})
	}
}

func TestClassifyAndExecuteSystemActionWithCursorDeletesPointedItem(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	item, err := app.store.CreateItem("Review parser cleanup", store.ItemOptions{
		WorkspaceID: &workspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionWithCursor(
		context.Background(),
		session.ID,
		session,
		"delete this",
		&chatCursorContext{
			View:          "inbox",
			ItemID:        item.ID,
			ItemTitle:     item.Title,
			ItemState:     store.ItemStateInbox,
			WorkspaceID:   workspace.ID,
			WorkspaceName: workspace.Name,
		},
	)
	if !handled {
		t.Fatal("expected cursor command to be handled")
	}
	if message != `Deleted item "Review parser cleanup".` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_state_changed" {
		t.Fatalf("payloads = %#v", payloads)
	}
	_, err = app.store.GetItem(item.ID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetItem() error = %v, want sql.ErrNoRows", err)
	}
}

func TestClassifyAndExecuteSystemActionWithCursorMovesPointedItemToWaiting(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	item, err := app.store.CreateItem("Ping release checklist", store.ItemOptions{
		WorkspaceID: &workspace.ID,
	})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionWithCursor(
		context.Background(),
		session.ID,
		session,
		"move this to waiting",
		&chatCursorContext{
			View:      "inbox",
			ItemID:    item.ID,
			ItemTitle: item.Title,
			ItemState: store.ItemStateInbox,
		},
	)
	if !handled {
		t.Fatal("expected cursor command to be handled")
	}
	if message != `Moved item "Ping release checklist" to waiting.` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "item_state_changed" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["view"]); got != "inbox" {
		t.Fatalf("payload view = %q, want inbox", got)
	}
	updated, err := app.store.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if updated.State != store.ItemStateWaiting {
		t.Fatalf("state = %q, want %q", updated.State, store.ItemStateWaiting)
	}
}

func TestClassifyAndExecuteSystemActionWithCursorOpensPointedWorkspacePath(t *testing.T) {
	app := newAuthedTestApp(t)
	app.intentLLMURL = ""
	app.intentClassifierURL = ""

	project, err := app.ensureDefaultProjectRecord()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	workspace, err := app.store.CreateWorkspace("Default", project.RootPath)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.ProjectKey)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}

	message, payloads, handled := app.classifyAndExecuteSystemActionWithCursor(
		context.Background(),
		session.ID,
		session,
		"open this",
		&chatCursorContext{
			View:          "workspace_browser",
			WorkspaceID:   workspace.ID,
			WorkspaceName: workspace.Name,
			Path:          "docs/guide.md",
			IsDir:         false,
		},
	)
	if !handled {
		t.Fatal("expected cursor command to be handled")
	}
	if message != `Opened file "docs/guide.md".` {
		t.Fatalf("message = %q", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "open_workspace_path" {
		t.Fatalf("payloads = %#v", payloads)
	}
	if got := strFromAny(payloads[0]["path"]); got != "docs/guide.md" {
		t.Fatalf("payload path = %q, want docs/guide.md", got)
	}
}
