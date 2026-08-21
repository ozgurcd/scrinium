// Package session owns durable tracked work-session checklists.
package session

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"scrinium/internal/store"
)

const (
	SchemaVersion = "scrinium.session.v1"
	IDPrefix      = "ses_"
)

type Lifecycle string

const (
	Active    Lifecycle = "active"
	Finished  Lifecycle = "finished"
	Abandoned Lifecycle = "abandoned"
)

type ErrorCode string

const (
	ErrorInvalidID          ErrorCode = "invalid_session_id"
	ErrorNotFound           ErrorCode = "session_not_found"
	ErrorRepositoryMismatch ErrorCode = "repository_mismatch"
	ErrorCorrupt            ErrorCode = "corrupt_session"
	ErrorClosed             ErrorCode = "session_closed"
	ErrorPrerequisite       ErrorCode = "session_prerequisite"
	ErrorMaintenance        ErrorCode = "pending_maintenance"
	ErrorStorage            ErrorCode = "session_storage"
)

type Error struct {
	Code      ErrorCode
	SessionID string
	Cause     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.SessionID == "" {
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	}
	return fmt.Sprintf("%s %s: %v", e.Code, e.SessionID, e.Cause)
}

func (e *Error) Unwrap() error { return e.Cause }

type Maintenance struct {
	Log            bool `json:"log"`
	Index          bool `json:"index"`
	SourceRegistry bool `json:"source_registry"`
}

type Record struct {
	SchemaVersion      string      `json:"schema_version"`
	ID                 string      `json:"id"`
	RepositoryIdentity string      `json:"repository_identity"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
	Status             Lifecycle   `json:"status"`
	PagesRead          []string    `json:"pages_read"`
	DocumentsWritten   []string    `json:"documents_written"`
	ClaimsWritten      []string    `json:"claims_written"`
	NewResources       []string    `json:"new_resources"`
	Pending            Maintenance `json:"pending_maintenance"`
	AbandonReason      string      `json:"abandon_reason,omitempty"`
}

// Status retains compatibility fields while exposing the durable model.
type Status struct {
	SessionID            string    `json:"session_id"`
	RepositoryIdentity   string    `json:"repository_identity"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	Status               Lifecycle `json:"status"`
	Active               bool      `json:"active"`
	PagesRead            []string  `json:"pages_read"`
	PagesWritten         []string  `json:"pages_written"`
	DocumentsWritten     []string  `json:"documents_written"`
	ClaimsWritten        []string  `json:"claims_written"`
	NewPages             []string  `json:"new_pages"`
	NewResources         []string  `json:"new_resources"`
	MissingRequiredReads []string  `json:"missing_required_reads"`
	NeedsLog             bool      `json:"needs_log"`
	NeedsIndex           bool      `json:"needs_index"`
	NeedsSourceRegistry  bool      `json:"needs_source_registry"`
	AbandonReason        string    `json:"abandon_reason,omitempty"`
	ObservedOperations   bool      `json:"observed_operations_only"`
}

type Write struct {
	Path          string
	ExistedBefore bool
	Claim         bool
}

type Service struct {
	records            *store.RuntimeRecords
	repositoryIdentity string
	now                func() time.Time
}

