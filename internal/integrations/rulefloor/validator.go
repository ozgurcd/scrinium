// Package rulefloor adapts Rulefloor's versioned JSON CLI to Scrinium's
// transport-independent validation boundary.
package rulefloor

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/validation"
)

const (
	ValidatorID          = "rulefloor"
	BindingSchemaVersion = "rulefloor.binding.v1"
	VersionSchemaVersion = "rulefloor.version.v1"
	ResultSchemaVersion  = "rulefloor.validation.v1"
	defaultExecutable    = "rulefloor"
	maxProcessOutput     = 1 << 20
	maxDiagnosticCount   = 16
	maxDiagnosticLength  = 1024
)

var (
	ruleIDPattern     = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*-[0-9]+$`)
	profilePattern    = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)
	reasonCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,127}$`)
	hex12Pattern      = regexp.MustCompile(`^[0-9a-f]{12}$`)
	hex64Pattern      = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Config contains only trusted process configuration. Claim bindings cannot
// override either the executable or repository root.
type Config struct {
	Executable     string
	RepositoryRoot string
}

// Validator implements validation.Validator through Rulefloor's JSON CLI.
type Validator struct {
	executable     string
	repositoryRoot string
	version        string
	runner         commandRunner
}

type commandRunner interface {
	Run(ctx context.Context, executable string, args ...string) commandResult
}

type commandResult struct {
	stdout          []byte
	stderr          []byte
	exitCode        int
	err             error
	stdoutTruncated bool
	stderrTruncated bool
}

type osCommandRunner struct{}

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := b.limit - b.buffer.Len()
	if remaining > 0 {
		if remaining < len(data) {
			_, _ = b.buffer.Write(data[:remaining])
		} else {
			_, _ = b.buffer.Write(data)
		}
	}
	if original > remaining {
		b.truncated = true
	}
	return original, nil
}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) commandResult {
	stdout := &limitedBuffer{limit: maxProcessOutput}
	stderr := &limitedBuffer{limit: maxProcessOutput}
	command := exec.CommandContext(ctx, executable, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = exitError.ExitCode()
		}
	}
	return commandResult{
		stdout: stdout.buffer.Bytes(), stderr: stderr.buffer.Bytes(), exitCode: exitCode, err: err,
		stdoutTruncated: stdout.truncated, stderrTruncated: stderr.truncated,
	}
}

type versionDocument struct {
	SchemaVersion string `json:"schema_version"`
	Version       string `json:"version"`
}

type validationDocument struct {
	SchemaVersion    string             `json:"schema_version"`
	Command          string             `json:"command"`
	RulefloorVersion string             `json:"rulefloor_version"`
	GeneratedAt      string             `json:"generated_at"`
	Repository       repositoryDocument `json:"repository"`
	Request          requestDocument    `json:"request"`
	Rule             ruleDocument       `json:"rule"`
	Evaluation       evaluationDocument `json:"evaluation"`
}

type repositoryDocument struct {
	Root              string `json:"root"`
	LedgerPath        string `json:"ledger_path"`
	LedgerFingerprint string `json:"ledger_fingerprint"`
}

type requestDocument struct {
	RuleID  string `json:"rule_id"`
	Mode    string `json:"mode"`
	Profile string `json:"profile"`
}

type ruleDocument struct {
	Exists           bool                `json:"exists"`
	Armed            bool                `json:"armed"`
	EnforcedBy       string              `json:"enforced_by"`
	CheckFile        string              `json:"check_file"`
	DeclaredProfile  string              `json:"declared_profile"`
	RedProofStatus   string              `json:"red_proof_status"`
	TestFingerprint  fingerprintDocument `json:"test_fingerprint"`
	ProofFingerprint *string             `json:"proof_fingerprint,omitempty"`
}

type fingerprintDocument struct {
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

type evaluationDocument struct {
	Outcome         string               `json:"outcome"`
	StaticIntegrity string               `json:"static_integrity"`
	Execution       executionDocument    `json:"execution"`
	Reason          string               `json:"reason"`
	Diagnostics     []diagnosticDocument `json:"diagnostics"`
}

type executionDocument struct {
	Requested bool   `json:"requested"`
	Performed bool   `json:"performed"`
	Status    string `json:"status"`
}

type diagnosticDocument struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Test    string `json:"test,omitempty"`
}

