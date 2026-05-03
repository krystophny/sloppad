package web

import (
	"context"
	"errors"

	"github.com/sloppy-org/slopshell/internal/mcpclient"
)

func (a *App) helpyEnabled() bool {
	if a == nil {
		return false
	}
	return a.helpyEndpoint.ok()
}

func (a *App) helpyClient() (*mcpclient.Client, error) {
	if a == nil || !a.helpyEndpoint.ok() {
		return nil, errors.New("helpy MCP daemon is not configured")
	}
	return mcpclient.New(a.helpyEndpoint.clientEndpoint(), nil, mcpToolsCallTimeout)
}

func (a *App) listHelpyTools() ([]mcpListedTool, error) {
	if !a.helpyEnabled() {
		return nil, nil
	}
	client, err := a.helpyClient()
	if err != nil {
		return nil, err
	}
	return client.ListTools(context.Background())
}

func (a *App) callHelpyTool(ctx context.Context, name string, args map[string]any) (map[string]any, error) {
	client, err := a.helpyClient()
	if err != nil {
		return nil, err
	}
	return client.CallTool(ctx, name, args)
}
