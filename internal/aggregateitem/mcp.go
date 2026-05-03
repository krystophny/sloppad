package aggregateitem

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sloppy-org/slopshell/internal/mcpclient"
)

const (
	toolGTDBind      = "brain.gtd.bind"
	toolGTDDedupScan = "brain.gtd.dedup_scan"
	toolGTDIngest    = "brain.gtd.ingest"
	toolBrainParse   = "brain.note.parse"
	toolGTDSetStatus = "brain.gtd.set_status"
)

type Client struct {
	client *mcpclient.Client
}

type BindRequest struct {
	ConfigPath     string
	Sphere         string
	WinnerPath     string
	Paths          []string
	Outcome        string
	SourceBindings []SourceBinding
}

type ScanRequest struct {
	ConfigPath             string
	DedupConfig            string
	Sphere                 string
	DeterministicThreshold float64
	LLMThreshold           float64
	CandidateThreshold     float64
}

type IngestRequest struct {
	ConfigPath    string
	SourcesConfig string
	Sphere        string
	Source        string
	Paths         []string
	Path          string
}

type ParseCommitmentRequest struct {
	ConfigPath string
	Sphere     string
	Path       string
}

type SetStatusRequest struct {
	ConfigPath    string
	SourcesConfig string
	Sphere        string
	Path          string
	CommitmentID  string
	Status        string
	ClosedAt      string
	ClosedVia     string
	MailAction    string
	MailLabel     string
	MailFolder    string
	TodoistListID string
}

func NewClient(rawEndpoint string, client *http.Client) (*Client, error) {
	ep, err := mcpclient.ParseEndpoint(rawEndpoint)
	if err != nil {
		return nil, err
	}
	mcp, err := mcpclient.New(ep, client, 20*time.Second)
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			return nil, errors.New("aggregate item MCP endpoint is required")
		}
		return nil, err
	}
	return &Client{client: mcp}, nil
}

func (c *Client) Bind(ctx context.Context, req BindRequest) (map[string]any, error) {
	args, err := req.arguments()
	if err != nil {
		return nil, err
	}
	return c.call(ctx, toolGTDBind, args)
}

func (c *Client) Scan(ctx context.Context, req ScanRequest) (ScanResult, error) {
	args, err := req.arguments()
	if err != nil {
		return ScanResult{}, err
	}
	result, err := c.call(ctx, toolGTDDedupScan, args)
	if err != nil {
		return ScanResult{}, err
	}
	return decodeStructuredField[ScanResult](result, "dedup")
}

func (c *Client) Ingest(ctx context.Context, req IngestRequest) (map[string]any, error) {
	args, err := req.arguments()
	if err != nil {
		return nil, err
	}
	return c.call(ctx, toolGTDIngest, args)
}

func (c *Client) ParseCommitment(ctx context.Context, req ParseCommitmentRequest) (Commitment, error) {
	args, err := req.arguments()
	if err != nil {
		return Commitment{}, err
	}
	result, err := c.call(ctx, toolBrainParse, args)
	if err != nil {
		return Commitment{}, err
	}
	return decodeStructuredField[Commitment](result, "commitment")
}

func (c *Client) SetStatus(ctx context.Context, req SetStatusRequest) (map[string]any, error) {
	args, err := req.arguments()
	if err != nil {
		return nil, err
	}
	return c.call(ctx, toolGTDSetStatus, args)
}

func (r BindRequest) arguments() (map[string]any, error) {
	args := map[string]any{}
	addString(args, "config_path", r.ConfigPath)
	addString(args, "sphere", r.Sphere)
	addString(args, "winner_path", r.WinnerPath)
	addString(args, "outcome", r.Outcome)
	addStringSlice(args, "paths", r.Paths)
	if len(r.SourceBindings) > 0 {
		bindings, err := sourceBindingsArg(r.SourceBindings)
		if err != nil {
			return nil, err
		}
		args["source_bindings"] = bindings
	}
	if err := requireArgs(args, "sphere", "winner_path"); err != nil {
		return nil, err
	}
	return args, nil
}

func (r ScanRequest) arguments() (map[string]any, error) {
	args := map[string]any{}
	addString(args, "config_path", r.ConfigPath)
	addString(args, "dedup_config", r.DedupConfig)
	addString(args, "sphere", r.Sphere)
	addPositiveFloat(args, "deterministic_threshold", r.DeterministicThreshold)
	addPositiveFloat(args, "llm_threshold", r.LLMThreshold)
	addPositiveFloat(args, "candidate_threshold", r.CandidateThreshold)
	if err := requireArgs(args, "sphere"); err != nil {
		return nil, err
	}
	return args, nil
}

