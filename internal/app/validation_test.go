package app

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/validation"
)

type fakeValidationBehavior struct {
	outcome   knowledge.ValidationOutcome
	assurance knowledge.Assurance
	err       error
	wait      bool
	block     <-chan struct{}
	mutate    func(*knowledge.ValidationResult)
}

type fakeValidator struct {
	descriptor validation.Descriptor
	mu         sync.Mutex
	behaviors  []fakeValidationBehavior
	calls      int
	started    chan struct{}
}

func (v *fakeValidator) Descriptor() validation.Descriptor { return v.descriptor }

func (v *fakeValidator) Validate(ctx context.Context, req validation.Request) (knowledge.ValidationResult, error) {
	v.mu.Lock()
	index := v.calls
	v.calls++
	behavior := v.behaviors[len(v.behaviors)-1]
	if index < len(v.behaviors) {
		behavior = v.behaviors[index]
	}
	started := v.started
	v.mu.Unlock()
	if started != nil {
		select {
		case started <- struct{}{}:
		default:
		}
	}
	if behavior.wait {
		<-ctx.Done()
		return knowledge.ValidationResult{}, ctx.Err()
	}
	if behavior.block != nil {
		select {
		case <-behavior.block:
		case <-ctx.Done():
			return knowledge.ValidationResult{}, ctx.Err()
		}
	}
	if behavior.err != nil {
		return knowledge.ValidationResult{}, behavior.err
	}
	completed := time.Now().UTC()
	result := knowledge.ValidationResult{
		ID: req.ResultID, BindingID: req.Binding.ID, ValidatorID: v.descriptor.ID, ValidatorVersion: v.descriptor.Version,
		BindingVersion: req.Binding.BindingVersion, RepositoryRevision: req.Repository.Revision,
		SnapshotFingerprint: req.Repository.Fingerprint, InputFingerprint: req.InputFingerprint,
		Assurance: behavior.assurance, Outcome: behavior.outcome, ReasonCode: "fake_deterministic_result", Reason: "deterministic test result",
		EvidenceIDs: append([]string(nil), req.Binding.EvidenceIDs...), StartedAt: req.StartedAt, CompletedAt: completed,
	}
	if req.Binding.ValidForSeconds > 0 {
		validUntil := completed.Add(time.Duration(req.Binding.ValidForSeconds) * time.Second)
		result.ValidUntil = &validUntil
	}
	if behavior.mutate != nil {
		behavior.mutate(&result)
	}
	return result, nil
}

func fakeDescriptor(id string, maximum knowledge.Assurance, versions ...string) validation.Descriptor {
	return validation.Descriptor{ID: id, Version: "1.0.0", SupportedBindingVersions: versions, MaximumAssurance: maximum}
}

type validationFixture struct {
	service   *Service
	ctx       context.Context
	sessionID string
	view      ClaimView
	binding   knowledge.ValidationBinding
}

