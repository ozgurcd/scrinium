# Decision: Add `capabilities` Tool for Agent Self-Orientation

**Date:** 2026-06-13  
**Status:** Accepted

## Context

Coding agents connecting to Scrinium via MCP need to understand what the server does, what tools are available, and what governance rules apply — without reading external docs. The MCP `initialize` and `tools/list` methods provide machine-level discovery (names and schemas), but they don't convey *purpose*, *governance semantics*, or *behavioral instructions*.

## Decision

Add a `capabilities` MCP tool that coding agents call **first** upon connecting. It returns an agent instruction payload containing:

- **Instruction text** — A direct system-prompt-style description of what Scrinium is and how the agent should behave (read before write, follow semantic errors, use create_draft for protected files).
- **Tool catalog with usage guidance** — Each tool with a `usage` field explaining *when* and *why* to use it, plus parameter descriptions.
- **Live governance state** — The actual protected file patterns and allowed tools read from the running config, so the agent always sees the truth.

## Rationale

- `tools/list` tells agents *what exists*. `capabilities` tells agents *what to do and why*.
- Returns the live governance config — never stale.
- Creates an immediate self-correcting feedback loop: agents that call `capabilities` first will respect governance without trial-and-error.
- The response is structured JSON, not prose — agents can parse and reason over it programmatically.

## Implementation

- Tool name: `capabilities`
- No input parameters required.
- Registered in `handleToolsList` and dispatched via `handleToolCall`.
- Handler: `handleCapabilities()` in `cmd/scrinium/app.go`.
