package knowledge

import (
	"fmt"
	"go/token"
	"regexp"
	"sort"
	"strings"
)

var (
	semanticIDPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*$`)
	validatorIDPattern    = regexp.MustCompile(`^[a-z][a-z0-9._-]*$`)
	diagnosticCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,127}$`)
	fingerprintPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// ValidationError identifies one deterministic schema or integrity failure.
type ValidationError struct {
	Code    string
	Path    string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Message
	}
	return e.Path + ": " + e.Message
}

func invalid(code, path, format string, args ...any) error {
	return &ValidationError{Code: code, Path: path, Message: fmt.Sprintf(format, args...)}
}

// ValidSemanticID reports whether an ID is safe, uppercase, and human-readable.
func ValidSemanticID(id string) bool {
	return len(id) >= 3 && len(id) <= 128 && semanticIDPattern.MatchString(id)
}

// ValidSymbolLocator reports whether a symbol_reference evidence locator
// carries the package-qualified symbol identity gograph emits:
// symbol:<import/path>::<Identifier> or symbol:<import/path>::(Recv).Method.
// A file path, a bare name, or anything ambiguous is refused.
func ValidSymbolLocator(locator string) bool {
	value, found := strings.CutPrefix(locator, "symbol:")
	if !found || len(value) > 512 {
		return false
	}
	packagePath, declaration, split := strings.Cut(value, "::")
	if !split || strings.Contains(declaration, "::") {
		return false
	}
	if packagePath == "" || strings.HasPrefix(packagePath, "/") || strings.HasPrefix(packagePath, ".") ||
		strings.Contains(packagePath, "..") || strings.ContainsAny(packagePath, `\ `) {
		return false
	}
	if token.IsIdentifier(declaration) {
		return true
	}
	receiver, method, dotted := strings.Cut(declaration, ").")
	if !dotted || !strings.HasPrefix(receiver, "(") {
		return false
	}
	receiver = strings.TrimPrefix(strings.TrimPrefix(receiver, "("), "*")
	return token.IsIdentifier(receiver) && token.IsIdentifier(method)
}

// Validate checks one complete claim aggregate without resolving other claims.
func (c Claim) Validate() error {
	if c.SchemaVersion != SchemaVersion {
		return invalid("unsupported_schema_version", "schema_version", "unsupported schema version %q", c.SchemaVersion)
	}
	if !ValidSemanticID(c.ID) {
		return invalid("invalid_id", "id", "invalid semantic claim ID %q", c.ID)
	}
	if strings.TrimSpace(c.Subject) == "" || len(c.Subject) > 200 {
		return invalid("invalid_subject", "subject", "must contain 1 to 200 characters")
	}
	if strings.TrimSpace(c.Statement) == "" || len(c.Statement) > 4000 {
		return invalid("invalid_statement", "statement", "must contain 1 to 4000 characters")
	}
	if err := c.Lifecycle.validate(c.ID); err != nil {
		return err
	}
	if err := c.Authorship.validate(); err != nil {
		return err
	}
	if c.Evidence == nil {
		return invalid("malformed_evidence", "evidence", "must be a JSON array")
	}
	if c.ValidationResults == nil {
		return invalid("malformed_validation_result", "validation_results", "must be a JSON array")
	}
	if c.CreatedAt.IsZero() || c.UpdatedAt.IsZero() || c.UpdatedAt.Before(c.CreatedAt) {
		return invalid("invalid_timestamp", "created_at", "created_at and updated_at must be valid and ordered")
	}
	if err := c.validateEvidence(); err != nil {
		return err
	}
	bindings, err := c.validatePolicy()
	if err != nil {
		return err
	}
	return c.validateResults(bindings)
}

func (l Lifecycle) validate(claimID string) error {
	switch l.State {
	case LifecycleActive:
		if l.SupersededBy != "" || l.WithdrawalReason != "" {
			return invalid("invalid_lifecycle_link", "lifecycle", "active claims cannot have supersession or withdrawal fields")
		}
	case LifecycleSuperseded:
		if !ValidSemanticID(l.SupersededBy) || l.SupersededBy == claimID || l.WithdrawalReason != "" {
			return invalid("invalid_lifecycle_link", "lifecycle", "superseded claims require a different valid superseded_by ID")
		}
	case LifecycleWithdrawn:
		if strings.TrimSpace(l.WithdrawalReason) == "" || l.SupersededBy != "" {
			return invalid("invalid_lifecycle_link", "lifecycle", "withdrawn claims require a reason and cannot name a successor")
		}
	default:
		return invalid("invalid_enum", "lifecycle.state", "invalid lifecycle state %q", l.State)
	}
	return nil
}

