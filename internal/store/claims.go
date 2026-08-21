package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"scrinium/internal/knowledge"
)

const claimsDirectory = "claims"

// ClaimError identifies one deterministic claim storage failure.
type ClaimError struct {
	Code string
	Path string
	Err  error
}

func (e *ClaimError) Error() string {
	if e.Path == "" {
		return e.Err.Error()
	}
	return e.Path + ": " + e.Err.Error()
}

func (e *ClaimError) Unwrap() error { return e.Err }

func claimError(code, path string, err error) error {
	return &ClaimError{Code: code, Path: path, Err: err}
}

// ClaimFile is one inspected canonical-file candidate, including malformed files.
type ClaimFile struct {
	Path     string
	Claim    *knowledge.Claim
	Revision Revision
	Err      error
}

// Revision is an opaque content identity for exact canonical claim bytes.
type Revision string

// ClaimRecord couples a claim read with the revision required for a later mutation.
type ClaimRecord struct {
	Claim    knowledge.Claim
	Revision Revision
}

// ClaimMutation reports the exact record produced by a compare-and-swap.
type ClaimMutation struct {
	Record  ClaimRecord
	Changed bool
}

// RevisionConflictError reports that the canonical bytes changed since observation.
type RevisionConflictError struct {
	ClaimID  string
	Expected Revision
	Current  Revision
}

func (e *RevisionConflictError) Error() string {
	return fmt.Sprintf("claim %s revision conflict: expected %s, current %s", e.ClaimID, e.Expected, e.Current)
}

// ClaimStore persists one deterministic JSON file per claim.
type ClaimStore struct {
	files *Store
}

// NewClaimStore creates a canonical claim store below the wiki root.
func NewClaimStore(files *Store) *ClaimStore {
	return &ClaimStore{files: files}
}

// ClaimPath returns the canonical relative path for a validated claim ID.
func ClaimPath(id string) (string, error) {
	if !knowledge.ValidSemanticID(id) {
		return "", claimError("invalid_id", "", fmt.Errorf("invalid semantic claim ID %q", id))
	}
	return filepath.ToSlash(filepath.Join(claimsDirectory, id+".json")), nil
}

// EncodeClaim returns stable two-space-indented JSON with a final newline.
func EncodeClaim(claim knowledge.Claim) ([]byte, error) {
	if err := claim.Validate(); err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(claim, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode claim: %w", err)
	}
	return append(data, '\n'), nil
}