func prepareValidationClaim(t *testing.T, validatorID, bindingVersion string, required bool, assurance knowledge.Assurance) *validationFixture {
	t.Helper()
	service, ctx, sessionID := prepareClaimService(t)
	if err := service.repository.Write(ctx, "project/state.txt", []byte("expected state\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, fingerprint, err := service.repository.Fingerprint(ctx, "project/state.txt")
	if err != nil {
		t.Fatal(err)
	}
	view := createApplicationClaim(t, service, ctx, sessionID, "PROJECT-STATE-1")
	checked := view.Claim.CreatedAt
	evidence := knowledge.Evidence{
		ID: "EVD-PROJECT-STATE-1", Kind: knowledge.EvidenceRepositoryReference, Polarity: knowledge.PolaritySupports,
		Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "repo:project/state.txt"},
		Locator: "repo:project/state.txt", Scope: "project state", Fingerprint: fingerprint,
		Availability: knowledge.AvailabilityAvailable, ObservedFingerprint: fingerprint,
		CapturedAt: view.Claim.CreatedAt, CheckedAt: &checked, DerivedFrom: []string{},
	}
	view, err = service.AttachEvidence(ctx, AttachEvidenceRequest{SessionID: sessionID, ClaimID: view.Claim.ID, Evidence: evidence, ExpectedRevision: view.Revision})
	if err != nil {
		t.Fatal(err)
	}
	snapshotFingerprint := validation.RepositoryFingerprint([]validation.RepositoryEntry{{Path: "project/state.txt", Fingerprint: fingerprint}})
	binding := knowledge.ValidationBinding{
		ID: "VAL-PROJECT-STATE-1", ValidatorID: validatorID, BindingVersion: bindingVersion, Reference: "project-state",
		Required: required, RequiredAssurance: assurance, EvidenceIDs: []string{evidence.ID},
		InputFingerprint: appTestFingerprint, SnapshotFingerprint: snapshotFingerprint,
	}
	snapshot := validation.RepositorySnapshot{Fingerprint: snapshotFingerprint}
	binding.InputFingerprint = validation.InputFingerprint(view.Claim, binding, snapshot)
	view, err = service.SetValidationPolicy(ctx, SetValidationPolicyRequest{
		SessionID: sessionID, ClaimID: view.Claim.ID, Policy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{binding}}, ExpectedRevision: view.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	return &validationFixture{service: service, ctx: ctx, sessionID: sessionID, view: view, binding: binding}
}

func TestValidationOrchestrationOutcomes(t *testing.T) {
	tests := []struct {
		name       string
		outcome    knowledge.ValidationOutcome
		assessment knowledge.Assessment
		freshness  knowledge.Freshness
	}{
		{name: "pass", outcome: knowledge.OutcomePass, assessment: knowledge.AssessmentVerified, freshness: knowledge.FreshnessCurrent},
		{name: "fail", outcome: knowledge.OutcomeFail, assessment: knowledge.AssessmentChallenged, freshness: knowledge.FreshnessCurrent},
		{name: "cannot evaluate", outcome: knowledge.OutcomeCannotEvaluate, assessment: knowledge.AssessmentSourced, freshness: knowledge.FreshnessUnknown},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
			service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
			validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{outcome: test.outcome, assurance: knowledge.AssuranceVerification}}}
			if err := service.RegisterValidator(validator); err != nil {
				t.Fatal(err)
			}
			attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if attempt.Result.Outcome != test.outcome || attempt.View.State.Assessment != test.assessment || attempt.View.State.Freshness != test.freshness || len(attempt.View.Claim.ValidationResults) != 1 {
				t.Fatalf("attempt = %#v", attempt)
			}
		})
	}
}

func TestValidationObservationAndAssuranceCeiling(t *testing.T) {
	tests := []struct {
		name       string
		returned   knowledge.Assurance
		outcome    knowledge.ValidationOutcome
		code       string
		assessment knowledge.Assessment
	}{
		{name: "observation pass", returned: knowledge.AssuranceObservation, outcome: knowledge.OutcomePass, assessment: knowledge.AssessmentObserved},
		{name: "ceiling violation", returned: knowledge.AssuranceVerification, outcome: knowledge.OutcomeCannotEvaluate, code: "assurance_above_ceiling", assessment: knowledge.AssessmentSourced},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareValidationClaim(t, "observer", "v1", true, knowledge.AssuranceObservation)
			service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
			validator := &fakeValidator{descriptor: fakeDescriptor("observer", knowledge.AssuranceObservation, "v1"), behaviors: []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: test.returned}}}
			if err := service.RegisterValidator(validator); err != nil {
				t.Fatal(err)
			}
			attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if attempt.Result.Outcome != test.outcome || (test.code != "" && attempt.Result.ReasonCode != test.code) || attempt.View.State.Assessment != test.assessment {
				t.Fatalf("attempt = %#v", attempt)
			}
		})
	}
}

