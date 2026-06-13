# Project Design: LLM-Wiki MCP Server

## Project Name: Scrinium

## 1. Overview
This project is a Model Context Protocol (MCP) server designed to manage a local `llm-wiki` structure. It provides an interface for AI coding agents to Create, Read, Update, and Delete (CRUD) wiki files through code, serving as a structured memory layer to keep agents focused on project rules and reduce hallucinations.

scrinium is a CLI based not HTTP based MCP server.

## 2. Technical Stack
- **Language**: Go >= 1.26.4
- **Dependencies**: Standard library preferred (`os`, `path/filepath`, `io/fs`) for lightweight filesystem operations.
- **Interface**: Model Context Protocol (MCP) using standard JSON-RPC.

## 3. Core Architecture & Governance

The server does not just provide raw filesystem access; it acts as an agentic governance gateway to protect project context from decay or unintended overwrites.

### 3.1. Policy-Based Access Control (PBAC)
To prevent context drift and unauthorized modifications to foundational rules, the server implements strict, deterministic read/write governance at the protocol layer.
- **Immutable (Read-Only) Zones**: Foundational architectural rules, core standards, and framework constraints.
- **Mutable (Read-Write) Zones**: Sprint contexts, active drafts, agent-generated session logs, and working notes.

### 3.2. Configuration via `scrinium.json`
The server is configured per project using a `scrinium.json` file. This configures the wiki's root directory (relative to the config file location) and defines the access control constraints.

**Example Configuration Schema:**
```json
{
  "wiki_root": "./llm-wiki",
  "write_governance": {
    "protected_files": ["rules.md", "agent-rules.md", "architecture/*", "core-decisions/*"]
  }
}
```

### 3.3. Graceful Semantic Rejection
If an agent attempts an unauthorized write (e.g., trying to modify a read-only architecture file), the server will safely trap the operation. Instead of crashing or returning a generic HTTP/RPC error, it returns a **semantic error string** designed specifically for LLM consumption.
*Example Response:* `"Error: 'architecture/db_schema.md' is a read-only foundational document. You cannot alter project rules. Write your proposed architecture change to 'drafts/db_schema_proposal.md' instead."*

This creates an immediate, self-correcting feedback loop for the agent.

## 4. MCP Capabilities Exposed

### 4.1. Resources (Read)
- The server will expose the entire `llm-wiki` directory structure as standard MCP URIs (e.g., `llm-wiki://architecture/overview.md`).
- Read operations are deliberately permissive to maximize the agent's contextual awareness of the project.

### 4.2. Tools (Write/Execute)
Tools must be tightly scoped to prevent runaway changes.
- `read_wiki_page`: Standard read operation.
- `update_wiki_page`: Modifies a specific file (strictly checked against the `scrinium.json` write governance limits).
- `create_draft`: Stages a proposed change into a temporary or dedicated drafts folder, preventing direct commits to canonical pages.
- `append_log`: Appends text to a rolling log (e.g., an architectural decision record) without allowing the agent to alter the pre-existing historical text.

## 5. Development & Verification Workflow

The server is built with a zero-tolerance approach for failing checks, ensuring robust deployment.

### 5.1. Foundational Logic and Rule Compliance
As the foundational logic for every task, the rules located in `~/.gemini/GEMINI.md` must always be prioritized. Before starting any code change, developers or agents must explicitly check `docs/ARCHITECTURAL_GUIDELINES.md` and `.agent/rules/` to ensure full compliance with the core system requirements.

### 5.2. Makefile Targets
The `Makefile` must include the following targets to streamline the workflow:
- `build`: Compiles the MCP server binary.
- `test`: Runs unit and integration tests (must explicitly cover PBAC boundary enforcement and the semantic error formatting).
- `verify`: Runs linters (e.g., `staticcheck`, `govulncheck`), static analysis, checks module tidiness, and validates formatting.

### 5.3. Completion Definition
A development task is exclusively declared "done" when `make test` and `make verify` execute completely without any errors. If either target fails, the developer (or agent) must iteratively fix the issues and run the targets again until successful.