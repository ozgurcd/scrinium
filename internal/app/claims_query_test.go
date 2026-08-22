package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"scrinium/internal/knowledge"
)

// prepareQueryFixture builds three distinguishable claims:
//   - QRY-RULEFLOOR-1: rulefloor binding on rule ROUTE-RATELIMIT-1,
//     target fleet-a, repo: evidence locator — lifecycle active.
//   - QRY-GOGRAPH-1: gograph binding on a symbol, target fleet-b,
//     symbol: evidence locator — lifecycle active.
//   - QRY-BARE-1: no policy, no evidence — withdrawn.
func prepareQueryFixture(t *testing.T) (*Service, context.Context, []string) {
	t.Helper()
	service, ctx, sessionID := prepareClaimService(t)
	now := time.Now().UTC()

	evidence := func(id, kind, locator string) knowledge.Evidence {
		return knowledge.Evidence{
			ID: id, Kind: knowledge.EvidenceKind(kind), Polarity: knowledge.PolaritySupports,
			Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "fixture"},
			Locator: locator, Scope: "query fixture", Availability: knowledge.AvailabilityAvailable,
			CapturedAt: now, DerivedFrom: []string{},
		}
	}
	binding := func(id, validator, reference, target string) knowledge.ValidationBinding {
		parameters := map[string]string{"mode": "static"}
		if validator == "gograph" {
			parameters = map[string]string{"predicate": "symbol_exists", "required_precision": "ast"}
		}
		if target != "" {
			parameters["target"] = target
		}
		return knowledge.ValidationBinding{
			ID: id, ValidatorID: validator, BindingVersion: validator + ".binding.v1",
			Reference: reference, Parameters: parameters, Required: true,
			RequiredAssurance: knowledge.AssuranceObservation, EvidenceIDs: []string{},
			InputFingerprint:   "sha256:" + strings.Repeat("a", 64),
			RepositoryRevision: "fixture",
		}
	}

	first, err := service.CreateClaim(ctx, CreateClaimRequest{
		SessionID: sessionID, ID: "QRY-RULEFLOOR-1", Subject: "fleet rule", Statement: "Rule holds.",
		AuthorshipKind: knowledge.AuthorshipOwner, AuthorOrigin: "owner:test",
		Evidence: []knowledge.Evidence{evidence("EV-1", "repository_reference", "repo:contracts/rules.md")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetValidationPolicy(ctx, SetValidationPolicyRequest{
		SessionID: sessionID, ClaimID: first.Claim.ID, ExpectedRevision: first.Revision,
		Policy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{
			binding("BIND-RF-1", "rulefloor", "ROUTE-RATELIMIT-1", "fleet-a"),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	second, err := service.CreateClaim(ctx, CreateClaimRequest{
		SessionID: sessionID, ID: "QRY-GOGRAPH-1", Subject: "fleet symbol", Statement: "Symbol exists.",
		AuthorshipKind: knowledge.AuthorshipOwner, AuthorOrigin: "owner:test",
		Evidence: []knowledge.Evidence{evidence("EV-1", "symbol_reference", "symbol:example.com/project/internal/api::NewOSSEngine")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetValidationPolicy(ctx, SetValidationPolicyRequest{
		SessionID: sessionID, ClaimID: second.Claim.ID, ExpectedRevision: second.Revision,
		Policy: &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{
			binding("BIND-GG-1", "gograph", "EXAMPLE-SYMBOL-1", "fleet-b"),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	third := createApplicationClaim(t, service, ctx, sessionID, "QRY-BARE-1")
	if _, err := service.WithdrawClaim(ctx, WithdrawClaimRequest{
		SessionID: sessionID, ClaimID: third.Claim.ID, Reason: "fixture withdrawal", ExpectedRevision: third.Revision,
	}); err != nil {
		t.Fatal(err)
	}

	return service, ctx, []string{"QRY-BARE-1", "QRY-GOGRAPH-1", "QRY-RULEFLOOR-1"}
}

func queryIDs(t *testing.T, service *Service, ctx context.Context, query ClaimQuery) []string {
	t.Helper()
	views, err := service.QueryClaims(ctx, time.Now(), query)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(views))
	for _, view := range views {
		ids = append(ids, view.Claim.ID)
	}
	return ids
}

func expectIDs(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// COMPATIBILITY: the zero query must return exactly what ListClaims
// returns, in the same stable claim-ID order.
func TestClaimQueryNoFilterMatchesListExactly(t *testing.T) {
	service, ctx, all := prepareQueryFixture(t)
	listed, err := service.ListClaims(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	queried := queryIDs(t, service, ctx, ClaimQuery{})
	expectIDs(t, queried, all...)
	if len(listed) != len(queried) {
		t.Fatalf("zero query returned %d claims, ListClaims %d", len(queried), len(listed))
	}
	for i := range listed {
		if listed[i].Claim.ID != queried[i] {
			t.Fatalf("order diverged at %d: %s vs %s", i, listed[i].Claim.ID, queried[i])
		}
	}
}

// Every filter must EXCLUDE as well as include.
func TestClaimQueryFiltersExcludeAndInclude(t *testing.T) {
	service, ctx, _ := prepareQueryFixture(t)
	tests := []struct {
		name  string
		query ClaimQuery
		want  []string
	}{
		{name: "validator_id", query: ClaimQuery{ValidatorID: "rulefloor"}, want: []string{"QRY-RULEFLOOR-1"}},
		{name: "binding_reference", query: ClaimQuery{BindingReference: "EXAMPLE-SYMBOL-1"}, want: []string{"QRY-GOGRAPH-1"}},
		{name: "target", query: ClaimQuery{Target: "fleet-a"}, want: []string{"QRY-RULEFLOOR-1"}},
		{name: "lifecycle", query: ClaimQuery{Lifecycle: "withdrawn"}, want: []string{"QRY-BARE-1"}},
		{name: "locator_prefix_repo", query: ClaimQuery{LocatorPrefix: "repo:contracts/"}, want: []string{"QRY-RULEFLOOR-1"}},
		{name: "locator_prefix_symbol", query: ClaimQuery{LocatorPrefix: "symbol:example.com/project/"}, want: []string{"QRY-GOGRAPH-1"}},
		{name: "assessment_excludes_bare", query: ClaimQuery{Assessment: "sourced"}, want: []string{"QRY-GOGRAPH-1", "QRY-RULEFLOOR-1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expectIDs(t, queryIDs(t, service, ctx, test.query), test.want...)
		})
	}
}

// AND-composition: one binding must satisfy every binding-level filter
// together; a cross-binding mix matches nothing.
func TestClaimQueryAndComposition(t *testing.T) {
	service, ctx, _ := prepareQueryFixture(t)
	expectIDs(t, queryIDs(t, service, ctx, ClaimQuery{ValidatorID: "rulefloor", Target: "fleet-a", Lifecycle: "active"}), "QRY-RULEFLOOR-1")
	expectIDs(t, queryIDs(t, service, ctx, ClaimQuery{ValidatorID: "rulefloor", Target: "fleet-b"}))
	expectIDs(t, queryIDs(t, service, ctx, ClaimQuery{ValidatorID: "gograph", LocatorPrefix: "repo:"}))
}

// An unknown filter VALUE is refused, never silently ignored.
func TestClaimQueryUnknownValueRefused(t *testing.T) {
	service, ctx, _ := prepareQueryFixture(t)
	for _, test := range []struct {
		query   ClaimQuery
		message string
	}{
		{ClaimQuery{Lifecycle: "zombie"}, "unknown lifecycle filter"},
		{ClaimQuery{Assessment: "vibes"}, "unknown assessment filter"},
		{ClaimQuery{Freshness: "fresh"}, "unknown freshness filter"},
	} {
		_, err := service.QueryClaims(ctx, time.Now(), test.query)
		if err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("query %+v: err %v, want %q", test.query, err, test.message)
		}
	}
}

// The symbol evidence kind refuses path-shaped or ambiguous locators.
func TestSymbolEvidenceLocatorValidation(t *testing.T) {
	service, ctx, sessionID := prepareClaimService(t)
	now := time.Now().UTC()
	make := func(locator string) error {
		_, err := service.CreateClaim(ctx, CreateClaimRequest{
			SessionID: sessionID, ID: "QRY-SYM-PROBE-1", Subject: "symbol", Statement: "Symbol claim.",
			AuthorshipKind: knowledge.AuthorshipOwner, AuthorOrigin: "owner:test",
			Evidence: []knowledge.Evidence{{
				ID: "EV-1", Kind: knowledge.EvidenceSymbolReference, Polarity: knowledge.PolaritySupports,
				Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "fixture"},
				Locator: locator, Scope: "probe", Availability: knowledge.AvailabilityAvailable,
				CapturedAt: now, DerivedFrom: []string{},
			}},
		})
		return err
	}
	for _, bad := range []string{
		"internal/api/router.go",
		"symbol:internal/api/router.go",
		"symbol:NewOSSEngine",
		"symbol:../up::Name",
		"symbol:example.com/x::a::b",
	} {
		err := make(bad)
		if err == nil || !strings.Contains(err.Error(), "package-qualified symbol identity") {
			t.Fatalf("locator %q: err %v, want the named refusal", bad, err)
		}
	}
	if err := make("symbol:example.com/project/internal/api::NewOSSEngine"); err != nil {
		t.Fatalf("valid symbol locator refused: %v", err)
	}
}
