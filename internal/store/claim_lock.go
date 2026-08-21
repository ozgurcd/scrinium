package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"scrinium/internal/knowledge"
)

const claimLockPollInterval = 10 * time.Millisecond

type claimLock struct {
	file *os.File
}

func (s *ClaimStore) lockClaim(ctx context.Context, id string) (*claimLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !knowledge.ValidSemanticID(id) {
		return nil, claimError("invalid_id", "", fmt.Errorf("invalid semantic claim ID %q", id))
	}
	directory, err := s.claimLockDirectory()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(directory, id+".lock")
	lock, err := lockRegularFile(ctx, path)
	if err != nil {
		return nil, claimError("claim_lock_failed", filepath.ToSlash(path), err)
	}
	return lock, nil
}

func lockRegularFile(ctx context.Context, path string) (*claimLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		if err == nil {
			err = fmt.Errorf("claim lock must be a non-linked regular file")
		}
		return nil, err
	}

	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &claimLock{file: file}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, err
		}
		timer := time.NewTimer(claimLockPollInterval)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *ClaimStore) claimLockDirectory() (string, error) {
	path := s.files.root
	for _, component := range []string{".scrinium", "locks", "claims"} {
		path = filepath.Join(path, component)
		if err := ensurePrivateDirectory(path); err != nil {
			return "", claimError("claim_lock_failed", filepath.ToSlash(path), err)
		}
	}
	return path, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil && !os.IsExist(err) {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("runtime lock path must be a non-linked directory")
	}
	return nil
}

func (l *claimLock) release() {
	if l == nil || l.file == nil {
		return
	}
	_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	_ = l.file.Close()
}
