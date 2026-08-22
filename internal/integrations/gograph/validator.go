package gograph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/validation"
)

var (
	versionPattern = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z._+-]{0,127}$`)
	codePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]{1,127}$`)
	hex64Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// Target names are abstract allowlist keys, never filesystem paths.
	targetNamePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)
)

var knownReasons = map[string]struct{}{
	"predicate_passed": {}, "predicate_failed": {}, "symbol_not_found": {}, "symbol_ambiguous": {},
	"package_not_found": {}, "relation_not_found": {}, "graph_missing": {}, "graph_invalid": {},
	"graph_schema_unsupported": {}, "source_policy_unsupported": {}, "graph_stale": {},
	"precision_insufficient": {}, "analysis_incomplete": {}, "symbol_identity_unstable": {},
	"unsupported_predicate": {}, "unsupported_language": {}, "repository_mismatch": {},
	"invalid_request": {}, "internal_error": {},
}

// Validator implements validation.Validator through Gograph's closed JSON CLI.
type Validator struct {
	executable     string
	repositoryRoot string
	targets        map[string]string
	version        string
	runner         commandRunner
}

// New validates trusted process configuration and discovers the Gograph
// protocol version. An unavailable executable returns an error so application
// startup can omit the optional validator without failing.
func New(ctx context.Context, config Config) (*Validator, error) {
	return newValidator(ctx, config, osCommandRunner{})
}

func newValidator(ctx context.Context, config Config, runner commandRunner) (*Validator, error) {
	if runner == nil {
		return nil, fmt.Errorf("gograph command runner is required")
	}
	executable := strings.TrimSpace(config.Executable)
	if executable == "" {
		executable = defaultExecutable
	}
	if strings.ContainsRune(executable, '\x00') {
		return nil, fmt.Errorf("gograph executable contains a NUL byte")
	}
	if strings.ContainsAny(executable, `/\\`) && !filepath.IsAbs(executable) {
		return nil, fmt.Errorf("configured Gograph executable path must be absolute")
	}
	root, err := canonicalDirectory(config.RepositoryRoot)
	if err != nil {
		return nil, fmt.Errorf("invalid Gograph repository root: %w", err)
	}
	targets := make(map[string]string, len(config.Targets))
	for name, targetRoot := range config.Targets {
		if !targetNamePattern.MatchString(name) {
			return nil, fmt.Errorf("invalid Gograph validation target name %q", name)
		}
		canonical, targetErr := canonicalDirectory(targetRoot)
		if targetErr != nil {
			return nil, fmt.Errorf("invalid Gograph validation target %q: %w", name, targetErr)
		}
		targets[name] = canonical
	}
	validator := &Validator{executable: executable, repositoryRoot: root, targets: targets, runner: runner}
	version, err := validator.discoverVersion(ctx)
	if err != nil {
		return nil, err
	}
	validator.version = version
	return validator, nil
}

func (v *Validator) Descriptor() validation.Descriptor {
	return validation.Descriptor{
		ID: ValidatorID, Version: v.version,
		SupportedBindingVersions: []string{BindingSchemaVersion},
		MaximumAssurance:         knowledge.AssuranceObservation,
	}
}

func (v *Validator) ValidateBinding(candidate knowledge.ValidationBinding) error {
	_, err := parseBinding(candidate)
	return err
}

