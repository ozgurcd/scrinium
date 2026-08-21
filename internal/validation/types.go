package validation

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"scrinium/internal/knowledge"
)

var (
	validatorIDPattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,63}$`)
	reasonCodePattern  = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)
)

type Descriptor struct {
	ID                       string
	Version                  string
	SupportedBindingVersions []string
	MaximumAssurance         knowledge.Assurance
}

func (d Descriptor) Validate() error {
	if !validatorIDPattern.MatchString(d.ID) {
		return validationError("invalid_validator_id", fmt.Sprintf("invalid validator ID %q", d.ID))
	}
	if strings.TrimSpace(d.Version) == "" {
		return validationError("invalid_validator_version", "validator version is required")
	}
	if d.MaximumAssurance != knowledge.AssuranceObservation && d.MaximumAssurance != knowledge.AssuranceVerification {
		return validationError("invalid_assurance", fmt.Sprintf("invalid maximum assurance %q", d.MaximumAssurance))
	}
	if d.ID == "manual" && d.MaximumAssurance != knowledge.AssuranceObservation {
		return validationError("manual_assurance_ceiling", "manual validator maximum assurance must be observation")
	}
	if len(d.SupportedBindingVersions) == 0 {
		return validationError("missing_binding_versions", "at least one supported binding version is required")
	}
	seen := make(map[string]bool, len(d.SupportedBindingVersions))
	for _, version := range d.SupportedBindingVersions {
		if strings.TrimSpace(version) == "" {
			return validationError("invalid_binding_version", "supported binding versions must not be empty")
		}
		if seen[version] {
			return validationError("duplicate_binding_version", fmt.Sprintf("duplicate binding version %q", version))
		}
		seen[version] = true
	}
	return nil
}

func (d Descriptor) SupportsBindingVersion(version string) bool {
	for _, supported := range d.SupportedBindingVersions {
		if supported == version {
			return true
		}
	}
	return false
}

type RepositorySnapshot struct {
	Revision    string
	Fingerprint string
	CapturedAt  time.Time
}

type Request struct {
	Claim            knowledge.Claim
	Binding          knowledge.ValidationBinding
	Repository       RepositorySnapshot
	InputFingerprint string
	Inputs           map[string]string
	ResultID         string
	StartedAt        time.Time
}

type Validator interface {
	Descriptor() Descriptor
	Validate(ctx context.Context, req Request) (knowledge.ValidationResult, error)
}

// BindingValidator is an optional adapter contract for strict, validator-owned
// binding schemas. Generic orchestration remains unaware of adapter fields.
type BindingValidator interface {
	ValidateBinding(binding knowledge.ValidationBinding) error
}

type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func validationError(code, message string) error {
	return &Error{Code: code, Message: message}
}

func AssuranceAllowed(actual, maximum knowledge.Assurance) bool {
	switch maximum {
	case knowledge.AssuranceObservation:
		return actual == knowledge.AssuranceObservation
	case knowledge.AssuranceVerification:
		return actual == knowledge.AssuranceObservation || actual == knowledge.AssuranceVerification
	default:
		return false
	}
}

func ValidateResult(descriptor Descriptor, req Request, result knowledge.ValidationResult, finishedAt time.Time) error {
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if err := validateResultIdentity(descriptor, req, result); err != nil {
		return err
	}
	if err := validateResultSemantics(descriptor, result); err != nil {
		return err
	}
	if err := validateResultInputs(req, result); err != nil {
		return err
	}
	if err := validateResultTiming(req, result, finishedAt); err != nil {
		return err
	}
	return validateResultEvidence(req, result)
}

func validateResultIdentity(descriptor Descriptor, req Request, result knowledge.ValidationResult) error {
	if !descriptor.SupportsBindingVersion(req.Binding.BindingVersion) {
		return validationError("unsupported_binding_version", fmt.Sprintf("validator %s does not support binding version %s", descriptor.ID, req.Binding.BindingVersion))
	}
	if result.ID != req.ResultID || !knowledge.ValidSemanticID(result.ID) {
		return validationError("result_identity_mismatch", "result ID does not match the validation attempt")
	}
	if result.ValidatorID != descriptor.ID || result.ValidatorID != req.Binding.ValidatorID {
		return validationError("result_validator_mismatch", "result validator ID does not match the resolved validator and binding")
	}
	if result.ValidatorVersion != descriptor.Version {
		return validationError("result_validator_version_mismatch", "result validator version does not match the registered descriptor")
	}
	if result.BindingID != req.Binding.ID || result.BindingVersion != req.Binding.BindingVersion {
		return validationError("result_binding_mismatch", "result binding identity or schema version does not match the request")
	}
	return nil
}

func validateResultSemantics(descriptor Descriptor, result knowledge.ValidationResult) error {
	if !AssuranceAllowed(result.Assurance, descriptor.MaximumAssurance) {
		return validationError("assurance_above_ceiling", "result assurance exceeds the validator descriptor ceiling")
	}
	switch result.Outcome {
	case knowledge.OutcomePass, knowledge.OutcomeFail, knowledge.OutcomeCannotEvaluate:
	default:
		return validationError("invalid_outcome", fmt.Sprintf("invalid validation outcome %q", result.Outcome))
	}
	if !reasonCodePattern.MatchString(result.ReasonCode) || strings.TrimSpace(result.Reason) == "" {
		return validationError("incomplete_result_reason", "structured reason code and message are required")
	}
	for key, value := range result.Metadata {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return validationError("incomplete_result_metadata", "result metadata keys and values must not be empty")
		}
	}
	for _, diagnostic := range result.Diagnostics {
		if !reasonCodePattern.MatchString(diagnostic.Code) || strings.TrimSpace(diagnostic.Message) == "" {
			return validationError("incomplete_result_diagnostic", "result diagnostic code and message are required")
		}
	}
	return nil
}

func validateResultInputs(req Request, result knowledge.ValidationResult) error {
	if req.Repository.Fingerprint == "" || req.InputFingerprint == "" {
		return validationError("missing_request_fingerprint", "repository and input fingerprints are required")
	}
	if result.InputFingerprint != req.InputFingerprint {
		return validationError("result_input_fingerprint_mismatch", "result input fingerprint does not match the request")
	}
	if result.RepositoryRevision != req.Repository.Revision || result.SnapshotFingerprint != req.Repository.Fingerprint {
		return validationError("result_repository_snapshot_mismatch", "result repository snapshot does not match the request")
	}
	return nil
}

func validateResultTiming(req Request, result knowledge.ValidationResult, finishedAt time.Time) error {
	if req.StartedAt.IsZero() || result.StartedAt.IsZero() || result.CompletedAt.IsZero() || result.StartedAt.Before(req.StartedAt) || result.CompletedAt.Before(result.StartedAt) || result.CompletedAt.After(finishedAt) {
		return validationError("invalid_result_timestamps", "result timestamps are outside the validation attempt")
	}
	if result.ValidUntil != nil && !result.ValidUntil.After(result.CompletedAt) {
		return validationError("invalid_result_timestamps", "result valid_until must be after completed_at")
	}
	return nil
}

func validateResultEvidence(req Request, result knowledge.ValidationResult) error {
	if result.EvidenceIDs == nil {
		return validationError("incomplete_result_evidence", "result evidence IDs must be an array")
	}
	evidence := make(map[string]bool, len(req.Claim.Evidence))
	for _, item := range req.Claim.Evidence {
		evidence[item.ID] = true
	}
	for _, id := range result.EvidenceIDs {
		if !evidence[id] {
			return validationError("result_evidence_mismatch", fmt.Sprintf("result references unknown evidence %s", id))
		}
	}
	return nil
}

func Execute(ctx context.Context, validator Validator, req Request) (knowledge.ValidationResult, error) {
	if err := ctx.Err(); err != nil {
		return knowledge.ValidationResult{}, err
	}
	return validator.Validate(ctx, cloneRequest(req))
}

// ValidateBinding asks an adapter to validate its owned binding payload when it
// exposes BindingValidator. Validators without a payload schema remain valid.
func ValidateBinding(validator Validator, binding knowledge.ValidationBinding) error {
	if validator == nil {
		return validationError("validator_unavailable", "validator is unavailable")
	}
	if schema, ok := validator.(BindingValidator); ok {
		return schema.ValidateBinding(cloneBinding(binding))
	}
	return nil
}

func cloneRequest(req Request) Request {
	copyRequest := req
	copyRequest.Inputs = cloneStringMap(req.Inputs)
	copyRequest.Binding = cloneBinding(req.Binding)
	copyRequest.Claim = cloneClaim(req.Claim)
	return copyRequest
}

func cloneClaim(claim knowledge.Claim) knowledge.Claim {
	copyClaim := claim
	copyClaim.Evidence = append([]knowledge.Evidence(nil), claim.Evidence...)
	for index := range copyClaim.Evidence {
		copyClaim.Evidence[index].DerivedFrom = append([]string(nil), claim.Evidence[index].DerivedFrom...)
		copyClaim.Evidence[index].CheckedAt = cloneTime(claim.Evidence[index].CheckedAt)
		copyClaim.Evidence[index].ValidUntil = cloneTime(claim.Evidence[index].ValidUntil)
	}
	if claim.ValidationPolicy != nil {
		policy := *claim.ValidationPolicy
		policy.Bindings = append([]knowledge.ValidationBinding(nil), claim.ValidationPolicy.Bindings...)
		for index := range policy.Bindings {
			policy.Bindings[index] = cloneBinding(policy.Bindings[index])
		}
		copyClaim.ValidationPolicy = &policy
	}
	copyClaim.ValidationResults = append([]knowledge.ValidationResult(nil), claim.ValidationResults...)
	for index := range copyClaim.ValidationResults {
		copyClaim.ValidationResults[index].EvidenceIDs = append([]string(nil), claim.ValidationResults[index].EvidenceIDs...)
		copyClaim.ValidationResults[index].Metadata = cloneStringMap(claim.ValidationResults[index].Metadata)
		copyClaim.ValidationResults[index].Diagnostics = append([]knowledge.ValidationDiagnostic(nil), claim.ValidationResults[index].Diagnostics...)
		copyClaim.ValidationResults[index].ValidUntil = cloneTime(claim.ValidationResults[index].ValidUntil)
	}
	return copyClaim
}

func cloneBinding(binding knowledge.ValidationBinding) knowledge.ValidationBinding {
	copyBinding := binding
	copyBinding.Parameters = cloneStringMap(binding.Parameters)
	copyBinding.EvidenceIDs = append([]string(nil), binding.EvidenceIDs...)
	return copyBinding
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	copyValues := make(map[string]string, len(values))
	for key, value := range values {
		copyValues[key] = value
	}
	return copyValues
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func normalizeDescriptor(descriptor Descriptor) Descriptor {
	copyDescriptor := descriptor
	copyDescriptor.SupportedBindingVersions = append([]string(nil), descriptor.SupportedBindingVersions...)
	sort.Strings(copyDescriptor.SupportedBindingVersions)
	return copyDescriptor
}
