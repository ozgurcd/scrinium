package app

import (
	"context"
	"errors"
	"os"
	"sort"
	"strings"
	"time"

	"scrinium/internal/provenance"
	"scrinium/internal/session"
	"scrinium/internal/store"
)

const sourceMigrationSchema = "scrinium.source-migration/v1"

type SourceMigrationCandidate struct {
	SourceID       string `json:"source_id"`
	RecordPath     string `json:"record_path"`
	SummaryPath    string `json:"summary_path"`
	RawPath        string `json:"raw_path"`
	RawFingerprint string `json:"raw_fingerprint"`
	AlreadyExists  bool   `json:"already_exists"`
}

type SourceMigrationDebt struct {
	SourceID string `json:"source_id,omitempty"`
	Path     string `json:"path"`
	Code     string `json:"code"`
	Message  string `json:"message"`
}

type SourceMigrationReport struct {
	SchemaVersion string                     `json:"schema_version"`
	Mode          string                     `json:"mode"`
	Candidates    []SourceMigrationCandidate `json:"candidates"`
	Created       []string                   `json:"created"`
	Existing      []string                   `json:"existing"`
	Debt          []SourceMigrationDebt      `json:"debt"`
}

type sourceMigrationEntry struct {
	record      provenance.SourceRecord
	summaryPath string
	existing    bool
}

func (s *Service) AssessSourceMigration(ctx context.Context) (SourceMigrationReport, error) {
	report, _, err := s.assessSourceMigration(ctx)
	return report, err
}

func (s *Service) ApplySourceMigration(ctx context.Context, sessionID string) (SourceMigrationReport, error) {
	report, entries, err := s.assessSourceMigration(ctx)
	if err != nil {
		return SourceMigrationReport{}, err
	}
	report.Mode = "apply"
	writes := make([]session.Write, 0, len(entries))
	for _, entry := range entries {
		path, _ := store.SourcePath(entry.record.ID)
		writes = append(writes, session.Write{Path: path, ExistedBefore: entry.existing})
	}
	if len(writes) == 0 {
		return report, nil
	}
	if err := s.sessions.DoWrite(ctx, sessionID, writes, func() (bool, error) {
		changed := false
		now := time.Now().UTC()
		for _, entry := range entries {
			if entry.existing {
				report.Existing = append(report.Existing, entry.record.ID)
				continue
			}
			record := entry.record
			record.CreatedAt = now
			record.UpdatedAt = now
			if _, createErr := s.sources.Create(ctx, record); createErr != nil {
				var sourceErr *store.SourceError
				if errors.As(createErr, &sourceErr) && sourceErr.Code == "source_exists" {
					report.Existing = append(report.Existing, record.ID)
					continue
				}
				return false, translateSourceError(createErr)
			}
			report.Created = append(report.Created, record.ID)
			changed = true
		}
		return changed, nil
	}); err != nil {
		return SourceMigrationReport{}, translateSessionError(err)
	}
	sort.Strings(report.Created)
	sort.Strings(report.Existing)
	return report, nil
}