func TestMissingAndUnsupportedValidatorsRecordCannotEvaluate(t *testing.T) {
	tests := []struct {
		name       string
		register   *fakeValidator
		reasonCode string
	}{
		{name: "missing", reasonCode: "validator_unavailable"},
		{name: "unsupported binding", register: &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v2"), behaviors: []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification}}}, reasonCode: "unsupported_binding_version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
			service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
			if test.register != nil {
				if err := service.RegisterValidator(test.register); err != nil {
					t.Fatal(err)
				}
			}
			attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
			if err != nil {
				t.Fatal(err)
			}
			if attempt.Result.Outcome != knowledge.OutcomeCannotEvaluate || attempt.Result.ReasonCode != test.reasonCode {
				t.Fatalf("attempt = %#v", attempt)
			}
		})
	}
}

func TestValidationTimeoutCancellationAndError(t *testing.T) {
	t.Run("timeout", func(t *testing.T) {
		fixture := prepareValidationClaim(t, "slow-validator", "v1", true, knowledge.AssuranceVerification)
		service, view, binding := fixture.service, fixture.view, fixture.binding
		validator := &fakeValidator{descriptor: fakeDescriptor("slow-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{wait: true}}}
		if err := service.RegisterValidator(validator); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()
		attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
		if err != nil || attempt.Result.ReasonCode != "deadline_exceeded" || attempt.Result.Outcome != knowledge.OutcomeCannotEvaluate {
			t.Fatalf("timeout attempt = %#v, err %v", attempt, err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		fixture := prepareValidationClaim(t, "slow-validator", "v1", true, knowledge.AssuranceVerification)
		service, view, binding := fixture.service, fixture.view, fixture.binding
		started := make(chan struct{}, 1)
		validator := &fakeValidator{descriptor: fakeDescriptor("slow-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{wait: true}}, started: started}
		if err := service.RegisterValidator(validator); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan ValidationAttempt, 1)
		errs := make(chan error, 1)
		go func() {
			attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
			result <- attempt
			errs <- err
		}()
		<-started
		cancel()
		attempt := <-result
		if err := <-errs; err != nil || attempt.Result.ReasonCode != "context_canceled" {
			t.Fatalf("canceled attempt = %#v, err %v", attempt, err)
		}
	})

	t.Run("validator error", func(t *testing.T) {
		fixture := prepareValidationClaim(t, "error-validator", "v1", true, knowledge.AssuranceVerification)
		service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
		validator := &fakeValidator{descriptor: fakeDescriptor("error-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{err: errors.New("setup failed")}}}
		if err := service.RegisterValidator(validator); err != nil {
			t.Fatal(err)
		}
		attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
		if err != nil || attempt.Result.ReasonCode != "validator_error" || attempt.Result.Outcome != knowledge.OutcomeCannotEvaluate {
			t.Fatalf("error attempt = %#v, err %v", attempt, err)
		}
	})
}

func TestValidationAuthenticityDowngrades(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*knowledge.ValidationResult)
		code   string
	}{
		{name: "binding mismatch", code: "result_binding_mismatch", mutate: func(result *knowledge.ValidationResult) { result.BindingID = "VAL-OTHER-1" }},
		{name: "validator identity mismatch", code: "result_validator_mismatch", mutate: func(result *knowledge.ValidationResult) { result.ValidatorID = "other-validator" }},
		{name: "repository mismatch", code: "result_repository_snapshot_mismatch", mutate: func(result *knowledge.ValidationResult) { result.SnapshotFingerprint = appTestFingerprint }},
		{name: "input mismatch", code: "result_input_fingerprint_mismatch", mutate: func(result *knowledge.ValidationResult) { result.InputFingerprint = appTestFingerprint }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
			service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
			validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification, mutate: test.mutate}}}
			if err := service.RegisterValidator(validator); err != nil {
				t.Fatal(err)
			}
			attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
			if err != nil || attempt.Result.Outcome != knowledge.OutcomeCannotEvaluate || attempt.Result.ReasonCode != test.code {
				t.Fatalf("attempt = %#v, err %v", attempt, err)
			}
		})
	}
}

func TestValidationRejectsStaleRepositoryAndBindingInput(t *testing.T) {
	t.Run("repository snapshot", func(t *testing.T) {
		fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
		service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
		validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification}}}
		if err := service.RegisterValidator(validator); err != nil {
			t.Fatal(err)
		}
		if err := service.repository.Write(ctx, "project/state.txt", []byte("changed state\n"), 0644); err != nil {
			t.Fatal(err)
		}
		attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
		if err != nil || attempt.Result.Outcome != knowledge.OutcomeCannotEvaluate || attempt.Result.ReasonCode != "stale_repository_snapshot" || attempt.View.State.Freshness != knowledge.FreshnessStale {
			t.Fatalf("stale repository attempt = %#v, err %v", attempt, err)
		}
	})

	t.Run("binding input", func(t *testing.T) {
		fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
		service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
		validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification}}}
		if err := service.RegisterValidator(validator); err != nil {
			t.Fatal(err)
		}
		binding.InputFingerprint = appTestFingerprint
		view, err := service.SetValidationPolicy(ctx, SetValidationPolicyRequest{
			SessionID: fixture.sessionID, ClaimID: view.Claim.ID, Policy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{binding}}, ExpectedRevision: view.Revision,
		})
		if err != nil {
			t.Fatal(err)
		}
		attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
		if err != nil || attempt.Result.Outcome != knowledge.OutcomeCannotEvaluate || attempt.Result.ReasonCode != "stale_validator_input" || attempt.View.State.Freshness != knowledge.FreshnessStale {
			t.Fatalf("stale input attempt = %#v, err %v", attempt, err)
		}
	})
}

