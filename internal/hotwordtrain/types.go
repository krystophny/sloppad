package hotwordtrain

type Recording struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	CreatedAt  string `json:"created_at"`
	FileName   string `json:"file_name"`
	SizeBytes  int64  `json:"size_bytes"`
	DurationMS int    `json:"duration_ms"`
}

type Feedback struct {
	ID          string `json:"id"`
	RecordingID string `json:"recording_id"`
	Outcome     string `json:"outcome"`
	CreatedAt   string `json:"created_at"`
}

type FeedbackSummary struct {
	Total          int    `json:"total"`
	MissedTriggers int    `json:"missed_triggers"`
	FalseTriggers  int    `json:"false_triggers"`
	LatestOutcome  string `json:"latest_outcome,omitempty"`
	LatestAt       string `json:"latest_at,omitempty"`
}

type Model struct {
	Name       string `json:"name"`
	FileName   string `json:"file_name"`
	Path       string `json:"path"`
	CreatedAt  string `json:"created_at"`
	SizeBytes  int64  `json:"size_bytes"`
	Production bool   `json:"production"`
}

type ModelStatus struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	Message   string `json:"message,omitempty"`
	Count     int    `json:"count,omitempty"`
	Target    int    `json:"target,omitempty"`
	OutputDir string `json:"output_dir,omitempty"`
}

type Status struct {
	State            string        `json:"state"`
	Stage            string        `json:"stage"`
	Message          string        `json:"message,omitempty"`
	Error            string        `json:"error,omitempty"`
	Progress         int           `json:"progress"`
	StartedAt        string        `json:"started_at,omitempty"`
	UpdatedAt        string        `json:"updated_at,omitempty"`
	FinishedAt       string        `json:"finished_at,omitempty"`
	Models           []ModelStatus `json:"models,omitempty"`
	GeneratedSamples int           `json:"generated_samples,omitempty"`
	LatestModel      string        `json:"latest_model,omitempty"`
}

type GenerateRequest struct {
	Models      []string `json:"models"`
	SampleCount int      `json:"sample_count"`
}

type TrainRequest struct {
	ConfigPath string `json:"config_path"`
}
