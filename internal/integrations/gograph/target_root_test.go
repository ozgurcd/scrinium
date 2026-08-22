package gograph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"scrinium/internal/knowledge"
)

// decoyFixture builds a GOVERNED repository carrying a DECOY
// .gograph/graph.json and a separate TARGET repository carrying the real
// graph with DIFFERENT bytes, allowlisted as "product". A validator that
// fingerprints the governed repo's graph instead of the target's cannot
// pass this fixture — and a governed repo WITHOUT a graph would hide the
// defect, which is exactly what the fleet pilot ran into.
func decoyFixture(t *testing.T) (*adapterFixture, string, string) {
	t.Helper()
	governed := t.TempDir()
	if err := os.Mkdir(filepath.Join(governed, ".gograph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(governed, ".gograph", "graph.json"), []byte("{\"decoy\":true}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := os.Mkdir(filepath.Join(target, ".gograph"), 0o755); err != nil {
		t.Fatal(err)
	}
	targetGraph := []byte("{\"target\":true}\n")
	if err := os.WriteFile(filepath.Join(target, ".gograph", "graph.json"), targetGraph, 0o644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(targetGraph)
	runner := &scriptedRunner{version: jsonProcess(versionDocument{SchemaVersion: VersionSchemaVersion, Version: "1.5.7"}, 0)}
	validator, err := newValidator(context.Background(), Config{
		Executable: "gograph", RepositoryRoot: governed,
		Targets: map[string]string{"product": target},
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &adapterFixture{
		repository: validator.targets["product"], graphHash: hex.EncodeToString(digest[:]),
		runner: runner, validator: validator, started: time.Now().UTC().Add(-time.Second),
	}
	return fixture, validator.repositoryRoot, validator.targets["product"]
}

func targetBoundBinding() knowledge.ValidationBinding {
	binding := testBinding(predicateSymbolExists)
	binding.Parameters["target"] = "product"
	return binding
}

// The TARGET's graph must be the one fingerprinted: the document reports
// the target root and the target graph's sha256, the governed decoy
// differs, and the validation must still PASS. Watched failing against
// the unfixed code (currentGraphFingerprint read the governed decoy):
// cannot_evaluate / gograph_inauthentic_output.
func TestTargetBindingFingerprintsTargetGraphNotGovernedDecoy(t *testing.T) {
	fixture, _, _ := decoyFixture(t)
	binding := targetBoundBinding()
	document := testDocument(t, fixture, binding, "pass", "predicate_passed")
	fixture.runner.validation = jsonProcess(document, 0)

	result, err := fixture.validator.Validate(context.Background(), testRequest(fixture, binding, "RES-TARGET-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != knowledge.OutcomePass {
		t.Fatalf("outcome = %s (%s: %s), want pass against the TARGET graph", result.Outcome, result.ReasonCode, result.Reason)
	}
	if result.Metadata["gograph.target"] != "product" {
		t.Fatalf("pass metadata must record the target, got %q", result.Metadata["gograph.target"])
	}
}

// Without a target the governed repo's graph stays authoritative: a
// document claiming the target graph's fingerprint must refuse.
func TestGovernedBindingStillUsesGovernedGraph(t *testing.T) {
	fixture, governedRoot, _ := decoyFixture(t)
	binding := testBinding(predicateSymbolExists) // no target parameter
	document := testDocument(t, fixture, binding, "pass", "predicate_passed")
	document.Repository.Root = governedRoot // document claims the governed root
	fixture.runner.validation = jsonProcess(document, 0)

	result, err := fixture.validator.Validate(context.Background(), testRequest(fixture, binding, "RES-GOVERNED-1"))
	if err != nil {
		t.Fatal(err)
	}
	// fixture.graphHash is the TARGET graph's fingerprint; the governed
	// decoy differs, so the governed-path check must refuse it.
	if result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != "gograph_inauthentic_output" {
		t.Fatalf("outcome = %s (%s), want cannot_evaluate/gograph_inauthentic_output on the governed decoy", result.Outcome, result.ReasonCode)
	}
}

// A refusal must say WHICH target it refused: the inauthentic branch
// carries the authenticated metadata, target name included.
func TestInauthenticRefusalCarriesTargetMetadata(t *testing.T) {
	fixture, _, _ := decoyFixture(t)
	binding := targetBoundBinding()
	document := testDocument(t, fixture, binding, "pass", "predicate_passed")
	document.Analysis.GraphFingerprint = "1111111111111111111111111111111111111111111111111111111111111111"
	fixture.runner.validation = jsonProcess(document, 0)

	result, err := fixture.validator.Validate(context.Background(), testRequest(fixture, binding, "RES-INAUTH-1"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != knowledge.OutcomeCannotEvaluate || result.ReasonCode != "gograph_inauthentic_output" {
		t.Fatalf("outcome = %s (%s), want cannot_evaluate/gograph_inauthentic_output", result.Outcome, result.ReasonCode)
	}
	if result.Metadata["gograph.target"] != "product" {
		t.Fatalf("inauthentic refusal must name its target, got metadata %v", result.Metadata)
	}
}