func (s *Service) assessSourceMigration(ctx context.Context) (SourceMigrationReport, []sourceMigrationEntry, error) {
	report := SourceMigrationReport{SchemaVersion: sourceMigrationSchema, Mode: "dry_run", Candidates: []SourceMigrationCandidate{}, Created: []string{}, Existing: []string{}, Debt: []SourceMigrationDebt{}}
	registry, err := s.store.Read(ctx, "source-registry.md")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			report.Debt = append(report.Debt, SourceMigrationDebt{Path: "source-registry.md", Code: "registry_missing", Message: "legacy source registry is missing"})
			return report, nil, nil
		}
		return SourceMigrationReport{}, nil, storageError(err, "failed to read source-registry.md: %v", err)
	}
	legacy, debt := parseLegacySourceRegistry(string(registry))
	report.Debt = append(report.Debt, debt...)
	entries := make([]sourceMigrationEntry, 0, len(legacy))
	counts := make(map[string]int)
	for _, fields := range legacy {
		counts[fields["source_id"]]++
	}
	duplicateReported := make(map[string]bool)
	for _, fields := range legacy {
		id := fields["source_id"]
		if counts[id] > 1 {
			if !duplicateReported[id] {
				report.Debt = append(report.Debt, SourceMigrationDebt{SourceID: id, Path: "source-registry.md", Code: "ambiguous_legacy_record", Message: "source ID appears in multiple registry entries"})
				duplicateReported[id] = true
			}
			continue
		}
		if fields["__malformed"] == "true" {
			continue
		}
		entry, entryDebt := s.migrationEntry(ctx, fields)
		if entryDebt != nil {
			report.Debt = append(report.Debt, *entryDebt)
			continue
		}
		entries = append(entries, entry)
		path, _ := store.SourcePath(entry.record.ID)
		report.Candidates = append(report.Candidates, SourceMigrationCandidate{
			SourceID: entry.record.ID, RecordPath: path, SummaryPath: entry.summaryPath,
			RawPath: entry.record.RawPath, RawFingerprint: entry.record.RawFingerprint, AlreadyExists: entry.existing,
		})
	}
	sort.Slice(report.Candidates, func(i, j int) bool { return report.Candidates[i].SourceID < report.Candidates[j].SourceID })
	sort.Slice(report.Debt, func(i, j int) bool {
		if report.Debt[i].Path != report.Debt[j].Path {
			return report.Debt[i].Path < report.Debt[j].Path
		}
		if report.Debt[i].SourceID != report.Debt[j].SourceID {
			return report.Debt[i].SourceID < report.Debt[j].SourceID
		}
		return report.Debt[i].Code < report.Debt[j].Code
	})
	return report, entries, nil
}

func (s *Service) migrationEntry(ctx context.Context, fields map[string]string) (sourceMigrationEntry, *SourceMigrationDebt) {
	id := fields["source_id"]
	if !provenance.ValidSourceID(id) {
		return sourceMigrationEntry{}, migrationDebt(id, "source-registry.md", "invalid_source_id", "legacy source ID is invalid")
	}
	if entry, debt, handled := s.existingMigrationEntry(ctx, id, fields); handled {
		return entry, debt
	}
	summaryPath := fields["source_summary"]
	if summaryPath == "" {
		summaryPath = sourceSummaryPath(id)
	}
	if debt := s.validateLegacySummary(ctx, id, summaryPath, fields); debt != nil {
		return sourceMigrationEntry{}, debt
	}
	record, debt := s.legacySourceRecord(ctx, id, fields)
	if debt != nil {
		return sourceMigrationEntry{}, debt
	}
	return sourceMigrationEntry{record: record, summaryPath: summaryPath}, nil
}

func (s *Service) existingMigrationEntry(ctx context.Context, id string, fields map[string]string) (sourceMigrationEntry, *SourceMigrationDebt, bool) {
	stored, getErr := s.sources.Get(ctx, id)
	if getErr == nil {
		if rawPath := fields["raw_path"]; rawPath != "" && rawPath != stored.Source.RawPath {
			return sourceMigrationEntry{}, migrationDebt(id, "source-registry.md", "canonical_source_conflict", "registry raw path differs from canonical source metadata"), true
		}
		if fingerprint := fields["raw_fingerprint"]; fingerprint != "" && fingerprint != stored.Source.RawFingerprint {
			return sourceMigrationEntry{}, migrationDebt(id, "source-registry.md", "changed_raw_fingerprint", "registry fingerprint differs from canonical source metadata"), true
		}
		return sourceMigrationEntry{record: stored.Source, summaryPath: sourceSummaryPath(id), existing: true}, nil, true
	}
	var existingErr *store.SourceError
	if !errors.As(getErr, &existingErr) || existingErr.Code != "source_not_found" {
		return sourceMigrationEntry{}, migrationDebt(id, "source-registry.md", "invalid_source_record", getErr.Error()), true
	}
	return sourceMigrationEntry{}, nil, false
}

