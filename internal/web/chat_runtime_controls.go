package web

import (
	"regexp"
	"strings"

	"github.com/krystophny/tabura/internal/modelprofile"
)

var switchModelPrefixPattern = regexp.MustCompile(`(?i)^(?:switch\s+model\s+to|switch\s+to)\s+(.+)$`)

func parseInlineRuntimeControlIntent(text string) *SystemAction {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	if action := parseInlineSwitchModelIntent(trimmed); action != nil {
		return action
	}

	lower := strings.ToLower(trimmed)
	switch lower {
	case "be quiet", "quiet please", "go quiet", "toggle silent mode":
		return &SystemAction{Action: "toggle_silent", Params: map[string]interface{}{}}
	case "toggle live dialogue", "toggle dialogue mode", "toggle dialogue":
		return &SystemAction{Action: "toggle_live_dialogue", Params: map[string]interface{}{}}
	case "cancel work", "cancel current work", "stop work", "stop current work", "stop current task", "abort current task":
		return &SystemAction{Action: "cancel_work", Params: map[string]interface{}{}}
	case "status", "status?", "show status", "show me status", "what's your status", "what is your status":
		return &SystemAction{Action: "show_status", Params: map[string]interface{}{}}
	default:
		return nil
	}
}

func parseInlineSwitchModelIntent(text string) *SystemAction {
	matches := switchModelPrefixPattern.FindStringSubmatch(strings.TrimSpace(text))
	if len(matches) != 2 {
		return nil
	}
	body := strings.TrimSpace(matches[1])
	if body == "" {
		return nil
	}
	parts := strings.Fields(body)
	if len(parts) == 0 {
		return nil
	}
	alias := modelprofile.ResolveAlias(parts[0], "")
	if alias == "" {
		return nil
	}

	params := map[string]interface{}{"alias": alias}
	if len(parts) == 1 {
		return &SystemAction{Action: "switch_model", Params: params}
	}

	remainder := strings.ToLower(strings.Join(parts[1:], " "))
	remainder = strings.NewReplacer(
		"extra high", modelprofile.ReasoningExtraHigh,
		"extra_high", modelprofile.ReasoningExtraHigh,
		"with ", "",
		"reasoning", "",
		"effort", "",
	).Replace(remainder)
	remainder = strings.TrimSpace(remainder)
	if remainder == "" {
		return &SystemAction{Action: "switch_model", Params: params}
	}
	for _, candidate := range modelprofile.ReasoningEffortsForAlias(alias) {
		if remainder != candidate {
			continue
		}
		params["effort"] = candidate
		break
	}
	return &SystemAction{Action: "switch_model", Params: params}
}
