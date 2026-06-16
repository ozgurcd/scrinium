package scrinium

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestApp creates an App backed by a temporary directory with the given
// governance config. Caller must call the returned cleanup function.
func newTestApp(t *testing.T, protectedFiles []string) (*App, func()) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "scrinium-test-*")
	if err != nil {
		t.Fatal(err)
	}

	// Build the JSON config.
	protectedJSON := "[]"
	if len(protectedFiles) > 0 {
		quoted := make([]string, len(protectedFiles))
		for i, f := range protectedFiles {
			quoted[i] = `"` + f + `"`
		}
		protectedJSON = "[" + strings.Join(quoted, ",") + "]"
	}

	configContent := `{
		"wiki_root": "./wiki",
		"write_governance": {
			"protected_files": ` + protectedJSON + `
		}
	}`

	configPath := filepath.Join(tempDir, "config.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	app, err := NewApp(configPath)
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}

	cleanup := func() {
		if err := os.RemoveAll(tempDir); err != nil {
			t.Logf("Warning: cleanup failed: %v", err)
		}
	}

	return app, cleanup
}

func TestNewApp(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md"})
	defer cleanup()

	if app.config == nil {
		t.Error("Config should not be nil")
	}
	if app.wikiRoot == "" {
		t.Error("Wiki root should not be empty")
	}
}

