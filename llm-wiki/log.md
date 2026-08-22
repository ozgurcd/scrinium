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

## [2026-06-14] maintenance | Document ingestion workflow in README

- Objective: Address missing documentation on how document ingestion works under Scrinium.
- Pages touched: `log.md`.
- Files touched: `README.md`.
- Outcome: Added Section "4. Document Ingestion Prompt" to `README.md` providing a copy-pasteable prompt template for instructing coding agents to perform the end-to-end ingestion flow.
- Validation: `make verify` passed successfully.

## [2026-06-16] maintenance | Map MCP tools to CLI subcommands

- Objective: Support running MCP tools directly from the CLI to unify tool execution and improve ease of manual testing/checking.
- Pages touched: `log.md`.
- Files touched: `cmd/scrinium/cli.go`, `cmd/scrinium/app_test.go`.
- Outcome: Updated `IsCLISubcommand` and `RunCLI` to match all known MCP tool names. Implemented `runMCPToolAsCLI` which registers matching flags for all MCP parameters (e.g. `--path`, `--content`, `--log_file`, etc.), initializes the App relative to `--repo` (default `.` for `./scrinium.json`), dispatches the call through `App.handleToolCall()`, and formats the outputs (with pretty-printed JSON if applicable). Cleaned up unused `stderr` parameter in `runMCPToolAsCLI`.
- Validation: Added `TestRunCLIMCPTools` verifying capabilities, read_wiki_page, and missing config errors; `make verify` passed successfully.



## [2026-08-20] maintenance | Align platform startup governance

- Objective: Make repository startup instructions consistently reference the existing open-source platform document.
- Pages touched: `AGENTS.md`, `docs/v0.2-evidence-architecture.md`, `llm-wiki/log.md`.
- Outcome: Startup governance and the architecture assessment now point to `llm-wiki/platform/open-source.md`; `llm-wiki/index.md` was already consistent and required no change.
- Validation: Pending repository reference scan, Gograph checks, `make verify`, and `git diff --check`.
- Follow-ups: none.
- Validation update: Repository-wide ignored-file-inclusive scan found no remaining stale platform terminology; `gograph build . --precise`, `gograph review --uncommitted`, `make verify`, and `git diff --check` passed. No Go symbols changed and no vulnerabilities were found.
## [2026-08-20] maintenance | Complete v0.2 Phase 1 architecture boundaries
- Objective: Refactor Scrinium transport, application workflows, filesystem confinement, write governance, in-memory session bookkeeping, and compatibility lint into explicit packages without beginning Claim/Evidence implementation.
- Pages touched: projects/scrinium.md, log.md
- Outcome: cmd/scrinium/app.go is a thin composition and compatibility façade; internal/mcp adapts JSON-RPC to typed internal/app workflows; internal/store, internal/governance, internal/session, and internal/lint own their existing responsibilities. Session-dependent direct CLI workflows now fail explicitly because sessions remain process-local. CI runs make verify on pull requests and pushes to main. The architecture document records the fixed owner decisions for semantic immutable claim IDs, observation-only manual verification, per-source JSON metadata, v0.2 compatibility interfaces, and complete validation history.
- Validation: gograph precise build and review completed; make verify passed; git diff --check passed.
- Follow-ups: Claim, Evidence, Validation, durable sessions, per-source JSON records, Gograph adapters, and Rulefloor adapters remain out of scope until separately authorized.

## [2026-08-20] maintenance | Add v0.2 Phase 2 evidence-backed knowledge core

