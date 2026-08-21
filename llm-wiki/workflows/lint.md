# Wiki and Claim Lint Workflow

## Deterministic checks

Use `claim_lint` for strict canonical claim/schema/reference/lifecycle/evidence/binding/result and deterministically knowable fingerprint checks. Deterministic findings describe exact record conditions.

## Heuristic review

Legacy `lint_llm_wiki` retains compatibility checks. Instruction-like text is scanned only in deterministic source-summary paths and is reported as a heuristic review lead. Heuristic findings never change claim state.

Scrinium does not implement semantic contradiction detection or stale-prose detection.
