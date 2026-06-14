# Query Workflow

Use this workflow when answering questions from the wiki.

## Success Criteria

- Answer is based on current wiki pages, not memory or old chat context.
- Relevant pages are read before synthesis.
- Claims cite wiki pages or source IDs when possible.
- Useful new analysis is offered as a wiki update when it should persist.
- Durable filed answers are recorded in `log.md`.

## Steps

1. Read `index.md` first to identify likely relevant pages.
2. Read directly relevant source summaries, entity pages, concept pages, project pages, status pages, and syntheses.
3. If the wiki lacks enough evidence, say what is missing and whether a source ingest or web lookup is needed.
4. Answer with citations to wiki paths or source IDs. Distinguish sourced facts from inference.
5. If the question produces a durable comparison, synthesis, or decision, ask whether to file it back into `llm-wiki/` unless the user already requested a wiki update.
6. When filing a durable answer, update `index.md` and append `## [YYYY-MM-DD] query | <Question>` to `log.md`.
7. If a contradiction or stale claim is discovered, update the relevant page or record a lint finding.

## Answer Rules

- Do not treat old logs, historical audit notes, previous prompt reports, or previous final reports as current requirements unless explicitly marked current.
- Prefer current status and decision pages over historical reports.
- Surface uncertainty. Do not merge conflicting claims silently.
- Do not cite raw source files directly in final answers when a source summary page exists; cite the source ID and wiki page instead.