type binding struct {
	ruleID  string
	mode    string
	profile string
}

type authenticatedDocument struct {
	completedAt time.Time
	metadata    map[string]string
	diagnostics []knowledge.ValidationDiagnostic
}

// New validates trusted process configuration and discovers the exact
// Rulefloor version used as the generic validator descriptor version.
func New(ctx context.Context, config Config) (*Validator, error) {
	return newValidator(ctx, config, osCommandRunner{})
}

func newValidator(ctx context.Context, config Config, runner commandRunner) (*Validator, error) {
	if runner == nil {
		return nil, fmt.Errorf("rulefloor command runner is required")
	}
	executable := strings.TrimSpace(config.Executable)
	if executable == "" {
		executable = defaultExecutable
	}
	if strings.ContainsRune(executable, '\x00') {
		return nil, fmt.Errorf("rulefloor executable contains a NUL byte")
	}
	if strings.ContainsAny(executable, `/\\`) && !filepath.IsAbs(executable) {
		return nil, fmt.Errorf("configured Rulefloor executable path must be absolute")
	}
	root, err := canonicalDirectory(config.RepositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid Rulefloor repository root: %w", err)
	}
	validator := &Validator{executable: executable, repositoryRoot: root, runner: runner}
	version, err := validator.discoverVersion(ctx)
	if err != nil {
		return nil, err
	}
	validator.version = version
	return validator, nil
}

// Descriptor describes the adapter and the exact Rulefloor binary version
// discovered through rulefloor.version.v1.
func (v *Validator) Descriptor() validation.Descriptor {
	return validation.Descriptor{
		ID: ValidatorID, Version: v.version,
		SupportedBindingVersions: []string{BindingSchemaVersion},
		MaximumAssurance:         knowledge.AssuranceVerification,
	}
}

// ValidateBinding enforces the adapter-owned binding schema. The generic
// binding reference is the Rulefloor rule ID; parameters are limited to mode
// and optional profile.
func (v *Validator) ValidateBinding(candidate knowledge.ValidationBinding) error {
	_, err := parseBinding(candidate)
	return err
}

// Validate invokes only the versioned Rulefloor machine interface and returns
// a generic Scrinium validation result. It never mutates either repository.
func (v *Validator) Validate(ctx context.Context, req validation.Request) (knowledge.ValidationResult, error) {
	parsed, err := parseBinding(req.Binding)
	if err != nil {
		return v.cannotEvaluate(req, "rulefloor_invalid_binding", err.Error(), nil, nil), nil
	}
	if err := ctx.Err(); err != nil {
		return v.contextResult(req, err, nil), nil
	}
	version, err := v.discoverVersion(ctx)
	if err != nil {
		return v.cannotEvaluate(req, "rulefloor_version_unavailable", err.Error(), nil, nil), nil
	}
	if version != v.version {
		return v.cannotEvaluate(req, "rulefloor_version_changed", "Rulefloor version changed after validator registration", nil, map[string]string{"rulefloor.discovered_version": version}), nil
	}

	args := []string{"validate", parsed.ruleID, "--repo", v.repositoryRoot, "--mode", parsed.mode}
	if parsed.profile != "" {
		args = append(args, "--profile", parsed.profile)
	}
	args = append(args, "--json")
	process := v.runner.Run(ctx, v.executable, args...)
	if err := ctx.Err(); err != nil {
		return v.contextResult(req, err, &process), nil
	}
	if process.stdoutTruncated {
		return v.cannotEvaluate(req, "rulefloor_output_too_large", "Rulefloor JSON output exceeded the adapter limit", &process, nil), nil
	}

	var document validationDocument
	if err := decodeStrict(process.stdout, &document); err != nil {
		return v.cannotEvaluate(req, "rulefloor_malformed_output", "Rulefloor did not return one valid validation JSON document", &process, nil), nil
	}
	if err := requireValidationDocumentFields(process.stdout); err != nil {
		return v.cannotEvaluate(req, "rulefloor_malformed_output", err.Error(), &process, nil), nil
	}
	authenticated, authenticityErr := v.authenticate(req, parsed, document, process.exitCode)
	if authenticityErr != nil {
		return v.cannotEvaluate(req, "rulefloor_inauthentic_output", authenticityErr.Error(), &process, authenticated.metadata), nil
	}
	outcome := knowledge.ValidationOutcome(document.Evaluation.Outcome)
	assurance := assuranceFor(document)
	result := v.result(req, outcome, assurance, document.Evaluation.Reason, resultReason(parsed.ruleID, document), authenticated.completedAt)
	result.Metadata = authenticated.metadata
	result.Diagnostics = authenticated.diagnostics
	return result, nil
}

