package web

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	defaultVoxTypeMCPURL = "http://127.0.0.1:8091/mcp"

	sttActionStart  = "start"
	sttActionAppend = "append"
	sttActionStop   = "stop"
	sttActionCancel = "cancel"
)

type pushToPromptRequest struct {
	Action         string `json:"action"`
	SessionID      string `json:"session_id"`
	Seq            int    `json:"seq"`
	MimeType       string `json:"mime_type"`
	AudioBase64    string `json:"audio_base64"`
	VoxTypeMCPURL  string `json:"voxtype_mcp_url"`
	ProducerMCPURL string `json:"producer_mcp_url"`
}

type httpErr struct {
	Status  int
	Message string
}

func (e *httpErr) Error() string {
	return e.Message
}

func (a *App) handlePushToPromptSTT(w http.ResponseWriter, r *http.Request) {
	if !a.requireAuth(w, r) {
		return
	}
	var req pushToPromptRequest
	if err := decodeJSON(r, &req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	if req.Action == "" {
		http.Error(w, "action is required", http.StatusBadRequest)
		return
	}
	result, err := dispatchPushToPromptVoxTypeMCP(req)
	if err != nil {
		if he, ok := err.(*httpErr); ok {
			http.Error(w, he.Message, he.Status)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func dispatchPushToPromptVoxTypeMCP(req pushToPromptRequest) (map[string]interface{}, error) {
	toolName := ""
	args := map[string]interface{}{}
	switch req.Action {
	case sttActionStart:
		if strings.TrimSpace(req.SessionID) == "" {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "session_id is required"}
		}
		mimeType := normalizeSTTMimeType(req.MimeType)
		if !isAllowedSTTMimeType(mimeType) {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "mime_type must be audio/* or application/octet-stream"}
		}
		toolName = "push_to_prompt_start"
		args["session_id"] = req.SessionID
		args["mime_type"] = mimeType
	case sttActionAppend:
		if strings.TrimSpace(req.SessionID) == "" {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "session_id is required"}
		}
		if req.Seq < 0 {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "seq must be >= 0"}
		}
		decodedAudio := strings.TrimSpace(req.AudioBase64)
		if decodedAudio == "" {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "audio_base64 is required"}
		}
		audioData, err := base64.StdEncoding.DecodeString(decodedAudio)
		if err != nil {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "audio_base64 must be valid base64"}
		}
		if len(audioData) == 0 {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "audio payload is empty"}
		}
		if len(audioData) > maxMailSTTAudioBytes {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "audio payload exceeds max size"}
		}
		toolName = "push_to_prompt_append"
		args["session_id"] = req.SessionID
		args["seq"] = req.Seq
		args["audio_chunk_base64"] = decodedAudio
	case sttActionStop:
		if strings.TrimSpace(req.SessionID) == "" {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "session_id is required"}
		}
		toolName = "push_to_prompt_stop"
		args["session_id"] = req.SessionID
	case sttActionCancel:
		if strings.TrimSpace(req.SessionID) == "" {
			return nil, &httpErr{Status: http.StatusBadRequest, Message: "session_id is required"}
		}
		toolName = "push_to_prompt_cancel"
		args["session_id"] = req.SessionID
	default:
		return nil, &httpErr{Status: http.StatusBadRequest, Message: "unsupported action"}
	}
	mcpURL, err := resolveVoxTypeMCPURL(req.VoxTypeMCPURL)
	if err != nil {
		return nil, &httpErr{Status: http.StatusBadRequest, Message: err.Error()}
	}
	resp, err := mcpToolsCallURL(mcpURL, toolName, args)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func resolveVoxTypeMCPURL(raw string) (string, error) {
	candidate := strings.TrimSpace(raw)
	if candidate == "" {
		candidate = strings.TrimSpace(os.Getenv("TABULA_VOXTYPE_MCP_URL"))
	}
	if candidate == "" {
		candidate = defaultVoxTypeMCPURL
	}
	u, err := url.Parse(candidate)
	if err != nil {
		return "", fmt.Errorf("invalid voxtype_mcp_url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("voxtype_mcp_url must use http or https")
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("voxtype_mcp_url must include host")
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf("voxtype_mcp_url host must be loopback")
	}
	if strings.TrimSpace(u.Path) == "" || u.Path == "/" {
		u.Path = "/mcp"
	}
	if u.Path != "/mcp" {
		return "", fmt.Errorf("voxtype_mcp_url path must be /mcp")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("voxtype_mcp_url must not include query or fragment")
	}
	return u.String(), nil
}
