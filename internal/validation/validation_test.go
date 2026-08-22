package validation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"scrinium/internal/knowledge"
)

const validationTestFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var validationTestTime = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

type testValidator struct {
	descriptor Descriptor
	validate   func(context.Context, Request) (knowledge.ValidationResult, error)
}

func (v *testValidator) Descriptor() Descriptor { return v.descriptor }

func (v *testValidator) Validate(ctx context.Context, req Request) (knowledge.ValidationResult, error) {
	if v.validate == nil {
		return knowledge.ValidationResult{}, nil
	}
	return v.validate(ctx, req)
}

func testDescriptor() Descriptor {
	return Descriptor{ID: "test-validator", Version: "1.2.3", SupportedBindingVersions: []string{"v2", "v1"}, MaximumAssurance: knowledge.AssuranceVerification}
}

func testValidationRequest() Request {
	evidence := knowledge.Evidence{
		ID: "EVD-PROJECT-1", Kind: knowledge.EvidenceRepositoryReference, Polarity: knowledge.PolaritySupports,
		Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "repo:project.txt"},
		Locator: "repo:project.txt", Scope: "project", Fingerprint: validationTestFingerprint,
		Availability: knowledge.AvailabilityAvailable, CapturedAt: validationTestTime, DerivedFrom: []string{},
	}
	binding := knowledge.ValidationBinding{
		ID: "VAL-PROJECT-1", ValidatorID: "test-validator", BindingVersion: "v1", Reference: "project-check",
		Required: true, RequiredAssurance: knowledge.AssuranceVerification, EvidenceIDs: []string{evidence.ID},
		InputFingerprint: validationTestFingerprint, SnapshotFingerprint: validationTestFingerprint,
	}
	claim := knowledge.Claim{
		SchemaVersion: knowledge.SchemaVersion, ID: "PROJECT-CLAIM-1", Subject: "project", Statement: "The project property holds.",
		Lifecycle:  knowledge.Lifecycle{State: knowledge.LifecycleActive},
		Authorship: knowledge.Authorship{Kind: knowledge.AuthorshipOwner, Origin: "owner:project", RecordedAt: validationTestTime},
		Evidence:   []knowledge.Evidence{evidence}, ValidationPolicy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{binding}},
		ValidationResults: []knowledge.ValidationResult{}, CreatedAt: validationTestTime, UpdatedAt: validationTestTime,
	}
	return Request{
		Claim: claim, Binding: binding,
		Repository:       RepositorySnapshot{Fingerprint: validationTestFingerprint, CapturedAt: validationTestTime},
		InputFingerprint: validationTestFingerprint, Inputs: map[string]string{"key": "value"},
		ResultID: "RESULT-PROJECT-1", StartedAt: validationTestTime,
	}
}

func testValidationResult(req Request) knowledge.ValidationResult {
	return knowledge.ValidationResult{
		ID: req.ResultID, BindingID: req.Binding.ID, ValidatorID: req.Binding.ValidatorID, ValidatorVersion: testDescriptor().Version,
		BindingVersion: req.Binding.BindingVersion, SnapshotFingerprint: req.Repository.Fingerprint, InputFingerprint: req.InputFingerprint,
		Assurance: knowledge.AssuranceVerification, Outcome: knowledge.OutcomePass, ReasonCode: "predicate_held", Reason: "predicate held",
		EvidenceIDs: []string{"EVD-PROJECT-1"}, StartedAt: req.StartedAt, CompletedAt: req.StartedAt.Add(time.Second),
	}
}

func TestRegistryRegistrationAndResolution(t *testing.T) {
	registry := NewRegistry()
	validator := &testValidator{descriptor: testDescriptor()}
	if err := registry.Register(validator); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(validator); validationErrorCode(err) != "duplicate_validator" {
		t.Fatalf("duplicate registration error = %v", err)
	}
	resolved, descriptor, exists := registry.Resolve("test-validator")
	if !exists || resolved != validator || descriptor.SupportedBindingVersions[0] != "v1" {
		t.Fatalf("unexpected deterministic resolution: %#v %#v %v", resolved, descriptor, exists)
	}
	if _, _, err := registry.ResolveBinding(testValidationRequest().Binding); err != nil {
		t.Fatal(err)
	}
	unsupported := testValidationRequest().Binding
	unsupported.BindingVersion = "v99"
	if _, _, err := registry.ResolveBinding(unsupported); validationErrorCode(err) != "unsupported_binding_version" {
		t.Fatalf("unsupported binding error = %v", err)
	}
	if _, _, err := registry.ResolveBinding(knowledge.ValidationBinding{ValidatorID: "absent", BindingVersion: "v1"}); validationErrorCode(err) != "validator_unavailable" {
		t.Fatalf("absent validator error = %v", err)
	}
}

func TestDescriptorValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Descriptor)
		code   string
	}{
		{name: "valid", mutate: func(*Descriptor) {}},
		{name: "invalid ID", code: "invalid_validator_id", mutate: func(d *Descriptor) { d.ID = "Bad ID" }},
		{name: "missing version", code: "invalid_validator_version", mutate: func(d *Descriptor) { d.Version = "" }},
		{name: "missing binding versions", code: "missing_binding_versions", mutate: func(d *Descriptor) { d.SupportedBindingVersions = nil }},
		{name: "duplicate binding version", code: "duplicate_binding_version", mutate: func(d *Descriptor) { d.SupportedBindingVersions = []string{"v1", "v1"} }},
		{name: "invalid assurance", code: "invalid_assurance", mutate: func(d *Descriptor) { d.MaximumAssurance = "absolute" }},
		{name: "manual ceiling", code: "manual_assurance_ceiling", mutate: func(d *Descriptor) { d.ID = "manual" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor := testDescriptor()
			test.mutate(&descriptor)
			err := descriptor.Validate()
			if validationErrorCode(err) != test.code {
				t.Fatalf("error code = %q, want %q (%v)", validationErrorCode(err), test.code, err)
			}
		})
	}
}

func TestResultAuthenticity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Descriptor, *Request, *knowledge.ValidationResult)
		code   string
	}{
		{name: "valid", mutate: func(*Descriptor, *Request, *knowledge.ValidationResult) {}},
		{name: "binding mismatch", code: "result_binding_mismatch", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) { result.BindingID = "VAL-OTHER-1" }},
		{name: "validator mismatch", code: "result_validator_mismatch", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) {
			result.ValidatorID = "other-validator"
		}},
		{name: "validator version mismatch", code: "result_validator_version_mismatch", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) { result.ValidatorVersion = "9.9.9" }},
		{name: "assurance ceiling", code: "assurance_above_ceiling", mutate: func(descriptor *Descriptor, _ *Request, _ *knowledge.ValidationResult) {
			descriptor.MaximumAssurance = knowledge.AssuranceObservation
		}},
		{name: "snapshot mismatch", code: "result_repository_snapshot_mismatch", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) {
			result.SnapshotFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{name: "repository revision mismatch", code: "result_repository_snapshot_mismatch", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) {
			result.RepositoryRevision = "other-revision"
		}},
		{name: "input mismatch", code: "result_input_fingerprint_mismatch", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) {
			result.InputFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{name: "future timestamp", code: "invalid_result_timestamps", mutate: func(_ *Descriptor, req *Request, result *knowledge.ValidationResult) {
			result.CompletedAt = req.StartedAt.Add(3 * time.Second)
		}},
		{name: "invalid outcome", code: "invalid_outcome", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) { result.Outcome = "maybe" }},
		{name: "missing structured reason", code: "incomplete_result_reason", mutate: func(_ *Descriptor, _ *Request, result *knowledge.ValidationResult) { result.ReasonCode = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			descriptor := testDescriptor()
			req := testValidationRequest()
			result := testValidationResult(req)
			test.mutate(&descriptor, &req, &result)
			err := ValidateResult(descriptor, req, result, req.StartedAt.Add(2*time.Second))
			if validationErrorCode(err) != test.code {
				t.Fatalf("error code = %q, want %q (%v)", validationErrorCode(err), test.code, err)
			}
		})
	}
}

