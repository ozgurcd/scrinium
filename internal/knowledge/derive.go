package knowledge

import (
	"fmt"
	"sort"
	"time"
)

type freshnessLevel uint8

const (
	levelCurrent freshnessLevel = iota
	levelUnknown
	levelStale
)

// DeriveState computes assessment and freshness without mutating the claim.
func DeriveState(claim Claim, now time.Time) (DerivedState, error) {
	if err := claim.Validate(); err != nil {
		return DerivedState{}, err
	}
	now = now.UTC()
	evidence := deriveEvidence(claim.Evidence, now)
	validation := deriveValidation(claim, evidence.byID, now)
	state := DerivedState{
		Lifecycle:     claim.Lifecycle.State,
		Assessment:    deriveAssessment(evidence, validation),
		Freshness:     deriveFreshness(evidence, validation),
		Reasons:       append(evidence.reasons, validation.reasons...),
		LastValidated: validation.lastValidated,
	}
	if state.Assessment == AssessmentVerified {
		state.Reasons = append(state.Reasons, "all required verification-grade bindings currently pass")
	}
	sort.Strings(state.Reasons)
	return state, nil
}

type evidenceDerivation struct {
	byID         map[string]Evidence
	level        freshnessLevel
	hasRelevant  bool
	hasSourced   bool
	hasObserved  bool
	hasChallenge bool
	reasons      []string
}

func deriveEvidence(items []Evidence, now time.Time) evidenceDerivation {
	derived := evidenceDerivation{
		byID:    make(map[string]Evidence, len(items)),
		level:   levelCurrent,
		reasons: []string{},
	}
	for _, evidence := range items {
		derived.byID[evidence.ID] = evidence
		if evidence.Polarity == PolarityContext {
			continue
		}
		derived.hasRelevant = true
		itemLevel := evidenceFreshness(evidence, now)
		derived.level = maxFreshness(derived.level, itemLevel)
		if itemLevel == levelStale || evidence.Availability == AvailabilityMissing {
			continue
		}
		if evidence.Polarity == PolarityChallenges && itemLevel == levelCurrent {
			derived.hasChallenge = true
			derived.reasons = append(derived.reasons, "current challenge evidence: "+evidence.ID)
			continue
		}
		if evidence.Polarity != PolaritySupports {
			continue
		}
		derived.hasSourced = true
		if itemLevel == levelCurrent && observationKind(evidence.Kind) {
			derived.hasObserved = true
		}
	}
	return derived
}

type validationDerivation struct {
	level         freshnessLevel
	hasRelevant   bool
	hasObserved   bool
	hasChallenge  bool
	verified      bool
	lastValidated *time.Time
	reasons       []string
}

func deriveValidation(claim Claim, evidenceByID map[string]Evidence, now time.Time) validationDerivation {
	derived := validationDerivation{level: levelCurrent, verified: claim.ValidationPolicy != nil, reasons: []string{}}
	if claim.ValidationPolicy == nil {
		return derived
	}

	latest := latestResults(claim.ValidationResults)
	hasRequiredVerification := false
	for _, binding := range claim.ValidationPolicy.Bindings {
		if binding.Required && binding.RequiredAssurance == AssuranceVerification {
			hasRequiredVerification = true
		}
		result, exists := latest[binding.ID]
		if !exists {
			applyMissingResult(&derived, binding)
			continue
		}
		applyValidationResult(&derived, claim.ValidationResults, binding, result, evidenceByID, now)
	}
	if !hasRequiredVerification {
		derived.verified = false
	}
	return derived
}

func applyMissingResult(derived *validationDerivation, binding ValidationBinding) {
	if !binding.Required {
		return
	}
	derived.hasRelevant = true
	derived.level = maxFreshness(derived.level, levelUnknown)
	derived.verified = false
	derived.reasons = append(derived.reasons, "missing result for required binding: "+binding.ID)
}

func applyValidationResult(derived *validationDerivation, history []ValidationResult, binding ValidationBinding, result ValidationResult, evidence map[string]Evidence, now time.Time) {
	derived.hasRelevant = true
	resultLevel := resultFreshness(binding, result, evidence, now)
	if result.Outcome == OutcomeCannotEvaluate {
		if previousPass(history, binding.ID, result.ID) {
			resultLevel = levelStale
		} else {
			resultLevel = maxFreshness(resultLevel, levelUnknown)
		}
	}
	derived.level = maxFreshness(derived.level, resultLevel)
	updateLastValidated(derived, result.CompletedAt)
	if resultLevel == levelCurrent && result.Outcome == OutcomeFail && binding.Required {
		derived.hasChallenge = true
		derived.reasons = append(derived.reasons, "required validation failed: "+binding.ID)
	}
	if resultLevel == levelCurrent && result.Outcome == OutcomePass {
		derived.hasObserved = true
	}
	if binding.Required && !resultQualifies(binding, result, resultLevel) {
		derived.verified = false
	}
}

