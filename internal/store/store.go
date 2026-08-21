// Package store owns confined filesystem access to the configured wiki root.
package store

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Store provides repository-local wiki file operations.
type Store struct {
	root string
}

// New creates the wiki root and returns a confined store for it.
func New(root string) (*Store, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve wiki root: %w", err)
	}
	if err := os.MkdirAll(absRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create wiki root: %w", err)
	}
	return &Store{root: absRoot}, nil
}

// Root returns the configured absolute wiki root.
func (s *Store) Root() string {
	return s.root
}

// Read reads one confined path.
func (s *Store) Read(ctx context.Context, path string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fullPath, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(fullPath)
}

// Exists reports whether a confined path exists.
func (s *Store) Exists(ctx context.Context, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	fullPath, err := s.resolve(path)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(fullPath); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

// Write atomically replaces one confined path.
func (s *Store) Write(ctx context.Context, path string, data []byte, perm os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	return atomicWriteFile(fullPath, data, perm)
}

// Append appends data and one trailing newline to a confined path.
func (s *Store) Append(ctx context.Context, path, data string, perm os.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fullPath, err := s.resolve(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}
	f, err := os.OpenFile(fullPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(data + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Move renames one confined path to another.
func (s *Store) Move(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	fromPath, err := s.resolve(from)
	if err != nil {
		return err
	}
	toPath, err := s.resolve(to)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(toPath), 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}
	return os.Rename(fromPath, toPath)
}

// List returns every file path under the wiki root in deterministic order.
func (s *Store) List(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files := make([]string, 0)
	err := fs.WalkDir(os.DirFS(s.root), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (path == ".scrinium" || strings.HasPrefix(filepath.ToSlash(path), ".scrinium/")) {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".scrinium-") && strings.HasSuffix(path, ".tmp") {
			return nil
		}
		files = append(files, filepath.ToSlash(path))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk wiki directory: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// Fingerprint returns a deterministic SHA-256 fingerprint for one confined
// regular file. Missing files return exists=false without an error.
func (s *Store) Fingerprint(ctx context.Context, path string) (exists bool, fingerprint string, err error) {
	if err := ctx.Err(); err != nil {
		return false, "", err
	}
	fullPath, err := s.resolve(path)
	if err != nil {
		return false, "", err
	}
	canonicalRoot, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return false, "", err
	}
	if err := rejectLinkedPath(canonicalRoot, filepath.Join(canonicalRoot, path)); err != nil {
		return false, "", err
	}
	info, err := os.Lstat(fullPath)
	if os.IsNotExist(err) {
		return false, "", nil
	}
	if err != nil {
		return false, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return true, "", fmt.Errorf("%s is not a regular file", path)
	}
	data, err := readRegularFile(fullPath)
	if err != nil {
		return true, "", err
	}
	digest := sha256.Sum256(data)
	return true, fmt.Sprintf("sha256:%x", digest), nil
}

func rejectLinkedPath(root, target string) error {
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(canonicalRoot, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path is outside store root")
	}
	current := canonicalRoot
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("path component %s is a symbolic link", filepath.ToSlash(current))
		}
	}
	return nil
}

func (s *Store) resolve(relative string) (string, error) {
	root, err := filepath.EvalSymlinks(s.root)
	if err != nil {
		return "", fmt.Errorf("invalid wiki root: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("invalid wiki root: %w", err)
	}

	joined := filepath.Join(root, relative)
	abs, err := filepath.Abs(joined)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	if !strings.HasPrefix(abs, root+string(os.PathSeparator)) && abs != root {
		return "", fmt.Errorf("error: path '%s' escapes the wiki root — access denied", relative)
	}

	if real, evalErr := filepath.EvalSymlinks(abs); evalErr == nil {
		if !strings.HasPrefix(real, root+string(os.PathSeparator)) && real != root {
			return "", fmt.Errorf("error: path '%s' resolves outside the wiki root via symlink — access denied", relative)
		}
		return real, nil
	}

	parentReal, err := existingParentRealPath(abs)
	if err != nil {
		return "", fmt.Errorf("invalid path parent: %w", err)
	}
	if !strings.HasPrefix(parentReal, root+string(os.PathSeparator)) && parentReal != root {
		return "", fmt.Errorf("error: path '%s' resolves outside the wiki root via symlink parent — access denied", relative)
	}
	return abs, nil
}

func existingParentRealPath(path string) (string, error) {
	parent := filepath.Dir(path)
	for {
		real, err := filepath.EvalSymlinks(parent)
		if err == nil {
			return real, nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", err
		}
		parent = next
	}
}

func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".scrinium-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	removeTemp := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		removeTemp()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		removeTemp()
		return err
	}
	if err := tmp.Close(); err != nil {
		removeTemp()
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		removeTemp()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		removeTemp()
		return err
	}
	return nil
}

func atomicCreateFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".scrinium-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	if err := os.Link(tmpName, path); err != nil {
		return err
	}
	return nil
}
