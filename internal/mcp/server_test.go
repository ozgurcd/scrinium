package mcp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsvc "scrinium/internal/app"
	"scrinium/internal/publicapi"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	repository := t.TempDir()
	configPath := filepath.Join(repository, "scrinium.json")
	if err := os.WriteFile(configPath, []byte(`{"wiki_root":"llm-wiki"}`), 0644); err != nil {
		t.Fatal(err)
	}
	application, err := appsvc.Open(context.Background(), configPath, appsvc.Content{
		Guide: "# Guide\n", StandardFiles: map[string]string{
			"page.md": "hello", "index.md": "# Index\n", "agent-rules.md": "# Agent Rules\n",
			"workflows/ingest.md": "# Ingest\n", "log.md": "# Log\n", "source-registry.md": "# Sources\n",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetupWiki(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(application, "test")
}

func TestDispatchesMCPToolToApplication(t *testing.T) {
	server := newTestServer(t)
	raw := json.RawMessage(`{"name":"read_wiki_page","arguments":{"path":"page.md"}}`)
	result, rpcErr := server.Dispatch(context.Background(), "tools/call", raw)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	content := result.(map[string]any)["content"].([]map[string]any)
	if content[0]["text"] != "hello" {
		t.Fatalf("unexpected MCP result: %#v", result)
	}
}

func TestToolFailureUsesMCPErrorContent(t *testing.T) {
	server := newTestServer(t)
	raw := json.RawMessage(`{"name":"update_wiki_page","arguments":{"path":"page.md","content":"changed"}}`)
	result, rpcErr := server.Dispatch(context.Background(), "tools/call", raw)
	if rpcErr != nil {
		t.Fatal(rpcErr)
	}
	if isError, _ := result.(map[string]any)["isError"].(bool); !isError {
		t.Fatalf("expected semantic MCP error result, got %#v", result)
	}
}

func TestCompatibilityMetadataIsPreserved(t *testing.T) {
	server := newTestServer(t)
	listed := server.ToolsList().(map[string]any)["tools"].([]map[string]any)
	var capabilitiesSchema map[string]any
	toolsByName := map[string]map[string]any{}
	for _, tool := range listed {
		toolsByName[tool["name"].(string)] = tool
		if tool["name"] == "capabilities" {
			capabilitiesSchema = tool["inputSchema"].(map[string]any)
		}
	}
	required, ok := capabilitiesSchema["required"].([]string)
	if !ok || len(required) != 0 {
		t.Fatalf("empty tool schema must preserve required as an empty string slice: %#v", capabilitiesSchema)
	}

	result := server.Capabilities().(map[string]any)
	content := result["content"].([]map[string]any)
	var payload map[string]any
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["schema_version"] != publicapi.CapabilitiesSchema {
		t.Fatalf("unexpected capabilities schema: %#v", payload)
	}
	preferred := payload["preferred_operations"].([]any)
	if len(preferred) == 0 || preferred[0] != "claim_create" {
		t.Fatalf("preferred operations do not lead with claims: %#v", preferred)
	}
	if _, ok := toolsByName["claim_validate"]; !ok {
		t.Fatal("claim_validate missing from tool metadata")
	}
	legacy, ok := toolsByName["create_page"]
	if !ok || !strings.Contains(legacy["description"].(string), "Deprecated") {
		t.Fatalf("legacy page tool is not marked deprecated: %#v", legacy)
	}
}
