package app

import (
	"context"
	"testing"
)

func TestMigrationAssessmentIsTaggedOnlyAndDryRun(t *testing.T) {
	service := newTestService(t, nil)
	content := `# Authentication

Ambiguous prose must not become a claim.

` + "```scrinium-claim-candidate" + `
{"id":"AUTH-ADMIN-LOCAL-1","subject":"authentication","statement":"Administrators retain local authentication."}
` + "```" + `
`
	if err := service.store.Write(context.Background(), "authentication.md", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	report, err := service.AssessClaimMigration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || len(report.Candidates) != 1 || report.Candidates[0].ID != "AUTH-ADMIN-LOCAL-1" {
		t.Fatalf("unexpected candidate report: %#v", report)
	}
	if len(report.Debt) == 0 || report.Debt[0].Code != "untagged_markdown" {
		t.Fatalf("ambiguous prose was not reported as debt: %#v", report.Debt)
	}
	claims, err := service.claims.List(context.Background())
	if err != nil || len(claims) != 0 {
		t.Fatalf("dry run wrote canonical claims: %#v, %v", claims, err)
	}
}

func TestMigrationAssessmentReportsMalformedTaggedBlock(t *testing.T) {
	service := newTestService(t, nil)
	content := "```scrinium-claim-candidate\n{\"id\":\"bad\"}\n```\n"
	if err := service.store.Write(context.Background(), "bad.md", []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	report, err := service.AssessClaimMigration(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, debt := range report.Debt {
		if debt.Path == "bad.md" && debt.Code == "malformed_claim_candidate" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected malformed tagged candidate debt, got %#v", report.Debt)
	}
}