func (v *Validator) Validate(ctx context.Context, req validation.Request) (knowledge.ValidationResult, error) {
	parsed, err := parseBinding(req.Binding)
	if err != nil {
		return v.cannotEvaluate(req, "gograph_invalid_binding", err.Error(), nil, nil), nil
	}
	if err := ctx.Err(); err != nil {
		return v.contextResult(req, err, nil), nil
	}
	version, err := v.discoverVersion(ctx)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return v.contextResult(req, contextErr, nil), nil
		}
		return v.cannotEvaluate(req, "gograph_version_unavailable", err.Error(), nil, nil), nil
	}
	if version != v.version {
		return v.cannotEvaluate(req, "gograph_version_changed", "Gograph version changed after validator registration", nil, map[string]string{"gograph.discovered_version": version}), nil
	}

	root := v.repositoryRoot
	if parsed.target != "" {
		allowlisted, ok := v.targets[parsed.target]
		if !ok {
			return v.cannotEvaluate(req, "gograph_unknown_target", fmt.Sprintf("binding names validation target %q, which is not allowlisted in scrinium.json validation_targets", parsed.target), nil, nil), nil
		}
		current, rootErr := canonicalDirectory(allowlisted)
		if rootErr != nil || current != allowlisted {
			return v.cannotEvaluate(req, "gograph_target_unavailable", fmt.Sprintf("validation target %q no longer resolves to its registered directory", parsed.target), nil, nil), nil
		}
		root = allowlisted
	}

	process := v.runner.Run(ctx, v.executable, "validate", "--repo", root, "--binding-json", parsed.json, "--json")
	if err := ctx.Err(); err != nil {
		return v.contextResult(req, err, &process), nil
	}
	if process.stdoutTruncated {
		return v.cannotEvaluate(req, "gograph_output_too_large", "Gograph JSON output exceeded the adapter limit", &process, nil), nil
	}
	var document validationDocument
	if err := decodeStrict(process.stdout, &document); err != nil {
		return v.cannotEvaluate(req, "gograph_malformed_output", "Gograph did not return one valid validation JSON document", &process, nil), nil
	}
	if err := requireValidationFields(process.stdout); err != nil {
		return v.cannotEvaluate(req, "gograph_malformed_output", err.Error(), &process, nil), nil
	}
	authenticated, authenticityErr := v.authenticate(req, parsed, root, document, process.exitCode)
	if authenticityErr != nil {
		return v.cannotEvaluate(req, "gograph_inauthentic_output", authenticityErr.Error(), &process, nil), nil
	}
	result := v.result(req, knowledge.ValidationOutcome(document.Evaluation.Outcome), document.Evaluation.Reason, resultReason(parsed.document, document), authenticated.completedAt)
	result.Metadata = authenticated.metadata
	result.Diagnostics = authenticated.diagnostics
	return result, nil
}

func (v *Validator) discoverVersion(ctx context.Context) (string, error) {
	process := v.runner.Run(ctx, v.executable, "version", "--json")
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if process.err != nil || process.exitCode != 0 || process.stdoutTruncated {
		return "", fmt.Errorf("gograph version command was unavailable or unsuccessful")
	}
	var document versionDocument
	if err := decodeStrict(process.stdout, &document); err != nil {
		return "", fmt.Errorf("invalid Gograph version JSON: %w", err)
	}
	if err := requireFields(process.stdout, "schema_version", "version"); err != nil {
		return "", fmt.Errorf("invalid Gograph version JSON: %w", err)
	}
	if document.SchemaVersion != VersionSchemaVersion {
		return "", fmt.Errorf("unsupported Gograph version schema %q", document.SchemaVersion)
	}
	if !versionPattern.MatchString(document.Version) {
		return "", fmt.Errorf("gograph version is structurally invalid")
	}
	return document.Version, nil
}

func (v *Validator) authenticate(req validation.Request, requested binding, root string, document validationDocument, exitCode int) (authenticatedDocument, error) {
	authenticated := authenticatedDocument{
		metadata:    documentMetadata(req, requested, document),
		diagnostics: convertDiagnostics(document.Evaluation.Diagnostics),
	}
	if err := validateDocument(document, requested.document); err != nil {
		return authenticated, err
	}
	generatedAt, err := parseUTCTimestamp(document.GeneratedAt)
	if err != nil || generatedAt.Before(req.StartedAt) || generatedAt.After(time.Now().UTC().Add(time.Second)) {
		return authenticated, fmt.Errorf("gograph generated_at is outside the validation attempt")
	}
	if document.GographVersion != v.version {
		return authenticated, fmt.Errorf("gograph validation version does not match version discovery")
	}
	if document.Request.Binding == nil || !sameBinding(*document.Request.Binding, requested.document) {
		return authenticated, fmt.Errorf("gograph response binding does not match the requested predicate")
	}
	if document.Request.BindingFingerprint != requested.fingerprint {
		return authenticated, fmt.Errorf("gograph binding fingerprint does not match the canonical request")
	}
	if err := v.validateRepository(root, document.Repository); err != nil {
		return authenticated, err
	}
	expectedExit := map[string]int{"pass": 0, "fail": 1, "cannot_evaluate": 2}[document.Evaluation.Outcome]
	if exitCode != expectedExit {
		return authenticated, fmt.Errorf("gograph outcome %s is inconsistent with exit code %d", document.Evaluation.Outcome, exitCode)
	}
	if err := v.validateAnalysis(document); err != nil {
		return authenticated, err
	}
	if err := validateEvidence(document.Evidence, requested.document, document.Evaluation.Outcome); err != nil {
		return authenticated, err
	}
	authenticated.completedAt = generatedAt
	return authenticated, nil
}

