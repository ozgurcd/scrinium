# LLM Wiki Log

This is the canonical chronological log for the project LLM Wiki. Keep entries append-only and parseable.

## Format

Use this heading pattern for every event:

```markdown
## [YYYY-MM-DD] <event-type> | <short title>
```

Event types include `session`, `ingest`, `query`, `lint`, `decision`, and `maintenance`.

## Entries

## [2026-06-13] maintenance | LLM Wiki structure aligned with raw-wiki-schema pattern

- Objective: Add the missing structural pieces needed for the repository to follow the LLM Wiki pattern.
- Outcome: Added immutable `raw/` source layer, source registry, source summaries area, ingest/query/lint workflows, page schemas, untrusted-source handling, prompt templates, and index updates.
- Files touched: `AGENTS.md`, `raw/README.md`, `raw/inbox/.gitkeep`, `raw/assets/.gitkeep`, `llm-wiki/index.md`, `llm-wiki/prompt-templates.md`, `llm-wiki/workflows/ingest.md`, `llm-wiki/workflows/query.md`, `llm-wiki/workflows/lint.md`, `llm-wiki/schemas/page-schemas.md`, `llm-wiki/security/untrusted-sources.md`, `llm-wiki/source-registry.md`, `llm-wiki/sources/README.md`, `llm-wiki/drafts/agent-rules-llm-wiki-operating-model-2026-06-13.md`.
- Validation: Documentation whitespace check passed for changed scope. No Go code changed.

## [2026-06-13] maintenance | Canonical log.md added

- Objective: Make `llm-wiki/log.md` the canonical chronological log described by the LLM Wiki pattern.
- Outcome: Created this canonical log and updated guidance to prefer `log.md` over per-session files under `logs/`.
- Validation: Pending final structure check.
- Validation update: Confirmed `llm-wiki/log.md` exists, guidance points ingest/query/lint/session templates to `log.md`, and trailing-whitespace check passed for the changed guidance scope. Legacy `logs/session-2026-06-13.md` remains only for continuity.

## [2026-06-13] ingest | Project Design: LLM-Wiki MCP Server

- Source ID: `SRC-20260613-project-design`
- Raw path: `raw/inbox/PROJECT_DESIGN.md`
- Source summary: `sources/SRC-20260613-project-design.md`
- Pages touched: `source-registry.md`, `sources/SRC-20260613-project-design.md`, `projects/scrinium.md`, `concepts/policy-based-access-control.md`, `concepts/semantic-rejection.md`, `index.md`, `log.md`
- Key claims: Scrinium is a CLI-based Go MCP server using JSON-RPC over stdio; governance is configured through `scrinium.json`; protected writes should produce semantic, LLM-readable rejection messages; code completion requires `make test` and `make verify`.
- Contradictions or uncertainty: Source references `~/.gemini/GEMINI.md`, `docs/ARCHITECTURAL_GUIDELINES.md`, and `.agent/rules/`, but active project guidance is `AGENTS.md` plus governed `llm-wiki` pages. Source also says CRUD, while current tool surface does not expose unrestricted delete semantics.
- Security notes: Source was treated as untrusted evidence. Imperative source text was not treated as active instruction.


## [2026-06-13] lint | First post-ingest wiki lint

- Pages checked: all markdown pages under `llm-wiki/`.
- Findings: medium 1, low 2.
- Fixes made: added missing `index.md` entries for `scrinium-guide.md` and `drafts/agent-rules-llm-wiki-operating-model-2026-06-13.md`; clarified non-source references in `projects/scrinium.md` and `concepts/semantic-rejection.md`.
- Drafts created: none.
- Follow-ups: protected `agent-rules.md` still needs owner acceptance of `drafts/agent-rules-llm-wiki-operating-model-2026-06-13.md` if the protected page should include the LLM Wiki operating model directly.


## [2026-06-13] query | LLM Wiki directory compliance

- Pages read: `AGENTS.md`, `index.md`, `agent-rules.md`, `log.md`, `drafts/agent-rules-llm-wiki-operating-model-2026-06-13.md`, Karpathy LLM Wiki gist.
- Answer filed at: `syntheses/llm-wiki-structure-compliance.md`
- New synthesis: The gist specifies conceptual layers plus `index.md` and `log.md`, but does not mandate or forbid most wiki subdirectories. Current extra directories are local schema/governance choices; `logs/` is legacy because `log.md` is canonical.
- Open questions: none.


## [2026-06-13] maintenance | Remove legacy logs directory

