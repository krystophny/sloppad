package web

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/krystophny/tabura/internal/gemini"
)

const geminiTurnSystemPrompt = `You are Tabura's grounded web assistant.
Answer the user directly.
Only use links supplied by grounding metadata.`

type geminiTurnResult struct {
	text     string
	grounded bool
	err      error
	latency  time.Duration
}

func (r geminiTurnResult) canClaim() bool {
	return strings.TrimSpace(r.text) != "" && r.grounded
}

func (r geminiTurnResult) canFallback() bool {
	return strings.TrimSpace(r.text) != ""
}

func (a *App) geminiModelLabel() string {
	if a == nil || a.geminiClient == nil {
		return gemini.DefaultModel
	}
	if model := strings.TrimSpace(a.geminiClient.Model); model != "" {
		return model
	}
	return gemini.DefaultModel
}

func buildGeminiTurnMessages(prompt string) []gemini.Message {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return nil
	}
	return []gemini.Message{
		{Role: "system", Content: geminiTurnSystemPrompt},
		{Role: "user", Content: trimmed},
	}
}

func (a *App) runGeminiTurn(ctx context.Context, prompt string) geminiTurnResult {
	if a == nil || a.geminiClient == nil {
		return geminiTurnResult{}
	}
	resp, err := a.geminiClient.Complete(ctx, gemini.CompletionRequest{
		Messages:              buildGeminiTurnMessages(prompt),
		MaxTokens:             512,
		Temperature:           0.2,
		EnableSearchGrounding: true,
	})
	if err != nil {
		return geminiTurnResult{err: err}
	}
	grounded := resp.GroundingMetadata != nil && len(resp.GroundingMetadata.Sources) > 0
	return geminiTurnResult{
		text:     strings.TrimSpace(renderGeminiGroundedMarkdown(resp.Text, resp.GroundingMetadata)),
		grounded: grounded,
		latency:  resp.Latency,
	}
}

func renderGeminiGroundedMarkdown(text string, metadata *gemini.GroundingMetadata) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || metadata == nil || len(metadata.Sources) == 0 {
		return trimmed
	}

	numbers := buildGeminiCitationNumbers(metadata)
	inlineText, used := injectGeminiInlineCitations(trimmed, metadata, numbers)
	if len(used) == 0 {
		for idx := range metadata.Sources {
			used = append(used, idx)
		}
	}
	sort.Slice(used, func(i, j int) bool {
		return numbers[used[i]] < numbers[used[j]]
	})

	var b strings.Builder
	b.WriteString(strings.TrimSpace(inlineText))
	b.WriteString("\n\nSources:\n")
	for _, idx := range used {
		if idx < 0 || idx >= len(metadata.Sources) {
			continue
		}
		source := metadata.Sources[idx]
		title := strings.TrimSpace(source.Title)
		if title == "" {
			title = strings.TrimSpace(source.URI)
		}
		fmt.Fprintf(&b, "%d. [%s](%s)\n", numbers[idx], title, source.URI)
	}
	return strings.TrimSpace(b.String())
}

func buildGeminiCitationNumbers(metadata *gemini.GroundingMetadata) map[int]int {
	numbers := make(map[int]int, len(metadata.Sources))
	next := 1
	for _, support := range metadata.Supports {
		for _, idx := range support.SourceIndices {
			if idx < 0 || idx >= len(metadata.Sources) {
				continue
			}
			if numbers[idx] != 0 {
				continue
			}
			numbers[idx] = next
			next++
		}
	}
	for idx := range metadata.Sources {
		if numbers[idx] != 0 {
			continue
		}
		numbers[idx] = next
		next++
	}
	return numbers
}

func injectGeminiInlineCitations(text string, metadata *gemini.GroundingMetadata, numbers map[int]int) (string, []int) {
	if len(metadata.Supports) == 0 {
		return text, nil
	}

	insertions := map[int][]int{}
	used := map[int]struct{}{}
	for _, support := range metadata.Supports {
		if support.EndIndex <= 0 || support.EndIndex > len(text) {
			continue
		}
		for _, idx := range support.SourceIndices {
			if idx < 0 || idx >= len(metadata.Sources) {
				continue
			}
			insertions[support.EndIndex] = appendUniqueInt(insertions[support.EndIndex], idx)
			used[idx] = struct{}{}
		}
	}
	if len(insertions) == 0 {
		return text, nil
	}

	positions := make([]int, 0, len(insertions))
	for pos := range insertions {
		positions = append(positions, pos)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(positions)))

	out := text
	for _, pos := range positions {
		indices := insertions[pos]
		sort.Slice(indices, func(i, j int) bool {
			return numbers[indices[i]] < numbers[indices[j]]
		})
		out = out[:pos] + renderGeminiCitationLinks(indices, metadata, numbers) + out[pos:]
	}

	orderedUsed := make([]int, 0, len(used))
	for idx := range used {
		orderedUsed = append(orderedUsed, idx)
	}
	sort.Ints(orderedUsed)
	return out, orderedUsed
}

func renderGeminiCitationLinks(indices []int, metadata *gemini.GroundingMetadata, numbers map[int]int) string {
	var b strings.Builder
	for _, idx := range indices {
		if idx < 0 || idx >= len(metadata.Sources) {
			continue
		}
		uri := strings.TrimSpace(metadata.Sources[idx].URI)
		if uri == "" {
			continue
		}
		fmt.Fprintf(&b, " [%d](%s)", numbers[idx], uri)
	}
	return b.String()
}

func appendUniqueInt(values []int, value int) []int {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
