package lint

import (
	"context"
	"errors"
	"sort"
	"strings"

	"scrinium/internal/provenance"
	"scrinium/internal/store"
)

type SourceReport struct {
	OK             bool           `json:"ok"`
	FilesChecked   int            `json:"files_checked"`
	SourcesChecked int            `json:"sources_checked"`
	Findings       []ClaimFinding `json:"findings"`
}

type SourceService struct {
	sources    *store.SourceStore
	repository *store.Store
}

func NewSourceService(sources *store.SourceStore, repository *store.Store) *SourceService {
	return &SourceService{sources: sources, repository: repository}
}

func (s *SourceService) Build(ctx context.Context) (SourceReport, error) {
	entries, err := s.sources.Inspect(ctx)
	if err != nil {
		return SourceReport{}, err
	}
	findings := make([]ClaimFinding, 0)
	sources := make(map[string]provenance.SourceRecord)
	paths := make(map[string][]string)
	for _, entry := range entries {
		if entry.Source != nil {
			paths[entry.Source.ID] = append(paths[entry.Source.ID], entry.Path)
			if _, exists := sources[entry.Source.ID]; !exists {
				sources[entry.Source.ID] = *entry.Source
			}
		}
		if entry.Err != nil {
			findings = append(findings, sourceFileFinding(entry.Path, entry.Err))
		}
	}
	for id, duplicatePaths := range paths {
		if len(duplicatePaths) > 1 {
			sort.Strings(duplicatePaths)
			findings = append(findings, deterministicFinding("high", strings.Join(duplicatePaths, ", "), "duplicate_source_id", "Source ID "+id+" appears in multiple files.", "Keep one canonical file matching the immutable source ID."))
		}
	}
	ids := make([]string, 0, len(sources))
	for id := range sources {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		record := sources[id]
		path, _ := store.SourcePath(id)
		if record.Status == provenance.StatusSuperseded {
			successor, exists := sources[record.SupersededBy]
			switch {
			case !exists:
				findings = append(findings, deterministicFinding("high", path, "broken_source_reference", "superseded_by references missing source "+record.SupersededBy+".", "Create or migrate the successor before superseding this source."))
			case successor.Status != provenance.StatusCurrent:
				findings = append(findings, deterministicFinding("high", path, "invalid_source_lifecycle_link", "superseded_by references a non-current source.", "Use a current successor source."))
			}
		}
		exists, fingerprint, fingerprintErr := s.repository.Fingerprint(ctx, record.RawPath)
		switch {
		case fingerprintErr != nil:
			findings = append(findings, deterministicFinding("high", path, "invalid_source_raw_file", fingerprintErr.Error(), "Restore a confined non-linked regular raw source file."))
		case !exists:
			findings = append(findings, deterministicFinding("high", path, "missing_source_raw_file", "Raw source "+record.RawPath+" is missing.", "Restore the exact bytes or withdraw the source."))
		case fingerprint != record.RawFingerprint:
			findings = append(findings, deterministicFinding("high", path, "changed_source_fingerprint", "Raw source bytes differ from the stored fingerprint.", "Review the changed bytes and use explicit source refresh."))
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Code < findings[j].Code
	})
	return SourceReport{OK: len(findings) == 0, FilesChecked: len(entries), SourcesChecked: len(sources), Findings: findings}, nil
}

func sourceFileFinding(path string, err error) ClaimFinding {
	code := "malformed_source_file"
	var sourceErr *store.SourceError
	if errors.As(err, &sourceErr) {
		switch sourceErr.Code {
		case "filename_id_mismatch", "duplicate_json_key", "invalid_source_json":
			code = sourceErr.Code
		}
	}
	return deterministicFinding("high", path, code, err.Error(), "Correct the canonical JSON explicitly; Scrinium will not silently repair source records.")
}
