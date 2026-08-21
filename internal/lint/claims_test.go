package lint

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/provenance"
	"scrinium/internal/store"
	"scrinium/internal/validation"
)

var lintClaimTime = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

const lintValidationFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func lintClaim(id string) knowledge.Claim {
	return knowledge.Claim{
		SchemaVersion: knowledge.SchemaVersion, ID: id, Subject: "subject", Statement: "A durable assertion.",
		Lifecycle:  knowledge.Lifecycle{State: knowledge.LifecycleActive},
		Authorship: knowledge.Authorship{Kind: knowledge.AuthorshipOwner, Origin: "owner:project", RecordedAt: lintClaimTime},
		Evidence:   []knowledge.Evidence{}, ValidationResults: []knowledge.ValidationResult{},
		CreatedAt: lintClaimTime, UpdatedAt: lintClaimTime,
	}
}

func newClaimLinter(t *testing.T) (*store.Store, *store.ClaimStore, *ClaimService) {
	t.Helper()
	repositoryPath := t.TempDir()
	repository, err := store.New(repositoryPath)
	if err != nil {
		t.Fatal(err)
	}
	wiki, err := store.New(filepath.Join(repositoryPath, "llm-wiki"))
	if err != nil {
		t.Fatal(err)
	}
	claims := store.NewClaimStore(wiki)
	sources := store.NewSourceStore(wiki)
	validators := validation.NewRegistry()
	return repository, claims, NewClaimService(claims, sources, repository, validators, validation.NewSnapshotter(repository))
}

type lintValidator struct {
	descriptor validation.Descriptor
	bindingErr error
}

func (v lintValidator) Descriptor() validation.Descriptor { return v.descriptor }

func (lintValidator) Validate(context.Context, validation.Request) (knowledge.ValidationResult, error) {
	return knowledge.ValidationResult{}, nil
}

func (v lintValidator) ValidateBinding(knowledge.ValidationBinding) error { return v.bindingErr }

func findingCodes(report ClaimReport) map[string]bool {
	codes := make(map[string]bool, len(report.Findings))
	for _, finding := range report.Findings {
		codes[finding.Code] = true
		if finding.AnalysisKind != "deterministic" {
			panic("claim lint finding was not deterministic")
		}
	}
	return codes
}

