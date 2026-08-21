// Package knowledge defines Scrinium's transport- and storage-independent
// evidence-backed claim model.
package knowledge

import "time"

const SchemaVersion = "scrinium.claim/v1"

type LifecycleState string

const (
	LifecycleActive     LifecycleState = "active"
	LifecycleSuperseded LifecycleState = "superseded"
	LifecycleWithdrawn  LifecycleState = "withdrawn"
)

type Assessment string

const (
	AssessmentAsserted   Assessment = "asserted"
	AssessmentSourced    Assessment = "sourced"
	AssessmentObserved   Assessment = "observed"
	AssessmentVerified   Assessment = "verified"
	AssessmentChallenged Assessment = "challenged"
)

type Freshness string

const (
	FreshnessCurrent Freshness = "current"
	FreshnessStale   Freshness = "stale"
	FreshnessUnknown Freshness = "unknown"
)

type ValidationOutcome string

const (
	OutcomePass           ValidationOutcome = "pass"
	OutcomeFail           ValidationOutcome = "fail"
	OutcomeCannotEvaluate ValidationOutcome = "cannot_evaluate"
)

type Assurance string

const (
	AssuranceObservation  Assurance = "observation"
	AssuranceVerification Assurance = "verification"
)

type EvidenceKind string

const (
	EvidenceOwnerAssertion       EvidenceKind = "owner_assertion"
	EvidenceHumanAssertion       EvidenceKind = "human_assertion"
	EvidenceDecision             EvidenceKind = "decision"
	EvidenceExternalSource       EvidenceKind = "external_source"
	EvidenceRepositoryReference  EvidenceKind = "repository_reference"
	EvidenceManualVerification   EvidenceKind = "manual_verification"
	EvidenceValidatorObservation EvidenceKind = "validator_observation"
	EvidenceValidatorProof       EvidenceKind = "validator_proof"
)

type EvidencePolarity string

const (
	PolaritySupports   EvidencePolarity = "supports"
	PolarityChallenges EvidencePolarity = "challenges"
	PolarityContext    EvidencePolarity = "context"
)

type OriginKind string

const (
	OriginOwner        OriginKind = "owner"
	OriginHuman        OriginKind = "human"
	OriginRepository   OriginKind = "repository"
	OriginExternal     OriginKind = "external"
	OriginValidator    OriginKind = "validator"
	OriginLLMGenerated OriginKind = "llm_generated"
)

type EvidenceAvailability string

const (
	AvailabilityAvailable EvidenceAvailability = "available"
	AvailabilityMissing   EvidenceAvailability = "missing"
	AvailabilityUnknown   EvidenceAvailability = "unknown"
)

type AuthorshipKind string

const (
	AuthorshipOwner  AuthorshipKind = "owner"
	AuthorshipHuman  AuthorshipKind = "human"
	AuthorshipAgent  AuthorshipKind = "agent"
	AuthorshipImport AuthorshipKind = "import"
)

