package web

import (
	"errors"
	"fmt"
	"strings"

	"github.com/krystophny/tabura/internal/store"
)

func parseInlineCursorIntent(text string, cursor *chatCursorContext) *SystemAction {
	cursor = normalizeChatCursorContext(cursor)
	if cursor == nil {
		return nil
	}
	normalized := normalizeItemCommandText(text)
	if normalized == "" {
		return nil
	}

	if cursor.hasPointedItem() {
		view := firstNonEmptyCursorText(cursor.View, cursor.ItemState)
		if match := itemDelegatePattern.FindStringSubmatch(strings.TrimSpace(text)); len(match) == 2 {
			if actor := cleanActorReference(match[1]); actor != "" {
				return &SystemAction{
					Action: "cursor_triage_item",
					Params: map[string]interface{}{
						"item_id":       cursor.ItemID,
						"triage_action": "delegate",
						"actor":         actor,
						"view":          view,
					},
				}
			}
		}
		switch normalized {
		case "open this", "open this item", "show this", "show this item":
			return &SystemAction{
				Action: "cursor_open_item",
				Params: map[string]interface{}{
					"item_id": cursor.ItemID,
				},
			}
		case "delete this", "delete this item", "remove this", "remove this item":
			return &SystemAction{
				Action: "cursor_triage_item",
				Params: map[string]interface{}{
					"item_id":       cursor.ItemID,
					"triage_action": "delete",
					"view":          view,
				},
			}
		case "mark this done", "mark this item done", "complete this", "finish this", "done this":
			return &SystemAction{
				Action: "cursor_triage_item",
				Params: map[string]interface{}{
					"item_id":       cursor.ItemID,
					"triage_action": "done",
					"view":          view,
				},
			}
		case "move this to waiting", "move this item to waiting", "put this in waiting", "wait on this":
			return &SystemAction{
				Action: "cursor_triage_item",
				Params: map[string]interface{}{
					"item_id":       cursor.ItemID,
					"triage_action": "waiting",
					"view":          view,
				},
			}
		case "move this to someday", "put this in someday", "someday this", "not now":
			return &SystemAction{
				Action: "cursor_triage_item",
				Params: map[string]interface{}{
					"item_id":       cursor.ItemID,
					"triage_action": "someday",
					"view":          view,
				},
			}
		}
	}

	if cursor.hasPointedPath() {
		switch normalized {
		case "open this", "open this file", "open this folder", "show this", "show this file", "show this folder":
			return &SystemAction{
				Action: "cursor_open_path",
				Params: map[string]interface{}{
					"path":   cursor.Path,
					"is_dir": cursor.IsDir,
				},
			}
		}
	}

	return nil
}

func systemActionCursorTriage(params map[string]interface{}) string {
	for _, key := range []string{"triage_action", "action"} {
		if value := strings.TrimSpace(fmt.Sprint(params[key])); value != "" && value != "<nil>" {
			return strings.ToLower(value)
		}
	}
	return ""
}

func systemActionCursorView(params map[string]interface{}) string {
	return strings.ToLower(strings.TrimSpace(systemActionStringParam(params, "view")))
}

func systemActionPathFlag(params map[string]interface{}) bool {
	switch value := params["is_dir"].(type) {
	case bool:
		return value
	case string:
		return strings.EqualFold(strings.TrimSpace(value), "true")
	default:
		return false
	}
}

func (a *App) executeCursorAction(session store.ChatSession, action *SystemAction) (string, map[string]interface{}, error) {
	switch strings.ToLower(strings.TrimSpace(action.Action)) {
	case "cursor_open_item":
		itemID := systemActionItemID(action.Params)
		if itemID <= 0 {
			return "", nil, errors.New("item_id is required")
		}
		item, err := a.store.GetItem(itemID)
		if err != nil {
			return "", nil, err
		}
		return fmt.Sprintf("Opened item %q.", item.Title), map[string]interface{}{
			"type":    "open_item_sidebar_item",
			"item_id": item.ID,
		}, nil
	case "cursor_triage_item":
		itemID := systemActionItemID(action.Params)
		if itemID <= 0 {
			return "", nil, errors.New("item_id is required")
		}
		item, err := a.store.GetItem(itemID)
		if err != nil {
			return "", nil, err
		}
		view := systemActionCursorView(action.Params)
		triageAction := systemActionCursorTriage(action.Params)
		payload := map[string]interface{}{
			"type":    "item_state_changed",
			"item_id": item.ID,
		}
		if view != "" {
			payload["view"] = view
		}
		switch triageAction {
		case "done":
			if err := a.store.TriageItemDone(item.ID); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("Marked item %q done.", item.Title), payload, nil
		case "delete":
			if err := a.store.DeleteItem(item.ID); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("Deleted item %q.", item.Title), payload, nil
		case "waiting":
			if err := a.store.UpdateItemState(item.ID, store.ItemStateWaiting); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("Moved item %q to waiting.", item.Title), payload, nil
		case "someday":
			if err := a.store.TriageItemSomeday(item.ID); err != nil {
				return "", nil, err
			}
			return fmt.Sprintf("Moved item %q to someday.", item.Title), payload, nil
		case "delegate":
			actor, err := a.resolveActorByName(systemActionActorName(action.Params))
			if err != nil {
				return "", nil, err
			}
			if err := a.store.TriageItemDelegate(item.ID, actor.ID); err != nil {
				return "", nil, err
			}
			payload["actor_id"] = actor.ID
			return fmt.Sprintf("Delegated item %q to %s.", item.Title, actor.Name), payload, nil
		default:
			return "", nil, fmt.Errorf("unsupported cursor triage action: %s", triageAction)
		}
	case "cursor_open_path":
		path := strings.TrimSpace(systemActionStringParam(action.Params, "path"))
		if path == "" {
			return "", nil, errors.New("path is required")
		}
		isDir := systemActionPathFlag(action.Params)
		if isDir {
			return fmt.Sprintf("Opened folder %q.", path), map[string]interface{}{
				"type":   "open_workspace_path",
				"path":   path,
				"is_dir": true,
			}, nil
		}
		return fmt.Sprintf("Opened file %q.", path), map[string]interface{}{
			"type":   "open_workspace_path",
			"path":   path,
			"is_dir": false,
		}, nil
	default:
		return "", nil, fmt.Errorf("unsupported cursor action: %s", action.Action)
	}
}
