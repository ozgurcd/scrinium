package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"scrinium/internal/governance"
	"scrinium/internal/integrations/gograph"
	"scrinium/internal/integrations/rulefloor"
	"scrinium/internal/lint"
	"scrinium/internal/session"
	"scrinium/internal/store"
	"scrinium/internal/validation"
)

// Content supplies the compatibility guide and setup templates.
type Content struct {
	Guide         string
	StandardFiles map[string]string
}

// Service coordinates typed application workflows.
type Service struct {
	configPath    string
	config        Config
	store         *store.Store
	governance    *governance.Policy
	sessions      *session.Service
	lint          *lint.Service
	claimLint     *lint.ClaimService
	sourceLint    *lint.SourceService
	claims        *store.ClaimStore
	sources       *store.SourceStore
	repository    *store.Store
	validators    *validation.Registry
	validatorMu   sync.RWMutex
	validatorInfo []ValidatorStatus
	snapshots     *validation.Snapshotter
	standardFiles map[string]string
}

// Open loads repository configuration and composes the application services.
func Open(ctx context.Context, configPath string, content Content) (*Service, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	absConfig, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}
	config, err := loadConfig(absConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if config.WikiRoot == "" {
		return nil, fmt.Errorf("wiki_root must not be empty in %s", absConfig)
	}

	st, err := store.New(filepath.Join(filepath.Dir(absConfig), config.WikiRoot))
	if err != nil {
		return nil, err
	}
	repository, err := store.New(filepath.Dir(absConfig))
	if err != nil {
		return nil, err
	}
	protected := []string(nil)
	if config.WriteGovernance != nil {
		protected = config.WriteGovernance.ProtectedFiles
	}
	policy, err := governance.New(protected)
	if err != nil {
		return nil, fmt.Errorf("invalid write governance: %w", err)
	}

	standardFiles := make(map[string]string, len(content.StandardFiles)+1)
	standardPages := make([]string, 0, len(content.StandardFiles))
	for path, body := range content.StandardFiles {
		standardFiles[path] = body
		standardPages = append(standardPages, path)
	}
	// The compatibility guide is created by setup_llm_wiki like every other
	// standard page — never as a side effect of opening the service. Open
	// (and therefore every operation, including capabilities) must be
	// read-only toward the wiki: the pilot that adopted Scrinium measured a
	// bare `capabilities` call writing scrinium-guide.md into a governed
	// wiki. Kept out of standardPages so the lint standard-page set is
	// unchanged.
	if content.Guide != "" {
		standardFiles["scrinium-guide.md"] = content.Guide
	}
	// Resolve the trusted validation-target allowlist ONCE, with the same
	// confinement policy everywhere: names are abstract (no path
	// characters), every root must be an existing real directory reached
	// without a symlinked final component, and validators only ever READ
	// from it — Scrinium's writes stay under wiki_root.
	targetRoots, err := resolveValidationTargets(filepath.Dir(absConfig), config.ValidationTargets)
	if err != nil {
		return nil, err
	}
	validators := validation.NewRegistry()
	manual := validation.NewManualValidator()
	if err := validators.Register(manual); err != nil {
		return nil, fmt.Errorf("failed to register manual validator: %w", err)
	}
	validatorInfo := []ValidatorStatus{{ID: validation.ManualValidatorID, Available: true, Descriptor: manual.Descriptor(), BindingSchemas: []string{validation.ManualBindingVersion}}}
	rulefloorConfig := rulefloor.Config{Executable: configuredRulefloorExecutable(config), RepositoryRoot: repository.Root(), Targets: targetRoots}
	if adapter, adapterErr := rulefloor.New(ctx, rulefloorConfig); adapterErr == nil {
		if registerErr := validators.Register(adapter); registerErr != nil {
			return nil, fmt.Errorf("failed to register Rulefloor validator: %w", registerErr)
		}
		validatorInfo = append(validatorInfo, ValidatorStatus{ID: rulefloor.ValidatorID, Optional: true, Available: true, Descriptor: adapter.Descriptor(), BindingSchemas: []string{rulefloor.BindingSchemaVersion}})
	} else {
		validatorInfo = append(validatorInfo, ValidatorStatus{ID: rulefloor.ValidatorID, Optional: true, Reason: boundedStatusReason(adapterErr), BindingSchemas: []string{rulefloor.BindingSchemaVersion}})
	}
	gographConfig := gograph.Config{Executable: configuredGographExecutable(config), RepositoryRoot: repository.Root(), Targets: targetRoots}
	if adapter, adapterErr := gograph.New(ctx, gographConfig); adapterErr == nil {
		if registerErr := validators.Register(adapter); registerErr != nil {
			return nil, fmt.Errorf("failed to register Gograph validator: %w", registerErr)
		}
		validatorInfo = append(validatorInfo, ValidatorStatus{ID: gograph.ValidatorID, Optional: true, Available: true, Descriptor: adapter.Descriptor(), BindingSchemas: []string{gograph.BindingSchemaVersion}})
	} else {
		validatorInfo = append(validatorInfo, ValidatorStatus{ID: gograph.ValidatorID, Optional: true, Reason: boundedStatusReason(adapterErr), BindingSchemas: []string{gograph.BindingSchemaVersion}})
	}
	sessions, err := session.New(st, repository.Root())
	if err != nil {
		return nil, fmt.Errorf("failed to initialize durable sessions: %w", err)
	}
	svc := &Service{
		configPath:    absConfig,
		config:        *config,
		store:         st,
		repository:    repository,
		governance:    policy,
		sessions:      sessions,
		claims:        store.NewClaimStore(st),
		sources:       store.NewSourceStore(st),
		validators:    validators,
		validatorInfo: validatorInfo,
		snapshots:     validation.NewSnapshotter(repository, targetRoots),
		standardFiles: standardFiles,
	}
	svc.lint = lint.New(st, standardPages)
	svc.claimLint = lint.NewClaimService(svc.claims, svc.sources, repository, validators, svc.snapshots)
	svc.sourceLint = lint.NewSourceService(svc.sources, repository)

	return svc, nil
}