func New(files *store.Store, repositoryRoot string) (*Service, error) {
	records, err := store.NewRuntimeRecords(files, "sessions")
	if err != nil {
		return nil, err
	}
	identity, err := repositoryIdentity(repositoryRoot)
	if err != nil {
		return nil, err
	}
	return &Service{records: records, repositoryIdentity: identity, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Service) Begin(ctx context.Context) (Status, error) {
	for attempts := 0; attempts < 8; attempts++ {
		id, err := newID()
		if err != nil {
			return Status{}, sessionError(ErrorStorage, "", err)
		}
		now := s.now().UTC()
		record := Record{
			SchemaVersion: SchemaVersion, ID: id, RepositoryIdentity: s.repositoryIdentity,
			CreatedAt: now, UpdatedAt: now, Status: Active,
			PagesRead: []string{}, DocumentsWritten: []string{}, ClaimsWritten: []string{}, NewResources: []string{},
		}
		data, err := encode(record)
		if err != nil {
			return Status{}, sessionError(ErrorCorrupt, id, err)
		}
		if err := s.records.Create(ctx, id, data); err != nil {
			if errors.Is(err, os.ErrExist) {
				continue
			}
			return Status{}, translateStorageError(id, err)
		}
		return view(record), nil
	}
	return Status{}, sessionError(ErrorStorage, "", fmt.Errorf("could not allocate a unique session ID"))
}

func (s *Service) Get(ctx context.Context, id string) (Status, error) {
	record, err := s.read(ctx, id)
	if err != nil {
		return Status{}, err
	}
	return view(record), nil
}

func (s *Service) Continue(ctx context.Context, id string) (Status, error) {
	record, err := s.read(ctx, id)
	if err != nil {
		return Status{}, err
	}
	if record.Status != Active {
		return Status{}, sessionError(ErrorClosed, id, fmt.Errorf("session is %s", record.Status))
	}
	return view(record), nil
}

func (s *Service) ListActive(ctx context.Context) ([]Status, error) {
	items, err := s.records.List(ctx)
	if err != nil {
		return nil, translateStorageError("", err)
	}
	result := make([]Status, 0, len(items))
	for id, data := range items {
		record, decodeErr := decode(data)
		if decodeErr != nil {
			return nil, sessionError(ErrorCorrupt, id, decodeErr)
		}
		if err := s.validateRepository(record); err != nil {
			return nil, err
		}
		if record.Status == Active {
			result = append(result, view(record))
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CreatedAt.Equal(result[j].CreatedAt) {
			return result[i].SessionID < result[j].SessionID
		}
		return result[i].CreatedAt.Before(result[j].CreatedAt)
	})
	return result, nil
}

func (s *Service) RecordRead(ctx context.Context, id, path string) error {
	if strings.TrimSpace(id) == "" {
		return nil
	}
	_, err := s.update(ctx, id, func(record *Record) error {
		if err := requireActive(*record); err != nil {
			return err
		}
		record.PagesRead = addSorted(record.PagesRead, normalizePath(path))
		return nil
	})
	return err
}

func (s *Service) RequireReadyForWrite(ctx context.Context, id, path string) error {
	if strings.TrimSpace(id) == "" {
		return sessionError(ErrorPrerequisite, "", fmt.Errorf("write rejected: no active durable session. Call begin_session and pass its session ID, then read index.md and agent-rules.md before writing"))
	}
	record, err := s.read(ctx, id)
	if err != nil {
		return err
	}
	if err := requireActive(record); err != nil {
		return err
	}
	return requireReads(record, path)
}

// DoWrite holds the session lock only around a short application mutation.
func (s *Service) DoWrite(ctx context.Context, id string, writes []Write, action func() (bool, error)) error {
	if strings.TrimSpace(id) == "" {
		return sessionError(ErrorPrerequisite, "", fmt.Errorf("write rejected: no active durable session. Call begin_session and pass its session ID, then read index.md and agent-rules.md before writing"))
	}
	_, err := s.update(ctx, id, func(record *Record) error {
		if err := requireActive(*record); err != nil {
			return err
		}
		for _, write := range writes {
			if err := requireReads(*record, write.Path); err != nil {
				return err
			}
		}
		changed, err := action()
		if err != nil {
			return err
		}
		if !changed {
			return nil
		}
		for _, write := range writes {
			recordWrite(record, write)
		}
		return nil
	})
	return err
}

func (s *Service) Finish(ctx context.Context, id string) (Status, error) {
	return s.update(ctx, id, func(record *Record) error {
		if err := requireActive(*record); err != nil {
			return err
		}
		if missing := missingRequiredReads(*record, ""); len(missing) > 0 {
			return sessionError(ErrorPrerequisite, record.ID, fmt.Errorf("cannot finish session: missing required reads: %s", strings.Join(missing, ", ")))
		}
		pending := pendingDescriptions(record.Pending)
		if len(pending) > 0 {
			return sessionError(ErrorMaintenance, record.ID, fmt.Errorf("cannot finish session: pending LLM Wiki maintenance: %s", strings.Join(pending, "; ")))
		}
		record.Status = Finished
		return nil
	})
}

func (s *Service) Abandon(ctx context.Context, id, reason string) (Status, error) {
	if strings.TrimSpace(reason) == "" {
		return Status{}, sessionError(ErrorPrerequisite, id, fmt.Errorf("abandonment reason is required"))
	}
	return s.update(ctx, id, func(record *Record) error {
		if err := requireActive(*record); err != nil {
			return err
		}
		record.Status = Abandoned
		record.AbandonReason = strings.TrimSpace(reason)
		return nil
	})
}

func (s *Service) update(ctx context.Context, id string, mutate func(*Record) error) (Status, error) {
	if !ValidID(id) {
		return Status{}, sessionError(ErrorInvalidID, id, fmt.Errorf("invalid session ID"))
	}
	mutation, err := s.records.Update(ctx, id, func(data []byte) ([]byte, error) {
		record, err := decode(data)
		if err != nil {
			return nil, sessionError(ErrorCorrupt, id, err)
		}
		if record.ID != id {
			return nil, sessionError(ErrorCorrupt, id, fmt.Errorf("filename/session ID mismatch"))
		}
		if err := s.validateRepository(record); err != nil {
			return nil, err
		}
		before := record
		if err := mutate(&record); err != nil {
			return nil, err
		}
		if recordsEqual(before, record) {
			return data, nil
		}
		record.UpdatedAt = s.now().UTC()
		return encode(record)
	})
	if err != nil {
		return Status{}, translateStorageError(id, err)
	}
	record, err := decode(mutation.Data)
	if err != nil {
		return Status{}, sessionError(ErrorCorrupt, id, err)
	}
	return view(record), nil
}

func (s *Service) read(ctx context.Context, id string) (Record, error) {
	if !ValidID(id) {
		return Record{}, sessionError(ErrorInvalidID, id, fmt.Errorf("invalid session ID"))
	}
	data, err := s.records.Read(ctx, id)
	if err != nil {
		return Record{}, translateStorageError(id, err)
	}
	record, err := decode(data)
	if err != nil {
		return Record{}, sessionError(ErrorCorrupt, id, err)
	}
	if record.ID != id {
		return Record{}, sessionError(ErrorCorrupt, id, fmt.Errorf("filename/session ID mismatch"))
	}
	if err := s.validateRepository(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func (s *Service) validateRepository(record Record) error {
	if record.RepositoryIdentity != s.repositoryIdentity {
		return sessionError(ErrorRepositoryMismatch, record.ID, fmt.Errorf("session belongs to a different repository"))
	}
	return nil
}

func ValidID(id string) bool {
	if len(id) != len(IDPrefix)+32 || !strings.HasPrefix(id, IDPrefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(id, IDPrefix))
	return err == nil
}

func newID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return IDPrefix + hex.EncodeToString(value[:]), nil
}

func repositoryIdentity(root string) (string, error) {
	real, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository identity: %w", err)
	}
	abs, err := filepath.Abs(real)
	if err != nil {
		return "", fmt.Errorf("resolve repository identity: %w", err)
	}
	digest := sha256.Sum256([]byte(filepath.Clean(abs)))
	return fmt.Sprintf("sha256:%x", digest), nil
}

func encode(record Record) ([]byte, error) {
	if err := validateRecord(record); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func decode(data []byte) (Record, error) {
	if err := rejectDuplicateKeys(data); err != nil {
		return Record{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var record Record
	if err := decoder.Decode(&record); err != nil {
		return Record{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Record{}, fmt.Errorf("multiple JSON values")
		}
		return Record{}, err
	}
	if err := validateRecord(record); err != nil {
		return Record{}, err
	}
	return record, nil
}

func validateRecord(record Record) error {
	if record.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported session schema %q", record.SchemaVersion)
	}
	if !ValidID(record.ID) {
		return fmt.Errorf("invalid session ID")
	}
	if !strings.HasPrefix(record.RepositoryIdentity, "sha256:") || len(record.RepositoryIdentity) != 71 {
		return fmt.Errorf("invalid repository identity")
	}
	if record.CreatedAt.IsZero() || record.UpdatedAt.IsZero() ||
		record.CreatedAt.Location() != time.UTC || record.UpdatedAt.Location() != time.UTC {
		return fmt.Errorf("session timestamps must be non-zero UTC values")
	}
	if record.UpdatedAt.Before(record.CreatedAt) {
		return fmt.Errorf("updated_at precedes created_at")
	}
	if record.Status != Active && record.Status != Finished && record.Status != Abandoned {
		return fmt.Errorf("invalid session status %q", record.Status)
	}
	if record.Status == Abandoned && strings.TrimSpace(record.AbandonReason) == "" {
		return fmt.Errorf("abandoned session requires a reason")
	}
	if record.Status != Abandoned && record.AbandonReason != "" {
		return fmt.Errorf("abandon reason is only valid for abandoned sessions")
	}
	for _, paths := range [][]string{record.PagesRead, record.DocumentsWritten, record.ClaimsWritten, record.NewResources} {
		if err := validatePaths(paths); err != nil {
			return err
		}
	}
	return nil
}

func rejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]bool)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("duplicate JSON key %q", key)
			}
			seen[key] = true
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	_, err = decoder.Token()
	return err
}

