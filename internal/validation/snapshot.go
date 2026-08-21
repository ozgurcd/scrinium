package validation

import (
	"context"
	"crypto/sha256"
	"fmt"
	"hash"
	"sort"
	"strconv"
	"strings"
	"time"

	"scrinium/internal/knowledge"
)

type Repository interface {
	Fingerprint(ctx context.Context, path string) (exists bool, fingerprint string, err error)
}

type Snapshotter struct {
	repository Repository
}

type RepositoryEntry struct {
	Path        string
	Fingerprint string
}

func NewSnapshotter(repository Repository) *Snapshotter {
	return &Snapshotter{repository: repository}
}

func (s *Snapshotter) Build(ctx context.Context, claim knowledge.Claim, binding knowledge.ValidationBinding) (RepositorySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return RepositorySnapshot{}, err
	}
	if s == nil || s.repository == nil {
		return RepositorySnapshot{}, validationError("repository_unavailable", "repository fingerprint service is unavailable")
	}
	evidence := make(map[string]knowledge.Evidence, len(claim.Evidence))
	for _, item := range claim.Evidence {
		evidence[item.ID] = item
	}
	entries := make([]RepositoryEntry, 0)
	seenPaths := make(map[string]bool)
	staleEvidence := ""
	for _, evidenceID := range binding.EvidenceIDs {
		item, exists := evidence[evidenceID]
		if !exists {
			return RepositorySnapshot{}, validationError("missing_repository_state", fmt.Sprintf("binding references missing evidence %s", evidenceID))
		}
		if item.Kind != knowledge.EvidenceRepositoryReference || !strings.HasPrefix(item.Locator, "repo:") {
			continue
		}
		if item.Availability != knowledge.AvailabilityAvailable {
			return RepositorySnapshot{}, validationError("missing_repository_state", fmt.Sprintf("repository evidence %s is not available", item.ID))
		}
		path := strings.TrimPrefix(item.Locator, "repo:")
		if path == "" || seenPaths[path] {
			continue
		}
		seenPaths[path] = true
		exists, fingerprint, err := s.repository.Fingerprint(ctx, path)
		if err != nil {
			return RepositorySnapshot{}, validationError("repository_snapshot_failed", err.Error())
		}
		if !exists {
			return RepositorySnapshot{}, validationError("missing_repository_state", fmt.Sprintf("repository path %s does not exist", path))
		}
		if item.Fingerprint != "" && item.Fingerprint != fingerprint {
			staleEvidence = item.ID
		}
		entries = append(entries, RepositoryEntry{Path: path, Fingerprint: fingerprint})
	}
	if len(entries) == 0 {
		return RepositorySnapshot{}, validationError("repository_snapshot_unavailable", "binding has no reproducible repository_reference evidence")
	}
	fingerprint := RepositoryFingerprint(entries)
	snapshot := RepositorySnapshot{Revision: binding.RepositoryRevision, Fingerprint: fingerprint, CapturedAt: time.Now().UTC()}
	if staleEvidence != "" {
		return snapshot, validationError("stale_repository_snapshot", fmt.Sprintf("repository evidence %s fingerprint changed", staleEvidence))
	}
	if binding.SnapshotFingerprint == "" {
		return snapshot, validationError("repository_snapshot_unavailable", "binding has no expected repository snapshot fingerprint")
	}
	if binding.SnapshotFingerprint != fingerprint {
		return snapshot, validationError("stale_repository_snapshot", "current scoped repository fingerprint does not match the binding")
	}
	return snapshot, nil
}

func RepositoryFingerprint(entries []RepositoryEntry) string {
	copyEntries := append([]RepositoryEntry(nil), entries...)
	sort.Slice(copyEntries, func(i, j int) bool {
		if copyEntries[i].Path != copyEntries[j].Path {
			return copyEntries[i].Path < copyEntries[j].Path
		}
		return copyEntries[i].Fingerprint < copyEntries[j].Fingerprint
	})
	digest := sha256.New()
	for _, entry := range copyEntries {
		writeField(digest, "path", entry.Path)
		writeField(digest, "fingerprint", entry.Fingerprint)
	}
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}

func InputFingerprint(claim knowledge.Claim, binding knowledge.ValidationBinding, snapshot RepositorySnapshot) string {
	digest := sha256.New()
	writeField(digest, "claim_id", claim.ID)
	writeField(digest, "subject", claim.Subject)
	writeField(digest, "statement", claim.Statement)
	writeField(digest, "binding_id", binding.ID)
	writeField(digest, "validator_id", binding.ValidatorID)
	writeField(digest, "binding_version", binding.BindingVersion)
	writeField(digest, "reference", binding.Reference)
	writeField(digest, "required", strconv.FormatBool(binding.Required))
	writeField(digest, "required_assurance", string(binding.RequiredAssurance))
	writeField(digest, "valid_for_seconds", strconv.FormatInt(binding.ValidForSeconds, 10))
	parameterKeys := make([]string, 0, len(binding.Parameters))
	for key := range binding.Parameters {
		parameterKeys = append(parameterKeys, key)
	}
	sort.Strings(parameterKeys)
	for _, key := range parameterKeys {
		writeField(digest, "parameter_key", key)
		writeField(digest, "parameter_value", binding.Parameters[key])
	}
	evidenceByID := make(map[string]knowledge.Evidence, len(claim.Evidence))
	for _, item := range claim.Evidence {
		evidenceByID[item.ID] = item
	}
	evidenceIDs := append([]string(nil), binding.EvidenceIDs...)
	sort.Strings(evidenceIDs)
	for _, id := range evidenceIDs {
		writeField(digest, "evidence_id", id)
		item := evidenceByID[id]
		writeField(digest, "evidence_kind", string(item.Kind))
		writeField(digest, "evidence_polarity", string(item.Polarity))
		writeField(digest, "evidence_origin", string(item.Origin.Kind)+":"+item.Origin.Reference)
		writeField(digest, "evidence_locator", item.Locator)
		writeField(digest, "evidence_scope", item.Scope)
		writeField(digest, "evidence_fingerprint", item.Fingerprint)
	}
	writeField(digest, "repository_revision", snapshot.Revision)
	writeField(digest, "repository_fingerprint", snapshot.Fingerprint)
	return fmt.Sprintf("sha256:%x", digest.Sum(nil))
}

func writeField(digest hash.Hash, name, value string) {
	_, _ = fmt.Fprintf(digest, "%d:%s%d:%s", len(name), name, len(value), value)
}
