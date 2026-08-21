// Package lint contains the existing wiki lint behavior behind a typed boundary.
package lint

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"scrinium/internal/provenance"
	"scrinium/internal/store"
)

// Finding is one compatibility lint finding.
type Finding struct {
	Severity string `json:"severity"`
	Path     string `json:"path"`
	Code     string `json:"code"`
	Method   string `json:"method"`
	Evidence string `json:"evidence"`
	Fix      string `json:"fix"`
}

// Report is the current lint result shape.
type Report struct {
	OK                   bool      `json:"ok"`
	FilesChecked         int       `json:"files_checked"`
	MissingStandardPages []string  `json:"missing_standard_pages"`
	Findings             []Finding `json:"findings"`
}

// Service evaluates the existing compatibility lint checks.
type Service struct {
	store         *store.Store
	standardPages []string
}

// New creates a lint service with a stable standard-page set.
func New(st *store.Store, standardPages []string) *Service {
	pages := append([]string(nil), standardPages...)
	sort.Strings(pages)
	return &Service{store: st, standardPages: pages}
}

// Build performs the v0.1 lint checks without changing their semantics.
func (s *Service) Build(ctx context.Context) (Report, error) {
	files, err := s.store.List(ctx)
	if err != nil {
		return Report{}, err
	}
	fileSet := make(map[string]bool, len(files))
	for _, path := range files {
		fileSet[path] = true
	}

	missingStandard := make([]string, 0)
	for _, path := range s.standardPages {
		if !fileSet[path] {
			missingStandard = append(missingStandard, path)
		}
	}

	indexContent := ""
	if fileSet["index.md"] {
		data, readErr := s.store.Read(ctx, "index.md")
		if readErr != nil {
			return Report{}, fmt.Errorf("failed to read index.md: %w", readErr)
		}
		indexContent = string(data)
	}

	findings := make([]Finding, 0)
	for _, path := range missingStandard {
		findings = append(findings, finding("high", path, "missing_standard_page", "Standard LLM Wiki page is missing.", "Run setup_llm_wiki or create the page."))
	}
	if !fileSet["log.md"] {
		findings = append(findings, finding("high", "log.md", "missing_log", "Canonical log.md is missing.", "Run setup_llm_wiki or create log.md."))
	}
	for _, path := range files {
		if path == "index.md" || strings.HasPrefix(path, "archive/") {
			continue
		}
		if !strings.Contains(indexContent, path) {
			findings = append(findings, finding("medium", path, "missing_index_reference", "Page is not referenced by index.md.", "Add a one-line entry to index.md or archive the page."))
		}
	}
	for _, path := range files {
		if !strings.HasSuffix(path, ".md") {
			continue
		}
		data, readErr := s.store.Read(ctx, path)
		if readErr != nil {
			return Report{}, fmt.Errorf("failed to read %s: %w", path, readErr)
		}
		text := string(data)
		if isSourceSummary(path) && !strings.Contains(text, "Source ID") && !strings.Contains(text, "source ID") {
			findings = append(findings, finding("medium", path, "missing_source_metadata", "Source summary lacks visible source metadata.", "Add source ID, original path, trust level, and derived pages."))
		}
		if isSourceSummary(path) && sourceInstructionRisk(text) {
			findings = append(findings, heuristicFinding("high", path, "heuristic_source_instruction_review", "Source summary contains instruction-like phrases that may need provenance review.", "Review manually against security/untrusted-sources.md; this finding does not change claim state."))
		}
	}

	return Report{
		OK:                   len(findings) == 0,
		FilesChecked:         len(files),
		MissingStandardPages: missingStandard,
		Findings:             findings,
	}, nil
}

func finding(severity, path, code, evidence, fix string) Finding {
	return Finding{Severity: severity, Path: path, Code: code, Method: "deterministic", Evidence: evidence, Fix: fix}
}

func heuristicFinding(severity, path, code, evidence, fix string) Finding {
	return Finding{Severity: severity, Path: path, Code: code, Method: "heuristic", Evidence: evidence, Fix: fix}
}

func isSourceSummary(path string) bool {
	if !strings.HasPrefix(path, "sources/") || strings.Count(path, "/") != 1 || !strings.HasSuffix(path, ".md") {
		return false
	}
	id := strings.TrimSuffix(strings.TrimPrefix(path, "sources/"), ".md")
	return provenance.ValidSourceID(id)
}

func sourceInstructionRisk(text string) bool {
	lower := strings.ToLower(text)
	riskPhrases := []string{
		"ignore previous instructions",
		"ignore all previous instructions",
		"system prompt",
		"developer message",
		"you must execute",
		"run this command",
	}
	for _, phrase := range riskPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}
