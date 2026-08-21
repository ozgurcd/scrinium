package lint

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/provenance"
	"scrinium/internal/store"
	"scrinium/internal/validation"
)

type ClaimFinding struct {
	AnalysisKind string `json:"analysis_kind"`
	Severity     string `json:"severity"`
	Path         string `json:"path"`
	Code         string `json:"code"`
	Evidence     string `json:"evidence"`
	Fix          string `json:"fix"`
}

type ClaimReport struct {
	OK            bool           `json:"ok"`
	FilesChecked  int            `json:"files_checked"`
	ClaimsChecked int            `json:"claims_checked"`
	Findings      []ClaimFinding `json:"findings"`
}

// ClaimService performs deterministic canonical-claim integrity checks only.
type ClaimService struct {
	claims     *store.ClaimStore
	sources    *store.SourceStore
	repository *store.Store
	validators *validation.Registry
	snapshots  *validation.Snapshotter
}

func NewClaimService(claims *store.ClaimStore, sources *store.SourceStore, repository *store.Store, validators *validation.Registry, snapshots *validation.Snapshotter) *ClaimService {
	return &ClaimService{claims: claims, sources: sources, repository: repository, validators: validators, snapshots: snapshots}
}

func (s *ClaimService) Build(ctx context.Context, now time.Time) (ClaimReport, error) {
	entries, err := s.claims.Inspect(ctx)
	if err != nil {
		return ClaimReport{}, err
	}
	findings := make([]ClaimFinding, 0)
	claims := make(map[string]knowledge.Claim)
	pathsByID := make(map[string][]string)
	for _, entry := range entries {
		if entry.Claim != nil {
			pathsByID[entry.Claim.ID] = append(pathsByID[entry.Claim.ID], entry.Path)
			if _, exists := claims[entry.Claim.ID]; !exists {
				claims[entry.Claim.ID] = *entry.Claim
			}
		}
		if entry.Err != nil {
			findings = append(findings, claimFileFinding(entry.Path, entry.Err))
		}
	}
	for id, paths := range pathsByID {
		if len(paths) > 1 {
			sort.Strings(paths)
			findings = append(findings, deterministicFinding("high", strings.Join(paths, ", "), "duplicate_claim_id", "Claim ID "+id+" appears in multiple files.", "Keep exactly one canonical file whose filename matches the immutable claim ID."))
		}
	}

	ids := make([]string, 0, len(claims))
	for id := range claims {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		claim := claims[id]
		path, _ := store.ClaimPath(id)
		if claim.Lifecycle.State == knowledge.LifecycleSuperseded {
			successor, exists := claims[claim.Lifecycle.SupersededBy]
			switch {
			case !exists:
				findings = append(findings, deterministicFinding("high", path, "broken_claim_reference", "superseded_by references missing claim "+claim.Lifecycle.SupersededBy+".", "Create the referenced successor or correct the lifecycle transition through an authorized migration."))
			case successor.Lifecycle.State != knowledge.LifecycleActive:
				findings = append(findings, deterministicFinding("high", path, "invalid_lifecycle_link", "superseded_by references a non-active successor.", "Supersede the claim with an active successor."))
			}
		}
		findings = append(findings, s.evidenceFindings(ctx, path, claim, now)...)
		findings = append(findings, s.validationFindings(ctx, path, claim)...)
		claim = s.observeSourceEvidence(ctx, claim, now)
		state, deriveErr := knowledge.DeriveState(claim, now)
		if deriveErr == nil && state.Freshness == knowledge.FreshnessStale {
			findings = append(findings, deterministicFinding("medium", path, "stale_claim_inputs", "Derived freshness is stale because recorded evidence or validation inputs no longer qualify as current.", "Refresh the affected evidence or record a new validation result; do not preserve verified presentation."))
		}
		findings = append(findings, missingRequiredResultFindings(path, claim)...)
	}
	for _, cycle := range supersessionCycles(claims) {
		findings = append(findings, deterministicFinding("high", strings.Join(cycle, " -> "), "supersession_cycle", "Supersession links form a cycle.", "Repair lifecycle links through an explicit migration; do not infer a current claim."))
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Code < findings[j].Code
	})
	return ClaimReport{OK: len(findings) == 0, FilesChecked: len(entries), ClaimsChecked: len(claims), Findings: findings}, nil
}

