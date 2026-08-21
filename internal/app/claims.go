package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"scrinium/internal/knowledge"
	"scrinium/internal/provenance"
	"scrinium/internal/session"
	"scrinium/internal/store"
)

type CreateClaimRequest struct {
	SessionID        string
	ID               string
	Subject          string
	Statement        string
	AuthorshipKind   knowledge.AuthorshipKind
	AuthorOrigin     string
	Evidence         []knowledge.Evidence
	ValidationPolicy *knowledge.ValidationPolicy
}

type UpdateClaimRequest struct {
	SessionID        string
	ID               string
	Subject          *string
	Statement        *string
	MeaningUnchanged bool
	ExpectedRevision ClaimRevision
}

type AttachEvidenceRequest struct {
	SessionID        string
	ClaimID          string
	Evidence         knowledge.Evidence
	ExpectedRevision ClaimRevision
}

type SetValidationPolicyRequest struct {
	SessionID        string
	ClaimID          string
	Policy           *knowledge.ValidationPolicy
	ExpectedRevision ClaimRevision
}

type SupersedeClaimRequest struct {
	SessionID        string
	ClaimID          string
	SuccessorID      string
	ExpectedRevision ClaimRevision
}

type WithdrawClaimRequest struct {
	SessionID        string
	ClaimID          string
	Reason           string
	ExpectedRevision ClaimRevision
}

type claimUpdate struct {
	sessionID  string
	id         string
	expected   ClaimRevision
	activeOnly bool
	mutate     func(*knowledge.Claim) (bool, error)
}

// ClaimRevision is an opaque token for the exact canonical claim bytes read.
type ClaimRevision string

// ClaimConflictError reports an optimistic-concurrency conflict with current state.
type ClaimConflictError struct {
	ClaimID          string
	ExpectedRevision ClaimRevision
	CurrentRevision  ClaimRevision
	Cause            error
}

func (e *ClaimConflictError) Error() string {
	return fmt.Sprintf("claim %s changed since it was read", e.ClaimID)
}

func (e *ClaimConflictError) Unwrap() error {
	return &Error{Kind: ErrorConflict, Message: e.Error(), Cause: e.Cause}
}

type ClaimView struct {
	Claim    knowledge.Claim        `json:"claim"`
	State    knowledge.DerivedState `json:"state"`
	Revision ClaimRevision          `json:"revision"`
}

// CreateClaim creates one active claim. Derived state is never accepted.
func (s *Service) CreateClaim(ctx context.Context, req CreateClaimRequest) (ClaimView, error) {
	if err := s.requireClaimWrite(req.ID); err != nil {
		return ClaimView{}, err
	}
	now := time.Now().UTC()
	evidence := req.Evidence
	if evidence == nil {
		evidence = []knowledge.Evidence{}
	}
	evidence, err := s.prepareSourceEvidence(ctx, evidence, now)
	if err != nil {
		return ClaimView{}, err
	}
	claim := knowledge.Claim{
		SchemaVersion: knowledge.SchemaVersion,
		ID:            req.ID, Subject: req.Subject, Statement: req.Statement,
		Lifecycle:  knowledge.Lifecycle{State: knowledge.LifecycleActive},
		Authorship: knowledge.Authorship{Kind: req.AuthorshipKind, Origin: req.AuthorOrigin, RecordedAt: now},
		Evidence:   evidence, ValidationPolicy: req.ValidationPolicy,
		ValidationResults: []knowledge.ValidationResult{}, CreatedAt: now, UpdatedAt: now,
	}
	var record store.ClaimRecord
	write := session.Write{Path: claimPath(req.ID), ExistedBefore: false, Claim: true}
	if err := s.sessions.DoWrite(ctx, req.SessionID, []session.Write{write}, func() (bool, error) {
		var createErr error
		record, createErr = s.claims.Create(ctx, claim)
		if createErr != nil {
			return false, translateClaimError(createErr)
		}
		return true, nil
	}); err != nil {
		return ClaimView{}, translateSessionError(err)
	}
	return s.deriveView(ctx, record, now)
}

// GetClaim returns one claim and its current derived state.
func (s *Service) GetClaim(ctx context.Context, id string, at time.Time) (ClaimView, error) {
	record, err := s.claims.Get(ctx, id)
	if err != nil {
		return ClaimView{}, translateClaimError(err)
	}
	return s.deriveView(ctx, record, normalizeInspectionTime(at))
}

