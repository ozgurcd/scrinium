package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"scrinium/internal/provenance"
)

func testSource() provenance.SourceRecord {
	received := provenance.Date("2026-08-20")
	now := time.Date(2026, 8, 21, 12, 0, 0, 0, time.UTC)
	return provenance.SourceRecord{
		SchemaVersion: provenance.SchemaVersion, ID: "SRC-20260820-project-design", Title: "Project design",
		SourceType: provenance.SourceTypeProjectDocument, Origin: provenance.Origin{Kind: provenance.OriginOwner, Trust: provenance.TrustOwner},
		RawPath: "raw/design.md", RawFingerprint: "sha256:" + strings.Repeat("a", 64), ReceivedDate: &received,
		IngestDate: "2026-08-21", Status: provenance.StatusCurrent, DerivedClaims: []string{},
		DerivedPages: []string{"projects/scrinium.md"}, ProvenanceNotes: []string{}, CreatedAt: now, UpdatedAt: now,
	}
}

func newSourceStore(t *testing.T) (*Store, *SourceStore) {
	t.Helper()
	files, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return files, NewSourceStore(files)
}

func TestSourceJSONRoundTripAndStrictFailures(t *testing.T) {
	record := testSource()
	encoded, err := EncodeSource(record)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasSuffix(encoded, []byte("\n")) || bytes.Contains(encoded, []byte("\t")) {
		t.Fatalf("serialization is not canonical: %q", encoded)
	}
	if bytes.Index(encoded, []byte(`"schema_version"`)) > bytes.Index(encoded, []byte(`"id"`)) {
		t.Fatal("field order is not stable")
	}
	decoded, err := DecodeSource(encoded)
	if err != nil || decoded.ID != record.ID {
		t.Fatalf("round trip failed: %#v %v", decoded, err)
	}
	tests := map[string][]byte{
		"unknown field":      bytes.Replace(encoded, []byte(`"title":`), []byte(`"unknown":true,"title":`), 1),
		"duplicate key":      bytes.Replace(encoded, []byte(`"title":`), []byte(`"id":"SRC-20260820-project-design","title":`), 1),
		"unsupported schema": bytes.Replace(encoded, []byte(provenance.SchemaVersion), []byte("scrinium.source/v9"), 1),
		"invalid enum":       bytes.Replace(encoded, []byte(`"project_document"`), []byte(`"website"`), 1),
		"invalid date":       bytes.Replace(encoded, []byte(`"2026-08-21"`), []byte(`"not-a-date"`), 1),
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeSource(data); err == nil {
				t.Fatal("expected strict decode failure")
			}
		})
	}
}

func TestSourceStoreCASAndFilenameMismatch(t *testing.T) {
	files, sources := newSourceStore(t)
	ctx := context.Background()
	record, err := sources.Create(ctx, testSource())
	if err != nil {
		t.Fatal(err)
	}
	if !ValidRevision(record.Revision) {
		t.Fatal("read did not return a revision")
	}
	noOp, err := sources.Update(ctx, record.Source.ID, record.Revision, func(*provenance.SourceRecord) error { return nil })
	if err != nil || noOp.Changed || noOp.Record.Revision != record.Revision {
		t.Fatalf("no-op changed revision: %#v %v", noOp, err)
	}
	updated, err := sources.Update(ctx, record.Source.ID, record.Revision, func(source *provenance.SourceRecord) error {
		source.Title = "Updated design"
		source.UpdatedAt = source.UpdatedAt.Add(time.Second)
		return nil
	})
	if err != nil || !updated.Changed || updated.Record.Revision == record.Revision {
		t.Fatalf("CAS update failed: %#v %v", updated, err)
	}
	_, err = sources.Update(ctx, record.Source.ID, record.Revision, func(*provenance.SourceRecord) error { return nil })
	var conflict *SourceRevisionConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("stale update was not a typed conflict: %v", err)
	}
	other := testSource()
	other.ID = "SRC-20260820-other"
	data, err := EncodeSource(other)
	if err != nil {
		t.Fatal(err)
	}
	if err := files.Write(ctx, "sources/records/SRC-20260820-wrong.json", data, 0644); err != nil {
		t.Fatal(err)
	}
	entries, err := sources.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		var sourceErr *SourceError
		if errors.As(entry.Err, &sourceErr) && sourceErr.Code == "filename_id_mismatch" {
			found = true
		}
	}
	if !found {
		t.Fatal("filename/ID mismatch was not reported")
	}
}

func TestConcurrentSourceRegistrationAllowsOneWriter(t *testing.T) {
	files, first := newSourceStore(t)
	second := NewSourceStore(files)
	ctx := context.Background()
	start := make(chan struct{})
	errorsSeen := make(chan error, 2)
	var wait sync.WaitGroup
	for _, sources := range []*SourceStore{first, second} {
		wait.Add(1)
		go func(sources *SourceStore) {
			defer wait.Done()
			<-start
			_, err := sources.Create(ctx, testSource())
			errorsSeen <- err
		}(sources)
	}
	close(start)
	wait.Wait()
	close(errorsSeen)
	successes, conflicts := 0, 0
	for err := range errorsSeen {
		if err == nil {
			successes++
			continue
		}
		var sourceErr *SourceError
		if errors.As(err, &sourceErr) && sourceErr.Code == "source_exists" {
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one create and one conflict, got %d/%d", successes, conflicts)
	}
}

func TestSourceStoreRejectsSymlinkAndHonorsLockTimeout(t *testing.T) {
	files, sources := newSourceStore(t)
	ctx := context.Background()
	directory := filepath.Join(files.Root(), "sources", "records")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(t.TempDir(), "outside.json"), filepath.Join(directory, testSource().ID+".json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := sources.Get(ctx, testSource().ID); err == nil {
		t.Fatal("symlink source record was accepted")
	}
	if err := os.Remove(filepath.Join(directory, testSource().ID+".json")); err != nil {
		t.Fatal(err)
	}
	created, err := sources.Create(ctx, testSource())
	if err != nil {
		t.Fatal(err)
	}
	lock, err := sources.lockSource(ctx, created.Source.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.release()
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	_, err = sources.Update(waitCtx, created.Source.ID, created.Revision, func(*provenance.SourceRecord) error { return nil })
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock timeout was not propagated: %v", err)
	}
}

func TestSourceAtomicUpdateLeavesNoTemporaryFiles(t *testing.T) {
	files, sources := newSourceStore(t)
	record, err := sources.Create(context.Background(), testSource())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sources.Update(context.Background(), record.Source.ID, record.Revision, func(source *provenance.SourceRecord) error {
		source.Title = "Atomic update"
		source.UpdatedAt = source.UpdatedAt.Add(time.Second)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(files.Root(), "sources", "records"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".scrinium-") {
			t.Fatalf("temporary artifact remained after atomic update: %s", entry.Name())
		}
	}
}