func claimFileFinding(path string, err error) ClaimFinding {
	code := "malformed_claim_file"
	var claimErr *store.ClaimError
	if errors.As(err, &claimErr) && claimErr.Code == "filename_id_mismatch" {
		code = "filename_id_mismatch"
	}
	var validationErr *knowledge.ValidationError
	if errors.As(err, &validationErr) {
		switch validationErr.Code {
		case "broken_evidence_reference", "invalid_validation_binding_reference", "invalid_result_binding_reference", "result_validator_mismatch", "result_binding_mismatch", "invalid_lifecycle_link", "unsupported_schema_version":
			code = validationErr.Code
		}
	}
	return deterministicFinding("high", path, code, err.Error(), "Correct the canonical JSON explicitly; Scrinium will not repair malformed claim files.")
}

func (s *ClaimService) validationFindings(ctx context.Context, path string, claim knowledge.Claim) []ClaimFinding {
	if claim.ValidationPolicy == nil {
		return nil
	}
	findings := make([]ClaimFinding, 0)
	latest := latestValidationResults(claim.ValidationResults)
	for _, binding := range claim.ValidationPolicy.Bindings {
		validator, descriptor, exists := s.validators.Resolve(binding.ValidatorID)
		if !exists {
			findings = append(findings, deterministicFinding("high", path, "unknown_validator", "Binding "+binding.ID+" references unregistered validator "+binding.ValidatorID+".", "Register the validator or replace the binding; unavailable validation cannot preserve verified state."))
			continue
		}
		findings = append(findings, bindingDescriptorFindings(path, binding, descriptor)...)
		if descriptor.SupportsBindingVersion(binding.BindingVersion) {
			if err := validation.ValidateBinding(validator, binding); err != nil {
				findings = append(findings, deterministicFinding("high", path, "invalid_binding_schema", "Binding "+binding.ID+" is invalid for validator "+binding.ValidatorID+": "+err.Error()+".", "Correct the adapter-owned binding fields; invalid bindings cannot be evaluated."))
			}
		}
		snapshot, snapshotErr := s.snapshots.Build(ctx, claim, binding)
		findings = append(findings, bindingSnapshotFindings(path, claim, binding, snapshot, snapshotErr)...)
		result, hasResult := latest[binding.ID]
		if hasResult {
			findings = append(findings, resultCompatibilityFindings(path, binding, descriptor, result, snapshot, snapshotErr)...)
		}
	}
	return findings
}

func latestValidationResults(results []knowledge.ValidationResult) map[string]knowledge.ValidationResult {
	latest := make(map[string]knowledge.ValidationResult)
	for _, result := range results {
		current, exists := latest[result.BindingID]
		if !exists || result.CompletedAt.After(current.CompletedAt) || (result.CompletedAt.Equal(current.CompletedAt) && result.ID > current.ID) {
			latest[result.BindingID] = result
		}
	}
	return latest
}

func bindingDescriptorFindings(path string, binding knowledge.ValidationBinding, descriptor validation.Descriptor) []ClaimFinding {
	findings := make([]ClaimFinding, 0, 2)
	if !descriptor.SupportsBindingVersion(binding.BindingVersion) {
		findings = append(findings, deterministicFinding("high", path, "unsupported_binding_schema", "Validator "+binding.ValidatorID+" does not support binding version "+binding.BindingVersion+".", "Migrate the binding explicitly or install a compatible validator version."))
	}
	if !validation.AssuranceAllowed(binding.RequiredAssurance, descriptor.MaximumAssurance) {
		findings = append(findings, deterministicFinding("high", path, "binding_assurance_above_validator_ceiling", "Binding "+binding.ID+" requires assurance above the validator ceiling.", "Use a validator capable of the required assurance or lower policy assurance explicitly."))
	}
	return findings
}