func validateDocument(document validationDocument, requested bindingDocument) error {
	if document.SchemaVersion != ResultSchemaVersion || document.Command != "validate" {
		return fmt.Errorf("unsupported Gograph validation schema or command")
	}
	if !versionPattern.MatchString(document.GographVersion) {
		return fmt.Errorf("gograph version is structurally invalid")
	}
	if _, known := knownReasons[document.Evaluation.Reason]; !known {
		return fmt.Errorf("gograph reason code is unsupported")
	}
	if document.Evaluation.Outcome != "pass" && document.Evaluation.Outcome != "fail" && document.Evaluation.Outcome != "cannot_evaluate" {
		return fmt.Errorf("gograph outcome is invalid")
	}
	if document.Evaluation.Diagnostics == nil || len(document.Evaluation.Diagnostics) > maxDiagnosticCount {
		return fmt.Errorf("gograph diagnostics are missing or exceed the contract limit")
	}
	for _, diagnostic := range document.Evaluation.Diagnostics {
		if !codePattern.MatchString(diagnostic.Code) || strings.TrimSpace(diagnostic.Message) == "" || len(diagnostic.Message) > maxDiagnosticLength {
			return fmt.Errorf("gograph diagnostic is structurally invalid")
		}
		if err := validateLocationPath(diagnostic.Path, true); err != nil {
			return err
		}
	}
	if document.Evaluation.Outcome == "pass" && document.Evaluation.Reason != "predicate_passed" {
		return fmt.Errorf("gograph pass has an inconsistent reason code")
	}
	if document.Evaluation.Outcome == "fail" && !evaluatedFailureReason(document.Evaluation.Reason) {
		return fmt.Errorf("gograph fail has a non-evaluated reason code")
	}
	if document.Evaluation.Outcome == "cannot_evaluate" && (document.Evaluation.Reason == "predicate_passed" || document.Evaluation.Reason == "predicate_failed") {
		return fmt.Errorf("gograph uncertainty has an evaluated reason code")
	}
	if document.Request.Binding != nil && document.Request.Binding.Predicate != requested.Predicate {
		return fmt.Errorf("gograph returned the wrong predicate")
	}
	return nil
}

func evaluatedFailureReason(reason string) bool {
	switch reason {
	case "predicate_failed", "symbol_not_found", "package_not_found", "relation_not_found":
		return true
	default:
		return false
	}
}

func (v *Validator) validateRepository(boundRoot string, repository repositoryDocument) error {
	root, err := canonicalDirectory(repository.Root)
	if err != nil || root != boundRoot {
		return fmt.Errorf("gograph repository root does not match the bound validation root")
	}
	if repository.SourceFingerprint != "" && !hex64Pattern.MatchString(repository.SourceFingerprint) {
		return fmt.Errorf("gograph source fingerprint is invalid")
	}
	if len(repository.GitRevision) > 256 || strings.ContainsAny(repository.GitRevision, "\r\n\x00") {
		return fmt.Errorf("gograph Git revision is structurally invalid")
	}
	return nil
}

