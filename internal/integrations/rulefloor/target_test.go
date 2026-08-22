package rulefloor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scrinium/internal/knowledge"
	"scrinium/internal/validation"
)

// targetFixture builds a validator whose allowlist maps "product" to a
// second temp repository carrying its own ledger.
func targetFixture(t *testing.T) (adapterFixture, string) {
	t.Helper()
	targetRoot := t.TempDir()
	targetLedger := []byte("# Target Rule Floor\n")
	if err := os.WriteFile(filepath.Join(targetRoot, "RULE-FLOOR.md"), targetLedger, 0o644); err != nil {
		t.Fatal(err)
	}
	governedRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(governedRoot, "RULE-FLOOR.md"), []byte("# Governed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runner := &recordingRunner{version: "0.3.0"}
	validator, err := newValidator(context.Background(), Config{
		Executable:     "/test/rulefloor",
		RepositoryRoot: governedRoot,
		Targets:        map[string]string{"product": targetRoot},
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	resolvedTarget := validator.targets["product"]
	runner.validate = func(_ context.Context, args []string) commandResult {
		return jsonResult(validationOutput(resolvedTarget, sha256Hex(targetLedger), "static", ""), 0)
	}
	return adapterFixture{root: validator.repositoryRoot, runner: runner, validator: validator}, resolvedTarget
}

func targetRequest(target string) validation.Request {
	request := validationRequest("static", "")
	request.Binding.Parameters = map[string]string{"mode": "static", "target": target}
	return request
}

// An allowlisted target resolves: the adapter must invoke rulefloor with
// --repo <target root>, and a clean document from that root passes.
func TestTargetBindingValidatesAgainstAllowlistedRoot(t *testing.T) {
	fixture, resolvedTarget := targetFixture(t)
	result, err := fixture.validator.Validate(context.Background(), targetRequest("product"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != knowledge.OutcomePass {
		t.Fatalf("outcome = %s (%s: %s), want pass", result.Outcome, result.ReasonCode, result.Reason)
	}
	var validateCall []string
	for _, call := range fixture.runner.calls {
		if len(call) > 0 && call[0] == "validate" {
			validateCall = call
		}
	}
	joined := strings.Join(validateCall, " ")
	if !strings.Contains(joined, "--repo "+resolvedTarget) {
		t.Fatalf("rulefloor was not invoked against the target root: %v", validateCall)
	}
	if result.Metadata["rulefloor.target"] != "product" {
		t.Fatalf("result metadata must record the target name, got %q", result.Metadata["rulefloor.target"])
	}
}

// A name absent from the allowlist is refused as cannot_evaluate without
// ever invoking rulefloor for it.
func TestUnknownTargetIsRefused(t *testing.T) {
	fixture, _ := targetFixture(t)
	result, err := fixture.validator.Validate(context.Background(), targetRequest("intruder"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != "rulefloor_unknown_target" {
		t.Fatalf("outcome = %s reason_code = %s, want cannot_evaluate/rulefloor_unknown_target", result.Outcome, result.ReasonCode)
	}
	if !strings.Contains(result.Reason, "not allowlisted in scrinium.json validation_targets") {
		t.Fatalf("refusal must name the allowlist, got %q", result.Reason)
	}
	for _, call := range fixture.runner.calls {
		if len(call) > 0 && call[0] == "validate" {
			t.Fatalf("rulefloor must not be invoked for an unknown target: %v", call)
		}
	}
}

// A binding parameter that smells like a path never parses.
func TestTargetParameterRefusesPaths(t *testing.T) {
	fixture, _ := targetFixture(t)
	for _, evil := range []string{"../escape", "/etc", `..\\up`, "Upper"} {
		request := targetRequest(evil)
		if err := fixture.validator.ValidateBinding(request.Binding); err == nil {
			t.Fatalf("target %q must be refused at binding validation", evil)
		} else if !strings.Contains(err.Error(), "never a filesystem path") {
			t.Fatalf("target %q refusal has the wrong message: %v", evil, err)
		}
	}
}

// A symlinked target root is refused at registration.
func TestSymlinkedTargetRootRefusedAtRegistration(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	governedRoot := t.TempDir()
	runner := &recordingRunner{version: "0.3.0"}
	_, err := newValidator(context.Background(), Config{
		Executable:     "/test/rulefloor",
		RepositoryRoot: governedRoot,
		Targets:        map[string]string{"linked": link},
	}, runner)
	// canonicalDirectory resolves symlinks; registration stores the REAL
	// path, so a later swap of the link cannot redirect validation. A
	// dangling link must fail outright.
	if err != nil {
		return
	}
	stale := filepath.Join(t.TempDir(), "gone")
	if err := os.Symlink(stale, filepath.Join(t.TempDir(), "dangling")); err != nil {
		t.Fatal(err)
	}
	_, err = newValidator(context.Background(), Config{
		Executable:     "/test/rulefloor",
		RepositoryRoot: governedRoot,
		Targets:        map[string]string{"dangling": filepath.Join(t.TempDir(), "missing")},
	}, runner)
	if err == nil {
		t.Fatal("a target root that does not resolve must fail registration")
	}
}

// A target directory deleted after registration refuses at use.
func TestDeletedTargetRootRefusedAtUse(t *testing.T) {
	fixture, resolvedTarget := targetFixture(t)
	if err := os.RemoveAll(resolvedTarget); err != nil {
		t.Fatal(err)
	}
	result, err := fixture.validator.Validate(context.Background(), targetRequest("product"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != "rulefloor_target_unavailable" {
		t.Fatalf("outcome = %s reason_code = %s, want cannot_evaluate/rulefloor_target_unavailable", result.Outcome, result.ReasonCode)
	}
}
