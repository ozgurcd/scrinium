# Scrinium Architecture Overview

Status: proposed protected overview for v0.2 acceptance

Scrinium is a local, repository-owned evidence-backed project knowledge system for coding agents.

## Knowledge model

The central object is a Claim: an immutable semantic ID plus subject, statement, lifecycle, typed evidence, optional validation policy, and complete validation history. Assessment and freshness are derived; callers cannot set them.

Canonical claims live at `llm-wiki/claims/<CLAIM-ID>.json`. Canonical source provenance lives at `llm-wiki/sources/records/<SOURCE-ID>.json`. Markdown is human documentation and compatibility views, not canonical claim state.

## Boundaries

```text
CLI / MCP adapters
  -> typed application services
       -> knowledge derivation and validation orchestration
       -> claim/source/session stores
       -> governance and deterministic lint
       -> optional subprocess validators
```

Domain/application code does not depend on MCP or JSON-RPC types. Rulefloor- and Gograph-specific schemas remain inside their adapters; the claim model uses generic bindings/results.

## Trust model

- lifecycle: active, superseded, withdrawn;
- assessment: asserted, sourced, observed, verified, challenged;
- freshness: current, stale, unknown;
- validation outcome: pass, fail, cannot_evaluate.

Manual validation and Gograph are observation-grade. Rulefloor static validation is observation-grade. Only an authenticated supported Rulefloor execute pass may satisfy a verification-grade binding. Verification requires an explicit policy, current required passes, available referenced evidence, and matching fingerprints. A later `cannot_evaluate` removes current verified presentation.

## Concurrency and sessions

Claim and source reads return exact-byte SHA-256 revisions. Every mutation is claim/source scoped, cross-process locked, and compare-and-swap; conflicts are returned without retry or merge. External validators run without holding claim locks and obsolete results are rejected at persistence.

Sessions are ignored repository-local JSON records and durable tracked work-session checklists. They survive process restarts but are not authentication or proof of activity outside Scrinium.

## External validators

Rulefloor validation means one configured rule was evaluated under the recorded mode/profile and repository snapshot. Gograph validation means one supported Go structural predicate was evaluated against an existing current graph/build context. Gograph never rebuilds the graph and CHA edges are possible static targets, not runtime certainty.

Neither validator establishes unrelated behavior or global project correctness.

## Public surface

Generic claim, source, validation, lint, and session operations are exposed through MCP and CLI with strict versioned JSON. Mutation inputs require revisions. Capabilities report deterministic guarantees, heuristic limitations, and optional validator availability.

v0.1 page/wiki tools and `enforce-agents` remain deprecated compatibility interfaces during v0.2.

## Deferred to v0.3

Background validation scheduling, automatic stale-session cleanup, validation-history retention/compaction, and compatibility-removal policy.
