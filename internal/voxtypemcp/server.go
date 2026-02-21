package voxtypemcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	maxSessionAudioBytes   = 10 * 1024 * 1024
	defaultRequestID       = 1
	defaultCaptureMode     = "daemon"
	defaultSessionMimeType = "audio/webm"
)

type sessionState struct {
	MimeType        string
	CaptureBackend  string
	TranscriptPath  string
	StartedAt       time.Time
	LastSeq         int
	Bytes           []byte
	IgnoredChunkSum int
}

type Server struct {
	bind string
	port int

	mu       sync.Mutex
	sessions map[string]*sessionState
}

func NewServer(bind string, port int) *Server {
	return &Server{
		bind:     strings.TrimSpace(bind),
		port:     port,
		sessions: map[string]*sessionState{},
	}
}

func (s *Server) Start() error {
	if s.bind == "" {
		s.bind = "127.0.0.1"
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/mcp", s.handleMCP)
	addr := netJoinHostPort(s.bind, s.port)
	fmt.Printf("voxtype MCP server listening on http://%s/mcp\n", addr)
	return (&http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}).ListenAndServe()
}

func netJoinHostPort(host string, port int) string {
	return host + ":" + strconv.Itoa(port)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]interface{}{
		"status": "ok",
	})
}

func (s *Server) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, reqID(req), -32700, "parse error: invalid JSON")
		return
	}
	id := reqID(req)
	method := strings.TrimSpace(fmt.Sprint(req["method"]))
	params, _ := req["params"].(map[string]interface{})
	switch method {
	case "initialize":
		writeRPCResult(w, id, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]interface{}{
				"name":    "tabula-voxtype-mcp",
				"version": "0.0.5",
			},
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
		})
		return
	case "tools/list":
		tools := []map[string]interface{}{
			{"name": "push_to_prompt_start", "description": "start a push-to-prompt capture session"},
			{"name": "push_to_prompt_append", "description": "append captured audio chunk to a session"},
			{"name": "push_to_prompt_stop", "description": "stop a session and transcribe the buffered audio"},
			{"name": "push_to_prompt_cancel", "description": "cancel and discard a capture session"},
			{"name": "push_to_prompt_health", "description": "health and dependency status"},
		}
		writeRPCResult(w, id, map[string]interface{}{"tools": tools})
		return
	case "tools/call":
		name := strings.TrimSpace(fmt.Sprint(params["name"]))
		args, _ := params["arguments"].(map[string]interface{})
		result, err := s.callTool(name, args)
		if err != nil {
			writeRPCError(w, id, -32000, err.Error())
			return
		}
		writeRPCResult(w, id, result)
		return
	default:
		writeRPCError(w, id, -32601, "method not found")
		return
	}
}

func reqID(req map[string]interface{}) interface{} {
	if req == nil {
		return defaultRequestID
	}
	if id, ok := req["id"]; ok {
		return id
	}
	return defaultRequestID
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeRPCResult(w http.ResponseWriter, id interface{}, structuredContent map[string]interface{}) {
	if id == nil {
		id = defaultRequestID
	}
	writeJSON(w, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"result": map[string]interface{}{
			"structuredContent": structuredContent,
		},
	})
}

func writeRPCError(w http.ResponseWriter, id interface{}, code int, msg string) {
	if id == nil {
		id = defaultRequestID
	}
	writeJSON(w, map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      id,
		"error": map[string]interface{}{
			"code":    code,
			"message": msg,
		},
	})
}

func (s *Server) callTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	switch name {
	case "push_to_prompt_start":
		return s.toolStart(args)
	case "push_to_prompt_append":
		return s.toolAppend(args)
	case "push_to_prompt_stop":
		return s.toolStop(args)
	case "push_to_prompt_cancel":
		return s.toolCancel(args)
	case "push_to_prompt_health":
		return s.toolHealth()
	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}

func strArg(args map[string]interface{}, key string) string {
	return strings.TrimSpace(fmt.Sprint(args[key]))
}

func intArg(args map[string]interface{}, key string, def int) int {
	raw, ok := args[key]
	if !ok {
		return def
	}
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return def
}

