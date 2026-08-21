package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsvc "scrinium/internal/app"
	"scrinium/internal/knowledge"
	"scrinium/internal/publicapi"
	"scrinium/internal/validation"
)

type workflowValidator struct {
	descriptor validation.Descriptor
	outcome    knowledge.ValidationOutcome
}

func (v *workflowValidator) Descriptor() validation.Descriptor { return v.descriptor }

func (v *workflowValidator) Validate(_ context.Context, req validation.Request) (knowledge.ValidationResult, error) {
	assurance := v.descriptor.MaximumAssurance
	if req.Binding.Parameters["mode"] == "static" {
		assurance = knowledge.AssuranceObservation
	}
	completed := time.Now().UTC()
	return knowledge.ValidationResult{
		ID: req.ResultID, BindingID: req.Binding.ID, ValidatorID: v.descriptor.ID,
		ValidatorVersion: v.descriptor.Version, BindingVersion: req.Binding.BindingVersion,
		RepositoryRevision: req.Repository.Revision, SnapshotFingerprint: req.Repository.Fingerprint,
		InputFingerprint: req.InputFingerprint, Assurance: assurance, Outcome: v.outcome,
		ReasonCode: "fixture_result", Reason: "deterministic public workflow fixture",
		EvidenceIDs: append([]string(nil), req.Binding.EvidenceIDs...), StartedAt: req.StartedAt,
		CompletedAt: completed,
	}, nil
}