func validatePaths(paths []string) error {
	previous := ""
	for _, path := range paths {
		if path == "" || path != normalizePath(path) || strings.HasPrefix(path, "../") || filepath.IsAbs(path) {
			return fmt.Errorf("invalid tracked resource path %q", path)
		}
		if previous != "" && path <= previous {
			return fmt.Errorf("tracked resource paths must be unique and sorted")
		}
		previous = path
	}
	return nil
}

func recordsEqual(left, right Record) bool {
	left.UpdatedAt = time.Time{}
	right.UpdatedAt = time.Time{}
	leftJSON, _ := json.Marshal(left)
	rightJSON, _ := json.Marshal(right)
	return bytes.Equal(leftJSON, rightJSON)
}

func requireActive(record Record) error {
	if record.Status != Active {
		return sessionError(ErrorClosed, record.ID, fmt.Errorf("session is %s", record.Status))
	}
	return nil
}

func requireReads(record Record, path string) error {
	missing := missingRequiredReads(record, path)
	if len(missing) == 0 {
		return nil
	}
	return sessionError(ErrorPrerequisite, record.ID, fmt.Errorf("write rejected: missing required wiki reads before writing %s: %s", normalizePath(path), strings.Join(missing, ", ")))
}

func missingRequiredReads(record Record, path string) []string {
	required := []string{"index.md", "agent-rules.md"}
	cleanPath := normalizePath(path)
	switch {
	case cleanPath == "source-registry.md" || strings.HasPrefix(cleanPath, "sources/"):
		required = append(required, "workflows/ingest.md")
	case strings.HasPrefix(cleanPath, "syntheses/"):
		required = append(required, "workflows/query.md")
	case strings.Contains(cleanPath, "lint"):
		required = append(required, "workflows/lint.md")
	}
	read := make(map[string]bool, len(record.PagesRead))
	for _, item := range record.PagesRead {
		read[item] = true
	}
	missing := make([]string, 0, len(required))
	for _, item := range required {
		if !read[item] {
			missing = append(missing, item)
		}
	}
	return missing
}

