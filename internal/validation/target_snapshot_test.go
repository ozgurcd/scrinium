package validation

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// targetSnapshotFixture writes a real external target repository with a
// ledger and returns a snapshotter allowlisting it as "product".
func targetSnapshotFixture(t *testing.T) (*Snapshotter, string) {
	t.Helper()
	targetRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetRoot, "RULE-FLOOR.md"), []byte("FLOOR: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	repository := testRepository{fingerprints: map[string]string{"project.txt": validationTestFingerprint}}
	return NewSnapshotter(repository, map[string]string{"product": targetRoot}), targetRoot
}

func targetSnapshotRequest() Request {
	req := testValidationRequest()
	req.Binding.ValidatorID = "rulefloor"
	req.Binding.Parameters = map[string]string{"mode": "static", "target": "product"}
	req.Binding.SnapshotFingerprint = ""
	return req
}

// The external ledger's bytes are folded into the scoped snapshot — and
// through snapshot.Fingerprint into the input fingerprint — so a moved
// external repository turns a prior pass STALE instead of silently
// re-passing under the pinned fingerprints.
func TestTargetLedgerFoldsIntoSnapshotAndGoesStaleOnMove(t *testing.T) {
	snapshotter, targetRoot := targetSnapshotFixture(t)
	req := targetSnapshotRequest()

	// Learn-by-running round: an unpinned binding refuses, but the error
	// carries the computed snapshot — the same flow the policy pin uses.
	before, err := snapshotter.Build(context.Background(), req.Claim, req.Binding)
	if validationErrorCode(err) != "repository_snapshot_unavailable" || before.Fingerprint == "" {
		t.Fatalf("probe round: code=%q fingerprint=%q (%v)", validationErrorCode(err), before.Fingerprint, err)
	}

	// Pin it the way claim_set_validation_policy does; a clean build passes.
	req.Binding.SnapshotFingerprint = before.Fingerprint
	pinned, err := snapshotter.Build(context.Background(), req.Claim, req.Binding)
	if err != nil {
		t.Fatal(err)
	}
	inputBefore := InputFingerprint(req.Claim, req.Binding, pinned)

	// Move the EXTERNAL repository: the pinned binding must go stale.
	if err := os.WriteFile(filepath.Join(targetRoot, "RULE-FLOOR.md"), []byte("FLOOR: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = snapshotter.Build(context.Background(), req.Claim, req.Binding)
	if validationErrorCode(err) != "stale_repository_snapshot" {
		t.Fatalf("moved external ledger: error code = %q, want stale_repository_snapshot (%v)", validationErrorCode(err), err)
	}

	// And the recomputed fingerprints differ, so the pinned input
	// fingerprint is stale too — never a silent re-pass.
	req.Binding.SnapshotFingerprint = ""
	after, _ := snapshotter.Build(context.Background(), req.Claim, req.Binding)
	if after.Fingerprint == before.Fingerprint {
		t.Fatal("external ledger change must change the scoped snapshot fingerprint")
	}
	if InputFingerprint(req.Claim, req.Binding, after) == inputBefore {
		t.Fatal("external ledger change must change the input fingerprint")
	}
}

func TestTargetSnapshotRefusals(t *testing.T) {
	snapshotter, targetRoot := targetSnapshotFixture(t)

	unknown := targetSnapshotRequest()
	unknown.Binding.Parameters["target"] = "intruder"
	_, err := snapshotter.Build(context.Background(), unknown.Claim, unknown.Binding)
	if validationErrorCode(err) != "unknown_validation_target" {
		t.Fatalf("unknown target: error code = %q, want unknown_validation_target (%v)", validationErrorCode(err), err)
	}

	manual := targetSnapshotRequest()
	manual.Binding.ValidatorID = "manual"
	_, err = snapshotter.Build(context.Background(), manual.Claim, manual.Binding)
	if validationErrorCode(err) != "unsupported_target_binding" {
		t.Fatalf("manual target: error code = %q, want unsupported_target_binding (%v)", validationErrorCode(err), err)
	}

	if err := os.Remove(filepath.Join(targetRoot, "RULE-FLOOR.md")); err != nil {
		t.Fatal(err)
	}
	missing := targetSnapshotRequest()
	_, err = snapshotter.Build(context.Background(), missing.Claim, missing.Binding)
	if validationErrorCode(err) != "missing_repository_state" {
		t.Fatalf("missing target ledger: error code = %q, want missing_repository_state (%v)", validationErrorCode(err), err)
	}
}