func parseBinding(candidate knowledge.ValidationBinding) (binding, error) {
	if candidate.ValidatorID != ValidatorID {
		return binding{}, &validation.Error{Code: "invalid_binding_schema", Message: "binding validator_id must be rulefloor"}
	}
	if candidate.BindingVersion != BindingSchemaVersion {
		return binding{}, &validation.Error{Code: "unsupported_binding_version", Message: "unsupported Rulefloor binding version"}
	}
	if len(candidate.Reference) > 128 || !ruleIDPattern.MatchString(candidate.Reference) {
		return binding{}, &validation.Error{Code: "invalid_binding_schema", Message: "Rulefloor reference must be a safe uppercase semantic rule ID with a numeric suffix"}
	}
	for key := range candidate.Parameters {
		if key != "mode" && key != "profile" {
			return binding{}, &validation.Error{Code: "invalid_binding_schema", Message: "Rulefloor binding contains unsupported parameter " + key}
		}
	}
	mode := candidate.Parameters["mode"]
	if mode != "static" && mode != "execute" {
		return binding{}, &validation.Error{Code: "invalid_binding_schema", Message: "Rulefloor mode must be static or execute"}
	}
	profile := candidate.Parameters["profile"]
	if mode == "static" && profile != "" {
		return binding{}, &validation.Error{Code: "invalid_binding_schema", Message: "Rulefloor static mode does not accept a profile"}
	}
	if profile != "" && !profilePattern.MatchString(profile) {
		return binding{}, &validation.Error{Code: "invalid_binding_schema", Message: "Rulefloor profile has an invalid format"}
	}
	return binding{ruleID: candidate.Reference, mode: mode, profile: profile}, nil
}

func (v *Validator) discoverVersion(ctx context.Context) (string, error) {
	process := v.runner.Run(ctx, v.executable, "version", "--json")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if process.err != nil || process.exitCode != 0 || process.stdoutTruncated {
		return "", fmt.Errorf("rulefloor version command was unavailable or unsuccessful")
	}
	var document versionDocument
	if err := decodeStrict(process.stdout, &document); err != nil {
		return "", fmt.Errorf("invalid Rulefloor version JSON: %w", err)
	}
	if err := requireFields(process.stdout, []string{"schema_version", "version"}); err != nil {
		return "", fmt.Errorf("invalid Rulefloor version JSON: %w", err)
	}
	if document.SchemaVersion != VersionSchemaVersion {
		return "", fmt.Errorf("unsupported Rulefloor version schema %q", document.SchemaVersion)
	}
	if strings.TrimSpace(document.Version) == "" || len(document.Version) > 128 {
		return "", fmt.Errorf("rulefloor version is structurally invalid")
	}
	return document.Version, nil
}

func (v *Validator) authenticate(req validation.Request, requested binding, document validationDocument, exitCode int) (authenticatedDocument, error) {
	authenticated := authenticatedDocument{
		metadata:    documentMetadata(req, document),
		diagnostics: convertDiagnostics(document.Evaluation.Diagnostics),
	}
	if err := validateDocumentFields(document); err != nil {
		return authenticated, err
	}
	generatedAt, err := time.Parse(time.RFC3339Nano, document.GeneratedAt)
	if err != nil || generatedAt.Location() != time.UTC || generatedAt.Before(req.StartedAt) || generatedAt.After(time.Now().UTC().Add(time.Second)) {
		return authenticated, fmt.Errorf("rulefloor generated_at is outside the validation attempt")
	}
	if document.RulefloorVersion != v.version {
		return authenticated, fmt.Errorf("rulefloor validation version does not match version discovery")
	}
	if document.Request.RuleID != requested.ruleID || document.Request.Mode != requested.mode || document.Request.Profile != requested.profile {
		return authenticated, fmt.Errorf("rulefloor response does not match the requested rule, mode, and profile")
	}
	if err := v.validateRepository(document.Repository); err != nil {
		return authenticated, err
	}
	expectedExit := map[string]int{"pass": 0, "fail": 1, "cannot_evaluate": 2}[document.Evaluation.Outcome]
	if exitCode != expectedExit {
		return authenticated, fmt.Errorf("rulefloor outcome %s is inconsistent with exit code %d", document.Evaluation.Outcome, exitCode)
	}
	if err := validateEvaluation(requested, document); err != nil {
		return authenticated, err
	}
	authenticated.completedAt = generatedAt
	return authenticated, nil
}

