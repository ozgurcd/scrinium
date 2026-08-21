package governance

import "testing"

func TestPolicyWriteAndAppendBehavior(t *testing.T) {
	policy, err := New([]string{"rules.md", "architecture/*", "core-decisions/*"})
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"rules.md", "architecture/overview.md", "architecture/nested/design.md"} {
		if policy.AllowsWrite(path) {
			t.Fatalf("expected %s to be protected", path)
		}
	}
	if !policy.AllowsWrite("notes/current.md") {
		t.Fatal("expected ordinary notes to be writable")
	}
	if policy.AllowsAppend("rules.md") {
		t.Fatal("expected direct protected file append to be blocked")
	}
	if !policy.AllowsAppend("core-decisions/record.md") {
		t.Fatal("expected compatibility append behavior under protected directory")
	}
}

func TestPolicyRejectsInvalidPattern(t *testing.T) {
	if _, err := New([]string{"architecture/["}); err == nil {
		t.Fatal("expected malformed protected pattern to be rejected")
	}
}