// Claim is the canonical aggregate persisted as one JSON file.
// Assessment and freshness are intentionally absent because they are derived.
type Claim struct {
	SchemaVersion     string             `json:"schema_version"`
	ID                string             `json:"id"`
	Subject           string             `json:"subject"`
	Statement         string             `json:"statement"`
	Lifecycle         Lifecycle          `json:"lifecycle"`
	Authorship        Authorship         `json:"authorship"`
	Evidence          []Evidence         `json:"evidence"`
	ValidationPolicy  *ValidationPolicy  `json:"validation_policy,omitempty"`
	ValidationResults []ValidationResult `json:"validation_results"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
}

type Lifecycle struct {
	State            LifecycleState `json:"state"`
	SupersededBy     string         `json:"superseded_by,omitempty"`
	WithdrawalReason string         `json:"withdrawal_reason,omitempty"`
}

type Authorship struct {
	Kind       AuthorshipKind `json:"kind"`
	Origin     string         `json:"origin"`
	RecordedAt time.Time      `json:"recorded_at"`
}

type EvidenceOrigin struct {
	Kind      OriginKind `json:"kind"`
	Reference string     `json:"reference"`
}

// Evidence records why a claim is supported, challenged, or contextualized.
type Evidence struct {
	ID                  string               `json:"id"`
	Kind                EvidenceKind         `json:"kind"`
	Polarity            EvidencePolarity     `json:"polarity"`
	Origin              EvidenceOrigin       `json:"origin"`
	Locator             string               `json:"locator"`
	Scope               string               `json:"scope"`
	Fingerprint         string               `json:"fingerprint,omitempty"`
	Availability        EvidenceAvailability `json:"availability"`
	ObservedFingerprint string               `json:"observed_fingerprint,omitempty"`
	CapturedAt          time.Time            `json:"captured_at"`
	CheckedAt           *time.Time           `json:"checked_at,omitempty"`
	ValidUntil          *time.Time           `json:"valid_until,omitempty"`
	DerivedFrom         []string             `json:"derived_from"`
}

type ValidationPolicy struct {
	Mode     string              `json:"mode"`
	Bindings []ValidationBinding `json:"bindings"`
}

// ValidationBinding is generic and contains no integration-specific types.
type ValidationBinding struct {
	ID                  string            `json:"id"`
	ValidatorID         string            `json:"validator_id"`
	BindingVersion      string            `json:"binding_version"`
	Reference           string            `json:"reference"`
	Parameters          map[string]string `json:"parameters,omitempty"`
	Required            bool              `json:"required"`
	RequiredAssurance   Assurance         `json:"required_assurance"`
	EvidenceIDs         []string          `json:"evidence_ids"`
	InputFingerprint    string            `json:"input_fingerprint"`
	RepositoryRevision  string            `json:"repository_revision,omitempty"`
	SnapshotFingerprint string            `json:"snapshot_fingerprint,omitempty"`
	ValidForSeconds     int64             `json:"valid_for_seconds,omitempty"`
}

// ValidationResult is one immutable validation attempt. History is retained.
type ValidationResult struct {
	ID                  string                 `json:"id"`
	BindingID           string                 `json:"binding_id"`
	ValidatorID         string                 `json:"validator_id"`
	ValidatorVersion    string                 `json:"validator_version"`
	BindingVersion      string                 `json:"binding_version"`
	RepositoryRevision  string                 `json:"repository_revision,omitempty"`
	SnapshotFingerprint string                 `json:"snapshot_fingerprint,omitempty"`
	InputFingerprint    string                 `json:"input_fingerprint"`
	Assurance           Assurance              `json:"assurance"`
	Outcome             ValidationOutcome      `json:"outcome"`
	ReasonCode          string                 `json:"reason_code,omitempty"`
	Reason              string                 `json:"reason"`
	EvidenceIDs         []string               `json:"evidence_ids"`
	Metadata            map[string]string      `json:"metadata,omitempty"`
	Diagnostics         []ValidationDiagnostic `json:"diagnostics,omitempty"`
	StartedAt           time.Time              `json:"started_at"`
	CompletedAt         time.Time              `json:"completed_at"`
	ValidUntil          *time.Time             `json:"valid_until,omitempty"`
}

// ValidationDiagnostic is a concise, structured diagnostic emitted by a validator.
// It deliberately carries no validator-specific behavior.
type ValidationDiagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Target  string `json:"target,omitempty"`
}

// DerivedState is computed and never persisted as caller-controlled state.
type DerivedState struct {
	Lifecycle     LifecycleState `json:"lifecycle"`
	Assessment    Assessment     `json:"assessment"`
	Freshness     Freshness      `json:"freshness"`
	LastValidated *time.Time     `json:"last_validated,omitempty"`
	Reasons       []string       `json:"reasons"`
}
