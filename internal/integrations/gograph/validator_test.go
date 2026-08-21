package gograph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/validation"
)

const testInputFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type scriptedRunner struct {
	mu              sync.Mutex
	version         commandResult
	validation      commandResult
	blockValidation bool
	calls           [][]string
}

func (r *scriptedRunner) Run(ctx context.Context, executable string, args ...string) commandResult {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{executable}, args...))
	block := r.blockValidation && len(args) > 0 && args[0] == "validate"
	version := r.version
	result := r.validation
	r.mu.Unlock()
	if block {
		<-ctx.Done()
		return commandResult{exitCode: -1, err: ctx.Err()}
	}
	if len(args) > 0 && args[0] == "version" {
		return version
	}
	return result
}

type adapterFixture struct {
	repository string
	graphHash  string
	runner     *scriptedRunner
	validator  *Validator
	started    time.Time
}

func newAdapterFixture(t *testing.T) *adapterFixture {
	t.Helper()
	repository := t.TempDir()
	graphDirectory := filepath.Join(repository, ".gograph")
	if err := os.Mkdir(graphDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	graphBytes := []byte("{\"fixture\":true}\n")
	if err := os.WriteFile(filepath.Join(graphDirectory, "graph.json"), graphBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(graphBytes)
	runner := &scriptedRunner{version: jsonProcess(versionDocument{SchemaVersion: VersionSchemaVersion, Version: "1.5.7"}, 0)}
	validator, err := newValidator(context.Background(), Config{Executable: "gograph", RepositoryRoot: repository}, runner)
	if err != nil {
		t.Fatal(err)
	}
	return &adapterFixture{
		repository: validator.repositoryRoot, graphHash: hex.EncodeToString(digest[:]), runner: runner,
		validator: validator, started: time.Now().UTC().Add(-time.Second),
	}
}

func jsonProcess(document any, exitCode int) commandResult {
	data, err := json.Marshal(document)
	if err != nil {
		panic(err)
	}
	result := commandResult{stdout: data, exitCode: exitCode}
	if exitCode != 0 {
		result.err = errors.New("process exited")
	}
	return result
}

func testBinding(predicate string) knowledge.ValidationBinding {
	binding := knowledge.ValidationBinding{
		ID: "VAL-GOGRAPH-1", ValidatorID: ValidatorID, BindingVersion: BindingSchemaVersion,
		Required: true, RequiredAssurance: knowledge.AssuranceObservation,
		EvidenceIDs: []string{}, InputFingerprint: testInputFingerprint,
		RepositoryRevision: "revision-1", Parameters: map[string]string{"predicate": predicate},
	}
	switch predicate {
	case predicateSymbolExists:
		binding.Reference = "example.com/project/internal/service::Authorize"
		binding.Parameters["required_precision"] = precisionAST
	case predicatePackageImports:
		binding.Reference = "example.com/project/internal/service"
		binding.Parameters["object"] = "example.com/project/internal/store"
		binding.Parameters["required_precision"] = precisionAST
	case predicateCallEdge:
		binding.Reference = "example.com/project/internal/service::Authorize"
		binding.Parameters["object"] = "example.com/project/internal/store::Load"
		binding.Parameters["required_precision"] = precisionPrecise
	case predicateTypeImplements:
		binding.Reference = "example.com/project/internal/service::Service"
		binding.Parameters["object"] = "example.com/project/internal/service::Authorizer"
		binding.Parameters["required_precision"] = precisionPrecise
	}
	return binding
}

func testRequest(fixture *adapterFixture, binding knowledge.ValidationBinding, resultID string) validation.Request {
	return validation.Request{
		Claim:   knowledge.Claim{ID: "STRUCTURE-VALIDATION-1", Subject: "structure", Statement: "The selected structure exists."},
		Binding: binding,
		Repository: validation.RepositorySnapshot{
			Revision: "revision-1", Fingerprint: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", CapturedAt: fixture.started,
		},
		InputFingerprint: testInputFingerprint, ResultID: resultID, StartedAt: fixture.started,
	}
}

func testDocument(t *testing.T, fixture *adapterFixture, binding knowledge.ValidationBinding, outcome, reason string) validationDocument {
	t.Helper()
	parsed, err := parseBinding(binding)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	graphGenerated := now.Add(-time.Minute).Format(time.RFC3339Nano)
	document := validationDocument{
		SchemaVersion: ResultSchemaVersion, Command: "validate", GographVersion: "1.5.7", GeneratedAt: now.Format(time.RFC3339Nano),
		Repository: repositoryDocument{Root: fixture.repository, SourceFingerprint: strings.Repeat("c", 64)},
		Analysis: analysisDocument{
			GraphSchemaVersion: "2", SourcePolicyVersion: 1, GraphFingerprint: fixture.graphHash,
			BuildContextFingerprint: strings.Repeat("d", 64), Mode: precisionPrecise, Precision: precisionPrecise,
			Completeness: "complete", Freshness: "current", GraphGeneratedAt: &graphGenerated,
		},
		Request:    requestDocument{BindingFingerprint: parsed.fingerprint, Binding: &parsed.document},
		Evaluation: evaluationDocument{Outcome: outcome, Reason: reason, Diagnostics: []diagnosticDocument{}},
		Evidence:   evidenceDocument{MatchedRelations: []matchedRelationDocument{}},
	}
	if binding.Parameters["required_precision"] == precisionAST {
		document.Analysis.Mode = precisionAST
		document.Analysis.Precision = precisionAST
	}
	addResolvedEvidence(&document, parsed.document)
	if outcome != "pass" {
		document.Evidence.MatchedRelations = []matchedRelationDocument{}
		if parsed.document.Predicate == predicateSymbolExists {
			document.Evidence.ResolvedSubject = nil
		}
	}
	return document
}

func addResolvedEvidence(document *validationDocument, binding bindingDocument) {
	subjectKind := "function"
	if binding.Predicate == predicateTypeImplements {
		subjectKind = "struct"
	}
	document.Evidence.ResolvedSubject = &resolvedReferenceDocument{
		Kind: binding.Subject.Kind, ID: binding.Subject.ID, SymbolKind: subjectKind,
		Locations: []locationDocument{{Path: "internal/service/service.go", Line: 10}},
	}
	if binding.Subject.Kind == referencePackage {
		document.Evidence.ResolvedSubject.SymbolKind = ""
	}
	if binding.Object == nil {
		return
	}
	objectKind := "function"
	if binding.Predicate == predicateTypeImplements {
		objectKind = "interface"
	}
	document.Evidence.ResolvedObject = &resolvedReferenceDocument{Kind: binding.Object.Kind, ID: binding.Object.ID, SymbolKind: objectKind, Locations: []locationDocument{}}
	if binding.Object.Kind == referencePackage {
		document.Evidence.ResolvedObject.SymbolKind = ""
	}
	classification := map[string]string{
		predicatePackageImports: "direct", predicateCallEdge: "resolved_static", predicateTypeImplements: "precise_static",
	}[binding.Predicate]
	document.Evidence.MatchedRelations = []matchedRelationDocument{{
		Kind: binding.Predicate, SubjectID: binding.Subject.ID, ObjectID: binding.Object.ID,
		Classification: classification, Locations: []locationDocument{{Path: "internal/service/service.go", Line: 20, Column: 3}},
	}}
}

func runDocument(t *testing.T, fixture *adapterFixture, binding knowledge.ValidationBinding, document validationDocument, exitCode int) knowledge.ValidationResult {
	t.Helper()
	fixture.runner.mu.Lock()
	fixture.runner.validation = jsonProcess(document, exitCode)
	fixture.runner.mu.Unlock()
	request := testRequest(fixture, binding, "RES-GOGRAPH-1")
	result, err := fixture.validator.Validate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if err := validation.ValidateResult(fixture.validator.Descriptor(), request, result, time.Now().UTC().Add(time.Second)); err != nil {
		t.Fatalf("generic result authenticity failed: %v", err)
	}
	return result
}

func TestDescriptorRegistrationAndVersionDiscovery(t *testing.T) {
	fixture := newAdapterFixture(t)
	descriptor := fixture.validator.Descriptor()
	if descriptor.ID != ValidatorID || descriptor.Version != "1.5.7" || descriptor.MaximumAssurance != knowledge.AssuranceObservation || !descriptor.SupportsBindingVersion(BindingSchemaVersion) {
		t.Fatalf("unexpected descriptor: %+v", descriptor)
	}
	registry := validation.NewRegistry()
	if err := registry.Register(fixture.validator); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(fixture.validator); err == nil {
		t.Fatal("duplicate Gograph validator registration succeeded")
	}

	tests := []struct {
		name    string
		version commandResult
	}{
		{name: "malformed", version: commandResult{stdout: []byte("not-json"), exitCode: 0}},
		{name: "duplicate", version: commandResult{stdout: []byte(`{"schema_version":"gograph.version.v1","version":"1","version":"2"}`), exitCode: 0}},
		{name: "wrong schema", version: jsonProcess(versionDocument{SchemaVersion: "gograph.version.v2", Version: "1.5.7"}, 0)},
		{name: "invalid version", version: jsonProcess(versionDocument{SchemaVersion: VersionSchemaVersion, Version: "bad version"}, 0)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &scriptedRunner{version: test.version}
			if _, err := newValidator(context.Background(), Config{Executable: "gograph", RepositoryRoot: fixture.repository}, runner); err == nil {
				t.Fatal("invalid version response was accepted")
			}
		})
	}
	missing := filepath.Join(t.TempDir(), "missing-gograph")
	if _, err := New(context.Background(), Config{Executable: missing, RepositoryRoot: fixture.repository}); err == nil {
		t.Fatal("missing executable was accepted")
	}
}

func TestBindingSchemaValidation(t *testing.T) {
	fixture := newAdapterFixture(t)
	tests := []struct {
		name   string
		mutate func(*knowledge.ValidationBinding)
		code   string
	}{
		{name: "valid symbol", mutate: func(*knowledge.ValidationBinding) {}},
		{name: "unknown field", code: "invalid_binding_schema", mutate: func(binding *knowledge.ValidationBinding) { binding.Parameters["flags"] = "--anything" }},
		{name: "unsupported version", code: "unsupported_binding_version", mutate: func(binding *knowledge.ValidationBinding) { binding.BindingVersion = "gograph.binding.v2" }},
		{name: "unsupported predicate", code: "unsupported_predicate", mutate: func(binding *knowledge.ValidationBinding) { binding.Parameters["predicate"] = "reachability" }},
		{name: "invalid symbol", code: "invalid_symbol_identity", mutate: func(binding *knowledge.ValidationBinding) { binding.Reference = "Authorize" }},
		{name: "unstable symbol", code: "invalid_symbol_identity", mutate: func(binding *knowledge.ValidationBinding) { binding.Reference = "_/tmp/project::Authorize" }},
		{name: "unexpected object", code: "invalid_binding_schema", mutate: func(binding *knowledge.ValidationBinding) {
			binding.Parameters["object"] = "example.com/project::Other"
		}},
		{name: "call needs precise", code: "invalid_binding_schema", mutate: func(binding *knowledge.ValidationBinding) {
			*binding = testBinding(predicateCallEdge)
			binding.Parameters["required_precision"] = precisionAST
		}},
		{name: "invalid package", code: "invalid_package_identity", mutate: func(binding *knowledge.ValidationBinding) {
			*binding = testBinding(predicatePackageImports)
			binding.Reference = "../service"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binding := testBinding(predicateSymbolExists)
			test.mutate(&binding)
			err := fixture.validator.ValidateBinding(binding)
			if test.code == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var validationErr *validation.Error
			if !errors.As(err, &validationErr) || validationErr.Code != test.code {
				t.Fatalf("error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestPredicateOutcomeAndAssuranceMapping(t *testing.T) {
	tests := []struct {
		name      string
		predicate string
		outcome   string
		reason    string
		exitCode  int
		mutate    func(*validationDocument)
	}{
		{name: "symbol pass", predicate: predicateSymbolExists, outcome: "pass", reason: "predicate_passed", exitCode: 0},
		{name: "symbol evaluated fail", predicate: predicateSymbolExists, outcome: "fail", reason: "symbol_not_found", exitCode: 1},
		{name: "symbol cannot evaluate", predicate: predicateSymbolExists, outcome: "cannot_evaluate", reason: "analysis_incomplete", exitCode: 2},
		{name: "package pass", predicate: predicatePackageImports, outcome: "pass", reason: "predicate_passed", exitCode: 0},
		{name: "package evaluated fail", predicate: predicatePackageImports, outcome: "fail", reason: "relation_not_found", exitCode: 1},
		{name: "implementation pass", predicate: predicateTypeImplements, outcome: "pass", reason: "predicate_passed", exitCode: 0},
		{name: "implementation evaluated fail", predicate: predicateTypeImplements, outcome: "fail", reason: "relation_not_found", exitCode: 1},
		{name: "implementation insufficient precision", predicate: predicateTypeImplements, outcome: "cannot_evaluate", reason: "precision_insufficient", exitCode: 2, mutate: func(document *validationDocument) {
			document.Analysis.Mode, document.Analysis.Precision = "precise_fallback", "precise_fallback"
			document.Analysis.Completeness = "partial"
		}},
		{name: "call pass", predicate: predicateCallEdge, outcome: "pass", reason: "predicate_passed", exitCode: 0},
		{name: "call evaluated fail", predicate: predicateCallEdge, outcome: "fail", reason: "relation_not_found", exitCode: 1},
		{name: "call incomplete", predicate: predicateCallEdge, outcome: "cannot_evaluate", reason: "analysis_incomplete", exitCode: 2},
		{name: "CHA possible target", predicate: predicateCallEdge, outcome: "pass", reason: "predicate_passed", exitCode: 0, mutate: func(document *validationDocument) {
			document.Evidence.MatchedRelations[0].Classification = "cha_possible_target"
		}},
		{name: "graph missing", predicate: predicateSymbolExists, outcome: "cannot_evaluate", reason: "graph_missing", exitCode: 2, mutate: func(document *validationDocument) {
			document.Repository.SourceFingerprint = ""
			document.Analysis = analysisDocument{}
		}},
		{name: "graph stale", predicate: predicateSymbolExists, outcome: "cannot_evaluate", reason: "graph_stale", exitCode: 2, mutate: func(document *validationDocument) {
			document.Analysis.Freshness = "stale"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdapterFixture(t)
			binding := testBinding(test.predicate)
			document := testDocument(t, fixture, binding, test.outcome, test.reason)
			if test.mutate != nil {
				test.mutate(&document)
			}
			result := runDocument(t, fixture, binding, document, test.exitCode)
			if string(result.Outcome) != test.outcome || result.Assurance != knowledge.AssuranceObservation {
				t.Fatalf("result = %s/%s, want %s/observation", result.Outcome, result.Assurance, test.outcome)
			}
			if test.name == "CHA possible target" && result.Metadata["gograph.relationship_classification"] != "cha_possible_target" {
				t.Fatalf("CHA classification missing from metadata: %#v", result.Metadata)
			}
		})
	}
}

func TestValidationAuthenticityFailuresBecomeCannotEvaluate(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*adapterFixture, *validationDocument)
		exit   int
	}{
		{name: "wrong schema", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.SchemaVersion = "gograph.validation.v2"
		}},
		{name: "wrong Gograph version", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.GographVersion = "1.5.8"
		}},
		{name: "wrong predicate", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.Request.Binding.Predicate = predicateTypeImplements
		}},
		{name: "wrong subject", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.Request.Binding.Subject.ID = "example.com/project::Other"
		}},
		{name: "wrong object", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.Request.Binding.Object.ID = "example.com/project::Other"
		}},
		{name: "repository mismatch", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.Repository.Root = filepath.Dir(document.Repository.Root)
		}},
		{name: "wrong source fingerprint", mutate: func(_ *adapterFixture, document *validationDocument) { document.Repository.SourceFingerprint = "bad" }},
		{name: "wrong graph fingerprint", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.Analysis.GraphFingerprint = strings.Repeat("e", 64)
		}},
		{name: "wrong binding fingerprint", mutate: func(_ *adapterFixture, document *validationDocument) {
			document.Request.BindingFingerprint = strings.Repeat("f", 64)
		}},
		{name: "exit outcome mismatch", exit: 1, mutate: func(_ *adapterFixture, _ *validationDocument) {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdapterFixture(t)
			binding := testBinding(predicateCallEdge)
			document := testDocument(t, fixture, binding, "pass", "predicate_passed")
			test.mutate(fixture, &document)
			fixture.runner.validation = jsonProcess(document, test.exit)
			result, err := fixture.validator.Validate(context.Background(), testRequest(fixture, binding, "RES-GOGRAPH-1"))
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != "gograph_inauthentic_output" {
				t.Fatalf("result = %s/%s", result.Outcome, result.ReasonCode)
			}
		})
	}

	fixture := newAdapterFixture(t)
	binding := testBinding(predicateSymbolExists)
	for name, output := range map[string]string{
		"malformed": "not-json",
		"duplicate": `{"schema_version":"gograph.validation.v1","schema_version":"duplicate"}`,
		"unknown":   `{"schema_version":"gograph.validation.v1","unknown":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			fixture.runner.validation = commandResult{stdout: []byte(output), exitCode: 0}
			result, err := fixture.validator.Validate(context.Background(), testRequest(fixture, binding, "RES-GOGRAPH-2"))
			if err != nil || result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != "gograph_malformed_output" {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
		})
	}
	t.Run("non-zero without structured result", func(t *testing.T) {
		fixture.runner.validation = commandResult{stdout: []byte("failure"), stderr: []byte("gograph failed"), exitCode: 2, err: errors.New("process exited")}
		result, err := fixture.validator.Validate(context.Background(), testRequest(fixture, binding, "RES-GOGRAPH-3"))
		if err != nil || result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != "gograph_malformed_output" {
			t.Fatalf("result = %+v, err = %v", result, err)
		}
	})
}

func TestFingerprintEvidenceArgumentsAndBoundedDiagnostics(t *testing.T) {
	fixture := newAdapterFixture(t)
	binding := testBinding(predicateCallEdge)
	document := testDocument(t, fixture, binding, "pass", "predicate_passed")
	document.Evaluation.Diagnostics = []diagnosticDocument{{Code: "predicate_passed", Message: "matched selected call", Path: "internal/service/service.go"}}
	result := runDocument(t, fixture, binding, document, 0)
	for _, key := range []string{
		"repository.fingerprint", "validation.input_fingerprint", "gograph.version", "gograph.predicate",
		"gograph.subject", "gograph.object", "gograph.source_fingerprint", "gograph.graph_fingerprint",
		"gograph.binding_fingerprint", "gograph.build_context_fingerprint", "gograph.analysis_mode",
		"gograph.precision", "gograph.completeness", "gograph.freshness", "gograph.reason_code",
		"gograph.resolved_subject", "gograph.resolved_object", "gograph.relationship", "gograph.relationship_location",
	} {
		if result.Metadata[key] == "" {
			t.Fatalf("metadata %s is missing: %#v", key, result.Metadata)
		}
	}
	fixture.runner.mu.Lock()
	calls := append([][]string(nil), fixture.runner.calls...)
	fixture.runner.mu.Unlock()
	last := calls[len(calls)-1]
	wantPrefix := []string{"gograph", "validate", "--repo", fixture.repository, "--binding-json"}
	if len(last) != 7 || !reflect.DeepEqual(last[:5], wantPrefix) || last[6] != "--json" {
		t.Fatalf("unexpected explicit process arguments: %#v", last)
	}

	fixture.runner.validation = commandResult{stdout: []byte("bad"), stderr: []byte(strings.Repeat("x", 4096)), exitCode: -1, err: errors.New("failed")}
	malformed, err := fixture.validator.Validate(context.Background(), testRequest(fixture, binding, "RES-GOGRAPH-2"))
	if err != nil {
		t.Fatal(err)
	}
	if len(malformed.Diagnostics) != 1 || len(malformed.Diagnostics[0].Message) > maxDiagnosticLength {
		t.Fatalf("diagnostics were not bounded: %#v", malformed.Diagnostics)
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		code string
	}{
		{name: "timeout", ctx: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), time.Millisecond)
		}, code: "gograph_deadline_exceeded"},
		{name: "cancellation", ctx: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx, func() {}
		}, code: "gograph_context_canceled"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdapterFixture(t)
			fixture.runner.blockValidation = true
			ctx, cancel := test.ctx()
			defer cancel()
			result, err := fixture.validator.Validate(ctx, testRequest(fixture, testBinding(predicateSymbolExists), "RES-GOGRAPH-1"))
			if err != nil || result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != test.code {
				t.Fatalf("result = %+v, err = %v", result, err)
			}
		})
	}
}

func TestValidationHistoryAndFreshnessDegradation(t *testing.T) {
	fixture := newAdapterFixture(t)
	binding := testBinding(predicateSymbolExists)
	passDocument := testDocument(t, fixture, binding, "pass", "predicate_passed")
	pass := runDocument(t, fixture, binding, passDocument, 0)

	checked := fixture.started
	evidence := knowledge.Evidence{
		ID: "EVD-GOGRAPH-SOURCE-1", Kind: knowledge.EvidenceRepositoryReference, Polarity: knowledge.PolaritySupports,
		Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "repo:go.mod"},
		Locator: "repo:go.mod", Scope: "selected Go structure", Availability: knowledge.AvailabilityAvailable,
		CapturedAt: fixture.started, CheckedAt: &checked, DerivedFrom: []string{},
	}
	binding.EvidenceIDs = []string{evidence.ID}
	pass.EvidenceIDs = []string{evidence.ID}
	claim := knowledge.Claim{
		SchemaVersion: knowledge.SchemaVersion, ID: "STRUCTURE-VALIDATION-1", Subject: "structure", Statement: "The selected symbol exists.",
		Lifecycle:  knowledge.Lifecycle{State: knowledge.LifecycleActive},
		Authorship: knowledge.Authorship{Kind: knowledge.AuthorshipOwner, Origin: "owner:project", RecordedAt: fixture.started},
		Evidence:   []knowledge.Evidence{evidence}, ValidationPolicy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{binding}},
		ValidationResults: []knowledge.ValidationResult{pass}, CreatedAt: fixture.started, UpdatedAt: pass.CompletedAt,
	}
	state, err := knowledge.DeriveState(claim, pass.CompletedAt.Add(time.Second))
	if err != nil || state.Assessment != knowledge.AssessmentObserved || state.Freshness != knowledge.FreshnessCurrent {
		t.Fatalf("pass state = %+v, err = %v", state, err)
	}

	staleDocument := testDocument(t, fixture, binding, "cannot_evaluate", "graph_stale")
	staleDocument.Analysis.Freshness = "stale"
	fixture.runner.validation = jsonProcess(staleDocument, 2)
	request := testRequest(fixture, binding, "RES-GOGRAPH-2")
	request.StartedAt = time.Now().UTC().Add(-time.Second)
	staleDocument.GeneratedAt = time.Now().UTC().Format(time.RFC3339Nano)
	fixture.runner.validation = jsonProcess(staleDocument, 2)
	stale, err := fixture.validator.Validate(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	claim.ValidationResults = append(claim.ValidationResults, stale)
	claim.UpdatedAt = stale.CompletedAt
	state, err = knowledge.DeriveState(claim, stale.CompletedAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if len(claim.ValidationResults) != 2 || state.Assessment != knowledge.AssessmentSourced || state.Freshness != knowledge.FreshnessStale {
		t.Fatalf("stale revalidation state = %+v, history = %d", state, len(claim.ValidationResults))
	}
}