func TestRunCLIEnforceAgentsWritesManagedBlocks(t *testing.T) {
	repo := t.TempDir()
	configPath := filepath.Join(repo, "scrinium.json")
	if err := os.WriteFile(configPath, []byte(`{"wiki_root":"./llm-wiki"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("# Existing Agents\n\nKeep this line.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"enforce-agents", "--repo", repo}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d stderr=%s", code, stderr.String())
	}

	agentsData, err := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	agents := string(agentsData)
	if !strings.Contains(agents, "Keep this line.") {
		t.Fatalf("existing AGENTS.md content should be preserved, got:\n%s", agents)
	}
	if !strings.Contains(agents, "<!-- BEGIN SCRINIUM ENFORCEMENT -->") ||
		!strings.Contains(agents, "After any harness or plugin bootstrap instructions are loaded, call Scrinium `capabilities` before project work or wiki writes.") ||
		!strings.Contains(agents, "Call `finish_session` before reporting completion.") {
		t.Fatalf("AGENTS.md should contain Scrinium enforcement block, got:\n%s", agents)
	}

	claudeData, err := os.ReadFile(filepath.Join(repo, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	claude := string(claudeData)
	if !strings.Contains(claude, "# Claude Code Instructions") ||
		!strings.Contains(claude, "<!-- BEGIN SCRINIUM ENFORCEMENT -->") {
		t.Fatalf("CLAUDE.md should contain Claude heading and enforcement block, got:\n%s", claude)
	}

	snippetData, err := os.ReadFile(filepath.Join(repo, "docs", "scrinium-agent-enforcement.md"))
	if err != nil {
		t.Fatal(err)
	}
	snippet := string(snippetData)
	if !strings.Contains(snippet, `"command": "scrinium"`) ||
		!strings.Contains(snippet, configPath) ||
		!strings.Contains(snippet, "OpenCode") ||
		!strings.Contains(snippet, "Antigravity") {
		t.Fatalf("agent enforcement doc should include MCP snippet and target agents, got:\n%s", snippet)
	}
}

func TestRunCLIEnforceAgentsIsIdempotent(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "scrinium.json"), []byte(`{"wiki_root":"./llm-wiki"}`), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	if code := RunCLI([]string{"enforce-agents", "--repo", repo}, &stdout, &stderr); code != 0 {
		t.Fatalf("first run exit code = %d stderr=%s", code, stderr.String())
	}
	first, err := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}

	stdout.Reset()
	stderr.Reset()
	if code := RunCLI([]string{"enforce-agents", "--repo", repo}, &stdout, &stderr); code != 0 {
		t.Fatalf("second run exit code = %d stderr=%s", code, stderr.String())
	}
	second, err := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("enforce-agents should be idempotent\nfirst:\n%s\nsecond:\n%s", string(first), string(second))
	}
}

func TestRunCLIEnforceAgentsDryRunDoesNotWrite(t *testing.T) {
	repo := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"enforce-agents", "--repo", repo, "--dry-run"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected dry-run exit code 0, got %d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(repo, "AGENTS.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write AGENTS.md, stat err=%v", err)
	}
	if !strings.Contains(stdout.String(), "would update AGENTS.md") {
		t.Fatalf("dry-run should report planned writes, got stdout=%s", stdout.String())
	}
}

func TestRunCLIEnforceAgentsCheckFailsWhenMissing(t *testing.T) {
	repo := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"enforce-agents", "--repo", repo, "--check"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("check should fail when enforcement files are missing, stdout=%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "agent enforcement is not current") {
		t.Fatalf("check should explain stale enforcement, got stderr=%s", stderr.String())
	}
}

func TestRunCLIEnforceAgentsHelpExitsZero(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"enforce-agents", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("help should exit 0, got %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: scrinium enforce-agents") {
		t.Fatalf("help should print usage to stdout, got %s", stdout.String())
	}
}

func TestRunCLIVersionPrintsEmbeddedVersion(t *testing.T) {
	oldVersion := version
	version = "9.8.7-test"
	defer func() {
		version = oldVersion
	}()

	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("version should exit 0, got %d stderr=%s", code, stderr.String())
	}
	if stdout.String() != "scrinium 9.8.7-test\n" {
		t.Fatalf("unexpected version output: %q", stdout.String())
	}
}

func TestRunCLIPreservesMCPModeForConfigPath(t *testing.T) {
	if IsCLISubcommand([]string{"./scrinium.json"}) {
		t.Fatal("config path should not be treated as a CLI subcommand")
	}
	if !IsCLISubcommand([]string{"enforce-agents"}) {
		t.Fatal("enforce-agents should be treated as a CLI subcommand")
	}
	if !IsCLISubcommand([]string{"version"}) {
		t.Fatal("version should be treated as a CLI subcommand")
	}
}

func TestNewAppBootstrappedGuideMentionsSetupTool(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	data, err := os.ReadFile(filepath.Join(app.wikiRoot, "scrinium-guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "setup_llm_wiki") {
		t.Fatalf("bootstrapped guide should mention setup_llm_wiki, got %q", string(data))
	}
}

func TestDefaultProtectedFilesDoNotIncludeAgentRules(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md", "architecture/*", "core-decisions/*"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "agent-rules.md",
		"content": "# Agent Rules\n\nupdated\n",
	}

	if _, err := app.handleUpdateWikiPage(params); err != nil {
		t.Fatalf("agent-rules.md should be writable when not configured as protected: %v", err)
	}
}

func TestSetupLLMWikiCreatesCanonicalStructure(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	// NewApp bootstraps scrinium-guide.md; setup_llm_wiki should create the
	// LLM Wiki operating structure without overwriting that existing file.
	result, err := app.handleSetupLLMWiki(map[string]any{})
	if err != nil {
		t.Fatalf("setup_llm_wiki should succeed: %v", err)
	}

	resultMap, _ := result.(map[string]any)
	contentArr, _ := resultMap["content"].([]map[string]any)
	if len(contentArr) == 0 {
		t.Fatal("expected non-empty MCP content response")
	}

	expectedFiles := []string{
		"index.md",
		"log.md",
		"agent-rules.md",
		"prompt-templates.md",
		"source-registry.md",
		"workflows/ingest.md",
		"workflows/query.md",
		"workflows/lint.md",
		"schemas/page-schemas.md",
		"security/untrusted-sources.md",
		"sources/README.md",
	}
	for _, rel := range expectedFiles {
		if _, err := os.Stat(filepath.Join(app.wikiRoot, rel)); err != nil {
			t.Fatalf("expected setup to create %s: %v", rel, err)
		}
	}

	if _, err := os.Stat(filepath.Join(app.wikiRoot, "logs")); !os.IsNotExist(err) {
		t.Fatalf("setup should not create legacy logs directory, stat err: %v", err)
	}
}

func TestSetupLLMWikiDoesNotOverwriteExistingPages(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	indexPath := filepath.Join(app.wikiRoot, "index.md")
	if err := os.WriteFile(indexPath, []byte("custom index"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := app.handleSetupLLMWiki(map[string]any{}); err != nil {
		t.Fatalf("setup_llm_wiki should succeed: %v", err)
	}

	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom index" {
		t.Fatalf("setup should not overwrite existing index.md, got %q", string(data))
	}
}

func TestMCPToolCall_SetupLLMWiki(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	raw := json.RawMessage(`{"name":"setup_llm_wiki","arguments":{}}`)
	if _, err := app.handleToolCall(raw); err != nil {
		t.Fatalf("setup_llm_wiki tool call should succeed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(app.wikiRoot, "index.md")); err != nil {
		t.Fatalf("expected setup_llm_wiki tool to create index.md: %v", err)
	}
}

func TestToolsListIncludesSetupLLMWiki(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	result := app.handleToolsList()
	resultMap, _ := result.(map[string]any)
	tools, _ := resultMap["tools"].([]map[string]any)
	for _, tool := range tools {
		if tool["name"] == "setup_llm_wiki" {
			return
		}
	}

	t.Fatal("tools/list should include setup_llm_wiki")
}

func TestToolsListIncludesSessionTools(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	result := app.handleToolsList()
	resultMap, _ := result.(map[string]any)
	tools, _ := resultMap["tools"].([]map[string]any)
	found := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		found[name] = true
	}

	for _, name := range []string{"begin_session", "session_status", "finish_session"} {
		if !found[name] {
			t.Fatalf("tools/list should include %s", name)
		}
	}
}

func TestToolsListIncludesAdoptionTools(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	result := app.handleToolsList()
	resultMap, _ := result.(map[string]any)
	tools, _ := resultMap["tools"].([]map[string]any)
	found := map[string]bool{}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		found[name] = true
	}

	for _, name := range []string{"lint_llm_wiki", "adopt_llm_wiki", "register_source", "create_page", "move_page", "archive_page"} {
		if !found[name] {
			t.Fatalf("tools/list should include %s", name)
		}
	}
}

func TestCapabilitiesExplainsAdoptionTools(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	text := toolText(t, app.handleCapabilities())
	for _, name := range []string{"lint_llm_wiki", "adopt_llm_wiki", "register_source", "create_page", "move_page", "archive_page"} {
		if !strings.Contains(text, name) {
			t.Fatalf("capabilities should explain %s, got %s", name, text)
		}
	}
}

func TestLintLLMWikiReportsMissingStandardPagesAndIndexGaps(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(app.wikiRoot, "index.md"), []byte("# Index\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.wikiRoot, "notes.md"), []byte("# Notes\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := app.handleLintLLMWiki(map[string]any{})
	if err != nil {
		t.Fatalf("lint_llm_wiki should succeed: %v", err)
	}
	text := toolText(t, result)
	if !strings.Contains(text, "agent-rules.md") {
		t.Fatalf("lint should report missing standard pages, got: %s", text)
	}
	if !strings.Contains(text, "notes.md") {
		t.Fatalf("lint should report page missing from index, got: %s", text)
	}
}

func TestAdoptLLMWikiReportsSetupRecommendation(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(app.wikiRoot, "index.md"), []byte("# Existing Wiki\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := app.handleAdoptLLMWiki(map[string]any{})
	if err != nil {
		t.Fatalf("adopt_llm_wiki should succeed: %v", err)
	}
	text := toolText(t, result)
	if !strings.Contains(text, "setup_llm_wiki") || !strings.Contains(text, "agent-rules.md") {
		t.Fatalf("adoption report should recommend setup for missing standard pages, got: %s", text)
	}
}

func TestCreatePageCreatesOnlyNewPages(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "notes/new.md",
		"content": "# New\n",
	}
	if _, err := app.handleCreatePage(params); err != nil {
		t.Fatalf("create_page should create a new page: %v", err)
	}
	if _, err := app.handleCreatePage(params); err == nil {
		t.Fatal("create_page should reject existing pages")
	}
}

func TestMovePageRenamesWithinWikiAndRejectsProtectedPaths(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	if _, err := app.handleCreatePage(map[string]any{
		"path":    "notes/old.md",
		"content": "# Old\n",
	}); err != nil {
		t.Fatalf("create source page: %v", err)
	}
	if _, err := app.handleMovePage(map[string]any{
		"from": "notes/old.md",
		"to":   "notes/new.md",
	}); err != nil {
		t.Fatalf("move_page should rename page: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.wikiRoot, "notes/new.md")); err != nil {
		t.Fatalf("expected moved page: %v", err)
	}
	if _, err := app.handleMovePage(map[string]any{
		"from": "notes/new.md",
		"to":   "rules.md",
	}); err == nil {
		t.Fatal("move_page should reject protected destination")
	}
}

func TestArchivePageMovesToArchive(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	if _, err := app.handleCreatePage(map[string]any{
		"path":    "notes/old.md",
		"content": "# Old\n",
	}); err != nil {
		t.Fatalf("create page: %v", err)
	}
	if _, err := app.handleArchivePage(map[string]any{
		"path":   "notes/old.md",
		"reason": "superseded",
	}); err != nil {
		t.Fatalf("archive_page should move page: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.wikiRoot, "archive/notes/old.md")); err != nil {
		t.Fatalf("expected archived page: %v", err)
	}
}

func TestRegisterSourceCreatesSummaryAndRegistryEntry(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app, "workflows/ingest.md")

	if _, err := app.handleRegisterSource(map[string]any{
		"source_id":     "SRC-20260614-real-world",
		"title":         "Real World Adoption",
		"raw_path":      "raw/inbox/REAL_WORLD.md",
		"source_type":   "owner note",
		"trust_level":   "trusted-owner",
		"received_date": "2026-06-14",
		"ingest_date":   "2026-06-14",
		"summary":       "Adoption requirements.",
	}); err != nil {
		t.Fatalf("register_source should succeed: %v", err)
	}

	summary, err := os.ReadFile(filepath.Join(app.wikiRoot, "sources/SRC-20260614-real-world.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(summary), "Real World Adoption") {
		t.Fatalf("summary should contain title, got %s", string(summary))
	}
	registry, err := os.ReadFile(filepath.Join(app.wikiRoot, "source-registry.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(registry), "SRC-20260614-real-world") {
		t.Fatalf("registry should contain source ID, got %s", string(registry))
	}
}

func TestSessionEnforcement_UpdateRequiresBeginSessionAndRequiredReads(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	if _, err := app.handleSetupLLMWiki(map[string]any{}); err != nil {
		t.Fatal(err)
	}

	params := map[string]any{
		"path":    "notes/a.md",
		"content": "content",
	}

	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("expected update to require begin_session")
	}
	if !strings.Contains(err.Error(), "begin_session") {
		t.Fatalf("expected begin_session guidance, got: %v", err)
	}

	if _, err := app.handleBeginSession(map[string]any{}); err != nil {
		t.Fatalf("begin_session should succeed: %v", err)
	}

	_, err = app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("expected update to require startup page reads")
	}
	if !strings.Contains(err.Error(), "index.md") || !strings.Contains(err.Error(), "agent-rules.md") {
		t.Fatalf("expected missing startup pages, got: %v", err)
	}

	readRequiredStartupPages(t, app)

	if _, err := app.handleUpdateWikiPage(params); err != nil {
		t.Fatalf("update should succeed after session startup reads: %v", err)
	}
}

func TestSessionEnforcement_SourceWriteRequiresIngestWorkflow(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "sources/SRC-20260613-test.md",
		"content": "source summary",
	}
	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("expected source write to require ingest workflow read")
	}
	if !strings.Contains(err.Error(), "workflows/ingest.md") {
		t.Fatalf("expected ingest workflow requirement, got: %v", err)
	}

	if _, err := app.handleReadWikiPage(map[string]any{"path": "workflows/ingest.md"}); err != nil {
		t.Fatalf("read workflows/ingest.md: %v", err)
	}
	if _, err := app.handleUpdateWikiPage(params); err != nil {
		t.Fatalf("source write should succeed after ingest workflow read: %v", err)
	}
}

func TestSessionEnforcement_SynthesisWriteRequiresQueryWorkflow(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "syntheses/answer.md",
		"content": "durable answer",
	}
	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("expected synthesis write to require query workflow read")
	}
	if !strings.Contains(err.Error(), "workflows/query.md") {
		t.Fatalf("expected query workflow requirement, got: %v", err)
	}

	if _, err := app.handleReadWikiPage(map[string]any{"path": "workflows/query.md"}); err != nil {
		t.Fatalf("read workflows/query.md: %v", err)
	}
	if _, err := app.handleUpdateWikiPage(params); err != nil {
		t.Fatalf("synthesis write should succeed after query workflow read: %v", err)
	}
}

func TestSessionFinishRequiresLogAndIndexForNewPages(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	if _, err := app.handleUpdateWikiPage(map[string]any{
		"path":    "notes/a.md",
		"content": "content",
	}); err != nil {
		t.Fatalf("update should succeed: %v", err)
	}

	_, err := app.handleFinishSession(map[string]any{})
	if err == nil {
		t.Fatal("expected finish_session to require log and index updates")
	}
	if !strings.Contains(err.Error(), "log.md") || !strings.Contains(err.Error(), "index.md") {
		t.Fatalf("expected log.md and index.md requirements, got: %v", err)
	}

	if _, err := app.handleUpdateWikiPage(map[string]any{
		"path":    "index.md",
		"content": "# LLM Wiki Index\n\n- notes/a.md\n",
	}); err != nil {
		t.Fatalf("index update should succeed: %v", err)
	}
	if _, err := app.handleAppendLog(map[string]any{
		"log_file": "log.md",
		"content":  "## [2026-06-13] maintenance | Added notes page",
	}); err != nil {
		t.Fatalf("append log should succeed: %v", err)
	}
	if _, err := app.handleFinishSession(map[string]any{}); err != nil {
		t.Fatalf("finish_session should succeed after log and index updates: %v", err)
	}
}

func TestSessionFinishRequiresSourceRegistryForSourcePages(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app, "workflows/ingest.md")

	if _, err := app.handleUpdateWikiPage(map[string]any{
		"path":    "sources/SRC-20260613-test.md",
		"content": "source summary",
	}); err != nil {
		t.Fatalf("source update should succeed: %v", err)
	}
	if _, err := app.handleUpdateWikiPage(map[string]any{
		"path":    "index.md",
		"content": "# LLM Wiki Index\n\n- sources/SRC-20260613-test.md\n",
	}); err != nil {
		t.Fatalf("index update should succeed: %v", err)
	}
	if _, err := app.handleAppendLog(map[string]any{
		"log_file": "log.md",
		"content":  "## [2026-06-13] ingest | Test source",
	}); err != nil {
		t.Fatalf("append log should succeed: %v", err)
	}

	_, err := app.handleFinishSession(map[string]any{})
	if err == nil {
		t.Fatal("expected finish_session to require source-registry.md")
	}
	if !strings.Contains(err.Error(), "source-registry.md") {
		t.Fatalf("expected source registry requirement, got: %v", err)
	}

	if _, err := app.handleUpdateWikiPage(map[string]any{
		"path":    "source-registry.md",
		"content": "# Source Registry\n\n- SRC-20260613-test\n",
	}); err != nil {
		t.Fatalf("source registry update should succeed: %v", err)
	}
	if _, err := app.handleAppendLog(map[string]any{
		"log_file": "log.md",
		"content":  "## [2026-06-13] maintenance | Updated registry",
	}); err != nil {
		t.Fatalf("append log should succeed: %v", err)
	}
	if _, err := app.handleFinishSession(map[string]any{}); err != nil {
		t.Fatalf("finish_session should succeed after source registry update: %v", err)
	}
}

func TestSessionStatusReportsPendingRequirements(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	if _, err := app.handleUpdateWikiPage(map[string]any{
		"path":    "notes/a.md",
		"content": "content",
	}); err != nil {
		t.Fatalf("update should succeed: %v", err)
	}

	result, err := app.handleSessionStatus(map[string]any{})
	if err != nil {
		t.Fatalf("session_status should succeed: %v", err)
	}
	text := toolText(t, result)
	if !strings.Contains(text, `"needs_log":true`) || !strings.Contains(text, `"needs_index":true`) {
		t.Fatalf("status should report pending log and index requirements, got: %s", text)
	}
}

func TestMCPToolCall_SessionLifecycleTools(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	if _, err := app.handleSetupLLMWiki(map[string]any{}); err != nil {
		t.Fatal(err)
	}

	for _, raw := range []json.RawMessage{
		json.RawMessage(`{"name":"begin_session","arguments":{}}`),
		json.RawMessage(`{"name":"session_status","arguments":{}}`),
	} {
		if _, err := app.handleToolCall(raw); err != nil {
			t.Fatalf("tool call should succeed: %v", err)
		}
	}
}

// ---------------------------------------------------------------------------
// Phase 5, Step 13: PBAC enforcement tests
// ---------------------------------------------------------------------------

func TestPBAC_UpdateProtectedFileIsRejected(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md", "architecture/*", "core-decisions/*"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "rules.md",
		"content": "overwritten",
	}

	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("expected error when updating protected file, got nil")
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("expected semantic 'read-only' message, got: %s", err.Error())
	}
}

func TestPBAC_UpdateProtectedNestedDirIsRejected(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"architecture/*"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "architecture/sub/deep.md",
		"content": "overwritten",
	}

	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("expected error for nested path under protected glob, got nil")
	}
}

func TestPBAC_UpdateUnprotectedFileSucceeds(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "drafts/proposal.md",
		"content": "some content",
	}

	result, err := app.handleUpdateWikiPage(params)
	if err != nil {
		t.Fatalf("update to unprotected file should succeed: %v", err)
	}
	resultMap, _ := result.(map[string]any)
	contentArr, _ := resultMap["content"].([]map[string]any)
	if len(contentArr) == 0 || contentArr[0]["text"] == "" {
		t.Errorf("expected non-empty MCP content response, got %v", result)
	}
}

func TestPBAC_AppendLogBypassesGovernance(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"core-decisions/*"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"log_file": "core-decisions/record.md",
		"content":  "new decision entry",
	}

	result, err := app.handleAppendLog(params)
	if err != nil {
		t.Fatalf("append_log should bypass governance for protected paths: %v", err)
	}
	resultMap, _ := result.(map[string]any)
	contentArr, _ := resultMap["content"].([]map[string]any)
	if len(contentArr) == 0 || contentArr[0]["text"] == "" {
		t.Errorf("expected non-empty MCP content response, got %v", result)
	}
}

func TestPBAC_AppendLogBlockedOnDirectFile(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md", "core-decisions/*"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	// append_log to a directly named protected file should be blocked.
	params := map[string]any{
		"log_file": "rules.md",
		"content":  "sneaky append",
	}
	_, err := app.handleAppendLog(params)
	if err == nil {
		t.Fatal("append_log should block appending to directly protected files")
	}
	if !strings.Contains(err.Error(), "directly protected") {
		t.Errorf("expected 'directly protected' message, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Phase 5, Step 14: Semantic error formatting tests
// ---------------------------------------------------------------------------

func TestSemanticError_ProtectedFileMessageFormat(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	params := map[string]any{
		"path":    "rules.md",
		"content": "overwrite attempt",
	}

	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	msg := err.Error()
	// Per PROJECT_DESIGN.md §3.3: must mention the file, say "read-only", and
	// suggest drafts.
	if !strings.Contains(msg, "rules.md") {
		t.Errorf("error should mention the file path, got: %s", msg)
	}
	if !strings.Contains(msg, "read-only") {
		t.Errorf("error should say 'read-only', got: %s", msg)
	}
	if !strings.Contains(msg, "draft") {
		t.Errorf("error should suggest using drafts, got: %s", msg)
	}
}

func TestSemanticError_PathTraversalMessageFormat(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	params := map[string]any{
		"path": "../../etc/passwd",
	}

	_, err := app.handleReadWikiPage(params)
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error should mention path escaping, got: %s", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Phase 5, Step 15: Path traversal rejection tests
// ---------------------------------------------------------------------------

func TestPathTraversal_ReadIsBlocked(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	attacks := []string{
		"../../etc/passwd",
		"../../../etc/shadow",
		"sub/../../..",
		"../sibling/secret.md",
	}

	for _, attack := range attacks {
		params := map[string]any{"path": attack}
		_, err := app.handleReadWikiPage(params)
		if err == nil {
			t.Errorf("read_wiki_page should reject traversal path %q", attack)
		}
	}
}

func TestPathTraversal_UpdateIsBlocked(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	params := map[string]any{
		"path":    "../../etc/evil",
		"content": "pwned",
	}

	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("update_wiki_page should reject traversal path")
	}
}

func TestPathTraversal_CreateDraftIsBlocked(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	params := map[string]any{
		"name":    "../../etc/evil",
		"content": "pwned",
	}

	_, err := app.handleCreateDraft(params)
	if err == nil {
		t.Fatal("create_draft should reject traversal path")
	}
}

func TestPathTraversal_AppendLogIsBlocked(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	params := map[string]any{
		"log_file": "../../etc/evil",
		"content":  "pwned",
	}

	_, err := app.handleAppendLog(params)
	if err == nil {
		t.Fatal("append_log should reject traversal path")
	}
}

func TestPathTraversal_UpdateThroughSymlinkParentIsBlocked(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	outsideDir := t.TempDir()
	if err := os.Symlink(outsideDir, filepath.Join(app.wikiRoot, "linked")); err != nil {
		t.Skipf("symlink not available: %v", err)
	}

	params := map[string]any{
		"path":    "linked/escape.md",
		"content": "outside write",
	}
	_, err := app.handleUpdateWikiPage(params)
	if err == nil {
		t.Fatal("update_wiki_page should reject writes through symlink parent")
	}
	if _, statErr := os.Stat(filepath.Join(outsideDir, "escape.md")); !os.IsNotExist(statErr) {
		t.Fatalf("outside target should not be created, stat err: %v", statErr)
	}
}

func TestPathTraversal_ValidPathSucceeds(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()
	prepareSessionForWrites(t, app)

	// Write a file first, then read it.
	writeParams := map[string]any{
		"path":    "notes/test.md",
		"content": "hello world",
	}
	if _, err := app.handleUpdateWikiPage(writeParams); err != nil {
		t.Fatalf("write should succeed for valid path: %v", err)
	}

	readParams := map[string]any{"path": "notes/test.md"}
	result, err := app.handleReadWikiPage(readParams)
	if err != nil {
		t.Fatalf("read should succeed for valid path: %v", err)
	}

	resultMap, _ := result.(map[string]any)
	contentArr, _ := resultMap["content"].([]map[string]any)
	if len(contentArr) == 0 {
		t.Fatal("expected non-empty MCP content response")
	}
	text, _ := contentArr[0]["text"].(string)
	if text != "hello world" {
		t.Errorf("expected 'hello world', got %q", text)
	}
}

func TestGitignoreDoesNotIgnoreScriniumSourceDirectory(t *testing.T) {
	cmd := exec.Command("git", "check-ignore", "cmd/scrinium/app.go")
	cmd.Dir = filepath.Clean("../..")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("cmd/scrinium/app.go must not be ignored by git, got: %s", string(output))
	}
	if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		t.Fatalf("git check-ignore failed unexpectedly: %v output: %s", err, string(output))
	}
}

// ---------------------------------------------------------------------------
// MCP Protocol Flow Tests
// ---------------------------------------------------------------------------

func TestMCPToolCall_CorrectParamFormat(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	// Write a file first so we can read it via MCP.
	if err := os.WriteFile(filepath.Join(app.wikiRoot, "test.md"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	// Simulate real MCP tools/call params: {"name": "...", "arguments": {...}}
	raw := json.RawMessage(`{"name":"read_wiki_page","arguments":{"path":"test.md"}}`)
	result, err := app.handleToolCall(raw)
	if err != nil {
		t.Fatalf("handleToolCall should succeed: %v", err)
	}

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	contentArr, _ := resultMap["content"].([]map[string]any)
	if len(contentArr) == 0 {
		t.Fatal("expected non-empty MCP content response")
	}
	text, _ := contentArr[0]["text"].(string)
	if text != "hello" {
		t.Errorf("expected 'hello', got %q", text)
	}
}

func TestMCPToolCall_Capabilities(t *testing.T) {
	oldVersion := version
	version = "4.5.6-test"
	defer func() {
		version = oldVersion
	}()

	app, cleanup := newTestApp(t, []string{"rules.md"})
	defer cleanup()

	raw := json.RawMessage(`{"name":"capabilities","arguments":{}}`)
	result, err := app.handleToolCall(raw)
	if err != nil {
		t.Fatalf("capabilities tool call should succeed: %v", err)
	}

	resultMap, _ := result.(map[string]any)
	contentArr, _ := resultMap["content"].([]map[string]any)
	if len(contentArr) == 0 {
		t.Fatal("expected non-empty MCP content response")
	}
	text, _ := contentArr[0]["text"].(string)
	if !strings.Contains(text, "Scrinium") {
		t.Errorf("capabilities response should mention Scrinium, got %q", text)
	}
	if !strings.Contains(text, "setup_llm_wiki") {
		t.Errorf("capabilities response should mention setup_llm_wiki, got %q", text)
	}
	if !strings.Contains(text, `"version":"4.5.6-test"`) {
		t.Errorf("capabilities response should include Scrinium version, got %q", text)
	}
}

func TestMCPToolCall_ErrorReturnsIsError(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md"})
	defer cleanup()
	prepareSessionForWrites(t, app)

	// Try to update a protected file — should return isError content,
	// not a Go error (since dispatch wraps tool errors in mcpErrorResult).
	raw := json.RawMessage(`{"name":"update_wiki_page","arguments":{"path":"rules.md","content":"hack"}}`)
	_, err := app.handleToolCall(raw)
	if err == nil {
		t.Fatal("expected error for protected file write")
	}

	// Verify dispatch wraps it correctly.
	result, rpcErr := app.dispatch("tools/call", raw)
	if rpcErr != nil {
		t.Fatalf("dispatch should NOT return JSON-RPC error for tool failures, got: %v", rpcErr)
	}
	resultMap, _ := result.(map[string]any)
	if isErr, _ := resultMap["isError"].(bool); !isErr {
		t.Error("expected isError: true in tool error response")
	}
}

func TestConfigValidation_EmptyWikiRoot(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "scrinium-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	configPath := filepath.Join(tempDir, "bad.json")
	if err := os.WriteFile(configPath, []byte(`{"wiki_root": ""}`), 0644); err != nil {
		t.Fatal(err)
	}

	_, err = NewApp(configPath)
	if err == nil {
		t.Fatal("expected error for empty wiki_root")
	}
	if !strings.Contains(err.Error(), "wiki_root must not be empty") {
		t.Errorf("expected wiki_root validation error, got: %v", err)
	}
}

func TestCreateDraft_EscapeDraftsDir(t *testing.T) {
	app, cleanup := newTestApp(t, []string{"rules.md"})
	defer cleanup()

	// Attempt to escape drafts/ via "../rules.md" — should be blocked.
	params := map[string]any{
		"name":    "../rules.md",
		"content": "governance bypass attempt",
	}
	_, err := app.handleCreateDraft(params)
	if err == nil {
		t.Fatal("create_draft should reject names that escape drafts/ directory")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error should mention escaping, got: %v", err)
	}
}

func TestResourceRead(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	if err := os.WriteFile(filepath.Join(app.wikiRoot, "page.md"), []byte("resource content"), 0644); err != nil {
		t.Fatal(err)
	}

	raw := json.RawMessage(`{"uri":"llm-wiki://page.md"}`)
	result, err := app.handleResourceRead(raw)
	if err != nil {
		t.Fatalf("resource read should succeed: %v", err)
	}

	resultMap, _ := result.(map[string]any)
	contents, _ := resultMap["contents"].([]map[string]any)
	if len(contents) == 0 {
		t.Fatal("expected non-empty contents array")
	}
	if contents[0]["text"] != "resource content" {
		t.Errorf("expected 'resource content', got %q", contents[0]["text"])
	}
	if contents[0]["mimeType"] != "text/markdown" {
		t.Errorf("expected text/markdown, got %q", contents[0]["mimeType"])
	}
}

func prepareSessionForWrites(t *testing.T, app *App, extraReads ...string) {
	t.Helper()

	if _, err := app.handleSetupLLMWiki(map[string]any{}); err != nil {
		t.Fatalf("setup_llm_wiki: %v", err)
	}
	if _, err := app.handleBeginSession(map[string]any{}); err != nil {
		t.Fatalf("begin_session: %v", err)
	}
	readRequiredStartupPages(t, app)
	for _, path := range extraReads {
		if _, err := app.handleReadWikiPage(map[string]any{"path": path}); err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
	}
}

func readRequiredStartupPages(t *testing.T, app *App) {
	t.Helper()

	for _, path := range []string{"index.md", "agent-rules.md"} {
		if _, err := app.handleReadWikiPage(map[string]any{"path": path}); err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
	}
}

func toolText(t *testing.T, result any) string {
	t.Helper()

	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", result)
	}
	contentArr, _ := resultMap["content"].([]map[string]any)
	if len(contentArr) == 0 {
		t.Fatal("expected non-empty MCP content response")
	}
	text, _ := contentArr[0]["text"].(string)
	return text
}

func TestRunCLIMCPTools(t *testing.T) {
	// Create a temporary directory with a scrinium.json and a dummy wiki
	tmpDir, err := os.MkdirTemp("", "scrinium-cli-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configContent := `{
		"wiki_root": "./llm-wiki",
		"write_governance": {
			"protected_files": [
				"rules.md"
			]
		}
	}`
	configPath := filepath.Join(tmpDir, "scrinium.json")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	wikiRoot := filepath.Join(tmpDir, "llm-wiki")
	if err := os.MkdirAll(wikiRoot, 0755); err != nil {
		t.Fatalf("failed to create wiki root: %v", err)
	}

	indexContent := "# Index"
	if err := os.WriteFile(filepath.Join(wikiRoot, "index.md"), []byte(indexContent), 0644); err != nil {
		t.Fatalf("failed to write index: %v", err)
	}

	// 1. Test capabilities subcommand
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"capabilities", "--repo", tmpDir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "read_wiki_page") || !strings.Contains(stdout.String(), "rules.md") {
		t.Fatalf("unexpected capabilities output: %s", stdout.String())
	}

	// 2. Test read_wiki_page subcommand
	stdout.Reset()
	stderr.Reset()
	code = RunCLI([]string{"read_wiki_page", "--repo", tmpDir, "--path", "index.md"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "# Index") {
		t.Fatalf("unexpected read_wiki_page output: %s", stdout.String())
	}

	// 3. Test missing config error
	stdout.Reset()
	stderr.Reset()
	code = RunCLI([]string{"capabilities", "--repo", filepath.Join(tmpDir, "non-existent")}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "failed to load config") {
		t.Fatalf("expected failed to load config error, got: %s", stderr.String())
	}
}
