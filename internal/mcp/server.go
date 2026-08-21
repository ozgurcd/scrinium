// Package mcp adapts JSON-RPC/MCP messages to typed application workflows.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"

	appsvc "scrinium/internal/app"
	"scrinium/internal/publicapi"
)

type request struct {
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
}

type response struct {
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
	JSONRPC string          `json:"jsonrpc"`
}

// RPCError is a JSON-RPC protocol error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server owns MCP schemas, JSON-RPC dispatch, and response formatting.
type Server struct {
	app            *appsvc.Service
	version        string
	sessionMu      sync.RWMutex
	currentSession string
}

// New composes an MCP adapter over the typed application service.
func New(application *appsvc.Service, version string) *Server {
	return &Server{app: application, version: version}
}

// Run serves newline-delimited JSON-RPC until EOF or cancellation.
func (s *Server) Run(ctx context.Context, input io.Reader, output io.Writer) error {
	log.Println("Scrinium MCP Server started (stdio transport)")
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil
		}
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			if encodeErr := encoder.Encode(response{JSONRPC: "2.0", Error: &RPCError{Code: -32700, Message: "Parse error: " + err.Error()}}); encodeErr != nil {
				log.Printf("failed to write parse-error response: %v", encodeErr)
			}
			continue
		}
		result, rpcErr := s.Dispatch(ctx, req.Method, req.Params)
		if req.ID == nil {
			continue
		}
		if err := encoder.Encode(response{ID: req.ID, JSONRPC: "2.0", Result: result, Error: rpcErr}); err != nil {
			log.Printf("failed to write response: %v", err)
		}
	}
	return scanner.Err()
}

// Dispatch routes a JSON-RPC method without exposing transport types to app.
func (s *Server) Dispatch(ctx context.Context, method string, params json.RawMessage) (any, *RPCError) {
	switch method {
	case "initialize":
		return s.Initialize(), nil
	case "notifications/initialized":
		return nil, nil
	case "ping":
		return map[string]any{}, nil
	case "resources/list":
		result, err := s.ResourcesList(ctx)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: err.Error()}
		}
		return result, nil
	case "resources/read":
		result, err := s.ResourceRead(ctx, params)
		if err != nil {
			return nil, &RPCError{Code: -32603, Message: err.Error()}
		}
		return result, nil
	case "tools/list":
		return s.ToolsList(), nil
	case "tools/call":
		result, err := s.ToolCall(ctx, params)
		if err != nil {
			return ErrorResult(err.Error()), nil
		}
		return result, nil
	default:
		return nil, &RPCError{Code: -32601, Message: "Method not found: " + method}
	}
}

// Initialize returns MCP server metadata.
func (s *Server) Initialize() any {
	return map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]any{
			"resources": map[string]any{},
			"tools":     map[string]any{},
		},
		"serverInfo": map[string]any{"name": "scrinium", "version": s.version},
	}
}

// ResourcesList returns all repository-local wiki files as MCP resources.
func (s *Server) ResourcesList(ctx context.Context) (any, error) {
	paths, err := s.app.Documents(ctx)
	if err != nil {
		return nil, err
	}
	resources := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		resources = append(resources, map[string]any{
			"uri":         "llm-wiki://" + path,
			"name":        filepath.Base(path),
			"description": "Wiki page: " + path,
			"mimeType":    mimeTypeForPath(path),
		})
	}
	return map[string]any{"resources": resources}, nil
}

// ResourceRead parses one MCP URI and reads it through the application.
func (s *Server) ResourceRead(ctx context.Context, raw json.RawMessage) (any, error) {
	var params struct {
		URI string `json:"uri"`
	}
	if err := json.Unmarshal(raw, &params); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	const prefix = "llm-wiki://"
	if !strings.HasPrefix(params.URI, prefix) {
		return nil, fmt.Errorf("unsupported URI scheme: %s", params.URI)
	}
	path := strings.TrimPrefix(params.URI, prefix)
	page, err := s.app.ReadPage(ctx, appsvc.PageRequest{Path: path, SessionID: s.sessionID(nil)})
	if err != nil {
		return nil, fmt.Errorf("failed to read resource: %w", err)
	}
	return map[string]any{"contents": []map[string]any{{
		"uri": params.URI, "mimeType": mimeTypeForPath(path), "text": page.Content,
	}}}, nil
}

func (s *Server) sessionID(args map[string]any) string {
	if args != nil {
		if id, ok := args["session_id"].(string); ok && strings.TrimSpace(id) != "" {
			return id
		}
	}
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.currentSession
}

func (s *Server) setCurrentSession(id string) {
	s.sessionMu.Lock()
	s.currentSession = id
	s.sessionMu.Unlock()
}

func mimeTypeForPath(path string) string {
	switch {
	case strings.HasSuffix(path, ".md"):
		return "text/markdown"
	case strings.HasSuffix(path, ".json"):
		return "application/json"
	default:
		return "text/plain"
	}
}

// TextResult formats successful MCP text content.
func TextResult(text string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}}
}

// ErrorResult formats semantic tool failures according to MCP.
func ErrorResult(text string) map[string]any {
	result := TextResult(text)
	result["isError"] = true
	return result
}

func PublicErrorResult(operation string, err error) map[string]any {
	result, encodeErr := jsonTextResult(publicapi.ErrorFrom(operation, err))
	if encodeErr != nil {
		return ErrorResult("failed to serialize public error")
	}
	result["isError"] = true
	return result
}

func jsonTextResult(value any) (map[string]any, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize result: %w", err)
	}
	return TextResult(string(data)), nil
}
