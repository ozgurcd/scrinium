package scrinium

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"scrinium/internal/publicapi"
)

// prepareClaimListCLIRepository builds two discriminable claims through
// the CLI itself: CLI-QRY-SYMBOL-1 (active, symbol evidence) and
// CLI-QRY-GONE-1 (withdrawn, no evidence).
func prepareClaimListCLIRepository(t *testing.T) string {
	t.Helper()
	repository := preparePublicCLIRepository(t)
	begin := runPublicCLIJSON(t, "session_begin", "--repo", repository, "--json")
	var session publicapi.SessionResult
	if err := json.Unmarshal(begin, &session); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		runCLISessionCommand(t, "read_wiki_page", "--repo", repository, "--session", session.SessionID, "--path", path)
	}
	runPublicCLIJSON(t, "claim_create", "--repo", repository, "--session", session.SessionID, "--json", "--input-json",
		`{"schema_version":"scrinium.claim-create/v1","id":"CLI-QRY-SYMBOL-1","subject":"symbol claim","statement":"The symbol exists.","authorship":{"kind":"owner","origin":"owner:test"},"evidence":[{"id":"EV-1","kind":"symbol_reference","polarity":"supports","origin":{"kind":"repository","reference":"fixture"},"locator":"symbol:example.com/project/internal/api::NewOSSEngine","scope":"query fixture","availability":"available","captured_at":"2026-08-22T12:00:00Z","derived_from":[]}]}`)
	createdBytes := runPublicCLIJSON(t, "claim_create", "--repo", repository, "--session", session.SessionID, "--json", "--input-json",
		`{"schema_version":"scrinium.claim-create/v1","id":"CLI-QRY-GONE-1","subject":"withdrawn claim","statement":"This claim is withdrawn.","authorship":{"kind":"owner","origin":"owner:test"},"evidence":[]}`)
	var created publicapi.ClaimResult
	if err := json.Unmarshal(createdBytes, &created); err != nil {
		t.Fatal(err)
	}
	runPublicCLIJSON(t, "claim_withdraw", "--repo", repository, "--session", session.SessionID, "--json", "--input-json",
		`{"schema_version":"scrinium.claim-withdraw/v1","claim_id":"CLI-QRY-GONE-1","reason":"fixture withdrawal","expected_revision":"`+string(created.Revision)+`"}`)
	return repository
}

func claimListIDs(t *testing.T, data []byte) []string {
	t.Helper()
	var list publicapi.ClaimListResult
	if err := json.Unmarshal(data, &list); err != nil {
		t.Fatalf("claim_list result is not %s: %v: %s", publicapi.ClaimListSchema, err, data)
	}
	ids := make([]string, 0, len(list.Claims))
	for _, claim := range list.Claims {
		ids = append(ids, claim.Claim.ID)
	}
	return ids
}

// Each filter must REACH the app through the CLI: the discriminating
// result, not the absence of an error, is the proof.
func TestCLIClaimListFiltersReachTheApp(t *testing.T) {
	repository := prepareClaimListCLIRepository(t)
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{name: "lifecycle", query: `{"schema_version":"scrinium.claim-query/v1","lifecycle":"withdrawn"}`, want: []string{"CLI-QRY-GONE-1"}},
		{name: "locator_prefix", query: `{"schema_version":"scrinium.claim-query/v1","locator_prefix":"symbol:example.com/project/"}`, want: []string{"CLI-QRY-SYMBOL-1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := claimListIDs(t, runPublicCLIJSON(t, "claim_list", "--repo", repository, "--json", "--input-json", test.query))
			if len(got) != len(test.want) || got[0] != test.want[0] {
				t.Fatalf("filtered claim_list = %v, want %v", got, test.want)
			}
		})
	}
}

// No flags must still list everything, in stable claim-ID order.
func TestCLIClaimListNoFlagsListsAll(t *testing.T) {
	repository := prepareClaimListCLIRepository(t)
	got := claimListIDs(t, runPublicCLIJSON(t, "claim_list", "--repo", repository, "--json"))
	want := []string{"CLI-QRY-GONE-1", "CLI-QRY-SYMBOL-1"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("claim_list = %v, want %v", got, want)
	}
}

// An unknown filter value refuses with the same named message the MCP
// path gives, as a machine-parseable error document.
func TestCLIClaimListUnknownValueRefused(t *testing.T) {
	repository := prepareClaimListCLIRepository(t)
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"claim_list", "--repo", repository, "--json", "--input-json",
		`{"schema_version":"scrinium.claim-query/v1","lifecycle":"zombie"}`}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("unknown lifecycle value must refuse; stdout=%s", stdout.String())
	}
	var publicErr publicapi.PublicError
	if err := json.Unmarshal(stdout.Bytes(), &publicErr); err != nil {
		t.Fatalf("machine error is not JSON: %v: %s", err, stdout.String())
	}
	if !strings.Contains(publicErr.Message, "unknown lifecycle filter") {
		t.Fatalf("refusal message = %q, want the MCP path's named message", publicErr.Message)
	}
}
