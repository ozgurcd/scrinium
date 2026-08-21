package app

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"sync/atomic"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/session"
	"scrinium/internal/store"
	"scrinium/internal/validation"
)

var validationResultSequence atomic.Uint64

type ValidateClaimBindingRequest struct {
	SessionID        string
	ClaimID          string
	BindingID        string
	Inputs           map[string]string
	ExpectedRevision ClaimRevision
}

type ValidationAttempt struct {
	Result knowledge.ValidationResult `json:"result"`
	View   ClaimView                  `json:"view"`
}

type ValidateRequiredBindingsRequest struct {
	SessionID        string
	ClaimID          string
	Inputs           map[string]map[string]string
	ExpectedRevision ClaimRevision
}

type RequiredValidationRun struct {
	Results []knowledge.ValidationResult `json:"results"`
	View    ClaimView                    `json:"view"`
}

type ValidatorStatus struct {
	ID             string                `json:"id"`
	Optional       bool                  `json:"optional"`
	Available      bool                  `json:"available"`
	Reason         string                `json:"reason,omitempty"`
	Descriptor     validation.Descriptor `json:"descriptor,omitempty"`
	BindingSchemas []string              `json:"binding_schemas"`
}

type validationPersistence struct {
	sessionID string
	original  knowledge.Claim
	expected  store.Revision
	binding   knowledge.ValidationBinding
	result    knowledge.ValidationResult
}

func (s *Service) RegisterValidator(validator validation.Validator) error {
	if err := s.validators.Register(validator); err != nil {
		return appError(ErrorInvalid, err.Error(), err)
	}
	descriptor := validator.Descriptor()
	s.validatorMu.Lock()
	defer s.validatorMu.Unlock()
	status := ValidatorStatus{
		ID: descriptor.ID, Available: true, Descriptor: descriptor,
		BindingSchemas: append([]string(nil), descriptor.SupportedBindingVersions...),
	}
	for index := range s.validatorInfo {
		if s.validatorInfo[index].ID == descriptor.ID {
			status.Optional = s.validatorInfo[index].Optional
			s.validatorInfo[index] = status
			return nil
		}
	}
	s.validatorInfo = append(s.validatorInfo, status)
	sort.Slice(s.validatorInfo, func(i, j int) bool { return s.validatorInfo[i].ID < s.validatorInfo[j].ID })
	return nil
}

func (s *Service) AvailableValidators() []validation.Descriptor {
	return s.validators.Descriptors()
}

func (s *Service) ValidatorStatuses() []ValidatorStatus {
	s.validatorMu.RLock()
	defer s.validatorMu.RUnlock()
	statuses := make([]ValidatorStatus, len(s.validatorInfo))
	copy(statuses, s.validatorInfo)
	for i := range statuses {
		statuses[i].BindingSchemas = append([]string(nil), statuses[i].BindingSchemas...)
		statuses[i].Descriptor.SupportedBindingVersions = append([]string(nil), statuses[i].Descriptor.SupportedBindingVersions...)
	}
	return statuses
}

func (s *Service) ValidatorDescriptor(id string) (validation.Descriptor, error) {
	descriptor, exists := s.validators.Descriptor(id)
	if !exists {
		return validation.Descriptor{}, appError(ErrorValidator, fmt.Sprintf("validator %s is not registered", id), nil)
	}
	return descriptor, nil
}

