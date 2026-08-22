// Package app coordinates Scrinium's compatibility page workflows.
package app

import (
	"fmt"

	"scrinium/internal/lint"
	"scrinium/internal/session"
)

// Config is the repository-owned Scrinium configuration.
type Config struct {
	WikiRoot        string           `json:"wiki_root"`
	WriteGovernance *WriteGovernance `json:"write_governance,omitempty"`
	Validators      *Validators      `json:"validators,omitempty"`
	// ValidationTargets maps abstract target NAMES to local repository
	// roots that validators may READ (owner ruling: a validator may read
	// outside the governed repo; Scrinium writes stay under wiki_root).
	// Bindings name a target; only names allowlisted here resolve — a
	// binding can never carry a filesystem path (see the validator
	// Config doc comments below). Additive within the v1 config schema.
	ValidationTargets map[string]string `json:"validation_targets,omitempty"`
}

// WriteGovernance configures protected wiki paths.
type WriteGovernance struct {
	ProtectedFiles []string `json:"protected_files"`
}

// Validators contains optional external-validator process configuration.
type Validators struct {
	Rulefloor *RulefloorValidator `json:"rulefloor,omitempty"`
	Gograph   *GographValidator   `json:"gograph,omitempty"`
}

// RulefloorValidator configures only the trusted executable. Binding content
// cannot supply executable arguments or repository paths.
type RulefloorValidator struct {
	Executable string `json:"executable,omitempty"`
}

// GographValidator configures only the trusted executable. Binding content
// cannot supply executable arguments, repository paths, or query text.
type GographValidator struct {
	Executable string `json:"executable,omitempty"`
}

// ErrorKind identifies an application failure without transport coupling.
type ErrorKind string

const (
	ErrorInvalid    ErrorKind = "invalid_argument"
	ErrorNotFound   ErrorKind = "not_found"
	ErrorValidator  ErrorKind = "validator_unavailable"
	ErrorConflict   ErrorKind = "conflict"
	ErrorGovernance ErrorKind = "governance"
	ErrorSession    ErrorKind = "session"
	ErrorStorage    ErrorKind = "storage"
	ErrorIntegrity  ErrorKind = "integrity"
)

// Error is a typed application error.
type Error struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.Cause }

func appError(kind ErrorKind, message string, cause error) error {
	return &Error{Kind: kind, Message: message, Cause: cause}
}

func storageError(err error, format string, args ...any) error {
	return appError(ErrorStorage, fmt.Sprintf(format, args...), err)
}

// PageRequest identifies one wiki page.
type PageRequest struct {
	Path      string
	SessionID string
}

// Page contains one repository-local wiki document.
type Page struct {
	Path    string
	Content string
}

// WritePageRequest replaces one wiki page.
type WritePageRequest struct {
	Path      string
	Content   string
	SessionID string
}

// DraftRequest creates one document below drafts/.
type DraftRequest struct {
	Name      string
	Content   string
	SessionID string
}

// AppendRequest appends content to one wiki file.
type AppendRequest struct {
	Path      string
	Content   string
	SessionID string
}

// MovePageRequest moves one wiki page without overwriting the destination.
type MovePageRequest struct {
	From      string
	To        string
	SessionID string
}

// ArchivePageRequest moves a page under archive/ by default.
type ArchivePageRequest struct {
	Path        string
	ArchivePath string
	SessionID   string
}

// RegisterSourceRequest contains the existing source-registry inputs.
type RegisterSourceRequest struct {
	SessionID       string
	SourceID        string
	Title           string
	RawPath         string
	SourceType      string
	Origin          string
	TrustLevel      string
	ReceivedDate    string
	IngestDate      string
	Summary         string
	DerivedClaims   []string
	DerivedPages    []string
	ProvenanceNotes []string
}

// SetupResult reports which standard files setup created or retained.
type SetupResult struct {
	Created []string
	Skipped []string
}

// AdoptionReport is the typed compatibility adoption result.
type AdoptionReport struct {
	Mode            string         `json:"mode"`
	MissingPages    []string       `json:"missing_pages"`
	LintFindings    []lint.Finding `json:"lint_findings"`
	Recommendations []string       `json:"recommendations"`
}

// GovernanceStatus is the live policy view exposed to adapters.
type GovernanceStatus struct {
	Enabled        bool
	ProtectedFiles []string
}

// SessionStatus exposes the typed compatibility session view.
type SessionStatus = session.Status

// SessionError preserves a typed durable-session code at the application boundary.
type SessionError struct {
	Code      session.ErrorCode
	SessionID string
	Cause     error
}

func (e *SessionError) Error() string { return e.Cause.Error() }

func (e *SessionError) Unwrap() error { return e.Cause }
