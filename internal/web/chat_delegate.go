package web

import (
	"fmt"
	"log"
	"strings"
	"time"
)

const (
	delegateStatusPollInterval = 850 * time.Millisecond
	delegateStatusPollTimeout  = 90 * time.Minute
	delegateStatusMaxEvents    = 32
)

func delegateWatchKey(sessionID, jobID string) string {
	return strings.TrimSpace(sessionID) + ":" + strings.TrimSpace(jobID)
}

func (a *App) startDelegateStatusWatcher(sessionID, canvasSessionID, jobID, model string) {
	sessionID = strings.TrimSpace(sessionID)
	canvasSessionID = strings.TrimSpace(canvasSessionID)
	jobID = strings.TrimSpace(jobID)
	model = strings.TrimSpace(model)
	if sessionID == "" || canvasSessionID == "" || jobID == "" {
		return
	}
	if !a.beginDelegateWatch(sessionID, jobID) {
		return
	}
	go func() {
		defer a.endDelegateWatch(sessionID, jobID)
		a.pollDelegateStatusLoop(sessionID, canvasSessionID, jobID, model)
	}()
}

func (a *App) beginDelegateWatch(sessionID, jobID string) bool {
	key := delegateWatchKey(sessionID, jobID)
	if key == ":" {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.delegateWatches == nil {
		a.delegateWatches = map[string]struct{}{}
	}
	if _, exists := a.delegateWatches[key]; exists {
		return false
	}
	a.delegateWatches[key] = struct{}{}
	return true
}

func (a *App) endDelegateWatch(sessionID, jobID string) {
	key := delegateWatchKey(sessionID, jobID)
	if key == ":" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.delegateWatches, key)
}

func (a *App) pollDelegateStatusLoop(sessionID, canvasSessionID, jobID, model string) {
	afterSeq := 0
	deadline := time.Now().Add(delegateStatusPollTimeout)
	for time.Now().Before(deadline) {
		port, ok := a.tunnels.getPort(canvasSessionID)
		if !ok {
			time.Sleep(delegateStatusPollInterval)
			continue
		}
		status, err := a.mcpToolsCall(port, "delegate_to_model_status", map[string]interface{}{
			"job_id":     jobID,
			"after_seq":  afterSeq,
			"max_events": delegateStatusMaxEvents,
		})
		if err != nil {
			log.Printf("delegate status poll failed session=%q job=%q: %v", sessionID, jobID, err)
			time.Sleep(delegateStatusPollInterval)
			continue
		}
		afterSeq = intFromAny(status["after_seq"], afterSeq)
		a.broadcastDelegateProgressEvents(sessionID, status)
		done, _ := status["done"].(bool)
		if !done {
			time.Sleep(delegateStatusPollInterval)
			continue
		}
		finalText := delegateFinalOutputText(jobID, status)
		if strings.TrimSpace(finalText) != "" {
			a.persistDelegateAssistantMessage(sessionID, finalText, jobID, model, strings.TrimSpace(fmt.Sprint(status["status"])))
		}
		return
	}
	log.Printf("delegate status poll timed out session=%q job=%q after %s", sessionID, jobID, delegateStatusPollTimeout)
}

func (a *App) broadcastDelegateProgressEvents(sessionID string, status map[string]interface{}) {
	rawEvents, _ := status["events"].([]interface{})
	for _, raw := range rawEvents {
		event, _ := raw.(map[string]interface{})
		if event == nil {
			continue
		}
		detail := strings.TrimSpace(fmt.Sprint(event["text"]))
		if detail == "" || detail == "<nil>" {
			continue
		}
		itemType := strings.TrimSpace(fmt.Sprint(event["type"]))
		if itemType == "" || itemType == "<nil>" {
			itemType = "status"
		}
		a.broadcastChatEvent(sessionID, map[string]interface{}{
			"type":      "item_completed",
			"turn_id":   "",
			"item_type": "delegate_" + itemType,
			"detail":    detail,
		})
	}
}

func delegateFinalOutputText(jobID string, status map[string]interface{}) string {
	state := strings.ToLower(strings.TrimSpace(fmt.Sprint(status["status"])))
	message := strings.TrimSpace(fmt.Sprint(status["message"]))
	errText := strings.TrimSpace(fmt.Sprint(status["error"]))
	if message == "<nil>" {
		message = ""
	}
	if errText == "<nil>" {
		errText = ""
	}
	if message != "" {
		if state == "failed" && errText != "" && !strings.Contains(strings.ToLower(message), strings.ToLower(errText)) {
			return message + "\n\nError: " + errText
		}
		return message
	}
	switch state {
	case "failed":
		if errText != "" {
			return fmt.Sprintf("Delegated job %s failed: %s", jobID, errText)
		}
		return fmt.Sprintf("Delegated job %s failed.", jobID)
	case "canceled":
		if errText != "" {
			return fmt.Sprintf("Delegated job %s canceled: %s", jobID, errText)
		}
		return fmt.Sprintf("Delegated job %s canceled.", jobID)
	default:
		if errText != "" {
			return fmt.Sprintf("Delegated job %s finished with error: %s", jobID, errText)
		}
		return ""
	}
}

func (a *App) persistDelegateAssistantMessage(sessionID, text, jobID, model, status string) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return
	}
	stored, err := a.store.AddChatMessage(sessionID, "assistant", trimmed, trimmed, "markdown")
	if err != nil {
		log.Printf("delegate completion persist failed session=%q job=%q: %v", sessionID, jobID, err)
		return
	}
	a.broadcastChatEvent(sessionID, map[string]interface{}{
		"type":             "assistant_output",
		"role":             "assistant",
		"id":               stored.ID,
		"turn_id":          "",
		"thread_id":        "",
		"message":          trimmed,
		"render_on_canvas": false,
		"delegate_job_id":  jobID,
		"delegate_model":   model,
		"delegate_status":  status,
	})
}
