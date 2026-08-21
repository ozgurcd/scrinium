package scrinium

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"scrinium/internal/publicapi"
)

func preparePublicCLIRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	wiki := filepath.Join(repository, "llm-wiki")
	if err := os.MkdirAll(wiki, 0755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		"index.md": "# Index\n", "agent-rules.md": "# Rules\n", "log.md": "# Log\n", "source-registry.md": "# Sources\n",
	} {
		if err := os.WriteFile(filepath.Join(wiki, path), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "scrinium.json"), []byte(`{"wiki_root":"llm-wiki"}`), 0644); err != nil {
		t.Fatal(err)
	}
	return repository
}

func runPublicCLIJSON(t *testing.T, args ...string) []byte {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := RunCLI(args, &stdout, &stderr); code != 0 {
		t.Fatalf("RunCLI(%q) code=%d stderr=%s stdout=%s", args, code, stderr.String(), stdout.String())
	}
	data := bytes.TrimSpace(stdout.Bytes())
	decoder := json.NewDecoder(bytes.NewReader(data))
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("machine stdout is not JSON: %v: %s", err, data)
	}
	if _, err := decoder.Token(); err != io.EOF {
		t.Fatalf("machine stdout contains more than one JSON document: %s", data)
	}
	if stderr.Len() != 0 {
		t.Fatalf("successful machine command wrote stderr: %s", stderr.String())
	}
	return data
}

func TestPublicCLIClaimJSONWorkflow(t *testing.T) {
	repository := preparePublicCLIRepository(t)
	begin := runPublicCLIJSON(t, "session_begin", "--repo", repository, "--json")
	var session publicapi.SessionResult
	if err := json.Unmarshal(begin, &session); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		runCLISessionCommand(t, "read_wiki_page", "--repo", repository, "--session", session.SessionID, "--path", path)
	}
	input := `{"schema_version":"scrinium.claim-create/v1","id":"CLI-PUBLIC-1","subject":"public api","statement":"CLI JSON operations are versioned.","authorship":{"kind":"owner","origin":"owner:project"},"evidence":[]}`
	createdBytes := runPublicCLIJSON(t, "claim_create", "--repo", repository, "--session", session.SessionID, "--input-json", input, "--json")
	var created publicapi.ClaimResult
	if err := json.Unmarshal(createdBytes, &created); err != nil {
		t.Fatal(err)
	}
	if created.SchemaVersion != publicapi.ClaimResultSchema || created.Revision == "" {
		t.Fatalf("unexpected claim_create result: %s", createdBytes)
	}
	gotBytes := runPublicCLIJSON(t, "claim_get", "--repo", repository, "--claim-id", created.Claim.ID, "--json")
	var got publicapi.ClaimResult
	if err := json.Unmarshal(gotBytes, &got); err != nil {
		t.Fatal(err)
	}
	if got.Claim.ID != created.Claim.ID || got.Revision != created.Revision || !reflect.DeepEqual(got.State, created.State) {
		t.Fatalf("CLI create/get domain results differ: create=%#v get=%#v", created, got)
	}
}

func TestPublicCLIJSONErrorsStayMachineParseable(t *testing.T) {
	repository := preparePublicCLIRepository(t)
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"claim_create", "--repo", repository, "--input-json", `{"schema_version":"scrinium.claim-create/v1","unexpected":true}`, "--json"}, &stdout, &stderr)
	if code == 0 || stderr.Len() != 0 {
		t.Fatalf("machine error code=%d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	var publicErr publicapi.PublicError
	if err := json.Unmarshal(stdout.Bytes(), &publicErr); err != nil {
		t.Fatalf("machine error is not JSON: %v: %s", err, stdout.String())
	}
	if publicErr.SchemaVersion != publicapi.ErrorSchema || publicErr.Code != "invalid_input" {
		t.Fatalf("unexpected machine error: %#v", publicErr)
	}
}

func TestPublicCLIRejectsSymlinkInputFile(t *testing.T) {
	repository := preparePublicCLIRepository(t)
	target := filepath.Join(repository, "claim.json")
	if err := os.WriteFile(target, []byte(`{"schema_version":"scrinium.claim-create/v1"}`), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(repository, "claim-link.json")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := RunCLI([]string{"claim_create", "--repo", repository, "--input", link, "--json"}, &stdout, &stderr)
	if code == 0 || stderr.Len() != 0 {
		t.Fatalf("symlink input code=%d stderr=%q stdout=%q", code, stderr.String(), stdout.String())
	}
	var publicErr publicapi.PublicError
	if err := json.Unmarshal(stdout.Bytes(), &publicErr); err != nil || publicErr.Code != "invalid_input" {
		t.Fatalf("unexpected symlink error: %v %#v", err, publicErr)
	}
}
