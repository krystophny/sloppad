package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParseInlineCanvasNavigationIntent(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		wantAction    string
		wantScope     string
		wantDirection string
	}{
		{name: "next slide", text: "next slide", wantAction: "navigate_canvas", wantScope: "page_or_artifact", wantDirection: "next"},
		{name: "go back", text: "go back one slide", wantAction: "navigate_canvas", wantScope: "page_or_artifact", wantDirection: "previous"},
		{name: "german next", text: "Computer, zur nächsten Folie", wantAction: "navigate_canvas", wantScope: "page_or_artifact", wantDirection: "next"},
		{name: "german previous", text: "noch einmal zurück", wantAction: "navigate_canvas", wantScope: "page_or_artifact", wantDirection: "previous"},
		{name: "next document", text: "next document", wantAction: "navigate_canvas", wantScope: "artifact", wantDirection: "next"},
		{name: "german previous document", text: "zum vorigen Dokument", wantAction: "navigate_canvas", wantScope: "artifact", wantDirection: "previous"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			action := parseInlineCanvasNavigationIntent(tc.text)
			if action == nil {
				t.Fatal("expected action")
			}
			if action.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q", action.Action, tc.wantAction)
			}
			if got := systemActionNavigationScope(action.Params); got != tc.wantScope {
				t.Fatalf("scope = %q, want %q", got, tc.wantScope)
			}
			if got := systemActionNavigationDirection(action.Params); got != tc.wantDirection {
				t.Fatalf("direction = %q, want %q", got, tc.wantDirection)
			}
		})
	}
}

func TestExecuteSystemActionNavigateCanvasReturnsSilentPayload(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	msg, payload, err := app.executeSystemAction(session.ID, session, &SystemAction{
		Action: "navigate_canvas",
		Params: map[string]interface{}{
			"scope":     "page_or_artifact",
			"direction": "next",
		},
	})
	if err != nil {
		t.Fatalf("execute navigate_canvas: %v", err)
	}
	if msg != "" {
		t.Fatalf("message = %q, want empty", msg)
	}
	if got := strFromAny(payload["type"]); got != "navigate_canvas" {
		t.Fatalf("payload type = %q, want navigate_canvas", got)
	}
	if got := strFromAny(payload["direction"]); got != "next" {
		t.Fatalf("payload direction = %q, want next", got)
	}
	if got := strFromAny(payload["scope"]); got != "page_or_artifact" {
		t.Fatalf("payload scope = %q, want page_or_artifact", got)
	}
}

func TestExecuteSystemActionPlanNavigateCanvasSkipsDoneFallback(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	message, payloads, err := app.executeSystemActionPlanUnsafe(session.ID, session, "next slide", []*SystemAction{
		{
			Action: "navigate_canvas",
			Params: map[string]interface{}{
				"scope":     "page_or_artifact",
				"direction": "next",
			},
		},
	})
	if err != nil {
		t.Fatalf("execute plan: %v", err)
	}
	if message != "" {
		t.Fatalf("message = %q, want empty", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "navigate_canvas" {
		t.Fatalf("payloads = %#v, want navigate_canvas payload", payloads)
	}
}

func TestClassifyAndExecuteSystemActionCanvasNavigationBypassesIntentLLM(t *testing.T) {
	app := newAuthedTestApp(t)
	project, err := app.ensureDefaultWorkspace()
	if err != nil {
		t.Fatalf("ensure default project: %v", err)
	}
	session, err := app.store.GetOrCreateChatSession(project.WorkspacePath)
	if err != nil {
		t.Fatalf("chat session: %v", err)
	}
	llmCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		llmCalls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	app.intentLLMURL = server.URL

	message, payloads, handled := app.classifyAndExecuteSystemAction(context.Background(), session.ID, session, "Computer, next slide")
	if !handled {
		t.Fatal("expected navigation fast path to handle next slide")
	}
	if llmCalls != 0 {
		t.Fatalf("intent llm calls = %d, want 0", llmCalls)
	}
	if message != "" {
		t.Fatalf("message = %q, want empty", message)
	}
	if len(payloads) != 1 || strFromAny(payloads[0]["type"]) != "navigate_canvas" {
		t.Fatalf("payloads = %#v, want navigate_canvas payload", payloads)
	}
	if got := strFromAny(payloads[0]["direction"]); got != "next" {
		t.Fatalf("direction = %q, want next", got)
	}
}