- Objective: Introduce canonical Claim, Evidence, ValidationBinding, and ValidationResult records without adding validator integrations, durable sessions, or per-source JSON.
- Pages touched: `projects/scrinium.md`, `index.md`, `drafts/architecture-overview-v0.2-phase2-2026-08-20.md`, `log.md`.
- Outcome: Added the transport-independent `internal/knowledge` model; strict deterministic one-file-per-claim JSON persistence under `llm-wiki/claims/`; typed application claim operations; lifecycle-independent derived assessment and freshness; separate deterministic claim lint; and a tagged-only, write-free migration assessment. Existing MCP/CLI page compatibility behavior remains unchanged, and no Gograph or Rulefloor adapter was added.
- Trust behavior: `cannot_evaluate` never counts as pass; a later `cannot_evaluate` removes current verified presentation; manual validation is observation-grade only; context evidence does not raise assessment; authorship is provenance rather than self-supporting evidence; shared evidence lineage is not counted as independent corroboration.
- Validation: `make verify` passed build, uncached tests, vet, staticcheck, and govulncheck with no vulnerabilities; `git diff --check` passed; Gograph precise build indexed 36 Go files across 9 packages, configured checks passed with zero warnings, and package-boundary enforcement was unavailable because no `.gograph/boundaries.json` exists.
- Follow-ups: Define and authenticate the generic validator execution boundary, decide the public claim MCP/CLI surface, decide cross-process claim concurrency control, then add separately authorized validator adapters. Durable sessions and per-source JSON remain deferred.


## [2026-08-20] maintenance | Add v0.2 Phase 3 generic validation orchestration

- Objective: Add a generic validator execution boundary, deterministic registry, repository-bound input fingerprints, result authenticity checks, and application-owned result persistence without adding external adapters.
- Pages touched: `projects/scrinium.md`, `index.md`, `drafts/architecture-overview-v0.2-phase3-2026-08-20.md`, `log.md`.
- Outcome: Added `internal/validation` descriptors, registry, isolated requests, result validation, scoped repository snapshots, and the observation-grade manual validator. Typed application operations validate one binding or all required bindings and inspect available descriptors. Validators return results only; the application persists complete history and derives state. Missing validators, unsupported schemas, repository uncertainty, cancellation, timeouts, execution errors, and unauthentic results become structured `cannot_evaluate` records. Deterministic claim lint now checks validator/binding identity, schema support, assurance ceilings, input/snapshot fingerprints, stale scoped repository state, and missing required results.
- Trust behavior: Validator assurance cannot exceed its descriptor; manual validation cannot exceed observation grade; `cannot_evaluate` never counts as pass; previous passes lose current verified presentation when revalidation becomes unevaluable; no validator-provided assessment or freshness is accepted.
- Validation: `make verify` passed build, uncached tests, vet, staticcheck, and govulncheck with no vulnerabilities. `git diff --check` passed. Final Gograph precise build indexed 43 Go files across 10 packages with 574 symbols and 3,142 calls; review and configured checks passed with zero errors or warnings. Package-boundary enforcement remains unavailable because no `.gograph/boundaries.json` exists.
- Follow-ups: External adapter discovery/configuration and conformance fixtures, Gograph and Rulefloor adapters, public validation MCP/CLI surface, cross-process claim concurrency, durable sessions, and per-source JSON remain separately authorized work.


## [2026-08-20] maintenance | Block Phase 4 on missing Rulefloor machine contract

- Objective: Inspect Rulefloor's public interface before implementing the first external validator adapter.
- Pages touched: `projects/scrinium.md`, `log.md`.
- Outcome: Stopped before implementation. Installed Rulefloor v0.2.0 and current upstream main expose human-readable `list`, `show`, and aggregate `check` output, but no versioned JSON output and no command that evaluates one rule ID. `check --report` is a Playwright JSON input, not Rulefloor machine output. Exit codes and prose cannot reliably identify target-rule existence, armed/proof status, static versus executed evaluation, per-rule outcome, fingerprints, or structured cannot-evaluate reasons. No Scrinium code, configuration, or tests were added, and the Rulefloor repository was not modified.
- Required external change: Rulefloor must publish a strict versioned JSON command for one rule that reports tool version, repository context, requested/effective mode and profile, existence/armed/proof state, whether the bound test actually executed, pass/fail/cannot-evaluate outcome, concise diagnostic codes, and ledger/test/proof fingerprints.
- Validation: Pending no-code `make verify`, final Gograph check, and `git diff --check`.
- Follow-ups: Resume the adapter only after the Rulefloor machine contract is released. Gograph integration, durable sessions, and per-source JSON remain deferred.

- Validation update: `make verify` passed build, uncached tests, vet, staticcheck, and govulncheck with no vulnerabilities. Final Gograph precise build indexed 43 Go files across 10 packages with 574 symbols and 3,142 calls; review and configured checks passed with zero errors or warnings, with package boundaries skipped because `.gograph/boundaries.json` is absent. `git diff --check` passed.
## [2026-08-20] maintenance | Add v0.2 Rulefloor validator adapter

