package knowledge

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAuthorshipKindsListsEveryAcceptedKind(t *testing.T) {
	want := []AuthorshipKind{AuthorshipOwner, AuthorshipHuman, AuthorshipAgent, AuthorshipImport, AuthorshipTool}
	if got := AuthorshipKinds(); !reflect.DeepEqual(got, want) {
		t.Fatalf("AuthorshipKinds() = %q, want %q", got, want)
	}
	if AuthorshipTool != "tool" {
		t.Fatalf("AuthorshipTool = %q, want %q", AuthorshipTool, "tool")
	}
}

func TestAuthorshipKindValidation(t *testing.T) {
	tests := []struct {
		name     string
		kind     AuthorshipKind
		origin   string
		wantCode string
		wantText string
	}{
		{name: "owner", kind: AuthorshipOwner, origin: "owner:project"},
		{name: "human", kind: AuthorshipHuman, origin: "reviewer"},
		{name: "agent", kind: AuthorshipAgent, origin: "agent:session"},
		{name: "import", kind: AuthorshipImport, origin: "legacy-wiki"},
		{name: "tool", kind: AuthorshipTool, origin: "nuntius"},
		{name: "capitalized near miss", kind: "Tool", origin: "nuntius", wantCode: "invalid_enum", wantText: `invalid authorship kind "Tool"`},
		{name: "plural near miss", kind: "tools", origin: "nuntius", wantCode: "invalid_enum", wantText: `invalid authorship kind "tools"`},
		{name: "padded near miss", kind: " tool", origin: "nuntius", wantCode: "invalid_enum", wantText: `invalid authorship kind " tool"`},
		{name: "empty kind", kind: "", origin: "nuntius", wantCode: "invalid_enum", wantText: `invalid authorship kind ""`},
		{name: "tool without origin", kind: AuthorshipTool, origin: "", wantCode: "invalid_authorship", wantText: "origin and recorded_at are required"},
		{name: "tool with blank origin", kind: AuthorshipTool, origin: " \t", wantCode: "invalid_authorship", wantText: "origin and recorded_at are required"},
		{name: "agent without origin", kind: AuthorshipAgent, origin: "", wantCode: "invalid_authorship", wantText: "origin and recorded_at are required"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := validClaim()
			claim.Authorship = Authorship{Kind: test.kind, Origin: test.origin, RecordedAt: testTime}
			err := claim.Validate()
			if test.wantCode == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v, want accepted", err)
				}
				return
			}
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) || validationErr.Code != test.wantCode || !strings.Contains(err.Error(), test.wantText) {
				t.Fatalf("Validate() error = %v, want %s containing %q", err, test.wantCode, test.wantText)
			}
		})
	}
}

// RULE: AUTHORSHIP-NEUTRAL-1
func TestAuthorshipNeverChangesDerivedState(t *testing.T) {
	scenarios := []struct {
		name   string
		mutate func(*Claim)
	}{
		{name: "no evidence", mutate: func(*Claim) {}},
		{name: "documentary evidence", mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
		}},
		{name: "passing verification", mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			claim.ValidationResults = []ValidationResult{validationResult("RES-AUTH-PASS", OutcomePass, AssuranceVerification, testTime)}
		}},
		{name: "failing verification", mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.ValidationPolicy = verificationPolicy("EVD-AUTH-ADR-14")
			claim.ValidationResults = []ValidationResult{validationResult("RES-AUTH-FAIL", OutcomeFail, AssuranceVerification, testTime)}
		}},
		{name: "superseded", mutate: func(claim *Claim) {
			claim.Evidence = []Evidence{decisionEvidence()}
			claim.Lifecycle = Lifecycle{State: LifecycleSuperseded, SupersededBy: "AUTH-ADMIN-LOCAL-2"}
		}},
	}
	now := testTime.Add(time.Minute)
	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			derive := func(kind AuthorshipKind) DerivedState {
				claim := validClaim()
				scenario.mutate(&claim)
				claim.Authorship = Authorship{Kind: kind, Origin: "origin-of-" + string(kind), RecordedAt: testTime}
				state, err := DeriveState(claim, now)
				if err != nil {
					t.Fatalf("DeriveState(%s) error = %v", kind, err)
				}
				return state
			}
			want := derive(AuthorshipAgent)
			for _, kind := range AuthorshipKinds() {
				if got := derive(kind); !reflect.DeepEqual(got, want) {
					t.Fatalf("%s-authored state = %#v, agent-authored state = %#v", kind, got, want)
				}
			}
		})
	}
}
