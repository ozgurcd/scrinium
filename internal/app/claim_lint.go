package app

import (
	"context"
	"time"

	"scrinium/internal/lint"
)

// LintClaims runs strict canonical-claim integrity checks separately from
// legacy page lint and its heuristic source-instruction scan.
func (s *Service) LintClaims(ctx context.Context, at time.Time) (lint.ClaimReport, error) {
	return s.claimLint.Build(ctx, normalizeInspectionTime(at))
}

func (s *Service) LintSources(ctx context.Context) (lint.SourceReport, error) {
	return s.sourceLint.Build(ctx)
}