func (v *Validator) validateAnalysis(document validationDocument) error {
	analysis := document.Analysis
	evaluated := document.Evaluation.Outcome == "pass" || document.Evaluation.Outcome == "fail"
	if analysis.GraphFingerprint != "" && !hex64Pattern.MatchString(analysis.GraphFingerprint) {
		return fmt.Errorf("gograph graph fingerprint is invalid")
	}
	if analysis.BuildContextFingerprint != "" && !hex64Pattern.MatchString(analysis.BuildContextFingerprint) {
		return fmt.Errorf("gograph build-context fingerprint is invalid")
	}
	if analysis.Mode != "" && analysis.Mode != "ast" && analysis.Mode != "precise" && analysis.Mode != "precise_fallback" {
		return fmt.Errorf("gograph analysis mode is invalid")
	}
	if analysis.Precision != "" && analysis.Precision != "ast" && analysis.Precision != "precise" && analysis.Precision != "precise_fallback" {
		return fmt.Errorf("gograph precision is invalid")
	}
	if analysis.Completeness != "" && analysis.Completeness != "complete" && analysis.Completeness != "partial" {
		return fmt.Errorf("gograph completeness is invalid")
	}
	if analysis.Freshness != "" && analysis.Freshness != "current" && analysis.Freshness != "stale" && analysis.Freshness != "unknown" {
		return fmt.Errorf("gograph freshness is invalid")
	}
	if analysis.GraphGeneratedAt != nil {
		generated, err := parseUTCTimestamp(*analysis.GraphGeneratedAt)
		if err != nil || generated.After(time.Now().UTC().Add(time.Second)) {
			return fmt.Errorf("gograph graph timestamp is invalid")
		}
	}
	if evaluated {
		if !hex64Pattern.MatchString(document.Repository.SourceFingerprint) || !hex64Pattern.MatchString(analysis.GraphFingerprint) || !hex64Pattern.MatchString(analysis.BuildContextFingerprint) {
			return fmt.Errorf("gograph evaluated result lacks required fingerprints")
		}
		if analysis.GraphSchemaVersion == "" || analysis.SourcePolicyVersion < 1 || analysis.Completeness != "complete" || analysis.Freshness != "current" || analysis.GraphGeneratedAt == nil {
			return fmt.Errorf("gograph evaluated result lacks current complete analysis")
		}
		graphGenerated, graphErr := parseUTCTimestamp(*analysis.GraphGeneratedAt)
		resultGenerated, resultErr := parseUTCTimestamp(document.GeneratedAt)
		if graphErr != nil || resultErr != nil || graphGenerated.After(resultGenerated) {
			return fmt.Errorf("gograph graph timestamp is inconsistent with the validation result")
		}
		if document.Request.Binding.RequiredPrecision == precisionPrecise && (analysis.Precision != "precise" || analysis.Mode != "precise") {
			return fmt.Errorf("gograph evaluated result lacks required precise analysis")
		}
		if document.Evaluation.Outcome == "fail" && analysis.Precision == "precise_fallback" {
			return fmt.Errorf("gograph precise fallback cannot establish absence")
		}
	}
	if analysis.GraphFingerprint != "" {
		actual, err := v.currentGraphFingerprint()
		if err != nil || actual != analysis.GraphFingerprint {
			return fmt.Errorf("gograph graph fingerprint does not match the persisted graph")
		}
	}
	return nil
}

func (v *Validator) currentGraphFingerprint() (string, error) {
	directory := filepath.Join(v.repositoryRoot, ".gograph")
	directoryInfo, err := os.Lstat(directory)
	if err != nil || !directoryInfo.IsDir() || directoryInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("gograph artifact directory is unavailable")
	}
	graphPath := filepath.Join(directory, "graph.json")
	info, err := os.Lstat(graphPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("gograph graph is unavailable as a regular repository file")
	}
	data, err := os.ReadFile(graphPath)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func validateEvidence(evidence evidenceDocument, requested bindingDocument, outcome string) error {
	if evidence.MatchedRelations == nil {
		return fmt.Errorf("gograph matched_relations must be an array")
	}
	if evidence.ResolvedSubject != nil {
		if err := validateResolvedReference(*evidence.ResolvedSubject, requested.Subject); err != nil {
			return err
		}
	}
	if requested.Object == nil {
		if evidence.ResolvedObject != nil || len(evidence.MatchedRelations) != 0 {
			return fmt.Errorf("gograph symbol_exists evidence contains an unexpected object or relation")
		}
	} else if evidence.ResolvedObject != nil {
		if err := validateResolvedReference(*evidence.ResolvedObject, *requested.Object); err != nil {
			return err
		}
	}
	for _, relation := range evidence.MatchedRelations {
		if relation.Kind != requested.Predicate || relation.SubjectID != requested.Subject.ID || requested.Object == nil || relation.ObjectID != requested.Object.ID {
			return fmt.Errorf("gograph relationship evidence does not match the request")
		}
		if err := validateClassification(requested.Predicate, relation.Classification); err != nil {
			return err
		}
		if relation.Locations == nil {
			return fmt.Errorf("gograph relationship locations must be an array")
		}
		for _, location := range relation.Locations {
			if err := validateLocation(location); err != nil {
				return err
			}
		}
	}
	if outcome == "pass" {
		if evidence.ResolvedSubject == nil {
			return fmt.Errorf("gograph pass lacks resolved subject evidence")
		}
		if requested.Object != nil && (evidence.ResolvedObject == nil || len(evidence.MatchedRelations) == 0) {
			return fmt.Errorf("gograph relationship pass lacks resolved relation evidence")
		}
	}
	if outcome == "fail" && requested.Object != nil && (evidence.ResolvedSubject == nil || evidence.ResolvedObject == nil || len(evidence.MatchedRelations) != 0) {
		return fmt.Errorf("gograph relationship failure lacks conclusive absence evidence")
	}
	return nil
}

