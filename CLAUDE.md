# Claude Code Instructions

<!-- BEGIN SCRINIUM ENFORCEMENT -->
# Scrinium Knowledge Workflow

Audience: Claude Code.
Generated for agents: antigravity, claudecode, codex, opencode.

Scrinium is an evidence-backed project knowledge and write-governance service. Canonical claims and sources record provenance and bounded validation; stored Markdown is not automatically true.

## Required Loop

1. Start Scrinium MCP with command `scrinium` and args `/Users/odemir/Development/scrinium/scrinium.json`.
2. After any harness or plugin bootstrap instructions are loaded, call Scrinium `capabilities` before project work or wiki writes.
3. Call `begin_session` before project changes and retain its durable session ID.
4. Read `index.md` and `agent-rules.md` with `read_wiki_page`.
5. Read any relevant workflow pages before specialized wiki work.
6. Make project changes.
7. Use claim and source operations for canonical knowledge state; use deprecated page tools only for human documentation and compatibility.
8. Update `log.md`, `index.md`, and `source-registry.md` when Scrinium reports they are required.
9. Call `session_status` for that session.
10. Call `finish_session` for that session before reporting completion.

Do not report completion while `finish_session` fails. Satisfy its pending maintenance checklist first.

## Boundaries

Scrinium sessions are durable tracked work-session checklists. They record only operations Scrinium observes and are not authentication, a security boundary, or proof of agent compliance. External validation is scoped to its recorded binding and fingerprints; it does not establish global correctness.
<!-- END SCRINIUM ENFORCEMENT -->