func recordWrite(record *Record, write Write) {
	path := normalizePath(write.Path)
	if write.Claim || strings.HasPrefix(path, "claims/") {
		record.ClaimsWritten = addSorted(record.ClaimsWritten, path)
	} else {
		record.DocumentsWritten = addSorted(record.DocumentsWritten, path)
	}
	if path == "log.md" {
		record.Pending.Log = false
		return
	}
	record.Pending.Log = true
	if path == "index.md" {
		record.Pending.Index = false
	}
	if path == "source-registry.md" {
		record.Pending.SourceRegistry = false
	}
	if !write.ExistedBefore && path != "index.md" {
		record.NewResources = addSorted(record.NewResources, path)
		record.Pending.Index = true
	}
	if strings.HasPrefix(path, "sources/") {
		record.Pending.SourceRegistry = true
	}
}

func view(record Record) Status {
	missing := []string{}
	if record.Status == Active {
		missing = missingRequiredReads(record, "")
	}
	return Status{
		SessionID: record.ID, RepositoryIdentity: record.RepositoryIdentity,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, Status: record.Status, Active: record.Status == Active,
		PagesRead: cloneStrings(record.PagesRead), PagesWritten: mergeSorted(record.DocumentsWritten, record.ClaimsWritten),
		DocumentsWritten: cloneStrings(record.DocumentsWritten), ClaimsWritten: cloneStrings(record.ClaimsWritten),
		NewPages: cloneStrings(record.NewResources), NewResources: cloneStrings(record.NewResources),
		MissingRequiredReads: missing, NeedsLog: record.Pending.Log, NeedsIndex: record.Pending.Index,
		NeedsSourceRegistry: record.Pending.SourceRegistry, AbandonReason: record.AbandonReason, ObservedOperations: true,
	}
}

func pendingDescriptions(pending Maintenance) []string {
	result := make([]string, 0, 3)
	if pending.Log {
		result = append(result, "append log.md")
	}
	if pending.Index {
		result = append(result, "update index.md for new resources")
	}
	if pending.SourceRegistry {
		result = append(result, "update source-registry.md for source summaries")
	}
	return result
}

func normalizePath(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." {
		return ""
	}
	return clean
}

func addSorted(values []string, value string) []string {
	index := sort.SearchStrings(values, value)
	if index < len(values) && values[index] == value {
		return values
	}
	values = append(values, "")
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}

func mergeSorted(left, right []string) []string {
	result := cloneStrings(left)
	for _, value := range right {
		result = addSorted(result, value)
	}
	return result
}

func cloneStrings(values []string) []string { return append([]string(nil), values...) }

func sessionError(code ErrorCode, id string, cause error) error {
	return &Error{Code: code, SessionID: id, Cause: cause}
}

func translateStorageError(id string, err error) error {
	var typed *Error
	if errors.As(err, &typed) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		return sessionError(ErrorNotFound, id, fmt.Errorf("requested session does not exist"))
	}
	return sessionError(ErrorStorage, id, err)
}