func updateLastValidated(derived *validationDerivation, completed time.Time) {
	if derived.lastValidated != nil && !completed.After(*derived.lastValidated) {
		return
	}
	value := completed
	derived.lastValidated = &value
}

func resultQualifies(binding ValidationBinding, result ValidationResult, level freshnessLevel) bool {
	return level == levelCurrent && result.Outcome == OutcomePass && assuranceMeets(result.Assurance, binding.RequiredAssurance)
}

func deriveAssessment(evidence evidenceDerivation, validation validationDerivation) Assessment {
	switch {
	case evidence.hasChallenge || validation.hasChallenge:
		return AssessmentChallenged
	case validation.verified:
		return AssessmentVerified
	case evidence.hasObserved || validation.hasObserved:
		return AssessmentObserved
	case evidence.hasSourced:
		return AssessmentSourced
	default:
		return AssessmentAsserted
	}
}

func deriveFreshness(evidence evidenceDerivation, validation validationDerivation) Freshness {
	level := maxFreshness(evidence.level, validation.level)
	hasRelevant := evidence.hasRelevant || validation.hasRelevant
	switch {
	case level == levelStale:
		return FreshnessStale
	case !hasRelevant || level == levelUnknown:
		return FreshnessUnknown
	default:
		return FreshnessCurrent
	}
}

func observationKind(kind EvidenceKind) bool {
	return kind == EvidenceManualVerification || kind == EvidenceValidatorObservation || kind == EvidenceValidatorProof
}

func evidenceFreshness(evidence Evidence, now time.Time) freshnessLevel {
	if evidence.Availability == AvailabilityMissing {
		return levelStale
	}
	if evidence.ValidUntil != nil && !now.Before(*evidence.ValidUntil) {
		return levelStale
	}
	if evidence.Fingerprint != "" && evidence.ObservedFingerprint != "" && evidence.Fingerprint != evidence.ObservedFingerprint {
		return levelStale
	}
	if evidence.Availability == AvailabilityUnknown || (evidence.Fingerprint != "" && evidence.ObservedFingerprint == "") {
		return levelUnknown
	}
	return levelCurrent
}

func resultFreshness(binding ValidationBinding, result ValidationResult, evidence map[string]Evidence, now time.Time) freshnessLevel {
	if result.InputFingerprint != binding.InputFingerprint ||
		(binding.RepositoryRevision != "" && result.RepositoryRevision != binding.RepositoryRevision) ||
		(binding.SnapshotFingerprint != "" && result.SnapshotFingerprint != binding.SnapshotFingerprint) {
		return levelStale
	}
	if result.ValidUntil != nil && !now.Before(*result.ValidUntil) {
		return levelStale
	}
	if binding.ValidForSeconds > 0 && !now.Before(result.CompletedAt.Add(time.Duration(binding.ValidForSeconds)*time.Second)) {
		return levelStale
	}
	level := levelCurrent
	for _, id := range append(append([]string(nil), binding.EvidenceIDs...), result.EvidenceIDs...) {
		item, exists := evidence[id]
		if !exists {
			return levelStale
		}
		level = maxFreshness(level, evidenceFreshness(item, now))
	}
	return level
}

func assuranceMeets(actual, required Assurance) bool {
	if required == AssuranceObservation {
		return actual == AssuranceObservation || actual == AssuranceVerification
	}
	return actual == AssuranceVerification
}

func latestResults(results []ValidationResult) map[string]ValidationResult {
	latest := make(map[string]ValidationResult)
	for _, result := range results {
		current, exists := latest[result.BindingID]
		if !exists || result.CompletedAt.After(current.CompletedAt) || (result.CompletedAt.Equal(current.CompletedAt) && result.ID > current.ID) {
			latest[result.BindingID] = result
		}
	}
	return latest
}

func previousPass(results []ValidationResult, bindingID, latestID string) bool {
	for _, result := range results {
		if result.BindingID == bindingID && result.ID != latestID && result.Outcome == OutcomePass {
			return true
		}
	}
	return false
}

func maxFreshness(left, right freshnessLevel) freshnessLevel {
	if right > left {
		return right
	}
	return left
}

// ExplainResultFreshness is used by deterministic lint without duplicating rules.
func ExplainResultFreshness(binding ValidationBinding, result ValidationResult, evidence map[string]Evidence, now time.Time) (Freshness, string) {
	switch resultFreshness(binding, result, evidence, now.UTC()) {
	case levelStale:
		return FreshnessStale, fmt.Sprintf("validation result %s no longer matches its binding, evidence, or validity period", result.ID)
	case levelUnknown:
		return FreshnessUnknown, fmt.Sprintf("validation result %s depends on evidence with unknown freshness", result.ID)
	default:
		return FreshnessCurrent, ""
	}
}