func bindingSnapshotFindings(path string, claim knowledge.Claim, binding knowledge.ValidationBinding, snapshot validation.RepositorySnapshot, snapshotErr error) []ClaimFinding {
	if snapshotErr != nil {
		return []ClaimFinding{deterministicFinding("high", path, "stale_repository_snapshot", "Binding "+binding.ID+" repository state cannot be reproduced: "+snapshotErr.Error(), "Restore the scoped repository evidence or refresh the binding snapshot.")}
	}
	if validation.InputFingerprint(claim, binding, snapshot) != binding.InputFingerprint {
		return []ClaimFinding{deterministicFinding("high", path, "stale_binding_input_fingerprint", "Binding "+binding.ID+" input fingerprint no longer matches current claim and evidence inputs.", "Review the changed inputs and issue a new binding fingerprint.")}
	}
	return nil
}

func resultCompatibilityFindings(path string, binding knowledge.ValidationBinding, descriptor validation.Descriptor, result knowledge.ValidationResult, snapshot validation.RepositorySnapshot, snapshotErr error) []ClaimFinding {
	findings := make([]ClaimFinding, 0, 4)
	if result.ValidatorID != descriptor.ID || result.ValidatorVersion != descriptor.Version {
		findings = append(findings, deterministicFinding("high", path, "result_validator_mismatch", "Latest result for "+binding.ID+" does not match the registered validator identity/version.", "Re-run the registered validator; do not trust the mismatched result."))
	}
	if !validation.AssuranceAllowed(result.Assurance, descriptor.MaximumAssurance) {
		findings = append(findings, deterministicFinding("high", path, "result_assurance_above_validator_ceiling", "Latest result for "+binding.ID+" exceeds the validator assurance ceiling.", "Reject the result and re-run the validator within its declared assurance ceiling."))
	}
	if result.InputFingerprint != binding.InputFingerprint {
		findings = append(findings, deterministicFinding("high", path, "result_fingerprint_mismatch", "Latest result for "+binding.ID+" was produced for a different input fingerprint.", "Re-run validation against the current binding input."))
	}
	if result.RepositoryRevision != binding.RepositoryRevision || result.SnapshotFingerprint != binding.SnapshotFingerprint || (snapshotErr == nil && result.SnapshotFingerprint != snapshot.Fingerprint) {
		findings = append(findings, deterministicFinding("high", path, "stale_repository_snapshot", "Latest result for "+binding.ID+" does not match the current repository snapshot.", "Re-run validation against the current scoped repository state."))
	}
	return findings
}