- Objective: Remove wiki files/directories that are unnecessary for the LLM Wiki pattern after `log.md` became canonical.
- Pages touched: `index.md`, `syntheses/llm-wiki-structure-compliance.md`, `log.md`.
- Files removed: `logs/.gitkeep`, `logs/session-2026-06-13.md`.
- Outcome: `log.md` is now the only chronological LLM Wiki log path.
- Validation: Pending structure check.
- Follow-ups: none.

- Validation update: Removed empty `llm-wiki/logs/` directory and updated `sources/SRC-20260613-project-design.md` to reflect that `log.md` is the only chronological log path.
- Correction: Earlier log text said `logs/session-2026-06-13.md` remained for continuity. That is now superseded by the cleanup above; `llm-wiki/logs/` has been removed and `log.md` is the only chronological log path.

## [2026-06-13] maintenance | Add setup_llm_wiki tool and update governance

- Objective: Make `agent-rules.md` writable, expose a setup tool for projects without an LLM Wiki, and update tool metadata.
- Pages touched: `agent-rules.md`, `index.md`, `scrinium-guide.md`, `projects/scrinium.md`, `concepts/policy-based-access-control.md`, `log.md`.
- Code touched: `scrinium.json`, `cmd/scrinium/app.go`, `cmd/scrinium/app_test.go`.
- Outcome: `agent-rules.md` is removed from protected files; `setup_llm_wiki` is listed in MCP tools and capabilities; setup creates the standard LLM Wiki skeleton without overwriting existing pages; obsolete agent-rules draft was removed.
- Validation: `go test ./... -count=1 -timeout=120s`, `gograph build . --precise`, and `make verify` passed. `gograph review --uncommitted` reported no modified symbols in the graph.
- Follow-ups: none.

- Correction: The earlier lint follow-up about accepting `drafts/agent-rules-llm-wiki-operating-model-2026-06-13.md` is superseded. `agent-rules.md` is now writable in the active config, the operating-model text was merged directly, and the draft file was removed.
- Validation update: `make verify` passed after the setup tool and governance changes. Output included `go build`, `go test ./... -count=1 -timeout=120s`, `go vet ./...`, `staticcheck ./...`, and `govulncheck ./...` with no vulnerabilities found.
- Final validation update: `make verify` passed again after README cleanup. Output included build, tests, vet, staticcheck, and govulncheck with no vulnerabilities found.

## [2026-06-14] maintenance | Enforce LLM Wiki session loop

- Objective: Add stronger assurance that agents read wiki context before writes and update the wiki after project changes.
- Pages touched: `agent-rules.md`, `index.md`, `scrinium-guide.md`, `projects/scrinium.md`, `log.md`.
- Code touched: `cmd/scrinium/app.go`, `cmd/scrinium/app_test.go`, `README.md`.
- Outcome: Added `begin_session`, `session_status`, and `finish_session`; wiki writes now require active sessions and startup reads; session completion requires `log.md`, `index.md`, and `source-registry.md` maintenance when applicable.
- Validation: `go test ./... -count=1 -timeout=120s`, `gograph build . --precise`, and `make verify` passed. `gograph review --uncommitted` reported no modified symbols in the graph.

## [2026-06-14] maintenance | Document Scrinium init and wiki adoption

- Objective: Explain how to start Scrinium and maintain `llm-wiki/` for both brand new repositories and repositories with existing manual or non-Scrinium wiki docs.
- Pages touched: `index.md`, `log.md`.
- Files touched: `README.md`, `docs/scrinium-init-and-maintenance.md`.
- Outcome: Added an operator guide covering required files, `setup_llm_wiki`, greenfield initialization, existing-wiki adoption, lint/adoption checks, and the ongoing enforced session loop.
- Validation: Confirmed the new guide exists, README links to it, the wiki index references it, and the guide includes both requested adoption paths plus the ongoing session loop.

## [2026-06-14] maintenance | Add real-world adoption tools

- Objective: Add Scrinium tools needed for real-world LLM Wiki adoption and maintenance.
- Pages touched: `index.md`, `scrinium-guide.md`, `projects/scrinium.md`, `log.md`.
- Files touched: `cmd/scrinium/app.go`, `cmd/scrinium/app_test.go`, `README.md`, `docs/scrinium-init-and-maintenance.md`.
- Outcome: Added `lint_llm_wiki`, `adopt_llm_wiki`, `register_source`, `create_page`, `move_page`, and `archive_page`; `capabilities` now explains each tool to coding agents; `archive_page` tells agents to treat archived content as historical only and remove it from active working context.
- Validation: `go test ./... -count=1 -timeout=120s`, `make verify`, `gograph build . --precise`, and `gograph review --uncommitted` passed. `govulncheck` reported no vulnerabilities.

## [2026-06-14] maintenance | Harden install target

