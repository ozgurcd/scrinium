package rulefloor

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

const (
	testRepositoryFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testInputFingerprint      = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testProofFingerprint      = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

type runnerFunc func(context.Context, string, ...string) commandResult

func (function runnerFunc) Run(ctx context.Context, executable string, args ...string) commandResult {
	return function(ctx, executable, args...)
}

type adapterFixture struct {
	root              string
	ledgerFingerprint string
	runner            *recordingRunner
	validator         *Validator
}

type recordingRunner struct {
	mu            sync.Mutex
	validate      func(context.Context, []string) commandResult
	calls         [][]string
	version       string
	versionResult *commandResult
}

func (runner *recordingRunner) Run(ctx context.Context, _ string, args ...string) commandResult {
	runner.mu.Lock()
	runner.calls = append(runner.calls, append([]string(nil), args...))
	runner.mu.Unlock()
	if len(args) > 0 && args[0] == "version" {
		if runner.versionResult != nil {
			return *runner.versionResult
		}
		return jsonResult(versionDocument{SchemaVersion: VersionSchemaVersion, Version: runner.version}, 0)
	}
	return runner.validate(ctx, args)
}

func newAdapterFixture(t *testing.T, validate func(context.Context, []string) commandResult) adapterFixture {
	t.Helper()
	root := t.TempDir()
	ledger := []byte("# Rule Floor\n")
	if err := os.WriteFile(filepath.Join(root, "RULE-FLOOR.md"), ledger, 0o644); err != nil {
		t.Fatal(err)
	}
	fingerprint := sha256Hex(ledger)
	runner := &recordingRunner{version: "0.3.0", validate: validate}
	validator, err := newValidator(context.Background(), Config{Executable: "/test/rulefloor", RepositoryRoot: root}, runner)
	if err != nil {
		t.Fatal(err)
	}
	return adapterFixture{root: validator.repositoryRoot, ledgerFingerprint: fingerprint, runner: runner, validator: validator}
}

func validationRequest(mode, profile string) validation.Request {
	started := time.Now().UTC().Add(-100 * time.Millisecond)
	return validation.Request{
		Claim: knowledge.Claim{ID: "AUTH-ADMIN-LOCAL-1"},
		Binding: knowledge.ValidationBinding{
			ID: "VAL-AUTH-RULEFLOOR-1", ValidatorID: ValidatorID, BindingVersion: BindingSchemaVersion,
			Reference: "AUTH-ADMIN-1", Parameters: map[string]string{"mode": mode}, Required: true,
			RequiredAssurance: knowledge.AssuranceVerification, EvidenceIDs: []string{}, InputFingerprint: testInputFingerprint,
			SnapshotFingerprint: testRepositoryFingerprint,
		},
		Repository:       validation.RepositorySnapshot{Revision: "rev-1", Fingerprint: testRepositoryFingerprint, CapturedAt: started},
		InputFingerprint: testInputFingerprint, ResultID: "RES-AUTH-RULEFLOOR-1", StartedAt: started,
	}
}

func validationOutput(root, ledgerFingerprint, mode, profile string) validationDocument {
	proof := testProofFingerprint
	execution := executionDocument{Status: "not_requested"}
	if mode == "execute" {
		execution = executionDocument{Requested: true, Performed: true, Status: "pass"}
	}
	return validationDocument{
		SchemaVersion: ResultSchemaVersion, Command: "validate", RulefloorVersion: "0.3.0", GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Repository: repositoryDocument{Root: root, LedgerPath: filepath.Join(root, "RULE-FLOOR.md"), LedgerFingerprint: ledgerFingerprint},
		Request:    requestDocument{RuleID: "AUTH-ADMIN-1", Mode: mode, Profile: profile},
		Rule: ruleDocument{
			Exists: true, Armed: true, EnforcedBy: "go-test", CheckFile: "auth_test.go", DeclaredProfile: "unit",
			RedProofStatus: "present", TestFingerprint: fingerprintDocument{Expected: "123456789abc", Actual: "123456789abc"}, ProofFingerprint: &proof,
		},
		Evaluation: evaluationDocument{Outcome: "pass", StaticIntegrity: "pass", Execution: execution, Reason: "rule_passed", Diagnostics: []diagnosticDocument{}},
	}
}

func jsonResult(document any, exitCode int) commandResult {
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

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func TestDescriptorAndVersionDiscovery(t *testing.T) {
	fixture := newAdapterFixture(t, func(context.Context, []string) commandResult {
		return commandResult{}
	})
	descriptor := fixture.validator.Descriptor()
	if descriptor.ID != ValidatorID || descriptor.Version != "0.3.0" || descriptor.MaximumAssurance != knowledge.AssuranceVerification || !descriptor.SupportsBindingVersion(BindingSchemaVersion) {
		t.Fatalf("unexpected descriptor: %#v", descriptor)
	}
	if len(fixture.runner.calls) != 1 || !reflect.DeepEqual(fixture.runner.calls[0], []string{"version", "--json"}) {
		t.Fatalf("unexpected version discovery calls: %#v", fixture.runner.calls)
	}
	registry := validation.NewRegistry()
	if err := registry.Register(fixture.validator); err != nil {
		t.Fatalf("register Rulefloor adapter: %v", err)
	}
	if _, registered, exists := registry.Resolve(ValidatorID); !exists || registered.Version != "0.3.0" {
		t.Fatalf("registered descriptor = %#v exists=%v", registered, exists)
	}
}

func TestVersionDiscoveryRejectsUnavailableAndInvalidDocuments(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name   string
		result commandResult
	}{
		{name: "malformed", result: commandResult{stdout: []byte("not json"), exitCode: 0}},
		{name: "wrong schema", result: jsonResult(versionDocument{SchemaVersion: "rulefloor.version.v2", Version: "0.3.0"}, 0)},
		{name: "unknown field", result: commandResult{stdout: []byte(`{"schema_version":"rulefloor.version.v1","version":"0.3.0","extra":true}`), exitCode: 0}},
		{name: "duplicate field", result: commandResult{stdout: []byte(`{"schema_version":"rulefloor.version.v1","version":"0.3.0","version":"0.4.0"}`), exitCode: 0}},
		{name: "failed", result: commandResult{exitCode: -1, err: errors.New("missing executable")}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := runnerFunc(func(context.Context, string, ...string) commandResult { return test.result })
			if _, err := newValidator(context.Background(), Config{Executable: "/missing/rulefloor", RepositoryRoot: root}, runner); err == nil {
				t.Fatal("expected version discovery failure")
			}
		})
	}
	if _, err := New(context.Background(), Config{Executable: filepath.Join(root, "missing-rulefloor"), RepositoryRoot: root}); err == nil {
		t.Fatal("expected missing real executable to be unavailable")
	}
}