func newPublicWorkflowServer(t *testing.T) (*Server, string) {
	t.Helper()
	repository := t.TempDir()
	configPath := filepath.Join(repository, "scrinium.json")
	config := fmt.Sprintf(`{"wiki_root":"llm-wiki","validators":{"rulefloor":{"executable":%q},"gograph":{"executable":%q}}}`,
		filepath.Join(repository, "missing-rulefloor"), filepath.Join(repository, "missing-gograph"))
	if err := os.WriteFile(configPath, []byte(config), 0644); err != nil {
		t.Fatal(err)
	}
	application, err := appsvc.Open(context.Background(), configPath, appsvc.Content{StandardFiles: map[string]string{
		"index.md": "# Index\n", "agent-rules.md": "# Agent Rules\n", "workflows/ingest.md": "# Ingest\n",
		"log.md": "# Log\n", "source-registry.md": "# Sources\n",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetupWiki(context.Background()); err != nil {
		t.Fatal(err)
	}
	return New(application, "test"), repository
}

func publicResult[T any](t *testing.T, result any) T {
	t.Helper()
	content := result.(map[string]any)["content"].([]map[string]any)
	var target T
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &target); err != nil {
		t.Fatal(err)
	}
	return target
}

func callPublic[T any](t *testing.T, server *Server, name string, args map[string]any) T {
	t.Helper()
	result, err := server.CallTool(context.Background(), name, args)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return publicResult[T](t, result)
}

func preparePublicSession(t *testing.T, server *Server) string {
	t.Helper()
	status := callPublic[publicapi.SessionResult](t, server, "session_begin", map[string]any{})
	for _, path := range []string{"index.md", "agent-rules.md", "workflows/ingest.md"} {
		if _, err := server.CallTool(context.Background(), "read_wiki_page", map[string]any{"session_id": status.SessionID, "path": path}); err != nil {
			t.Fatal(err)
		}
	}
	return status.SessionID
}

func TestPublicClaimValidationWorkflowAndTrustPresentation(t *testing.T) {
	server, repository := newPublicWorkflowServer(t)
	sessionID := preparePublicSession(t, server)
	rulefloor := &workflowValidator{descriptor: validation.Descriptor{
		ID: "rulefloor", Version: "fixture-1", SupportedBindingVersions: []string{"rulefloor.binding.v1"},
		MaximumAssurance: knowledge.AssuranceVerification,
	}, outcome: knowledge.OutcomePass}
	gograph := &workflowValidator{descriptor: validation.Descriptor{
		ID: "gograph", Version: "fixture-1", SupportedBindingVersions: []string{"gograph.binding.v1"},
		MaximumAssurance: knowledge.AssuranceObservation,
	}, outcome: knowledge.OutcomePass}
	if err := server.app.RegisterValidator(rulefloor); err != nil {
		t.Fatal(err)
	}
	if err := server.app.RegisterValidator(gograph); err != nil {
		t.Fatal(err)
	}

	create := publicapi.ClaimCreateInput{
		SchemaVersion: publicapi.ClaimCreateSchema, ID: "AUTH-ADMIN-LOCAL-1", Subject: "authentication",
		Statement:  "Site administrators retain local authentication.",
		Authorship: publicapi.AuthorshipInput{Kind: knowledge.AuthorshipOwner, Origin: "owner:project"}, Evidence: []knowledge.Evidence{},
	}
	view := callPublic[publicapi.ClaimResult](t, server, "claim_create", map[string]any{"session_id": sessionID, "input": create})
	if view.Revision == "" || view.State.Assessment != knowledge.AssessmentAsserted {
		t.Fatalf("unexpected created claim: %#v", view)
	}

	repositoryPath := filepath.Join(repository, "project", "auth.go")
	if err := os.MkdirAll(filepath.Dir(repositoryPath), 0755); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("package project\n")
	if err := os.WriteFile(repositoryPath, bytes, 0644); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(bytes)
	fingerprint := fmt.Sprintf("sha256:%x", digest[:])
	captured := view.Claim.CreatedAt
	evidence := knowledge.Evidence{
		ID: "EVD-AUTH-REPOSITORY-1", Kind: knowledge.EvidenceRepositoryReference, Polarity: knowledge.PolaritySupports,
		Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "repo:project/auth.go"},
		Locator: "repo:project/auth.go", Scope: "authentication implementation", Fingerprint: fingerprint,
		Availability: knowledge.AvailabilityAvailable, ObservedFingerprint: fingerprint,
		CapturedAt: captured, CheckedAt: &captured, DerivedFrom: []string{},
	}
	view = callPublic[publicapi.ClaimResult](t, server, "claim_add_evidence", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimEvidenceInput{
			SchemaVersion: publicapi.ClaimEvidenceSchema, ClaimID: view.Claim.ID,
			ExpectedRevision: view.Revision, Evidence: evidence,
		},
	})

	snapshot := validation.RepositorySnapshot{Fingerprint: validation.RepositoryFingerprint([]validation.RepositoryEntry{{Path: "project/auth.go", Fingerprint: fingerprint}})}
	bindings := []knowledge.ValidationBinding{
		{ID: "VAL-AUTH-RULEFLOOR-1", ValidatorID: "rulefloor", BindingVersion: "rulefloor.binding.v1", Reference: "AUTH-ADMIN-1", Parameters: map[string]string{"mode": "execute"}, Required: true, RequiredAssurance: knowledge.AssuranceVerification, EvidenceIDs: []string{evidence.ID}, SnapshotFingerprint: snapshot.Fingerprint},
		{ID: "VAL-AUTH-GOGRAPH-1", ValidatorID: "gograph", BindingVersion: "gograph.binding.v1", Reference: "auth structure", Parameters: map[string]string{"predicate": "symbol_exists"}, Required: false, RequiredAssurance: knowledge.AssuranceObservation, EvidenceIDs: []string{evidence.ID}, SnapshotFingerprint: snapshot.Fingerprint},
	}
	for index := range bindings {
		bindings[index].InputFingerprint = validation.InputFingerprint(view.Claim, bindings[index], snapshot)
	}
	view = callPublic[publicapi.ClaimResult](t, server, "claim_set_validation_policy", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimPolicyInput{
			SchemaVersion: publicapi.ClaimPolicySchema, ClaimID: view.Claim.ID,
			ExpectedRevision: view.Revision, Policy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: bindings},
		},
	})

	staticBinding := bindings[0]
	staticBinding.Parameters = map[string]string{"mode": "static"}
	staticBinding.RequiredAssurance = knowledge.AssuranceObservation
	staticBinding.InputFingerprint = validation.InputFingerprint(view.Claim, staticBinding, snapshot)
	staticPolicy := &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{staticBinding}}
	staticView := callPublic[publicapi.ClaimResult](t, server, "claim_set_validation_policy", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimPolicyInput{SchemaVersion: publicapi.ClaimPolicySchema, ClaimID: view.Claim.ID, ExpectedRevision: view.Revision, Policy: staticPolicy},
	})
	staticAttempt := callPublic[publicapi.ClaimValidationRun](t, server, "claim_validate", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimValidationInput{SchemaVersion: publicapi.ClaimValidationSchema, ClaimID: view.Claim.ID, ExpectedRevision: staticView.Revision, Selection: "binding", BindingID: staticBinding.ID},
	})
	if staticAttempt.Claim.State.Assessment == knowledge.AssessmentVerified || staticAttempt.Results[0].Assurance != knowledge.AssuranceObservation {
		t.Fatalf("Rulefloor static result claimed verification: %#v", staticAttempt)
	}

	view = callPublic[publicapi.ClaimResult](t, server, "claim_set_validation_policy", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimPolicyInput{SchemaVersion: publicapi.ClaimPolicySchema, ClaimID: view.Claim.ID, ExpectedRevision: staticAttempt.Claim.Revision, Policy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: bindings}},
	})
	ruleAttempt := callPublic[publicapi.ClaimValidationRun](t, server, "claim_validate", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimValidationInput{SchemaVersion: publicapi.ClaimValidationSchema, ClaimID: view.Claim.ID, ExpectedRevision: view.Revision, Selection: "binding", BindingID: bindings[0].ID},
	})
	graphAttempt := callPublic[publicapi.ClaimValidationRun](t, server, "claim_validate", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimValidationInput{SchemaVersion: publicapi.ClaimValidationSchema, ClaimID: view.Claim.ID, ExpectedRevision: ruleAttempt.Claim.Revision, Selection: "binding", BindingID: bindings[1].ID},
	})
	if graphAttempt.Results[0].Assurance != knowledge.AssuranceObservation {
		t.Fatalf("Gograph exceeded observation assurance: %#v", graphAttempt.Results[0])
	}
	if graphAttempt.Claim.State.Assessment != knowledge.AssessmentVerified || graphAttempt.Claim.State.Freshness != knowledge.FreshnessCurrent {
		t.Fatalf("expected verified and current dimensions to remain explicit: %#v", graphAttempt.Claim.State)
	}

	rulefloor.outcome = knowledge.OutcomeCannotEvaluate
	degraded := callPublic[publicapi.ClaimValidationRun](t, server, "claim_validate", map[string]any{
		"session_id": sessionID, "input": publicapi.ClaimValidationInput{SchemaVersion: publicapi.ClaimValidationSchema, ClaimID: view.Claim.ID, ExpectedRevision: graphAttempt.Claim.Revision, Selection: "binding", BindingID: bindings[0].ID},
	})
	if degraded.Claim.State.Assessment == knowledge.AssessmentVerified {
		t.Fatalf("cannot_evaluate retained verified presentation: %#v", degraded.Claim.State)
	}

	if err := os.WriteFile(repositoryPath, []byte("package project\n// changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stale := callPublic[publicapi.ClaimResult](t, server, "claim_get", map[string]any{"claim_id": view.Claim.ID})
	if stale.State.Freshness != knowledge.FreshnessStale {
		t.Fatalf("changed repository evidence did not make claim stale: %#v", stale.State)
	}
}

func TestPublicClaimConflictIsStructured(t *testing.T) {
	server, _ := newPublicWorkflowServer(t)
	sessionID := preparePublicSession(t, server)
	created := callPublic[publicapi.ClaimResult](t, server, "claim_create", map[string]any{
		"session_id": sessionID,
		"input":      publicapi.ClaimCreateInput{SchemaVersion: publicapi.ClaimCreateSchema, ID: "PROJECT-API-1", Subject: "api", Statement: "The API is versioned.", Authorship: publicapi.AuthorshipInput{Kind: knowledge.AuthorshipOwner, Origin: "owner:project"}, Evidence: []knowledge.Evidence{}},
	})
	subject, statement := "api", "The public API is versioned."
	update := publicapi.ClaimUpdateInput{SchemaVersion: publicapi.ClaimUpdateSchema, ClaimID: created.Claim.ID, ExpectedRevision: created.Revision, Subject: &subject, Statement: &statement, MeaningUnchanged: true}
	_ = callPublic[publicapi.ClaimResult](t, server, "claim_update", map[string]any{"session_id": sessionID, "input": update})
	payload, err := json.Marshal(map[string]any{"name": "claim_update", "arguments": map[string]any{"session_id": sessionID, "input": update}})
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.ToolCall(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	result := response.(map[string]any)
	if result["isError"] != true {
		t.Fatalf("stale update was not a semantic MCP error: %#v", result)
	}
	doc := publicResult[publicapi.PublicError](t, result)
	if doc.Code != "conflict" || doc.ExpectedRevision == "" || doc.CurrentRevision == "" {
		t.Fatalf("conflict metadata missing: %#v", doc)
	}
}

func TestPublicSourceWorkflowUsesCanonicalRecords(t *testing.T) {
	server, repository := newPublicWorkflowServer(t)
	sessionID := preparePublicSession(t, server)
	rawPath := filepath.Join(repository, "raw", "owner-note.txt")
	if err := os.MkdirAll(filepath.Dir(rawPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rawPath, []byte("owner decision\n"), 0644); err != nil {
		t.Fatal(err)
	}
	registered := callPublic[publicapi.SourceResult](t, server, "source_register", map[string]any{
		"session_id": sessionID,
		"input": publicapi.SourceRegisterInput{
			SchemaVersion: publicapi.SourceRegisterSchema, SourceID: "SRC-20260821-owner-note", Title: "Owner note",
			RawPath: "raw/owner-note.txt", SourceType: "owner_input", Origin: "owner", TrustLevel: "trusted-owner",
			ReceivedDate: "2026-08-21", IngestDate: "2026-08-21", Summary: "Owner decision source.",
			DerivedClaims: []string{}, DerivedPages: []string{}, ProvenanceNotes: []string{},
		},
	})
	if registered.Revision == "" || registered.Source.RawFingerprint == "" {
		t.Fatalf("canonical source result lacks revision/fingerprint: %#v", registered)
	}
	got := callPublic[publicapi.SourceResult](t, server, "source_get", map[string]any{"source_id": registered.Source.ID})
	if got.Revision != registered.Revision {
		t.Fatalf("source revision changed on read: %q != %q", got.Revision, registered.Revision)
	}
	listed := callPublic[publicapi.SourceListResult](t, server, "source_list", map[string]any{})
	if len(listed.Sources) != 1 || listed.Sources[0].Source.ID != registered.Source.ID {
		t.Fatalf("unexpected source listing: %#v", listed)
	}
	migration := callPublic[publicapi.SourceMigrationStatus](t, server, "source_migration_status", map[string]any{})
	if migration.SchemaVersion != publicapi.SourceMigrationSchema {
		t.Fatalf("unexpected migration schema: %#v", migration)
	}

	if err := os.WriteFile(rawPath, []byte("owner decision updated\n"), 0644); err != nil {
		t.Fatal(err)
	}
	refreshed := callPublic[publicapi.SourceResult](t, server, "source_refresh", map[string]any{
		"session_id": sessionID,
		"input":      publicapi.SourceRefreshInput{SchemaVersion: publicapi.SourceRefreshSchema, SourceID: registered.Source.ID, ExpectedRevision: registered.Revision},
	})
	if refreshed.Revision == registered.Revision || refreshed.Source.RawFingerprint == registered.Source.RawFingerprint {
		t.Fatalf("explicit source refresh did not accept changed bytes: %#v", refreshed)
	}
}
