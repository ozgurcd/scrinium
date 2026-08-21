package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"scrinium/internal/session"
)

func newTestService(t *testing.T, protected []string) *Service {
	t.Helper()
	repository := t.TempDir()
	missingRulefloor := filepath.Join(repository, "missing-rulefloor")
	missingGograph := filepath.Join(repository, "missing-gograph")
	config := fmt.Sprintf(`{"wiki_root":"llm-wiki","validators":{"rulefloor":{"executable":%q},"gograph":{"executable":%q}}}`, missingRulefloor, missingGograph)
	if protected != nil {
		config = fmt.Sprintf(`{"wiki_root":"llm-wiki","write_governance":{"protected_files":["rules.md"]},"validators":{"rulefloor":{"executable":%q},"gograph":{"executable":%q}}}`, missingRulefloor, missingGograph)
	}
	configPath := filepath.Join(repository, "scrinium.json")
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	service, err := Open(context.Background(), configPath, Content{
		Guide: "# Guide\n",
		StandardFiles: map[string]string{
			"index.md":            "# Index\n",
			"agent-rules.md":      "# Rules\n",
			"workflows/ingest.md": "# Ingest\n",
			"source-registry.md":  "# Source Registry\n\n## Sources\n",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestTypedApplicationRegistersSource(t *testing.T) {
	service := newTestService(t, nil)
	ctx := context.Background()
	if err := service.repository.Write(ctx, "raw/phase-one.md", []byte("phase one source\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetupWiki(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := service.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md", "workflows/ingest.md"} {
		if _, err := service.ReadPage(ctx, PageRequest{Path: path, SessionID: status.SessionID}); err != nil {
			t.Fatal(err)
		}
	}

	path, err := service.RegisterSource(ctx, RegisterSourceRequest{
		SessionID: status.SessionID, SourceID: "SRC-20260820-phase-one", Title: "Phase One", RawPath: "raw/phase-one.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "sources/SRC-20260820-phase-one.md" {
		t.Fatalf("unexpected summary path %q", path)
	}
	page, err := service.ReadPage(ctx, PageRequest{Path: path, SessionID: status.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page.Content, "Source ID: SRC-20260820-phase-one") {
		t.Fatalf("source summary lost compatibility metadata: %s", page.Content)
	}
}

func TestTypedApplicationReturnsGovernanceError(t *testing.T) {
	service := newTestService(t, []string{"rules.md"})
	ctx := context.Background()
	if _, err := service.SetupWiki(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := service.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		if _, err := service.ReadPage(ctx, PageRequest{Path: path, SessionID: status.SessionID}); err != nil {
			t.Fatal(err)
		}
	}
	err = service.UpdatePage(ctx, WritePageRequest{Path: "rules.md", Content: "changed", SessionID: status.SessionID})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Kind != ErrorGovernance {
		t.Fatalf("expected typed governance error, got %v", err)
	}
}

func TestDurableSessionAcrossApplicationInstances(t *testing.T) {
	first := newTestService(t, nil)
	ctx := context.Background()
	if _, err := first.SetupWiki(ctx); err != nil {
		t.Fatal(err)
	}
	if err := first.store.Write(ctx, "notes.md", []byte("before\n"), 0644); err != nil {
		t.Fatal(err)
	}
	started, err := first.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}

	openAgain := func() *Service {
		t.Helper()
		service, openErr := Open(ctx, first.ConfigPath(), Content{
			Guide: "# Guide\n",
			StandardFiles: map[string]string{
				"index.md": "# Index\n", "agent-rules.md": "# Rules\n",
			},
		})
		if openErr != nil {
			t.Fatal(openErr)
		}
		return service
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		if _, err := openAgain().ReadPage(ctx, PageRequest{Path: path, SessionID: started.SessionID}); err != nil {
			t.Fatal(err)
		}
	}
	if err := openAgain().UpdatePage(ctx, WritePageRequest{Path: "notes.md", Content: "after\n", SessionID: started.SessionID}); err != nil {
		t.Fatal(err)
	}
	status, err := openAgain().SessionStatus(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.DocumentsWritten) != 1 || !status.NeedsLog {
		t.Fatalf("cross-instance write was not tracked: %#v", status)
	}
	if err := openAgain().Append(ctx, AppendRequest{Path: "log.md", Content: "maintenance", SessionID: started.SessionID}); err != nil {
		t.Fatal(err)
	}
	finished, err := openAgain().FinishSession(ctx, started.SessionID)
	if err != nil || finished.Status != session.Finished {
		t.Fatalf("cross-instance finish = %#v, %v", finished, err)
	}
}

func TestOpenRejectsInvalidGovernancePattern(t *testing.T) {
	repository := t.TempDir()
	configPath := filepath.Join(repository, "scrinium.json")
	config := `{"wiki_root":"llm-wiki","write_governance":{"protected_files":["["]}}`
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), configPath, Content{}); err == nil || !strings.Contains(err.Error(), "invalid write governance") {
		t.Fatalf("expected invalid governance error, got %v", err)
	}
}

func TestOpenKeepsExternalValidatorsOptionalAndCopiesConfiguration(t *testing.T) {
	repository := t.TempDir()
	configPath := filepath.Join(repository, "scrinium.json")
	missingRulefloor := filepath.Join(repository, "missing-rulefloor")
	missingGograph := filepath.Join(repository, "missing-gograph")
	config := fmt.Sprintf(`{"wiki_root":"llm-wiki","validators":{"rulefloor":{"executable":%q},"gograph":{"executable":%q}}}`, missingRulefloor, missingGograph)
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	service, err := Open(context.Background(), configPath, Content{})
	if err != nil {
		t.Fatalf("external validator unavailability must not block startup: %v", err)
	}
	loaded := service.Config()
	if loaded.Validators == nil || loaded.Validators.Rulefloor == nil || loaded.Validators.Rulefloor.Executable != missingRulefloor || loaded.Validators.Gograph == nil || loaded.Validators.Gograph.Executable != missingGograph {
		t.Fatalf("unexpected validator configuration: %#v", loaded.Validators)
	}
	if _, err := service.ValidatorDescriptor("rulefloor"); err == nil {
		t.Fatal("unavailable Rulefloor must not be registered as available")
	}
	if _, err := service.ValidatorDescriptor("gograph"); err == nil {
		t.Fatal("unavailable Gograph must not be registered as available")
	}
	loaded.Validators.Rulefloor.Executable = "changed"
	loaded.Validators.Gograph.Executable = "changed"
	if service.Config().Validators.Rulefloor.Executable != missingRulefloor || service.Config().Validators.Gograph.Executable != missingGograph {
		t.Fatal("Config returned shared validator configuration")
	}
	if got := configuredRulefloorExecutable(&Config{}); got != "rulefloor" {
		t.Fatalf("default Rulefloor executable = %q", got)
	}
	if got := configuredGographExecutable(&Config{}); got != "gograph" {
		t.Fatalf("default Gograph executable = %q", got)
	}
}

func TestOpenHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(ctx, filepath.Join(t.TempDir(), "scrinium.json"), Content{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
