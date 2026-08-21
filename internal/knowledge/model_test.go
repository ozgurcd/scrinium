package knowledge

import (
	"errors"
	"testing"
	"time"
)

const testFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var testTime = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func validClaim() Claim {
	return Claim{
		SchemaVersion:     SchemaVersion,
		ID:                "AUTH-ADMIN-LOCAL-1",
		Subject:           "authentication",
		Statement:         "Administrators retain local authentication.",
		Lifecycle:         Lifecycle{State: LifecycleActive},
		Authorship:        Authorship{Kind: AuthorshipOwner, Origin: "owner:project", RecordedAt: testTime},
		Evidence:          []Evidence{},
		ValidationResults: []ValidationResult{},
		CreatedAt:         testTime,
		UpdatedAt:         testTime,
	}
}

func decisionEvidence() Evidence {
	checked := testTime
	return Evidence{
		ID: "EVD-AUTH-ADR-14", Kind: EvidenceDecision, Polarity: PolaritySupports,
		Origin:  EvidenceOrigin{Kind: OriginRepository, Reference: "adr:ADR-14"},
		Locator: "adr:ADR-14", Scope: "authentication policy", Availability: AvailabilityAvailable,
		CapturedAt: testTime, CheckedAt: &checked, DerivedFrom: []string{},
	}
}

func verificationPolicy(evidenceID string) *ValidationPolicy {
	return &ValidationPolicy{Mode: "all_required", Bindings: []ValidationBinding{{
		ID: "VAL-AUTH-ADMIN-1", ValidatorID: "test-validator", BindingVersion: "v1",
		Reference: "auth-admin", Required: true, RequiredAssurance: AssuranceVerification,
		EvidenceIDs: []string{evidenceID}, InputFingerprint: testFingerprint, RepositoryRevision: "rev-1",
	}}}
}

func validationResult(id string, outcome ValidationOutcome, assurance Assurance, completed time.Time) ValidationResult {
	return ValidationResult{
		ID: id, BindingID: "VAL-AUTH-ADMIN-1", ValidatorID: "test-validator", ValidatorVersion: "1.0.0",
		BindingVersion: "v1", RepositoryRevision: "rev-1", InputFingerprint: testFingerprint,
		Assurance: assurance, Outcome: outcome, Reason: "test result", EvidenceIDs: []string{"EVD-AUTH-ADR-14"},
		StartedAt: completed.Add(-time.Second), CompletedAt: completed,
	}
}

func TestLifecycleValidation(t *testing.T) {
	tests := []struct {
		name      string
		lifecycle Lifecycle
		wantError bool
	}{
		{name: "active", lifecycle: Lifecycle{State: LifecycleActive}},
		{name: "superseded", lifecycle: Lifecycle{State: LifecycleSuperseded, SupersededBy: "AUTH-ADMIN-LOCAL-2"}},
		{name: "withdrawn", lifecycle: Lifecycle{State: LifecycleWithdrawn, WithdrawalReason: "Owner withdrew policy"}},
		{name: "superseded without successor", lifecycle: Lifecycle{State: LifecycleSuperseded}, wantError: true},
		{name: "withdrawn without reason", lifecycle: Lifecycle{State: LifecycleWithdrawn}, wantError: true},
		{name: "active with successor", lifecycle: Lifecycle{State: LifecycleActive, SupersededBy: "AUTH-ADMIN-LOCAL-2"}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := validClaim()
			claim.Lifecycle = test.lifecycle
			err := claim.Validate()
			if (err != nil) != test.wantError {
				t.Fatalf("Validate() error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestEvidenceValidationAndLineage(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Claim)
		code   string
	}{
		{name: "valid lineage", mutate: func(claim *Claim) {
			parent := decisionEvidence()
			child := decisionEvidence()
			child.ID = "EVD-AUTH-ADR-14-SUMMARY"
			child.Origin = EvidenceOrigin{Kind: OriginLLMGenerated, Reference: "summary:ADR-14"}
			child.DerivedFrom = []string{parent.ID}
			claim.Evidence = []Evidence{parent, child}
		}},
		{name: "invalid polarity", code: "invalid_enum", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Polarity = "positive"
			claim.Evidence = []Evidence{evidence}
		}},
		{name: "broken lineage", code: "broken_evidence_reference", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.DerivedFrom = []string{"EVD-MISSING-1"}
			claim.Evidence = []Evidence{evidence}
		}},
		{name: "self support", code: "self_support", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Locator = "claim:" + claim.ID
			claim.Evidence = []Evidence{evidence}
		}},
		{name: "llm summary without lineage", code: "self_support", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Origin = EvidenceOrigin{Kind: OriginLLMGenerated, Reference: "summary:unlinked"}
			claim.Evidence = []Evidence{evidence}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := validClaim()
			test.mutate(&claim)
			err := claim.Validate()
			if test.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Code != test.code {
				t.Fatalf("expected code %s, got %v", test.code, err)
			}
		})
	}
}