func (s *Service) validateLegacySummary(ctx context.Context, id, summaryPath string, fields map[string]string) *SourceMigrationDebt {
	if summaryPath != sourceSummaryPath(id) {
		return migrationDebt(id, "source-registry.md", "ambiguous_legacy_record", "source summary path does not match the immutable source ID")
	}
	summary, err := s.store.Read(ctx, summaryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return migrationDebt(id, summaryPath, "missing_summary", "legacy source summary is missing")
		}
		return migrationDebt(id, summaryPath, "malformed_summary", err.Error())
	}
	summaryFields := parseLegacySummaryMetadata(string(summary))
	for registryKey, summaryKey := range map[string]string{
		"source_id": "source_id", "raw_path": "original_path", "source_type": "source_type",
		"trust_level": "trust_level", "received_date": "received_date", "ingest_date": "ingest_date",
	} {
		if fields[registryKey] == "" || summaryFields[summaryKey] == "" || fields[registryKey] != summaryFields[summaryKey] {
			return migrationDebt(id, summaryPath, "ambiguous_legacy_record", "registry and summary metadata are incomplete or disagree for "+registryKey)
		}
	}
	return nil
}

func (s *Service) legacySourceRecord(ctx context.Context, id string, fields map[string]string) (provenance.SourceRecord, *SourceMigrationDebt) {
	sourceType, parseErr := parseSourceType(fields["source_type"])
	if parseErr != nil {
		return provenance.SourceRecord{}, migrationDebt(id, "source-registry.md", "unsupported_source_type", parseErr.Error())
	}
	trust, parseErr := parseTrust(fields["trust_level"])
	if parseErr != nil {
		return provenance.SourceRecord{}, migrationDebt(id, "source-registry.md", "invalid_trust", parseErr.Error())
	}
	origin, parseErr := parseOrigin(fields["origin"], trust)
	if parseErr != nil {
		return provenance.SourceRecord{}, migrationDebt(id, "source-registry.md", "invalid_origin", parseErr.Error())
	}
	received, parseErr := optionalDate(fields["received_date"])
	if parseErr != nil || received == nil {
		return provenance.SourceRecord{}, migrationDebt(id, "source-registry.md", "invalid_date", "received date is missing or invalid")
	}
	ingest, parseErr := provenance.ParseDate(fields["ingest_date"])
	if parseErr != nil {
		return provenance.SourceRecord{}, migrationDebt(id, "source-registry.md", "invalid_date", parseErr.Error())
	}
	status := provenance.Status(fields["status"])
	if status == "" {
		status = provenance.StatusCurrent
	}
	fingerprint, debt := s.migrationRawFingerprint(ctx, id, fields)
	if debt != nil {
		return provenance.SourceRecord{}, debt
	}
	record := provenance.SourceRecord{
		SchemaVersion: provenance.SchemaVersion, ID: id, Title: fields["title"], SourceType: sourceType,
		Origin: origin, RawPath: fields["raw_path"], RawFingerprint: fingerprint, ReceivedDate: received,
		IngestDate: ingest, Status: status, SupersededBy: fields["superseded_by"],
		DerivedClaims: splitLegacyList(fields["derived_claims"]), DerivedPages: splitLegacyList(fields["derived_pages"]),
		ProvenanceNotes: splitLegacyList(fields["provenance_notes"]), CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(1, 0).UTC(),
	}
	if note := fields["notes"]; note != "" {
		record.ProvenanceNotes = sortedUnique(append(record.ProvenanceNotes, note))
	}
	if err := record.Validate(); err != nil {
		return provenance.SourceRecord{}, migrationDebt(id, "source-registry.md", "malformed_legacy_record", err.Error())
	}
	return record, nil
}

func (s *Service) migrationRawFingerprint(ctx context.Context, id string, fields map[string]string) (string, *SourceMigrationDebt) {
	rawPath := fields["raw_path"]
	exists, fingerprint, err := s.repository.Fingerprint(ctx, rawPath)
	if err != nil {
		return "", migrationDebt(id, rawPath, "invalid_raw_source", err.Error())
	}
	if !exists {
		return "", migrationDebt(id, rawPath, "missing_raw_source", "raw source file is missing")
	}
	if expected := fields["raw_fingerprint"]; expected != "" && expected != fingerprint {
		return "", migrationDebt(id, rawPath, "changed_raw_fingerprint", "raw source bytes differ from the registry fingerprint")
	}
	return fingerprint, nil
}

func parseLegacySourceRegistry(content string) ([]map[string]string, []SourceMigrationDebt) {
	parser := legacyRegistryParser{entries: []map[string]string{}, debt: []SourceMigrationDebt{}}
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		parser.consume(line)
	}
	parser.flush()
	return parser.entries, parser.debt
}

