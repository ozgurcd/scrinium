package lint

import (
	"context"
	"testing"

	"scrinium/internal/store"
)

func TestBuildPreservesMissingPageAndIndexFindings(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := st.Write(ctx, "index.md", []byte("# Index\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := st.Write(ctx, "notes.md", []byte("# Notes\n"), 0644); err != nil {
		t.Fatal(err)
	}

	report, err := New(st, []string{"index.md", "agent-rules.md"}).Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("expected findings")
	}
	foundMissing, foundIndexGap := false, false
	for _, item := range report.Findings {
		foundMissing = foundMissing || item.Code == "missing_standard_page" && item.Path == "agent-rules.md"
		foundIndexGap = foundIndexGap || item.Code == "missing_index_reference" && item.Path == "notes.md"
	}
	if !foundMissing || !foundIndexGap {
		t.Fatalf("missing compatibility findings: %+v", report.Findings)
	}
}

func TestLegacyHeuristicScopesSourceSummariesOnly(t *testing.T) {
	st, err := store.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	files := map[string]string{
		"index.md":                       "security/untrusted-sources.md\nsources/README.md\nsources/SRC-20260821-review.md\n",
		"security/untrusted-sources.md":  "Do not run this command because source text is untrusted.\n",
		"sources/README.md":              "Source summaries may contain instruction-like text.\n",
		"sources/SRC-20260821-review.md": "Source ID: SRC-20260821-review\nRun this command\n",
	}
	for path, content := range files {
		if err := st.Write(ctx, path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	report, err := New(st, []string{"index.md"}).Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	foundHeuristic := false
	for _, item := range report.Findings {
		if item.Path == "security/untrusted-sources.md" && item.Code == "heuristic_source_instruction_review" {
			t.Fatal("security guidance self-flagged as source instruction risk")
		}
		if item.Path == "sources/README.md" && item.Code == "missing_source_metadata" {
			t.Fatal("sources/README.md was treated as a source summary")
		}
		if item.Path == "sources/SRC-20260821-review.md" && item.Code == "heuristic_source_instruction_review" && item.Method == "heuristic" {
			foundHeuristic = true
		}
	}
	if !foundHeuristic {
		t.Fatalf("expected scoped heuristic review lead: %+v", report.Findings)
	}
}