func validateResolvedReference(actual resolvedReferenceDocument, expected referenceDocument) error {
	if actual.Kind != expected.Kind || actual.ID != expected.ID || actual.Locations == nil {
		return fmt.Errorf("gograph resolved identity does not match the request")
	}
	if expected.Kind == referenceSymbol && strings.TrimSpace(actual.SymbolKind) == "" {
		return fmt.Errorf("gograph resolved symbol lacks its kind")
	}
	for _, location := range actual.Locations {
		if err := validateLocation(location); err != nil {
			return err
		}
	}
	return nil
}

func validateClassification(predicate, classification string) error {
	valid := false
	switch predicate {
	case predicatePackageImports:
		valid = classification == "direct"
	case predicateCallEdge:
		valid = classification == "resolved_static" || classification == "cha_possible_target"
	case predicateTypeImplements:
		valid = classification == "precise_static"
	}
	if !valid {
		return fmt.Errorf("gograph relationship classification is invalid")
	}
	return nil
}

func validateLocation(location locationDocument) error {
	if err := validateLocationPath(location.Path, false); err != nil {
		return err
	}
	if location.Line < 0 || location.Column < 0 {
		return fmt.Errorf("gograph source location is invalid")
	}
	return nil
}

func validateLocationPath(value string, optional bool) error {
	if value == "" && optional {
		return nil
	}
	if value == "" || len(value) > 512 || path.IsAbs(value) || path.Clean(value) != value || value == ".." || strings.HasPrefix(value, "../") {
		return fmt.Errorf("gograph diagnostic or evidence path is not repository-relative")
	}
	return nil
}