func TestBindingSchemaValidation(t *testing.T) {
	fixture := newAdapterFixture(t, func(context.Context, []string) commandResult { return commandResult{} })
	valid := validationRequest("static", "").Binding
	tests := []struct {
		name   string
		mutate func(*knowledge.ValidationBinding)
	}{
		{name: "invalid rule", mutate: func(binding *knowledge.ValidationBinding) { binding.Reference = "--help" }},
		{name: "missing numeric suffix", mutate: func(binding *knowledge.ValidationBinding) { binding.Reference = "DOES-NOT-EXIST" }},
		{name: "invalid mode", mutate: func(binding *knowledge.ValidationBinding) { binding.Parameters["mode"] = "dynamic" }},
		{name: "profile with static", mutate: func(binding *knowledge.ValidationBinding) { binding.Parameters["profile"] = "unit" }},
		{name: "unsafe profile", mutate: func(binding *knowledge.ValidationBinding) {
			binding.Parameters["mode"], binding.Parameters["profile"] = "execute", "unit --tags=x"
		}},
		{name: "arbitrary flag", mutate: func(binding *knowledge.ValidationBinding) { binding.Parameters["flags"] = "--tags=x" }},
		{name: "wrong validator", mutate: func(binding *knowledge.ValidationBinding) { binding.ValidatorID = "other" }},
		{name: "wrong schema", mutate: func(binding *knowledge.ValidationBinding) { binding.BindingVersion = "rulefloor.binding.v2" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			candidate.Parameters = map[string]string{"mode": "static"}
			test.mutate(&candidate)
			if err := fixture.validator.ValidateBinding(candidate); err == nil {
				t.Fatal("expected invalid binding")
			}
		})
	}
	execute := valid
	execute.Parameters = map[string]string{"mode": "execute", "profile": "integration"}
	if err := fixture.validator.ValidateBinding(execute); err != nil {
		t.Fatalf("valid execute binding rejected: %v", err)
	}
}

