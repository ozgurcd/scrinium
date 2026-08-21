package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"scrinium/internal/provenance"
	"scrinium/internal/session"
	"scrinium/internal/store"
)

type SourceRevision string

type SourceView struct {
	Source   provenance.SourceRecord
	Revision SourceRevision
}

type UpdateSourceRequest struct {
	SessionID        string
	SourceID         string
	ExpectedRevision SourceRevision
	Title            *string
	DerivedClaims    []string
	DerivedPages     []string
	ProvenanceNotes  []string
}

type RefreshSourceRequest struct {
	SessionID        string
	SourceID         string
	ExpectedRevision SourceRevision
}

type SupersedeSourceRequest struct {
	SessionID        string
	SourceID         string
	SuccessorID      string
	ExpectedRevision SourceRevision
}

type WithdrawSourceRequest struct {
	SessionID        string
	SourceID         string
	ExpectedRevision SourceRevision
}

type SourceConflictError struct {
	SourceID         string
	ExpectedRevision SourceRevision
	CurrentRevision  SourceRevision
	Cause            error
}

func (e *SourceConflictError) Error() string {
	return fmt.Sprintf("source %s changed since it was read: expected %s, current %s", e.SourceID, e.ExpectedRevision, e.CurrentRevision)
}

func (e *SourceConflictError) Unwrap() error { return e.Cause }

func (s *Service) RegisterSource(ctx context.Context, req RegisterSourceRequest) (string, error) {
	_, err := s.registerSourceRecord(ctx, req)
	if err != nil {
		return "", err
	}
	return sourceSummaryPath(req.SourceID), nil
}

