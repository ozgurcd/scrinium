package app

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"scrinium/internal/knowledge"
)

const appTestFingerprint = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func prepareClaimService(t *testing.T) (*Service, context.Context, string) {
	t.Helper()
	service := newTestService(t, nil)
	ctx := context.Background()
	if _, err := service.SetupWiki(ctx); err != nil {
		t.Fatal(err)
	}
	status, err := service.BeginSession(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		if _, err := service.ReadPage(ctx, PageRequest{Path: path, SessionID: status.SessionID}); err != nil {
			t.Fatal(err)
		}
	}
	return service, ctx, status.SessionID
}

func createApplicationClaim(t *testing.T, service *Service, ctx context.Context, sessionID, id string) ClaimView {
	t.Helper()
	view, err := service.CreateClaim(ctx, CreateClaimRequest{
		SessionID: sessionID, ID: id, Subject: "authentication", Statement: "Administrators retain local authentication.",
		AuthorshipKind: knowledge.AuthorshipOwner, AuthorOrigin: "owner:project",
	})
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func appEvidence(at time.Time) knowledge.Evidence {
	checked := at
	return knowledge.Evidence{
		ID: "EVD-AUTH-ADR-14", Kind: knowledge.EvidenceDecision, Polarity: knowledge.PolaritySupports,
		Origin:  knowledge.EvidenceOrigin{Kind: knowledge.OriginRepository, Reference: "adr:ADR-14"},
		Locator: "adr:ADR-14", Scope: "authentication policy", Availability: knowledge.AvailabilityAvailable,
		CapturedAt: at, CheckedAt: &checked, DerivedFrom: []string{},
	}
}

func TestApplicationClaimLifecycleAndListing(t *testing.T) {
	service, ctx, sessionID := prepareClaimService(t)
	first := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-1")
	second := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-2")

	statement := "Administrators retain local authentication at all times."
	if _, err := service.UpdateClaim(ctx, UpdateClaimRequest{
		SessionID: sessionID, ID: first.Claim.ID, Statement: &statement, ExpectedRevision: first.Revision,
	}); err == nil {
		t.Fatal("material-meaning acknowledgement must be required")
	}
	updated, err := service.UpdateClaim(ctx, UpdateClaimRequest{
		SessionID: sessionID, ID: first.Claim.ID, Statement: &statement, MeaningUnchanged: true, ExpectedRevision: first.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	superseded, err := service.SupersedeClaim(ctx, SupersedeClaimRequest{
		SessionID: sessionID, ClaimID: updated.Claim.ID, SuccessorID: second.Claim.ID, ExpectedRevision: updated.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if superseded.Claim.Lifecycle.State != knowledge.LifecycleSuperseded || superseded.Claim.Lifecycle.SupersededBy != second.Claim.ID {
		t.Fatalf("unexpected supersession: %#v", superseded.Claim.Lifecycle)
	}

	third := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-3")
	withdrawn, err := service.WithdrawClaim(ctx, WithdrawClaimRequest{
		SessionID: sessionID, ClaimID: third.Claim.ID, Reason: "Policy removed", ExpectedRevision: third.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if withdrawn.Claim.Lifecycle.State != knowledge.LifecycleWithdrawn {
		t.Fatalf("unexpected withdrawal: %#v", withdrawn.Claim.Lifecycle)
	}

	listed, err := service.ListClaims(ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].Claim.ID != "AUTH-ADMIN-LOCAL-1" || listed[2].Claim.ID != "AUTH-ADMIN-LOCAL-3" {
		t.Fatalf("unexpected stable claim list: %#v", listed)
	}
}

func TestApplicationEvidenceAndPolicy(t *testing.T) {
	service, ctx, sessionID := prepareClaimService(t)
	view := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-1")
	evidence := appEvidence(view.Claim.CreatedAt)
	view, err := service.AttachEvidence(ctx, AttachEvidenceRequest{
		SessionID: sessionID, ClaimID: view.Claim.ID, Evidence: evidence, ExpectedRevision: view.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.State.Assessment != knowledge.AssessmentSourced || len(view.Claim.Evidence) != 1 {
		t.Fatalf("unexpected evidence state: %#v", view)
	}
	policy := &knowledge.ValidationPolicy{Mode: "all_required", Bindings: []knowledge.ValidationBinding{{
		ID: "VAL-AUTH-ADMIN-1", ValidatorID: "test-validator", BindingVersion: "v1", Reference: "auth-admin",
		Required: true, RequiredAssurance: knowledge.AssuranceVerification, EvidenceIDs: []string{evidence.ID},
		InputFingerprint: appTestFingerprint, RepositoryRevision: "rev-1",
	}}}
	view, err = service.SetValidationPolicy(ctx, SetValidationPolicyRequest{
		SessionID: sessionID, ClaimID: view.Claim.ID, Policy: policy, ExpectedRevision: view.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view.State.Assessment != knowledge.AssessmentSourced || view.State.Freshness != knowledge.FreshnessUnknown || len(view.Claim.ValidationResults) != 0 {
		t.Fatalf("policy without a validator result should remain sourced with unknown freshness: %#v", view)
	}
}

func TestApplicationClaimWritesRequireSessionAndRejectStaleUpdate(t *testing.T) {
	service := newTestService(t, nil)
	_, err := service.CreateClaim(context.Background(), CreateClaimRequest{
		ID: "AUTH-ADMIN-LOCAL-1", Subject: "authentication", Statement: "Administrators retain local authentication.",
		AuthorshipKind: knowledge.AuthorshipOwner, AuthorOrigin: "owner:project",
	})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Kind != ErrorSession {
		t.Fatalf("expected session error, got %v", err)
	}

	service, ctx, sessionID := prepareClaimService(t)
	view := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-1")
	subject := "login"
	_, err = service.UpdateClaim(ctx, UpdateClaimRequest{
		SessionID: sessionID, ID: view.Claim.ID, Subject: &subject, MeaningUnchanged: true,
	})
	if !errors.As(err, &appErr) || appErr.Kind != ErrorInvalid {
		t.Fatalf("expected missing revision error, got %v", err)
	}
	_, err = service.UpdateClaim(ctx, UpdateClaimRequest{
		SessionID: sessionID, ID: view.Claim.ID, Subject: &subject, MeaningUnchanged: true, ExpectedRevision: ClaimRevision("sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"),
	})
	if !errors.As(err, &appErr) || appErr.Kind != ErrorConflict {
		t.Fatalf("expected stale update conflict, got %v", err)
	}
	var conflict *ClaimConflictError
	if !errors.As(err, &conflict) || conflict.CurrentRevision != view.Revision {
		t.Fatalf("expected typed conflict with current revision, got %v", err)
	}
}

func TestApplicationNoOpMutationPreservesRevision(t *testing.T) {
	service, ctx, sessionID := prepareClaimService(t)
	view := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-1")
	subject := view.Claim.Subject
	unchanged, err := service.UpdateClaim(ctx, UpdateClaimRequest{
		SessionID: sessionID, ID: view.Claim.ID, Subject: &subject, MeaningUnchanged: true, ExpectedRevision: view.Revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	if unchanged.Revision != view.Revision || !unchanged.Claim.UpdatedAt.Equal(view.Claim.UpdatedAt) {
		t.Fatalf("no-op mutation changed record identity: before=%#v after=%#v", view, unchanged)
	}
}

func TestConcurrentClaimMutationsRejectOneStaleWriter(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Service, context.Context, string, ClaimView) error
	}{
		{
			name: "supersede versus update",
			mutate: func(service *Service, ctx context.Context, sessionID string, view ClaimView) error {
				_, err := service.SupersedeClaim(ctx, SupersedeClaimRequest{SessionID: sessionID, ClaimID: view.Claim.ID, SuccessorID: "AUTH-SUCCESSOR-1", ExpectedRevision: view.Revision})
				return err
			},
		},
		{
			name: "withdraw versus update",
			mutate: func(service *Service, ctx context.Context, sessionID string, view ClaimView) error {
				_, err := service.WithdrawClaim(ctx, WithdrawClaimRequest{SessionID: sessionID, ClaimID: view.Claim.ID, Reason: "withdrawn", ExpectedRevision: view.Revision})
				return err
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, ctx, sessionID := prepareClaimService(t)
			view := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-1")
			if test.name == "supersede versus update" {
				createApplicationClaim(t, service, ctx, sessionID, "AUTH-SUCCESSOR-1")
			}
			start := make(chan struct{})
			errs := make(chan error, 2)
			go func() {
				<-start
				errs <- test.mutate(service, ctx, sessionID, view)
			}()
			go func() {
				<-start
				subject := "concurrent-update"
				_, err := service.UpdateClaim(ctx, UpdateClaimRequest{
					SessionID: sessionID, ID: view.Claim.ID, Subject: &subject, MeaningUnchanged: true, ExpectedRevision: view.Revision,
				})
				errs <- err
			}()
			close(start)
			successes, conflicts := 0, 0
			for range 2 {
				err := <-errs
				if err == nil {
					successes++
					continue
				}
				var conflict *ClaimConflictError
				if errors.As(err, &conflict) {
					conflicts++
				}
			}
			if successes != 1 || conflicts != 1 {
				t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
			}
		})
	}
}

func TestConcurrentEvidenceAttachmentsRejectOneStaleWriter(t *testing.T) {
	service, ctx, sessionID := prepareClaimService(t)
	view := createApplicationClaim(t, service, ctx, sessionID, "AUTH-ADMIN-LOCAL-1")
	start := make(chan struct{})
	errs := make(chan error, 2)
	for index := range 2 {
		go func(index int) {
			<-start
			evidence := appEvidence(view.Claim.CreatedAt)
			evidence.ID = fmt.Sprintf("EVD-CONCURRENT-%d", index+1)
			_, err := service.AttachEvidence(ctx, AttachEvidenceRequest{
				SessionID: sessionID, ClaimID: view.Claim.ID, Evidence: evidence, ExpectedRevision: view.Revision,
			})
			errs <- err
		}(index)
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		err := <-errs
		if err == nil {
			successes++
			continue
		}
		var conflict *ClaimConflictError
		if errors.As(err, &conflict) {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	stored, err := service.GetClaim(ctx, view.Claim.ID, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Claim.Evidence) != 1 || stored.Revision == view.Revision {
		t.Fatalf("concurrent evidence mutation was not isolated: %#v", stored)
	}
}
