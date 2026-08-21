package publicapi

import (
	"encoding/json"
	"fmt"
	"strings"
)

func HumanSummary(data []byte) (string, error) {
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return "", err
	}
	switch header.SchemaVersion {
	case ClaimResultSchema:
		var result ClaimResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: lifecycle=%s assessment=%s freshness=%s revision=%s", result.Claim.ID, result.State.Lifecycle, result.State.Assessment, result.State.Freshness, result.Revision), nil
	case ClaimListSchema:
		var result ClaimListResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("%d claim(s)", len(result.Claims))}
		for _, claim := range result.Claims {
			lines = append(lines, fmt.Sprintf("%s: lifecycle=%s assessment=%s freshness=%s revision=%s", claim.Claim.ID, claim.State.Lifecycle, claim.State.Assessment, claim.State.Freshness, claim.Revision))
		}
		return strings.Join(lines, "\n"), nil
	case ClaimValidationRunSchema:
		var result ClaimValidationRun
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("%s: lifecycle=%s assessment=%s freshness=%s revision=%s", result.Claim.Claim.ID, result.Claim.State.Lifecycle, result.Claim.State.Assessment, result.Claim.State.Freshness, result.Claim.Revision)}
		for _, validation := range result.Results {
			lines = append(lines, fmt.Sprintf("%s/%s: outcome=%s assurance=%s reason=%s", validation.ValidatorID, validation.BindingID, validation.Outcome, validation.Assurance, validation.ReasonCode))
		}
		return strings.Join(lines, "\n"), nil
	case ClaimLintSchema:
		var result ClaimLintResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		return fmt.Sprintf("deterministic claim lint: ok=%t files=%d findings=%d", result.Report.OK, result.Report.FilesChecked, len(result.Report.Findings)), nil
	case SourceResultSchema:
		var result SourceResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: status=%s fingerprint=%s revision=%s", result.Source.ID, result.Source.Status, result.Source.RawFingerprint, result.Revision), nil
	case SourceListSchema:
		var result SourceListResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		lines := []string{fmt.Sprintf("%d source(s)", len(result.Sources))}
		for _, source := range result.Sources {
			lines = append(lines, fmt.Sprintf("%s: status=%s revision=%s", source.Source.ID, source.Source.Status, source.Revision))
		}
		return strings.Join(lines, "\n"), nil
	case SourceMigrationSchema:
		var result SourceMigrationStatus
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		return fmt.Sprintf("source migration: candidates=%d debt=%d", len(result.Report.Candidates), len(result.Report.Debt)), nil
	case SessionResultSchema:
		var result SessionResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		return fmt.Sprintf("%s: status=%s pending_reads=%d needs_log=%t needs_index=%t needs_source_registry=%t", result.SessionID, result.Status, len(result.MissingRequiredReads), result.NeedsLog, result.NeedsIndex, result.NeedsSourceRegistry), nil
	case SessionListSchema:
		var result SessionListResult
		if err := json.Unmarshal(data, &result); err != nil {
			return "", err
		}
		return fmt.Sprintf("%d active session(s)", len(result.Sessions)), nil
	case CapabilitiesSchema:
		return string(data), nil
	default:
		return "", fmt.Errorf("unsupported public result schema %q", header.SchemaVersion)
	}
}

func ValidateMachineDocument(data []byte) error {
	var header struct {
		SchemaVersion string `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return fmt.Errorf("public result is not JSON: %w", err)
	}
	if strings.TrimSpace(header.SchemaVersion) == "" {
		return fmt.Errorf("public result lacks schema_version")
	}
	return nil
}