func (s *ClaimService) evidenceFindings(ctx context.Context, path string, claim knowledge.Claim, now time.Time) []ClaimFinding {
	findings := make([]ClaimFinding, 0)
	for _, evidence := range claim.Evidence {
		switch {
		case evidence.Availability == knowledge.AvailabilityMissing:
			findings = append(findings, deterministicFinding("high", path, "missing_evidence", "Evidence "+evidence.ID+" is recorded as missing.", "Restore or replace the evidence and record a new check."))
		case evidence.Fingerprint != "" && evidence.ObservedFingerprint != "" && evidence.Fingerprint != evidence.ObservedFingerprint:
			findings = append(findings, deterministicFinding("high", path, "stale_evidence_fingerprint", "Evidence "+evidence.ID+" fingerprint no longer matches.", "Review the changed artifact and attach or record updated evidence."))
		case evidence.ValidUntil != nil && !now.Before(*evidence.ValidUntil):
			findings = append(findings, deterministicFinding("medium", path, "expired_evidence", "Evidence "+evidence.ID+" validity period expired.", "Re-check the evidence before relying on it."))
		}
		sourceID, sourceReferenced, sourceErr := provenance.SourceIDFromLocator(evidence.Locator)
		if sourceReferenced {
			switch {
			case sourceErr != nil:
				findings = append(findings, deterministicFinding("high", path, "invalid_source_id", "Evidence "+evidence.ID+" has an invalid canonical source locator.", "Use source:SRC-YYYYMMDD-slug or an exact canonical source ID."))
			case s.sources == nil:
				findings = append(findings, deterministicFinding("high", path, "missing_source_record", "Evidence "+evidence.ID+" cannot resolve source "+sourceID+" because canonical source storage is unavailable.", "Configure canonical source storage before relying on the evidence."))
			default:
				findings = append(findings, s.sourceEvidenceFindings(ctx, path, evidence, sourceID)...)
			}
		}
		if evidence.Kind != knowledge.EvidenceRepositoryReference || !strings.HasPrefix(evidence.Locator, "repo:") {
			continue
		}
		repositoryPath := strings.TrimPrefix(evidence.Locator, "repo:")
		exists, fingerprint, err := s.repository.Fingerprint(ctx, repositoryPath)
		switch {
		case err != nil:
			findings = append(findings, deterministicFinding("high", path, "invalid_repository_evidence", "Evidence "+evidence.ID+" cannot be checked: "+err.Error(), "Correct the confined regular-file repository locator."))
		case !exists:
			findings = append(findings, deterministicFinding("high", path, "missing_evidence", "Repository evidence "+evidence.ID+" does not exist at "+repositoryPath+".", "Restore the referenced file or replace the evidence."))
		case evidence.Fingerprint != "" && evidence.Fingerprint != fingerprint:
			findings = append(findings, deterministicFinding("high", path, "stale_evidence_fingerprint", "Repository evidence "+evidence.ID+" content fingerprint changed.", "Review the new bytes and explicitly update evidence."))
		}
	}
	return findings
}

func (s *ClaimService) sourceEvidenceFindings(ctx context.Context, path string, evidence knowledge.Evidence, sourceID string) []ClaimFinding {
	record, err := s.sources.Get(ctx, sourceID)
	if err != nil {
		var sourceErr *store.SourceError
		if errors.As(err, &sourceErr) && sourceErr.Code == "source_not_found" {
			return []ClaimFinding{deterministicFinding("high", path, "missing_source_record", "Evidence "+evidence.ID+" references missing canonical source "+sourceID+".", "Migrate or register the source record; do not infer its metadata from Markdown.")}
		}
		return []ClaimFinding{deterministicFinding("high", path, "invalid_source_record", "Evidence "+evidence.ID+" references an unreadable canonical source: "+err.Error(), "Repair the source JSON explicitly; Scrinium will not silently repair it.")}
	}
	if record.Source.Status == provenance.StatusWithdrawn {
		return []ClaimFinding{deterministicFinding("high", path, "withdrawn_source", "Evidence "+evidence.ID+" references withdrawn source "+sourceID+".", "Replace the evidence or explicitly review the source lifecycle.")}
	}
	exists, fingerprint, err := s.repository.Fingerprint(ctx, record.Source.RawPath)
	switch {
	case err != nil:
		return []ClaimFinding{deterministicFinding("high", path, "invalid_source_raw_file", "Source "+sourceID+" raw bytes cannot be checked: "+err.Error(), "Restore a confined non-linked regular raw source file.")}
	case !exists:
		return []ClaimFinding{deterministicFinding("high", path, "missing_source_raw_file", "Source "+sourceID+" raw file is missing.", "Restore the exact raw bytes or withdraw the source.")}
	case fingerprint != record.Source.RawFingerprint:
		return []ClaimFinding{deterministicFinding("high", path, "changed_source_fingerprint", "Source "+sourceID+" raw bytes no longer match its canonical fingerprint.", "Review the changed bytes and use explicit source refresh; never update the fingerprint silently.")}
	case evidence.Fingerprint != "" && evidence.Fingerprint != record.Source.RawFingerprint:
		return []ClaimFinding{deterministicFinding("high", path, "stale_evidence_fingerprint", "Evidence "+evidence.ID+" references a different fingerprint than canonical source "+sourceID+".", "Review the source version and attach explicit replacement evidence.")}
	default:
		return nil
	}
}

