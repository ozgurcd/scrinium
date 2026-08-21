package validation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"scrinium/internal/knowledge"
)

const (
	ManualValidatorID    = "manual"
	ManualBindingVersion = "v1"
	ManualOutcomeInput   = "outcome"
	ManualReasonInput    = "reason"
	ManualReasonCode     = "manual_observation"
)

type ManualValidator struct{}

func NewManualValidator() *ManualValidator { return &ManualValidator{} }

func (*ManualValidator) Descriptor() Descriptor {
	return Descriptor{
		ID:                       ManualValidatorID,
		Version:                  "1.0.0",
		SupportedBindingVersions: []string{ManualBindingVersion},
		MaximumAssurance:         knowledge.AssuranceObservation,
	}
}

func (v *ManualValidator) Validate(ctx context.Context, req Request) (knowledge.ValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return knowledge.ValidationResult{}, err
	}
	outcome := knowledge.ValidationOutcome(req.Inputs[ManualOutcomeInput])
	switch outcome {
	case knowledge.OutcomePass, knowledge.OutcomeFail, knowledge.OutcomeCannotEvaluate:
	default:
		return knowledge.ValidationResult{}, fmt.Errorf("manual outcome must be pass, fail, or cannot_evaluate")
	}
	reason := strings.TrimSpace(req.Inputs[ManualReasonInput])
	if reason == "" {
		return knowledge.ValidationResult{}, fmt.Errorf("manual validation reason is required")
	}
	completed := time.Now().UTC()
	result := knowledge.ValidationResult{
		ID:                  req.ResultID,
		BindingID:           req.Binding.ID,
		ValidatorID:         v.Descriptor().ID,
		ValidatorVersion:    v.Descriptor().Version,
		BindingVersion:      req.Binding.BindingVersion,
		RepositoryRevision:  req.Repository.Revision,
		SnapshotFingerprint: req.Repository.Fingerprint,
		InputFingerprint:    req.InputFingerprint,
		Assurance:           knowledge.AssuranceObservation,
		Outcome:             outcome,
		ReasonCode:          ManualReasonCode,
		Reason:              reason,
		EvidenceIDs:         append([]string(nil), req.Binding.EvidenceIDs...),
		StartedAt:           req.StartedAt,
		CompletedAt:         completed,
	}
	if req.Binding.ValidForSeconds > 0 {
		validUntil := completed.Add(time.Duration(req.Binding.ValidForSeconds) * time.Second)
		result.ValidUntil = &validUntil
	}
	return result, nil
}
