package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scrinium/internal/knowledge"
)

var claimTestTime = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func storeClaim(id string) knowledge.Claim {
	return knowledge.Claim{
		SchemaVersion: knowledge.SchemaVersion,
		ID:            id, Subject: "authentication", Statement: "Administrators retain local authentication.",
		Lifecycle:  knowledge.Lifecycle{State: knowledge.LifecycleActive},
		Authorship: knowledge.Authorship{Kind: knowledge.AuthorshipOwner, Origin: "owner:project", RecordedAt: claimTestTime},
		Evidence:   []knowledge.Evidence{}, ValidationResults: []knowledge.ValidationResult{},
		CreatedAt: claimTestTime, UpdatedAt: claimTestTime,
	}
}

func newClaimStore(t *testing.T) (*Store, *ClaimStore) {
	t.Helper()
	files, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return files, NewClaimStore(files)
}

func TestClaimJSONRoundTripAndDeterministicSerialization(t *testing.T) {
	claim := storeClaim("AUTH-ADMIN-LOCAL-1")
	first, err := EncodeClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EncodeClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !bytes.HasSuffix(first, []byte("\n")) || !bytes.Contains(first, []byte("\n  \"id\"")) {
		t.Fatalf("claim serialization is not stable two-space JSON with final newline:\n%s", first)
	}
	decoded, err := DecodeClaim(first)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID != claim.ID || decoded.Statement != claim.Statement {
		t.Fatalf("round trip changed claim: %#v", decoded)
	}
}

func TestDecodeClaimStrictFailures(t *testing.T) {
	valid, err := EncodeClaim(storeClaim("AUTH-ADMIN-LOCAL-1"))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		data []byte
		code string
	}{
		{name: "unknown field", data: bytes.Replace(valid, []byte("\n}"), []byte(",\n  \"unexpected\": true\n}"), 1), code: "invalid_claim_json"},
		{name: "duplicate key", data: bytes.Replace(valid, []byte("  \"id\":"), []byte("  \"id\": \"AUTH-ADMIN-LOCAL-1\",\n  \"id\":"), 1), code: "duplicate_json_key"},
		{name: "unsupported schema", data: bytes.Replace(valid, []byte(knowledge.SchemaVersion), []byte("scrinium.claim/v99"), 1), code: "unsupported_schema_version"},
		{name: "invalid ID", data: bytes.Replace(valid, []byte("AUTH-ADMIN-LOCAL-1"), []byte("../bad"), 1), code: "invalid_id"},
		{name: "invalid timestamp", data: bytes.Replace(valid, []byte(claimTestTime.Format(time.RFC3339)), []byte("not-a-time"), 1), code: "invalid_claim_json"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := DecodeClaim(test.data)
			var storeErr *ClaimError
			var validationErr *knowledge.ValidationError
			switch {
			case errors.As(err, &storeErr) && storeErr.Code == test.code:
			case errors.As(err, &validationErr) && validationErr.Code == test.code:
			default:
				t.Fatalf("expected code %s, got %v", test.code, err)
			}
		})
	}
}