func (s *ClaimService) observeSourceEvidence(ctx context.Context, claim knowledge.Claim, now time.Time) knowledge.Claim {
	evidence := make([]knowledge.Evidence, len(claim.Evidence))
	copy(evidence, claim.Evidence)
	claim.Evidence = evidence
	for index := range claim.Evidence {
		id, referenced, err := provenance.SourceIDFromLocator(claim.Evidence[index].Locator)
		if err != nil || !referenced || s.sources == nil {
			continue
		}
		checked := now
		claim.Evidence[index].CheckedAt = &checked
		record, err := s.sources.Get(ctx, id)
		if err != nil || record.Source.Status == provenance.StatusWithdrawn {
			claim.Evidence[index].Availability = knowledge.AvailabilityMissing
			claim.Evidence[index].ObservedFingerprint = ""
			continue
		}
		exists, fingerprint, err := s.repository.Fingerprint(ctx, record.Source.RawPath)
		switch {
		case err != nil:
			claim.Evidence[index].Availability = knowledge.AvailabilityUnknown
			claim.Evidence[index].ObservedFingerprint = ""
		case !exists:
			claim.Evidence[index].Availability = knowledge.AvailabilityMissing
			claim.Evidence[index].ObservedFingerprint = ""
		default:
			claim.Evidence[index].Availability = knowledge.AvailabilityAvailable
			claim.Evidence[index].ObservedFingerprint = fingerprint
		}
	}
	return claim
}

func missingRequiredResultFindings(path string, claim knowledge.Claim) []ClaimFinding {
	if claim.ValidationPolicy == nil {
		return nil
	}
	results := make(map[string]bool)
	for _, result := range claim.ValidationResults {
		results[result.BindingID] = true
	}
	findings := make([]ClaimFinding, 0)
	for _, binding := range claim.ValidationPolicy.Bindings {
		if binding.Required && !results[binding.ID] {
			findings = append(findings, deterministicFinding("medium", path, "missing_required_validation_result", "Required binding "+binding.ID+" has no validation result.", "Run the bound validator or record cannot_evaluate with a reason."))
		}
	}
	return findings
}

func supersessionCycles(claims map[string]knowledge.Claim) [][]string {
	state := make(map[string]uint8, len(claims))
	stack := make([]string, 0, len(claims))
	position := make(map[string]int, len(claims))
	cycles := make([][]string, 0)
	ids := make([]string, 0, len(claims))
	for id := range claims {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var visit func(string)
	visit = func(id string) {
		state[id] = 1
		position[id] = len(stack)
		stack = append(stack, id)
		claim := claims[id]
		if claim.Lifecycle.State == knowledge.LifecycleSuperseded {
			next := claim.Lifecycle.SupersededBy
			if _, exists := claims[next]; exists {
				switch state[next] {
				case 0:
					visit(next)
				case 1:
					cycle := append([]string(nil), stack[position[next]:]...)
					cycles = append(cycles, append(cycle, next))
				}
			}
		}
		stack = stack[:len(stack)-1]
		delete(position, id)
		state[id] = 2
	}
	for _, id := range ids {
		if state[id] == 0 {
			visit(id)
		}
	}
	return cycles
}

func deterministicFinding(severity, path, code, evidence, fix string) ClaimFinding {
	return ClaimFinding{AnalysisKind: "deterministic", Severity: severity, Path: path, Code: code, Evidence: evidence, Fix: fix}
}
