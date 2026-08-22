package publicapi

import (
	"scrinium/internal/app"
	"scrinium/internal/knowledge"
	"scrinium/internal/lint"
	"scrinium/internal/provenance"
)

const (
	ClaimCreateSchema        = "scrinium.claim-create/v1"
	ClaimUpdateSchema        = "scrinium.claim-update/v1"
	ClaimEvidenceSchema      = "scrinium.claim-evidence/v1"
	ClaimPolicySchema        = "scrinium.claim-policy/v1"
	ClaimValidationSchema    = "scrinium.claim-validation/v1"
	ClaimSupersedeSchema     = "scrinium.claim-supersede/v1"
	ClaimWithdrawSchema      = "scrinium.claim-withdraw/v1"
	ClaimResultSchema        = "scrinium.claim-result/v1"
	ClaimListSchema          = "scrinium.claim-list/v1"
	ClaimQuerySchema         = "scrinium.claim-query/v1"
	ClaimValidationRunSchema = "scrinium.claim-validation-run/v1"
	ClaimLintSchema          = "scrinium.claim-lint/v1"
	SourceRegisterSchema     = "scrinium.source-register/v1"
	SourceRefreshSchema      = "scrinium.source-refresh/v1"
	SourceResultSchema       = "scrinium.source-result/v1"
	SourceListSchema         = "scrinium.source-list/v1"
	SourceMigrationSchema    = "scrinium.source-migration-status/v1"
	SessionResultSchema      = "scrinium.session-result/v1"
	SessionListSchema        = "scrinium.session-list/v1"
	CapabilitiesSchema       = "scrinium.capabilities/v1"
	ErrorSchema              = "scrinium.error/v1"

	ValidationSelectionBinding  = "binding"
	ValidationSelectionRequired = "required"
)

type AuthorshipInput struct {
	Kind   knowledge.AuthorshipKind `json:"kind"`
	Origin string                   `json:"origin"`
}

type ClaimCreateInput struct {
	SchemaVersion    string                      `json:"schema_version"`
	ID               string                      `json:"id"`
	Subject          string                      `json:"subject"`
	Statement        string                      `json:"statement"`
	Authorship       AuthorshipInput             `json:"authorship"`
	Evidence         []knowledge.Evidence        `json:"evidence"`
	ValidationPolicy *knowledge.ValidationPolicy `json:"validation_policy,omitempty"`
}

func (i *ClaimCreateInput) SchemaVersionValue() string { return i.SchemaVersion }

type ClaimUpdateInput struct {
	SchemaVersion    string  `json:"schema_version"`
	ClaimID          string  `json:"claim_id"`
	ExpectedRevision string  `json:"expected_revision"`
	Subject          *string `json:"subject,omitempty"`
	Statement        *string `json:"statement,omitempty"`
	MeaningUnchanged bool    `json:"meaning_unchanged"`
}

func (i *ClaimUpdateInput) SchemaVersionValue() string { return i.SchemaVersion }

type ClaimEvidenceInput struct {
	SchemaVersion    string             `json:"schema_version"`
	ClaimID          string             `json:"claim_id"`
	ExpectedRevision string             `json:"expected_revision"`
	Evidence         knowledge.Evidence `json:"evidence"`
}

func (i *ClaimEvidenceInput) SchemaVersionValue() string { return i.SchemaVersion }

// ClaimQueryInput is the OPTIONAL claim_list filter document. Every field
// is optional and filters are AND-composed; a claim_list call without an
// input behaves exactly as before (list all). A NEW input schema rather
// than an extension of claim-list/v1 because claim-list/v1 is a RESULT
// schema: inputs and results are versioned separately throughout this
// API, and the result shape is unchanged by filtering.
type ClaimQueryInput struct {
	SchemaVersion    string `json:"schema_version"`
	ValidatorID      string `json:"validator_id,omitempty"`
	BindingReference string `json:"binding_reference,omitempty"`
	Target           string `json:"target,omitempty"`
	Lifecycle        string `json:"lifecycle,omitempty"`
	Assessment       string `json:"assessment,omitempty"`
	Freshness        string `json:"freshness,omitempty"`
	LocatorPrefix    string `json:"locator_prefix,omitempty"`
}

func (i *ClaimQueryInput) SchemaVersionValue() string { return i.SchemaVersion }

type ClaimPolicyInput struct {
	SchemaVersion    string                      `json:"schema_version"`
	ClaimID          string                      `json:"claim_id"`
	ExpectedRevision string                      `json:"expected_revision"`
	Policy           *knowledge.ValidationPolicy `json:"policy"`
}

func (i *ClaimPolicyInput) SchemaVersionValue() string { return i.SchemaVersion }

type ClaimValidationInput struct {
	SchemaVersion    string                       `json:"schema_version"`
	ClaimID          string                       `json:"claim_id"`
	ExpectedRevision string                       `json:"expected_revision"`
	Selection        string                       `json:"selection"`
	BindingID        string                       `json:"binding_id,omitempty"`
	Inputs           map[string]string            `json:"inputs,omitempty"`
	RequiredInputs   map[string]map[string]string `json:"required_inputs,omitempty"`
}

