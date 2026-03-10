package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const intentLLMSystemPrompt = `You are Tabura's local router. Output JSON only.
Allowed actions: switch_project, switch_workspace, list_workspace_items, list_workspaces, create_workspace, create_workspace_from_git, rename_workspace, delete_workspace, show_workspace_details, workspace_watch_start, workspace_watch_stop, workspace_watch_status, batch_work, batch_configure, review_policy, batch_limit, batch_status, assign_workspace_project, show_workspace_project, create_project, list_project_workspaces, link_workspace_artifact, list_linked_artifacts, switch_model, toggle_silent, toggle_live_dialogue, cancel_work, show_status, shell, open_file_canvas, show_calendar, show_briefing, make_item, delegate_item, snooze_item, split_items, reassign_workspace, reassign_project, clear_workspace, clear_project, capture_idea, refine_idea_note, promote_idea, apply_idea_promotion, create_github_issue, create_github_issue_split, print_item, review_someday, triage_someday, promote_someday, toggle_someday_review_nudge, show_filtered_items, sync_project, sync_sources, map_todoist_project, sync_todoist, create_todoist_task, sync_evernote, sync_bear, promote_bear_checklist, sync_zotero, cursor_open_item, cursor_triage_item, cursor_open_path, triage_item_by_title, chat.
Use {"action":"chat"} unless user clearly requests a system action.
For current-information requests (weather, web search, news, prices, schedules, latest/current updates), use {"action":"chat"} and MUST NOT use shell.
For shell-like requests use {"action":"shell","command":"..."}.
For open/show/display file requests, end with {"action":"open_file_canvas","path":"..."}.
If exact path is uncertain, use multi-step {"actions":[...]}: shell search first, then open_file_canvas with path="$last_shell_path".
For item materialization and idea/someday-review requests use make_item, delegate_item, snooze_item, split_items, reassign_workspace, reassign_project, clear_workspace, clear_project, capture_idea, refine_idea_note, promote_idea, apply_idea_promotion, create_github_issue, create_github_issue_split, review_someday, triage_someday, promote_someday, toggle_someday_review_nudge, or show_filtered_items.
Prefer case-insensitive filename search (for example -iname) and use single quotes inside JSON command strings.`

type localIntentLLMChatCompletionResponse struct {
	Choices []localIntentLLMChoice `json:"choices"`
}

type localIntentLLMChoice struct {
	Message localIntentLLMMessage `json:"message"`
}

type localIntentLLMMessage struct {
	Content string `json:"content"`
}

func parseIntentLLMProfileOptions(raw string) []string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return nil
	}
	parts := strings.Split(clean, ",")
	out := make([]string, 0, len(parts))
	seen := map[string]struct{}{}
	for _, part := range parts {
		token := strings.ToLower(strings.TrimSpace(part))
		if token == "" {
			continue
		}
		if _, exists := seen[token]; exists {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}
	return out
}

func resolveIntentLLMProfile(raw string) string {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if clean == "" {
		return DefaultIntentLLMProfile
	}
	return clean
}

func ensureIntentLLMProfileOption(options []string, profile string) []string {
	cleanProfile := strings.ToLower(strings.TrimSpace(profile))
	if cleanProfile == "" {
		cleanProfile = DefaultIntentLLMProfile
	}
	for _, option := range options {
		if strings.EqualFold(strings.TrimSpace(option), cleanProfile) {
			return options
		}
	}
	return append([]string{cleanProfile}, options...)
}

func (a *App) localIntentLLMModel() string {
	if a == nil {
		return DefaultIntentLLMModel
	}
	clean := strings.TrimSpace(a.intentLLMModel)
	if clean == "" {
		return DefaultIntentLLMModel
	}
	return clean
}

