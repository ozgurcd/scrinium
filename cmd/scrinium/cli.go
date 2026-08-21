package scrinium

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"scrinium/internal/publicapi"
)

const (
	scriniumBlockBegin = "<!-- BEGIN SCRINIUM ENFORCEMENT -->"
	scriniumBlockEnd   = "<!-- END SCRINIUM ENFORCEMENT -->"
)

var version = "0.1.0"

// IsCLISubcommand reports whether args select a normal CLI command instead of
// MCP stdio server mode.
func IsCLISubcommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	known := map[string]bool{
		"enforce-agents":              true,
		"version":                     true,
		"capabilities":                true,
		"claim_create":                true,
		"claim_get":                   true,
		"claim_list":                  true,
		"claim_update":                true,
		"claim_add_evidence":          true,
		"claim_set_validation_policy": true,
		"claim_validate":              true,
		"claim_supersede":             true,
		"claim_withdraw":              true,
		"claim_lint":                  true,
		"source_register":             true,
		"source_get":                  true,
		"source_list":                 true,
		"source_refresh":              true,
		"source_migration_status":     true,
		"session_begin":               true,
		"session_continue":            true,
		"session_finish":              true,
		"session_abandon":             true,
		"session_list":                true,
		"setup_llm_wiki":              true,
		"begin_session":               true,
		"continue_session":            true,
		"session_status":              true,
		"finish_session":              true,
		"abandon_session":             true,
		"list_active_sessions":        true,
		"lint_llm_wiki":               true,
		"adopt_llm_wiki":              true,
		"assess_source_migration":     true,
		"apply_source_migration":      true,
		"rebuild_source_registry":     true,
		"register_source":             true,
		"create_page":                 true,
		"move_page":                   true,
		"archive_page":                true,
		"read_wiki_page":              true,
		"update_wiki_page":            true,
		"create_draft":                true,
		"append_log":                  true,
	}
	return known[args[0]]
}

// RunCLI executes Scrinium's non-MCP CLI commands.
func RunCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "Usage: scrinium [enforce-agents|version|<mcp_tool_name>] ...")
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
		// Any other known subcommand is delegated as an MCP tool call
		if err := runMCPToolAsCLI(args[0], args[1:], stdout); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			var reported *reportedCLIError
			if errors.As(err, &reported) {
				return 1
			}
			if publicCLICommand(args[0]) && requestedJSON(args[1:]) {
				encoded, encodeErr := json.Marshal(publicapi.PublicError{
					SchemaVersion: publicapi.ErrorSchema, Code: "invalid_input", Message: err.Error(), Operation: args[0],
				})
				if encodeErr == nil {
					fmt.Fprintln(stdout, string(encoded))
					return 1
				}
			}
			fmt.Fprintf(stderr, "scrinium %s: %v\n", args[0], err)
			return 1
		}
		return 0
	}
}

type reportedCLIError struct{}

func (*reportedCLIError) Error() string { return "machine-readable error written" }

func requestedJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	return false
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
	return fmt.Sprintf(`# Scrinium Knowledge Workflow

Audience: %s.
Generated for agents: %s.

Scrinium is an evidence-backed project knowledge and write-governance service. Canonical claims and sources record provenance and bounded validation; stored Markdown is not automatically true.

## Required Loop

1. Start Scrinium MCP with command `+"`scrinium`"+` and args `+"`%s`"+`.
2. After any harness or plugin bootstrap instructions are loaded, call Scrinium `+"`capabilities`"+` before project work or wiki writes.
3. Call `+"`begin_session`"+` before project changes and retain its durable session ID.
4. Read `+"`index.md`"+` and `+"`agent-rules.md`"+` with `+"`read_wiki_page`"+`.
5. Read any relevant workflow pages before specialized wiki work.
6. Make project changes.
7. Use claim and source operations for canonical knowledge state; use deprecated page tools only for human documentation and compatibility.
8. Update `+"`log.md`"+`, `+"`index.md`"+`, and `+"`source-registry.md`"+` when Scrinium reports they are required.
9. Call `+"`session_status`"+` for that session.
10. Call `+"`finish_session`"+` for that session before reporting completion.

Do not report completion while `+"`finish_session`"+` fails. Satisfy its pending maintenance checklist first.

## Boundaries

Scrinium sessions are durable tracked work-session checklists. They record only operations Scrinium observes and are not authentication, a security boundary, or proof of agent compliance. External validation is scoped to its recorded binding and fingerprints; it does not establish global correctness.
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

func runMCPToolAsCLI(toolName string, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet(toolName, flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		fmt.Fprintf(stdout, "Usage: scrinium %s [--repo PATH] [other-flags]\n", toolName)
		fs.PrintDefaults()
	}

	var repo string
	fs.StringVar(&repo, "repo", ".", "repository root containing scrinium.json")

	// Declare flags for all known MCP parameters
	var sessionID, path, content, logFile, sourceID, claimID, title, rawPath, trustLevel, from, to, archivePath, reason, name string
	var inputPath, inputJSON string
	var machineJSON bool
	fs.StringVar(&sessionID, "session", "", "explicit durable session ID")
	fs.StringVar(&claimID, "claim-id", "", "immutable claim ID")
	fs.StringVar(&path, "path", "", "wiki page path")
	fs.StringVar(&content, "content", "", "content to write or append")
	fs.StringVar(&logFile, "log_file", "", "log file path")
	fs.StringVar(&sourceID, "source_id", "", "stable source ID (SRC-YYYYMMDD-slug)")
	fs.StringVar(&sourceID, "source-id", "", "immutable source ID")
	fs.StringVar(&title, "title", "", "source title")
	fs.StringVar(&rawPath, "raw_path", "", "original raw source path")
	fs.StringVar(&trustLevel, "trust_level", "", "trust level (trusted-project, trusted-owner, external, unknown)")
	fs.StringVar(&from, "from", "", "source page path")
	fs.StringVar(&to, "to", "", "destination page path")
	fs.StringVar(&archivePath, "archive_path", "", "optional archive path")
	fs.StringVar(&reason, "reason", "", "optional reason")
	fs.StringVar(&name, "name", "", "draft filename")
	fs.StringVar(&inputPath, "input", "", "strict versioned JSON input file for complex operations")
	fs.StringVar(&inputJSON, "input-json", "", "strict versioned inline JSON for complex operations")
	fs.BoolVar(&machineJSON, "json", false, "write exactly one versioned JSON document to stdout")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(fs.Args(), " "))
	}
	if directCLIRequiresSession(toolName) && strings.TrimSpace(sessionID) == "" {
		return fmt.Errorf("requires --session SESSION-ID")
	}

	// Build parameters map based on what flags were set
	params := make(map[string]any)
	visit := func(f *flag.Flag) {
		if f.Name == "repo" || f.Name == "session" || f.Name == "input" || f.Name == "input-json" || f.Name == "json" {
			return
		}
		key := f.Name
		if key == "claim-id" {
			key = "claim_id"
		} else if key == "source-id" {
			key = "source_id"
		}
		params[key] = f.Value.String()
	}
	fs.Visit(visit)
	if sessionID != "" {
		params["session_id"] = sessionID
	}
	input, err := loadCLIInput(toolName, inputPath, inputJSON)
	if err != nil {
		return err
	}
	if input != nil {
		params["input"] = input
	}

	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}

	configPath := filepath.Join(absRepo, "scrinium.json")
	app, err := NewApp(configPath)
	if err != nil {
		return err
	}

	// Construct the JSON-RPC raw message
	reqStruct := struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}{
		Name:      toolName,
		Arguments: params,
	}
	reqBytes, err := json.Marshal(reqStruct)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	res, err := app.handleToolCall(reqBytes)
	if err != nil {
		return err
	}

	// Print the result
	m, ok := res.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid response type from tool handler")
	}

	isError, _ := m["isError"].(bool)
	contentArr, _ := m["content"].([]map[string]any)
	if len(contentArr) == 0 {
		if isError {
			return fmt.Errorf("tool execution failed")
		}
		return nil
	}

	text, _ := contentArr[0]["text"].(string)
	if isError {
		if machineJSON && publicCLICommand(toolName) && json.Valid([]byte(text)) {
			fmt.Fprintln(stdout, text)
			return &reportedCLIError{}
		}
		return fmt.Errorf("%s", text)
	}
	if machineJSON {
		if !publicCLICommand(toolName) {
			return fmt.Errorf("--json is supported only by the public claim, source, validation, lint, session, and capabilities operations")
		}
		if err := publicapi.ValidateMachineDocument([]byte(text)); err != nil {
			return err
		}
		fmt.Fprintln(stdout, text)
		return nil
	}
	// session_status predates the v0.2 public surface and historically returned
	// pretty JSON. Preserve that human-mode compatibility; --json still emits
	// the stable versioned machine document without reformatting.
	if publicCLICommand(toolName) && toolName != "session_status" {
		summary, err := publicapi.HumanSummary([]byte(text))
		if err != nil {
			return fmt.Errorf("render public result: %w", err)
		}
		fmt.Fprintln(stdout, summary)
		return nil
	}

	// If the text is JSON, let's pretty-print it. Otherwise, print directly.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, []byte(text), "", "  "); err == nil {
		fmt.Fprintln(stdout, pretty.String())
	} else {
		fmt.Fprintln(stdout, text)
	}
	return nil
}

func publicCLICommand(name string) bool {
	return name == "capabilities" || name == "session_status" || strings.HasPrefix(name, "claim_") || strings.HasPrefix(name, "source_") || strings.HasPrefix(name, "session_")
}

func loadCLIInput(toolName, inputPath, inline string) (any, error) {
	expected, target := publicInputTarget(toolName)
	if expected == "" {
		if inputPath != "" || inline != "" {
			return nil, fmt.Errorf("%s does not accept --input or --input-json", toolName)
		}
		return nil, nil
	}
	if (inputPath == "") == (inline == "") {
		return nil, fmt.Errorf("%s requires exactly one of --input FILE or --input-json JSON", toolName)
	}
	var data []byte
	var err error
	if inputPath != "" {
		data, err = readCLIInputFile(inputPath)
	} else {
		data = []byte(inline)
	}
	if err != nil {
		return nil, err
	}
	if err := publicapi.DecodeJSON(data, expected, target); err != nil {
		return nil, err
	}
	return target, nil
}

func publicInputTarget(toolName string) (string, publicapi.VersionedInput) {
	switch toolName {
	case "claim_create":
		return publicapi.ClaimCreateSchema, &publicapi.ClaimCreateInput{}
	case "claim_update":
		return publicapi.ClaimUpdateSchema, &publicapi.ClaimUpdateInput{}
	case "claim_add_evidence":
		return publicapi.ClaimEvidenceSchema, &publicapi.ClaimEvidenceInput{}
	case "claim_set_validation_policy":
		return publicapi.ClaimPolicySchema, &publicapi.ClaimPolicyInput{}
	case "claim_validate":
		return publicapi.ClaimValidationSchema, &publicapi.ClaimValidationInput{}
	case "claim_supersede":
		return publicapi.ClaimSupersedeSchema, &publicapi.ClaimSupersedeInput{}
	case "claim_withdraw":
		return publicapi.ClaimWithdrawSchema, &publicapi.ClaimWithdrawInput{}
	case "source_register":
		return publicapi.SourceRegisterSchema, &publicapi.SourceRegisterInput{}
	case "source_refresh":
		return publicapi.SourceRefreshSchema, &publicapi.SourceRefreshInput{}
	default:
		return "", nil
	}
}

func readCLIInputFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect input file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("input must be a regular non-symlink file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, publicapi.MaxInputBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read input file: %w", err)
	}
	if len(data) > publicapi.MaxInputBytes {
		return nil, fmt.Errorf("input exceeds %d bytes", publicapi.MaxInputBytes)
	}
	return data, nil
}

func directCLIRequiresSession(toolName string) bool {
	switch toolName {
	case "continue_session", "session_status", "finish_session", "abandon_session",
		"session_continue", "session_finish", "session_abandon",
		"claim_create", "claim_update", "claim_add_evidence", "claim_set_validation_policy", "claim_validate", "claim_supersede", "claim_withdraw",
		"source_register", "source_refresh",
		"register_source", "create_page", "move_page", "archive_page",
		"update_wiki_page", "create_draft", "append_log", "apply_source_migration", "rebuild_source_registry":
		return true
	default:
		return false
	}
}
