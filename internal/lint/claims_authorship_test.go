package lint

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/store"
)

func TestClaimLintAcceptsToolAuthorshipAndNamesUnknownKinds(t *testing.T) {
	_, claims, linter := newClaimLinter(t)
	ctx := context.Background()
	claim := lintClaim("CLAIM-TOOL-1")
	claim.Authorship = knowledge.Authorship{Kind: knowledge.AuthorshipTool, Origin: "nuntius", RecordedAt: lintClaimTime}
	if _, err := claims.Create(ctx, claim); err != nil {
		t.Fatal(err)
	}
	report, err := linter.Build(ctx, lintClaimTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("tool-authored claim produced lint findings: %#v", report.Findings)
	}

	data, err := store.EncodeClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	nearMiss := bytes.Replace(bytes.Replace(data, []byte(`"kind": "tool"`), []byte(`"kind": "tools"`), 1), []byte("CLAIM-TOOL-1"), []byte("CLAIM-TOOL-2"), 1)
	if bytes.Equal(nearMiss, data) || !bytes.Contains(nearMiss, []byte(`"kind": "tools"`)) {
		t.Fatalf("fixture did not rewrite the authorship kind: %s", nearMiss)
	}
	if err := os.WriteFile(filepath.Join(linter.repository.Root(), "llm-wiki", "claims", "CLAIM-TOOL-2.json"), nearMiss, 0644); err != nil {
		t.Fatal(err)
	}
	report, err = linter.Build(ctx, lintClaimTime.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	named := false
	for _, finding := range report.Findings {
		named = named || strings.Contains(finding.Evidence, `invalid authorship kind "tools"`)
	}
	if !named {
		t.Fatalf("lint did not name the unknown authorship kind: %#v", report.Findings)
	}
}