func validateDocumentFields(document validationDocument) error {
	if err := validateDocumentEnvelope(document); err != nil {
		return err
	}
	if err := validateRuleFields(document.Rule); err != nil {
		return err
	}
	return validateDiagnosticFields(document.Evaluation.Diagnostics)
}

func validateDocumentEnvelope(document validationDocument) error {
	if document.SchemaVersion != ResultSchemaVersion || document.Command != "validate" {
		return fmt.Errorf("unsupported Rulefloor validation schema or command")
	}
	if strings.TrimSpace(document.RulefloorVersion) == "" || !reasonCodePattern.MatchString(document.Evaluation.Reason) {
		return fmt.Errorf("rulefloor version or reason code is structurally invalid")
	}
	if document.Evaluation.Outcome != "pass" && document.Evaluation.Outcome != "fail" && document.Evaluation.Outcome != "cannot_evaluate" {
		return fmt.Errorf("rulefloor outcome is invalid")
	}
	if !validStatus(document.Evaluation.StaticIntegrity) || !validStatus(document.Evaluation.Execution.Status) {
		return fmt.Errorf("rulefloor evaluation status is invalid")
	}
	if document.Repository.Root == "" || document.Repository.LedgerPath == "" || !hex64Pattern.MatchString(document.Repository.LedgerFingerprint) {
		return fmt.Errorf("rulefloor repository fields are incomplete")
	}
	return nil
}

func validateRuleFields(rule ruleDocument) error {
	if rule.RedProofStatus != "present" && rule.RedProofStatus != "missing" && rule.RedProofStatus != "not_applicable" {
		return fmt.Errorf("rulefloor red-proof status is invalid")
	}
	if err := validateFingerprintPair(rule.TestFingerprint); err != nil {
		return err
	}
	if rule.ProofFingerprint != nil && !hex64Pattern.MatchString(*rule.ProofFingerprint) {
		return fmt.Errorf("rulefloor proof fingerprint is invalid")
	}
	if rule.RedProofStatus == "present" && rule.ProofFingerprint == nil {
		return fmt.Errorf("rulefloor present red proof is missing its fingerprint")
	}
	if rule.RedProofStatus != "present" && rule.ProofFingerprint != nil {
		return fmt.Errorf("rulefloor returned a proof fingerprint without a present red proof")
	}
	return nil
}

func validateDiagnosticFields(diagnostics []diagnosticDocument) error {
	if diagnostics == nil {
		return fmt.Errorf("rulefloor diagnostics must be an array")
	}
	for _, diagnostic := range diagnostics {
		if !reasonCodePattern.MatchString(diagnostic.Code) || strings.TrimSpace(diagnostic.Message) == "" {
			return fmt.Errorf("rulefloor diagnostic is structurally invalid")
		}
	}
	return nil
}

func validateEvaluation(requested binding, document validationDocument) error {
	execution := document.Evaluation.Execution
	if err := validateExecutionState(execution); err != nil {
		return err
	}
	if requested.mode == "static" {
		return validateStaticEvaluation(document)
	}
	return validateExecuteEvaluation(requested, document)
}

func validateExecutionState(execution executionDocument) error {
	if execution.Performed && (execution.Status == "not_performed" || execution.Status == "not_requested") {
		return fmt.Errorf("rulefloor performed execution has a non-execution status")
	}
	if !execution.Performed && (execution.Status == "pass" || execution.Status == "fail") {
		return fmt.Errorf("rulefloor unperformed execution has an evaluated status")
	}
	return nil
}

func validateStaticEvaluation(document validationDocument) error {
	execution := document.Evaluation.Execution
	if execution.Requested || execution.Performed || execution.Status != "not_requested" {
		return fmt.Errorf("rulefloor static result claims execution state")
	}
	switch document.Evaluation.Outcome {
	case "pass":
		if document.Evaluation.StaticIntegrity != "pass" {
			return fmt.Errorf("rulefloor static pass lacks passing integrity")
		}
		return validatePassingRule(document)
	case "fail":
		if document.Evaluation.StaticIntegrity != "fail" {
			return fmt.Errorf("rulefloor static failure lacks failed integrity")
		}
	case "cannot_evaluate":
		if document.Evaluation.StaticIntegrity != "cannot_evaluate" && document.Evaluation.StaticIntegrity != "not_performed" {
			return fmt.Errorf("rulefloor static uncertainty has an evaluated integrity status")
		}
	}
	return nil
}