- Objective: Make `make install` suitable for installing the Scrinium binary under `/usr/local/bin` while remaining package/test friendly.
- Pages touched: `log.md`.
- Files touched: `Makefile`, `README.md`.
- Outcome: `make install` still defaults to `/usr/local/bin/scrinium`, now creates the target bin directory and supports `PREFIX`, `BINDIR`, and `DESTDIR` overrides. README documents installed usage with `scrinium ./scrinium.json`.
- Validation: `make -n install`, `GOCACHE=/private/tmp/scrinium-gocache make install DESTDIR=/private/tmp/scrinium-install-smoke`, executable check for `/private/tmp/scrinium-install-smoke/usr/local/bin/scrinium`, `make verify`, `gograph build . --precise`, and `gograph review --uncommitted` passed.

## [2026-06-14] maintenance | Add manual agent enforcement CLI

- Objective: Add a non-MCP command that humans can run to create or refresh agent instruction files for Codex, Claude Code, OpenCode, and Antigravity-compatible workflows.
- Pages touched: `projects/scrinium.md`, `log.md`.
- Files touched: `main.go`, `cmd/scrinium/cli.go`, `cmd/scrinium/app_test.go`, `README.md`, `docs/scrinium-init-and-maintenance.md`.
- Outcome: `scrinium enforce-agents` runs as a normal CLI subcommand instead of starting stdio MCP mode. It updates Scrinium-managed blocks in `AGENTS.md`, `CLAUDE.md`, and `docs/scrinium-agent-enforcement.md`, supports `--repo`, `--agents`, `--dry-run`, and `--check`, and preserves user content outside managed blocks.
- Validation: Focused `go test ./cmd/scrinium -run 'TestRunCLI|TestRunCLIPreserves' -count=1 -timeout=120s`, full `go test ./... -count=1 -timeout=120s`, `make verify`, `gograph build . --precise`, `gograph review --uncommitted`, `./scrinium enforce-agents --help`, and a temp-repo `./scrinium enforce-agents --dry-run`/write/`--check` smoke test passed.

## [2026-06-14] maintenance | Make agent enforcement compatible with bootstraps

- Objective: Avoid instruction-order conflicts with harness/plugin bootstraps such as Superpowers.
- Pages touched: `log.md`.
- Files touched: `cmd/scrinium/cli.go`, `cmd/scrinium/app_test.go`, `README.md`, `docs/scrinium-init-and-maintenance.md`.
- Outcome: Generated enforcement now tells agents to load harness/plugin bootstrap instructions first, then call Scrinium `capabilities` before project work or wiki writes.
- Validation: Focused `go test ./cmd/scrinium -run 'TestRunCLI|TestRunCLIPreserves' -count=1 -timeout=120s`, `make verify`, fresh-binary temp-repo `./scrinium enforce-agents` smoke check, `gograph build . --precise`, and `gograph review --uncommitted` passed.

## [2026-06-14] maintenance | Add SemVer build versioning

- Objective: Track Scrinium releases with SemVer, embed the version at compile time, and expose it to humans and MCP clients.
- Pages touched: `projects/scrinium.md`, `log.md`.
- Files touched: `Makefile`, `.bumpversion.cfg`, `cmd/scrinium/cli.go`, `cmd/scrinium/app.go`, `cmd/scrinium/app_test.go`, `README.md`.
- Outcome: `make build` injects `VERSION` with Go ldflags; `scrinium version`, MCP initialize metadata, and `capabilities` report the embedded version. `.bumpversion.cfg` tracks SemVer and updates `Makefile` without automatic commits or tags.
- Validation: Focused version tests, `make version`, `make verify`, `./scrinium version`, JSON-RPC `capabilities` smoke check, `gograph build . --precise`, and `gograph review --uncommitted` passed.

## [2026-06-14] maintenance | Fix GoReleaser deprecations and re-run upload failure

- Objective: Fix three issues from the GitHub Actions CI failure: deprecated `archives.format`, deprecated `brews`, and 422 asset-already-exists errors on re-triggered runs.
- Pages touched: `log.md`.
- Files touched: `.goreleaser.yaml`.
- Outcome: Replaced `archives.format` with `archives.formats: [tar.gz]`; replaced `brews` with `homebrew_casks` (directory `Formula` → `Casks`, `install` block → `binaries` list, removed `license` field not in cask schema); added `release.replace_existing_artifacts: true` so re-runs overwrite existing assets instead of failing with 422.
- Validation: YAML syntax validated with `ruby -e "require 'yaml'; YAML.load_file(...)"`. GoReleaser not installed locally; config will be validated by CI on next release.
- Follow-ups: Old `Formula/scrinium.rb` in the tap is now orphaned — can be deleted manually from `ozgurcd/homebrew-tap` after the next successful release.