type legacyRegistryParser struct {
	entries []map[string]string
	debt    []SourceMigrationDebt
	current map[string]string
	listKey string
}

func (parser *legacyRegistryParser) consume(line string) {
	if strings.HasPrefix(line, "### ") {
		parser.flush()
		parser.current = map[string]string{"source_id": strings.TrimSpace(strings.TrimPrefix(line, "### "))}
		parser.listKey = ""
		return
	}
	if parser.current == nil || strings.TrimSpace(line) == "" {
		return
	}
	if strings.HasPrefix(line, "  - ") && parser.listKey != "" {
		parser.consumeListValue(strings.TrimPrefix(line, "  - "))
		return
	}
	if !strings.HasPrefix(line, "- ") {
		parser.malformed("malformed_registry_entry", "unrecognized registry line: "+line)
		return
	}
	parser.consumeField(strings.TrimPrefix(line, "- "))
}

func (parser *legacyRegistryParser) consumeListValue(raw string) {
	value := trimLegacyValue(raw)
	if value != "" && value != "None" && value != "Pending review" {
		parser.current[parser.listKey] = appendLegacyValue(parser.current[parser.listKey], value)
	}
}

func (parser *legacyRegistryParser) consumeField(line string) {
	key, value, ok := strings.Cut(line, ":")
	if !ok {
		parser.malformed("malformed_registry_entry", "registry field lacks a colon")
		return
	}
	canonical, known := legacyRegistryKey(strings.TrimSpace(key))
	if !known {
		parser.malformed("malformed_registry_entry", "unknown registry field "+strings.TrimSpace(key))
		return
	}
	value = trimLegacyValue(value)
	if canonical == "derived_pages" || canonical == "derived_claims" || canonical == "provenance_notes" {
		parser.listKey = canonical
		parser.consumeListValue(value)
		return
	}
	parser.listKey = ""
	if _, duplicate := parser.current[canonical]; duplicate {
		parser.malformed("ambiguous_legacy_record", "duplicate registry field "+canonical)
		return
	}
	parser.current[canonical] = value
}

func (parser *legacyRegistryParser) malformed(code, message string) {
	parser.current["__malformed"] = "true"
	parser.debt = append(parser.debt, SourceMigrationDebt{SourceID: parser.current["source_id"], Path: "source-registry.md", Code: code, Message: message})
}

func (parser *legacyRegistryParser) flush() {
	if parser.current != nil {
		parser.entries = append(parser.entries, parser.current)
		parser.current = nil
	}
}

func parseLegacySummaryMetadata(content string) map[string]string {
	result := make(map[string]string)
	inMetadata := false
	for _, line := range strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n") {
		if line == "## Metadata" {
			inMetadata = true
			continue
		}
		if inMetadata && strings.HasPrefix(line, "## ") {
			break
		}
		if !inMetadata || !strings.HasPrefix(line, "- ") {
			continue
		}
		key, value, ok := strings.Cut(strings.TrimPrefix(line, "- "), ":")
		if !ok {
			continue
		}
		canonical := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(key), " ", "_"))
		result[canonical] = trimLegacyValue(value)
	}
	return result
}

func legacyRegistryKey(key string) (string, bool) {
	keys := map[string]string{
		"Title": "title", "Raw path": "raw_path", "Raw fingerprint": "raw_fingerprint",
		"Source summary": "source_summary", "Source type": "source_type", "Origin": "origin",
		"Trust level": "trust_level", "Trust classification": "trust_level", "Received date": "received_date",
		"Ingest date": "ingest_date", "Status": "status", "Superseded by": "superseded_by",
		"Derived claims": "derived_claims", "Derived pages": "derived_pages", "Notes": "notes",
		"Provenance notes": "provenance_notes",
	}
	value, ok := keys[key]
	return value, ok
}

func trimLegacyValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), "`")
}

func appendLegacyValue(existing, value string) string {
	if existing == "" {
		return value
	}
	return existing + "\n" + value
}

func splitLegacyList(value string) []string {
	if value == "" {
		return []string{}
	}
	return sortedUnique(strings.Split(value, "\n"))
}

func migrationDebt(id, path, code, message string) *SourceMigrationDebt {
	return &SourceMigrationDebt{SourceID: id, Path: path, Code: code, Message: message}
}
