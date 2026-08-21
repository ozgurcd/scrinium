// Package governance evaluates configured write policy independently of storage.
package governance

import (
	"fmt"
	"path/filepath"
	"strings"
)

// Policy protects repository-relative wiki paths from mutation.
type Policy struct {
	protected []string
}

// New validates and copies the configured protected path patterns.
func New(protected []string) (*Policy, error) {
	patterns := append([]string(nil), protected...)
	for _, pattern := range patterns {
		if _, err := filepath.Match(pattern, ""); err != nil {
			return nil, fmt.Errorf("invalid protected file pattern %q: %w", pattern, err)
		}
	}
	return &Policy{protected: patterns}, nil
}

// ProtectedFiles returns a copy of the configured patterns.
func (p *Policy) ProtectedFiles() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.protected...)
}

// Enabled reports whether governance was configured, even if it has no rules.
func (p *Policy) Enabled() bool {
	return p != nil
}

// AllowsWrite reports whether a normal write may target path.
func (p *Policy) AllowsWrite(path string) bool {
	if p == nil {
		return true
	}

	cleanPath := filepath.Clean(path)
	for _, pattern := range p.protected {
		if matched, _ := filepath.Match(pattern, cleanPath); matched {
			return false
		}
		if strings.HasSuffix(pattern, "/*") {
			dir := strings.TrimSuffix(pattern, "/*")
			if cleanPath == dir || strings.HasPrefix(cleanPath, dir+"/") {
				return false
			}
		}
	}
	return true
}

// AllowsAppend preserves v0.1 append behavior: direct file protections block
// appends, while directory-pattern protections permit them.
func (p *Policy) AllowsAppend(path string) bool {
	if p == nil {
		return true
	}

	cleanPath := filepath.Clean(path)
	for _, pattern := range p.protected {
		if strings.HasSuffix(pattern, "/*") {
			continue
		}
		if matched, _ := filepath.Match(pattern, cleanPath); matched {
			return false
		}
	}
	return true
}
