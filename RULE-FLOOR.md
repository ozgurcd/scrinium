FLOOR: 1
RED-PROOFS: 1

| ID | one-sentence rule | enforced-by | check | red-proof | hash |
|---|---|---|---|---|---|
| AUTHORSHIP-NEUTRAL-1 | Authorship never changes a claim's derived state: every authorship kind derives the same lifecycle, assessment, freshness and reasons from the same evidence and validation records. | go-test | internal/knowledge/authorship_test.go @ unit | [proof-v1 kind=mutation_observation] 2026-11-11 mutated internal/knowledge/derive.go DeriveState: a tool-authored asserted claim was raised to sourced; TestAuthorshipNeverChangesDerivedState FAIL on no_evidence 'tool-authored state Assessment sourced, agent-authored state Assessment asserted'; restored byte-identical (sha256 fddf58a4d411), green <!-- rulefloor-binding-v1:eyJleGVjdXRpb25fcG9saWN5IjoiZXhlY3V0ZSJ9 --> | 4f3859cb2b80 |
