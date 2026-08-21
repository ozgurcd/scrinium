package store

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"scrinium/internal/provenance"
)

const sourcesDirectory = "sources/records"

type SourceError struct {
	Code string
	Path string
	Err  error
}

func (e *SourceError) Error() string {
	if e.Path == "" {
		return e.Err.Error()
	}
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

func (e *SourceError) Unwrap() error { return e.Err }

func sourceError(code, path string, err error) error {
	return &SourceError{Code: code, Path: path, Err: err}
}

type SourceRecord struct {
	Source   provenance.SourceRecord
	Revision Revision
}

type SourceMutation struct {
	Record  SourceRecord
	Changed bool
}

type SourceFile struct {
	Path     string
	Source   *provenance.SourceRecord
	Revision Revision
	Err      error
}

type SourceRevisionConflictError struct {
	SourceID string
	Expected Revision
	Current  Revision
}

func (e *SourceRevisionConflictError) Error() string {
	return fmt.Sprintf("source %s revision conflict: expected %s, current %s", e.SourceID, e.Expected, e.Current)
}

type SourceStore struct {
	files *Store
}

func NewSourceStore(files *Store) *SourceStore {
	return &SourceStore{files: files}
}

func SourcePath(id string) (string, error) {
	if !provenance.ValidSourceID(id) {
		return "", sourceError("invalid_id", "", fmt.Errorf("invalid source ID %q", id))
	}
	return filepath.ToSlash(filepath.Join(sourcesDirectory, id+".json")), nil
}

func EncodeSource(record provenance.SourceRecord) ([]byte, error) {
	if err := record.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode source: %w", err)
	}
	return append(data, '\n'), nil
}