func resolveCaptureMode(raw string) string {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv("TABULA_VOXTYPE_MCP_CAPTURE_MODE")))
	}
	if mode == "daemon" || mode == "buffered" {
		return mode
	}
	return defaultCaptureMode
}

func (s *Server) toolStart(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	mimeType := strings.ToLower(strings.TrimSpace(strArg(args, "mime_type")))
	if mimeType == "" {
		mimeType = defaultSessionMimeType
	}
	requestedModeRaw := strArg(args, "capture_mode")
	captureMode := resolveCaptureMode(requestedModeRaw)

	state := &sessionState{
		MimeType:       mimeType,
		CaptureBackend: "buffered",
		StartedAt:      time.Now().UTC(),
		LastSeq:        -1,
		Bytes:          make([]byte, 0, 4096),
	}

	if captureMode == "daemon" {
		transcriptPath := makeSessionTranscriptPath(sid)
		startErr := voxtypeRecordStart(transcriptPath)
		if startErr == nil {
			state.CaptureBackend = "daemon"
			state.TranscriptPath = transcriptPath
			state.Bytes = nil
		} else if strings.EqualFold(strings.TrimSpace(requestedModeRaw), "daemon") {
			return nil, fmt.Errorf("failed to start voxtype daemon capture: %w", startErr)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.sessions[sid]; exists {
		if state.CaptureBackend == "daemon" {
			_ = voxtypeRecordCancel()
			_ = os.Remove(state.TranscriptPath)
		}
		return nil, fmt.Errorf("session %q already exists", sid)
	}
	s.sessions[sid] = state

	return map[string]interface{}{
		"ok":              true,
		"session_id":      sid,
		"started_at":      state.StartedAt.Format(time.RFC3339Nano),
		"capture_backend": state.CaptureBackend,
	}, nil
}

func (s *Server) toolAppend(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	seq := intArg(args, "seq", -1)
	if seq < 0 {
		return nil, errors.New("seq must be >= 0")
	}
	encoded := strArg(args, "audio_chunk_base64")
	if encoded == "" {
		return nil, errors.New("audio_chunk_base64 is required")
	}
	chunk, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("audio_chunk_base64 must be valid base64")
	}
	if len(chunk) == 0 {
		return nil, errors.New("audio chunk is empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.sessions[sid]
	if state == nil {
		return nil, fmt.Errorf("session %q not found", sid)
	}
	if state.LastSeq >= 0 && seq <= state.LastSeq {
		return nil, fmt.Errorf("seq must be strictly increasing (last=%d got=%d)", state.LastSeq, seq)
	}
	state.LastSeq = seq

	if state.CaptureBackend == "daemon" {
		state.IgnoredChunkSum += len(chunk)
		return map[string]interface{}{
			"ok":              true,
			"session_id":      sid,
			"received_seq":    seq,
			"capture_backend": state.CaptureBackend,
			"ignored_bytes":   state.IgnoredChunkSum,
		}, nil
	}

	if len(state.Bytes)+len(chunk) > maxSessionAudioBytes {
		return nil, errors.New("audio payload exceeds max size")
	}
	state.Bytes = append(state.Bytes, chunk...)
	return map[string]interface{}{
		"ok":              true,
		"session_id":      sid,
		"received_seq":    seq,
		"capture_backend": state.CaptureBackend,
		"buffered_bytes":  len(state.Bytes),
	}, nil
}

func (s *Server) toolStop(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	s.mu.Lock()
	state := s.sessions[sid]
	if state != nil {
		delete(s.sessions, sid)
	}
	s.mu.Unlock()
	if state == nil {
		return nil, fmt.Errorf("session %q not found", sid)
	}

	start := time.Now()
	if state.CaptureBackend == "daemon" {
		text, err := voxtypeRecordStopAndRead(state.TranscriptPath)
		if err != nil {
			return nil, err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			return nil, errors.New("voxtype returned empty transcript")
		}
		return map[string]interface{}{
			"ok":                   true,
			"session_id":           sid,
			"text":                 text,
			"language":             "",
			"language_probability": 0.0,
			"source":               "voxtype_mcp",
			"capture_backend":      state.CaptureBackend,
			"duration_ms":          time.Since(start).Milliseconds(),
		}, nil
	}

	if len(state.Bytes) == 0 {
		return nil, errors.New("no buffered audio for session")
	}
	text, err := transcribeWithVoxType(state.MimeType, state.Bytes)
	if err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("voxtype returned empty transcript")
	}
	return map[string]interface{}{
		"ok":                   true,
		"session_id":           sid,
		"text":                 text,
		"language":             "",
		"language_probability": 0.0,
		"source":               "voxtype_mcp",
		"capture_backend":      state.CaptureBackend,
		"duration_ms":          time.Since(start).Milliseconds(),
	}, nil
}

func (s *Server) toolCancel(args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	if sid == "" {
		return nil, errors.New("session_id is required")
	}
	s.mu.Lock()
	state := s.sessions[sid]
	_, existed := s.sessions[sid]
	delete(s.sessions, sid)
	s.mu.Unlock()

	backend := ""
	if state != nil {
		backend = state.CaptureBackend
		if state.CaptureBackend == "daemon" {
			_ = voxtypeRecordCancel()
			if state.TranscriptPath != "" {
				_ = os.Remove(state.TranscriptPath)
			}
		}
	}
	return map[string]interface{}{
		"ok":              true,
		"session_id":      sid,
		"canceled":        existed,
		"capture_backend": backend,
	}, nil
}

func (s *Server) toolHealth() (map[string]interface{}, error) {
	captureMode := resolveCaptureMode("")
	ffmpegPath, ffmpegErr := exec.LookPath("ffmpeg")
	voxtypePath, voxtypeErr := exec.LookPath("voxtype")
	daemonOK, daemonRaw, daemonErr := voxtypeDaemonHealth()

	ok := false
	switch captureMode {
	case "daemon":
		ok = voxtypeErr == nil && daemonOK
	default:
		ok = ffmpegErr == nil && voxtypeErr == nil
	}

	return map[string]interface{}{
		"ok":           ok,
		"capture_mode": captureMode,
		"default_mode": defaultCaptureMode,
		"dependencies": map[string]interface{}{
			"ffmpeg": map[string]interface{}{
				"ok":   ffmpegErr == nil,
				"path": ffmpegPath,
			},
			"voxtype": map[string]interface{}{
				"ok":   voxtypeErr == nil,
				"path": voxtypePath,
			},
			"voxtype_daemon": map[string]interface{}{
				"ok":     daemonOK,
				"detail": strings.TrimSpace(daemonRaw),
				"error":  errString(daemonErr),
			},
		},
	}, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func voxtypeDaemonHealth() (bool, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "voxtype", "status", "--format", "json")
	out, err := cmd.CombinedOutput()
	raw := strings.TrimSpace(string(out))
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return false, raw, errors.New("voxtype status timed out")
		}
		return false, raw, err
	}
	if raw == "" {
		return false, raw, errors.New("empty voxtype status response")
	}
	var payload map[string]interface{}
	if json.Unmarshal(out, &payload) == nil {
		alt := strings.ToLower(strings.TrimSpace(fmt.Sprint(payload["alt"])))
		if alt == "" || alt == "<nil>" {
			return true, raw, nil
		}
		return alt != "offline" && alt != "stopped" && alt != "error", raw, nil
	}
	return true, raw, nil
}

