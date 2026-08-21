package scrinium

import (
	"context"
	"encoding/json"
	"os"

	appsvc "scrinium/internal/app"
	"scrinium/internal/mcp"
)

// Config remains an alias for the existing configuration format.
type Config = appsvc.Config

// WriteGovernance remains an alias for the existing configuration format.
type WriteGovernance = appsvc.WriteGovernance

type jsonrpcError = mcp.RPCError

// App is the command composition root and compatibility façade.
type App struct {
	config   *Config
	wikiRoot string
	mcp      *mcp.Server
}

// NewApp composes the typed application and MCP adapter.
func NewApp(configPath string) (*App, error) {
	service, err := appsvc.Open(context.Background(), configPath, appsvc.Content{
		Guide:         defaultGuide,
		StandardFiles: defaultLLMWikiFiles,
	})
	if err != nil {
		return nil, err
	}
	config := service.Config()
	return &App{
		config:   &config,
		wikiRoot: service.WikiRoot(),
		mcp:      mcp.New(service, version),
	}, nil
}

// Run serves MCP over the command's standard streams.
func (a *App) Run(ctx context.Context) error {
	return a.mcp.Run(ctx, os.Stdin, os.Stdout)
}

func (a *App) dispatch(method string, params json.RawMessage) (any, *jsonrpcError) {
	return a.mcp.Dispatch(context.Background(), method, params)
}

func (a *App) handleToolsList() any    { return a.mcp.ToolsList() }
func (a *App) handleCapabilities() any { return a.mcp.Capabilities() }

func (a *App) handleResourceRead(raw json.RawMessage) (any, error) {
	return a.mcp.ResourceRead(context.Background(), raw)
}

func (a *App) handleToolCall(raw json.RawMessage) (any, error) {
	return a.mcp.ToolCall(context.Background(), raw)
}

func (a *App) callTool(name string, params map[string]any) (any, error) {
	return a.mcp.CallTool(context.Background(), name, params)
}

func (a *App) handleReadWikiPage(params map[string]any) (any, error) {
	return a.callTool("read_wiki_page", params)
}

func (a *App) handleUpdateWikiPage(params map[string]any) (any, error) {
	return a.callTool("update_wiki_page", params)
}

func (a *App) handleCreateDraft(params map[string]any) (any, error) {
	return a.callTool("create_draft", params)
}

func (a *App) handleAppendLog(params map[string]any) (any, error) {
	return a.callTool("append_log", params)
}

func (a *App) handleSetupLLMWiki(params map[string]any) (any, error) {
	return a.callTool("setup_llm_wiki", params)
}

func (a *App) handleBeginSession(params map[string]any) (any, error) {
	return a.callTool("begin_session", params)
}

func (a *App) handleSessionStatus(params map[string]any) (any, error) {
	return a.callTool("session_status", params)
}

func (a *App) handleFinishSession(params map[string]any) (any, error) {
	return a.callTool("finish_session", params)
}

func (a *App) handleLintLLMWiki(params map[string]any) (any, error) {
	return a.callTool("lint_llm_wiki", params)
}

func (a *App) handleAdoptLLMWiki(params map[string]any) (any, error) {
	return a.callTool("adopt_llm_wiki", params)
}

func (a *App) handleRegisterSource(params map[string]any) (any, error) {
	return a.callTool("register_source", params)
}

func (a *App) handleCreatePage(params map[string]any) (any, error) {
	return a.callTool("create_page", params)
}

func (a *App) handleMovePage(params map[string]any) (any, error) {
	return a.callTool("move_page", params)
}

func (a *App) handleArchivePage(params map[string]any) (any, error) {
	return a.callTool("archive_page", params)
}