func (a Authorship) validate() error {
	switch a.Kind {
	case AuthorshipOwner, AuthorshipHuman, AuthorshipAgent, AuthorshipImport:
	default:
		return invalid("invalid_enum", "authorship.kind", "invalid authorship kind %q", a.Kind)
	}
	if strings.TrimSpace(a.Origin) == "" || a.RecordedAt.IsZero() {
		return invalid("invalid_authorship", "authorship", "origin and recorded_at are required")
	}
	return nil
}

func (c Claim) validateEvidence() error {
	byID := make(map[string]Evidence, len(c.Evidence))
	for index, evidence := range c.Evidence {
		path := fmt.Sprintf("evidence[%d]", index)
		if err := evidence.validate(path); err != nil {
			return err
		}
		if evidence.CapturedAt.After(c.UpdatedAt) {
			return invalid("invalid_timestamp", path+".captured_at", "cannot be after claim updated_at")
		}
		if _, exists := byID[evidence.ID]; exists {
			return invalid("duplicate_evidence_id", path+".id", "duplicate evidence ID %q", evidence.ID)
		}
		if evidence.Polarity == PolaritySupports && isSelfReference(c.ID, evidence) {
			return invalid("self_support", path, "claim authorship or the claim itself cannot support its own correctness")
		}
		if evidence.Polarity == PolaritySupports && evidence.Origin.Kind == OriginLLMGenerated && len(evidence.DerivedFrom) == 0 {
			return invalid("self_support", path, "LLM-generated supporting evidence requires independent derived_from lineage")
		}
		byID[evidence.ID] = evidence
	}
	for index, evidence := range c.Evidence {
		for _, parent := range evidence.DerivedFrom {
			if parent == evidence.ID || byID[parent].ID == "" {
				return invalid("broken_evidence_reference", fmt.Sprintf("evidence[%d].derived_from", index), "unknown or self-referential evidence ID %q", parent)
			}
		}
	}
	if cycle := evidenceCycle(byID); len(cycle) > 0 {
		return invalid("evidence_lineage_cycle", "evidence.derived_from", "cycle: %s", strings.Join(cycle, " -> "))
	}
	return nil
}

func (e Evidence) validate(path string) error {
	if !ValidSemanticID(e.ID) {
		return invalid("invalid_id", path+".id", "invalid evidence ID %q", e.ID)
	}
	switch e.Kind {
	case EvidenceOwnerAssertion, EvidenceHumanAssertion, EvidenceDecision, EvidenceExternalSource,
		EvidenceRepositoryReference, EvidenceSymbolReference, EvidenceManualVerification, EvidenceValidatorObservation, EvidenceValidatorProof:
	default:
		return invalid("invalid_enum", path+".kind", "invalid evidence kind %q", e.Kind)
	}
	if e.Kind == EvidenceSymbolReference && !ValidSymbolLocator(e.Locator) {
		return invalid("malformed_evidence", path+".locator",
			"symbol_reference locator must be symbol:<import/path>::<Identifier> — a package-qualified symbol identity, never a file path")
	}
	switch e.Polarity {
	case PolaritySupports, PolarityChallenges, PolarityContext:
	default:
		return invalid("invalid_enum", path+".polarity", "invalid evidence polarity %q", e.Polarity)
	}
	switch e.Origin.Kind {
	case OriginOwner, OriginHuman, OriginRepository, OriginExternal, OriginValidator, OriginLLMGenerated:
	default:
		return invalid("invalid_enum", path+".origin.kind", "invalid origin kind %q", e.Origin.Kind)
	}
	if strings.TrimSpace(e.Origin.Reference) == "" || strings.TrimSpace(e.Locator) == "" || strings.TrimSpace(e.Scope) == "" {
		return invalid("malformed_evidence", path, "origin reference, locator, and scope are required")
	}
	switch e.Availability {
	case AvailabilityAvailable, AvailabilityMissing, AvailabilityUnknown:
	default:
		return invalid("invalid_enum", path+".availability", "invalid availability %q", e.Availability)
	}
	if e.CapturedAt.IsZero() || (e.CheckedAt != nil && e.CheckedAt.IsZero()) {
		return invalid("invalid_timestamp", path, "captured_at and checked_at must be valid")
	}
	if e.ValidUntil != nil && !e.ValidUntil.After(e.CapturedAt) {
		return invalid("invalid_timestamp", path+".valid_until", "must be after captured_at")
	}
	if err := validateFingerprint(path+".fingerprint", e.Fingerprint, false); err != nil {
		return err
	}
	if err := validateFingerprint(path+".observed_fingerprint", e.ObservedFingerprint, false); err != nil {
		return err
	}
	if e.ObservedFingerprint != "" && e.CheckedAt == nil {
		return invalid("malformed_evidence", path+".checked_at", "checked_at is required with observed_fingerprint")
	}
	if e.DerivedFrom == nil {
		return invalid("malformed_evidence", path+".derived_from", "must be a JSON array")
	}
	return nil
}

