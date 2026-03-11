package gemini

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	DefaultBaseURL   = "https://generativelanguage.googleapis.com"
	DefaultModel     = "gemini-3-flash-preview"
	defaultMaxTokens = 512
	defaultBackoff   = 5 * time.Minute
)

var ErrUnavailable = errors.New("gemini unavailable")

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type CompletionRequest struct {
	Messages              []Message
	MaxTokens             int
	Temperature           float64
	EnableSearchGrounding bool
}

type GroundingSource struct {
	Title string
	URI   string
}

type GroundingSupport struct {
	Text          string
	StartIndex    int
	EndIndex      int
	SourceIndices []int
}

type GroundingMetadata struct {
	SearchQueries []string
	Sources       []GroundingSource
	Supports      []GroundingSupport
}

type CompletionResponse struct {
	Text              string
	GroundingMetadata *GroundingMetadata
	TokensUsed        int
	Latency           time.Duration
}

type Client struct {
	BaseURL          string
	APIKey           string
	Model            string
	HTTPClient       *http.Client
	UnavailableAfter time.Duration

	now func() time.Time

	mu               sync.Mutex
	unavailableUntil time.Time
}

type requestContent struct {
	Role  string        `json:"role,omitempty"`
	Parts []requestPart `json:"parts"`
}

type requestPart struct {
	Text string `json:"text,omitempty"`
}

type requestBody struct {
	SystemInstruction *requestContent        `json:"system_instruction,omitempty"`
	Contents          []requestContent       `json:"contents"`
	Tools             []map[string]any       `json:"tools,omitempty"`
	GenerationConfig  map[string]interface{} `json:"generationConfig,omitempty"`
}