func TestClaimLintFindsBrokenLinksDuplicatesAndCycles(t *testing.T) {
	_, claims, linter := newClaimLinter(t)
	ctx := context.Background()
	first := lintClaim("CLAIM-CYCLE-1")
	first.Lifecycle = knowledge.Lifecycle{State: knowledge.LifecycleSuperseded, SupersededBy: "CLAIM-CYCLE-2"}
	second := lintClaim("CLAIM-CYCLE-2")
	second.Lifecycle = knowledge.Lifecycle{State: knowledge.LifecycleSuperseded, SupersededBy: "CLAIM-CYCLE-1"}
	broken := lintClaim("CLAIM-BROKEN-1")
	broken.Lifecycle = knowledge.Lifecycle{State: knowledge.LifecycleSuperseded, SupersededBy: "CLAIM-MISSING-1"}
	for _, claim := range []knowledge.Claim{first, second, broken} {
		if _, err := claims.Create(ctx, claim); err != nil {
			t.Fatal(err)
		}
	}
	report, err := linter.Build(ctx, lintClaimTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	codes := findingCodes(report)
	for _, code := range []string{"broken_claim_reference", "invalid_lifecycle_link", "supersession_cycle"} {
		if !codes[code] {
			t.Fatalf("missing %s in %#v", code, report.Findings)
		}
	}
}

func TestClaimLintFindsDuplicateIDAndFilenameMismatch(t *testing.T) {
	_, claims, linter := newClaimLinter(t)
	ctx := context.Background()
	claim := lintClaim("CLAIM-DUPLICATE-1")
	if _, err := claims.Create(ctx, claim); err != nil {
		t.Fatal(err)
	}
	data, err := store.EncodeClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	// Write through a second confined store handle to model legacy malformed data.
	if err := os.WriteFile(filepath.Join(linter.repository.Root(), "llm-wiki", "claims", "CLAIM-OTHER-1.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	report, err := linter.Build(ctx, lintClaimTime)
	if err != nil {
		t.Fatal(err)
	}
	codes := findingCodes(report)
	if !codes["duplicate_claim_id"] || !codes["filename_id_mismatch"] {
		t.Fatalf("expected duplicate and mismatch findings, got %#v", report.Findings)
	}
}

func TestClaimLintFindsMalformedReferences(t *testing.T) {
	_, claims, linter := newClaimLinter(t)
	claim := lintClaim("CLAIM-EVIDENCE-1")
	evidence := knowledge.Evidence{
		ID: "EVD-DECISION-1", Kind: knowledge.EvidenceDecision, Polarity: knowledge.PolaritySupports,
		Origin: knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "adr:1"}, Locator: "adr:1",
		Scope: "claim", Availability: knowledge.AvailabilityAvailable, CapturedAt: lintClaimTime, DerivedFrom: []string{},
	}
	claim.Evidence = []knowledge.Evidence{evidence}
	data, err := store.EncodeClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"derived_from": []`), []byte(`"derived_from": ["EVD-MISSING-1"]`), 1)
	claimDir := filepath.Join(linter.repository.Root(), "llm-wiki", "claims")
	if err := os.MkdirAll(claimDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claimDir, claim.ID+".json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	report, err := linter.Build(context.Background(), lintClaimTime)
	if err != nil {
		t.Fatal(err)
	}
	if !findingCodes(report)["broken_evidence_reference"] {
		t.Fatalf("expected broken evidence reference, got %#v", report.Findings)
	}
	_ = claims
}

func TestClaimLintChecksRepositoryEvidence(t *testing.T) {
	repository, claims, linter := newClaimLinter(t)
	ctx := context.Background()
	if err := repository.Write(ctx, "present.txt", []byte("changed"), 0644); err != nil {
		t.Fatal(err)
	}
	claim := lintClaim("CLAIM-REPOSITORY-1")
	claim.Evidence = []knowledge.Evidence{
		{
			ID: "EVD-REPOSITORY-1", Kind: knowledge.EvidenceRepositoryReference, Polarity: knowledge.PolaritySupports,
			Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "repo:present.txt"},
			Locator: "repo:present.txt", Scope: "file content",
			Fingerprint:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Availability: knowledge.AvailabilityAvailable, CapturedAt: lintClaimTime, DerivedFrom: []string{},
		},
		{
			ID: "EVD-REPOSITORY-2", Kind: knowledge.EvidenceRepositoryReference, Polarity: knowledge.PolaritySupports,
			Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "repo:missing.txt"},
			Locator: "repo:missing.txt", Scope: "file presence", Availability: knowledge.AvailabilityAvailable,
			CapturedAt: lintClaimTime, DerivedFrom: []string{},
		},
	}
	if _, err := claims.Create(ctx, claim); err != nil {
		t.Fatal(err)
	}
	report, err := linter.Build(ctx, lintClaimTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	codes := findingCodes(report)
	if !codes["stale_evidence_fingerprint"] || !codes["missing_evidence"] {
		t.Fatalf("expected repository evidence findings, got %#v", report.Findings)
	}
}

func TestClaimLintResolvesCanonicalSourceEvidence(t *testing.T) {
	repository, claims, linter := newClaimLinter(t)
	ctx := context.Background()
	if err := repository.Write(ctx, "raw/source.md", []byte("original\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, fingerprint, err := repository.Fingerprint(ctx, "raw/source.md")
	if err != nil {
		t.Fatal(err)
	}
	received := provenance.Date("2026-08-20")
	source := provenance.SourceRecord{
		SchemaVersion: provenance.SchemaVersion, ID: "SRC-20260820-lint-source", Title: "Lint source",
		SourceType: provenance.SourceTypeRepositoryDocument, Origin: provenance.Origin{Kind: provenance.OriginProject, Trust: provenance.TrustProject},
		RawPath: "raw/source.md", RawFingerprint: fingerprint, ReceivedDate: &received, IngestDate: "2026-08-20",
		Status: provenance.StatusCurrent, DerivedClaims: []string{}, DerivedPages: []string{}, ProvenanceNotes: []string{},
		CreatedAt: lintClaimTime, UpdatedAt: lintClaimTime,
	}
	if _, err := linter.sources.Create(ctx, source); err != nil {
		t.Fatal(err)
	}
	if err := repository.Write(ctx, "raw/source.md", []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	claim := lintClaim("CLAIM-SOURCE-LINT-1")
	claim.Evidence = []knowledge.Evidence{
		{ID: "EVD-SOURCE-LINT-1", Kind: knowledge.EvidenceExternalSource, Polarity: knowledge.PolaritySupports, Origin: knowledge.EvidenceOrigin{Kind: knowledge.OriginExternal, Reference: source.ID}, Locator: "source:" + source.ID, Scope: "source", Fingerprint: fingerprint, Availability: knowledge.AvailabilityAvailable, CapturedAt: lintClaimTime, DerivedFrom: []string{}},
		{ID: "EVD-SOURCE-LINT-2", Kind: knowledge.EvidenceExternalSource, Polarity: knowledge.PolaritySupports, Origin: knowledge.EvidenceOrigin{Kind: knowledge.OriginExternal, Reference: "missing"}, Locator: "SRC-20260820-missing", Scope: "source", Availability: knowledge.AvailabilityUnknown, CapturedAt: lintClaimTime, DerivedFrom: []string{}},
		{ID: "EVD-SOURCE-LINT-3", Kind: knowledge.EvidenceExternalSource, Polarity: knowledge.PolaritySupports, Origin: knowledge.EvidenceOrigin{Kind: knowledge.OriginExternal, Reference: "invalid"}, Locator: "source:SRC-invalid", Scope: "source", Availability: knowledge.AvailabilityUnknown, CapturedAt: lintClaimTime, DerivedFrom: []string{}},
	}
	if _, err := claims.Create(ctx, claim); err != nil {
		t.Fatal(err)
	}
	report, err := linter.Build(ctx, lintClaimTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	codes := findingCodes(report)
	for _, code := range []string{"changed_source_fingerprint", "missing_source_record", "invalid_source_id", "stale_claim_inputs"} {
		if !codes[code] {
			t.Fatalf("missing source finding %s: %#v", code, report.Findings)
		}
	}
}

func TestClaimLintChecksValidatorCompatibilityAndResults(t *testing.T) {
	repository, claims, linter := newClaimLinter(t)
	ctx := context.Background()
	if err := repository.Write(ctx, "project/state.txt", []byte("current state\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, fingerprint, err := repository.Fingerprint(ctx, "project/state.txt")
	if err != nil {
		t.Fatal(err)
	}
	snapshotFingerprint := validation.RepositoryFingerprint([]validation.RepositoryEntry{{Path: "project/state.txt", Fingerprint: fingerprint}})
	evidence := knowledge.Evidence{
		ID: "EVD-PROJECT-STATE-1", Kind: knowledge.EvidenceRepositoryReference, Polarity: knowledge.PolaritySupports,
		Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "repo:project/state.txt"},
		Locator: "repo:project/state.txt", Scope: "project", Fingerprint: fingerprint,
		Availability: knowledge.AvailabilityAvailable, CapturedAt: lintClaimTime, DerivedFrom: []string{},
	}

	unknown := lintClaim("CLAIM-UNKNOWN-VALIDATOR-1")
	unknown.Evidence = []knowledge.Evidence{evidence}
	unknownBinding := knowledge.ValidationBinding{
		ID: "VAL-UNKNOWN-1", ValidatorID: "unknown-validator", BindingVersion: "v1", Reference: "unknown",
		Required: true, RequiredAssurance: knowledge.AssuranceVerification, EvidenceIDs: []string{evidence.ID},
		InputFingerprint: lintValidationFingerprint, SnapshotFingerprint: snapshotFingerprint,
	}
	unknownBinding.InputFingerprint = validation.InputFingerprint(unknown, unknownBinding, validation.RepositorySnapshot{Fingerprint: snapshotFingerprint})
	unknown.ValidationPolicy = &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{unknownBinding}}
	if _, err := claims.Create(ctx, unknown); err != nil {
		t.Fatal(err)
	}

	observerDescriptor := validation.Descriptor{ID: "observer", Version: "1.0.0", SupportedBindingVersions: []string{"v1"}, MaximumAssurance: knowledge.AssuranceObservation}
	if err := linter.validators.Register(lintValidator{descriptor: observerDescriptor}); err != nil {
		t.Fatal(err)
	}
	observed := lintClaim("CLAIM-OBSERVER-1")
	observed.Evidence = []knowledge.Evidence{evidence}
	observedBinding := knowledge.ValidationBinding{
		ID: "VAL-OBSERVER-1", ValidatorID: "observer", BindingVersion: "v1", Reference: "observe",
		Required: true, RequiredAssurance: knowledge.AssuranceObservation, EvidenceIDs: []string{evidence.ID},
		InputFingerprint: lintValidationFingerprint, SnapshotFingerprint: snapshotFingerprint,
	}
	observedBinding.InputFingerprint = validation.InputFingerprint(observed, observedBinding, validation.RepositorySnapshot{Fingerprint: snapshotFingerprint})
	observed.ValidationPolicy = &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{observedBinding}}
	observed.ValidationResults = []knowledge.ValidationResult{{
		ID: "RESULT-OBSERVER-1", BindingID: observedBinding.ID, ValidatorID: observedBinding.ValidatorID,
		ValidatorVersion: "9.0.0", BindingVersion: observedBinding.BindingVersion,
		SnapshotFingerprint: snapshotFingerprint, InputFingerprint: lintValidationFingerprint,
		Assurance: knowledge.AssuranceVerification, Outcome: knowledge.OutcomePass, Reason: "claimed pass",
		EvidenceIDs: []string{evidence.ID}, StartedAt: lintClaimTime.Add(-time.Second), CompletedAt: lintClaimTime,
	}}
	if _, err := claims.Create(ctx, observed); err != nil {
		t.Fatal(err)
	}

	schemaDescriptor := validation.Descriptor{ID: "schema-validator", Version: "1.0.0", SupportedBindingVersions: []string{"v2"}, MaximumAssurance: knowledge.AssuranceVerification}
	if err := linter.validators.Register(lintValidator{descriptor: schemaDescriptor}); err != nil {
		t.Fatal(err)
	}
	unsupported := lintClaim("CLAIM-UNSUPPORTED-SCHEMA-1")
	unsupported.Evidence = []knowledge.Evidence{evidence}
	unsupportedBinding := knowledge.ValidationBinding{
		ID: "VAL-SCHEMA-1", ValidatorID: "schema-validator", BindingVersion: "v1", Reference: "schema",
		Required: true, RequiredAssurance: knowledge.AssuranceVerification, EvidenceIDs: []string{evidence.ID},
		InputFingerprint: lintValidationFingerprint, SnapshotFingerprint: snapshotFingerprint,
	}
	unsupportedBinding.InputFingerprint = validation.InputFingerprint(unsupported, unsupportedBinding, validation.RepositorySnapshot{Fingerprint: snapshotFingerprint})
	unsupported.ValidationPolicy = &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{unsupportedBinding}}
	if _, err := claims.Create(ctx, unsupported); err != nil {
		t.Fatal(err)
	}

	bindingDescriptor := validation.Descriptor{ID: "binding-validator", Version: "1.0.0", SupportedBindingVersions: []string{"v1"}, MaximumAssurance: knowledge.AssuranceVerification}
	if err := linter.validators.Register(lintValidator{descriptor: bindingDescriptor, bindingErr: &validation.Error{Code: "invalid_binding_schema", Message: "mode and profile are inconsistent"}}); err != nil {
		t.Fatal(err)
	}
	invalidBindingClaim := lintClaim("CLAIM-INVALID-BINDING-1")
	invalidBindingClaim.Evidence = []knowledge.Evidence{evidence}
	invalidBinding := knowledge.ValidationBinding{
		ID: "VAL-INVALID-BINDING-1", ValidatorID: "binding-validator", BindingVersion: "v1", Reference: "binding",
		Required: true, RequiredAssurance: knowledge.AssuranceVerification, EvidenceIDs: []string{evidence.ID},
		InputFingerprint: lintValidationFingerprint, SnapshotFingerprint: snapshotFingerprint,
	}
	invalidBinding.InputFingerprint = validation.InputFingerprint(invalidBindingClaim, invalidBinding, validation.RepositorySnapshot{Fingerprint: snapshotFingerprint})
	invalidBindingClaim.ValidationPolicy = &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{invalidBinding}}
	if _, err := claims.Create(ctx, invalidBindingClaim); err != nil {
		t.Fatal(err)
	}

	if err := repository.Write(ctx, "project/state.txt", []byte("changed state\n"), 0644); err != nil {
		t.Fatal(err)
	}
	report, err := linter.Build(ctx, lintClaimTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	codes := findingCodes(report)
	for _, code := range []string{
		"unknown_validator", "unsupported_binding_schema", "result_validator_mismatch",
		"result_assurance_above_validator_ceiling", "result_fingerprint_mismatch", "stale_repository_snapshot",
		"missing_required_validation_result", "invalid_binding_schema",
	} {
		if !codes[code] {
			t.Fatalf("missing finding %s: %#v", code, report.Findings)
		}
	}
}