func TestRevalidationHistoryAndDerivedState(t *testing.T) {
	tests := []struct {
		name       string
		outcomes   []knowledge.ValidationOutcome
		assessment knowledge.Assessment
		freshness  knowledge.Freshness
	}{
		{name: "fresh pass", outcomes: []knowledge.ValidationOutcome{knowledge.OutcomePass, knowledge.OutcomePass}, assessment: knowledge.AssessmentVerified, freshness: knowledge.FreshnessCurrent},
		{name: "pass becomes stale", outcomes: []knowledge.ValidationOutcome{knowledge.OutcomePass, knowledge.OutcomeCannotEvaluate}, assessment: knowledge.AssessmentSourced, freshness: knowledge.FreshnessStale},
		{name: "previous fail recovers", outcomes: []knowledge.ValidationOutcome{knowledge.OutcomeFail, knowledge.OutcomePass}, assessment: knowledge.AssessmentVerified, freshness: knowledge.FreshnessCurrent},
		{name: "previous cannot recovers", outcomes: []knowledge.ValidationOutcome{knowledge.OutcomeCannotEvaluate, knowledge.OutcomePass}, assessment: knowledge.AssessmentVerified, freshness: knowledge.FreshnessCurrent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
			service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
			behaviors := make([]fakeValidationBehavior, 0, len(test.outcomes))
			for _, outcome := range test.outcomes {
				behaviors = append(behaviors, fakeValidationBehavior{outcome: outcome, assurance: knowledge.AssuranceVerification})
			}
			validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: behaviors}
			if err := service.RegisterValidator(validator); err != nil {
				t.Fatal(err)
			}
			var attempt ValidationAttempt
			var err error
			for range test.outcomes {
				attempt, err = service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
				if err != nil {
					t.Fatal(err)
				}
				view = attempt.View
			}
			if len(attempt.View.Claim.ValidationResults) != len(test.outcomes) || attempt.View.State.Assessment != test.assessment || attempt.View.State.Freshness != test.freshness {
				t.Fatalf("attempt = %#v", attempt)
			}
		})
	}
}