func voxtypeRecordStart(transcriptPath string) error {
	if strings.TrimSpace(transcriptPath) == "" {
		return errors.New("transcript path is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "voxtype", "record", "start", "--file="+transcriptPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("voxtype record start timed out")
		}
		return fmt.Errorf("voxtype record start failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func voxtypeRecordStopAndRead(transcriptPath string) (string, error) {
	if strings.TrimSpace(transcriptPath) == "" {
		return "", errors.New("missing transcript path")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "voxtype", "record", "stop")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return "", errors.New("voxtype record stop timed out")
		}
		return "", fmt.Errorf("voxtype record stop failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	text, readErr := readTranscriptWithRetry(transcriptPath, 4*time.Second)
	_ = os.Remove(transcriptPath)
	if readErr != nil {
		return "", fmt.Errorf("failed to read voxtype transcript file: %w", readErr)
	}
	return text, nil
}

func voxtypeRecordCancel() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "voxtype", "record", "cancel")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("voxtype record cancel timed out")
		}
		return fmt.Errorf("voxtype record cancel failed: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func readTranscriptWithRetry(path string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			text := strings.TrimSpace(string(data))
			if text != "" {
				return text, nil
			}
		}
		if time.Now().After(deadline) {
			if err != nil {
				return "", err
			}
			return "", errors.New("transcript file was empty")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func makeSessionTranscriptPath(sessionID string) string {
	safe := sanitizeSessionID(sessionID)
	if safe == "" {
		safe = "session"
	}
	name := fmt.Sprintf("tabula-voxtype-%s-%d.txt", safe, time.Now().UnixNano())
	return filepath.Join(os.TempDir(), name)
}

func sanitizeSessionID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(strings.TrimSpace(b.String()), "-")
}

