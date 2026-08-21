// Package gograph adapts Gograph's versioned JSON CLI to Scrinium's
// transport-independent validation boundary.
package gograph

import (
	"time"

	"scrinium/internal/knowledge"
)

const (
	ValidatorID          = "gograph"
	BindingSchemaVersion = "gograph.binding.v1"
	VersionSchemaVersion = "gograph.version.v1"
	ResultSchemaVersion  = "gograph.validation.v1"
	defaultExecutable    = "gograph"

	maxProcessOutput    = 1 << 20
	maxDiagnosticCount  = 16
	maxDiagnosticLength = 512
)

const (
	predicateSymbolExists   = "symbol_exists"
	predicatePackageImports = "package_imports"
	predicateCallEdge       = "call_edge_exists"
	predicateTypeImplements = "type_implements"

	referenceSymbol  = "symbol"
	referencePackage = "package"
	precisionAST     = "ast"
	precisionPrecise = "precise"
)

type Config struct {
	Executable     string
	RepositoryRoot string
}

type binding struct {
	document    bindingDocument
	fingerprint string
	json        string
}

type bindingDocument struct {
	SchemaVersion     string             `json:"schema_version"`
	Predicate         string             `json:"predicate"`
	Subject           referenceDocument  `json:"subject"`
	Object            *referenceDocument `json:"object,omitempty"`
	RequiredPrecision string             `json:"required_precision"`
}

type referenceDocument struct {
	Language string `json:"language"`
	Kind     string `json:"kind"`
	ID       string `json:"id"`
}

type versionDocument struct {
	SchemaVersion string `json:"schema_version"`
	Version       string `json:"version"`
}

type validationDocument struct {
	SchemaVersion  string             `json:"schema_version"`
	Command        string             `json:"command"`
	GographVersion string             `json:"gograph_version"`
	GeneratedAt    string             `json:"generated_at"`
	Repository     repositoryDocument `json:"repository"`
	Analysis       analysisDocument   `json:"analysis"`
	Request        requestDocument    `json:"request"`
	Evaluation     evaluationDocument `json:"evaluation"`
	Evidence       evidenceDocument   `json:"evidence"`
}

type repositoryDocument struct {
	Root              string `json:"root"`
	SourceFingerprint string `json:"source_fingerprint,omitempty"`
	GitRevision       string `json:"git_revision,omitempty"`
}

type analysisDocument struct {
	GraphSchemaVersion      string  `json:"graph_schema_version,omitempty"`
	SourcePolicyVersion     int     `json:"source_policy_version,omitempty"`
	GraphFingerprint        string  `json:"graph_fingerprint,omitempty"`
	BuildContextFingerprint string  `json:"build_context_fingerprint,omitempty"`
	Mode                    string  `json:"mode,omitempty"`
	Precision               string  `json:"precision,omitempty"`
	Completeness            string  `json:"completeness,omitempty"`
	Freshness               string  `json:"freshness,omitempty"`
	GraphGeneratedAt        *string `json:"graph_generated_at,omitempty"`
}

type requestDocument struct {
	BindingFingerprint string           `json:"binding_fingerprint,omitempty"`
	Binding            *bindingDocument `json:"binding,omitempty"`
}

type evaluationDocument struct {
	Outcome     string               `json:"outcome"`
	Reason      string               `json:"reason"`
	Diagnostics []diagnosticDocument `json:"diagnostics"`
}

type diagnosticDocument struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

type evidenceDocument struct {
	ResolvedSubject  *resolvedReferenceDocument `json:"resolved_subject,omitempty"`
	ResolvedObject   *resolvedReferenceDocument `json:"resolved_object,omitempty"`
	MatchedRelations []matchedRelationDocument  `json:"matched_relations"`
}

type resolvedReferenceDocument struct {
	Kind       string             `json:"kind"`
	ID         string             `json:"id"`
	SymbolKind string             `json:"symbol_kind,omitempty"`
	Locations  []locationDocument `json:"locations"`
}

type matchedRelationDocument struct {
	Kind           string             `json:"kind"`
	SubjectID      string             `json:"subject_id"`
	ObjectID       string             `json:"object_id"`
	Classification string             `json:"classification,omitempty"`
	Locations      []locationDocument `json:"locations"`
}

type locationDocument struct {
	Path   string `json:"path"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type authenticatedDocument struct {
	completedAt time.Time
	metadata    map[string]string
	diagnostics []knowledge.ValidationDiagnostic
}
