package scrinium

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"scrinium/internal/knowledge"
	"scrinium/internal/publicapi"
)

func authorshipCLIInput(id, kind string) string {
	return `{"schema_version":"scrinium.claim-create/v1","id":"` + id + `","subject":"dispatch","statement":"The lane report was written.","authorship":{"kind":"` + kind + `","origin":"nuntius"},"evidence":[]}`
}

func TestPublicCLIClaimCreateAcceptsToolAuthorship(t *testing.T) {
	repository := preparePublicCLIRepository(t)
	var session publicapi.SessionResult
	if err := json.Unmarshal(runPublicCLIJSON(t, "session_begin", "--repo", repository, "--json"), &session); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		runCLISessionCommand(t, "read_wiki_page", "--repo", repository, "--session", session.SessionID, "--path", path)
	}
	results := map[string]publicapi.ClaimResult{}
	for id, kind := range map[string]string{"CLI-TOOL-1": "tool", "CLI-AGENT-1": "agent"} {
		var created publicapi.ClaimResult
		data := runPublicCLIJSON(t, "claim_create", "--repo", repository, "--session", session.SessionID, "--input-json", authorshipCLIInput(id, kind), "--json")
		if err := json.Unmarshal(data, &created); err != nil {
			t.Fatal(err)
		}
		results[kind] = created
	}
	tool := results["tool"]
	if tool.Claim.Authorship.Kind != knowledge.AuthorshipTool || tool.Claim.Authorship.Origin != "nuntius" {
		t.Fatalf("unexpected tool authorship: %#v", tool.Claim.Authorship)
	}
	if !reflect.DeepEqual(tool.State, results["agent"].State) {
		t.Fatalf("tool state = %#v, agent state = %#v", tool.State, results["agent"].State)
	}

	for _, kind := range []string{"Tool", "tools"} {
		var stdout, stderr bytes.Buffer
		code := RunCLI([]string{"claim_create", "--repo", repository, "--session", session.SessionID, "--input-json", authorshipCLIInput("CLI-TOOL-2", kind), "--json"}, &stdout, &stderr)
		if code == 0 || stderr.Len() != 0 {
			t.Fatalf("authorship kind %q code=%d stderr=%q stdout=%q", kind, code, stderr.String(), stdout.String())
		}
		var publicErr publicapi.PublicError
		if err := json.Unmarshal(stdout.Bytes(), &publicErr); err != nil {
			t.Fatalf("machine error is not JSON: %v: %s", err, stdout.String())
		}
		if want := `invalid authorship kind \"` + kind + `\"`; !strings.Contains(stdout.String(), want) {
			t.Fatalf("refusal = %s, want it to name %s", stdout.String(), want)
		}
	}

	var stdout, stderr bytes.Buffer
	if code := RunCLI([]string{"claim_create", "--repo", repository, "--input-json", authorshipCLIInput("CLI-TOOL-2", "tool"), "--json"}, &stdout, &stderr); code == 0 {
		t.Fatalf("tool-authored create without a session must be refused: %s", stdout.String())
	}
}

func TestPublicCLICapabilitiesListsAuthorshipKinds(t *testing.T) {
	repository := preparePublicCLIRepository(t)
	var payload struct {
		AuthorshipKinds []string `json:"authorship_kinds"`
	}
	if err := json.Unmarshal(runPublicCLIJSON(t, "capabilities", "--repo", repository, "--json"), &payload); err != nil {
		t.Fatal(err)
	}
	if want := []string{"owner", "human", "agent", "import", "tool"}; !reflect.DeepEqual(payload.AuthorshipKinds, want) {
		t.Fatalf("capabilities authorship_kinds = %q, want %q", payload.AuthorshipKinds, want)
	}
}