// ListClaims returns claims in stable ID order with derived state.
func (s *Service) ListClaims(ctx context.Context, at time.Time) ([]ClaimView, error) {
	records, err := s.claims.List(ctx)
	if err != nil {
		return nil, translateClaimError(err)
	}
	views := make([]ClaimView, 0, len(records))
	for _, record := range records {
		view, err := s.deriveView(ctx, record, normalizeInspectionTime(at))
		if err != nil {
			return nil, translateClaimError(err)
		}
		views = append(views, view)
	}
	return views, nil
}

// UpdateClaim permits only explicitly non-material subject/statement edits.
func (s *Service) UpdateClaim(ctx context.Context, req UpdateClaimRequest) (ClaimView, error) {
	if !req.MeaningUnchanged {
		return ClaimView{}, appError(ErrorInvalid, "claim update rejected: material meaning changes require a new claim that supersedes the old claim", nil)
	}
	if req.Subject == nil && req.Statement == nil {
		return ClaimView{}, appError(ErrorInvalid, "claim update requires subject or statement", nil)
	}
	return s.updateClaim(ctx, claimUpdate{sessionID: req.SessionID, id: req.ID, expected: req.ExpectedRevision, activeOnly: true, mutate: func(claim *knowledge.Claim) (bool, error) {
		changed := false
		if req.Subject != nil {
			if claim.Subject != *req.Subject {
				claim.Subject = *req.Subject
				changed = true
			}
		}
		if req.Statement != nil {
			if claim.Statement != *req.Statement {
				claim.Statement = *req.Statement
				changed = true
			}
		}
		return changed, nil
	}})
}

// AttachEvidence appends one immutable-ID evidence record.
func (s *Service) AttachEvidence(ctx context.Context, req AttachEvidenceRequest) (ClaimView, error) {
	prepared, err := s.prepareSourceEvidence(ctx, []knowledge.Evidence{req.Evidence}, time.Now().UTC())
	if err != nil {
		return ClaimView{}, err
	}
	return s.updateClaim(ctx, claimUpdate{sessionID: req.SessionID, id: req.ClaimID, expected: req.ExpectedRevision, mutate: func(claim *knowledge.Claim) (bool, error) {
		claim.Evidence = append(claim.Evidence, prepared[0])
		return true, nil
	}})
}

// SetValidationPolicy installs or updates the explicit all-required policy.
func (s *Service) SetValidationPolicy(ctx context.Context, req SetValidationPolicyRequest) (ClaimView, error) {
	return s.updateClaim(ctx, claimUpdate{sessionID: req.SessionID, id: req.ClaimID, expected: req.ExpectedRevision, activeOnly: true, mutate: func(claim *knowledge.Claim) (bool, error) {
		if reflect.DeepEqual(claim.ValidationPolicy, req.Policy) {
			return false, nil
		}
		claim.ValidationPolicy = req.Policy
		return true, nil
	}})
}

// SupersedeClaim transitions one active claim to an existing active successor.
func (s *Service) SupersedeClaim(ctx context.Context, req SupersedeClaimRequest) (ClaimView, error) {
	if req.ClaimID == req.SuccessorID {
		return ClaimView{}, appError(ErrorInvalid, "a claim cannot supersede itself", nil)
	}
	successor, err := s.claims.Get(ctx, req.SuccessorID)
	if err != nil {
		return ClaimView{}, translateClaimError(err)
	}
	if successor.Claim.Lifecycle.State != knowledge.LifecycleActive {
		return ClaimView{}, appError(ErrorIntegrity, "superseding claim must be active", nil)
	}
	if cycle, err := s.wouldCreateSupersessionCycle(ctx, req.ClaimID, req.SuccessorID); err != nil {
		return ClaimView{}, err
	} else if cycle {
		return ClaimView{}, appError(ErrorIntegrity, "supersession would create a cycle", nil)
	}
	return s.updateClaim(ctx, claimUpdate{sessionID: req.SessionID, id: req.ClaimID, expected: req.ExpectedRevision, activeOnly: true, mutate: func(claim *knowledge.Claim) (bool, error) {
		claim.Lifecycle = knowledge.Lifecycle{State: knowledge.LifecycleSuperseded, SupersededBy: req.SuccessorID}
		return true, nil
	}})
}

