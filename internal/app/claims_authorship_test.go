package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"scrinium/internal/knowledge"
)

func toolClaimRequest(sessionID, id string) CreateClaimRequest {
	return CreateClaimRequest{
		SessionID: sessionID, ID: id, Subject: "dispatch", Statement: "The lane report was written.",
		AuthorshipKind: knowledge.AuthorshipTool, AuthorOrigin: "nuntius",
	}
}

func TestToolAuthoredClaimIsStoredWithItsOrigin(t *testing.T) {
	service, ctx, sessionID := prepareClaimService(t)
	view, err := service.CreateClaim(ctx, toolClaimRequest(sessionID, "TOOL-AUTHORED-1"))
	if err != nil {
		t.Fatal(err)
	}
	if view.Claim.Authorship.Kind != knowledge.AuthorshipTool || view.Claim.Authorship.Origin != "nuntius" || view.Claim.Authorship.RecordedAt.IsZero() {
		t.Fatalf("unexpected tool authorship: %#v", view.Claim.Authorship)
	}
	if view.State.Assessment != knowledge.AssessmentAsserted {
		t.Fatalf("tool authorship must grant no assessment, got %s", view.State.Assessment)
	}

	missingOrigin := toolClaimRequest(sessionID, "TOOL-AUTHORED-2")
	missingOrigin.AuthorOrigin = ""
	var validationErr *knowledge.ValidationError
	if _, err := service.CreateClaim(ctx, missingOrigin); !errors.As(err, &validationErr) || validationErr.Code != "invalid_authorship" {
		t.Fatalf("tool authorship without origin must be refused, got %v", err)
	}
	for _, kind := range []knowledge.AuthorshipKind{"Tool", "tools"} {
		nearMiss := toolClaimRequest(sessionID, "TOOL-AUTHORED-3")
		nearMiss.AuthorshipKind = kind
		_, err := service.CreateClaim(ctx, nearMiss)
		if !errors.As(err, &validationErr) || validationErr.Code != "invalid_enum" || !strings.Contains(err.Error(), strconv.Quote(string(kind))) {
			t.Fatalf("authorship kind %q must be refused by name, got %v", kind, err)
		}
	}
	listed, err := service.ListClaims(ctx, view.Claim.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Claim.ID != view.Claim.ID {
		t.Fatalf("refused creates left records behind: %#v", listed)
	}
}

func TestToolAuthoredClaimDerivesTheSameStateAsAgentAuthored(t *testing.T) {
	service, ctx, sessionID := prepareClaimService(t)
	agentRequest := toolClaimRequest(sessionID, "AGENT-AUTHORED-1")
	agentRequest.AuthorshipKind, agentRequest.AuthorOrigin = knowledge.AuthorshipAgent, "agent:session"
	views := map[knowledge.AuthorshipKind]ClaimView{}
	for _, req := range []CreateClaimRequest{toolClaimRequest(sessionID, "TOOL-AUTHORED-1"), agentRequest} {
		view, err := service.CreateClaim(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		view, err = service.AttachEvidence(ctx, AttachEvidenceRequest{
			SessionID: sessionID, ClaimID: view.Claim.ID, Evidence: appEvidence(view.Claim.CreatedAt), ExpectedRevision: view.Revision,
		})
		if err != nil {
			t.Fatal(err)
		}
		views[req.AuthorshipKind] = view
	}
	tool, agent := views[knowledge.AuthorshipTool], views[knowledge.AuthorshipAgent]
	if tool.State.Assessment != knowledge.AssessmentSourced || !reflect.DeepEqual(tool.State, agent.State) {
		t.Fatalf("tool state = %#v, agent state = %#v", tool.State, agent.State)
	}
}

func TestToolAuthoredMutationsObeySessionRevisionAndGovernance(t *testing.T) {
	var appErr *Error
	unsessioned := newTestService(t, nil)
	if _, err := unsessioned.CreateClaim(context.Background(), toolClaimRequest("", "TOOL-AUTHORED-1")); !errors.As(err, &appErr) || appErr.Kind != ErrorSession {
		t.Fatalf("tool-authored create without a session must be refused, got %v", err)
	}

	service, ctx, sessionID := prepareClaimService(t)
	view, err := service.CreateClaim(ctx, toolClaimRequest(sessionID, "TOOL-AUTHORED-1"))
	if err != nil {
		t.Fatal(err)
	}
	subject := "lanes"
	if _, err := service.UpdateClaim(ctx, UpdateClaimRequest{
		SessionID: sessionID, ID: view.Claim.ID, Subject: &subject, MeaningUnchanged: true,
	}); !errors.As(err, &appErr) || appErr.Kind != ErrorInvalid {
		t.Fatalf("tool-authored update without a revision must be refused, got %v", err)
	}
	_, err = service.UpdateClaim(ctx, UpdateClaimRequest{
		SessionID: sessionID, ID: view.Claim.ID, Subject: &subject, MeaningUnchanged: true,
		ExpectedRevision: ClaimRevision("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	})
	var conflict *ClaimConflictError
	if !errors.As(err, &conflict) || conflict.CurrentRevision != view.Revision {
		t.Fatalf("tool-authored stale update must conflict with the current revision, got %v", err)
	}
	if _, err := service.UpdateClaim(ctx, UpdateClaimRequest{
		ID: view.Claim.ID, Subject: &subject, MeaningUnchanged: true, ExpectedRevision: view.Revision,
	}); !errors.As(err, &appErr) || appErr.Kind != ErrorSession {
		t.Fatalf("tool-authored update without a session must be refused, got %v", err)
	}
	current, err := service.GetClaim(ctx, view.Claim.ID, view.Claim.UpdatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision != view.Revision {
		t.Fatalf("refused mutations changed the claim: before=%s after=%s", view.Revision, current.Revision)
	}

	repository := t.TempDir()
	configPath := filepath.Join(repository, "scrinium.json")
	if err := os.WriteFile(configPath, []byte(`{"wiki_root":"llm-wiki","write_governance":{"protected_files":["claims/*"]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	governed, err := Open(ctx, configPath, Content{StandardFiles: map[string]string{"index.md": "# Index\n", "agent-rules.md": "# Rules\n"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := governed.CreateClaim(ctx, toolClaimRequest("", "TOOL-AUTHORED-1")); !errors.As(err, &appErr) || appErr.Kind != ErrorGovernance {
		t.Fatalf("tool-authored create in a protected path must be refused by governance, got %v", err)
	}
}