func TestManualValidationAssuranceCeiling(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Claim)
	}{
		{name: "binding", mutate: func(claim *Claim) {
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			claim.ValidationPolicy.Bindings[0].ValidatorID = "manual"
		}},
		{name: "result", mutate: func(claim *Claim) {
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			claim.ValidationPolicy.Bindings[0].ValidatorID = "manual"
			claim.ValidationPolicy.Bindings[0].RequiredAssurance = AssuranceObservation
			result := validationResult("RES-AUTH-1", OutcomePass, AssuranceVerification, testTime)
			result.ValidatorID = "manual"
			claim.ValidationResults = []ValidationResult{result}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := validClaim()
			claim.Evidence = []Evidence{decisionEvidence()}
			test.mutate(&claim)
			var validationErr *ValidationError
			if err := claim.Validate(); !errors.As(err, &validationErr) || validationErr.Code != "manual_assurance_ceiling" {
				t.Fatalf("expected manual assurance ceiling, got %v", err)
			}
		})
	}
}

func TestAssessmentDerivation(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(*Claim)
		assessment Assessment
		freshness  Freshness
	}{
		{name: "asserted", mutate: func(*Claim) {}, assessment: AssessmentAsserted, freshness: FreshnessUnknown},
		{name: "sourced", mutate: func(claim *Claim) { claim.Evidence = []Evidence{decisionEvidence()} }, assessment: AssessmentSourced, freshness: FreshnessCurrent},
		{name: "context does not raise", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Polarity = PolarityContext
			claim.Evidence = []Evidence{evidence}
		}, assessment: AssessmentAsserted, freshness: FreshnessUnknown},
		{name: "observed", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Kind = EvidenceManualVerification
			claim.Evidence = []Evidence{evidence}
		}, assessment: AssessmentObserved, freshness: FreshnessCurrent},
		{name: "challenged", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Polarity = PolarityChallenges
			claim.Evidence = []Evidence{evidence}
		}, assessment: AssessmentChallenged, freshness: FreshnessCurrent},
		{name: "verified", mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			claim.ValidationResults = []ValidationResult{validationResult("RES-AUTH-1", OutcomePass, AssuranceVerification, testTime)}
		}, assessment: AssessmentVerified, freshness: FreshnessCurrent},
		{name: "proof without policy is observed", mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Kind = EvidenceValidatorProof
			claim.Evidence = []Evidence{evidence}
		}, assessment: AssessmentObserved, freshness: FreshnessCurrent},
		{name: "required fail challenges", mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			claim.ValidationResults = []ValidationResult{validationResult("RES-AUTH-FAIL", OutcomeFail, AssuranceVerification, testTime)}
		}, assessment: AssessmentChallenged, freshness: FreshnessCurrent},
		{name: "cannot evaluate is not pass", mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			claim.ValidationResults = []ValidationResult{validationResult("RES-AUTH-CANNOT", OutcomeCannotEvaluate, AssuranceVerification, testTime)}
		}, assessment: AssessmentSourced, freshness: FreshnessUnknown},
		{name: "same lineage is not corroboration", mutate: func(claim *Claim) {
			parent := decisionEvidence()
			derived := decisionEvidence()
			derived.ID = "EVD-AUTH-ADR-14-COPY"
			derived.Origin = EvidenceOrigin{Kind: OriginLLMGenerated, Reference: "summary:ADR-14"}
			derived.DerivedFrom = []string{parent.ID}
			claim.Evidence = []Evidence{parent, derived}
		}, assessment: AssessmentSourced, freshness: FreshnessCurrent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := validClaim()
			test.mutate(&claim)
			state, err := DeriveState(claim, testTime.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if state.Assessment != test.assessment || state.Freshness != test.freshness {
				t.Fatalf("state = %s/%s, want %s/%s", state.Assessment, state.Freshness, test.assessment, test.freshness)
			}
		})
	}
}

func TestValidationReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Claim)
		code   string
	}{
		{name: "binding evidence", code: "invalid_validation_binding_reference", mutate: func(claim *Claim) {
			claim.ValidationPolicy = verificationPolicy("EVD-MISSING-1")
		}},
		{name: "result binding", code: "invalid_result_binding_reference", mutate: func(claim *Claim) {
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			result := validationResult("RES-AUTH-1", OutcomePass, AssuranceVerification, testTime)
			result.BindingID = "VAL-MISSING-1"
			claim.ValidationResults = []ValidationResult{result}
		}},
		{name: "result evidence", code: "broken_evidence_reference", mutate: func(claim *Claim) {
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			result := validationResult("RES-AUTH-1", OutcomePass, AssuranceVerification, testTime)
			result.EvidenceIDs = []string{"EVD-MISSING-1"}
			claim.ValidationResults = []ValidationResult{result}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := validClaim()
			claim.Evidence = []Evidence{decisionEvidence()}
			test.mutate(&claim)
			var validationErr *ValidationError
			if err := claim.Validate(); !errors.As(err, &validationErr) || validationErr.Code != test.code {
				t.Fatalf("expected code %s, got %v", test.code, err)
			}
		})
	}
}

func TestFreshnessDegradesFromPreviousPass(t *testing.T) {
	claim := validClaim()
	claim.Evidence = []Evidence{decisionEvidence()}
	claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
	claim.ValidationResults = []ValidationResult{
		validationResult("RES-AUTH-PASS", OutcomePass, AssuranceVerification, testTime),
		validationResult("RES-AUTH-CANNOT", OutcomeCannotEvaluate, AssuranceVerification, testTime.Add(time.Minute)),
	}
	claim.UpdatedAt = testTime.Add(2 * time.Minute)
	state, err := DeriveState(claim, testTime.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if state.Assessment != AssessmentSourced || state.Freshness != FreshnessStale {
		t.Fatalf("previous pass followed by cannot_evaluate = %s/%s", state.Assessment, state.Freshness)
	}
}

func TestFreshnessChangedFingerprintMissingEvidenceAndMissingResult(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Claim)
		want   Freshness
	}{
		{name: "changed fingerprint", want: FreshnessStale, mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Fingerprint = testFingerprint
			evidence.ObservedFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			claim.Evidence = []Evidence{evidence}
		}},
		{name: "missing evidence", want: FreshnessStale, mutate: func(claim *Claim) {
			evidence := decisionEvidence()
			evidence.Availability = AvailabilityMissing
			claim.Evidence = []Evidence{evidence}
		}},
		{name: "missing validator result", want: FreshnessUnknown, mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
		}},
		{name: "changed validation input", want: FreshnessStale, mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			result := validationResult("RES-AUTH-1", OutcomePass, AssuranceVerification, testTime)
			result.InputFingerprint = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			claim.ValidationResults = []ValidationResult{result}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := validClaim()
			test.mutate(&claim)
			state, err := DeriveState(claim, testTime.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if state.Freshness != test.want {
				t.Fatalf("freshness = %s, want %s", state.Freshness, test.want)
			}
		})
	}
}