func TestOutcomeAndAssuranceMapping(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		mutate    func(*validationDocument)
		exitCode  int
		outcome   knowledge.ValidationOutcome
		assurance knowledge.Assurance
	}{
		{name: "static pass observation", mode: "static", exitCode: 0, outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceObservation},
		{name: "execute pass verification", mode: "execute", exitCode: 0, outcome: knowledge.OutcomePass, assurance: knowledge.AssuranceVerification},
		{name: "execute evaluated failure", mode: "execute", exitCode: 1, outcome: knowledge.OutcomeFail, assurance: knowledge.AssuranceVerification, mutate: func(document *validationDocument) {
			document.Evaluation.Outcome, document.Evaluation.Execution.Status, document.Evaluation.Reason = "fail", "fail", "rule_failed"
			document.Evaluation.Diagnostics = []diagnosticDocument{{Code: "execution_failed", Message: "selected test failed"}}
		}},
		{name: "static hash mismatch", mode: "static", exitCode: 1, outcome: knowledge.OutcomeFail, assurance: knowledge.AssuranceObservation, mutate: func(document *validationDocument) {
			document.Rule.TestFingerprint.Actual = "abcdefabcdef"
			document.Evaluation.Outcome, document.Evaluation.StaticIntegrity, document.Evaluation.Reason = "fail", "fail", "hash_mismatch"
		}},
		{name: "unsupported execution", mode: "execute", exitCode: 2, outcome: knowledge.OutcomeCannotEvaluate, assurance: knowledge.AssuranceObservation, mutate: func(document *validationDocument) {
			document.Evaluation.Outcome, document.Evaluation.Execution.Performed, document.Evaluation.Execution.Status = "cannot_evaluate", false, "cannot_evaluate"
			document.Evaluation.Reason = "execution_unsupported"
		}},
		{name: "missing rule", mode: "static", exitCode: 2, outcome: knowledge.OutcomeCannotEvaluate, assurance: knowledge.AssuranceObservation, mutate: func(document *validationDocument) {
			document.Rule.Exists, document.Rule.Armed = false, false
			document.Rule.RedProofStatus, document.Rule.ProofFingerprint = "not_applicable", nil
			document.Rule.TestFingerprint = fingerprintDocument{}
			document.Evaluation.Outcome, document.Evaluation.StaticIntegrity, document.Evaluation.Reason = "cannot_evaluate", "not_performed", "rule_not_found"
		}},
		{name: "unarmed rule", mode: "static", exitCode: 2, outcome: knowledge.OutcomeCannotEvaluate, assurance: knowledge.AssuranceObservation, mutate: func(document *validationDocument) {
			document.Rule.Armed = false
			document.Rule.RedProofStatus, document.Rule.ProofFingerprint = "not_applicable", nil
			document.Rule.TestFingerprint = fingerprintDocument{}
			document.Evaluation.Outcome, document.Evaluation.StaticIntegrity, document.Evaluation.Reason = "cannot_evaluate", "not_performed", "rule_unarmed"
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var fixture adapterFixture
			fixture = newAdapterFixture(t, func(context.Context, []string) commandResult {
				document := validationOutput(fixture.root, fixture.ledgerFingerprint, test.mode, "")
				if test.mutate != nil {
					test.mutate(&document)
				}
				return jsonResult(document, test.exitCode)
			})
			req := validationRequest(test.mode, "")
			result, err := fixture.validator.Validate(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != test.outcome || result.Assurance != test.assurance {
				t.Fatalf("got outcome=%s assurance=%s result=%#v", result.Outcome, result.Assurance, result)
			}
			if err := validation.ValidateResult(fixture.validator.Descriptor(), req, result, time.Now().UTC()); err != nil {
				t.Fatalf("adapter returned inauthentic generic result: %v", err)
			}
		})
	}
}

