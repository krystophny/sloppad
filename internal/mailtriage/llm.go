package mailtriage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultSystemPrompt = `You classify emails for a personal triage system.

Return strict JSON with shape:
{"action":"inbox|cc|archive|trash","archive_label":"optional short label","confidence":0.0,"reason":"short reason","signals":["short signal"]}

Semantics:
- inbox: the user should look at this or act on it.
- cc: semantic carbon copy; useful to skim, but no action is needed.
- archive: keep for later reference, but do not keep visible.
- trash: clearly useless, spam, or safe to discard.

Rules:
- When unsure between inbox and anything else, choose inbox.
- When unsure between archive and trash, choose archive.
- Use archive_label only for clear project/reference buckets.
- Confidence is 0.0 to 1.0.
- Keep reason and signals short.`

const defaultRequestTimeout = 20 * time.Second

type OpenAIClassifier struct {
	BaseURL      string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
	Timeout      time.Duration
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c OpenAIClassifier) Classify(ctx context.Context, message Message) (Decision, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		return Decision{}, fmt.Errorf("mail triage classifier base URL is required")
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = "local"
	}
	systemPrompt := strings.TrimSpace(c.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = DefaultSystemPrompt
	}
	body, _ := json.Marshal(map[string]any{
		"model":       model,
		"temperature": 0,
		"max_tokens":  256,
		"response_format": map[string]any{
			"type": "json_object",
		},
		"chat_template_kwargs": map[string]any{
			"enable_thinking": false,
		},
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": buildUserPrompt(message)},
		},
	})
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return Decision{}, fmt.Errorf("mail triage classifier HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var payload chatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return Decision{}, err
	}
	if len(payload.Choices) == 0 {
		return Decision{}, nil
	}
	content := strings.TrimSpace(stripCodeFence(payload.Choices[0].Message.Content))
	if content == "" {
		return Decision{}, nil
	}
	var decision Decision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return Decision{}, err
	}
	decision.Model = model
	return normalizeDecision(decision), nil
}

func buildUserPrompt(message Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Message ID: %s\n", strings.TrimSpace(message.ID))
	if value := strings.TrimSpace(message.Provider); value != "" {
		fmt.Fprintf(&b, "Provider: %s\n", value)
	}
	if value := strings.TrimSpace(message.AccountLabel); value != "" {
		fmt.Fprintf(&b, "Account: %s\n", value)
	}
	if value := strings.TrimSpace(message.AccountAddress); value != "" {
		fmt.Fprintf(&b, "Account address: %s\n", value)
	}
	if value := strings.TrimSpace(message.Sender); value != "" {
		fmt.Fprintf(&b, "From: %s\n", value)
	}
	if len(message.Recipients) > 0 {
		fmt.Fprintf(&b, "Recipients: %s\n", strings.Join(message.Recipients, ", "))
	}
	if value := strings.TrimSpace(message.Subject); value != "" {
		fmt.Fprintf(&b, "Subject: %s\n", value)
	}
	if value := strings.TrimSpace(message.Snippet); value != "" {
		fmt.Fprintf(&b, "Snippet: %s\n", value)
	}
	if value := strings.TrimSpace(message.Body); value != "" {
		body := value
		if len(body) > 6000 {
			body = body[:6000]
		}
		fmt.Fprintf(&b, "Body:\n%s\n", body)
	}
	if len(message.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(message.Labels, ", "))
	}
	fmt.Fprintf(&b, "Has attachments: %t\n", message.HasAttachments)
	fmt.Fprintf(&b, "Is read: %t\n", message.IsRead)
	return strings.TrimSpace(b.String())
}

func stripCodeFence(raw string) string {
	clean := strings.TrimSpace(raw)
	if !strings.HasPrefix(clean, "```") {
		return clean
	}
	lines := strings.Split(clean, "\n")
	if len(lines) == 0 {
		return clean
	}
	lines = lines[1:]
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