func validateExecuteEvaluation(requested binding, document validationDocument) error {
	execution := document.Evaluation.Execution
	if !execution.Requested {
		return fmt.Errorf("rulefloor execute result does not record requested execution")
	}
	switch document.Evaluation.Outcome {
	case "pass":
		if document.Evaluation.StaticIntegrity != "pass" || !execution.Performed || execution.Status != "pass" {
			return fmt.Errorf("rulefloor execute pass lacks successful performed execution")
		}
		if requested.profile != "" && document.Rule.DeclaredProfile != requested.profile {
			return fmt.Errorf("rulefloor execute pass does not match the declared profile")
		}
		return validatePassingRule(document)
	case "fail":
		staticFailure := document.Evaluation.StaticIntegrity == "fail" && !execution.Performed
		executionFailure := document.Evaluation.StaticIntegrity == "pass" && execution.Performed && execution.Status == "fail"
		if !staticFailure && !executionFailure {
			return fmt.Errorf("rulefloor execute failure is not attributable to static integrity or performed execution")
		}
	}
	return nil
}

func validatePassingRule(document validationDocument) error {
	if !document.Rule.Exists || !document.Rule.Armed || strings.TrimSpace(document.Rule.EnforcedBy) == "" || strings.TrimSpace(document.Rule.CheckFile) == "" || strings.TrimSpace(document.Rule.DeclaredProfile) == "" {
		return fmt.Errorf("rulefloor pass lacks an existing armed check binding")
	}
	if document.Rule.RedProofStatus != "present" || document.Rule.TestFingerprint.Expected == "" || document.Rule.TestFingerprint.Expected != document.Rule.TestFingerprint.Actual {
		return fmt.Errorf("rulefloor pass lacks matching test and proof integrity")
	}
	return nil
}

func (v *Validator) validateRepository(repository repositoryDocument) error {
	root, err := canonicalDirectory(repository.Root)
	if err != nil || root != v.repositoryRoot {
		return fmt.Errorf("rulefloor repository root does not match the bound Scrinium repository")
	}
	expectedLedger := filepath.Join(v.repositoryRoot, "RULE-FLOOR.md")
	ledgerPath, err := filepath.Abs(repository.LedgerPath)
	if err == nil {
		ledgerPath, err = filepath.EvalSymlinks(ledgerPath)
	}
	if err != nil || filepath.Clean(ledgerPath) != expectedLedger {
		return fmt.Errorf("rulefloor ledger path escapes or differs from the bound repository")
	}
	info, err := os.Lstat(expectedLedger)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("rulefloor ledger is unavailable as a regular repository file")
	}
	data, err := os.ReadFile(expectedLedger)
	if err != nil {
		return fmt.Errorf("rulefloor ledger cannot be fingerprinted")
	}
	digest := sha256.Sum256(data)
	if hex.EncodeToString(digest[:]) != repository.LedgerFingerprint {
		return fmt.Errorf("rulefloor ledger fingerprint does not match repository bytes")
	}
	return nil
}

func validateFingerprintPair(fingerprint fingerprintDocument) error {
	if fingerprint.Expected == "" && fingerprint.Actual == "" {
		return nil
	}
	if !hex12Pattern.MatchString(fingerprint.Expected) || !hex12Pattern.MatchString(fingerprint.Actual) {
		return fmt.Errorf("rulefloor test fingerprints are invalid")
	}
	return nil
}

func validStatus(status string) bool {
	switch status {
	case "pass", "fail", "cannot_evaluate", "not_performed", "not_requested":
		return true
	default:
		return false
	}
}

func assuranceFor(document validationDocument) knowledge.Assurance {
	execution := document.Evaluation.Execution
	if document.Evaluation.Outcome != "cannot_evaluate" && document.Request.Mode == "execute" && execution.Requested && execution.Performed && (execution.Status == "pass" || execution.Status == "fail") {
		return knowledge.AssuranceVerification
	}
	return knowledge.AssuranceObservation
}