func (a *App) classifyIntentPlanWithLLM(ctx context.Context, text string) ([]*SystemAction, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(a.intentLLMURL), "/")
	if baseURL == "" {
		return nil, nil
	}
	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return nil, nil
	}
	requiresOpenCanvas := requestRequiresOpenCanvasAction(trimmedText)
	requestPlan := func(systemPrompt string, userPrompt string) ([]*SystemAction, error) {
		requestBody, _ := json.Marshal(map[string]interface{}{
			"model":       a.localIntentLLMModel(),
			"temperature": 0,
			"max_tokens":  256,
			"response_format": map[string]interface{}{
				"type": "json_object",
			},
			"chat_template_kwargs": map[string]interface{}{
				"enable_thinking": false,
			},
			"messages": []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": userPrompt},
			},
		})
		requestCtx, cancel := context.WithTimeout(ctx, intentLLMRequestTimeout)
		defer cancel()
		req, err := http.NewRequestWithContext(
			requestCtx,
			http.MethodPost,
			baseURL+"/v1/chat/completions",
			bytes.NewReader(requestBody),
		)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, intentLLMResponseLimit))
			return nil, fmt.Errorf("intent llm HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var payload localIntentLLMChatCompletionResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, intentLLMResponseLimit)).Decode(&payload); err != nil {
			return nil, err
		}
		if len(payload.Choices) == 0 {
			return nil, nil
		}
		content := strings.TrimSpace(payload.Choices[0].Message.Content)
		if content == "" {
			return nil, nil
		}
		actions, parseErr := parseSystemActionsJSON(stripCodeFence(content))
		if parseErr != nil {
			return nil, parseErr
		}
		if len(actions) == 0 {
			return nil, nil
		}
		normalized := make([]*SystemAction, 0, len(actions))
		for _, action := range actions {
			if normalizedAction := normalizeSystemActionForExecution(action, trimmedText); normalizedAction != nil {
				normalized = append(normalized, normalizedAction)
			}
		}
		if len(normalized) == 0 {
			return nil, nil
		}
		return normalized, nil
	}

	initialSystemPrompt := intentLLMSystemPrompt
	if requiresOpenCanvas {
		initialSystemPrompt += "\n\nConstraint: for explicit open/show/display file requests you MUST return an actions array whose final step is open_file_canvas. If path is uncertain, include a shell search step first and then use path=\"$last_shell_path\"."
	}
	actions, err := requestPlan(initialSystemPrompt, trimmedText)
	if err != nil {
		return nil, err
	}
	if requiresOpenCanvas && !planContainsAction(actions, "open_file_canvas") {
		previousPlanJSON := "null"
		if len(actions) > 0 {
			if encoded, marshalErr := json.Marshal(actions); marshalErr == nil {
				previousPlanJSON = string(encoded)
			}
		}
		hints := extractOpenRequestHints(trimmedText)
		hintText := "(none)"
		if len(hints) > 0 {
			hintText = strings.Join(hints, ", ")
		}
		retrySystemPrompt := intentLLMSystemPrompt + "\n\nConstraint: for explicit open/show/display file requests you MUST return an actions array whose final step is open_file_canvas. If path is uncertain, include a shell search step first and then use path=\"$last_shell_path\"."
		retryUserPrompt := "User request:\n" + trimmedText + "\n\nExtracted filename hints:\n" + hintText + "\n\nPrevious invalid plan (missing open_file_canvas or empty):\n" + previousPlanJSON
		if repaired, repairErr := requestPlan(retrySystemPrompt, retryUserPrompt); repairErr == nil && len(repaired) > 0 {
			actions = repaired
		}
		if !planContainsAction(actions, "open_file_canvas") {
			actions = ensureOpenCanvasTerminalAction(actions)
		}
		if !planContainsAction(actions, "open_file_canvas") {
			return nil, nil
		}
	}
	if len(actions) == 0 {
		return nil, nil
	}
	return actions, nil
}

func (a *App) classifyIntentWithLLM(ctx context.Context, text string) (*SystemAction, error) {
	actions, err := a.classifyIntentPlanWithLLM(ctx, text)
	if err != nil {
		return nil, err
	}
	if len(actions) == 0 {
		return nil, nil
	}
	return actions[0], nil
}
