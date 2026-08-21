package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

const runtimeLockPollInterval = 10 * time.Millisecond

// RuntimeRecords owns opaque, ignored operational records below
// llm-wiki/.scrinium. Callers own each record's schema.
type RuntimeRecords struct {
	files     *Store
	namespace string
}

// RuntimeMutation is returned after one locked record mutation.
type RuntimeMutation struct {
	Data    []byte
	Changed bool
}

// NewRuntimeRecords creates a confined operational-record store.
func NewRuntimeRecords(files *Store, namespace string) (*RuntimeRecords, error) {
	if files == nil {
		return nil, fmt.Errorf("runtime record store requires a file store")
	}
	if !validRuntimeComponent(namespace) {
		return nil, fmt.Errorf("invalid runtime namespace %q", namespace)
	}
	return &RuntimeRecords{files: files, namespace: namespace}, nil
}

// Root returns the configured wiki root containing the runtime area.
func (r *RuntimeRecords) Root() string { return r.files.Root() }

// Create exclusively creates one regular runtime record while holding its lock.
func (r *RuntimeRecords) Create(ctx context.Context, id string, data []byte) error {
	return r.withLock(ctx, id, func() error {
		directory, err := r.recordDirectory()
		if err != nil {
			return err
		}
		path := filepath.Join(directory, id+".json")
		if _, err := regularFile(path); err == nil {
			return os.ErrExist
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return atomicCreateFile(path, data, 0600)
	})
}

// Read returns exact bytes from one non-linked regular runtime record.
func (r *RuntimeRecords) Read(ctx context.Context, id string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validRuntimeComponent(id) {
		return nil, fmt.Errorf("invalid runtime record ID %q", id)
	}
	directory, err := r.recordDirectory()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, id+".json")
	return readRegularFile(path)
}

// Update locks, reads, and conditionally atomically replaces one runtime record.
// The callback runs while the cross-process record lock is held.
func (r *RuntimeRecords) Update(ctx context.Context, id string, mutate func([]byte) ([]byte, error)) (RuntimeMutation, error) {
	var result RuntimeMutation
	err := r.withLock(ctx, id, func() error {
		current, err := r.Read(ctx, id)
		if err != nil {
			return err
		}
		next, err := mutate(append([]byte(nil), current...))
		if err != nil {
			return err
		}
		if bytes.Equal(current, next) {
			result = RuntimeMutation{Data: current, Changed: false}
			return nil
		}
		directory, err := r.recordDirectory()
		if err != nil {
			return err
		}
		path := filepath.Join(directory, id+".json")
		latest, err := readRegularFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, latest) {
			return fmt.Errorf("runtime record changed outside the lock protocol")
		}
		if err := atomicWriteFile(path, next, 0600); err != nil {
			return err
		}
		result = RuntimeMutation{Data: append([]byte(nil), next...), Changed: true}
		return nil
	})
	return result, err
}

// List returns all regular runtime records in deterministic ID order.
func (r *RuntimeRecords) List(ctx context.Context) (map[string][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	directory, err := r.recordDirectory()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		if !validRuntimeComponent(id) {
			return nil, fmt.Errorf("invalid runtime record filename %q", name)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make(map[string][]byte, len(ids))
	for _, id := range ids {
		data, readErr := r.Read(ctx, id)
		if readErr != nil {
			return nil, readErr
		}
		result[id] = data
	}
	return result, nil
}

func (r *RuntimeRecords) withLock(ctx context.Context, id string, action func() error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !validRuntimeComponent(id) {
		return fmt.Errorf("invalid runtime record ID %q", id)
	}
	directory, err := r.lockDirectory()
	if err != nil {
		return err
	}
	path := filepath.Join(directory, id+".lock")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err == nil {
			err = fmt.Errorf("runtime lock must be a non-linked regular file")
		}
		return err
	}
	defer file.Close()
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN) //nolint:errcheck -- best-effort unlock on a closing descriptor
			return action()
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		timer := time.NewTimer(runtimeLockPollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (r *RuntimeRecords) recordDirectory() (string, error) {
	return r.privateDirectory(".scrinium", r.namespace)
}

func (r *RuntimeRecords) lockDirectory() (string, error) {
	return r.privateDirectory(".scrinium", "locks", r.namespace)
}

func (r *RuntimeRecords) privateDirectory(components ...string) (string, error) {
	path := r.files.root
	for _, component := range components {
		path = filepath.Join(path, component)
		if err := ensurePrivateDirectory(path); err != nil {
			return "", err
		}
	}
	return path, nil
}

func regularFile(path string) (os.FileInfo, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("runtime record must be a non-linked regular file")
	}
	return info, nil
}

func readRegularFile(path string) ([]byte, error) {
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("runtime record must be a non-linked regular file")
	}
	return io.ReadAll(file)
}

func validRuntimeComponent(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for i, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || (i > 0 && (r == '-' || r == '_')) {
			continue
		}
		return false
	}
	return true
}
