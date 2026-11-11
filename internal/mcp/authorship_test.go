package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"scrinium/internal/knowledge"
	"scrinium/internal/publicapi"
)

func toolCallError(t *testing.T, server *Server, name string, args map[string]any) publicapi.PublicError {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"name": name, "arguments": args})
	if err != nil {
		t.Fatal(err)
	}
	response, err := server.ToolCall(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	result := response.(map[string]any)
	if result["isError"] != true {
		t.Fatalf("%s was not a semantic MCP error: %#v", name, result)
	}
	return publicResult[publicapi.PublicError](t, result)
}

func TestCapabilitiesListsAuthorshipKinds(t *testing.T) {
	server, _ := newPublicWorkflowServer(t)
	var payload struct {
		AuthorshipKinds []string `json:"authorship_kinds"`
	}
	content := server.Capabilities().(map[string]any)["content"].([]map[string]any)
	if err := json.Unmarshal([]byte(content[0]["text"].(string)), &payload); err != nil {
		t.Fatal(err)
	}
	if want := []string{"owner", "human", "agent", "import", "tool"}; !reflect.DeepEqual(payload.AuthorshipKinds, want) {
		t.Fatalf("capabilities authorship_kinds = %q, want %q", payload.AuthorshipKinds, want)
	}
}

func TestPublicClaimCreateAcceptsToolAuthorshipAsLabelOnly(t *testing.T) {
	server, _ := newPublicWorkflowServer(t)
	sessionID := preparePublicSession(t, server)
	create := func(id string, kind knowledge.AuthorshipKind, origin string) publicapi.ClaimCreateInput {
		return publicapi.ClaimCreateInput{
			SchemaVersion: publicapi.ClaimCreateSchema, ID: id, Subject: "dispatch", Statement: "The lane report was written.",
			Authorship: publicapi.AuthorshipInput{Kind: kind, Origin: origin}, Evidence: []knowledge.Evidence{},
		}
	}
	created := callPublic[publicapi.ClaimResult](t, server, "claim_create", map[string]any{
		"session_id": sessionID, "input": create("TOOL-AUTHORED-1", knowledge.AuthorshipTool, "nuntius"),
	})
	if created.Claim.Authorship.Kind != knowledge.AuthorshipTool || created.Claim.Authorship.Origin != "nuntius" {
		t.Fatalf("unexpected tool authorship: %#v", created.Claim.Authorship)
	}
	agent := callPublic[publicapi.ClaimResult](t, server, "claim_create", map[string]any{
		"session_id": sessionID, "input": create("AGENT-AUTHORED-1", knowledge.AuthorshipAgent, "agent:session"),
	})
	if created.State.Assessment != knowledge.AssessmentAsserted || !reflect.DeepEqual(created.State, agent.State) {
		t.Fatalf("tool state = %#v, agent state = %#v", created.State, agent.State)
	}

	for _, kind := range []knowledge.AuthorshipKind{"Tool", "tools"} {
		doc := toolCallError(t, server, "claim_create", map[string]any{
			"session_id": sessionID, "input": create("TOOL-AUTHORED-2", kind, "nuntius"),
		})
		if want := `invalid authorship kind "` + string(kind) + `"`; !strings.Contains(doc.Message, want) {
			t.Fatalf("refusal message = %q, want %q", doc.Message, want)
		}
	}
	if doc := toolCallError(t, server, "claim_create", map[string]any{
		"session_id": sessionID, "input": create("TOOL-AUTHORED-2", knowledge.AuthorshipTool, ""),
	}); !strings.Contains(doc.Message, "origin and recorded_at are required") {
		t.Fatalf("missing origin refusal message = %q", doc.Message)
	}
	// A server with no begun session has no process-wide session to fall back on.
	unsessioned, _ := newPublicWorkflowServer(t)
	if doc := toolCallError(t, unsessioned, "claim_create", map[string]any{
		"input": create("TOOL-AUTHORED-2", knowledge.AuthorshipTool, "nuntius"),
	}); doc.Code != "session_required" {
		t.Fatalf("tool-authored create without a session: %#v", doc)
	}

	subject := "lanes"
	update := publicapi.ClaimUpdateInput{
		SchemaVersion: publicapi.ClaimUpdateSchema, ClaimID: created.Claim.ID, Subject: &subject, MeaningUnchanged: true,
		ExpectedRevision: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	doc := toolCallError(t, server, "claim_update", map[string]any{"session_id": sessionID, "input": update})
	if doc.Code != "conflict" || doc.CurrentRevision != string(created.Revision) {
		t.Fatalf("tool-authored stale update: %#v", doc)
	}
	update.ExpectedRevision = ""
	if doc := toolCallError(t, server, "claim_update", map[string]any{"session_id": sessionID, "input": update}); doc.Code != "invalid_input" {
		t.Fatalf("tool-authored update without a revision: %#v", doc)
	}
}