func transcribeWithVoxType(mimeType string, data []byte) (string, error) {
	tmpDir, err := os.MkdirTemp("", "tabula-voxtype-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	inExt := fileExtFromMime(mimeType)
	inputPath := filepath.Join(tmpDir, "input"+inExt)
	if err := os.WriteFile(inputPath, data, 0o600); err != nil {
		return "", fmt.Errorf("failed to write input audio: %w", err)
	}

	wavPath := filepath.Join(tmpDir, "input.wav")
	ffmpegCtx, ffmpegCancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer ffmpegCancel()
	ffmpegCmd := exec.CommandContext(
		ffmpegCtx,
		"ffmpeg",
		"-hide_banner",
		"-loglevel", "error",
		"-y",
		"-i", inputPath,
		"-ac", "1",
		"-ar", "16000",
		"-f", "wav",
		wavPath,
	)
	ffmpegOut, ffmpegErr := ffmpegCmd.CombinedOutput()
	if ffmpegErr != nil {
		return "", fmt.Errorf("ffmpeg conversion failed: %v: %s", ffmpegErr, strings.TrimSpace(string(ffmpegOut)))
	}

	voxCtx, voxCancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer voxCancel()
	voxCmd := exec.CommandContext(voxCtx, "voxtype", "-q", "transcribe", wavPath)
	stdout, err := voxCmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("voxtype stdout pipe: %w", err)
	}
	stderr, err := voxCmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("voxtype stderr pipe: %w", err)
	}
	if err := voxCmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start voxtype: %w", err)
	}
	outBytes, _ := io.ReadAll(stdout)
	errBytes, _ := io.ReadAll(stderr)
	waitErr := voxCmd.Wait()
	if waitErr != nil {
		return "", fmt.Errorf("voxtype transcribe failed: %v: %s", waitErr, strings.TrimSpace(string(errBytes)))
	}
	text := parseVoxTypeTranscript(string(outBytes))
	if text == "" {
		text = parseVoxTypeTranscript(string(errBytes))
	}
	if text == "" {
		return "", errors.New("voxtype produced no transcript output")
	}
	return text, nil
}

func parseVoxTypeTranscript(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Loading audio file:") ||
			strings.HasPrefix(line, "Audio format:") ||
			strings.HasPrefix(line, "Resampling from") ||
			strings.HasPrefix(line, "Processing ") ||
			strings.HasPrefix(line, "VAD:") {
			continue
		}
		parts = append(parts, line)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.TrimSpace(parts[len(parts)-1])
}

func fileExtFromMime(mimeType string) string {
	mt := strings.ToLower(strings.TrimSpace(mimeType))
	if strings.Contains(mt, "wav") {
		return ".wav"
	}
	if strings.Contains(mt, "ogg") {
		return ".ogg"
	}
	if strings.Contains(mt, "mp4") || strings.Contains(mt, "aac") || strings.Contains(mt, "m4a") {
		return ".m4a"
	}
	if strings.Contains(mt, "mpeg") {
		return ".mp3"
	}
	return ".webm"
}
