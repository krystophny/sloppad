package hotwordtrain

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	feedbackOutcomeMissedTrigger = "missed_trigger"
	feedbackOutcomeFalseTrigger  = "false_trigger"
	feedbackDirName              = "feedback"
)

func (m *Manager) feedbackDir() string {
	return filepath.Join(m.dataDir, "hotword-train", feedbackDirName)
}

func normalizeFeedbackOutcome(outcome string) string {
	switch strings.TrimSpace(strings.ToLower(outcome)) {
	case "missed", "miss", "missed_trigger":
		return feedbackOutcomeMissedTrigger
	case "false", "false_positive", "false_trigger":
		return feedbackOutcomeFalseTrigger
	default:
		return ""
	}
}

func (m *Manager) SaveFeedback(recordingID, outcome string) (Feedback, error) {
	recording, err := m.recordingByID(recordingID)
	if err != nil {
		return Feedback{}, err
	}
	if recording.Kind != recordingKindTest {
		return Feedback{}, errors.New("feedback requires a test recording")
	}
	normalized := normalizeFeedbackOutcome(outcome)
	if normalized == "" {
		return Feedback{}, errors.New("invalid feedback outcome")
	}
	if err := m.ensureDir(m.feedbackDir()); err != nil {
		return Feedback{}, err
	}
	feedback := Feedback{
		ID:          time.Now().UTC().Format("20060102T150405") + "-" + randomID(),
		RecordingID: recording.ID,
		Outcome:     normalized,
		CreatedAt:   nowRFC3339(),
	}
	if err := writeJSONFile(filepath.Join(m.feedbackDir(), feedback.ID+".json"), feedback); err != nil {
		return Feedback{}, err
	}
	return feedback, nil
}

func (m *Manager) ListFeedback() ([]Feedback, error) {
	entries, err := os.ReadDir(m.feedbackDir())
	if err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Feedback, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".json") {
			continue
		}
		var feedback Feedback
		if err := decodeJSONFile(filepath.Join(m.feedbackDir(), entry.Name()), &feedback); err != nil {
			continue
		}
		out = append(out, feedback)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt > out[j].CreatedAt
		}
		return out[i].ID > out[j].ID
	})
	return out, nil
}

func SummarizeFeedback(entries []Feedback) FeedbackSummary {
	summary := FeedbackSummary{Total: len(entries)}
	for _, entry := range entries {
		switch entry.Outcome {
		case feedbackOutcomeMissedTrigger:
			summary.MissedTriggers++
		case feedbackOutcomeFalseTrigger:
			summary.FalseTriggers++
		}
		if summary.LatestAt == "" || entry.CreatedAt > summary.LatestAt {
			summary.LatestAt = entry.CreatedAt
			summary.LatestOutcome = entry.Outcome
		}
	}
	return summary
}
