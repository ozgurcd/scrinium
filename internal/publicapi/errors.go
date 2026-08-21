package publicapi

import (
	"context"
	"errors"

	"scrinium/internal/app"
	"scrinium/internal/session"
)

func ErrorFrom(operation string, err error) PublicError {
	result := PublicError{
		SchemaVersion: ErrorSchema,
		Code:          "internal_error",
		Message:       err.Error(),
		Operation:     operation,
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		result.Code = "cannot_evaluate"
		result.Retryable = true
		return result
	}
	var claimConflict *app.ClaimConflictError
	if errors.As(err, &claimConflict) {
		result.Code = "conflict"
		result.ClaimID = claimConflict.ClaimID
		result.ExpectedRevision = string(claimConflict.ExpectedRevision)
		result.CurrentRevision = string(claimConflict.CurrentRevision)
		result.Retryable = true
		return result
	}
	var sourceConflict *app.SourceConflictError
	if errors.As(err, &sourceConflict) {
		result.Code = "conflict"
		result.SourceID = sourceConflict.SourceID
		result.ExpectedRevision = string(sourceConflict.ExpectedRevision)
		result.CurrentRevision = string(sourceConflict.CurrentRevision)
		result.Retryable = true
		return result
	}
	var sessionErr *app.SessionError
	if errors.As(err, &sessionErr) {
		result.SessionID = sessionErr.SessionID
		switch sessionErr.Code {
		case session.ErrorNotFound:
			result.Code = "not_found"
		case session.ErrorClosed:
			result.Code = "session_closed"
		case session.ErrorPrerequisite:
			result.Code = "session_required"
		case session.ErrorInvalidID, session.ErrorRepositoryMismatch:
			result.Code = "invalid_input"
		case session.ErrorCorrupt:
			result.Code = "malformed_canonical_record"
		case session.ErrorMaintenance:
			result.Code = "governance_denied"
		default:
			result.Code = "internal_error"
		}
		return result
	}
	var appErr *app.Error
	if errors.As(err, &appErr) {
		switch appErr.Kind {
		case app.ErrorInvalid:
			result.Code = "invalid_input"
		case app.ErrorConflict:
			result.Code = "conflict"
		case app.ErrorGovernance:
			result.Code = "governance_denied"
		case app.ErrorSession:
			result.Code = "session_required"
		case app.ErrorIntegrity:
			result.Code = "malformed_canonical_record"
		case app.ErrorNotFound:
			result.Code = "not_found"
		case app.ErrorValidator:
			result.Code = "validator_unavailable"
		}
	}
	return result
}