// WithdrawClaim retracts one active claim without a successor.
func (s *Service) WithdrawClaim(ctx context.Context, req WithdrawClaimRequest) (ClaimView, error) {
	if strings.TrimSpace(req.Reason) == "" {
		return ClaimView{}, appError(ErrorInvalid, "withdrawal reason is required", nil)
	}
	return s.updateClaim(ctx, claimUpdate{sessionID: req.SessionID, id: req.ClaimID, expected: req.ExpectedRevision, activeOnly: true, mutate: func(claim *knowledge.Claim) (bool, error) {
		claim.Lifecycle = knowledge.Lifecycle{State: knowledge.LifecycleWithdrawn, WithdrawalReason: req.Reason}
		return true, nil
	}})
}

// InspectClaimState computes state without permitting state setters.
func (s *Service) InspectClaimState(ctx context.Context, id string, at time.Time) (knowledge.DerivedState, error) {
	view, err := s.GetClaim(ctx, id, at)
	return view.State, err
}

func (s *Service) updateClaim(ctx context.Context, update claimUpdate) (ClaimView, error) {
	if err := s.requireClaimWrite(update.id); err != nil {
		return ClaimView{}, err
	}
	now := time.Now().UTC()
	var result store.ClaimMutation
	write := session.Write{Path: claimPath(update.id), ExistedBefore: true, Claim: true}
	if err := s.sessions.DoWrite(ctx, update.sessionID, []session.Write{write}, func() (bool, error) {
		var updateErr error
		result, updateErr = s.claims.Update(ctx, update.id, store.Revision(update.expected), func(claim *knowledge.Claim) error {
			if update.activeOnly && claim.Lifecycle.State != knowledge.LifecycleActive {
				return appError(ErrorConflict, "claim update rejected: lifecycle is not active", nil)
			}
			changed, err := update.mutate(claim)
			if err != nil {
				return err
			}
			if changed {
				claim.UpdatedAt = now
			}
			return nil
		})
		if updateErr != nil {
			return false, translateClaimError(updateErr)
		}
		return result.Changed, nil
	}); err != nil {
		return ClaimView{}, translateSessionError(err)
	}
	return s.deriveView(ctx, result.Record, now)
}

func (s *Service) requireClaimWrite(id string) error {
	path := claimPath(id)
	if !s.governance.AllowsWrite(path) {
		return protectedError(path)
	}
	return nil
}

func (s *Service) wouldCreateSupersessionCycle(ctx context.Context, claimID, successorID string) (bool, error) {
	seen := map[string]bool{claimID: true}
	current := successorID
	for current != "" {
		if seen[current] {
			return true, nil
		}
		seen[current] = true
		record, err := s.claims.Get(ctx, current)
		if err != nil {
			var claimErr *store.ClaimError
			if errors.As(err, &claimErr) && claimErr.Code == "claim_not_found" {
				return false, appError(ErrorIntegrity, fmt.Sprintf("broken supersession reference %s", current), err)
			}
			return false, translateClaimError(err)
		}
		claim := record.Claim
		if claim.Lifecycle.State != knowledge.LifecycleSuperseded {
			return false, nil
		}
		current = claim.Lifecycle.SupersededBy
	}
	return false, nil
}

func (s *Service) deriveView(ctx context.Context, record store.ClaimRecord, at time.Time) (ClaimView, error) {
	claim := s.resolveSourceEvidence(ctx, record.Claim, at)
	state, err := knowledge.DeriveState(claim, at)
	if err != nil {
		return ClaimView{}, translateClaimError(err)
	}
	return ClaimView{Claim: claim, State: state, Revision: ClaimRevision(record.Revision)}, nil
}