func TestValidationAuthenticityFailuresBecomeCannotEvaluate(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*validationDocument, adapterFixture)
		exitCode int
		code     string
	}{
		{name: "wrong schema", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) {
			document.SchemaVersion = "rulefloor.validation.v2"
		}},
		{name: "wrong rule", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) { document.Request.RuleID = "OTHER-1" }},
		{name: "wrong mode", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) { document.Request.Mode = "execute" }},
		{name: "wrong profile", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) { document.Request.Profile = "integration" }},
		{name: "repository mismatch", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) {
			document.Repository.Root = filepath.Dir(document.Repository.Root)
		}},
		{name: "ledger fingerprint mismatch", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) {
			document.Repository.LedgerFingerprint = strings.Repeat("d", 64)
		}},
		{name: "version mismatch", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) { document.RulefloorVersion = "0.4.0" }},
		{name: "exit mismatch", exitCode: 1, code: "rulefloor_inauthentic_output", mutate: func(*validationDocument, adapterFixture) {}},
		{name: "execute not performed", exitCode: 0, code: "rulefloor_inauthentic_output", mutate: func(document *validationDocument, _ adapterFixture) {
			document.Request.Mode = "execute"
			document.Evaluation.Execution = executionDocument{Requested: true, Performed: false, Status: "not_performed"}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var fixture adapterFixture
			fixture = newAdapterFixture(t, func(context.Context, []string) commandResult {
				document := validationOutput(fixture.root, fixture.ledgerFingerprint, "static", "")
				test.mutate(&document, fixture)
				return jsonResult(document, test.exitCode)
			})
			result, err := fixture.validator.Validate(context.Background(), validationRequest("static", ""))
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != test.code {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestMalformedAndUnexpectedProcessResults(t *testing.T) {
	tests := []struct {
		name   string
		result commandResult
		code   string
	}{
		{name: "malformed JSON", result: commandResult{stdout: []byte("not-json"), exitCode: 0}, code: "rulefloor_malformed_output"},
		{name: "nonzero without JSON", result: commandResult{stderr: []byte("failure"), exitCode: 7, err: errors.New("failed")}, code: "rulefloor_malformed_output"},
		{name: "truncated JSON", result: commandResult{stdout: []byte(`{}`), exitCode: 0, stdoutTruncated: true}, code: "rulefloor_output_too_large"},
		{name: "wrong schema shape", result: commandResult{stdout: []byte(`{"schema_version":"rulefloor.validation.v1"}`), exitCode: 0}, code: "rulefloor_malformed_output"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAdapterFixture(t, func(context.Context, []string) commandResult { return test.result })
			result, err := fixture.validator.Validate(context.Background(), validationRequest("static", ""))
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != test.code {
				t.Fatalf("unexpected result: %#v", result)
			}
		})
	}
}

func TestTimeoutCancellationAndDisappearingExecutable(t *testing.T) {
	fixture := newAdapterFixture(t, func(ctx context.Context, _ []string) commandResult {
		<-ctx.Done()
		return commandResult{exitCode: -1, err: ctx.Err()}
	})
	req := validationRequest("static", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result, err := fixture.validator.Validate(ctx, req)
	if err != nil || result.ReasonCode != "rulefloor_deadline_exceeded" {
		t.Fatalf("deadline result=%#v err=%v", result, err)
	}

	canceled, stop := context.WithCancel(context.Background())
	stop()
	result, err = fixture.validator.Validate(canceled, req)
	if err != nil || result.ReasonCode != "rulefloor_context_canceled" {
		t.Fatalf("cancellation result=%#v err=%v", result, err)
	}

	versionCalls := 0
	disappearing := runnerFunc(func(_ context.Context, _ string, args ...string) commandResult {
		if args[0] == "version" {
			versionCalls++
			if versionCalls == 1 {
				return jsonResult(versionDocument{SchemaVersion: VersionSchemaVersion, Version: "0.3.0"}, 0)
			}
			return commandResult{exitCode: -1, err: errors.New("executable disappeared")}
		}
		return commandResult{}
	})
	disappeared, err := newValidator(context.Background(), Config{Executable: "/test/rulefloor", RepositoryRoot: fixture.root}, disappearing)
	if err != nil {
		t.Fatal(err)
	}
	result, err = disappeared.Validate(context.Background(), req)
	if err != nil || result.ReasonCode != "rulefloor_version_unavailable" {
		t.Fatalf("missing executable result=%#v err=%v", result, err)
	}
}

func TestFingerprintMetadataArgumentsAndBoundedDiagnostics(t *testing.T) {
	var fixture adapterFixture
	fixture = newAdapterFixture(t, func(context.Context, []string) commandResult {
		document := validationOutput(fixture.root, fixture.ledgerFingerprint, "execute", "integration")
		document.Rule.DeclaredProfile = "integration"
		document.Evaluation.Outcome = "cannot_evaluate"
		document.Evaluation.Execution = executionDocument{Requested: true, Performed: true, Status: "cannot_evaluate"}
		document.Evaluation.Reason = "test_skipped"
		for index := 0; index < maxDiagnosticCount+5; index++ {
			document.Evaluation.Diagnostics = append(document.Evaluation.Diagnostics, diagnosticDocument{Code: "test_skipped", Message: strings.Repeat("x", maxDiagnosticLength+50), Path: "auth_test.go", Test: "TestAuth"})
		}
		return jsonResult(document, 2)
	})
	req := validationRequest("execute", "integration")
	req.Binding.Parameters["profile"] = "integration"
	result, err := fixture.validator.Validate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != knowledge.OutcomeCannotEvaluate || result.Assurance != knowledge.AssuranceObservation {
		t.Fatalf("unexpected outcome: %#v", result)
	}
	if len(result.Diagnostics) != maxDiagnosticCount+1 || len(result.Diagnostics[0].Message) != maxDiagnosticLength {
		t.Fatalf("diagnostics were not bounded: %#v", result.Diagnostics)
	}
	if result.Metadata["rulefloor.ledger_fingerprint"] != fixture.ledgerFingerprint || result.Metadata["rulefloor.proof_fingerprint"] != testProofFingerprint || result.Metadata["repository.fingerprint"] != testRepositoryFingerprint || result.Metadata["validation.input_fingerprint"] != testInputFingerprint {
		t.Fatalf("missing fingerprint metadata: %#v", result.Metadata)
	}
	wantArgs := []string{"validate", "AUTH-ADMIN-1", "--repo", fixture.root, "--mode", "execute", "--profile", "integration", "--json"}
	if got := fixture.runner.calls[len(fixture.runner.calls)-1]; !reflect.DeepEqual(got, wantArgs) {
		t.Fatalf("unexpected explicit arguments: got %#v want %#v", got, wantArgs)
	}
}
