package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"scrinium/internal/knowledge"
)

const candidateFence = "```scrinium-claim-candidate"

type MigrationCandidate struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	Statement string `json:"statement"`
}

type MigrationDebt struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Code   string `json:"code"`
	Reason string `json:"reason"`
}

type MigrationReport struct {
	DryRun       bool                 `json:"dry_run"`
	FilesChecked int                  `json:"files_checked"`
	Candidates   []MigrationCandidate `json:"candidates"`
	Debt         []MigrationDebt      `json:"debt"`
}

type taggedCandidate struct {
	ID        string `json:"id"`
	Subject   string `json:"subject"`
	Statement string `json:"statement"`
}

// AssessClaimMigration is read-only and recognizes only explicit JSON fences.
// Untagged prose is reported as debt and never converted.
func (s *Service) AssessClaimMigration(ctx context.Context) (MigrationReport, error) {
	paths, err := s.store.List(ctx)
	if err != nil {
		return MigrationReport{}, err
	}
	report := MigrationReport{DryRun: true, Candidates: []MigrationCandidate{}, Debt: []MigrationDebt{}}
	for _, path := range paths {
		if !strings.HasSuffix(path, ".md") {
			continue
		}
		report.FilesChecked++
		data, err := s.store.Read(ctx, path)
		if err != nil {
			return MigrationReport{}, err
		}
		candidates, debt := assessMarkdown(path, string(data))
		report.Candidates = append(report.Candidates, candidates...)
		report.Debt = append(report.Debt, debt...)
	}
	sort.Slice(report.Candidates, func(i, j int) bool {
		if report.Candidates[i].Path != report.Candidates[j].Path {
			return report.Candidates[i].Path < report.Candidates[j].Path
		}
		return report.Candidates[i].Line < report.Candidates[j].Line
	})
	sort.Slice(report.Debt, func(i, j int) bool {
		if report.Debt[i].Path != report.Debt[j].Path {
			return report.Debt[i].Path < report.Debt[j].Path
		}
		return report.Debt[i].Line < report.Debt[j].Line
	})
	return report, nil
}

func assessMarkdown(path, content string) ([]MigrationCandidate, []MigrationDebt) {
	lines := strings.Split(content, "\n")
	candidates := make([]MigrationCandidate, 0)
	debt := make([]MigrationDebt, 0)
	unconverted := make([]string, 0, len(lines))
	for index := 0; index < len(lines); {
		if strings.TrimSpace(lines[index]) != candidateFence {
			unconverted = append(unconverted, lines[index])
			index++
			continue
		}
		start := index
		index++
		body := make([]string, 0)
		for index < len(lines) && strings.TrimSpace(lines[index]) != "```" {
			body = append(body, lines[index])
			index++
		}
		if index == len(lines) {
			debt = append(debt, MigrationDebt{Path: path, Line: start + 1, Code: "malformed_claim_candidate", Reason: "tagged candidate fence is not closed"})
			break
		}
		index++
		candidate, err := decodeTaggedCandidate([]byte(strings.Join(body, "\n")))
		if err != nil {
			debt = append(debt, MigrationDebt{Path: path, Line: start + 1, Code: "malformed_claim_candidate", Reason: err.Error()})
			continue
		}
		candidates = append(candidates, MigrationCandidate{Path: path, Line: start + 1, ID: candidate.ID, Subject: candidate.Subject, Statement: candidate.Statement})
	}
	if strings.TrimSpace(strings.Join(unconverted, "\n")) != "" {
		debt = append(debt, MigrationDebt{Path: path, Code: "untagged_markdown", Reason: "prose was not converted because it is not an explicit scrinium-claim-candidate JSON block"})
	}
	return candidates, debt
}

func decodeTaggedCandidate(data []byte) (taggedCandidate, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var candidate taggedCandidate
	if err := decoder.Decode(&candidate); err != nil {
		return taggedCandidate{}, fmt.Errorf("invalid candidate JSON: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return taggedCandidate{}, fmt.Errorf("candidate must contain exactly one JSON object")
	}
	if !knowledge.ValidSemanticID(candidate.ID) || strings.TrimSpace(candidate.Subject) == "" || strings.TrimSpace(candidate.Statement) == "" {
		return taggedCandidate{}, fmt.Errorf("candidate requires a valid semantic ID, subject, and statement")
	}
	return candidate, nil
}