func (c Claim) validatePolicy() (map[string]ValidationBinding, error) {
	bindings := make(map[string]ValidationBinding)
	if c.ValidationPolicy == nil {
		return bindings, nil
	}
	if c.ValidationPolicy.Mode != "all_required" || len(c.ValidationPolicy.Bindings) == 0 {
		return nil, invalid("invalid_validation_policy", "validation_policy", "mode must be all_required and bindings must not be empty")
	}
	evidence := c.evidenceIDs()
	for index, binding := range c.ValidationPolicy.Bindings {
		path := fmt.Sprintf("validation_policy.bindings[%d]", index)
		if err := binding.validate(path); err != nil {
			return nil, err
		}
		if _, exists := bindings[binding.ID]; exists {
			return nil, invalid("duplicate_binding_id", path+".id", "duplicate binding ID %q", binding.ID)
		}
		for _, evidenceID := range binding.EvidenceIDs {
			if !evidence[evidenceID] {
				return nil, invalid("invalid_validation_binding_reference", path+".evidence_ids", "unknown evidence ID %q", evidenceID)
			}
		}
		bindings[binding.ID] = binding
	}
	return bindings, nil
}

func (b ValidationBinding) validate(path string) error {
	if !ValidSemanticID(b.ID) || !validatorIDPattern.MatchString(b.ValidatorID) || strings.TrimSpace(b.BindingVersion) == "" || strings.TrimSpace(b.Reference) == "" {
		return invalid("malformed_validation_binding", path, "valid ID, validator_id, binding_version, and reference are required")
	}
	if b.RequiredAssurance != AssuranceObservation && b.RequiredAssurance != AssuranceVerification {
		return invalid("invalid_enum", path+".required_assurance", "invalid assurance %q", b.RequiredAssurance)
	}
	if b.ValidatorID == "manual" && b.RequiredAssurance != AssuranceObservation {
		return invalid("manual_assurance_ceiling", path+".required_assurance", "manual validation is observation-grade only")
	}
	if b.EvidenceIDs == nil {
		return invalid("malformed_validation_binding", path+".evidence_ids", "must be a JSON array")
	}
	if err := validateFingerprint(path+".input_fingerprint", b.InputFingerprint, true); err != nil {
		return err
	}
	if err := validateFingerprint(path+".snapshot_fingerprint", b.SnapshotFingerprint, false); err != nil {
		return err
	}
	if b.RepositoryRevision == "" && b.SnapshotFingerprint == "" {
		return invalid("malformed_validation_binding", path, "repository_revision or snapshot_fingerprint is required")
	}
	if b.ValidForSeconds < 0 {
		return invalid("malformed_validation_binding", path+".valid_for_seconds", "cannot be negative")
	}
	return nil
}

func (c Claim) validateResults(bindings map[string]ValidationBinding) error {
	resultIDs := make(map[string]bool, len(c.ValidationResults))
	evidence := c.evidenceIDs()
	for index, result := range c.ValidationResults {
		path := fmt.Sprintf("validation_results[%d]", index)
		if err := result.validate(path); err != nil {
			return err
		}
		if result.CompletedAt.After(c.UpdatedAt) {
			return invalid("invalid_timestamp", path+".completed_at", "cannot be after claim updated_at")
		}
		if resultIDs[result.ID] {
			return invalid("duplicate_validation_result_id", path+".id", "duplicate result ID %q", result.ID)
		}
		resultIDs[result.ID] = true
		binding, exists := bindings[result.BindingID]
		if !exists {
			return invalid("invalid_result_binding_reference", path, "result does not match binding %q", result.BindingID)
		}
		if result.ValidatorID != binding.ValidatorID {
			return invalid("result_validator_mismatch", path+".validator_id", "result validator does not match binding %q", result.BindingID)
		}
		if result.BindingVersion != binding.BindingVersion {
			return invalid("result_binding_mismatch", path+".binding_version", "result binding version does not match binding %q", result.BindingID)
		}
		for _, evidenceID := range result.EvidenceIDs {
			if !evidence[evidenceID] {
				return invalid("broken_evidence_reference", path+".evidence_ids", "unknown evidence ID %q", evidenceID)
			}
		}
	}
	return nil
}

