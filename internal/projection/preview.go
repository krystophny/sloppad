package projection

import "strings"

const DefaultTextPreviewLimit = 500

func PreviewText(raw string) string {
	return PreviewTextLimit(raw, DefaultTextPreviewLimit)
}

func PreviewTextLimit(raw string, limit int) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return ""
	}
	if limit <= 0 {
		limit = DefaultTextPreviewLimit
	}
	runes := []rune(clean)
	if len(runes) <= limit {
		return clean
	}
	return strings.TrimSpace(string(runes[:limit])) + "..."
}
