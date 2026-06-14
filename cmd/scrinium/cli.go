package scrinium

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	scriniumBlockBegin = "<!-- BEGIN SCRINIUM ENFORCEMENT -->"
	scriniumBlockEnd   = "<!-- END SCRINIUM ENFORCEMENT -->"
)

var version = "0.1.0"

// IsCLISubcommand reports whether args select a normal CLI command instead of
// MCP stdio server mode.
func IsCLISubcommand(args []string) bool {
	return len(args) > 0 && (args[0] == "enforce-agents" || args[0] == "version")
}

// RunCLI executes Scrinium's non-MCP CLI commands.
func RunCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: scrinium enforce-agents [--repo PATH] [--agents LIST] [--dry-run] [--check]")
		return 2
	}

	switch args[0] {
	case "enforce-agents":
		if err := runEnforceAgents(args[1:], stdout); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintf(stderr, "scrinium enforce-agents: %v\n", err)
			return 1
		}
		return 0
	case "version":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "Usage: scrinium version")
			return 2
		}
		fmt.Fprintf(stdout, "scrinium %s\n", version)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown command %q\n", args[0])
		return 2
	}
}

type enforceAgentsOptions struct {
	repo   string
	agents []string
	dryRun bool
	check  bool
}

type enforcementFile struct {
	path    string
	content string
}

func runEnforceAgents(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("enforce-agents", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintln(stdout, "Usage: scrinium enforce-agents [--repo PATH] [--agents LIST] [--dry-run] [--check]")
		fs.PrintDefaults()
	}

	var agentsCSV string
	opts := enforceAgentsOptions{}
	fs.StringVar(&opts.repo, "repo", ".", "repository root to update")
	fs.StringVar(&agentsCSV, "agents", "codex,claudecode,opencode,antigravity", "comma-separated agent targets")
	fs.BoolVar(&opts.dryRun, "dry-run", false, "print planned writes without changing files")
	fs.BoolVar(&opts.check, "check", false, "exit non-zero if generated enforcement is stale or missing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}

	agents, err := parseAgentList(agentsCSV)
	if err != nil {
		return err
	}
	opts.agents = agents

	files, err := buildEnforcementFiles(opts)
	if err != nil {
		return err
	}
	changed, err := applyEnforcementFiles(opts, files, stdout)
	if err != nil {
		return err
	}
	if opts.check && changed {
		return fmt.Errorf("agent enforcement is not current")
	}
	if opts.check {
		fmt.Fprintln(stdout, "agent enforcement is current")
	}
	return nil
}

func parseAgentList(value string) ([]string, error) {
	known := map[string]bool{
		"codex":       true,
		"claudecode":  true,
		"opencode":    true,
		"antigravity": true,
	}
	seen := make(map[string]bool)
	agents := make([]string, 0, 4)
	for _, raw := range strings.Split(value, ",") {
		agent := strings.ToLower(strings.TrimSpace(raw))
		if agent == "" {
			continue
		}
		if !known[agent] {
			return nil, fmt.Errorf("unknown agent %q", raw)
		}
		if !seen[agent] {
			seen[agent] = true
			agents = append(agents, agent)
		}
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("at least one agent must be selected")
	}
	sort.Strings(agents)
	return agents, nil
}

func buildEnforcementFiles(opts enforceAgentsOptions) ([]enforcementFile, error) {
	repo, err := filepath.Abs(opts.repo)
	if err != nil {
		return nil, fmt.Errorf("resolve repo path: %w", err)
	}
	configPath := filepath.Join(repo, "scrinium.json")
	agentList := strings.Join(opts.agents, ", ")

	files := []enforcementFile{
		{
			path:    filepath.Join(repo, "AGENTS.md"),
			content: agentInstructionsContent(agentList, configPath),
		},
		{
			path:    filepath.Join(repo, "CLAUDE.md"),
			content: claudeInstructionsContent(agentList, configPath),
		},
		{
			path:    filepath.Join(repo, "docs", "scrinium-agent-enforcement.md"),
			content: agentEnforcementDocContent(agentList, configPath),
		},
	}
	return files, nil
}

func applyEnforcementFiles(opts enforceAgentsOptions, files []enforcementFile, stdout io.Writer) (bool, error) {
	changed := false
	for _, file := range files {
		current, err := readOptionalFile(file.path)
		if err != nil {
			return false, err
		}
		next := upsertManagedBlock(current, defaultPreamble(filepath.Base(file.path)), file.content)
		rel, relErr := filepath.Rel(opts.repo, file.path)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			rel = file.path
		}
		rel = filepath.ToSlash(rel)
		if current == next {
			if opts.dryRun {
				fmt.Fprintf(stdout, "unchanged %s\n", rel)
			}
			continue
		}
		changed = true
		if opts.dryRun || opts.check {
			fmt.Fprintf(stdout, "would update %s\n", rel)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(file.path), 0755); err != nil {
			return false, fmt.Errorf("create parent directory for %s: %w", rel, err)
		}
		if err := os.WriteFile(file.path, []byte(next), 0644); err != nil {
			return false, fmt.Errorf("write %s: %w", rel, err)
		}
		fmt.Fprintf(stdout, "updated %s\n", rel)
	}
	return changed, nil
}

func readOptionalFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return string(data), nil
	}
	if os.IsNotExist(err) {
		return "", nil
	}
	return "", fmt.Errorf("read %s: %w", path, err)
}

func upsertManagedBlock(current, preamble, block string) string {
	managed := scriniumBlockBegin + "\n" + strings.TrimSpace(block) + "\n" + scriniumBlockEnd
	if current == "" {
		return strings.TrimSpace(preamble) + "\n\n" + managed + "\n"
	}

	start := strings.Index(current, scriniumBlockBegin)
	end := strings.Index(current, scriniumBlockEnd)
	if start >= 0 && end >= start {
		end += len(scriniumBlockEnd)
		next := current[:start] + managed + current[end:]
		if !strings.HasSuffix(next, "\n") {
			next += "\n"
		}
		return next
	}

	next := strings.TrimRight(current, "\n") + "\n\n" + managed + "\n"
	return next
}

func defaultPreamble(base string) string {
	switch base {
	case "AGENTS.md":
		return "# AGENTS.md"
	case "CLAUDE.md":
		return "# Claude Code Instructions"
	default:
		return "# Scrinium Agent Enforcement"
	}
}

func agentInstructionsContent(agentList, configPath string) string {
	return sharedEnforcementBlock("Codex, OpenCode, Antigravity-compatible agents", agentList, configPath)
}

func claudeInstructionsContent(agentList, configPath string) string {
	return sharedEnforcementBlock("Claude Code", agentList, configPath)
}

func sharedEnforcementBlock(audience, agentList, configPath string) string {
	return fmt.Sprintf(`# Scrinium Enforcement

Audience: %s.
Generated for agents: %s.

Scrinium is the project memory and governance server. Treat `+"`llm-wiki/`"+` as the source of truth for durable project context.

## Required Loop

1. Start Scrinium MCP with command `+"`scrinium`"+` and args `+"`%s`"+`.
2. After any harness or plugin bootstrap instructions are loaded, call Scrinium `+"`capabilities`"+` before project work or wiki writes.
3. Call `+"`begin_session`"+` before project changes.
4. Read `+"`index.md`"+` and `+"`agent-rules.md`"+` with `+"`read_wiki_page`"+`.
5. Read any relevant workflow pages before specialized wiki work.
6. Make project changes.
7. Update `+"`llm-wiki`"+` through Scrinium tools so durable context stays current.
8. Update `+"`log.md`"+`, `+"`index.md`"+`, and `+"`source-registry.md`"+` when Scrinium reports they are required.
9. Call `+"`session_status`"+`.
10. Call `+"`finish_session`"+` before reporting completion.

Do not report completion while `+"`finish_session`"+` fails. Satisfy its pending maintenance checklist first.

## Boundaries

Scrinium can enforce wiki writes made through its MCP tools. It cannot see arbitrary direct filesystem edits unless the agent records them back into the wiki before finishing.
`, audience, agentList, configPath)
}

func agentEnforcementDocContent(agentList, configPath string) string {
	return fmt.Sprintf(`# Scrinium Agent Enforcement

Generated agent targets: %s.

Use this command to refresh the repository instruction files:

`+"```bash"+`
scrinium enforce-agents
`+"```"+`

## MCP Configuration Snippet

Use the same Scrinium MCP server configuration for Codex, Claude Code, OpenCode, and Antigravity where MCP server configuration is supported:

`+"```json"+`
{
  "mcpServers": {
    "scrinium": {
      "command": "scrinium",
      "args": ["%s"]
    }
  }
}
`+"```"+`

## Instruction Files

- `+"`AGENTS.md`"+` carries the shared enforcement block for Codex, OpenCode, Antigravity-compatible agents, and other tools that honor AGENTS-style repository instructions.
- `+"`CLAUDE.md`"+` carries the same enforcement block for Claude Code.

Tool-specific config file names can change. Prefer this shared instruction layer plus the MCP snippet unless a tool's current documentation defines a stable project-local config path.
`, agentList, configPath)
}