func (r ValidationResult) validate(path string) error {
	if err := r.validateIdentity(path); err != nil {
		return err
	}
	if err := r.validateOutcome(path); err != nil {
		return err
	}
	if err := r.validateTiming(path); err != nil {
		return err
	}
	if r.RepositoryRevision == "" && r.SnapshotFingerprint == "" {
		return invalid("malformed_validation_result", path, "repository_revision or snapshot_fingerprint is required")
	}
	if err := validateFingerprint(path+".input_fingerprint", r.InputFingerprint, true); err != nil {
		return err
	}
	if err := validateFingerprint(path+".snapshot_fingerprint", r.SnapshotFingerprint, false); err != nil {
		return err
	}
	if r.EvidenceIDs == nil {
		return invalid("malformed_validation_result", path+".evidence_ids", "must be a JSON array")
	}
	for key, value := range r.Metadata {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			return invalid("malformed_validation_result", path+".metadata", "metadata keys and values must not be empty")
		}
	}
	for index, diagnostic := range r.Diagnostics {
		diagnosticPath := fmt.Sprintf("%s.diagnostics[%d]", path, index)
		if !diagnosticCodePattern.MatchString(diagnostic.Code) || strings.TrimSpace(diagnostic.Message) == "" {
			return invalid("malformed_validation_result", diagnosticPath, "diagnostic code and message are required")
		}
	}
	return nil
}

func (r ValidationResult) validateIdentity(path string) error {
	if !ValidSemanticID(r.ID) || !ValidSemanticID(r.BindingID) || !validatorIDPattern.MatchString(r.ValidatorID) || strings.TrimSpace(r.ValidatorVersion) == "" || strings.TrimSpace(r.BindingVersion) == "" {
		return invalid("malformed_validation_result", path, "valid IDs and validator/binding versions are required")
	}
	if r.Assurance != AssuranceObservation && r.Assurance != AssuranceVerification {
		return invalid("invalid_enum", path+".assurance", "invalid assurance %q", r.Assurance)
	}
	if r.ValidatorID == "manual" && r.Assurance != AssuranceObservation {
		return invalid("manual_assurance_ceiling", path+".assurance", "manual validation is observation-grade only")
	}
	return nil
}

func (r ValidationResult) validateOutcome(path string) error {
	switch r.Outcome {
	case OutcomePass, OutcomeFail, OutcomeCannotEvaluate:
		return nil
	default:
		return invalid("invalid_enum", path+".outcome", "invalid outcome %q", r.Outcome)
	}
}

func (r ValidationResult) validateTiming(path string) error {
	if strings.TrimSpace(r.Reason) == "" || r.StartedAt.IsZero() || r.CompletedAt.IsZero() || r.CompletedAt.Before(r.StartedAt) {
		return invalid("invalid_timestamp", path, "reason and ordered started_at/completed_at are required")
	}
	if r.ValidUntil != nil && !r.ValidUntil.After(r.CompletedAt) {
		return invalid("invalid_timestamp", path+".valid_until", "must be after completed_at")
	}
	return nil
}

func validateFingerprint(path, value string, required bool) error {
	if value == "" && !required {
		return nil
	}
	if !fingerprintPattern.MatchString(value) {
		return invalid("invalid_fingerprint", path, "expected sha256:<64 lowercase hex characters>")
	}
	return nil
}

func (c Claim) evidenceIDs() map[string]bool {
	result := make(map[string]bool, len(c.Evidence))
	for _, evidence := range c.Evidence {
		result[evidence.ID] = true
	}
	return result
}

func isSelfReference(claimID string, evidence Evidence) bool {
	references := []string{evidence.Locator, evidence.Origin.Reference}
	for _, reference := range references {
		if reference == claimID || reference == "claim:"+claimID {
			return true
		}
	}
	return false
}

func evidenceCycle(evidence map[string]Evidence) []string {
	state := make(map[string]uint8, len(evidence))
	stack := make([]string, 0, len(evidence))
	positions := make(map[string]int, len(evidence))
	ids := make([]string, 0, len(evidence))
	for id := range evidence {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var visit func(string) []string
	visit = func(id string) []string {
		state[id] = 1
		positions[id] = len(stack)
		stack = append(stack, id)
		parents := append([]string(nil), evidence[id].DerivedFrom...)
		sort.Strings(parents)
		for _, parent := range parents {
			if state[parent] == 1 {
				cycle := append([]string(nil), stack[positions[parent]:]...)
				return append(cycle, parent)
			}
			if state[parent] == 0 {
				if cycle := visit(parent); len(cycle) > 0 {
					return cycle
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(positions, id)
		state[id] = 2
		return nil
	}
	for _, id := range ids {
		if state[id] == 0 {
			if cycle := visit(id); len(cycle) > 0 {
				return cycle
			}
		}
	}
	return nil
}
