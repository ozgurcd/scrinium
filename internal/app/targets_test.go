package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openWithTargets(t *testing.T, targetsJSON string) (*Service, error) {
	t.Helper()
	repository := t.TempDir()
	missingRulefloor := filepath.Join(repository, "missing-rulefloor")
	missingGograph := filepath.Join(repository, "missing-gograph")
	config := fmt.Sprintf(`{"wiki_root":"llm-wiki","validation_targets":%s,"validators":{"rulefloor":{"executable":%q},"gograph":{"executable":%q}}}`, targetsJSON, missingRulefloor, missingGograph)
	configPath := filepath.Join(repository, "scrinium.json")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return Open(context.Background(), configPath, Content{Guide: "# Guide\n", StandardFiles: map[string]string{"index.md": "# Index\n"}})
}

func TestValidationTargetConfigRefusals(t *testing.T) {
	real := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		targets string
		message string
	}{
		{name: "path-shaped name", targets: fmt.Sprintf(`{"../up":%q}`, real), message: "never a path"},
		{name: "uppercase name", targets: fmt.Sprintf(`{"Product":%q}`, real), message: "never a path"},
		{name: "missing root", targets: fmt.Sprintf(`{"gone":%q}`, filepath.Join(real, "missing")), message: "does not resolve"},
		{name: "symlinked root", targets: fmt.Sprintf(`{"linked":%q}`, link), message: "symlink"},
		{name: "non-directory root", targets: fmt.Sprintf(`{"flat":%q}`, file), message: "not a directory"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := openWithTargets(t, test.targets)
			if err == nil {
				t.Fatalf("config %s must refuse", test.targets)
			}
			if !strings.Contains(err.Error(), test.message) {
				t.Fatalf("refusal %q must mention %q", err.Error(), test.message)
			}
		})
	}
}

func TestValidationTargetsResolveAndAreNamedInCapabilities(t *testing.T) {
	targetRoot := t.TempDir()
	service, err := openWithTargets(t, fmt.Sprintf(`{"product":%q,"another":%q}`, targetRoot, targetRoot))
	if err != nil {
		t.Fatal(err)
	}
	names := service.ValidationTargetNames()
	if len(names) != 2 || names[0] != "another" || names[1] != "product" {
		t.Fatalf("target names = %v, want sorted [another product]", names)
	}
	copied := service.Config()
	copied.ValidationTargets["product"] = "mutated"
	if service.Config().ValidationTargets["product"] == "mutated" {
		t.Fatal("Config() must return a copy of the target map")
	}
}

// Scrinium's writes stay under wiki_root even with external read targets
// configured: a page path that escapes the wiki root is refused.
func TestWritesStayUnderWikiRootWithTargetsConfigured(t *testing.T) {
	targetRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetRoot, "RULE-FLOOR.md"), []byte("FLOOR: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := openWithTargets(t, fmt.Sprintf(`{"product":%q}`, targetRoot))
	if err != nil {
		t.Fatal(err)
	}
	sessionView, err := service.BeginSession(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, escape := range []string{"../escape.md", "../../RULE-FLOOR.md"} {
		writeErr := service.UpdatePage(context.Background(), WritePageRequest{SessionID: sessionView.SessionID, Path: escape, Content: "intrusion"})
		if writeErr == nil {
			t.Fatalf("write to %q must be refused", escape)
		}
	}
	if _, statErr := os.Stat(filepath.Join(targetRoot, "escape.md")); !os.IsNotExist(statErr) {
		t.Fatal("no file may appear in the external target root")
	}
	ledger, err := os.ReadFile(filepath.Join(targetRoot, "RULE-FLOOR.md"))
	if err != nil || string(ledger) != "FLOOR: 0\n" {
		t.Fatalf("external ledger must be byte-unchanged, got %q (%v)", ledger, err)
	}
}