func (i *ClaimValidationInput) SchemaVersionValue() string { return i.SchemaVersion }

type ClaimSupersedeInput struct {
	SchemaVersion    string `json:"schema_version"`
	ClaimID          string `json:"claim_id"`
	SuccessorID      string `json:"successor_id"`
	ExpectedRevision string `json:"expected_revision"`
}

func (i *ClaimSupersedeInput) SchemaVersionValue() string { return i.SchemaVersion }

type ClaimWithdrawInput struct {
	SchemaVersion    string `json:"schema_version"`
	ClaimID          string `json:"claim_id"`
	Reason           string `json:"reason"`
	ExpectedRevision string `json:"expected_revision"`
}

func (i *ClaimWithdrawInput) SchemaVersionValue() string { return i.SchemaVersion }

type SourceRegisterInput struct {
	SchemaVersion   string   `json:"schema_version"`
	SourceID        string   `json:"source_id"`
	Title           string   `json:"title"`
	RawPath         string   `json:"raw_path"`
	SourceType      string   `json:"source_type"`
	Origin          string   `json:"origin"`
	TrustLevel      string   `json:"trust_level"`
	ReceivedDate    string   `json:"received_date,omitempty"`
	IngestDate      string   `json:"ingest_date"`
	Summary         string   `json:"summary"`
	DerivedClaims   []string `json:"derived_claims"`
	DerivedPages    []string `json:"derived_pages"`
	ProvenanceNotes []string `json:"provenance_notes"`
}

func (i *SourceRegisterInput) SchemaVersionValue() string { return i.SchemaVersion }

type SourceRefreshInput struct {
	SchemaVersion    string `json:"schema_version"`
	SourceID         string `json:"source_id"`
	ExpectedRevision string `json:"expected_revision"`
}

func (i *SourceRefreshInput) SchemaVersionValue() string { return i.SchemaVersion }

type ClaimResult struct {
	SchemaVersion string                 `json:"schema_version"`
	Claim         knowledge.Claim        `json:"claim"`
	State         knowledge.DerivedState `json:"state"`
	Revision      string                 `json:"revision"`
}

func NewClaimResult(view app.ClaimView) ClaimResult {
	return ClaimResult{SchemaVersion: ClaimResultSchema, Claim: view.Claim, State: view.State, Revision: string(view.Revision)}
}

type ClaimListResult struct {
	SchemaVersion string        `json:"schema_version"`
	Claims        []ClaimResult `json:"claims"`
}

type ClaimValidationRun struct {
	SchemaVersion string                       `json:"schema_version"`
	Selection     string                       `json:"selection"`
	Results       []knowledge.ValidationResult `json:"results"`
	Claim         ClaimResult                  `json:"claim"`
}

type ClaimLintResult struct {
	SchemaVersion string           `json:"schema_version"`
	Method        string           `json:"method"`
	Report        lint.ClaimReport `json:"report"`
}

type SourceResult struct {
	SchemaVersion string                  `json:"schema_version"`
	Source        provenance.SourceRecord `json:"source"`
	Revision      string                  `json:"revision"`
}

func NewSourceResult(view app.SourceView) SourceResult {
	return SourceResult{SchemaVersion: SourceResultSchema, Source: view.Source, Revision: string(view.Revision)}
}

type SourceListResult struct {
	SchemaVersion string         `json:"schema_version"`
	Sources       []SourceResult `json:"sources"`
}

type SourceMigrationStatus struct {
	SchemaVersion string                    `json:"schema_version"`
	Report        app.SourceMigrationReport `json:"report"`
}

type SessionResult struct {
	SchemaVersion string `json:"schema_version"`
	app.SessionStatus
}

type SessionListResult struct {
	SchemaVersion string              `json:"schema_version"`
	Sessions      []app.SessionStatus `json:"sessions"`
}

type ValidatorStatus struct {
	ID                string               `json:"id"`
	Optional          bool                 `json:"optional"`
	Available         bool                 `json:"available"`
	Reason            string               `json:"reason,omitempty"`
	Descriptor        *ValidatorDescriptor `json:"descriptor,omitempty"`
	BindingSchemas    []string             `json:"binding_schemas"`
	TrustPresentation string               `json:"trust_presentation"`
}

type ValidatorDescriptor struct {
	ID                       string              `json:"id"`
	Version                  string              `json:"version"`
	SupportedBindingVersions []string            `json:"supported_binding_versions"`
	MaximumAssurance         knowledge.Assurance `json:"maximum_assurance"`
}

type PublicError struct {
	SchemaVersion    string `json:"schema_version"`
	Code             string `json:"code"`
	Message          string `json:"message"`
	Operation        string `json:"operation"`
	ClaimID          string `json:"claim_id,omitempty"`
	SourceID         string `json:"source_id,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	ExpectedRevision string `json:"expected_revision,omitempty"`
	CurrentRevision  string `json:"current_revision,omitempty"`
	Retryable        bool   `json:"retryable"`
}
