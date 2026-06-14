# Architecture Overview

## System Design
Scrinium is a single-package MCP server (`cmd/scrinium`) with a thin `main.go` entry point. The core type is `App`, which holds the configuration and wiki root path.

## Components
- **App** (`cmd/scrinium/app.go`) — MCP server, JSON-RPC handler, tool dispatch, and PBAC enforcement.
- **Config** — Loaded from `scrinium.json`: wiki root path and write governance rules.
- **main.go** — CLI entry point: parses args, creates `App`, calls `Run(ctx)`. Expects `scrinium.json` as argument.

## Technology Stack
- Language: Go 1.26+
- Transport: JSON-RPC over stdio (stdin/stdout)
- Storage: Local filesystem via `os` and `path/filepath`
- Dependencies: Go standard library only

## MCP Interface
- `resources/list` — Walk the wiki directory, return file URIs.
- `tools/call` — Dispatch to: `read_wiki_page`, `update_wiki_page`, `create_draft`, `append_log`.

## Security
- Path traversal prevention via `safePath()` (resolves and validates paths stay within wiki root).
- Graceful shutdown on SIGINT/SIGTERM via `signal.NotifyContext`.
- Log output directed to stderr to prevent JSON-RPC channel corruption.