func resultReason(ruleID string, document validationDocument) string {
	if len(document.Evaluation.Diagnostics) > 0 {
		return truncate(document.Evaluation.Diagnostics[0].Message, maxDiagnosticLength)
	}
	return fmt.Sprintf("Rulefloor reported %s for rule %s", document.Evaluation.Reason, ruleID)
}

func (v *Validator) result(req validation.Request, outcome knowledge.ValidationOutcome, assurance knowledge.Assurance, code, reason string, completed time.Time) knowledge.ValidationResult {
	result := knowledge.ValidationResult{
		ID: req.ResultID, BindingID: req.Binding.ID, ValidatorID: ValidatorID,
		ValidatorVersion: v.version, BindingVersion: req.Binding.BindingVersion,
		RepositoryRevision: req.Repository.Revision, SnapshotFingerprint: req.Repository.Fingerprint,
		InputFingerprint: req.InputFingerprint, Assurance: assurance, Outcome: outcome,
		ReasonCode: code, Reason: reason, EvidenceIDs: append([]string{}, req.Binding.EvidenceIDs...),
		StartedAt: req.StartedAt, CompletedAt: completed,
	}
	if req.Binding.ValidForSeconds > 0 {
		validUntil := completed.Add(time.Duration(req.Binding.ValidForSeconds) * time.Second)
		result.ValidUntil = &validUntil
	}
	return result
}

func (v *Validator) cannotEvaluate(req validation.Request, code, reason string, process *commandResult, metadata map[string]string) knowledge.ValidationResult {
	if metadata == nil {
		metadata = make(map[string]string)
	}
	metadata["repository.fingerprint"] = req.Repository.Fingerprint
	metadata["validation.input_fingerprint"] = req.InputFingerprint
	metadata["rulefloor.version"] = v.version
	diagnostics := make([]knowledge.ValidationDiagnostic, 0, 1)
	if process != nil {
		if message := strings.TrimSpace(string(process.stderr)); message != "" {
			diagnostics = append(diagnostics, knowledge.ValidationDiagnostic{Code: "rulefloor_stderr", Message: truncate(message, maxDiagnosticLength)})
		}
		if process.stderrTruncated {
			diagnostics = append(diagnostics, knowledge.ValidationDiagnostic{Code: "rulefloor_stderr_truncated", Message: "Rulefloor stderr exceeded the adapter limit"})
		}
	}
	result := v.result(req, knowledge.OutcomeCannotEvaluate, knowledge.AssuranceObservation, code, truncate(reason, maxDiagnosticLength), time.Now().UTC())
	result.Metadata = metadata
	result.Diagnostics = diagnostics
	return result
}

func (v *Validator) contextResult(req validation.Request, err error, process *commandResult) knowledge.ValidationResult {
	code := "rulefloor_context_canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "rulefloor_deadline_exceeded"
	}
	return v.cannotEvaluate(req, code, err.Error(), process, nil)
}

func documentMetadata(req validation.Request, document validationDocument) map[string]string {
	metadata := make(map[string]string)
	putMetadata(metadata, "repository.fingerprint", req.Repository.Fingerprint)
	putMetadata(metadata, "validation.input_fingerprint", req.InputFingerprint)
	putMetadata(metadata, "rulefloor.version", document.RulefloorVersion)
	putMetadata(metadata, "rulefloor.rule_id", document.Request.RuleID)
	putMetadata(metadata, "rulefloor.mode", document.Request.Mode)
	putMetadata(metadata, "rulefloor.ledger_fingerprint", document.Repository.LedgerFingerprint)
	putMetadata(metadata, "rulefloor.reason_code", document.Evaluation.Reason)
	putMetadata(metadata, "rulefloor.static_integrity", document.Evaluation.StaticIntegrity)
	putMetadata(metadata, "rulefloor.red_proof_status", document.Rule.RedProofStatus)
	putMetadata(metadata, "rulefloor.execution.requested", strconv.FormatBool(document.Evaluation.Execution.Requested))
	putMetadata(metadata, "rulefloor.execution.performed", strconv.FormatBool(document.Evaluation.Execution.Performed))
	putMetadata(metadata, "rulefloor.execution.status", document.Evaluation.Execution.Status)
	if document.Request.Profile != "" {
		metadata["rulefloor.profile"] = document.Request.Profile
	}
	if document.Rule.TestFingerprint.Expected != "" {
		metadata["rulefloor.test_fingerprint.expected"] = document.Rule.TestFingerprint.Expected
		metadata["rulefloor.test_fingerprint.actual"] = document.Rule.TestFingerprint.Actual
	}
	if document.Rule.ProofFingerprint != nil {
		metadata["rulefloor.proof_fingerprint"] = *document.Rule.ProofFingerprint
	}
	return metadata
}

