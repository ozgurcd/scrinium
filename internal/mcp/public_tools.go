package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	appsvc "scrinium/internal/app"
	"scrinium/internal/knowledge"
	"scrinium/internal/publicapi"
)

func (s *Server) callClaimTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "claim_create":
		var input publicapi.ClaimCreateInput
		if err := decodePublicInput(args, publicapi.ClaimCreateSchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		view, err := s.app.CreateClaim(ctx, appsvc.CreateClaimRequest{
			SessionID: s.sessionID(args), ID: input.ID, Subject: input.Subject, Statement: input.Statement,
			AuthorshipKind: input.Authorship.Kind, AuthorOrigin: input.Authorship.Origin,
			Evidence: input.Evidence, ValidationPolicy: input.ValidationPolicy,
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewClaimResult(view))
	case "claim_get":
		if err := allowedArgs(args, "claim_id"); err != nil {
			return nil, err
		}
		id, err := requiredString(args, "claim_id")
		if err != nil {
			return nil, err
		}
		view, err := s.app.GetClaim(ctx, id, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewClaimResult(view))
	case "claim_list":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		views, err := s.app.ListClaims(ctx, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		claims := make([]publicapi.ClaimResult, 0, len(views))
		for _, view := range views {
			claims = append(claims, publicapi.NewClaimResult(view))
		}
		return jsonTextResult(publicapi.ClaimListResult{SchemaVersion: publicapi.ClaimListSchema, Claims: claims})
	case "claim_update":
		var input publicapi.ClaimUpdateInput
		if err := decodePublicInput(args, publicapi.ClaimUpdateSchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		view, err := s.app.UpdateClaim(ctx, appsvc.UpdateClaimRequest{
			SessionID: s.sessionID(args), ID: input.ClaimID, Subject: input.Subject, Statement: input.Statement,
			MeaningUnchanged: input.MeaningUnchanged, ExpectedRevision: appsvc.ClaimRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewClaimResult(view))
	case "claim_add_evidence":
		var input publicapi.ClaimEvidenceInput
		if err := decodePublicInput(args, publicapi.ClaimEvidenceSchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		view, err := s.app.AttachEvidence(ctx, appsvc.AttachEvidenceRequest{
			SessionID: s.sessionID(args), ClaimID: input.ClaimID, Evidence: input.Evidence,
			ExpectedRevision: appsvc.ClaimRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewClaimResult(view))
	case "claim_set_validation_policy":
		var input publicapi.ClaimPolicyInput
		if err := decodePublicInput(args, publicapi.ClaimPolicySchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		view, err := s.app.SetValidationPolicy(ctx, appsvc.SetValidationPolicyRequest{
			SessionID: s.sessionID(args), ClaimID: input.ClaimID, Policy: input.Policy,
			ExpectedRevision: appsvc.ClaimRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewClaimResult(view))
	case "claim_validate":
		return s.validateClaim(ctx, args)
	case "claim_supersede":
		var input publicapi.ClaimSupersedeInput
		if err := decodePublicInput(args, publicapi.ClaimSupersedeSchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		view, err := s.app.SupersedeClaim(ctx, appsvc.SupersedeClaimRequest{
			SessionID: s.sessionID(args), ClaimID: input.ClaimID, SuccessorID: input.SuccessorID,
			ExpectedRevision: appsvc.ClaimRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewClaimResult(view))
	case "claim_withdraw":
		var input publicapi.ClaimWithdrawInput
		if err := decodePublicInput(args, publicapi.ClaimWithdrawSchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		view, err := s.app.WithdrawClaim(ctx, appsvc.WithdrawClaimRequest{
			SessionID: s.sessionID(args), ClaimID: input.ClaimID, Reason: input.Reason,
			ExpectedRevision: appsvc.ClaimRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewClaimResult(view))
	case "claim_lint":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		report, err := s.app.LintClaims(ctx, time.Now().UTC())
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.ClaimLintResult{SchemaVersion: publicapi.ClaimLintSchema, Method: "deterministic", Report: report})
	default:
		return nil, fmt.Errorf("unknown claim tool: %s", name)
	}
}

func (s *Server) validateClaim(ctx context.Context, args map[string]any) (any, error) {
	var input publicapi.ClaimValidationInput
	if err := decodePublicInput(args, publicapi.ClaimValidationSchema, &input, "session_id", "input"); err != nil {
		return nil, err
	}
	switch input.Selection {
	case publicapi.ValidationSelectionBinding:
		if strings.TrimSpace(input.BindingID) == "" {
			return nil, fmt.Errorf("binding selection requires binding_id")
		}
		if len(input.RequiredInputs) != 0 {
			return nil, fmt.Errorf("binding selection does not accept required_inputs")
		}
		attempt, err := s.app.ValidateClaimBinding(ctx, appsvc.ValidateClaimBindingRequest{
			SessionID: s.sessionID(args), ClaimID: input.ClaimID, BindingID: input.BindingID,
			Inputs: input.Inputs, ExpectedRevision: appsvc.ClaimRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.ClaimValidationRun{
			SchemaVersion: publicapi.ClaimValidationRunSchema, Selection: input.Selection,
			Results: []knowledge.ValidationResult{attempt.Result}, Claim: publicapi.NewClaimResult(attempt.View),
		})
	case publicapi.ValidationSelectionRequired:
		if strings.TrimSpace(input.BindingID) != "" || len(input.Inputs) != 0 {
			return nil, fmt.Errorf("required selection accepts required_inputs only")
		}
		run, err := s.app.ValidateRequiredClaimBindings(ctx, appsvc.ValidateRequiredBindingsRequest{
			SessionID: s.sessionID(args), ClaimID: input.ClaimID, Inputs: input.RequiredInputs,
			ExpectedRevision: appsvc.ClaimRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.ClaimValidationRun{
			SchemaVersion: publicapi.ClaimValidationRunSchema, Selection: input.Selection,
			Results: run.Results, Claim: publicapi.NewClaimResult(run.View),
		})
	default:
		return nil, fmt.Errorf("selection must be %q or %q", publicapi.ValidationSelectionBinding, publicapi.ValidationSelectionRequired)
	}
}

func (s *Server) callSourceTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "source_register":
		var input publicapi.SourceRegisterInput
		if err := decodePublicInput(args, publicapi.SourceRegisterSchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		_, err := s.app.RegisterSource(ctx, appsvc.RegisterSourceRequest{
			SessionID: s.sessionID(args), SourceID: input.SourceID, Title: input.Title, RawPath: input.RawPath,
			SourceType: input.SourceType, Origin: input.Origin, TrustLevel: input.TrustLevel,
			ReceivedDate: input.ReceivedDate, IngestDate: input.IngestDate, Summary: input.Summary,
			DerivedClaims: input.DerivedClaims, DerivedPages: input.DerivedPages, ProvenanceNotes: input.ProvenanceNotes,
		})
		if err != nil {
			return nil, err
		}
		view, err := s.app.GetSource(ctx, input.SourceID)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewSourceResult(view))
	case "source_get":
		if err := allowedArgs(args, "source_id"); err != nil {
			return nil, err
		}
		id, err := requiredString(args, "source_id")
		if err != nil {
			return nil, err
		}
		view, err := s.app.GetSource(ctx, id)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewSourceResult(view))
	case "source_list":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		views, err := s.app.ListSources(ctx)
		if err != nil {
			return nil, err
		}
		sources := make([]publicapi.SourceResult, 0, len(views))
		for _, view := range views {
			sources = append(sources, publicapi.NewSourceResult(view))
		}
		return jsonTextResult(publicapi.SourceListResult{SchemaVersion: publicapi.SourceListSchema, Sources: sources})
	case "source_refresh":
		var input publicapi.SourceRefreshInput
		if err := decodePublicInput(args, publicapi.SourceRefreshSchema, &input, "session_id", "input"); err != nil {
			return nil, err
		}
		view, err := s.app.RefreshSource(ctx, appsvc.RefreshSourceRequest{
			SessionID: s.sessionID(args), SourceID: input.SourceID,
			ExpectedRevision: appsvc.SourceRevision(input.ExpectedRevision),
		})
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.NewSourceResult(view))
	case "source_migration_status":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		report, err := s.app.AssessSourceMigration(ctx)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.SourceMigrationStatus{SchemaVersion: publicapi.SourceMigrationSchema, Report: report})
	default:
		return nil, fmt.Errorf("unknown source tool: %s", name)
	}
}

func (s *Server) callPublicSessionTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "session_begin":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		status, err := s.app.BeginSession(ctx)
		if err != nil {
			return nil, err
		}
		s.setCurrentSession(status.SessionID)
		return jsonTextResult(publicapi.SessionResult{SchemaVersion: publicapi.SessionResultSchema, SessionStatus: status})
	case "session_continue":
		if err := allowedArgs(args, "session_id"); err != nil {
			return nil, err
		}
		id, err := requiredSessionID(s, args)
		if err != nil {
			return nil, err
		}
		status, err := s.app.ContinueSession(ctx, id)
		if err != nil {
			return nil, err
		}
		s.setCurrentSession(id)
		return jsonTextResult(publicapi.SessionResult{SchemaVersion: publicapi.SessionResultSchema, SessionStatus: status})
	case "session_finish", "session_abandon":
		allowed := []string{"session_id"}
		if name == "session_abandon" {
			allowed = append(allowed, "reason")
		}
		if err := allowedArgs(args, allowed...); err != nil {
			return nil, err
		}
		id, err := requiredSessionID(s, args)
		if err != nil {
			return nil, err
		}
		var status appsvc.SessionStatus
		if name == "session_finish" {
			status, err = s.app.FinishSession(ctx, id)
		} else {
			reason, reasonErr := requiredString(args, "reason")
			if reasonErr != nil {
				return nil, reasonErr
			}
			status, err = s.app.AbandonSession(ctx, id, reason)
		}
		if err != nil {
			return nil, err
		}
		if s.sessionID(nil) == id {
			s.setCurrentSession("")
		}
		return jsonTextResult(publicapi.SessionResult{SchemaVersion: publicapi.SessionResultSchema, SessionStatus: status})
	case "session_list":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		statuses, err := s.app.ListActiveSessions(ctx)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.SessionListResult{SchemaVersion: publicapi.SessionListSchema, Sessions: statuses})
	default:
		return nil, fmt.Errorf("unknown session tool: %s", name)
	}
}

func decodePublicInput(args map[string]any, schema string, target publicapi.VersionedInput, allowed ...string) error {
	if err := allowedArgs(args, allowed...); err != nil {
		return err
	}
	value, exists := args["input"]
	if !exists {
		return fmt.Errorf("missing input parameter")
	}
	return publicapi.DecodeValue(value, schema, target)
}

func allowedArgs(args map[string]any, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key := range args {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("unknown parameter %q", key)
		}
	}
	return nil
}