// Config returns a copy of the loaded configuration.
func (s *Service) Config() Config {
	result := s.config
	if s.config.WriteGovernance != nil {
		result.WriteGovernance = &WriteGovernance{ProtectedFiles: append([]string(nil), s.config.WriteGovernance.ProtectedFiles...)}
	}
	if s.config.Validators != nil {
		result.Validators = &Validators{}
		if s.config.Validators.Rulefloor != nil {
			copyConfig := *s.config.Validators.Rulefloor
			result.Validators.Rulefloor = &copyConfig
		}
		if s.config.Validators.Gograph != nil {
			copyConfig := *s.config.Validators.Gograph
			result.Validators.Gograph = &copyConfig
		}
	}
	if s.config.ValidationTargets != nil {
		result.ValidationTargets = make(map[string]string, len(s.config.ValidationTargets))
		for name, path := range s.config.ValidationTargets {
			result.ValidationTargets[name] = path
		}
	}
	return result
}

// ConfigPath returns the absolute configuration path.
func (s *Service) ConfigPath() string { return s.configPath }

// ValidationTargetNames returns the sorted abstract names of the configured
// validation targets. Names only — resolved paths are machine-local detail
// and never travel in capability documents.
func (s *Service) ValidationTargetNames() []string {
	names := make([]string, 0, len(s.config.ValidationTargets))
	for name := range s.config.ValidationTargets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// resolveValidationTargets canonicalizes the trusted name->path allowlist.
// Relative paths resolve against the configuration directory. Every root
// must be an existing directory whose final path component is not a
// symlink; a name must be abstract (no path separators). Failures are
// configuration errors and fail Open loudly.
func resolveValidationTargets(configDir string, targets map[string]string) (map[string]string, error) {
	if len(targets) == 0 {
		return nil, nil
	}
	resolved := make(map[string]string, len(targets))
	for name, root := range targets {
		if !validTargetName(name) {
			return nil, appError(ErrorInvalid, fmt.Sprintf("invalid validation target name %q: must be a short lowercase name, never a path", name), nil)
		}
		if strings.TrimSpace(root) == "" {
			return nil, appError(ErrorInvalid, fmt.Sprintf("validation target %q has an empty path", name), nil)
		}
		path := root
		if !filepath.IsAbs(path) {
			path = filepath.Join(configDir, path)
		}
		info, err := os.Lstat(path)
		if err != nil {
			return nil, appError(ErrorInvalid, fmt.Sprintf("validation target %q does not resolve: %v", name, err), nil)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, appError(ErrorInvalid, fmt.Sprintf("validation target %q is a symlink; targets must be real directories", name), nil)
		}
		if !info.IsDir() {
			return nil, appError(ErrorInvalid, fmt.Sprintf("validation target %q is not a directory", name), nil)
		}
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, appError(ErrorInvalid, fmt.Sprintf("validation target %q cannot be resolved: %v", name, err), nil)
		}
		resolved[name] = absolute
	}
	return resolved, nil
}