func TestClaimStoreRejectsFilenameMismatch(t *testing.T) {
	files, claims := newClaimStore(t)
	data, err := EncodeClaim(storeClaim("AUTH-ADMIN-LOCAL-1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Write(context.Background(), "claims/AUTH-ADMIN-LOCAL-2.json", data, 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := claims.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var claimErr *ClaimError
	if len(entries) != 1 || !errors.As(entries[0].Err, &claimErr) || claimErr.Code != "filename_id_mismatch" {
		t.Fatalf("expected filename mismatch, got %#v", entries)
	}
}

func TestClaimStoreCreateUpdateImmutabilityAndCompareBeforeWrite(t *testing.T) {
	files, claims := newClaimStore(t)
	ctx := context.Background()
	claim := storeClaim("AUTH-ADMIN-LOCAL-1")
	record, err := claims.Create(ctx, claim)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := EncodeClaim(claim)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	if record.Revision != Revision(fmt.Sprintf("sha256:%x", digest[:])) {
		t.Fatalf("claim creation returned wrong exact-byte revision: %s", record.Revision)
	}
	mutation, err := claims.Update(ctx, claim.ID, record.Revision, func(*knowledge.Claim) error { return nil })
	if err != nil || mutation.Changed || mutation.Record.Revision != record.Revision {
		t.Fatalf("no-op update = %#v, err %v", mutation, err)
	}
	mutation, err = claims.Update(ctx, claim.ID, record.Revision, func(current *knowledge.Claim) error {
		current.Statement = "Administrators retain local authentication at all times."
		current.UpdatedAt = current.UpdatedAt.Add(time.Second)
		return nil
	})
	if err != nil || !mutation.Changed || mutation.Record.Revision == record.Revision {
		t.Fatalf("content update = %#v, err %v", mutation, err)
	}
	_, err = claims.Update(ctx, claim.ID, mutation.Record.Revision, func(current *knowledge.Claim) error {
		current.ID = "AUTH-ADMIN-LOCAL-2"
		return nil
	})
	var claimErr *ClaimError
	if !errors.As(err, &claimErr) || claimErr.Code != "immutable_claim_id" {
		t.Fatalf("expected immutable ID rejection, got %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(files.Root(), "claims"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != claim.ID+".json" {
		t.Fatalf("atomic claim persistence left unexpected entries: %#v", entries)
	}
	stored, err := claims.Get(ctx, claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Claim.Statement != "Administrators retain local authentication at all times." {
		t.Fatalf("atomic replacement did not persist complete claim: %#v", stored)
	}
}

func TestClaimStoreRejectsStaleRevision(t *testing.T) {
	_, claims := newClaimStore(t)
	record, err := claims.Create(context.Background(), storeClaim("AUTH-ADMIN-LOCAL-1"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := claims.Update(context.Background(), record.Claim.ID, record.Revision, func(claim *knowledge.Claim) error {
		claim.Subject = "login"
		claim.UpdatedAt = claim.UpdatedAt.Add(time.Second)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = claims.Update(context.Background(), record.Claim.ID, record.Revision, func(claim *knowledge.Claim) error {
		claim.Statement = "stale overwrite"
		return nil
	})
	var conflict *RevisionConflictError
	if !errors.As(err, &conflict) || conflict.Expected != record.Revision || conflict.Current != first.Record.Revision {
		t.Fatalf("expected typed revision conflict, got %v", err)
	}
	stored, err := claims.Get(context.Background(), record.Claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Claim.Statement == "stale overwrite" || stored.Revision != first.Record.Revision {
		t.Fatalf("stale mutation changed canonical claim: %#v", stored)
	}
}

func TestClaimStoreConcurrentCreateAllowsOneWriter(t *testing.T) {
	files, firstStore := newClaimStore(t)
	secondStore := NewClaimStore(files)
	stores := []*ClaimStore{firstStore, secondStore}
	claim := storeClaim("AUTH-ADMIN-LOCAL-1")
	start := make(chan struct{})
	errorsByWriter := make([]error, 2)
	var wait sync.WaitGroup
	for index := range errorsByWriter {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByWriter[index] = stores[index].Create(context.Background(), claim)
		}(index)
	}
	close(start)
	wait.Wait()
	successes := 0
	conflicts := 0
	for _, err := range errorsByWriter {
		if err == nil {
			successes++
			continue
		}
		var claimErr *ClaimError
		if errors.As(err, &claimErr) && claimErr.Code == "claim_exists" {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent create successes=%d conflicts=%d errors=%v", successes, conflicts, errorsByWriter)
	}
}

func TestClaimStoreListsClaimsInStableOrder(t *testing.T) {
	_, claims := newClaimStore(t)
	for _, id := range []string{"ZZZ-PROJECT-2", "AAA-PROJECT-1"} {
		if _, err := claims.Create(context.Background(), storeClaim(id)); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := claims.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 || listed[0].Claim.ID != "AAA-PROJECT-1" || listed[1].Claim.ID != "ZZZ-PROJECT-2" {
		t.Fatalf("unexpected claim order: %#v", listed)
	}
}

func TestClaimLockWaitHonorsCancellationAndDeadline(t *testing.T) {
	files, claims := newClaimStore(t)
	record, err := claims.Create(context.Background(), storeClaim("AUTH-ADMIN-LOCAL-1"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := claims.lockClaim(context.Background(), record.Claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	other := NewClaimStore(files)

	t.Run("cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		result := make(chan error, 1)
		go func() {
			_, err := other.Update(ctx, record.Claim.ID, record.Revision, func(*knowledge.Claim) error { return nil })
			result <- err
		}()
		time.Sleep(20 * time.Millisecond)
		cancel()
		err := <-result
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation, got %v", err)
		}
	})
	t.Run("deadline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel()
		_, err := other.Update(ctx, record.Claim.ID, record.Revision, func(*knowledge.Claim) error { return nil })
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline, got %v", err)
		}
	})
}

func TestClaimStoreProcessLevelMutualExclusion(t *testing.T) {
	files, claims := newClaimStore(t)
	record, err := claims.Create(context.Background(), storeClaim("AUTH-ADMIN-LOCAL-1"))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := claims.lockClaim(context.Background(), record.Claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	command := exec.Command(os.Args[0], "-test.run=^TestClaimStoreHelperProcess$")
	command.Env = append(os.Environ(),
		"SCRINIUM_CLAIM_HELPER=1",
		"SCRINIUM_CLAIM_ROOT="+files.Root(),
		"SCRINIUM_CLAIM_ID="+record.Claim.ID,
		"SCRINIUM_CLAIM_REVISION="+string(record.Revision),
		"SCRINIUM_CLAIM_READY="+ready,
	)
	if err := command.Start(); err != nil {
		lock.release()
		t.Fatal(err)
	}
	waitForFile(t, ready)
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		lock.release()
		t.Fatalf("helper bypassed held claim lock: %v", err)
	case <-time.After(40 * time.Millisecond):
	}
	lock.release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		_ = command.Process.Kill()
		t.Fatal("helper did not acquire released claim lock")
	}
	stored, err := claims.Get(context.Background(), record.Claim.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Claim.Subject != "process-updated" || stored.Revision == record.Revision {
		t.Fatalf("helper mutation was not persisted atomically: %#v", stored)
	}
}

func TestClaimStoreHelperProcess(t *testing.T) {
	if os.Getenv("SCRINIUM_CLAIM_HELPER") != "1" {
		return
	}
	files, err := New(os.Getenv("SCRINIUM_CLAIM_ROOT"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("SCRINIUM_CLAIM_READY"), []byte("ready"), 0600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	claims := NewClaimStore(files)
	_, err = claims.Update(context.Background(), os.Getenv("SCRINIUM_CLAIM_ID"), Revision(os.Getenv("SCRINIUM_CLAIM_REVISION")), func(claim *knowledge.Claim) error {
		claim.Subject = "process-updated"
		claim.UpdatedAt = claim.UpdatedAt.Add(time.Second)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("helper process did not signal readiness")
}

func TestClaimStoreRejectsNonRegularClaimPath(t *testing.T) {
	files, claims := newClaimStore(t)
	claimsPath := filepath.Join(files.Root(), "claims")
	if err := os.Mkdir(claimsPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(claimsPath, "AUTH-ADMIN-LOCAL-1.json"), 0755); err != nil {
		t.Fatal(err)
	}
	_, err := claims.Get(context.Background(), "AUTH-ADMIN-LOCAL-1")
	if err == nil || !strings.Contains(err.Error(), "non-linked regular file") {
		t.Fatalf("expected regular-file rejection, got %v", err)
	}
}
