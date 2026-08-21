package provenance

import (
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"

	"scrinium/internal/knowledge"
)

const SchemaVersion = "scrinium.source/v1"

var sourceIDPattern = regexp.MustCompile(`^SRC-([0-9]{8})-([a-z0-9]+(?:-[a-z0-9]+)*)$`)

type SourceType string

const (
	SourceTypeProjectDocument    SourceType = "project_document"
	SourceTypeDecision           SourceType = "decision"
	SourceTypeRepositoryDocument SourceType = "repository_document"
	SourceTypeExternalDocument   SourceType = "external_document"
	SourceTypeOwnerInput         SourceType = "owner_input"
	SourceTypeOther              SourceType = "other"
	SourceTypeUnknown            SourceType = "unknown"
)

type OriginKind string

const (
	OriginProject  OriginKind = "project"
	OriginOwner    OriginKind = "owner"
	OriginExternal OriginKind = "external"
	OriginUnknown  OriginKind = "unknown"
)

type TrustClassification string

const (
	TrustProject  TrustClassification = "trusted-project"
	TrustOwner    TrustClassification = "trusted-owner"
	TrustExternal TrustClassification = "external"
	TrustUnknown  TrustClassification = "unknown"
)

type Status string

const (
	StatusCurrent    Status = "current"
	StatusSuperseded Status = "superseded"
	StatusWithdrawn  Status = "withdrawn"
)

type Date string

func ParseDate(value string) (Date, error) {
	if _, err := time.Parse("2006-01-02", value); err != nil {
		return "", fmt.Errorf("invalid date %q: expected YYYY-MM-DD", value)
	}
	return Date(value), nil
}

type Origin struct {
	Kind  OriginKind          `json:"kind"`
	Trust TrustClassification `json:"trust"`
}