func putMetadata(metadata map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		metadata[key] = value
	}
}

func convertDiagnostics(source []diagnosticDocument) []knowledge.ValidationDiagnostic {
	limit := len(source)
	if limit > maxDiagnosticCount {
		limit = maxDiagnosticCount
	}
	result := make([]knowledge.ValidationDiagnostic, 0, limit+1)
	for _, diagnostic := range source[:limit] {
		result = append(result, knowledge.ValidationDiagnostic{
			Code: diagnostic.Code, Message: truncate(diagnostic.Message, maxDiagnosticLength),
			Path: truncate(diagnostic.Path, 256), Target: truncate(diagnostic.Test, 256),
		})
	}
	if len(source) > limit {
		result = append(result, knowledge.ValidationDiagnostic{Code: "rulefloor_diagnostics_truncated", Message: "Additional Rulefloor diagnostics were omitted"})
	}
	return result
}

func canonicalDirectory(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("repository root is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(real)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("repository root is not a directory")
	}
	return filepath.Clean(real), nil
}

func decodeStrict(data []byte, target any) error {
	if len(data) == 0 {
		return fmt.Errorf("empty JSON document")
	}
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder, "$", nil); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder, path string, first json.Token) error {
	token := first
	var err error
	if token == nil {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]bool)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid object key at %s", path)
			}
			if keys[key] {
				return fmt.Errorf("duplicate JSON key %q at %s", key, path)
			}
			keys[key] = true
			if err := scanJSONValue(decoder, path+"."+key, nil); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), nil); err != nil {
				return err
			}
			index++
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return err
}

func requireFields(data []byte, fields []string) error {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return err
	}
	for _, field := range fields {
		if _, exists := object[field]; !exists {
			return fmt.Errorf("missing required field %s", field)
		}
	}
	return nil
}

func requireValidationDocumentFields(data []byte) error {
	top, err := rawObject(data, "validation document")
	if err != nil {
		return err
	}
	if err := requireRawFields(top, "validation document", "schema_version", "command", "rulefloor_version", "generated_at", "repository", "request", "rule", "evaluation"); err != nil {
		return err
	}
	repository, err := rawObject(top["repository"], "repository")
	if err != nil {
		return err
	}
	if err := requireRawFields(repository, "repository", "root", "ledger_path", "ledger_fingerprint"); err != nil {
		return err
	}
	request, err := rawObject(top["request"], "request")
	if err != nil {
		return err
	}
	if err := requireRawFields(request, "request", "rule_id", "mode", "profile"); err != nil {
		return err
	}
	rule, err := rawObject(top["rule"], "rule")
	if err != nil {
		return err
	}
	if err := requireRawFields(rule, "rule", "exists", "armed", "enforced_by", "check_file", "declared_profile", "red_proof_status", "test_fingerprint"); err != nil {
		return err
	}
	testFingerprint, err := rawObject(rule["test_fingerprint"], "test_fingerprint")
	if err != nil {
		return err
	}
	if err := requireRawFields(testFingerprint, "test_fingerprint", "expected", "actual"); err != nil {
		return err
	}
	evaluation, err := rawObject(top["evaluation"], "evaluation")
	if err != nil {
		return err
	}
	if err := requireRawFields(evaluation, "evaluation", "outcome", "static_integrity", "execution", "reason", "diagnostics"); err != nil {
		return err
	}
	execution, err := rawObject(evaluation["execution"], "execution")
	if err != nil {
		return err
	}
	return requireRawFields(execution, "execution", "requested", "performed", "status")
}

func rawObject(data []byte, name string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%s must be a JSON object", name)
	}
	return object, nil
}

func requireRawFields(object map[string]json.RawMessage, name string, fields ...string) error {
	for _, field := range fields {
		if _, exists := object[field]; !exists {
			return fmt.Errorf("%s is missing required field %s", name, field)
		}
	}
	return nil
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}