- Objective: Integrate Rulefloor through Scrinium's generic validator boundary without importing Rulefloor packages or adding a Rulefloor-specific public command.
- Code touched: `internal/integrations/rulefloor/validator.go`, generic validation-result metadata/diagnostics and adapter-owned binding validation seams, application configuration/composition, deterministic lint, and associated tests.
- Configuration: `validators.rulefloor.executable` defaults to `rulefloor`; missing or invalid Rulefloor discovery does not block startup.
- Guarantee: Static pass records selected-rule ledger/test binding integrity at observation grade. Execute pass reaches verification grade only when selected-rule execution is requested, performed, passing, repository-bound, fingerprint-bound, and structurally authentic. Neither establishes unrelated project correctness.
- Failure behavior: Missing tools, unsupported execution, malformed/inconsistent JSON, outcome/exit mismatch, cancellation, timeout, repository mismatch, and fingerprint mismatch produce `cannot_evaluate`, never infrastructure-derived `fail`.
- Documentation touched: `docs/v0.2-evidence-architecture.md` and `projects/scrinium.md`.
- Validation: `make verify` passed build, uncached tests, vet, staticcheck, and govulncheck with no vulnerabilities. `git diff --check` passed. Final Gograph precise build indexed 45 Go files across 11 packages with 675 symbols and 3,625 calls; configured checks passed with zero errors or warnings, and the process-execution flow audit found no unsanitized claim-controlled path. Package boundaries were skipped because `.gograph/boundaries.json` does not exist.
- Follow-ups: Gograph adapter, durable sessions, per-source JSON, public claim-validation transport, and cross-process claim locking remain separately authorized work.

- Conformance update: A read-only probe of the released Rulefloor JSON contract confirmed that missing and unarmed static rules use `static_integrity: not_performed` with overall `cannot_evaluate`. The adapter and hermetic fixtures now accept that exact contract while preserving the Rulefloor reason code; locally invalid rule IDs are rejected before process execution. `make verify`, final Gograph precise/check/flow gates, and `git diff --check` passed again after this alignment.

## [2026-08-20] maintenance | Implement Gograph validation adapter
- Objective: Add the optional subprocess-only Gograph structural validator through the generic validation boundary.
- Pages touched: projects/scrinium.md, ../docs/v0.2-evidence-architecture.md, log.md
- Outcome: Added strict gograph.binding.v1 translation for symbol_exists, package_imports, call_edge_exists, and type_implements; authenticated gograph.version.v1 and gograph.validation.v1 results; capped all Gograph outcomes at observation assurance; retained concise fingerprints and structural evidence; and degraded unavailable, stale, incomplete, or inconsistent evaluation to cannot_evaluate without rebuilding graphs.
- Validation: make verify; gograph build . --precise; gograph review and impact inspection; git diff --check.
- Follow-ups: durable sessions, per-source JSON, and any future public generic validation transport remain separately deferred.
## [2026-08-20] maintenance | Align Gograph follow-up wording
- Objective: Remove a stale phase-specific label from the current Scrinium project page after the Gograph adapter implementation.
- Pages touched: projects/scrinium.md, log.md
- Outcome: The remaining protected architecture-draft follow-up now refers to all implemented external-validator phases.
- Validation: project-page consistency read and git diff --check.
- Follow-ups: owner acceptance of an updated protected architecture overview remains pending.
## [2026-08-20] maintenance | Add cross-process claim compare-and-swap

