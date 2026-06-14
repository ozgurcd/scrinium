# Development Guidelines

## Build & Verify
- `make build` — Compile the server binary to `./scrinium`.
- `make test` — Run tests with `-count=1 -timeout=120s`.
- `make verify` — Full verification: build, test, vet, format-check, staticcheck, govulncheck, tidy-check.

## Completion Definition
A task is done only when `make test` and `make verify` pass without errors.

## Testing Standards
- Unit tests live in `cmd/scrinium/app_test.go`.
- Tests must cover PBAC boundary enforcement and semantic error formatting.
- Path traversal rejection must be tested.
- No real DNS/network access in unit tests.

## Code Style
- Go 1.26+ idioms.
- `context.Context` must be respected — handle expiration properly.
- Use the standard library; avoid external dependencies.
- `gofmt` for formatting (checked non-destructively via `gofmt -l`).