func (s *Service) registerSourceRecord(ctx context.Context, req RegisterSourceRequest) (SourceView, error) {
	assessment, err := s.AssessSourceMigration(ctx)
	if err != nil {
		return SourceView{}, err
	}
	for _, candidate := range assessment.Candidates {
		if !candidate.AlreadyExists {
			return SourceView{}, appError(ErrorConflict, "legacy source metadata must be migrated explicitly before registering new sources", nil)
		}
	}
	for _, debt := range assessment.Debt {
		if debt.Code != "registry_missing" {
			return SourceView{}, appError(ErrorConflict, "legacy source migration debt must be resolved before regenerating source-registry.md: "+debt.Code, nil)
		}
	}
	now := time.Now().UTC()
	record, err := s.sourceRecordFromRequest(ctx, req, now)
	if err != nil {
		return SourceView{}, err
	}
	recordPath, _ := store.SourcePath(record.ID)
	summaryPath := sourceSummaryPath(record.ID)
	registryPath := "source-registry.md"
	for _, path := range []string{recordPath, summaryPath, registryPath} {
		if !s.governance.AllowsWrite(path) {
			return SourceView{}, protectedError(path)
		}
	}
	summaryExists, err := s.store.Exists(ctx, summaryPath)
	if err != nil {
		return SourceView{}, storageError(err, "failed to stat %s: %v", summaryPath, err)
	}
	if summaryExists {
		return SourceView{}, appError(ErrorConflict, fmt.Sprintf("source summary %s already exists; migrate the legacy record instead of overwriting it", summaryPath), nil)
	}
	registryExists, err := s.store.Exists(ctx, registryPath)
	if err != nil {
		return SourceView{}, storageError(err, "failed to stat %s: %v", registryPath, err)
	}
	writes := []session.Write{
		{Path: recordPath, ExistedBefore: false},
		{Path: summaryPath, ExistedBefore: false},
		{Path: registryPath, ExistedBefore: registryExists},
	}
	var created store.SourceRecord
	if err := s.sessions.DoWrite(ctx, req.SessionID, writes, func() (bool, error) {
		created, err = s.sources.Create(ctx, record)
		if err != nil {
			return false, translateSourceError(err)
		}
		if err := s.store.Write(ctx, summaryPath, []byte(sourceSummaryContent(record, fallback(req.Summary, "Summary pending."))), 0644); err != nil {
			return false, storageError(err, "canonical source was created but summary %s could not be written: %v", summaryPath, err)
		}
		if err := s.rebuildSourceRegistry(ctx); err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		return SourceView{}, translateSessionError(err)
	}
	return sourceView(created), nil
}

func (s *Service) GetSource(ctx context.Context, id string) (SourceView, error) {
	record, err := s.sources.Get(ctx, id)
	if err != nil {
		return SourceView{}, translateSourceError(err)
	}
	return sourceView(record), nil
}

func (s *Service) ListSources(ctx context.Context) ([]SourceView, error) {
	records, err := s.sources.List(ctx)
	if err != nil {
		return nil, translateSourceError(err)
	}
	views := make([]SourceView, 0, len(records))
	for _, record := range records {
		views = append(views, sourceView(record))
	}
	return views, nil
}

func (s *Service) UpdateSource(ctx context.Context, req UpdateSourceRequest) (SourceView, error) {
	if req.Title == nil && req.DerivedClaims == nil && req.DerivedPages == nil && req.ProvenanceNotes == nil {
		return SourceView{}, appError(ErrorInvalid, "source update requires at least one mutable field", nil)
	}
	return s.updateSource(ctx, req.SessionID, req.SourceID, req.ExpectedRevision, func(record *provenance.SourceRecord, now time.Time) error {
		changed := false
		if req.Title != nil {
			if record.Title != *req.Title {
				record.Title = *req.Title
				changed = true
			}
		}
		if req.DerivedClaims != nil {
			values := sortedUnique(req.DerivedClaims)
			if !equalStrings(record.DerivedClaims, values) {
				record.DerivedClaims = values
				changed = true
			}
		}
		if req.DerivedPages != nil {
			values := sortedUnique(req.DerivedPages)
			if !equalStrings(record.DerivedPages, values) {
				record.DerivedPages = values
				changed = true
			}
		}
		if req.ProvenanceNotes != nil {
			values := sortedUnique(req.ProvenanceNotes)
			if !equalStrings(record.ProvenanceNotes, values) {
				record.ProvenanceNotes = values
				changed = true
			}
		}
		if changed {
			record.UpdatedAt = now
		}
		return nil
	})
}

func (s *Service) RefreshSource(ctx context.Context, req RefreshSourceRequest) (SourceView, error) {
	return s.updateSource(ctx, req.SessionID, req.SourceID, req.ExpectedRevision, func(record *provenance.SourceRecord, now time.Time) error {
		exists, fingerprint, err := s.repository.Fingerprint(ctx, record.RawPath)
		if err != nil {
			return appError(ErrorIntegrity, fmt.Sprintf("cannot refresh raw source %s: %v", record.RawPath, err), err)
		}
		if !exists {
			return appError(ErrorIntegrity, fmt.Sprintf("cannot refresh missing raw source %s", record.RawPath), os.ErrNotExist)
		}
		if record.RawFingerprint != fingerprint {
			record.RawFingerprint = fingerprint
			record.UpdatedAt = now
		}
		return nil
	})
}

func (s *Service) SupersedeSource(ctx context.Context, req SupersedeSourceRequest) (SourceView, error) {
	if req.SourceID == req.SuccessorID || !provenance.ValidSourceID(req.SuccessorID) {
		return SourceView{}, appError(ErrorInvalid, "supersession requires a different valid successor source ID", nil)
	}
	successor, err := s.sources.Get(ctx, req.SuccessorID)
	if err != nil {
		return SourceView{}, translateSourceError(err)
	}
	if successor.Source.Status != provenance.StatusCurrent {
		return SourceView{}, appError(ErrorConflict, "source successor must be current", nil)
	}
	return s.updateSource(ctx, req.SessionID, req.SourceID, req.ExpectedRevision, func(record *provenance.SourceRecord, now time.Time) error {
		if record.Status != provenance.StatusCurrent {
			return appError(ErrorConflict, "only a current source can be superseded", nil)
		}
		record.Status = provenance.StatusSuperseded
		record.SupersededBy = req.SuccessorID
		record.UpdatedAt = now
		return nil
	})
}

func (s *Service) WithdrawSource(ctx context.Context, req WithdrawSourceRequest) (SourceView, error) {
	return s.updateSource(ctx, req.SessionID, req.SourceID, req.ExpectedRevision, func(record *provenance.SourceRecord, now time.Time) error {
		if record.Status != provenance.StatusCurrent {
			return appError(ErrorConflict, "only a current source can be withdrawn", nil)
		}
		record.Status = provenance.StatusWithdrawn
		record.SupersededBy = ""
		record.UpdatedAt = now
		return nil
	})
}

func (s *Service) updateSource(ctx context.Context, sessionID, id string, expected SourceRevision, mutate func(*provenance.SourceRecord, time.Time) error) (SourceView, error) {
	path, err := store.SourcePath(id)
	if err != nil {
		return SourceView{}, translateSourceError(err)
	}
	if !store.ValidRevision(store.Revision(expected)) {
		return SourceView{}, appError(ErrorInvalid, "expected source revision is required", nil)
	}
	if !s.governance.AllowsWrite(path) || !s.governance.AllowsWrite("source-registry.md") {
		return SourceView{}, protectedError(path)
	}
	var mutation store.SourceMutation
	writes := []session.Write{{Path: path, ExistedBefore: true}, {Path: "source-registry.md", ExistedBefore: true}}
	if err := s.sessions.DoWrite(ctx, sessionID, writes, func() (bool, error) {
		now := time.Now().UTC()
		mutation, err = s.sources.Update(ctx, id, store.Revision(expected), func(record *provenance.SourceRecord) error {
			return mutate(record, now)
		})
		if err != nil {
			return false, translateSourceError(err)
		}
		if !mutation.Changed {
			return false, nil
		}
		if err := s.rebuildSourceRegistry(ctx); err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		return SourceView{}, translateSessionError(err)
	}
	return sourceView(mutation.Record), nil
}

func (s *Service) RebuildSourceRegistry(ctx context.Context, sessionID string) error {
	existed, err := s.store.Exists(ctx, "source-registry.md")
	if err != nil {
		return storageError(err, "failed to stat source-registry.md: %v", err)
	}
	return translateSessionError(s.sessions.DoWrite(ctx, sessionID, []session.Write{{Path: "source-registry.md", ExistedBefore: existed}}, func() (bool, error) {
		before := []byte(nil)
		if existed {
			before, err = s.store.Read(ctx, "source-registry.md")
			if err != nil {
				return false, storageError(err, "failed to read source-registry.md: %v", err)
			}
		}
		records, err := s.sources.List(ctx)
		if err != nil {
			return false, translateSourceError(err)
		}
		after := []byte(RenderSourceRegistry(records))
		if string(before) == string(after) {
			// Rebuilding an already-current derived view still satisfies the
			// session's observed source-registry maintenance obligation.
			return true, nil
		}
		if err := s.store.Write(ctx, "source-registry.md", after, 0644); err != nil {
			return false, storageError(err, "failed to write source-registry.md: %v", err)
		}
		return true, nil
	}))
}

func (s *Service) rebuildSourceRegistry(ctx context.Context) error {
	records, err := s.sources.List(ctx)
	if err != nil {
		return translateSourceError(err)
	}
	if err := s.store.Write(ctx, "source-registry.md", []byte(RenderSourceRegistry(records)), 0644); err != nil {
		return storageError(err, "failed to write source-registry.md: %v", err)
	}
	return nil
}

func (s *Service) sourceRecordFromRequest(ctx context.Context, req RegisterSourceRequest, now time.Time) (provenance.SourceRecord, error) {
	if !provenance.ValidSourceID(req.SourceID) {
		return provenance.SourceRecord{}, appError(ErrorInvalid, fmt.Sprintf("invalid source_id %q: expected SRC-YYYYMMDD-slug", req.SourceID), nil)
	}
	if strings.TrimSpace(req.Title) == "" || strings.TrimSpace(req.RawPath) == "" {
		return provenance.SourceRecord{}, appError(ErrorInvalid, "source title and raw_path are required", nil)
	}
	sourceType, err := parseSourceType(fallback(req.SourceType, string(provenance.SourceTypeUnknown)))
	if err != nil {
		return provenance.SourceRecord{}, err
	}
	trust, err := parseTrust(fallback(req.TrustLevel, string(provenance.TrustUnknown)))
	if err != nil {
		return provenance.SourceRecord{}, err
	}
	origin, err := parseOrigin(req.Origin, trust)
	if err != nil {
		return provenance.SourceRecord{}, err
	}
	exists, fingerprint, err := s.repository.Fingerprint(ctx, req.RawPath)
	if err != nil {
		return provenance.SourceRecord{}, appError(ErrorIntegrity, fmt.Sprintf("raw source %s is not a confined regular file: %v", req.RawPath, err), err)
	}
	if !exists {
		return provenance.SourceRecord{}, appError(ErrorIntegrity, fmt.Sprintf("raw source %s does not exist", req.RawPath), os.ErrNotExist)
	}
	received, err := optionalDate(req.ReceivedDate)
	if err != nil {
		return provenance.SourceRecord{}, appError(ErrorInvalid, err.Error(), err)
	}
	ingest := provenance.Date(now.Format("2006-01-02"))
	if req.IngestDate != "" && req.IngestDate != "unknown" {
		ingest, err = provenance.ParseDate(req.IngestDate)
		if err != nil {
			return provenance.SourceRecord{}, appError(ErrorInvalid, err.Error(), err)
		}
	}
	record := provenance.SourceRecord{
		SchemaVersion: provenance.SchemaVersion, ID: req.SourceID, Title: strings.TrimSpace(req.Title),
		SourceType: sourceType, Origin: origin, RawPath: req.RawPath, RawFingerprint: fingerprint,
		ReceivedDate: received, IngestDate: ingest, Status: provenance.StatusCurrent,
		DerivedClaims: sortedUnique(req.DerivedClaims), DerivedPages: sortedUnique(req.DerivedPages),
		ProvenanceNotes: sortedUnique(req.ProvenanceNotes), CreatedAt: now, UpdatedAt: now,
	}
	if err := record.Validate(); err != nil {
		return provenance.SourceRecord{}, appError(ErrorInvalid, err.Error(), err)
	}
	return record, nil
}

func RenderSourceRegistry(records []store.SourceRecord) string {
	sorted := append([]store.SourceRecord(nil), records...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Source.ID < sorted[j].Source.ID })
	var builder strings.Builder
	builder.WriteString("# Source Registry\n\nThis compatibility view is generated from canonical records under `sources/records/`. Manual edits are not canonical.\n\n## Registry Rules\n\n- Every source has an immutable `SRC-YYYYMMDD-slug` ID.\n- Raw bytes remain repository-owned and are identified by a full SHA-256 fingerprint.\n- Source provenance does not establish the truth of claims derived from it.\n\n## Sources\n")
	for _, stored := range sorted {
		record := stored.Source
		builder.WriteString("\n### " + record.ID + "\n\n")
		builder.WriteString("- Title: " + record.Title + "\n")
		builder.WriteString("- Raw path: `" + record.RawPath + "`\n")
		builder.WriteString("- Raw fingerprint: `" + record.RawFingerprint + "`\n")
		builder.WriteString("- Source summary: `" + sourceSummaryPath(record.ID) + "`\n")
		builder.WriteString("- Source type: `" + string(record.SourceType) + "`\n")
		builder.WriteString("- Origin: `" + string(record.Origin.Kind) + "`\n")
		builder.WriteString("- Trust classification: `" + string(record.Origin.Trust) + "`\n")
		builder.WriteString("- Received date: " + renderDate(record.ReceivedDate) + "\n")
		builder.WriteString("- Ingest date: " + string(record.IngestDate) + "\n")
		builder.WriteString("- Status: `" + string(record.Status) + "`\n")
		if record.SupersededBy != "" {
			builder.WriteString("- Superseded by: `" + record.SupersededBy + "`\n")
		}
		builder.WriteString("- Derived claims:\n")
		writeRegistryList(&builder, record.DerivedClaims)
		builder.WriteString("- Derived pages:\n")
		writeRegistryList(&builder, record.DerivedPages)
		builder.WriteString("- Provenance notes:\n")
		writeRegistryList(&builder, record.ProvenanceNotes)
	}
	return builder.String()
}

func sourceSummaryContent(record provenance.SourceRecord, summary string) string {
	return fmt.Sprintf(`# %s

## Metadata

- Source ID: %s
- Canonical record: sources/records/%s.json
- Original path: %s
- Raw fingerprint: %s
- Source type: %s
- Origin: %s
- Received date: %s
- Ingest date: %s
- Trust level: %s

## Summary

%s

## Key Claims

- Pending extraction.

## Entities and Concepts

- Pending review.

## Contradictions or Updates

- Pending review.

## Derived Pages

- Pending review.
`, record.Title, record.ID, record.ID, record.RawPath, record.RawFingerprint, record.SourceType, record.Origin.Kind, renderDate(record.ReceivedDate), record.IngestDate, record.Origin.Trust, summary)
}

func sourceSummaryPath(id string) string { return "sources/" + id + ".md" }

func sourceView(record store.SourceRecord) SourceView {
	return SourceView{Source: record.Source, Revision: SourceRevision(record.Revision)}
}

func parseSourceType(value string) (provenance.SourceType, error) {
	legacy := map[string]provenance.SourceType{
		"project design document": provenance.SourceTypeProjectDocument,
		"owner note":              provenance.SourceTypeOwnerInput,
	}
	if sourceType, ok := legacy[value]; ok {
		return sourceType, nil
	}
	switch provenance.SourceType(value) {
	case provenance.SourceTypeProjectDocument, provenance.SourceTypeDecision, provenance.SourceTypeRepositoryDocument,
		provenance.SourceTypeExternalDocument, provenance.SourceTypeOwnerInput, provenance.SourceTypeOther, provenance.SourceTypeUnknown:
		return provenance.SourceType(value), nil
	default:
		return "", appError(ErrorInvalid, fmt.Sprintf("invalid source_type %q", value), nil)
	}
}

func parseTrust(value string) (provenance.TrustClassification, error) {
	trust := provenance.TrustClassification(value)
	switch trust {
	case provenance.TrustProject, provenance.TrustOwner, provenance.TrustExternal, provenance.TrustUnknown:
		return trust, nil
	default:
		return "", appError(ErrorInvalid, fmt.Sprintf("invalid trust_level %q", value), nil)
	}
}

func parseOrigin(value string, trust provenance.TrustClassification) (provenance.Origin, error) {
	if value == "" {
		switch trust {
		case provenance.TrustProject:
			value = string(provenance.OriginProject)
		case provenance.TrustOwner:
			value = string(provenance.OriginOwner)
		case provenance.TrustExternal:
			value = string(provenance.OriginExternal)
		default:
			value = string(provenance.OriginUnknown)
		}
	}
	origin := provenance.Origin{Kind: provenance.OriginKind(value), Trust: trust}
	valid := (origin.Kind == provenance.OriginProject && trust == provenance.TrustProject) ||
		(origin.Kind == provenance.OriginOwner && (trust == provenance.TrustOwner || trust == provenance.TrustUnknown)) ||
		(origin.Kind == provenance.OriginExternal && (trust == provenance.TrustExternal || trust == provenance.TrustUnknown)) ||
		(origin.Kind == provenance.OriginUnknown && trust == provenance.TrustUnknown)
	if !valid {
		return provenance.Origin{}, appError(ErrorInvalid, fmt.Sprintf("invalid origin/trust combination %q/%q", origin.Kind, trust), nil)
	}
	return origin, nil
}

func optionalDate(value string) (*provenance.Date, error) {
	if value == "" || value == "unknown" {
		return nil, nil
	}
	date, err := provenance.ParseDate(value)
	if err != nil {
		return nil, err
	}
	return &date, nil
}

func sortedUnique(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func renderDate(value *provenance.Date) string {
	if value == nil {
		return "unknown"
	}
	return string(*value)
}

func writeRegistryList(builder *strings.Builder, values []string) {
	if len(values) == 0 {
		builder.WriteString("  - None\n")
		return
	}
	for _, value := range values {
		builder.WriteString("  - `" + value + "`\n")
	}
}

func translateSourceError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return err
	}
	var conflict *store.SourceRevisionConflictError
	if errors.As(err, &conflict) {
		return &SourceConflictError{SourceID: conflict.SourceID, ExpectedRevision: SourceRevision(conflict.Expected), CurrentRevision: SourceRevision(conflict.Current), Cause: err}
	}
	var sourceErr *store.SourceError
	if errors.As(err, &sourceErr) {
		switch sourceErr.Code {
		case "source_exists", "immutable_source_id":
			return appError(ErrorConflict, err.Error(), err)
		case "invalid_id", "invalid_revision":
			return appError(ErrorInvalid, err.Error(), err)
		case "source_not_found":
			return appError(ErrorNotFound, err.Error(), err)
		case "malformed_source_file", "filename_id_mismatch", "duplicate_json_key", "invalid_source_json":
			return appError(ErrorIntegrity, err.Error(), err)
		default:
			return appError(ErrorStorage, err.Error(), err)
		}
	}
	return appError(ErrorIntegrity, err.Error(), err)
}