- Objective: Prevent concurrent Scrinium processes from silently overwriting claim mutations or validation history.
- Pages touched: `projects/scrinium.md`, `../docs/v0.2-evidence-architecture.md`, `log.md`.
- Outcome: Claim reads now return exact-byte SHA-256 revisions. All canonical mutations require the observed revision and execute under a per-claim repository-local advisory lock; stale writers receive typed conflicts and are never merged. Validator execution remains outside locks, and results are rejected if the claim changes before persistence. Runtime lock artifacts remain outside canonical claim JSON and wiki listing.
- Validation: `make verify` passed build, uncached tests, vet, staticcheck, and govulncheck with no vulnerabilities; post-change Gograph precise review and `git diff --check` are the final handoff gates.
- Follow-ups: Durable sessions, per-source JSON, public generic claim-validation transport, and background validation scheduling remain separately deferred.
## [2026-08-21] session | Durable cross-process work sessions
- Objective: Replace process-local session bookkeeping with strict repository-local durable tracked work-session checklists.
- Pages touched: projects/scrinium.md, index.md, ../docs/v0.2-evidence-architecture.md, ../docs/scrinium-init-and-maintenance.md
- Outcome: Added opaque explicit session IDs, active/finished/abandoned lifecycle, strict ignored JSON records, per-session cross-process locks, CLI --session workflows, MCP connection context/continue behavior, and observed-only maintenance tracking. No per-source JSON, public claim-validation API, or background scheduler was added.
- Validation: gograph build . --precise (52 files, 12 packages, 911 symbols, 5322 calls); gograph review --uncommitted; gograph check --uncommitted (0 errors, 4 existing complexity warnings, boundary check skipped because no boundaries configuration exists); make verify (passed); git diff --check (passed).
- Follow-ups: Per-source JSON migration and the final public claim-validation API remain deferred. Automatic stale-session cleanup and background scheduling remain out of scope.


## [2026-08-21] architecture | Canonical deterministic source provenance

Implemented v0.2 canonical source metadata as one strict `scrinium.source/v1` JSON record per immutable source ID under `sources/records/`. Source registration is canonical-first, verifies confined regular raw files, records full SHA-256 byte fingerprints, and rebuilds `source-registry.md` as a deterministic compatibility view. Markdown source summaries remain human derivatives.

Added exact-byte source revisions, per-source cross-process locking and compare-and-swap, explicit fingerprint refresh, simple current/superseded/withdrawn lifecycle, deterministic source lint, canonical source resolution for claim evidence and freshness, and session tracking for source writes.

Added read-only legacy migration assessment and explicit idempotent apply. Migrated `SRC-20260613-project-design` without changing its raw bytes or Markdown summary; the post-migration assessment reports no debt.

Updated `agent-rules.md`, `workflows/ingest.md`, `schemas/page-schemas.md`, `sources/README.md`, `scrinium-guide.md`, `projects/scrinium.md`, `index.md`, project documentation, and source compatibility guidance. Source provenance and fingerprints identify origin and exact bytes; they do not prove semantic correctness or claim truth.

Verification: `GOTOOLCHAIN=go1.26.4 make verify` passed, `git diff --check` passed, Gograph precise build completed, and Gograph check reported no errors with only pre-existing complexity warnings.


Maintenance note: the deterministic registry rebuild is now considered an observed maintenance action even when compare-before-write finds identical bytes. A regression test covers the no-op rebuild clearing the session obligation. Final `GOTOOLCHAIN=go1.26.4 make verify` and `git diff --check` passed.

## [2026-08-21] maintenance | Complete v0.2 public knowledge interface
- Objective: Expose canonical claims, evidence, validation, sources, and durable sessions through small generic MCP/CLI operations.
- Pages touched: agent-rules.md, workflows/query.md, workflows/lint.md, projects/scrinium.md, index.md, drafts/architecture-overview-v0.2-final-2026-08-21.md, log.md
- Outcome: Public schemas and trust/error presentation documented; protected architecture overview proposed through the governed draft workflow.
- Follow-ups: Owner acceptance of the protected architecture draft; v0.3 scheduling, cleanup, and history-retention policy remain deferred.
- Maintenance confirmation: indexed the governed final v0.2 architecture draft after its creation.
- Verification follow-up: marked remaining v0.1 Markdown tools explicitly deprecated in MCP discovery; final make verify and Gograph review passed.
## [2026-08-21] maintenance | Require Go 1.27 for Scrinium v0.2
- Objective: Make Go 1.27 the supported development, CI, verification, and release baseline.
- Pages touched: projects/scrinium.md, index.md, drafts/go-1.27-protected-governance-2026-08-21.md, log.md
- Outcome: go.mod now requires Go 1.27.0; CI/release and analysis-tool versions are Go 1.27 compatible; protected legacy references have a governed update draft.
- Validation: go version reported go1.27.0; go mod tidy, make verify, GoReleaser 2.17.1 config check, Go-1.27-built Gograph precise review/check, and git diff --check.
- Follow-ups: Owner acceptance is required to apply the protected rules and architecture wording draft.

