---
title: Proposed Architecture Overview — v0.2 Through Phase 3
type: architecture
status: draft
updated: 2026-08-20
---

# Proposed Architecture Overview — v0.2 Through Phase 3

This draft proposes the eventual replacement for the protected legacy `architecture/overview.md`. It records the implemented Phase 1 boundaries, Phase 2 evidence-backed knowledge core, and Phase 3 generic validation execution layer. It is not an accepted replacement until the owner approves it.

## Dependency Direction

```text
cmd/scrinium and internal/mcp
              |
              v
        internal/app
       /      |       \
      v       v        v
knowledge  validation  compatibility services
   |          |         |
   v          v         v
 store      store     governance/session/lint
```

Transport adapters translate JSON-RPC, MCP, and CLI data into typed application requests. Application services orchestrate use cases. Domain and validation packages do not depend on MCP or JSON-RPC types.

## Implemented Boundaries

- `cmd/scrinium` composes services and retains compatibility entry points.
- `internal/mcp` owns JSON-RPC/MCP transport adaptation and tool dispatch.
- `internal/app` owns typed workflows, persistence orchestration, and derived-state presentation.
- `internal/store` owns repository-confined filesystem access, regular-file checks, symlink escape protection, compare-before-write behavior, and atomic replacement.
- `internal/governance` owns write policy independently from filesystem mechanics.
- `internal/session` owns the current process-local session state.
- `internal/lint` owns legacy compatibility lint and the separate deterministic claim/validation lint path.
- `internal/knowledge` owns Claim, Evidence, ValidationBinding, ValidationResult, lifecycle rules, and derived assessment/freshness.
- `internal/validation` owns validator descriptors, registration, request/result authenticity checks, scoped repository snapshots, and the manual validator.

## Canonical Knowledge Storage

Each claim is stored as strict deterministic JSON at:

```text
llm-wiki/claims/<CLAIM-ID>.json
```

The filename must match the immutable semantic claim ID. Unknown fields, duplicate keys, unsupported schema versions, malformed records, unsafe paths, and non-regular files are rejected. Serialization is stable, two-space indented, newline terminated, compare-before-write, and atomic.

Markdown remains human-readable project documentation. It can reference claims but is not canonical claim state.

## Claim State

The persisted independent dimensions are:

- lifecycle: `active`, `superseded`, or `withdrawn`
- evidence polarity: `supports`, `challenges`, or `context`
- validation outcome: `pass`, `fail`, or `cannot_evaluate`

Assessment and freshness are derived. Callers cannot set them directly. Scrinium degrades toward uncertainty: `cannot_evaluate` is never a pass, missing or stale required results remove verified presentation, and manual validation is observation-grade only.

## Validation Execution

Validators are external evidence producers, not owners of claim state. The generic validator interface receives an isolated Claim, ValidationBinding, deterministic repository snapshot, input fingerprint, and cancellation context. It returns one ValidationResult and cannot persist or mutate Scrinium state.

A validator descriptor declares a stable ID, version, supported binding schema versions, and maximum assurance. The registry rejects invalid descriptors and duplicate IDs, resolves validators deterministically, and tolerates absent validators.

Application orchestration owns the execution sequence:

1. Load the claim and resolve the binding.
2. Resolve and validate the validator descriptor.
3. Build the repository snapshot and deterministic input fingerprint.
4. Execute the validator with isolated request values and the caller context.
5. Authenticate result identity, version, binding, assurance ceiling, fingerprints, repository snapshot, outcome, reason, and timestamps.
6. Persist a valid result or a structured `cannot_evaluate` result for operational/authenticity failures.
7. Recompute derived claim state and return the updated view.

A changed claim or binding during execution causes a conflict rather than persisting a result for obsolete meaning. Validation history remains append-only within the claim record for v0.2.

## Repository Snapshot Model

Scrinium does not assume Git is available. A validation binding must identify repository state through an expected snapshot fingerprint, optionally accompanied by an opaque revision label.

The current snapshotter hashes only explicitly bound `repository_reference` evidence with `repo:` locators. Paths are sorted, repository-confined, regular-file checked, and fingerprinted from current bytes. The aggregate snapshot fingerprint and the validation input fingerprint are deterministic. Scrinium does not scan the entire repository or infer a revision from Git.

If required repository state is missing, escapes confinement, changes from the expected fingerprint, or cannot be reproduced, the attempt records `cannot_evaluate`. A revision label without a reproducible fingerprint is insufficient for execution.

## Manual Validation

The built-in `manual` validator uses the same registry, snapshot, authenticity, persistence, and derivation path as every future adapter. Its descriptor maximum is observation assurance. It may produce pass, fail, or cannot-evaluate outcomes, but it can never produce verification-grade assurance or verified assessment.

## Lint Separation

Legacy wiki lint remains behavior-compatible and includes explicitly heuristic checks such as prompt-injection phrase scanning.

Deterministic claim/validation lint separately checks malformed claims and references, lifecycle and supersession integrity, unknown validators, unsupported binding schemas, assurance above descriptor ceilings, validator/binding identity mismatches, input/snapshot fingerprint mismatches, stale repository snapshots, and missing required results. It does not claim semantic contradiction detection.

## Compatibility and Deferred Work

Existing page-oriented MCP/CLI tools and `enforce-agents` remain deprecated compatibility interfaces throughout v0.2. The source registry remains Markdown-backed for now. Sessions remain process-local, and direct CLI workflows that require a shared session reject that unsupported model.

Deferred until separately authorized:

- Gograph validation adapter
- Rulefloor validation adapter
- durable cross-process sessions
- per-source deterministic JSON records
- public claim validation MCP/CLI commands
- cross-process claim locking or compare-and-swap
- owner acceptance of this draft as the protected architecture overview
