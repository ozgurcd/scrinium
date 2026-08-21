# Go 1.27 Protected Governance Update

Status: proposed for owner acceptance

Owner decision: Scrinium v0.2 officially requires Go 1.27.

## Required protected-document edits

### `rules.md`

Replace:

`Follow Go 1.26+ best practices`

with:

`Follow Go 1.27+ best practices. The supported verification baseline is the Go toolchain declared in go.mod.`

### `architecture/overview.md`

Replace the technology-stack language entry:

`Language: Go 1.26+`

with:

`Language: Go 1.27+; CI and release builds select the repository-declared Go 1.27 toolchain.`

The broader legacy architecture content should be superseded through the existing final v0.2 architecture draft rather than edited piecemeal.

### `architecture/development.md`

Replace:

`Go 1.26+ idioms.`

with:

`Go 1.27+ idioms. Run make verify with the toolchain declared in go.mod; Go 1.26 is unsupported.`

## Tooling basis

- CI installs Staticcheck 0.8.0 and govulncheck 1.7.0 under Go 1.27.
- CI and release workflows use actions/setup-go v7 with go.mod.
- Releases use GoReleaser action v7 and GoReleaser 2.17.1.
- GoReleaser builds explicitly select GOTOOLCHAIN=go1.27.0.
