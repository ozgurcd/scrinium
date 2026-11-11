# Release-notes fragment: authorship kind `tool` (SCRINIUM-AUTHORSHIP-TOOL)

Fold into the next version's release notes.

- New claim authorship kind `tool`: deterministic software created the claim, with no model and no person writing it. `origin` names the program (for example `nuntius`) and is required, as it is for every kind. The kind is accepted by canonical claim validation, `claim_create` over CLI and MCP, and `claim_lint`.
- Authorship stays provenance only. `tool` asserts nothing about truth, grants no assessment, never changes assessment, freshness, or lifecycle, and needs the same session, governance, and `expected_revision` as any other write. Unknown kinds, including `Tool` and `tools`, are still refused by name.
- `capabilities` gains `authorship_kinds`, the list of kinds this binary accepts (additive within `scrinium.capabilities/v1`).
- Compatibility: `tool` is a new enumerated value inside `scrinium.claim/v1` and `scrinium.claim-create/v1`; the schema version is unchanged and existing records are unaffected. An older binary refuses a claim file that uses it: `claim_get` returns `malformed_canonical_record`, `claim_list` fails for the whole claims directory while the file is present, and `claim_lint` reports `malformed_claim_file`. Upgrade every Scrinium binary that reads a repository before writing `tool`-authored claims into it, and check `authorship_kinds` in `capabilities` first; a binary without that key does not accept `tool`.