type responseBody struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
		GroundingMetadata *struct {
			WebSearchQueries []string `json:"webSearchQueries"`
			GroundingChunks  []struct {
				Web struct {
					URI   string `json:"uri"`
					Title string `json:"title"`
				} `json:"web"`
			} `json:"groundingChunks"`
			GroundingSupports []struct {
				Segment struct {
					StartIndex int    `json:"startIndex"`
					EndIndex   int    `json:"endIndex"`
					Text       string `json:"text"`
				} `json:"segment"`
				GroundingChunkIndices []int `json:"groundingChunkIndices"`
			} `json:"groundingSupports"`
		} `json:"groundingMetadata"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func NewClient(baseURL, apiKey, model string) *Client {
	return &Client{
		BaseURL:          strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		APIKey:           strings.TrimSpace(apiKey),
		Model:            strings.TrimSpace(model),
		UnavailableAfter: defaultBackoff,
		now:              time.Now,
	}
}

func (c *Client) IsAvailable() bool {
	if c == nil {
		return false
	}
	if strings.TrimSpace(c.BaseURL) == "" || strings.TrimSpace(c.APIKey) == "" || strings.TrimSpace(c.Model) == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.unavailableUntil.IsZero() && c.now().Before(c.unavailableUntil) {
		return false
	}
	return true
}

func (c *Client) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	if c == nil || !c.IsAvailable() {
		return nil, ErrUnavailable
	}
	if len(req.Messages) == 0 {
		return nil, errors.New("gemini request requires at least one message")
	}
	payload, err := buildRequestBody(req)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	endpoint := c.BaseURL + "/v1beta/models/" + url.PathEscape(c.Model) + ":generateContent"
	startedAt := time.Now()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-goog-api-key", c.APIKey)

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		c.markUnavailable()
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError {
		c.markUnavailable()
		return nil, ErrUnavailable
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("gemini completion failed: status %d", resp.StatusCode)
	}

	var decoded responseBody
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, err
	}
	if len(decoded.Candidates) == 0 {
		return nil, errors.New("gemini completion missing candidates")
	}

	candidate := decoded.Candidates[0]
	text := extractText(candidate.Content.Parts)
	if text == "" {
		return nil, errors.New("gemini completion missing text")
	}

	return &CompletionResponse{
		Text:              text,
		GroundingMetadata: parseGroundingMetadata(candidate.GroundingMetadata),
		TokensUsed:        decoded.UsageMetadata.TotalTokenCount,
		Latency:           time.Since(startedAt),
	}, nil
}

func buildRequestBody(req CompletionRequest) (requestBody, error) {
	system, contents := buildContents(req.Messages)
	if len(contents) == 0 {
		return requestBody{}, errors.New("gemini request requires at least one non-system message")
	}
	body := requestBody{
		SystemInstruction: system,
		Contents:          contents,
		GenerationConfig: map[string]interface{}{
			"maxOutputTokens": resolveMaxTokens(req.MaxTokens),
			"temperature":     req.Temperature,
		},
	}
	if req.EnableSearchGrounding {
		body.Tools = []map[string]any{
			{"google_search": map[string]any{}},
		}
	}
	return body, nil
}

func buildContents(messages []Message) (*requestContent, []requestContent) {
	systemParts := make([]requestPart, 0, len(messages))
	contents := make([]requestContent, 0, len(messages))
	for _, message := range messages {
		text := strings.TrimSpace(message.Content)
		if text == "" {
			continue
		}
		switch normalizeRole(message.Role) {
		case "system":
			systemParts = append(systemParts, requestPart{Text: text})
		case "model":
			contents = append(contents, requestContent{
				Role:  "model",
				Parts: []requestPart{{Text: text}},
			})
		default:
			contents = append(contents, requestContent{
				Role:  "user",
				Parts: []requestPart{{Text: text}},
			})
		}
	}
	if len(systemParts) == 0 {
		return nil, contents
	}
	return &requestContent{Parts: systemParts}, contents
}

func normalizeRole(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "system":
		return "system"
	case "assistant", "model":
		return "model"
	default:
		return "user"
	}
}

func resolveMaxTokens(value int) int {
	if value > 0 {
		return value
	}
	return defaultMaxTokens
}

func extractText(parts []struct {
	Text string `json:"text"`
}) string {
	var b strings.Builder
	for _, part := range parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(text)
		}
	}
	return strings.TrimSpace(b.String())
}

func parseGroundingMetadata(raw *struct {
	WebSearchQueries []string `json:"webSearchQueries"`
	GroundingChunks  []struct {
		Web struct {
			URI   string `json:"uri"`
			Title string `json:"title"`
		} `json:"web"`
	} `json:"groundingChunks"`
	GroundingSupports []struct {
		Segment struct {
			StartIndex int    `json:"startIndex"`
			EndIndex   int    `json:"endIndex"`
			Text       string `json:"text"`
		} `json:"segment"`
		GroundingChunkIndices []int `json:"groundingChunkIndices"`
	} `json:"groundingSupports"`
}) *GroundingMetadata {
	if raw == nil {
		return nil
	}
	metadata := &GroundingMetadata{
		SearchQueries: make([]string, 0, len(raw.WebSearchQueries)),
		Sources:       make([]GroundingSource, 0, len(raw.GroundingChunks)),
		Supports:      make([]GroundingSupport, 0, len(raw.GroundingSupports)),
	}
	for _, query := range raw.WebSearchQueries {
		if clean := strings.TrimSpace(query); clean != "" {
			metadata.SearchQueries = append(metadata.SearchQueries, clean)
		}
	}
	for _, chunk := range raw.GroundingChunks {
		uri := strings.TrimSpace(chunk.Web.URI)
		title := strings.TrimSpace(chunk.Web.Title)
		if uri == "" {
			continue
		}
		metadata.Sources = append(metadata.Sources, GroundingSource{
			Title: title,
			URI:   uri,
		})
	}
	for _, support := range raw.GroundingSupports {
		indices := make([]int, 0, len(support.GroundingChunkIndices))
		for _, idx := range support.GroundingChunkIndices {
			if idx >= 0 {
				indices = append(indices, idx)
			}
		}
		metadata.Supports = append(metadata.Supports, GroundingSupport{
			Text:          strings.TrimSpace(support.Segment.Text),
			StartIndex:    support.Segment.StartIndex,
			EndIndex:      support.Segment.EndIndex,
			SourceIndices: indices,
		})
	}
	if len(metadata.SearchQueries) == 0 && len(metadata.Sources) == 0 && len(metadata.Supports) == 0 {
		return nil
	}
	return metadata
}

func (c *Client) markUnavailable() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	until := c.now().Add(c.unavailableFor())
	if until.After(c.unavailableUntil) {
		c.unavailableUntil = until
	}
}

func (c *Client) unavailableFor() time.Duration {
	if c == nil || c.UnavailableAfter <= 0 {
		return defaultBackoff
	}
	return c.UnavailableAfter
}