## [2026-08-21] maintenance | Verify Go 1.27 supported baseline
- Objective: Confirm Scrinium v0.2 development, CI, analysis, and release tooling under Go 1.27.
- Pages touched: log.md
- Outcome: Go 1.27.0 is the declared and verified baseline; the public MCP workflow fixture now keeps fake external validators hermetic when Gograph is installed.
- Validation: go version; go mod tidy; focused MCP test repeated 10 times; make verify; GoReleaser 2.17.1 configuration check; Gograph precise build, review, and check; git diff --check pending final run.
- Follow-ups: Owner acceptance remains required for drafts/go-1.27-protected-governance-2026-08-21.md.
## [2026-08-21] maintenance | Release Scrinium v0.2.0
- Objective: Publish the evidence-backed v0.2 core and Go 1.27 baseline.
- Pages touched: log.md
- Outcome: Release candidate 0.2.0 is ready with generic claim/source/session APIs, optional Rulefloor and Gograph validators, deterministic local storage, CAS, and durable sessions.
- Validation: Go 1.27.0 make verify passed; GoReleaser 2.17.1 configuration check passed; Gograph 1.5.8 precise build, review, and check completed with zero errors; public capabilities JSON smoke passed; git diff --check passed.
- Follow-ups: Protected architecture and Go 1.27 governance drafts remain pending owner acceptance; local Rulefloor smoke remains unavailable until PATH resolves a JSON-contract-capable release.

## 2026-08-22 — Release-hygiene fixes from the identuum wiki pilot

- Objective: Fix three defects the identuum wiki pilot measured before any new release republishes a broken artifact.
- Changes: (1) `.goreleaser.yaml` homebrew_casks publication REMOVED — the tap now carries a hand-maintained source-build formula (rulefloor mechanism); any GoReleaser homebrew block would overwrite it with an artifact-download install method, and the cask's quarantined unnotarized binary was SIGKILLed by Gatekeeper (measured: identical binary runs without the quarantine xattr, dies with it). (2) Release provenance teeth: the published v0.2.0 assets stamped vcs.modified=true (+dirty — irreproducible from the tag; a clean-clone build of the same tag stamps vcs.modified=false); `make release` now refuses a dirty tree and release CI asserts vcs.modified=false on every built binary. (3) Discovery is read-only: `app.Open` no longer bootstraps `scrinium-guide.md` (a bare `capabilities` call used to write into the governed wiki); the guide is now a `setup_llm_wiki` standard page. Regression tests: TestNewAppWritesNothingIntoWiki, TestSetupCreatedGuideMentionsSetupTool.
- Pages touched: log.md; docs/v0.2-evidence-architecture.md and docs/v0.2-public-api.md updated to the read-only-discovery contract.
- Validation: make verify.

## 2026-08-22 — Release guard false refusal fixed

- Objective: The dirty-tree guard refused a CLEAN tree on the owner's first `make release` (git status --porcelain empty before and after).
- Cause, measured: verify ends in tidy-check, whose cp/`go mod tidy`/mv dance restored identical content but fresh inodes+mtimes; git's stat cache marked go.mod/go.sum modified, `git diff-index --quiet HEAD --` trusted the stale cache and fired, while `git status` (which refreshes the index) read clean.
- Fix: tidy-check now runs `go mod tidy -diff` (reports without writing; measured supported and clean); the release guard runs `git update-index -q --refresh` before diff-index so stat-dirt never reads as a dirty tree.
- Proof: make verify twice, guard exit 0 after each; recreated stat-dirt — bare diff-index exit 1 (old false refusal reproduced), hardened sequence exit 0; content append to README.md — refusal message + exit 1, restored byte-identical (sha256 match), guard exit 0. Release CI untouched (fresh-checkout check was never affected).
- Pages touched: log.md.
- Validation: make verify.

