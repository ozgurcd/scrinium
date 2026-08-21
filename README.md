# Scrinium

Scrinium is a local, repository-owned evidence-backed knowledge system for coding agents.

It records project claims, their provenance, and bounded validation results. Canonical claims and source metadata are deterministic, human-readable JSON files that remain Git-diffable. Markdown remains useful for documentation, but storing Markdown does not make it true.

Scrinium optionally integrates with Gograph for structural observation and Rulefloor for selected invariant/test validation. Neither integration proves unrelated behavior or global project correctness.

## Trust model

Scrinium keeps independent dimensions visible:

- lifecycle: `active`, `superseded`, `withdrawn`;
- assessment: `asserted`, `sourced`, `observed`, `verified`, `challenged`;
- freshness: `current`, `stale`, `unknown`;
- validation outcome: `pass`, `fail`, `cannot_evaluate`.

Assessment and freshness are derived. Callers cannot set them directly. An LLM summary is not evidence for its own correctness, manual validation is observation-grade, `cannot_evaluate` never counts as pass, and stale or missing evidence removes current verified presentation.

## Repository storage

```text
llm-wiki/
  claims/<CLAIM-ID>.json             canonical claims and validation history
  sources/records/<SOURCE-ID>.json   canonical source provenance
  sources/<SOURCE-ID>.md             human-readable source summaries
  source-registry.md                 generated compatibility view
  .scrinium/                         ignored locks and durable work sessions
```

Claim and source mutations use exact-byte SHA-256 revision tokens and cross-process compare-and-swap. Conflicts are returned to the caller; Scrinium does not silently merge or use last-write-wins.

## Preferred workflow

1. Call `capabilities`.
2. Start a durable checklist with `session_begin` and retain its session ID.
3. Read `index.md`, `agent-rules.md`, and relevant workflows.
4. Register provenance with `source_register` when applicable.
5. Create or read a claim. Mutations require the revision returned by the preceding read.
6. Attach evidence and set an explicit validation policy.
7. Use generic `claim_validate`; the binding selects an available validator.
8. Inspect lifecycle, assessment, freshness, evidence, validation history, and revision separately.
9. Maintain required human documentation/log views, then use `session_finish`.

Sessions are durable tracked work-session checklists. They are coordination metadata, not authentication, a security boundary, or proof of agent compliance. Scrinium can track only operations it observes.

## Public MCP operations

Claims:

`claim_create`, `claim_get`, `claim_list`, `claim_update`, `claim_add_evidence`, `claim_set_validation_policy`, `claim_validate`, `claim_supersede`, `claim_withdraw`, `claim_lint`.

Sources:

`source_register`, `source_get`, `source_list`, `source_refresh`, `source_migration_status`.

Sessions:

`session_begin`, `session_continue`, `session_status`, `session_finish`, `session_abandon`, `session_list`.

Complex inputs use a strict versioned JSON object. The CLI accepts the same object with `--input FILE` or `--input-json JSON`; add `--json` for one versioned machine-readable JSON document on stdout. See [docs/v0.2-public-api.md](docs/v0.2-public-api.md).

Existing page/wiki tools and `enforce-agents` remain available as deprecated v0.2 compatibility interfaces. They are not the preferred knowledge-state API.

## External validators

- A Rulefloor static pass means the selected rule's binding and test integrity matched the recorded ledger state. It is observation-grade.
- A Rulefloor execute pass can be verification-grade only when the selected supported rule was actually executed under the recorded mode/profile and repository snapshot.
- A Gograph result is observation-grade structural evidence for the selected predicate and recorded graph/build context. CHA edges are possible static targets, not runtime certainty. Unresolved dispatch returns `cannot_evaluate`; validation never rebuilds the graph.

Validators are optional. Missing executables do not prevent Scrinium startup. An attempted unavailable validation degrades safely to `cannot_evaluate`.

## Install and configure

```bash
brew tap ozgurcd/tap
brew install --cask scrinium
```

Or build locally:

```bash
make build
scrinium version
```

Building and verifying Scrinium v0.2 requires Go 1.27 or newer. CI and release builds use the Go 1.27 toolchain declared in `go.mod`.

Minimal `scrinium.json`:

```json
{
  "wiki_root": "./llm-wiki",
  "write_governance": {
    "protected_files": ["rules.md", "architecture/*"]
  },
  "validators": {
    "rulefloor": {"executable": "rulefloor"},
    "gograph": {"executable": "gograph"}
  }
}
```

External validators are discovered through the configured executable or `PATH`. Claim content cannot select executables, repository roots, arbitrary flags, or shell commands.

## Deterministic lint and heuristic review

`claim_lint` performs deterministic canonical-record, reference, lifecycle, evidence, binding, result, and fingerprint checks. Legacy `lint_llm_wiki` retains compatibility checks; instruction-like source text is explicitly a heuristic review lead and never changes claim state.

Scrinium does not perform semantic contradiction detection or stale-prose detection.

## Development

```bash
make verify
```

The verification gate uses the Go version declared by the repository and runs build, tests, formatting/vet, static analysis, vulnerability checks, and Gograph checks.
