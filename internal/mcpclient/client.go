package mcpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Endpoint struct {
	SocketPath  string
	HTTPBaseURL string
}

type ListedTool struct {
	Name        string
	Description string
	InputSchema map[string]any
}

type Client struct {
	endpoint Endpoint
	client   *http.Client
	timeout  time.Duration
}

func ParseEndpoint(raw string) (Endpoint, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Endpoint{}, nil
	}
	if strings.HasPrefix(s, "unix:") {
		path := strings.TrimPrefix(s, "unix:")
		path = strings.TrimPrefix(path, "//")
		if path == "" {
			return Endpoint{}, errors.New("empty unix socket path")
		}
		return Endpoint{SocketPath: filepath.Clean(path)}, nil
	}
	if strings.HasPrefix(s, "/") {
		return Endpoint{SocketPath: filepath.Clean(s)}, nil
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		s = strings.TrimSuffix(s, "/mcp")
		s = strings.TrimRight(s, "/")
		return Endpoint{HTTPBaseURL: s}, nil
	}
	return Endpoint{}, fmt.Errorf("unrecognized MCP endpoint: %q", s)
}

func (e Endpoint) OK() bool {
	return strings.TrimSpace(e.SocketPath) != "" || strings.TrimSpace(e.HTTPBaseURL) != ""
}

func (e Endpoint) HTTPURL(route string) string {
	if route == "" {
		route = "/"
	}
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	if e.SocketPath != "" {
		return "http://unix" + route
	}
	return strings.TrimRight(e.HTTPBaseURL, "/") + route
}

func (e Endpoint) WSURL(route string) string {
	if route == "" {
		route = "/"
	}
	if !strings.HasPrefix(route, "/") {
		route = "/" + route
	}
	if e.SocketPath != "" {
		return "ws://unix" + route
	}
	base := strings.TrimRight(e.HTTPBaseURL, "/")
	switch {
	case strings.HasPrefix(base, "https://"):
		base = "wss://" + strings.TrimPrefix(base, "https://")
	case strings.HasPrefix(base, "http://"):
		base = "ws://" + strings.TrimPrefix(base, "http://")
	}
	return base + route
}

func (e Endpoint) HTTPClient(timeout time.Duration) *http.Client {
	if e.SocketPath != "" {
		socket := e.SocketPath
		dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		}
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:           dial,
				ResponseHeaderTimeout: timeout,
				MaxIdleConns:          16,
				MaxIdleConnsPerHost:   16,
				IdleConnTimeout:       30 * time.Second,
			},
		}
	}
	return &http.Client{Timeout: timeout}
}

func (e Endpoint) WSDialer() *websocket.Dialer {
	if e.SocketPath != "" {
		socket := e.SocketPath
		return &websocket.Dialer{
			HandshakeTimeout: 5 * time.Second,
			NetDialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		}
	}
	return &websocket.Dialer{HandshakeTimeout: 5 * time.Second}
}

