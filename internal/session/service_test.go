package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"scrinium/internal/store"
)

func newTestService(t *testing.T) (*Service, context.Context) {
	t.Helper()
	repository := t.TempDir()
	files, err := store.New(filepath.Join(repository, "llm-wiki"))
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(files, repository)
	if err != nil {
		t.Fatal(err)
	}
	return service, context.Background()
}

func TestWritePrerequisitesAndMaintenance(t *testing.T) {
	svc, ctx := newTestService(t)
	status, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RequireReadyForWrite(ctx, status.SessionID, "notes/a.md"); err == nil || !strings.Contains(err.Error(), "index.md") {
		t.Fatalf("expected startup reads requirement, got %v", err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		if err := svc.RecordRead(ctx, status.SessionID, path); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.DoWrite(ctx, status.SessionID, []Write{{Path: "notes/a.md"}}, func() (bool, error) {
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	status, err = svc.Get(ctx, status.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !status.NeedsLog || !status.NeedsIndex {
		t.Fatalf("expected log and index maintenance, got %+v", status)
	}
}

func TestSourceWriteRequiresIngestWorkflow(t *testing.T) {
	svc, ctx := newTestService(t)
	status, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		if err := svc.RecordRead(ctx, status.SessionID, path); err != nil {
			t.Fatal(err)
		}
	}
	err = svc.RequireReadyForWrite(ctx, status.SessionID, "sources/SRC-20260820-test.md")
	if err == nil || !strings.Contains(err.Error(), "workflows/ingest.md") {
		t.Fatalf("expected ingest workflow requirement, got %v", err)
	}
}

func TestUnknownSessionIsTyped(t *testing.T) {
	svc, ctx := newTestService(t)
	_, err := svc.Get(ctx, "ses_00000000000000000000000000000000")
	var sessionErr *Error
	if !errors.As(err, &sessionErr) || sessionErr.Code != ErrorNotFound {
		t.Fatalf("expected typed not found, got %v", err)
	}
}

func TestBeginIsUniqueAndPersistsAcrossServices(t *testing.T) {
	repository := t.TempDir()
	files, err := store.New(filepath.Join(repository, "llm-wiki"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := New(files, repository)
	if err != nil {
		t.Fatal(err)
	}
	one, err := first.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	two, err := first.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if one.SessionID == two.SessionID || !ValidID(one.SessionID) || !ValidID(two.SessionID) {
		t.Fatalf("session IDs are not unique opaque IDs: %q %q", one.SessionID, two.SessionID)
	}
	second, err := New(files, repository)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := second.Get(context.Background(), one.SessionID)
	if err != nil || persisted.Status != Active {
		t.Fatalf("session did not persist across service instances: %#v %v", persisted, err)
	}
	active, err := second.ListActive(context.Background())
	if err != nil || len(active) != 2 {
		t.Fatalf("active sessions = %#v, %v", active, err)
	}
}

func TestSimultaneousSessionsRemainIsolated(t *testing.T) {
	svc, ctx := newTestService(t)
	first, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var wait sync.WaitGroup
	for _, item := range []struct {
		id   string
		path string
	}{{first.SessionID, "first.md"}, {second.SessionID, "second.md"}} {
		wait.Add(1)
		go func(id, path string) {
			defer wait.Done()
			if err := svc.RecordRead(ctx, id, path); err != nil {
				t.Errorf("RecordRead(%s): %v", id, err)
			}
		}(item.id, item.path)
	}
	wait.Wait()
	firstStatus, err := svc.Get(ctx, first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	secondStatus, err := svc.Get(ctx, second.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if contains(firstStatus.PagesRead, "second.md") || contains(secondStatus.PagesRead, "first.md") {
		t.Fatalf("one session overwrote another: first=%#v second=%#v", firstStatus, secondStatus)
	}
}

func TestFinishAndAbandonLifecycle(t *testing.T) {
	svc, ctx := newTestService(t)
	started, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Finish(ctx, started.SessionID); err == nil {
		t.Fatal("finish succeeded without required reads")
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		if err := svc.RecordRead(ctx, started.SessionID, path); err != nil {
			t.Fatal(err)
		}
	}
	finished, err := svc.Finish(ctx, started.SessionID)
	if err != nil || finished.Status != Finished {
		t.Fatalf("finish = %#v, %v", finished, err)
	}
	if err := svc.RecordRead(ctx, started.SessionID, "other.md"); !hasCode(err, ErrorClosed) {
		t.Fatalf("finished session accepted mutation: %v", err)
	}

	other, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Abandon(ctx, other.SessionID, ""); err == nil {
		t.Fatal("empty abandonment reason accepted")
	}
	abandoned, err := svc.Abandon(ctx, other.SessionID, "work intentionally stopped")
	if err != nil || abandoned.Status != Abandoned || abandoned.AbandonReason == "" {
		t.Fatalf("abandon = %#v, %v", abandoned, err)
	}
	err = svc.DoWrite(ctx, other.SessionID, []Write{{Path: "notes.md"}}, func() (bool, error) { return true, nil })
	if !hasCode(err, ErrorClosed) {
		t.Fatalf("abandoned session accepted write: %v", err)
	}
}

func TestNoOpAndConcurrentTrackingPreserveState(t *testing.T) {
	svc, ctx := newTestService(t)
	started, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"index.md", "agent-rules.md"} {
		if err := svc.RecordRead(ctx, started.SessionID, path); err != nil {
			t.Fatal(err)
		}
	}
	before, err := svc.Get(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DoWrite(ctx, started.SessionID, []Write{{Path: "ignored.md"}}, func() (bool, error) {
		return false, nil
	}); err != nil {
		t.Fatal(err)
	}
	after, err := svc.Get(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) || len(after.PagesWritten) != 0 {
		t.Fatalf("no-op changed session: before=%#v after=%#v", before, after)
	}

	var wait sync.WaitGroup
	for index := range 24 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			path := fmt.Sprintf("notes/%02d.md", index)
			if err := svc.RecordRead(ctx, started.SessionID, path); err != nil {
				t.Errorf("record read: %v", err)
				return
			}
			err := svc.DoWrite(ctx, started.SessionID, []Write{{Path: path, ExistedBefore: true}}, func() (bool, error) {
				return true, nil
			})
			if err != nil {
				t.Errorf("record write: %v", err)
			}
		}(index)
	}
	wait.Wait()
	tracked, err := svc.Get(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tracked.PagesRead) != 26 || len(tracked.DocumentsWritten) != 24 || !tracked.NeedsLog {
		t.Fatalf("concurrent state was lost: %#v", tracked)
	}
}

func TestCorruptMismatchTraversalAndSymlinkRecordsFailSafely(t *testing.T) {
	t.Run("corrupt", func(t *testing.T) {
		svc, ctx := newTestService(t)
		started, err := svc.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		path := sessionPath(svc, started.SessionID)
		if err := os.WriteFile(path, []byte(`{"schema_version":"scrinium.session.v1","id":"x","id":"y"}`), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.Get(ctx, started.SessionID); !hasCode(err, ErrorCorrupt) {
			t.Fatalf("expected corrupt-session error, got %v", err)
		}
	})
	t.Run("repository mismatch", func(t *testing.T) {
		svc, ctx := newTestService(t)
		started, err := svc.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		otherRoot := t.TempDir()
		files, err := store.New(svc.records.Root())
		if err != nil {
			t.Fatal(err)
		}
		other, err := New(files, otherRoot)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := other.Get(ctx, started.SessionID); !hasCode(err, ErrorRepositoryMismatch) {
			t.Fatalf("expected repository mismatch, got %v", err)
		}
	})
	t.Run("invalid ID", func(t *testing.T) {
		svc, ctx := newTestService(t)
		if _, err := svc.Get(ctx, "../../escape"); !hasCode(err, ErrorInvalidID) {
			t.Fatalf("expected invalid ID, got %v", err)
		}
	})
	t.Run("symlink", func(t *testing.T) {
		svc, ctx := newTestService(t)
		started, err := svc.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		path := sessionPath(svc, started.SessionID)
		external := filepath.Join(t.TempDir(), "external.json")
		if err := os.WriteFile(external, []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, path); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if _, err := svc.Get(ctx, started.SessionID); !hasCode(err, ErrorStorage) {
			t.Fatalf("expected symlink rejection, got %v", err)
		}
	})
}

func TestSessionJSONIsDeterministic(t *testing.T) {
	svc, ctx := newTestService(t)
	started, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(sessionPath(svc, started.SessionID))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(data), "\n") || !strings.Contains(string(data), "\n  \"schema_version\"") {
		t.Fatalf("session JSON is not two-space indented with final newline: %q", data)
	}
	record, err := decode(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(again) {
		t.Fatalf("session serialization is unstable")
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil || raw["schema_version"] != SchemaVersion {
		t.Fatalf("invalid persisted schema: %#v %v", raw, err)
	}
}

func TestLockWaitCancellationAndTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		context func() (context.Context, context.CancelFunc)
	}{
		{name: "cancellation", context: func() (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(20*time.Millisecond, cancel)
			return ctx, cancel
		}},
		{name: "timeout", context: func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 20*time.Millisecond)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, ctx := newTestService(t)
			started, err := svc.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			lock := lockSessionFile(t, svc, started.SessionID)
			defer unlockSessionFile(lock)
			waitCtx, cancel := test.context()
			defer cancel()
			err = svc.RecordRead(waitCtx, started.SessionID, "index.md")
			if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("expected context lock failure, got %v", err)
			}
		})
	}
}

func TestSessionProcessHelper(t *testing.T) {
	if os.Getenv("SCRINIUM_SESSION_PROCESS_HELPER") != "1" {
		return
	}
	repository := os.Getenv("SCRINIUM_SESSION_REPOSITORY")
	id := os.Getenv("SCRINIUM_SESSION_ID")
	files, err := store.New(filepath.Join(repository, "llm-wiki"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(files, repository)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.RecordRead(context.Background(), id, "process-read.md"); err != nil {
		t.Fatal(err)
	}
}

func TestProcessLevelMutualExclusionAndPersistence(t *testing.T) {
	svc, ctx := newTestService(t)
	started, err := svc.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lock := lockSessionFile(t, svc, started.SessionID)
	cmd := exec.Command(os.Args[0], "-test.run=TestSessionProcessHelper")
	cmd.Env = append(os.Environ(),
		"SCRINIUM_SESSION_PROCESS_HELPER=1",
		"SCRINIUM_SESSION_REPOSITORY="+repositoryRoot(svc),
		"SCRINIUM_SESSION_ID="+started.SessionID,
	)
	if err := cmd.Start(); err != nil {
		unlockSessionFile(lock)
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- cmd.Wait() }()
	select {
	case err := <-finished:
		unlockSessionFile(lock)
		t.Fatalf("helper bypassed held process lock: %v", err)
	case <-time.After(75 * time.Millisecond):
	}
	unlockSessionFile(lock)
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	status, err := svc.Get(ctx, started.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(status.PagesRead, "process-read.md") {
		t.Fatalf("child-process update did not persist: %#v", status)
	}
}

func hasCode(err error, code ErrorCode) bool {
	var typed *Error
	return errors.As(err, &typed) && typed.Code == code
}

func contains(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}

func repositoryRoot(svc *Service) string {
	return filepath.Dir(svc.records.Root())
}

func sessionPath(svc *Service, id string) string {
	return filepath.Join(svc.records.Root(), ".scrinium", "sessions", id+".json")
}

func lockSessionFile(t *testing.T, svc *Service, id string) *os.File {
	t.Helper()
	path := filepath.Join(svc.records.Root(), ".scrinium", "locks", "sessions", id+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	return file
}

func unlockSessionFile(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