func (s *Service) prepareSourceEvidence(ctx context.Context, evidence []knowledge.Evidence, at time.Time) ([]knowledge.Evidence, error) {
	prepared := make([]knowledge.Evidence, len(evidence))
	copy(prepared, evidence)
	for index := range prepared {
		id, referenced, err := provenance.SourceIDFromLocator(prepared[index].Locator)
		if err != nil {
			return nil, appError(ErrorInvalid, err.Error(), err)
		}
		if !referenced {
			continue
		}
		record, err := s.sources.Get(ctx, id)
		if err != nil {
			var sourceErr *store.SourceError
			if errors.As(err, &sourceErr) && sourceErr.Code == "source_not_found" {
				checked := at
				prepared[index].Availability = knowledge.AvailabilityMissing
				prepared[index].CheckedAt = &checked
				continue
			}
			return nil, translateSourceError(err)
		}
		prepared[index].Fingerprint = record.Source.RawFingerprint
		prepared[index] = s.observeSourceEvidence(ctx, prepared[index], record.Source, at)
	}
	return prepared, nil
}

func (s *Service) resolveSourceEvidence(ctx context.Context, claim knowledge.Claim, at time.Time) knowledge.Claim {
	evidence := make([]knowledge.Evidence, len(claim.Evidence))
	copy(evidence, claim.Evidence)
	claim.Evidence = evidence
	for index := range claim.Evidence {
		id, referenced, err := provenance.SourceIDFromLocator(claim.Evidence[index].Locator)
		if err != nil || !referenced {
			continue
		}
		record, getErr := s.sources.Get(ctx, id)
		if getErr != nil {
			checked := at
			claim.Evidence[index].Availability = knowledge.AvailabilityMissing
			claim.Evidence[index].CheckedAt = &checked
			continue
		}
		claim.Evidence[index] = s.observeSourceEvidence(ctx, claim.Evidence[index], record.Source, at)
	}
	return claim
}

func (s *Service) observeSourceEvidence(ctx context.Context, evidence knowledge.Evidence, source provenance.SourceRecord, at time.Time) knowledge.Evidence {
	checked := at
	evidence.CheckedAt = &checked
	if source.Status == provenance.StatusWithdrawn {
		evidence.Availability = knowledge.AvailabilityMissing
		evidence.ObservedFingerprint = ""
		return evidence
	}
	exists, fingerprint, err := s.repository.Fingerprint(ctx, source.RawPath)
	if err != nil {
		evidence.Availability = knowledge.AvailabilityUnknown
		evidence.ObservedFingerprint = ""
		return evidence
	}
	if !exists {
		evidence.Availability = knowledge.AvailabilityMissing
		evidence.ObservedFingerprint = ""
		return evidence
	}
	evidence.Availability = knowledge.AvailabilityAvailable
	evidence.ObservedFingerprint = fingerprint
	return evidence
}

func normalizeInspectionTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}

func claimPath(id string) string { return "claims/" + id + ".json" }

func requireExpectedRevision(id string, expected ClaimRevision, current store.Revision) error {
	storeExpected := store.Revision(expected)
	if !store.ValidRevision(storeExpected) {
		return appError(ErrorInvalid, "expected claim revision is required", nil)
	}
	if storeExpected == current {
		return nil
	}
	return &ClaimConflictError{
		ClaimID: id, ExpectedRevision: expected, CurrentRevision: ClaimRevision(current),
	}
}

func translateClaimError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return err
	}
	var conflict *store.RevisionConflictError
	if errors.As(err, &conflict) {
		return &ClaimConflictError{
			ClaimID: conflict.ClaimID, ExpectedRevision: ClaimRevision(conflict.Expected),
			CurrentRevision: ClaimRevision(conflict.Current), Cause: err,
		}
	}
	var claimErr *store.ClaimError
	if errors.As(err, &claimErr) {
		switch claimErr.Code {
		case "claim_exists", "immutable_claim_id":
			return appError(ErrorConflict, err.Error(), err)
		case "invalid_revision":
			return appError(ErrorInvalid, err.Error(), err)
		case "claim_not_found":
			return appError(ErrorNotFound, err.Error(), err)
		case "malformed_claim_file", "filename_id_mismatch", "duplicate_json_key", "invalid_claim_json":
			return appError(ErrorIntegrity, err.Error(), err)
		default:
			return appError(ErrorStorage, err.Error(), err)
		}
	}
	var validationErr *knowledge.ValidationError
	if errors.As(err, &validationErr) {
		return appError(ErrorIntegrity, err.Error(), err)
	}
	return appError(ErrorStorage, err.Error(), err)
}