func (r IngestRequest) arguments() (map[string]any, error) {
	args := map[string]any{}
	addString(args, "config_path", r.ConfigPath)
	addString(args, "sources_config", r.SourcesConfig)
	addString(args, "sphere", r.Sphere)
	addString(args, "source", r.Source)
	addString(args, "path", r.Path)
	addStringSlice(args, "paths", r.Paths)
	if err := requireArgs(args, "sphere", "source"); err != nil {
		return nil, err
	}
	return args, nil
}

func (r ParseCommitmentRequest) arguments() (map[string]any, error) {
	args := map[string]any{}
	addString(args, "config_path", r.ConfigPath)
	addString(args, "sphere", r.Sphere)
	addString(args, "path", r.Path)
	if err := requireArgs(args, "sphere", "path"); err != nil {
		return nil, err
	}
	return args, nil
}

func (r SetStatusRequest) arguments() (map[string]any, error) {
	args := map[string]any{}
	addString(args, "config_path", r.ConfigPath)
	addString(args, "sources_config", r.SourcesConfig)
	addString(args, "sphere", r.Sphere)
	addString(args, "path", r.Path)
	addString(args, "commitment_id", r.CommitmentID)
	addString(args, "status", r.Status)
	addString(args, "closed_at", r.ClosedAt)
	addString(args, "closed_via", r.ClosedVia)
	addString(args, "mail_action", r.MailAction)
	addString(args, "mail_label", r.MailLabel)
	addString(args, "mail_folder", r.MailFolder)
	addString(args, "todoist_list_id", r.TodoistListID)
	if err := requireArgs(args, "sphere", "status"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(r.Path) == "" && strings.TrimSpace(r.CommitmentID) == "" {
		return nil, errors.New("path or commitment_id is required")
	}
	return args, nil
}

func (c *Client) call(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return c.client.CallTool(ctx, name, arguments)
}

func sourceBindingsArg(bindings []SourceBinding) ([]map[string]any, error) {
	out := make([]map[string]any, 0, len(bindings))
	for index, binding := range bindings {
		clean, err := sourceBindingMap(binding)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(stringArg(clean, "provider")) == "" {
			return nil, fmt.Errorf("source_bindings[%d].provider is required", index)
		}
		if strings.TrimSpace(stringArg(clean, "ref")) == "" {
			return nil, fmt.Errorf("source_bindings[%d].ref is required", index)
		}
		if _, ok := clean["location"].(map[string]any); !ok {
			return nil, fmt.Errorf("source_bindings[%d].location is required", index)
		}
		if _, ok := clean["writeable"].(bool); !ok {
			return nil, fmt.Errorf("source_bindings[%d].writeable is required", index)
		}
		if len(stringListArg(clean, "authoritative_for")) == 0 {
			return nil, fmt.Errorf("source_bindings[%d].authoritative_for is required", index)
		}
		out = append(out, clean)
	}
	return out, nil
}

func sourceBindingMap(binding SourceBinding) (map[string]any, error) {
	data, err := json.Marshal(binding)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return cleanBinding(raw), nil
}

func cleanBinding(binding map[string]any) map[string]any {
	clean := make(map[string]any, len(binding))
	for key, value := range binding {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		switch typed := value.(type) {
		case string:
			if trimmed := strings.TrimSpace(typed); trimmed != "" {
				clean[key] = trimmed
			}
		case []string:
			if values := cleanStrings(typed); len(values) > 0 {
				clean[key] = values
			}
		default:
			if value != nil {
				clean[key] = value
			}
		}
	}
	return clean
}

func decodeStructuredField[T any](structured map[string]any, key string) (T, error) {
	var out T
	raw, ok := structured[key]
	if !ok {
		return out, fmt.Errorf("MCP call failed: missing %s", key)
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return out, err
	}
	return out, nil
}

func addString(args map[string]any, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		args[key] = value
	}
}

func addStringSlice(args map[string]any, key string, values []string) {
	if clean := cleanStrings(values); len(clean) > 0 {
		args[key] = clean
	}
}

func addPositiveFloat(args map[string]any, key string, value float64) {
	if value > 0 {
		args[key] = value
	}
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func requireArgs(args map[string]any, keys ...string) error {
	for _, key := range keys {
		if strings.TrimSpace(stringArg(args, key)) == "" {
			return fmt.Errorf("%s is required", key)
		}
	}
	return nil
}

func stringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func stringListArg(args map[string]any, key string) []string {
	switch value := args[key].(type) {
	case []string:
		return cleanStrings(value)
	case []any:
		out := make([]string, 0, len(value))
		for _, item := range value {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" && text != "<nil>" {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}