type SourceRecord struct {
	SchemaVersion   string     `json:"schema_version"`
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	SourceType      SourceType `json:"source_type"`
	Origin          Origin     `json:"origin"`
	RawPath         string     `json:"raw_path"`
	RawFingerprint  string     `json:"raw_fingerprint"`
	ReceivedDate    *Date      `json:"received_date"`
	IngestDate      Date       `json:"ingest_date"`
	Status          Status     `json:"status"`
	SupersededBy    string     `json:"superseded_by,omitempty"`
	DerivedClaims   []string   `json:"derived_claims"`
	DerivedPages    []string   `json:"derived_pages"`
	ProvenanceNotes []string   `json:"provenance_notes"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func ValidSourceID(id string) bool {
	match := sourceIDPattern.FindStringSubmatch(id)
	if match == nil {
		return false
	}
	_, err := time.Parse("20060102", match[1])
	return err == nil
}

func SourceDate(id string) (Date, bool) {
	match := sourceIDPattern.FindStringSubmatch(id)
	if match == nil {
		return "", false
	}
	parsed, err := time.Parse("20060102", match[1])
	if err != nil {
		return "", false
	}
	return Date(parsed.Format("2006-01-02")), true
}

func SourceIDFromLocator(locator string) (string, bool, error) {
	value := strings.TrimSpace(locator)
	referenced := strings.HasPrefix(value, "source:") || strings.HasPrefix(value, "SRC-")
	if !referenced {
		return "", false, nil
	}
	id := strings.TrimPrefix(value, "source:")
	if !ValidSourceID(id) {
		return "", true, fmt.Errorf("invalid canonical source locator %q", locator)
	}
	return id, true, nil
}

func (record SourceRecord) Validate() error {
	for _, validate := range []func() error{
		record.validateIdentity,
		record.validateDates,
		record.validateLifecycle,
		record.validateReferences,
		record.validateTimestamps,
	} {
		if err := validate(); err != nil {
			return err
		}
	}
	return nil
}

func (record SourceRecord) validateIdentity() error {
	if record.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported source schema version %q", record.SchemaVersion)
	}
	if !ValidSourceID(record.ID) {
		return fmt.Errorf("invalid source ID %q", record.ID)
	}
	if strings.TrimSpace(record.Title) == "" {
		return fmt.Errorf("source title is required")
	}
	if !validSourceType(record.SourceType) {
		return fmt.Errorf("invalid source type %q", record.SourceType)
	}
	if err := record.Origin.validate(); err != nil {
		return err
	}
	if !safeRepositoryPath(record.RawPath) {
		return fmt.Errorf("unsafe raw source path %q", record.RawPath)
	}
	if !validFingerprint(record.RawFingerprint) {
		return fmt.Errorf("invalid raw source fingerprint %q", record.RawFingerprint)
	}
	return nil
}

func (record SourceRecord) validateDates() error {
	if record.ReceivedDate != nil {
		if _, err := ParseDate(string(*record.ReceivedDate)); err != nil {
			return err
		}
	}
	if _, err := ParseDate(string(record.IngestDate)); err != nil {
		return err
	}
	if record.ReceivedDate != nil && string(record.IngestDate) < string(*record.ReceivedDate) {
		return fmt.Errorf("ingest date must not precede received date")
	}
	return nil
}

func (record SourceRecord) validateReferences() error {
	if record.DerivedClaims == nil || record.DerivedPages == nil || record.ProvenanceNotes == nil {
		return fmt.Errorf("derived_claims, derived_pages, and provenance_notes must be JSON arrays")
	}
	for _, id := range record.DerivedClaims {
		if !knowledge.ValidSemanticID(id) {
			return fmt.Errorf("invalid derived claim ID %q", id)
		}
	}
	for _, page := range record.DerivedPages {
		if !safeWikiPage(page) {
			return fmt.Errorf("unsafe derived page reference %q", page)
		}
	}
	for _, note := range record.ProvenanceNotes {
		if strings.TrimSpace(note) == "" {
			return fmt.Errorf("provenance notes must not be blank")
		}
	}
	return nil
}

func (record SourceRecord) validateTimestamps() error {
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() || record.UpdatedAt.Before(record.CreatedAt) {
		return fmt.Errorf("created_at and updated_at must be valid ordered timestamps")
	}
	return nil
}

func (origin Origin) validate() error {
	switch origin.Kind {
	case OriginProject:
		if origin.Trust != TrustProject {
			return fmt.Errorf("project origin requires trusted-project classification")
		}
	case OriginOwner:
		if origin.Trust != TrustOwner && origin.Trust != TrustUnknown {
			return fmt.Errorf("owner origin requires trusted-owner or unknown classification")
		}
	case OriginExternal:
		if origin.Trust != TrustExternal && origin.Trust != TrustUnknown {
			return fmt.Errorf("external origin requires external or unknown classification")
		}
	case OriginUnknown:
		if origin.Trust != TrustUnknown {
			return fmt.Errorf("unknown origin requires unknown classification")
		}
	default:
		return fmt.Errorf("invalid source origin %q", origin.Kind)
	}
	return nil
}

func (record SourceRecord) validateLifecycle() error {
	switch record.Status {
	case StatusCurrent, StatusWithdrawn:
		if record.SupersededBy != "" {
			return fmt.Errorf("only superseded sources may name a successor")
		}
	case StatusSuperseded:
		if !ValidSourceID(record.SupersededBy) || record.SupersededBy == record.ID {
			return fmt.Errorf("superseded source requires a different valid successor ID")
		}
	default:
		return fmt.Errorf("invalid source status %q", record.Status)
	}
	return nil
}

func validSourceType(sourceType SourceType) bool {
	switch sourceType {
	case SourceTypeProjectDocument, SourceTypeDecision, SourceTypeRepositoryDocument,
		SourceTypeExternalDocument, SourceTypeOwnerInput, SourceTypeOther, SourceTypeUnknown:
		return true
	default:
		return false
	}
}

func validFingerprint(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "sha256:") {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func safeRepositoryPath(value string) bool {
	if value == "" || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") {
		return false
	}
	cleaned := path.Clean(value)
	return cleaned == value && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func safeWikiPage(value string) bool {
	return safeRepositoryPath(value) && strings.HasSuffix(value, ".md")
}