func DecodeSource(data []byte) (provenance.SourceRecord, error) {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return provenance.SourceRecord{}, sourceError("duplicate_json_key", "", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record provenance.SourceRecord
	if err := decoder.Decode(&record); err != nil {
		return provenance.SourceRecord{}, sourceError("invalid_source_json", "", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return provenance.SourceRecord{}, sourceError("invalid_source_json", "", err)
	}
	if err := record.Validate(); err != nil {
		return provenance.SourceRecord{}, err
	}
	return record, nil
}

func (s *SourceStore) Create(ctx context.Context, source provenance.SourceRecord) (SourceRecord, error) {
	if err := ctx.Err(); err != nil {
		return SourceRecord{}, err
	}
	data, err := EncodeSource(source)
	if err != nil {
		return SourceRecord{}, err
	}
	path, err := SourcePath(source.ID)
	if err != nil {
		return SourceRecord{}, err
	}
	lock, err := s.lockSource(ctx, source.ID)
	if err != nil {
		return SourceRecord{}, err
	}
	defer lock.release()
	fullPath, exists, err := s.sourceFilePath(path, true)
	if err != nil {
		return SourceRecord{}, err
	}
	if exists {
		return SourceRecord{}, sourceError("source_exists", path, fmt.Errorf("source %s already exists", source.ID))
	}
	if err := atomicCreateFile(fullPath, data, 0644); err != nil {
		if errors.Is(err, os.ErrExist) {
			return SourceRecord{}, sourceError("source_exists", path, fmt.Errorf("source %s already exists", source.ID))
		}
		return SourceRecord{}, sourceError("source_write_failed", path, err)
	}
	return SourceRecord{Source: source, Revision: revisionFor(data)}, nil
}

func (s *SourceStore) Get(ctx context.Context, id string) (SourceRecord, error) {
	path, err := SourcePath(id)
	if err != nil {
		return SourceRecord{}, err
	}
	fullPath, exists, err := s.sourceFilePath(path, false)
	if err != nil {
		return SourceRecord{}, err
	}
	if !exists {
		return SourceRecord{}, sourceError("source_not_found", path, os.ErrNotExist)
	}
	data, err := readRegularFile(fullPath)
	if err != nil {
		return SourceRecord{}, sourceError("source_read_failed", path, err)
	}
	record, err := DecodeSource(data)
	if err != nil {
		return SourceRecord{}, sourceError("malformed_source_file", path, err)
	}
	if record.ID != id {
		return SourceRecord{}, sourceError("filename_id_mismatch", path, fmt.Errorf("filename ID %s does not match source ID %s", id, record.ID))
	}
	return SourceRecord{Source: record, Revision: revisionFor(data)}, nil
}

func (s *SourceStore) Update(ctx context.Context, id string, expected Revision, mutate func(*provenance.SourceRecord) error) (SourceMutation, error) {
	if !validRevision(expected) {
		return SourceMutation{}, sourceError("invalid_revision", "", fmt.Errorf("expected source revision is required"))
	}
	lock, err := s.lockSource(ctx, id)
	if err != nil {
		return SourceMutation{}, err
	}
	defer lock.release()
	record, err := s.Get(ctx, id)
	if err != nil {
		return SourceMutation{}, err
	}
	if record.Revision != expected {
		return SourceMutation{}, &SourceRevisionConflictError{SourceID: id, Expected: expected, Current: record.Revision}
	}
	updated := record.Source
	if err := mutate(&updated); err != nil {
		return SourceMutation{}, err
	}
	if updated.ID != id {
		return SourceMutation{}, sourceError("immutable_source_id", "id", fmt.Errorf("source IDs are immutable"))
	}
	data, err := EncodeSource(updated)
	if err != nil {
		return SourceMutation{}, err
	}
	path, _ := SourcePath(id)
	fullPath, exists, err := s.sourceFilePath(path, false)
	if err != nil {
		return SourceMutation{}, err
	}
	if !exists {
		return SourceMutation{}, sourceError("source_not_found", path, os.ErrNotExist)
	}
	current, err := readRegularFile(fullPath)
	if err != nil {
		return SourceMutation{}, sourceError("source_read_failed", path, err)
	}
	currentRevision := revisionFor(current)
	if currentRevision != expected {
		return SourceMutation{}, &SourceRevisionConflictError{SourceID: id, Expected: expected, Current: currentRevision}
	}
	if bytes.Equal(current, data) {
		return SourceMutation{Record: record, Changed: false}, nil
	}
	if err := atomicWriteFile(fullPath, data, 0644); err != nil {
		return SourceMutation{}, sourceError("source_write_failed", path, err)
	}
	return SourceMutation{Record: SourceRecord{Source: updated, Revision: revisionFor(data)}, Changed: true}, nil
}

func (s *SourceStore) List(ctx context.Context) ([]SourceRecord, error) {
	entries, err := s.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	records := make([]SourceRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.Err != nil {
			return nil, entry.Err
		}
		records = append(records, SourceRecord{Source: *entry.Source, Revision: entry.Revision})
	}
	return records, nil
}

func (s *SourceStore) Inspect(ctx context.Context) ([]SourceFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, exists, err := s.sourceDirectory(false)
	if err != nil || !exists {
		return []SourceFile{}, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, sourceError("sources_read_failed", sourcesDirectory, err)
	}
	result := make([]SourceFile, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".scrinium-") && strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		path := filepath.ToSlash(filepath.Join(sourcesDirectory, entry.Name()))
		item := SourceFile{Path: path}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			item.Err = sourceError("malformed_source_file", path, fmt.Errorf("source entries must be non-linked regular .json files"))
			result = append(result, item)
			continue
		}
		data, readErr := readRegularFile(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			item.Err = sourceError("source_read_failed", path, readErr)
			result = append(result, item)
			continue
		}
		record, decodeErr := DecodeSource(data)
		if decodeErr != nil {
			item.Err = sourceError("malformed_source_file", path, decodeErr)
			result = append(result, item)
			continue
		}
		item.Source = &record
		item.Revision = revisionFor(data)
		filenameID := strings.TrimSuffix(entry.Name(), ".json")
		if filenameID != record.ID {
			item.Err = sourceError("filename_id_mismatch", path, fmt.Errorf("filename ID %s does not match source ID %s", filenameID, record.ID))
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func (s *SourceStore) lockSource(ctx context.Context, id string) (*claimLock, error) {
	if !provenance.ValidSourceID(id) {
		return nil, sourceError("invalid_id", "", fmt.Errorf("invalid source ID %q", id))
	}
	directory := s.files.Root()
	for _, component := range []string{".scrinium", "locks", "sources"} {
		directory = filepath.Join(directory, component)
		if err := ensurePrivateDirectory(directory); err != nil {
			return nil, sourceError("source_lock_failed", filepath.ToSlash(directory), err)
		}
	}
	path := filepath.Join(directory, id+".lock")
	lock, err := lockRegularFile(ctx, path)
	if err != nil {
		return nil, sourceError("source_lock_failed", filepath.ToSlash(path), err)
	}
	return lock, nil
}

func (s *SourceStore) sourceDirectory(create bool) (string, bool, error) {
	current := s.files.Root()
	for _, component := range []string{"sources", "records"} {
		path := filepath.Join(current, component)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) && create {
			if err := os.Mkdir(path, 0755); err != nil && !errors.Is(err, os.ErrExist) {
				return "", false, sourceError("sources_directory_failed", filepath.ToSlash(path), err)
			}
			info, err = os.Lstat(path)
		}
		if os.IsNotExist(err) {
			return path, false, nil
		}
		if err != nil {
			return "", false, sourceError("sources_directory_failed", filepath.ToSlash(path), err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", false, sourceError("sources_directory_failed", filepath.ToSlash(path), fmt.Errorf("source directory components must be non-linked directories"))
		}
		current = path
	}
	return current, true, nil
}

func (s *SourceStore) sourceFilePath(path string, createDirectory bool) (string, bool, error) {
	_, _, err := s.sourceDirectory(createDirectory)
	if err != nil {
		return "", false, err
	}
	fullPath := filepath.Join(s.files.Root(), filepath.FromSlash(path))
	info, err := os.Lstat(fullPath)
	if os.IsNotExist(err) {
		return fullPath, false, nil
	}
	if err != nil {
		return "", false, sourceError("source_stat_failed", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false, sourceError("malformed_source_file", path, fmt.Errorf("source record must be a non-linked regular file"))
	}
	return fullPath, true, nil
}
