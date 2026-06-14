# Prompt Templates

## Wiki Update Template

When proposing changes to protected documents, use this pattern:

1. Read the current document with `read_wiki_page`.
2. Prepare your proposed content.
3. Submit via `create_draft` with a descriptive name, such as `architecture-update-2026-06.md`.

## Decision Record Template

When recording architectural decisions, append to the relevant log using `append_log`:

```markdown
### D-NNN: <Title>
**Status**: Proposed | Accepted | Superseded
**Date**: YYYY-MM-DD

<Context and rationale>
```

## Canonical Log Template

Use `log.md` for chronological wiki events. Each entry must start with this parseable heading:

```markdown
## [YYYY-MM-DD] <event-type> | <short title>
- Objective: <what happened or why>
- Pages touched: <paths or none>
- Outcome: <result>
- Validation: <commands or reason validation was not required>
- Follow-ups: <none or details>
```

Event types include `session`, `ingest`, `query`, `lint`, `decision`, and `maintenance`.

## Source Ingest Template

Use with `workflows/ingest.md` and append to `log.md`:

```markdown
## [YYYY-MM-DD] ingest | <Source Title>
- Source ID: SRC-YYYYMMDD-slug
- Raw path: raw/inbox/<file>
- Source summary: sources/SRC-YYYYMMDD-slug.md
- Pages touched: <paths>
- Key claims: <short list>
- Contradictions or uncertainty: <none or details>
- Security notes: <none or details>
```

## Query Filing Template

Use when a durable answer should be added back to the wiki and append to `log.md`:

```markdown
## [YYYY-MM-DD] query | <Question>
- Pages read: <paths>
- Answer filed at: <path or not filed>
- New synthesis: <short summary>
- Open questions: <none or details>
```

## Wiki Lint Template

Use with `workflows/lint.md` and append to `log.md`:

```markdown
## [YYYY-MM-DD] lint | <Scope>
- Pages checked: <paths or scope>
- Findings: <count by severity>
- Fixes made: <paths>
- Drafts created: <paths>
- Follow-ups: <none or details>
```