// DecodeClaim rejects duplicate keys, unknown fields, trailing values, and
// every invalid domain record instead of repairing it.
func DecodeClaim(data []byte) (knowledge.Claim, error) {
	if err := rejectDuplicateJSONKeys(data); err != nil {
		return knowledge.Claim{}, claimError("duplicate_json_key", "", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var claim knowledge.Claim
	if err := decoder.Decode(&claim); err != nil {
		return knowledge.Claim{}, claimError("invalid_claim_json", "", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return knowledge.Claim{}, claimError("invalid_claim_json", "", err)
	}
	if err := claim.Validate(); err != nil {
		return knowledge.Claim{}, err
	}
	return claim, nil
}

// Create writes a new claim and never overwrites an existing path.
func (s *ClaimStore) Create(ctx context.Context, claim knowledge.Claim) (ClaimRecord, error) {
	if err := ctx.Err(); err != nil {
		return ClaimRecord{}, err
	}
	data, err := EncodeClaim(claim)
	if err != nil {
		return ClaimRecord{}, err
	}
	path, err := ClaimPath(claim.ID)
	if err != nil {
		return ClaimRecord{}, err
	}
	lock, err := s.lockClaim(ctx, claim.ID)
	if err != nil {
		return ClaimRecord{}, err
	}
	defer lock.release()
	fullPath, exists, err := s.claimFilePath(path, true)
	if err != nil {
		return ClaimRecord{}, err
	}
	if exists {
		return ClaimRecord{}, claimError("claim_exists", path, fmt.Errorf("claim %s already exists", claim.ID))
	}
	if err := atomicCreateFile(fullPath, data, 0644); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ClaimRecord{}, claimError("claim_exists", path, fmt.Errorf("claim %s already exists", claim.ID))
		}
		return ClaimRecord{}, claimError("claim_write_failed", path, err)
	}
	return ClaimRecord{Claim: claim, Revision: revisionFor(data)}, nil
}

// Get loads one strict canonical claim and verifies filename identity.
func (s *ClaimStore) Get(ctx context.Context, id string) (ClaimRecord, error) {
	return s.readClaim(ctx, id)
}

func (s *ClaimStore) readClaim(ctx context.Context, id string) (ClaimRecord, error) {
	if err := ctx.Err(); err != nil {
		return ClaimRecord{}, err
	}
	path, err := ClaimPath(id)
	if err != nil {
		return ClaimRecord{}, err
	}
	fullPath, exists, err := s.claimFilePath(path, false)
	if err != nil {
		return ClaimRecord{}, err
	}
	if !exists {
		return ClaimRecord{}, claimError("claim_not_found", path, os.ErrNotExist)
	}
	data, err := os.ReadFile(fullPath)
	if err != nil {
		return ClaimRecord{}, claimError("claim_read_failed", path, err)
	}
	claim, err := DecodeClaim(data)
	if err != nil {
		return ClaimRecord{}, claimError("malformed_claim_file", path, err)
	}
	if claim.ID != id {
		return ClaimRecord{}, claimError("filename_id_mismatch", path, fmt.Errorf("filename ID %s does not match claim ID %s", id, claim.ID))
	}
	return ClaimRecord{Claim: claim, Revision: revisionFor(data)}, nil
}

// Update performs a claim-level cross-process compare-and-swap and rejects ID changes.
func (s *ClaimStore) Update(ctx context.Context, id string, expected Revision, mutate func(*knowledge.Claim) error) (ClaimMutation, error) {
	if !validRevision(expected) {
		return ClaimMutation{}, claimError("invalid_revision", "", fmt.Errorf("expected claim revision is required"))
	}
	lock, err := s.lockClaim(ctx, id)
	if err != nil {
		return ClaimMutation{}, err
	}
	defer lock.release()
	record, err := s.readClaim(ctx, id)
	if err != nil {
		return ClaimMutation{}, err
	}
	if record.Revision != expected {
		return ClaimMutation{}, &RevisionConflictError{ClaimID: id, Expected: expected, Current: record.Revision}
	}
	claim := record.Claim
	if err := mutate(&claim); err != nil {
		return ClaimMutation{}, err
	}
	if claim.ID != id {
		return ClaimMutation{}, claimError("immutable_claim_id", "id", fmt.Errorf("claim IDs are immutable"))
	}
	data, err := EncodeClaim(claim)
	if err != nil {
		return ClaimMutation{}, err
	}
	path, _ := ClaimPath(id)
	fullPath, exists, err := s.claimFilePath(path, false)
	if err != nil {
		return ClaimMutation{}, err
	}
	if !exists {
		return ClaimMutation{}, claimError("claim_not_found", path, os.ErrNotExist)
	}
	current, err := os.ReadFile(fullPath)
	if err != nil {
		return ClaimMutation{}, claimError("claim_read_failed", path, err)
	}
	currentRevision := revisionFor(current)
	if currentRevision != expected {
		return ClaimMutation{}, &RevisionConflictError{ClaimID: id, Expected: expected, Current: currentRevision}
	}
	if bytes.Equal(current, data) {
		return ClaimMutation{Record: record, Changed: false}, nil
	}
	if err := atomicWriteFile(fullPath, data, 0644); err != nil {
		return ClaimMutation{}, claimError("claim_write_failed", path, err)
	}
	return ClaimMutation{Record: ClaimRecord{Claim: claim, Revision: revisionFor(data)}, Changed: true}, nil
}

// List returns every valid canonical claim in stable ID order.
func (s *ClaimStore) List(ctx context.Context) ([]ClaimRecord, error) {
	entries, err := s.Inspect(ctx)
	if err != nil {
		return nil, err
	}
	claims := make([]ClaimRecord, 0, len(entries))
	for _, entry := range entries {
		if entry.Err != nil {
			return nil, entry.Err
		}
		claims = append(claims, ClaimRecord{Claim: *entry.Claim, Revision: entry.Revision})
	}
	sort.Slice(claims, func(i, j int) bool { return claims[i].Claim.ID < claims[j].Claim.ID })
	return claims, nil
}

// Inspect returns valid and malformed entries so deterministic lint can report
// every file instead of stopping at the first failure.
func (s *ClaimStore) Inspect(ctx context.Context) ([]ClaimFile, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, exists, err := s.claimDirectory(false)
	if err != nil || !exists {
		return []ClaimFile{}, err
	}
	dirEntries, err := os.ReadDir(directory)
	if err != nil {
		return nil, claimError("claims_read_failed", claimsDirectory, err)
	}
	result := make([]ClaimFile, 0, len(dirEntries))
	for _, entry := range dirEntries {
		if strings.HasPrefix(entry.Name(), ".scrinium-") && strings.HasSuffix(entry.Name(), ".tmp") {
			continue
		}
		path := filepath.ToSlash(filepath.Join(claimsDirectory, entry.Name()))
		item := ClaimFile{Path: path}
		if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			item.Err = claimError("malformed_claim_file", path, fmt.Errorf("claim entries must be non-linked regular .json files"))
			result = append(result, item)
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			if infoErr == nil {
				infoErr = fmt.Errorf("not a regular file")
			}
			item.Err = claimError("malformed_claim_file", path, infoErr)
			result = append(result, item)
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(directory, entry.Name()))
		if readErr != nil {
			item.Err = claimError("claim_read_failed", path, readErr)
			result = append(result, item)
			continue
		}
		claim, decodeErr := DecodeClaim(data)
		if decodeErr != nil {
			item.Err = claimError("malformed_claim_file", path, decodeErr)
			result = append(result, item)
			continue
		}
		item.Claim = &claim
		item.Revision = revisionFor(data)
		filenameID := strings.TrimSuffix(entry.Name(), ".json")
		if filenameID != claim.ID {
			item.Err = claimError("filename_id_mismatch", path, fmt.Errorf("filename ID %s does not match claim ID %s", filenameID, claim.ID))
		}
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func revisionFor(data []byte) Revision {
	digest := sha256.Sum256(data)
	return Revision(fmt.Sprintf("sha256:%x", digest[:]))
}

// ValidRevision reports whether a token has the store's opaque revision shape.
func ValidRevision(revision Revision) bool { return validRevision(revision) }

func validRevision(revision Revision) bool {
	const prefix = "sha256:"
	value := string(revision)
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+sha256.Size*2 {
		return false
	}
	for _, character := range value[len(prefix):] {
		if !strings.ContainsRune("0123456789abcdef", character) {
			return false
		}
	}
	return true
}

func (s *ClaimStore) claimFilePath(path string, createDirectory bool) (string, bool, error) {
	directory, _, err := s.claimDirectory(createDirectory)
	if err != nil {
		return "", false, err
	}
	fullPath := filepath.Join(directory, filepath.Base(path))
	info, err := os.Lstat(fullPath)
	if os.IsNotExist(err) {
		return fullPath, false, nil
	}
	if err != nil {
		return "", false, claimError("claim_stat_failed", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return "", false, claimError("claim_not_regular", path, fmt.Errorf("claim path must be a non-linked regular file"))
	}
	return fullPath, true, nil
}

func (s *ClaimStore) claimDirectory(create bool) (string, bool, error) {
	path := filepath.Join(s.files.root, claimsDirectory)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		if !create {
			return path, false, nil
		}
		if err := os.Mkdir(path, 0755); err != nil && !os.IsExist(err) {
			return "", false, claimError("claims_directory_failed", claimsDirectory, err)
		}
		info, err = os.Lstat(path)
	}
	if err != nil {
		return "", false, claimError("claims_directory_failed", claimsDirectory, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", false, claimError("claims_directory_failed", claimsDirectory, fmt.Errorf("claims path must be a non-linked directory"))
	}
	return path, true, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := scanJSONValue(decoder, "$", nil); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder, path string, first json.Token) error {
	token := first
	var err error
	if token == nil {
		token, err = decoder.Token()
		if err != nil {
			return err
		}
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]bool)
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("invalid object key at %s", path)
			}
			if keys[key] {
				return fmt.Errorf("duplicate JSON key %q at %s", key, path)
			}
			keys[key] = true
			if err := scanJSONValue(decoder, path+"."+key, nil); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index), nil); err != nil {
				return err
			}
			index++
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple JSON values are not allowed")
	}
	return err
}
