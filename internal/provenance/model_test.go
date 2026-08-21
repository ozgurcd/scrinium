package provenance

import (
	"strings"
	"testing"
	"time"
)

func validRecord() SourceRecord {
	received := Date("2026-08-20")
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	return SourceRecord{
		SchemaVersion: SchemaVersion, ID: "SRC-20260820-project-design", Title: "Project design",
		SourceType: SourceTypeProjectDocument, Origin: Origin{Kind: OriginOwner, Trust: TrustOwner},
		RawPath: "raw/inbox/design.md", RawFingerprint: "sha256:" + strings.Repeat("a", 64),
		ReceivedDate: &received, IngestDate: "2026-08-21", Status: StatusCurrent,
		DerivedClaims: []string{"AUTH-LOCAL-1"}, DerivedPages: []string{"projects/scrinium.md"},
		ProvenanceNotes: []string{"Owner-provided design input."}, CreatedAt: now, UpdatedAt: now,
	}
}

func TestSourceRecordValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*SourceRecord)
	}{
		{name: "invalid id", mutate: func(record *SourceRecord) { record.ID = "../source" }},
		{name: "invalid source type", mutate: func(record *SourceRecord) { record.SourceType = "web" }},
		{name: "invalid origin", mutate: func(record *SourceRecord) { record.Origin.Kind = "friend" }},
		{name: "invalid trust", mutate: func(record *SourceRecord) { record.Origin.Trust = "high" }},
		{name: "invalid date", mutate: func(record *SourceRecord) { record.IngestDate = "yesterday" }},
		{name: "unsafe raw path", mutate: func(record *SourceRecord) { record.RawPath = "../secret" }},
		{name: "invalid fingerprint", mutate: func(record *SourceRecord) { record.RawFingerprint = "sha256:short" }},
		{name: "invalid lifecycle", mutate: func(record *SourceRecord) { record.Status = StatusSuperseded }},
		{name: "immutable arrays required", mutate: func(record *SourceRecord) { record.DerivedPages = nil }},
		{name: "invalid timestamp", mutate: func(record *SourceRecord) { record.UpdatedAt = time.Time{} }},
	}
	if err := validRecord().Validate(); err != nil {
		t.Fatalf("valid record rejected: %v", err)
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := validRecord()
			test.mutate(&record)
			if err := record.Validate(); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}
}

func TestSourceIDAndLocatorValidation(t *testing.T) {
	for _, id := range []string{"SRC-20260820-project", "SRC-20260229-invalid", "SRC-20260820-Upper", "SRC-20260820-a/b"} {
		valid := ValidSourceID(id)
		if id == "SRC-20260820-project" && !valid {
			t.Fatalf("valid ID rejected: %s", id)
		}
		if id != "SRC-20260820-project" && valid {
			t.Fatalf("invalid ID accepted: %s", id)
		}
	}
	id, referenced, err := SourceIDFromLocator("source:SRC-20260820-project")
	if err != nil || !referenced || id != "SRC-20260820-project" {
		t.Fatalf("canonical locator did not resolve: %q %t %v", id, referenced, err)
	}
	if _, referenced, err := SourceIDFromLocator("source:../../bad"); err == nil || !referenced {
		t.Fatal("invalid source locator was not rejected")
	}
	if _, referenced, err := SourceIDFromLocator("repo:README.md"); err != nil || referenced {
		t.Fatal("non-source locator should be ignored")
	}
}