func New(endpoint Endpoint, client *http.Client, timeout time.Duration) (*Client, error) {
	if !endpoint.OK() {
		return nil, errors.New("MCP endpoint is not configured")
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	if client == nil {
		client = SharedHTTPClient(endpoint, timeout)
	}
	return &Client{endpoint: endpoint, client: client, timeout: timeout}, nil
}

func (c *Client) ListTools(ctx context.Context) ([]ListedTool, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
		"params":  map[string]any{},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.HTTPURL("/mcp"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			return nil, fmt.Errorf("MCP call timed out after %s", c.timeout)
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("MCP call failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var envelope map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, err
	}
	if rpcErr, ok := envelope["error"].(map[string]any); ok {
		return nil, fmt.Errorf("MCP error: %v", rpcErr["message"])
	}
	result, _ := envelope["result"].(map[string]any)
	if result == nil {
		return nil, errors.New("MCP call failed: missing result")
	}
	rawTools, _ := result["tools"].([]any)
	tools := make([]ListedTool, 0, len(rawTools))
	for _, raw := range rawTools {
		obj, _ := raw.(map[string]any)
		if obj == nil {
			continue
		}
		schema, _ := obj["inputSchema"].(map[string]any)
		tools = append(tools, ListedTool{
			Name:        strings.TrimSpace(fmt.Sprint(obj["name"])),
			Description: strings.TrimSpace(fmt.Sprint(obj["description"])),
			InputSchema: schema,
		})
	}
	return tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, arguments map[string]any) (map[string]any, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params": map[string]any{
			"name":      name,
			"arguments": arguments,
		},
	}
	var out map[string]any
	if err := c.call(ctx, payload, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) call(ctx context.Context, payload map[string]any, target any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint.HTTPURL("/mcp"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		var netErr net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
			return fmt.Errorf("MCP call timed out after %s", c.timeout)
		}
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("MCP call failed: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var envelope map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return err
	}
	if rpcErr, ok := envelope["error"].(map[string]any); ok {
		return fmt.Errorf("MCP error: %v", rpcErr["message"])
	}
	result, _ := envelope["result"].(map[string]any)
	if result == nil {
		return errors.New("MCP call failed: missing result")
	}
	if isErr, _ := result["isError"].(bool); isErr {
		name := ""
		if params, _ := payload["params"].(map[string]any); params != nil {
			name = strings.TrimSpace(fmt.Sprint(params["name"]))
		}
		if name != "" {
			return fmt.Errorf("MCP tool %q failed: %s", name, ResultErrorText(result))
		}
		return errors.New(ResultErrorText(result))
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if structured == nil {
		return errors.New("MCP call failed: missing structuredContent")
	}
	data, err := json.Marshal(structured)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func ResultErrorText(result map[string]any) string {
	content, _ := result["content"].([]any)
	parts := make([]string, 0, len(content))
	for _, item := range content {
		entry, _ := item.(map[string]any)
		if entry == nil {
			continue
		}
		text := strings.TrimSpace(fmt.Sprint(entry["text"]))
		if text == "" || text == "<nil>" {
			continue
		}
		parts = append(parts, text)
	}
	if len(parts) == 0 {
		return "unknown error"
	}
	return strings.Join(parts, " | ")
}

func WaitForReady(endpoint Endpoint, timeout time.Duration, errCh <-chan error) error {
	if !endpoint.OK() {
		return errors.New("waitForReady: endpoint not configured")
	}
	deadline := time.Now().Add(timeout)
	client := endpoint.HTTPClient(750 * time.Millisecond)
	for time.Now().Before(deadline) {
		select {
		case err := <-errCh:
			if err == nil {
				return errors.New("mcp listener exited before becoming healthy")
			}
			return fmt.Errorf("mcp listener failed to start: %w", err)
		default:
		}
		resp, err := client.Get(endpoint.HTTPURL("/health"))
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	select {
	case err := <-errCh:
		if err == nil {
			return errors.New("mcp listener exited before becoming healthy")
		}
		return fmt.Errorf("mcp listener failed to start: %w", err)
	default:
	}
	return errors.New("mcp health check timeout")
}

var (
	sharedHTTPClientMu    sync.Mutex
	sharedHTTPClientCache = map[string]*http.Client{}
)

func SharedHTTPClient(endpoint Endpoint, timeout time.Duration) *http.Client {
	if !endpoint.OK() {
		return &http.Client{Timeout: timeout}
	}
	key := strings.TrimSpace(endpoint.SocketPath)
	if key == "" {
		key = strings.TrimSpace(endpoint.HTTPBaseURL)
	}
	if key == "" {
		return endpoint.HTTPClient(timeout)
	}
	sharedHTTPClientMu.Lock()
	defer sharedHTTPClientMu.Unlock()
	if client, ok := sharedHTTPClientCache[key]; ok {
		return client
	}
	client := endpoint.HTTPClient(timeout)
	sharedHTTPClientCache[key] = client
	return client
}

func DefaultSocketPath(envKey, socketName string) string {
	if raw := strings.TrimSpace(os.Getenv(envKey)); raw != "" {
		return raw
	}
	if runtime.GOOS == "darwin" {
		home := strings.TrimSpace(os.Getenv("HOME"))
		if home == "" {
			return ""
		}
		return filepath.Join(home, "Library", "Caches", "sloppy", socketName)
	}
	if rt := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); rt != "" {
		return filepath.Join(rt, "sloppy", socketName)
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "sloppy", socketName)
}

func RejectPlainHTTP(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return fmt.Errorf("plaintext MCP URLs are no longer supported (got %q); use a unix socket", raw)
	}
	return nil
}
