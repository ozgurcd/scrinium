package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/lint"
	"scrinium/internal/provenance"
)

func prepareSourceService(t *testing.T) (*Service, context.Context, string) {
	t.Helper()
	service := newTestService(t, nil)
	ctx := context.Background()
	if _, err := service.SetupWiki(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := service.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md", "workflows/ingest.md"} {
		if _, err := service.ReadPage(ctx, PageRequest{Path: path, SessionID: status.SessionID}); err != nil {
			t.Fatal(err)
		}
	}
	return service, ctx, status.SessionID
}

func registerTestSource(t *testing.T, service *Service, ctx context.Context, sessionID, id, rawPath string) SourceView {
	t.Helper()
	if err := service.repository.Write(ctx, rawPath, []byte("raw bytes for "+id+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.RegisterSource(ctx, RegisterSourceRequest{
		SessionID: sessionID, SourceID: id, Title: "Source " + id, RawPath: rawPath,
		SourceType: string(provenance.SourceTypeOwnerInput), Origin: string(provenance.OriginOwner),
		TrustLevel: string(provenance.TrustOwner), ReceivedDate: "2026-08-20", IngestDate: "2026-08-21",
	}); err != nil {
		t.Fatal(err)
	}
	view, err := service.GetSource(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func TestCanonicalSourceRegistrationAndSessionTracking(t *testing.T) {
	service, ctx, sessionID := prepareSourceService(t)
	view := registerTestSource(t, service, ctx, sessionID, "SRC-20260820-canonical", "raw/canonical.md")
	if view.Revision == "" || !strings.HasPrefix(view.Source.RawFingerprint, "sha256:") {
		t.Fatalf("canonical source lacks revision/fingerprint: %#v", view)
	}
	data, err := service.store.Read(ctx, "sources/records/SRC-20260820-canonical.json")
	if err != nil || !strings.HasSuffix(string(data), "\n") || !strings.Contains(string(data), provenance.SchemaVersion) {
		t.Fatalf("canonical JSON missing or unstable: %q %v", data, err)
	}
	registry, err := service.store.Read(ctx, "source-registry.md")
	if err != nil || !strings.Contains(string(registry), "generated from canonical records") || !strings.Contains(string(registry), view.Source.RawFingerprint) {
		t.Fatalf("registry view not derived from canonical state: %s %v", registry, err)
	}
	status, err := service.SessionStatus(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, path := range status.DocumentsWritten {
		if path == "sources/records/SRC-20260820-canonical.json" {
			found = true
		}
	}
	if !found || status.NeedsSourceRegistry {
		t.Fatalf("session did not track canonical source write: %#v", status)
	}
}

func TestNoOpSourceRegistryRebuildSatisfiesSessionMaintenance(t *testing.T) {
	service, ctx, sessionID := prepareSourceService(t)
	registerTestSource(t, service, ctx, sessionID, "SRC-20260820-registry", "raw/registry.md")

	if err := service.UpdatePage(ctx, WritePageRequest{
		Path:      "sources/README.md",
		Content:   "# Source summaries\n",
		SessionID: sessionID,
	}); err != nil {
		t.Fatal(err)
	}
	status, err := service.SessionStatus(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !status.NeedsSourceRegistry {
		t.Fatal("source documentation write did not require registry maintenance")
	}

	if err := service.RebuildSourceRegistry(ctx, sessionID); err != nil {
		t.Fatal(err)
	}
	status, err = service.SessionStatus(ctx, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	if status.NeedsSourceRegistry {
		t.Fatal("no-op deterministic registry rebuild did not satisfy maintenance")
	}
}

func TestSourceRegistrationRejectsMissingUnsafeAndSymlinkRawFiles(t *testing.T) {
	service, ctx, sessionID := prepareSourceService(t)
	request := RegisterSourceRequest{
		SessionID: sessionID, SourceID: "SRC-20260820-missing", Title: "Missing", RawPath: "raw/missing.md",
		SourceType: "unknown", TrustLevel: "unknown",
	}
	if _, err := service.RegisterSource(ctx, request); err == nil {
		t.Fatal("missing raw source was accepted")
	}
	request.SourceID = "SRC-20260820-traversal"
	request.RawPath = "../outside.md"
	if _, err := service.RegisterSource(ctx, request); err == nil {
		t.Fatal("unsafe raw path was accepted")
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	rawDirectory := filepath.Join(service.repository.Root(), "raw")
	if err := os.MkdirAll(rawDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(rawDirectory, "linked.md")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	request.SourceID = "SRC-20260820-linked"
	request.RawPath = "raw/linked.md"
	if _, err := service.RegisterSource(ctx, request); err == nil {
		t.Fatal("symlink raw source was accepted")
	}
}

func TestSourceCASLifecycleAndExplicitRefresh(t *testing.T) {
	service, ctx, sessionID := prepareSourceService(t)
	first := registerTestSource(t, service, ctx, sessionID, "SRC-20260820-first", "raw/first.md")
	second := registerTestSource(t, service, ctx, sessionID, "SRC-20260820-second", "raw/second.md")
	title := "Updated title"
	updated, err := service.UpdateSource(ctx, UpdateSourceRequest{SessionID: sessionID, SourceID: first.Source.ID, ExpectedRevision: first.Revision, Title: &title})
	if err != nil || updated.Revision == first.Revision {
		t.Fatalf("source CAS update failed: %#v %v", updated, err)
	}
	_, err = service.UpdateSource(ctx, UpdateSourceRequest{SessionID: sessionID, SourceID: first.Source.ID, ExpectedRevision: first.Revision, Title: &title})
	var conflict *SourceConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale source mutation was not typed: %v", err)
	}
	if err := service.repository.Write(ctx, first.Source.RawPath, []byte("changed bytes\n"), 0644); err != nil {
		t.Fatal(err)
	}
	lintBefore, err := service.LintSources(ctx)
	if err != nil || !hasFinding(lintBefore.Findings, "changed_source_fingerprint") {
		t.Fatalf("changed bytes were not reported: %#v %v", lintBefore, err)
	}
	refreshed, err := service.RefreshSource(ctx, RefreshSourceRequest{SessionID: sessionID, SourceID: first.Source.ID, ExpectedRevision: updated.Revision})
	if err != nil || refreshed.Source.RawFingerprint == first.Source.RawFingerprint {
		t.Fatalf("explicit refresh did not accept new bytes: %#v %v", refreshed, err)
	}
	superseded, err := service.SupersedeSource(ctx, SupersedeSourceRequest{SessionID: sessionID, SourceID: first.Source.ID, SuccessorID: second.Source.ID, ExpectedRevision: refreshed.Revision})
	if err != nil || superseded.Source.Status != provenance.StatusSuperseded || superseded.Source.SupersededBy != second.Source.ID {
		t.Fatalf("source supersession failed: %#v %v", superseded, err)
	}
	withdrawn, err := service.WithdrawSource(ctx, WithdrawSourceRequest{SessionID: sessionID, SourceID: second.Source.ID, ExpectedRevision: second.Revision})
	if err != nil || withdrawn.Source.Status != provenance.StatusWithdrawn {
		t.Fatalf("source withdrawal failed: %#v %v", withdrawn, err)
	}
}

func TestClaimEvidenceResolvesCanonicalSourceAndBecomesStale(t *testing.T) {
	service, ctx, sessionID := prepareSourceService(t)
	source := registerTestSource(t, service, ctx, sessionID, "SRC-20260820-evidence", "raw/evidence.md")
	now := time.Now().UTC()
	view, err := service.CreateClaim(ctx, CreateClaimRequest{
		SessionID: sessionID, ID: "SOURCE-EVIDENCE-1", Subject: "provenance", Statement: "The source records this design.",
		AuthorshipKind: knowledge.AuthorshipHuman, AuthorOrigin: "owner",
		Evidence: []knowledge.Evidence{{
			ID: "EVIDENCE-SOURCE-1", Kind: knowledge.EvidenceExternalSource, Polarity: knowledge.PolaritySupports,
			Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginExternal, Reference: source.Source.ID},
			Locator: "source:" + source.Source.ID, Scope: "documented design", Availability: knowledge.AvailabilityUnknown,
			CapturedAt: now, DerivedFrom: []string{},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.State.Assessment != knowledge.AssessmentSourced || view.State.Freshness != knowledge.FreshnessCurrent {
		t.Fatalf("canonical source did not produce sourced/current state: %#v", view.State)
	}
	if view.Claim.Evidence[0].Fingerprint != source.Source.RawFingerprint {
		t.Fatal("source fingerprint was not bound into evidence")
	}
	if err := service.repository.Write(ctx, source.Source.RawPath, []byte("changed after claim\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stale, err := service.GetClaim(ctx, view.Claim.ID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if stale.State.Freshness != knowledge.FreshnessStale {
		t.Fatalf("changed source did not make evidence stale: %#v", stale.State)
	}
	report, err := service.LintClaims(ctx, time.Now().UTC())
	if err != nil || !hasFinding(report.Findings, "changed_source_fingerprint") {
		t.Fatalf("claim lint did not report changed source: %#v %v", report, err)
	}
}

func hasFinding(findings []lint.ClaimFinding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