func (s *Service) ValidateClaimBinding(ctx context.Context, req ValidateClaimBindingRequest) (ValidationAttempt, error) {
	if err := s.requireClaimWrite(req.ClaimID); err != nil {
		return ValidationAttempt{}, err
	}
	if err := s.sessions.RequireReadyForWrite(ctx, req.SessionID, claimPath(req.ClaimID)); err != nil {
		return ValidationAttempt{}, translateSessionError(err)
	}
	record, err := s.claims.Get(ctx, req.ClaimID)
	if err != nil {
		return ValidationAttempt{}, translateClaimError(err)
	}
	if err := requireExpectedRevision(req.ClaimID, req.ExpectedRevision, record.Revision); err != nil {
		return ValidationAttempt{}, err
	}
	claim := record.Claim
	binding, err := claimBinding(claim, req.BindingID)
	if err != nil {
		return ValidationAttempt{}, err
	}
	started := time.Now().UTC()
	request := validation.Request{
		Claim: claim, Binding: binding, Inputs: cloneInputs(req.Inputs),
		Repository: declaredSnapshot(binding, started), InputFingerprint: binding.InputFingerprint,
		ResultID: newValidationResultID(binding.ID, started), StartedAt: started,
	}
	validator, descriptor, resolveErr := s.validators.ResolveBinding(binding)
	if resolveErr != nil {
		result := cannotEvaluateResult(request, descriptor, validationCode(resolveErr), resolveErr.Error(), started)
		return s.persistValidationResult(ctx, validationPersistence{req.SessionID, claim, record.Revision, binding, result})
	}
	snapshot, snapshotErr := s.snapshots.Build(ctx, claim, binding)
	if snapshot.Fingerprint == "" {
		snapshot = declaredSnapshot(binding, started)
	}
	request.Repository = snapshot
	request.InputFingerprint = validation.InputFingerprint(claim, binding, snapshot)
	if snapshotErr != nil {
		result := cannotEvaluateResult(request, descriptor, validationCode(snapshotErr), snapshotErr.Error(), time.Now().UTC())
		return s.persistValidationResult(ctx, validationPersistence{req.SessionID, claim, record.Revision, binding, result})
	}
	if request.InputFingerprint != binding.InputFingerprint {
		result := cannotEvaluateResult(request, descriptor, "stale_validator_input", "current validation input fingerprint does not match the binding", time.Now().UTC())
		return s.persistValidationResult(ctx, validationPersistence{req.SessionID, claim, record.Revision, binding, result})
	}
	result, executeErr := validation.Execute(ctx, validator, request)
	finished := time.Now().UTC()
	if executeErr != nil {
		code := "validator_error"
		switch {
		case errors.Is(executeErr, context.DeadlineExceeded):
			code = "deadline_exceeded"
		case errors.Is(executeErr, context.Canceled):
			code = "context_canceled"
		}
		result = cannotEvaluateResult(request, descriptor, code, executeErr.Error(), finished)
	} else if authenticErr := validation.ValidateResult(descriptor, request, result, finished); authenticErr != nil {
		result = cannotEvaluateResult(request, descriptor, validationCode(authenticErr), authenticErr.Error(), finished)
	}
	return s.persistValidationResult(ctx, validationPersistence{req.SessionID, claim, record.Revision, binding, result})
}

func (s *Service) ValidateRequiredClaimBindings(ctx context.Context, req ValidateRequiredBindingsRequest) (RequiredValidationRun, error) {
	record, err := s.claims.Get(ctx, req.ClaimID)
	if err != nil {
		return RequiredValidationRun{}, translateClaimError(err)
	}
	if err := requireExpectedRevision(req.ClaimID, req.ExpectedRevision, record.Revision); err != nil {
		return RequiredValidationRun{}, err
	}
	claim := record.Claim
	if claim.ValidationPolicy == nil {
		return RequiredValidationRun{}, appError(ErrorInvalid, "claim has no validation policy", nil)
	}
	bindingIDs := make([]string, 0)
	for _, binding := range claim.ValidationPolicy.Bindings {
		if binding.Required {
			bindingIDs = append(bindingIDs, binding.ID)
		}
	}
	run := RequiredValidationRun{Results: []knowledge.ValidationResult{}}
	expected := req.ExpectedRevision
	for _, bindingID := range bindingIDs {
		if err := ctx.Err(); err != nil {
			return run, err
		}
		attempt, err := s.ValidateClaimBinding(ctx, ValidateClaimBindingRequest{
			SessionID: req.SessionID, ClaimID: req.ClaimID, BindingID: bindingID, Inputs: req.Inputs[bindingID], ExpectedRevision: expected,
		})
		if err != nil {
			return run, err
		}
		run.Results = append(run.Results, attempt.Result)
		run.View = attempt.View
		expected = attempt.View.Revision
	}
	if len(bindingIDs) == 0 {
		view, err := s.deriveView(ctx, record, time.Now().UTC())
		return RequiredValidationRun{Results: []knowledge.ValidationResult{}, View: view}, err
	}
	return run, nil
}