func parseUTCTimestamp(value string) (time.Time, error) {
	if !strings.HasSuffix(value, "Z") {
		return time.Time{}, fmt.Errorf("timestamp is not UTC")
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed, nil
}

func resultReason(requested bindingDocument, document validationDocument) string {
	if len(document.Evaluation.Diagnostics) > 0 {
		return truncate(document.Evaluation.Diagnostics[0].Message, maxDiagnosticLength)
	}
	return fmt.Sprintf("Gograph reported %s for %s", document.Evaluation.Reason, bindingSummary(requested))
}

func (v *Validator) result(req validation.Request, outcome knowledge.ValidationOutcome, code, reason string, completed time.Time) knowledge.ValidationResult {
	result := knowledge.ValidationResult{
		ID: req.ResultID, BindingID: req.Binding.ID, ValidatorID: ValidatorID,
		ValidatorVersion: v.version, BindingVersion: req.Binding.BindingVersion,
		RepositoryRevision: req.Repository.Revision, SnapshotFingerprint: req.Repository.Fingerprint,
		InputFingerprint: req.InputFingerprint, Assurance: knowledge.AssuranceObservation,
		Outcome: outcome, ReasonCode: code, Reason: reason,
		EvidenceIDs: append([]string{}, req.Binding.EvidenceIDs...),
		StartedAt:   req.StartedAt, CompletedAt: completed,
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
	metadata["gograph.version"] = v.version
	diagnostics := make([]knowledge.ValidationDiagnostic, 0, 2)
	if process != nil {
		if message := strings.TrimSpace(string(process.stderr)); message != "" {
			diagnostics = append(diagnostics, knowledge.ValidationDiagnostic{Code: "gograph_stderr", Message: truncate(message, maxDiagnosticLength)})
		}
		if process.stderrTruncated {
			diagnostics = append(diagnostics, knowledge.ValidationDiagnostic{Code: "gograph_stderr_truncated", Message: "Gograph stderr exceeded the adapter limit"})
		}
	}
	result := v.result(req, knowledge.OutcomeCannotEvaluate, code, truncate(reason, maxDiagnosticLength), time.Now().UTC())
	result.Metadata = metadata
	result.Diagnostics = diagnostics
	return result
}

func (v *Validator) contextResult(req validation.Request, err error, process *commandResult) knowledge.ValidationResult {
	code := "gograph_context_canceled"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "gograph_deadline_exceeded"
	}
	return v.cannotEvaluate(req, code, err.Error(), process, nil)
}

func documentMetadata(req validation.Request, requested binding, document validationDocument) map[string]string {
	metadata := make(map[string]string)
	putMetadata(metadata, "repository.fingerprint", req.Repository.Fingerprint)
	putMetadata(metadata, "validation.input_fingerprint", req.InputFingerprint)
	putMetadata(metadata, "gograph.target", requested.target)
	putMetadata(metadata, "gograph.version", document.GographVersion)
	putMetadata(metadata, "gograph.predicate", bindingPredicate(document))
	putMetadata(metadata, "gograph.source_fingerprint", document.Repository.SourceFingerprint)
	putMetadata(metadata, "gograph.graph_fingerprint", document.Analysis.GraphFingerprint)
	putMetadata(metadata, "gograph.binding_fingerprint", document.Request.BindingFingerprint)
	putMetadata(metadata, "gograph.build_context_fingerprint", document.Analysis.BuildContextFingerprint)
	putMetadata(metadata, "gograph.analysis_mode", document.Analysis.Mode)
	putMetadata(metadata, "gograph.precision", document.Analysis.Precision)
	putMetadata(metadata, "gograph.completeness", document.Analysis.Completeness)
	putMetadata(metadata, "gograph.freshness", document.Analysis.Freshness)
	putMetadata(metadata, "gograph.reason_code", document.Evaluation.Reason)
	putMetadata(metadata, "gograph.git_revision", document.Repository.GitRevision)
	if document.Request.Binding != nil {
		putMetadata(metadata, "gograph.subject", document.Request.Binding.Subject.ID)
		if document.Request.Binding.Object != nil {
			putMetadata(metadata, "gograph.object", document.Request.Binding.Object.ID)
		}
	}
	if document.Evidence.ResolvedSubject != nil {
		putMetadata(metadata, "gograph.resolved_subject", document.Evidence.ResolvedSubject.ID)
		putMetadata(metadata, "gograph.subject_location", firstLocation(document.Evidence.ResolvedSubject.Locations))
	}
	if document.Evidence.ResolvedObject != nil {
		putMetadata(metadata, "gograph.resolved_object", document.Evidence.ResolvedObject.ID)
		putMetadata(metadata, "gograph.object_location", firstLocation(document.Evidence.ResolvedObject.Locations))
	}
	if len(document.Evidence.MatchedRelations) > 0 {
		relation := document.Evidence.MatchedRelations[0]
		putMetadata(metadata, "gograph.relationship", relation.Kind)
		putMetadata(metadata, "gograph.relationship_classification", relation.Classification)
		putMetadata(metadata, "gograph.relationship_location", firstLocation(relation.Locations))
	}
	return metadata
}

func bindingPredicate(document validationDocument) string {
	if document.Request.Binding == nil {
		return ""
	}
	return document.Request.Binding.Predicate
}

func firstLocation(locations []locationDocument) string {
	if len(locations) == 0 || locations[0].Path == "" {
		return ""
	}
	value := locations[0].Path
	if locations[0].Line > 0 {
		value += ":" + strconv.Itoa(locations[0].Line)
		if locations[0].Column > 0 {
			value += ":" + strconv.Itoa(locations[0].Column)
		}
	}
	return value
}

func putMetadata(metadata map[string]string, key, value string) {
	if strings.TrimSpace(value) != "" {
		metadata[key] = truncate(value, maxDiagnosticLength)
	}
}

func convertDiagnostics(source []diagnosticDocument) []knowledge.ValidationDiagnostic {
	result := make([]knowledge.ValidationDiagnostic, 0, len(source))
	for _, diagnostic := range source {
		result = append(result, knowledge.ValidationDiagnostic{
			Code: diagnostic.Code, Message: truncate(diagnostic.Message, maxDiagnosticLength), Path: truncate(diagnostic.Path, 256),
		})
	}
	return result
}
