# Scrinium

Status: v0.2 core implementation complete; protected architecture overview acceptance pending owner review.

Scrinium is a local, repository-owned evidence-backed knowledge system for coding agents. It records claims, provenance, validation policy/history, and derived assessment/freshness without treating storage as truth.

## Supported Toolchain

Scrinium v0.2 requires Go 1.27 or newer. The supported verification and release baseline is the Go 1.27 toolchain declared in `go.mod`. CI uses Staticcheck 0.8.0, govulncheck 1.7.0, setup-go v7, and GoReleaser 2.17.1.

## Implemented v0.2 core

- deterministic one-file-per-claim JSON and typed evidence;
- generic validator orchestration;
- optional Rulefloor and Gograph subprocess adapters;
- cross-process claim/source CAS and locks;
- durable cross-process session checklists;
- canonical one-file-per-source provenance JSON;
- generic public MCP/CLI claim, source, validation, lint, and session operations;
- strict versioned public JSON inputs/results/errors;
- deterministic claim lint separated from legacy heuristic review;
- deprecated v0.1 page/wiki compatibility tools retained.

## Trust limits

Rulefloor static and all Gograph validation are observation-grade. Supported authenticated Rulefloor execution may satisfy verification-grade bindings. Gograph proves selected Go structural predicates only. Scrinium cannot prove global correctness, agent compliance, semantic contradiction absence, or stale prose.

## Deferred

Background validation scheduling, automatic stale-session cleanup, validation-history retention/compaction, and compatibility removal policy are deferred to v0.3.
