package scrinium

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirectCLIRequiresExplicitSession(t *testing.T) {
	tools := []string{
		"continue_session", "session_status", "finish_session", "abandon_session",
		"register_source", "create_page", "move_page", "archive_page",
		"update_wiki_page", "create_draft", "append_log",
	}
	for _, tool := range tools {
		t.Run(tool, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := RunCLI([]string{tool}, &stdout, &stderr); code != 1 {
				t.Fatalf("expected exit code 1, got %d", code)
			}
			if !strings.Contains(stderr.String(), "requires --session SESSION-ID") {
				t.Fatalf("expected explicit session requirement, got %q", stderr.String())
			}
		})
	}
}

func TestDirectCLISessionPersistsAcrossInvocations(t *testing.T) {
	repository := t.TempDir()
	wiki := filepath.Join(repository, "llm-wiki")
	if err := os.MkdirAll(wiki, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"index.md": "# Index\n", "agent-rules.md": "# Rules\n"} {
		if err := os.WriteFile(filepath.Join(wiki, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "scrinium.json"), []byte(`{"wiki_root":"llm-wiki"}`), 0644); err != nil {
		t.Fatal(err)
	}

	begin := runCLISessionCommand(t, "begin_session", "--repo", repository)
	var started map[string]any
	if err := json.Unmarshal([]byte(begin), &started); err != nil {
		t.Fatalf("decode begin result: %v: %s", err, begin)
	}
	sessionID, _ := started["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("begin did not return session ID: %s", begin)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		runCLISessionCommand(t, "read_wiki_page", "--repo", repository, "--session", sessionID, "--path", path)
	}
	statusText := runCLISessionCommand(t, "session_status", "--repo", repository, "--session", sessionID)
	var status map[string]any
	if err := json.Unmarshal([]byte(statusText), &status); err != nil {
		t.Fatal(err)
	}
	if reads, ok := status["pages_read"].([]any); !ok || len(reads) != 2 {
		t.Fatalf("cross-invocation reads were not retained: %s", statusText)
	}
	finished := runCLISessionCommand(t, "finish_session", "--repo", repository, "--session", sessionID)
	if !strings.Contains(finished, `"status": "finished"`) {
		t.Fatalf("session did not finish across invocations: %s", finished)
	}
}

func TestDirectCLISessionPersistsAcrossProcesses(t *testing.T) {
	repository := prepareCLIRepository(t)
	begin := runCLIProcessSessionCommand(t, "begin_session", "--repo", repository)
	var started map[string]any
	if err := json.Unmarshal([]byte(begin), &started); err != nil {
		t.Fatal(err)
	}
	sessionID, _ := started["session_id"].(string)
	if sessionID == "" {
		t.Fatalf("begin did not return session ID: %s", begin)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		runCLIProcessSessionCommand(t, "read_wiki_page", "--repo", repository, "--session", sessionID, "--path", path)
	}
	status := runCLIProcessSessionCommand(t, "session_status", "--repo", repository, "--session", sessionID)
	if !strings.Contains(status, "agent-rules.md") || !strings.Contains(status, "index.md") {
		t.Fatalf("separate CLI processes lost read tracking: %s", status)
	}
	finished := runCLIProcessSessionCommand(t, "finish_session", "--repo", repository, "--session", sessionID)
	if !strings.Contains(finished, `"status": "finished"`) {
		t.Fatalf("separate CLI process could not finish: %s", finished)
	}
}

func TestCLIProcessHelper(t *testing.T) {
	if os.Getenv("SCRINIUM_CLI_PROCESS_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		t.Fatal("missing helper arguments")
	}
	os.Exit(RunCLI(os.Args[separator+1:], os.Stdout, os.Stderr))
}

func runCLISessionCommand(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := RunCLI(args, &stdout, &stderr); code != 0 {
		t.Fatalf("RunCLI(%q) code=%d stderr=%s stdout=%s", args, code, stderr.String(), stdout.String())
	}
	return stdout.String()
}

func runCLIProcessSessionCommand(t *testing.T, args ...string) string {
	t.Helper()
	commandArgs := append([]string{"-test.run=TestCLIProcessHelper", "--"}, args...)
	cmd := exec.Command(os.Args[0], commandArgs...)
	cmd.Env = append(os.Environ(), "SCRINIUM_CLI_PROCESS_HELPER=1")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("CLI process %q failed: %v: %s", args, err, output)
	}
	return string(output)
}

func prepareCLIRepository(t *testing.T) string {
	t.Helper()
	repository := t.TempDir()
	wiki := filepath.Join(repository, "llm-wiki")
	if err := os.MkdirAll(wiki, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"index.md": "# Index\n", "agent-rules.md": "# Rules\n"} {
		if err := os.WriteFile(filepath.Join(wiki, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(repository, "scrinium.json"), []byte(`{"wiki_root":"llm-wiki"}`), 0644); err != nil {
		t.Fatal(err)
	}
	return repository
}