func validTargetName(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	if strings.ContainsAny(name, `/\`) || name != strings.ToLower(name) || strings.Contains(name, "..") {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return name[0] >= 'a' && name[0] <= 'z'
}

// WikiRoot returns the absolute wiki root.
func (s *Service) WikiRoot() string { return s.store.Root() }

// Governance returns the live write-policy view.
func (s *Service) Governance() GovernanceStatus {
	return GovernanceStatus{Enabled: s.config.WriteGovernance != nil, ProtectedFiles: s.governance.ProtectedFiles()}
}

// Documents lists repository-local wiki documents.
func (s *Service) Documents(ctx context.Context) ([]string, error) {
	return s.store.List(ctx)
}

// ReadPage reads one page and records the read in an active session.
func (s *Service) ReadPage(ctx context.Context, req PageRequest) (Page, error) {
	if strings.TrimSpace(req.Path) == "" {
		return Page{}, appError(ErrorInvalid, "missing path parameter", nil)
	}
	data, err := s.store.Read(ctx, req.Path)
	if err != nil {
		return Page{}, storageError(err, "failed to read file: %v", err)
	}
	if err := s.sessions.RecordRead(ctx, req.SessionID, req.Path); err != nil {
		return Page{}, translateSessionError(err)
	}
	return Page{Path: req.Path, Content: string(data)}, nil
}

// UpdatePage replaces one governed page.
func (s *Service) UpdatePage(ctx context.Context, req WritePageRequest) error {
	if strings.TrimSpace(req.Path) == "" {
		return appError(ErrorInvalid, "missing path parameter", nil)
	}
	existed, err := s.store.Exists(ctx, req.Path)
	if err != nil {
		return storageError(err, "failed to stat %s: %v", req.Path, err)
	}
	if !s.governance.AllowsWrite(req.Path) {
		return protectedError(req.Path)
	}
	return translateSessionError(s.sessions.DoWrite(ctx, req.SessionID, []session.Write{{Path: req.Path, ExistedBefore: existed}}, func() (bool, error) {
		if err := s.store.Write(ctx, req.Path, []byte(req.Content), 0644); err != nil {
			return false, storageError(err, "failed to write file: %v", err)
		}
		return true, nil
	}))
}

// CreateDraft writes below drafts/ and rejects names that escape that subtree.
func (s *Service) CreateDraft(ctx context.Context, req DraftRequest) (string, error) {
	if strings.TrimSpace(req.Name) == "" {
		return "", appError(ErrorInvalid, "missing name parameter", nil)
	}
	tracked := normalizePath(filepath.Join("drafts", req.Name))
	if tracked == "drafts" || !strings.HasPrefix(tracked, "drafts/") {
		return "", appError(ErrorGovernance, fmt.Sprintf("error: draft name '%s' escapes the drafts directory — access denied", req.Name), nil)
	}
	existed, err := s.store.Exists(ctx, tracked)
	if err != nil {
		return "", storageError(err, "failed to stat draft %s: %v", req.Name, err)
	}
	if err := s.sessions.DoWrite(ctx, req.SessionID, []session.Write{{Path: tracked, ExistedBefore: existed}}, func() (bool, error) {
		if err := s.store.Write(ctx, tracked, []byte(req.Content), 0644); err != nil {
			return false, storageError(err, "failed to create draft: %v", err)
		}
		return true, nil
	}); err != nil {
		return "", translateSessionError(err)
	}
	return filepath.ToSlash(filepath.Join("drafts", req.Name)), nil
}

// Append appends one governed log entry.
func (s *Service) Append(ctx context.Context, req AppendRequest) error {
	if strings.TrimSpace(req.Path) == "" {
		return appError(ErrorInvalid, "missing log_file parameter", nil)
	}
	if !s.governance.AllowsAppend(req.Path) {
		return appError(ErrorGovernance, fmt.Sprintf("error: '%s' is a directly protected file — append_log cannot modify it", req.Path), nil)
	}
	existed, err := s.store.Exists(ctx, req.Path)
	if err != nil {
		return storageError(err, "failed to stat %s: %v", req.Path, err)
	}
	return translateSessionError(s.sessions.DoWrite(ctx, req.SessionID, []session.Write{{Path: req.Path, ExistedBefore: existed}}, func() (bool, error) {
		if err := s.store.Append(ctx, req.Path, req.Content, 0644); err != nil {
			return false, storageError(err, "failed to write to log file: %v", err)
		}
		return true, nil
	}))
}

// BeginSession creates a fresh durable tracked work session.
func (s *Service) BeginSession(ctx context.Context) (SessionStatus, error) {
	status, err := s.sessions.Begin(ctx)
	if err != nil {
		return SessionStatus{}, translateSessionError(err)
	}
	return status, nil
}

// SessionStatus returns one explicit durable session view.
func (s *Service) SessionStatus(ctx context.Context, id string) (SessionStatus, error) {
	status, err := s.sessions.Get(ctx, id)
	if err != nil {
		return SessionStatus{}, translateSessionError(err)
	}
	return status, nil
}

// ContinueSession verifies that one durable session remains active.
func (s *Service) ContinueSession(ctx context.Context, id string) (SessionStatus, error) {
	status, err := s.sessions.Continue(ctx, id)
	if err != nil {
		return SessionStatus{}, translateSessionError(err)
	}
	return status, nil
}

// FinishSession validates and closes one explicit durable session.
func (s *Service) FinishSession(ctx context.Context, id string) (SessionStatus, error) {
	status, err := s.sessions.Finish(ctx, id)
	if err != nil {
		return SessionStatus{}, translateSessionError(err)
	}
	return status, nil
}

// AbandonSession explicitly closes a session without claiming completion.
func (s *Service) AbandonSession(ctx context.Context, id, reason string) (SessionStatus, error) {
	status, err := s.sessions.Abandon(ctx, id, reason)
	if err != nil {
		return SessionStatus{}, translateSessionError(err)
	}
	return status, nil
}

// ListActiveSessions lists active durable sessions for this repository.
func (s *Service) ListActiveSessions(ctx context.Context) ([]SessionStatus, error) {
	statuses, err := s.sessions.ListActive(ctx)
	if err != nil {
		return nil, translateSessionError(err)
	}
	return statuses, nil
}

// Lint runs the existing deterministic compatibility lint behavior.
func (s *Service) Lint(ctx context.Context) (lint.Report, error) { return s.lint.Build(ctx) }

// Adopt performs the existing read-only adoption scan.
func (s *Service) Adopt(ctx context.Context) (AdoptionReport, error) {
	report, err := s.Lint(ctx)
	if err != nil {
		return AdoptionReport{}, err
	}
	return AdoptionReport{
		Mode:         "adoption_scan",
		MissingPages: report.MissingStandardPages,
		LintFindings: report.Findings,
		Recommendations: []string{
			"Call setup_llm_wiki to add missing standard pages without overwriting existing pages.",
			"Review lint findings before treating the wiki as authoritative.",
			"Resolve contradictions with the owner instead of choosing silently.",
			"Update index.md and append log.md after adoption changes.",
		},
	}, nil
}

// SetupWiki creates missing standard pages without overwriting existing pages.
func (s *Service) SetupWiki(ctx context.Context) (SetupResult, error) {
	result := SetupResult{Created: make([]string, 0, len(s.standardFiles)), Skipped: []string{}}
	paths := make([]string, 0, len(s.standardFiles))
	for path := range s.standardFiles {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		exists, err := s.store.Exists(ctx, path)
		if err != nil {
			return SetupResult{}, storageError(err, "failed to stat %s: %v", path, err)
		}
		if exists {
			result.Skipped = append(result.Skipped, path)
			continue
		}
		if err := s.store.Write(ctx, path, []byte(s.standardFiles[path]), 0644); err != nil {
			return SetupResult{}, storageError(err, "failed to create %s: %v", path, err)
		}
		result.Created = append(result.Created, path)
	}
	return result, nil
}

func protectedError(path string) error {
	return appError(ErrorGovernance, fmt.Sprintf("error: '%s' is a read-only foundational document. You cannot alter project rules. Write your proposed changes to a draft instead", path), nil)
}

func translateSessionError(err error) error {
	if err == nil {
		return nil
	}
	var claimConflict *ClaimConflictError
	if errors.As(err, &claimConflict) {
		return claimConflict
	}
	var sourceConflict *SourceConflictError
	if errors.As(err, &sourceConflict) {
		return sourceConflict
	}
	var application *Error
	if errors.As(err, &application) {
		return application
	}
	var typed *session.Error
	if errors.As(err, &typed) {
		return &SessionError{Code: typed.Code, SessionID: typed.SessionID, Cause: appError(ErrorSession, typed.Error(), err)}
	}
	return &SessionError{Code: session.ErrorStorage, Cause: appError(ErrorSession, err.Error(), err)}
}

func normalizePath(path string) string {
	clean := filepath.ToSlash(filepath.Clean(path))
	clean = strings.TrimPrefix(clean, "./")
	if clean == "." {
		return ""
	}
	return clean
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	return &config, nil
}

func configuredRulefloorExecutable(config *Config) string {
	if config != nil && config.Validators != nil && config.Validators.Rulefloor != nil {
		if executable := strings.TrimSpace(config.Validators.Rulefloor.Executable); executable != "" {
			return executable
		}
	}
	return "rulefloor"
}

func configuredGographExecutable(config *Config) string {
	if config != nil && config.Validators != nil && config.Validators.Gograph != nil {
		if executable := strings.TrimSpace(config.Validators.Gograph.Executable); executable != "" {
			return executable
		}
	}
	return "gograph"
}

func boundedStatusReason(err error) string {
	if err == nil {
		return ""
	}
	const limit = 512
	message := err.Error()
	if len(message) <= limit {
		return message
	}
	return message[:limit] + "..."
}
