package publicapi

import (
	"strings"
	"testing"

	"scrinium/internal/app"
)

func TestDecodeJSONIsStrictAndVersioned(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "unknown field", data: `{"schema_version":"scrinium.claim-create/v1","id":"AUTH-1","subject":"authentication","statement":"admins use local authentication","authorship":{"kind":"human","origin":"owner"},"evidence":[],"extra":true}`, want: "unknown field"},
		{name: "duplicate key", data: `{"schema_version":"scrinium.claim-create/v1","id":"AUTH-1","id":"AUTH-2","subject":"authentication","statement":"admins use local authentication","authorship":{"kind":"human","origin":"owner"},"evidence":[]}`, want: "duplicate object key"},
		{name: "unsupported schema", data: `{"schema_version":"scrinium.claim-create/v2","id":"AUTH-1","subject":"authentication","statement":"admins use local authentication","authorship":{"kind":"human","origin":"owner"},"evidence":[]}`, want: "unsupported schema_version"},
		{name: "multiple values", data: `{"schema_version":"scrinium.claim-create/v1","id":"AUTH-1","subject":"authentication","statement":"admins use local authentication","authorship":{"kind":"human","origin":"owner"},"evidence":[]} {}`, want: "multiple JSON values"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var input ClaimCreateInput
			err := DecodeJSON([]byte(test.data), ClaimCreateSchema, &input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("DecodeJSON() error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestDecodeJSONRejectsOversizedInput(t *testing.T) {
	var input ClaimCreateInput
	err := DecodeJSON(make([]byte, MaxInputBytes+1), ClaimCreateSchema, &input)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("DecodeJSON() error = %v, want size limit", err)
	}
}

func TestConflictErrorExposesRevisions(t *testing.T) {
	doc := ErrorFrom("claim_update", &app.ClaimConflictError{
		ClaimID:          "AUTH-1",
		ExpectedRevision: app.ClaimRevision("old"),
		CurrentRevision:  app.ClaimRevision("current"),
	})
	if doc.Code != "conflict" || doc.ClaimID != "AUTH-1" || doc.ExpectedRevision != "old" || doc.CurrentRevision != "current" || !doc.Retryable {
		t.Fatalf("unexpected conflict document: %#v", doc)
	}
}

func TestValidateMachineDocumentRequiresVersionedJSON(t *testing.T) {
	for _, data := range []string{`not json`, `{}`, `{"schema_version":""}`} {
		if err := ValidateMachineDocument([]byte(data)); err == nil {
			t.Fatalf("ValidateMachineDocument(%q) succeeded", data)
		}
	}
	if err := ValidateMachineDocument([]byte(`{"schema_version":"scrinium.claim-result/v1"}`)); err != nil {
		t.Fatal(err)
	}
}