func TestValidationHistoryRetainsExternalMetadataAndDiagnostics(t *testing.T) {
	fixture := prepareValidationClaim(t, "external-validator", "v1", true, knowledge.AssuranceVerification)
	validator := &fakeValidator{
		descriptor: fakeDescriptor("external-validator", knowledge.AssuranceVerification, "v1"),
		behaviors: []fakeValidationBehavior{{
			outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification,
			mutate: func(result *knowledge.ValidationResult) {
				result.Metadata = map[string]string{"external.version": "1.2.3", "external.fingerprint": appTestFingerprint}
				result.Diagnostics = []knowledge.ValidationDiagnostic{{Code: "external_pass", Message: "selected invariant passed", Target: "RULE-1"}}
			},
		}},
	}
	if err := fixture.service.RegisterValidator(validator); err != nil {
		t.Fatal(err)
	}
	attempt, err := fixture.service.ValidateClaimBinding(fixture.ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: fixture.view.Claim.ID, BindingID: fixture.binding.ID, ExpectedRevision: fixture.view.Revision})
	if err != nil {
		t.Fatal(err)
	}
	stored := attempt.View.Claim.ValidationResults
	if len(stored) != 1 || stored[0].Metadata["external.version"] != "1.2.3" || len(stored[0].Diagnostics) != 1 || stored[0].Diagnostics[0].Target != "RULE-1" {
		t.Fatalf("external validation details were not retained: %#v", stored)
	}
}

func TestRequiredAndOptionalBindingOrchestration(t *testing.T) {
	fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
	service, ctx, view, first := fixture.service, fixture.ctx, fixture.view, fixture.binding
	second := first
	second.ID = "VAL-PROJECT-STATE-2"
	second.Reference = "project-state-secondary"
	second.InputFingerprint = validation.InputFingerprint(view.Claim, second, validation.RepositorySnapshot{Fingerprint: second.SnapshotFingerprint})
	optional := first
	optional.ID = "VAL-PROJECT-STATE-OPTIONAL"
	optional.Reference = "project-state-optional"
	optional.Required = false
	optional.InputFingerprint = validation.InputFingerprint(view.Claim, optional, validation.RepositorySnapshot{Fingerprint: optional.SnapshotFingerprint})
	view, err := service.SetValidationPolicy(ctx, SetValidationPolicyRequest{
		SessionID:        fixture.sessionID,
		ClaimID:          view.Claim.ID,
		Policy:           &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{first, second, optional}},
		ExpectedRevision: view.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{
		{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification},
		{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification},
		{outcome: knowledge.OutcomeFail, assurance: knowledge.AssuranceVerification},
	}}
	if err := service.RegisterValidator(validator); err != nil {
		t.Fatal(err)
	}
	run, err := service.ValidateRequiredClaimBindings(ctx, ValidateRequiredBindingsRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, ExpectedRevision: view.Revision})
	if err != nil || len(run.Results) != 2 || run.View.State.Assessment != knowledge.AssessmentVerified {
		t.Fatalf("required run = %#v, err %v", run, err)
	}
	attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: optional.ID, ExpectedRevision: run.View.Revision})
	if err != nil || attempt.Result.Outcome != knowledge.OutcomeFail || attempt.View.State.Assessment != knowledge.AssessmentVerified || len(attempt.View.Claim.ValidationResults) != 3 {
		t.Fatalf("optional failure = %#v, err %v", attempt, err)
	}
}

func TestManualValidationUsesGenericInfrastructure(t *testing.T) {
	fixture := prepareValidationClaim(t, validation.ManualValidatorID, validation.ManualBindingVersion, true, knowledge.AssuranceObservation)
	service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
	attempt, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{
		SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision,
		Inputs: map[string]string{validation.ManualOutcomeInput: string(knowledge.OutcomePass), validation.ManualReasonInput: "owner observed expected behavior"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempt.Result.ValidatorID != validation.ManualValidatorID || attempt.Result.Assurance != knowledge.AssuranceObservation || attempt.View.State.Assessment != knowledge.AssessmentObserved {
		t.Fatalf("manual attempt = %#v", attempt)
	}
}

func TestConcurrentValidationAttemptsRejectStaleResultWithoutLosingHistory(t *testing.T) {
	fixture := prepareValidationClaim(t, "test-validator", "v1", true, knowledge.AssuranceVerification)
	service, ctx, view, binding := fixture.service, fixture.ctx, fixture.view, fixture.binding
	validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification}}}
	if err := service.RegisterValidator(validator); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := service.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{SessionID: fixture.sessionID, ClaimID: view.Claim.ID, BindingID: binding.ID, ExpectedRevision: view.Revision})
			errs <- err
		}()
	}
	wait.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
			continue
		}
		var conflict *ClaimConflictError
		if errors.As(err, &conflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	stored, err := service.GetClaim(ctx, view.Claim.ID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Claim.ValidationResults) != 1 || stored.State.Assessment != knowledge.AssessmentVerified {
		t.Fatalf("concurrent results = %#v", stored)
	}
}