## 2026-08-22 — Provenance assert moved before publication; +dirty root cause corrected

- Objective: v0.2.1 release run 32584410033 FAILED at "Assert clean build provenance" AFTER GoReleaser had published — wrong measurement and wrong order.
- Root cause, measured (CORRECTS the earlier entries' "built outside this workflow from a dirty tree" explanation for v0.2.0): the pipeline stamped +dirty BY CONSTRUCTION. GoReleaser writes dist/ into the tree, dist/ was not gitignored, and Go's build stamper counts UNTRACKED files (git status --porcelain semantics) while the workflow's diff-index refusal saw tracked changes only. Fresh clone of v0.2.1 + goreleaser build → binary stamps v0.2.1+dirty, vcs.modified=true; same clone with dist/ gitignored → vcs.modified=false; a single untracked file alone flips the stamp back to true (snapshot-mode control).
- Fixes: dist/ gitignored; BOTH release guards (Makefile release target and the workflow step) now refuse on `git status --porcelain` non-empty — the same definition Go stamps by — keeping the update-index refresh; the provenance assert is now a per-build post hook in .goreleaser.yaml (fails the build before archiving/publication; proved: clean tree → build succeeded + vcs.modified=false, untracked file → "post hook failed … PROVENANCE FAIL", exit 1, nothing published). GoReleaser OSS cannot split build/assert/publish without rebuilding (release --skip=publish then release --clean publishes a DIFFERENT build; `continue` is Pro-only), so the assert rides the build.
- v0.2.1 disposition, RECORDED not acted: the public tag stays (never rewrite a public tag); the tap formula builds from the GitHub source tarball, so brew users never touch the +dirty binaries; whether to delete the v0.2.1 release's binary assets is an OWNER decision, by name (e.g. `gh release delete-asset v0.2.1 <asset>`), deliberately not taken here.
- Pages touched: log.md.
- Validation: make verify; goreleaser hook proved positive and negative in a fixture clone.

## 2026-08-22 — Validation targets: validators may read outside the governed repo

- Objective: Close the adopting pilot's blocker — wiki-hosted claims could not validate product-repo rules (rulefloor_inauthentic_output / graph_missing) while a co-located control passed.
- Owner ruling implemented: a validator MAY READ outside the governed repository; Scrinium still WRITES only under wiki_root. scrinium.json gains `validation_targets` (abstract name -> local root; additive within the v1 config schema); Rulefloor and Gograph bindings gain a `target` parameter naming one — bindings still cannot carry filesystem paths (internal/app/types.go rule kept). Only allowlisted names resolve; roots must be real directories (no symlinked final component, re-verified at use); the target's canonical artifact (RULE-FLOOR.md / .gograph/graph.json) folds into the scoped snapshot and the input fingerprint, so a moved external repo turns a prior pass STALE (stale_repository_snapshot) rather than silently re-passing. capabilities lists target NAMES only.
- Tests: allowlisted target validates (adapter invoked with --repo <target>, metadata records rulefloor.target); unknown name refused (rulefloor_unknown_target, adapter never invoked); path-shaped names refused at binding parse; symlinked/missing/non-directory roots refused at Open (measured messages); deleted root refused at use (rulefloor_target_unavailable); external-ledger move flips snapshot + input fingerprints (stale test); writes outside wiki_root still refused with targets configured (external root byte-unchanged).
- E2E on a throwaway pair (real rulefloor v0.3.0 binary): wiki claim EXT-RF-1 bound to target "product" rule T-1 -> pass/rule_passed, state observed/current; after touching the target's RULE-FLOOR.md -> cannot_evaluate/stale_repository_snapshot, freshness stale.
- Also fixed en route: app-layer cannotEvaluateResult persisted nil evidence_ids for empty-evidence bindings (refused as malformed canonical record) — now [] like the adapters; target bindings make empty wiki evidence normal.
- Schema decision: everything additive within v1 — new optional config key, new optional binding parameter inside the existing generic Parameters map, one new capabilities field. No schema version bump required.
- Pages touched: log.md; docs/v0.2-public-api.md (Validation targets section).
- Validation: make verify; new unit tests; throwaway E2E pass + stale rounds.
