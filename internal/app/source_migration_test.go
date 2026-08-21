package app

import (
	"context"
	"strings"
	"testing"
)

func legacyRegistry(id, rawPath, summaryPath string) string {
	return `# Source Registry

This registry tracks raw sources ingested into the wiki.

## Sources

### ` + id + `

- Title: Legacy design
- Raw path: ` + rawPath + `
- Source summary: ` + summaryPath + `
- Source type: project design document
- Trust level: trusted-owner
- Received date: 2026-08-20
- Ingest date: 2026-08-21
- Status: current
- Derived pages:
  - projects/scrinium.md
- Notes: Legacy provenance note.
`
}

func legacySummary(id, rawPath string) string {
	return `# Legacy design

## Metadata

- Source ID: ` + id + `
- Original path: ` + rawPath + `
- Source type: project design document
- Received date: 2026-08-20
- Ingest date: 2026-08-21
- Trust level: trusted-owner

## Summary

Legacy summary remains unchanged.
`
}

func writeLegacySource(t *testing.T, service *Service, id, rawPath string) (string, string) {
	t.Helper()
	ctx := context.Background()
	summaryPath := sourceSummaryPath(id)
	registry := legacyRegistry(id, rawPath, summaryPath)
	summary := legacySummary(id, rawPath)
	if err := service.repository.Write(ctx, rawPath, []byte("legacy raw bytes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := service.store.Write(ctx, summaryPath, []byte(summary), 0644); err != nil {
		t.Fatal(err)
	}
	if err := service.store.Write(ctx, "source-registry.md", []byte(registry), 0644); err != nil {
		t.Fatal(err)
	}
	return registry, summary
}

func TestSourceMigrationDryRunApplyAndIdempotence(t *testing.T) {
	service := newTestService(t, nil)
	ctx := context.Background()
	id := "SRC-20260820-legacy"
	registryBefore, summaryBefore := writeLegacySource(t, service, id, "raw/legacy.md")
	report, err := service.AssessSourceMigration(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Candidates) != 1 || len(report.Debt) != 0 || report.Candidates[0].SourceID != id {
		t.Fatalf("unexpected dry-run report: %#v", report)
	}
	if _, err := service.GetSource(ctx, id); err == nil {
		t.Fatal("dry-run created a canonical record")
	}
	registryAfterDryRun, _ := service.store.Read(ctx, "source-registry.md")
	if string(registryAfterDryRun) != registryBefore {
		t.Fatal("dry-run modified the registry")
	}
	sessionID := sourceMigrationSession(t, service)
	applied, err := service.ApplySourceMigration(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Created) != 1 || applied.Created[0] != id {
		t.Fatalf("migration did not create canonical source: %#v", applied)
	}
	stored, err := service.GetSource(ctx, id)
	if err != nil || stored.Source.RawFingerprint == "" || stored.Source.ProvenanceNotes[0] != "Legacy provenance note." {
		t.Fatalf("migrated source lost provenance: %#v %v", stored, err)
	}
	registryAfter, _ := service.store.Read(ctx, "source-registry.md")
	summaryAfter, _ := service.store.Read(ctx, sourceSummaryPath(id))
	if string(registryAfter) != registryBefore || string(summaryAfter) != summaryBefore {
		t.Fatal("migration apply rewrote legacy Markdown")
	}
	second, err := service.ApplySourceMigration(ctx, sessionID)
	if err != nil || len(second.Created) != 0 || len(second.Existing) != 1 || second.Existing[0] != id {
		t.Fatalf("migration apply was not idempotent: %#v %v", second, err)
	}
}

func TestSourceMigrationReportsAmbiguousMalformedAndMissingInputs(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, *Service)
		code  string
	}{
		{name: "ambiguous duplicate", code: "ambiguous_legacy_record", setup: func(t *testing.T, service *Service) {
			registry, _ := writeLegacySource(t, service, "SRC-20260820-duplicate", "raw/duplicate.md")
			if err := service.store.Write(context.Background(), "source-registry.md", []byte(registry+"\n"+strings.TrimPrefix(registry, "# Source Registry\n")), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "malformed field", code: "malformed_registry_entry", setup: func(t *testing.T, service *Service) {
			registry, _ := writeLegacySource(t, service, "SRC-20260820-malformed", "raw/malformed.md")
			registry = strings.Replace(registry, "- Notes:", "- Unexpected:", 1)
			if err := service.store.Write(context.Background(), "source-registry.md", []byte(registry), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing summary", code: "missing_summary", setup: func(t *testing.T, service *Service) {
			id := "SRC-20260820-no-summary"
			if err := service.repository.Write(context.Background(), "raw/no-summary.md", []byte("raw\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := service.store.Write(context.Background(), "source-registry.md", []byte(legacyRegistry(id, "raw/no-summary.md", sourceSummaryPath(id))), 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "missing raw", code: "missing_raw_source", setup: func(t *testing.T, service *Service) {
			id := "SRC-20260820-no-raw"
			if err := service.store.Write(context.Background(), sourceSummaryPath(id), []byte(legacySummary(id, "raw/no-raw.md")), 0644); err != nil {
				t.Fatal(err)
			}
			if err := service.store.Write(context.Background(), "source-registry.md", []byte(legacyRegistry(id, "raw/no-raw.md", sourceSummaryPath(id))), 0644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := newTestService(t, nil)
			test.setup(t, service)
			report, err := service.AssessSourceMigration(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Candidates) != 0 || !migrationHasDebt(report, test.code) {
				t.Fatalf("migration debt %s not reported safely: %#v", test.code, report)
			}
		})
	}
}

func sourceMigrationSession(t *testing.T, service *Service) string {
	t.Helper()
	ctx := context.Background()
	if _, err := service.SetupWiki(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := service.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, page := range []string{"index.md", "agent-rules.md", "workflows/ingest.md"} {
		if _, err := service.ReadPage(ctx, PageRequest{Path: page, SessionID: status.SessionID}); err != nil {
			t.Fatal(err)
		}
	}
	return status.SessionID
}

func migrationHasDebt(report SourceMigrationReport, code string) bool {
	for _, debt := range report.Debt {
		if debt.Code == code {
			return true
		}
	}
	return false
}