func TestValidationResultConflictsWhenClaimChangesDuringExecution(t *testing.T) {
	tests := []struct {
		validatorID    string
		bindingVersion string
	}{
		{validatorID: "rulefloor", bindingVersion: "rulefloor.binding.v1"},
		{validatorID: "gograph", bindingVersion: "gograph.binding.v1"},
	}
	for _, test := range tests {
		t.Run(test.validatorID, func(t *testing.T) {
			fixture := prepareValidationClaim(t, test.validatorID, test.bindingVersion, true, knowledge.AssuranceObservation)
			started := make(chan struct{}, 1)
			release := make(chan struct{})
			validator := &fakeValidator{
				descriptor: fakeDescriptor(test.validatorID, knowledge.AssuranceObservation, test.bindingVersion),
				behaviors:  []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceObservation, block: release}},
				started:    started,
			}
			if err := fixture.service.RegisterValidator(validator); err != nil {
				t.Fatal(err)
			}
			attempts := make(chan ValidationAttempt, 1)
			errs := make(chan error, 1)
			go func() {
				attempt, err := fixture.service.ValidateClaimBinding(fixture.ctx, ValidateClaimBindingRequest{
					SessionID: fixture.sessionID, ClaimID: fixture.view.Claim.ID, BindingID: fixture.binding.ID, ExpectedRevision: fixture.view.Revision,
				})
				attempts <- attempt
				errs <- err
			}()
			<-started
			subject := "changed-during-validation"
			changed, err := fixture.service.UpdateClaim(fixture.ctx, UpdateClaimRequest{
				SessionID: fixture.sessionID, ID: fixture.view.Claim.ID, Subject: &subject, MeaningUnchanged: true, ExpectedRevision: fixture.view.Revision,
			})
			if err != nil {
				t.Fatal(err)
			}
			close(release)
			attempt := <-attempts
			err = <-errs
			var conflict *ClaimConflictError
			if !errors.As(err, &conflict) || conflict.CurrentRevision != changed.Revision || attempt.Result.ID != "" {
				t.Fatalf("expected obsolete %s result conflict, attempt=%#v err=%v", test.validatorID, attempt, err)
			}
			stored, err := fixture.service.GetClaim(fixture.ctx, fixture.view.Claim.ID, time.Now().UTC())
			if err != nil {
				t.Fatal(err)
			}
			if len(stored.Claim.ValidationResults) != 0 || stored.Revision != changed.Revision {
				t.Fatalf("obsolete result was attached: %#v", stored)
			}
		})
	}
}

func TestValidatorInspectionAndDuplicateRegistration(t *testing.T) {
	service := newTestService(t, nil)
	validator := &fakeValidator{descriptor: fakeDescriptor("test-validator", knowledge.AssuranceVerification, "v1"), behaviors: []fakeValidationBehavior{{outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification}}}
	if err := service.RegisterValidator(validator); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterValidator(validator); err == nil {
		t.Fatal("expected duplicate validator rejection")
	}
	descriptors := service.AvailableValidators()
	if len(descriptors) != 2 || descriptors[0].ID != validation.ManualValidatorID || descriptors[1].ID != "test-validator" {
		t.Fatalf("descriptors = %#v", descriptors)
	}
	if descriptor, err := service.ValidatorDescriptor("test-validator"); err != nil || descriptor.Version != "1.0.0" {
		t.Fatalf("descriptor = %#v, err %v", descriptor, err)
	}
}
