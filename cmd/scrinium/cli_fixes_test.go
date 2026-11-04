package scrinium

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeEnforceConfig(t *testing.T, repo, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "scrinium.json"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// SCR-01: -h/--help print usage instead of being taken as a config path.
func TestHelpFlagsPrintUsage(t *testing.T) {
	for _, flagName := range []string{"-h", "--help"} {
		t.Run(flagName, func(t *testing.T) {
			if !IsCLISubcommand([]string{flagName}) {
				t.Fatalf("%s must be handled by the CLI, not treated as an MCP config path", flagName)
			}
			var stdout, stderr bytes.Buffer
			if code := RunCLI([]string{flagName}, &stdout, &stderr); code != 0 {
				t.Fatalf("%s exit code = %d stderr=%s", flagName, code, stderr.String())
			}
			if stderr.Len() != 0 {
				t.Fatalf("%s wrote to stderr: %s", flagName, stderr.String())
			}
			out := stdout.String()
			if !strings.Contains(out, "Usage: scrinium") {
				t.Fatalf("usage text missing:\n%s", out)
			}
			for name := range cliSubcommands {
				if !strings.Contains(out, name) {
					t.Fatalf("usage text must list subcommand %q:\n%s", name, out)
				}
			}
			if !strings.Contains(out, "scrinium.json") {
				t.Fatalf("usage text must describe MCP server mode with a config path:\n%s", out)
			}
		})
	}
	if IsCLISubcommand([]string{"--helpx"}) || IsCLISubcommand([]string{"scrinium.json"}) {
		t.Fatal("only exact -h/--help select help; a config path must still start the MCP server")
	}
}

// SCR-02: enforcement targets are configurable so AGENTS.md can be the only
// file written; CLAUDE.md is never created when it is not selected.
func TestEnforceAgentsFilesFlagLimitsTargets(t *testing.T) {
	repo := t.TempDir()
	writeEnforceConfig(t, repo, `{"wiki_root":"./llm-wiki"}`)

	var stdout, stderr bytes.Buffer
	if code := RunCLI([]string{"enforce-agents", "--repo", repo, "--files", "agents"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d stderr=%s", code, stderr.String())
	}
	if !pathExists(filepath.Join(repo, "AGENTS.md")) {
		t.Fatal("AGENTS.md must be written")
	}
	if pathExists(filepath.Join(repo, "CLAUDE.md")) {
		t.Fatal("CLAUDE.md must not be created when only agents is selected")
	}
	if pathExists(filepath.Join(repo, "docs", "scrinium-agent-enforcement.md")) {
		t.Fatal("the enforcement doc must not be created when only agents is selected")
	}

	stdout.Reset()
	stderr.Reset()
	if code := RunCLI([]string{"enforce-agents", "--repo", repo, "--files", "agents", "--check"}, &stdout, &stderr); code != 0 {
		t.Fatalf("--check must pass for the selected files only, code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestEnforceAgentsConfigSelectsTargetsAndFlagOverrides(t *testing.T) {
	repo := t.TempDir()
	writeEnforceConfig(t, repo, `{"wiki_root":"./llm-wiki","enforcement":{"files":["agents"]}}`)

	var stdout, stderr bytes.Buffer
	if code := RunCLI([]string{"enforce-agents", "--repo", repo}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d stderr=%s", code, stderr.String())
	}
	if pathExists(filepath.Join(repo, "CLAUDE.md")) || pathExists(filepath.Join(repo, "docs")) {
		t.Fatal("configured agents-only enforcement must not create CLAUDE.md or docs/")
	}

	stdout.Reset()
	stderr.Reset()
	if code := RunCLI([]string{"enforce-agents", "--repo", repo, "--files", "agents,claude"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d stderr=%s", code, stderr.String())
	}
	if !pathExists(filepath.Join(repo, "CLAUDE.md")) {
		t.Fatal("an explicit --files must override the configured selection")
	}
}

func TestEnforceAgentsAgentsOnlyAudienceCoversClaudeCode(t *testing.T) {
	repo := t.TempDir()
	writeEnforceConfig(t, repo, `{"wiki_root":"./llm-wiki"}`)
	var stdout, stderr bytes.Buffer
	if code := RunCLI([]string{"enforce-agents", "--repo", repo, "--files", "agents,doc"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d stderr=%s", code, stderr.String())
	}
	agents, _ := os.ReadFile(filepath.Join(repo, "AGENTS.md"))
	if !strings.Contains(string(agents), "Claude Code") {
		t.Fatalf("AGENTS.md must address Claude Code when CLAUDE.md is not generated:\n%s", agents)
	}
	doc, _ := os.ReadFile(filepath.Join(repo, "docs", "scrinium-agent-enforcement.md"))
	if strings.Contains(string(doc), "CLAUDE.md") {
		t.Fatalf("the doc must not describe a CLAUDE.md that is not generated:\n%s", doc)
	}
}

func TestEnforceAgentsRejectsBadSelectionWithoutWriting(t *testing.T) {
	cases := map[string]struct {
		config string
		args   []string
	}{
		"unknown flag value":   {`{"wiki_root":"./w"}`, []string{"--files", "agents,gemini"}},
		"empty flag value":     {`{"wiki_root":"./w"}`, []string{"--files", " , "}},
		"unknown config value": {`{"wiki_root":"./w","enforcement":{"files":["nope"]}}`, []string{}},
		"empty config list":    {`{"wiki_root":"./w","enforcement":{"files":[]}}`, []string{}},
		"malformed config":     {`{"wiki_root":`, []string{}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			repo := t.TempDir()
			writeEnforceConfig(t, repo, tc.config)
			var stdout, stderr bytes.Buffer
			args := append([]string{"enforce-agents", "--repo", repo}, tc.args...)
			if code := RunCLI(args, &stdout, &stderr); code == 0 {
				t.Fatalf("expected failure, stdout=%s", stdout.String())
			}
			for _, name := range []string{"AGENTS.md", "CLAUDE.md", "docs"} {
				if pathExists(filepath.Join(repo, name)) {
					t.Fatalf("%s must not be written on a rejected selection", name)
				}
			}
		})
	}
}

// SCR-03: the skeleton created by setup_llm_wiki passes its own lint.
func TestSetupLLMWikiSkeletonPassesLint(t *testing.T) {
	app, cleanup := newTestApp(t, nil)
	defer cleanup()

	if _, err := app.handleSetupLLMWiki(map[string]any{}); err != nil {
		t.Fatalf("setup_llm_wiki: %v", err)
	}
	result, err := app.handleLintLLMWiki(map[string]any{})
	if err != nil {
		t.Fatalf("lint_llm_wiki: %v", err)
	}
	var report struct {
		OK       bool `json:"ok"`
		Findings []struct {
			Path string `json:"path"`
			Code string `json:"code"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(toolText(t, result)), &report); err != nil {
		t.Fatalf("lint report is not JSON: %v", err)
	}
	if !report.OK || len(report.Findings) != 0 {
		t.Fatalf("the skeleton must pass its own lint, got %+v", report.Findings)
	}
}