func (s *Service) persistValidationResult(ctx context.Context, persistence validationPersistence) (ValidationAttempt, error) {
	persistCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		persistCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	}
	defer cancel()
	if err := s.requireClaimWrite(persistence.original.ID); err != nil {
		return ValidationAttempt{}, err
	}
	updatedAt := time.Now().UTC()
	if persistence.result.CompletedAt.After(updatedAt) {
		updatedAt = persistence.result.CompletedAt
	}
	var mutation store.ClaimMutation
	write := session.Write{Path: claimPath(persistence.original.ID), ExistedBefore: true, Claim: true}
	if err := s.sessions.DoWrite(persistCtx, persistence.sessionID, []session.Write{write}, func() (bool, error) {
		var updateErr error
		mutation, updateErr = s.claims.Update(persistCtx, persistence.original.ID, persistence.expected, func(current *knowledge.Claim) error {
			if current.Lifecycle.State != knowledge.LifecycleActive {
				return appError(ErrorConflict, "validation result rejected: claim lifecycle is not active", nil)
			}
			currentBinding, bindingErr := claimBinding(*current, persistence.binding.ID)
			if bindingErr != nil {
				return bindingErr
			}
			if !sameValidationBinding(currentBinding, persistence.binding) || current.Subject != persistence.original.Subject || current.Statement != persistence.original.Statement {
				return appError(ErrorConflict, "validation result rejected: claim or binding changed during validation", nil)
			}
			current.ValidationResults = append(current.ValidationResults, persistence.result)
			current.UpdatedAt = updatedAt
			return nil
		})
		if updateErr != nil {
			return false, translateClaimError(updateErr)
		}
		return mutation.Changed, nil
	}); err != nil {
		return ValidationAttempt{}, translateSessionError(err)
	}
	view, err := s.deriveView(ctx, mutation.Record, updatedAt)
	if err != nil {
		return ValidationAttempt{}, err
	}
	return ValidationAttempt{Result: persistence.result, View: view}, nil
}

func claimBinding(claim knowledge.Claim, id string) (knowledge.ValidationBinding, error) {
	if claim.ValidationPolicy == nil {
		return knowledge.ValidationBinding{}, appError(ErrorInvalid, "claim has no validation policy", nil)
	}
	for _, binding := range claim.ValidationPolicy.Bindings {
		if binding.ID == id {
			return binding, nil
		}
	}
	return knowledge.ValidationBinding{}, appError(ErrorInvalid, fmt.Sprintf("validation binding %s does not exist", id), nil)
}

func cannotEvaluateResult(req validation.Request, descriptor validation.Descriptor, code, reason string, completed time.Time) knowledge.ValidationResult {
	validatorVersion := descriptor.Version
	if validatorVersion == "" {
		validatorVersion = "unavailable"
	}
	if completed.Before(req.StartedAt) {
		completed = req.StartedAt
	}
	return knowledge.ValidationResult{
		ID: req.ResultID, BindingID: req.Binding.ID, ValidatorID: req.Binding.ValidatorID,
		ValidatorVersion: validatorVersion, BindingVersion: req.Binding.BindingVersion,
		RepositoryRevision: req.Repository.Revision, SnapshotFingerprint: req.Repository.Fingerprint,
		InputFingerprint: req.InputFingerprint, Assurance: knowledge.AssuranceObservation,
		Outcome: knowledge.OutcomeCannotEvaluate, ReasonCode: code, Reason: reason,
		EvidenceIDs: append([]string(nil), req.Binding.EvidenceIDs...),
		StartedAt:   req.StartedAt, CompletedAt: completed,
	}
}

func declaredSnapshot(binding knowledge.ValidationBinding, at time.Time) validation.RepositorySnapshot {
	return validation.RepositorySnapshot{Revision: binding.RepositoryRevision, Fingerprint: binding.SnapshotFingerprint, CapturedAt: at}
}

func newValidationResultID(bindingID string, at time.Time) string {
	sequence := validationResultSequence.Add(1)
	return fmt.Sprintf("RESULT-%s-%d-%d", bindingID, at.UnixNano(), sequence)
}

func validationCode(err error) string {
	var validationErr *validation.Error
	if errors.As(err, &validationErr) {
		return validationErr.Code
	}
	return "validation_setup_failed"
}

func cloneInputs(inputs map[string]string) map[string]string {
	if inputs == nil {
		return nil
	}
	copyInputs := make(map[string]string, len(inputs))
	for key, value := range inputs {
		copyInputs[key] = value
	}
	return copyInputs
}

func sameValidationBinding(left, right knowledge.ValidationBinding) bool {
	return left.ID == right.ID && left.ValidatorID == right.ValidatorID && left.BindingVersion == right.BindingVersion &&
		left.Reference == right.Reference && left.Required == right.Required && left.RequiredAssurance == right.RequiredAssurance &&
		left.InputFingerprint == right.InputFingerprint && left.RepositoryRevision == right.RepositoryRevision &&
		left.SnapshotFingerprint == right.SnapshotFingerprint && left.ValidForSeconds == right.ValidForSeconds &&
		maps.Equal(left.Parameters, right.Parameters) && slices.Equal(left.EvidenceIDs, right.EvidenceIDs)
}