func TestExecuteProvidesIsolatedRequest(t *testing.T) {
	req := testValidationRequest()
	validator := &testValidator{descriptor: testDescriptor(), validate: func(_ context.Context, isolated Request) (knowledge.ValidationResult, error) {
		isolated.Claim.Statement = "mutated"
		isolated.Claim.Evidence[0].DerivedFrom = append(isolated.Claim.Evidence[0].DerivedFrom, "EVD-MUTATED-1")
		isolated.Binding.Parameters = map[string]string{"mutated": "true"}
		isolated.Inputs["key"] = "mutated"
		return testValidationResult(req), nil
	}}
	if _, err := Execute(context.Background(), validator, req); err != nil {
		t.Fatal(err)
	}
	if req.Claim.Statement == "mutated" || len(req.Claim.Evidence[0].DerivedFrom) != 0 || req.Binding.Parameters != nil || req.Inputs["key"] != "value" {
		t.Fatalf("validator mutated orchestration-owned input: %#v", req)
	}
}

type testRepository struct {
	fingerprints map[string]string
	err          error
}

func (r testRepository) Fingerprint(ctx context.Context, path string) (bool, string, error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	if r.err != nil {
		return false, "", r.err
	}
	fingerprint, exists := r.fingerprints[path]
	return exists, fingerprint, nil
}

func TestScopedRepositorySnapshotAndInputFingerprint(t *testing.T) {
	req := testValidationRequest()
	expected := RepositoryFingerprint([]RepositoryEntry{{Path: "project.txt", Fingerprint: validationTestFingerprint}})
	req.Binding.SnapshotFingerprint = expected
	snapshotter := NewSnapshotter(testRepository{fingerprints: map[string]string{"project.txt": validationTestFingerprint}}, nil)
	snapshot, err := snapshotter.Build(context.Background(), req.Claim, req.Binding)
	if err != nil || snapshot.Fingerprint != expected {
		t.Fatalf("snapshot = %#v, err %v", snapshot, err)
	}
	first := InputFingerprint(req.Claim, req.Binding, snapshot)
	second := InputFingerprint(req.Claim, req.Binding, snapshot)
	changed := req.Claim
	changed.Statement = "Changed meaning."
	if first != second || first == InputFingerprint(changed, req.Binding, snapshot) {
		t.Fatalf("input fingerprint is not deterministic and content-sensitive")
	}

	tests := []struct {
		name       string
		repository testRepository
		mutate     func(*Request)
		code       string
	}{
		{name: "missing file", repository: testRepository{fingerprints: map[string]string{}}, mutate: func(*Request) {}, code: "missing_repository_state"},
		{name: "changed evidence fingerprint", repository: testRepository{fingerprints: map[string]string{"project.txt": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}, mutate: func(*Request) {}, code: "stale_repository_snapshot"},
		{name: "no repository evidence", repository: testRepository{fingerprints: map[string]string{}}, mutate: func(request *Request) { request.Binding.EvidenceIDs = nil }, code: "repository_snapshot_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := req
			test.mutate(&request)
			_, err := NewSnapshotter(test.repository, nil).Build(context.Background(), request.Claim, request.Binding)
			if validationErrorCode(err) != test.code {
				t.Fatalf("error code = %q, want %q (%v)", validationErrorCode(err), test.code, err)
			}
		})
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := snapshotter.Build(canceled, req.Claim, req.Binding); !errors.Is(err, context.Canceled) {
		t.Fatalf("snapshot cancellation = %v", err)
	}
}

func TestManualValidatorUsesObservationCeiling(t *testing.T) {
	req := testValidationRequest()
	req.Binding.ValidatorID = ManualValidatorID
	req.Binding.BindingVersion = ManualBindingVersion
	req.Binding.RequiredAssurance = knowledge.AssuranceObservation
	req.Inputs = map[string]string{ManualOutcomeInput: string(knowledge.OutcomePass), ManualReasonInput: "owner observed behavior"}
	validator := NewManualValidator()
	result, err := validator.Validate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	finished := time.Now().UTC().Add(time.Second)
	if result.Assurance != knowledge.AssuranceObservation || result.Outcome != knowledge.OutcomePass {
		t.Fatalf("unexpected manual result: %#v", result)
	}
	if err := ValidateResult(validator.Descriptor(), req, result, finished); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Reason, "owner observed") {
		t.Fatalf("manual reason not retained: %#v", result)
	}
}

func validationErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var validationErr *Error
	if errors.As(err, &validationErr) {
		return validationErr.Code
	}
	return "other"
}
