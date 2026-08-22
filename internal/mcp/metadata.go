package mcp

import (
	"encoding/json"

	"scrinium/internal/publicapi"
)

// ToolsList returns the existing public MCP tool names and schemas.
func (s *Server) ToolsList() any {
	session := sessionProperty()
	tools := []map[string]any{
		tool("claim_create", "Create one canonical claim and return its derived state and revision.", properties(session, objectProperty("input", publicapi.ClaimCreateSchema+" object.")), "input"),
		tool("claim_get", "Get one canonical claim with trust state, evidence, validation history, and revision.", properties(property("claim_id", "Immutable semantic claim ID.")), "claim_id"),
		emptyTool("claim_list", "List canonical claims with derived state and revisions, in stable claim-ID order. Accepts an optional scrinium.claim-query/v1 input whose filters (validator_id, binding_reference, target, lifecycle, assessment, freshness, locator_prefix) AND-compose; without input it lists everything."),
		tool("claim_update", "Meaning-preserving claim update using an explicit revision.", properties(session, objectProperty("input", publicapi.ClaimUpdateSchema+" object.")), "input"),
		tool("claim_add_evidence", "Attach typed evidence using an explicit claim revision.", properties(session, objectProperty("input", publicapi.ClaimEvidenceSchema+" object.")), "input"),
		tool("claim_set_validation_policy", "Set or clear generic validation bindings using an explicit revision.", properties(session, objectProperty("input", publicapi.ClaimPolicySchema+" object.")), "input"),
		tool("claim_validate", "Validate one binding or all required bindings through the generic validator registry.", properties(session, objectProperty("input", publicapi.ClaimValidationSchema+" object.")), "input"),
		tool("claim_supersede", "Supersede a claim with an explicit successor and revision.", properties(session, objectProperty("input", publicapi.ClaimSupersedeSchema+" object.")), "input"),
		tool("claim_withdraw", "Withdraw a claim with a reason and explicit revision.", properties(session, objectProperty("input", publicapi.ClaimWithdrawSchema+" object.")), "input"),
		emptyTool("claim_lint", "Run deterministic canonical claim, evidence, lifecycle, source-reference, and validation-integrity lint."),
		tool("source_register", "Register canonical source provenance and exact raw bytes before compatibility views.", properties(session, objectProperty("input", publicapi.SourceRegisterSchema+" object.")), "input"),
		tool("source_get", "Get one canonical source record and revision.", properties(property("source_id", "Immutable source ID.")), "source_id"),
		emptyTool("source_list", "List canonical source records and revisions."),
		tool("source_refresh", "Explicitly accept current raw bytes using source compare-and-swap.", properties(session, objectProperty("input", publicapi.SourceRefreshSchema+" object.")), "input"),
		emptyTool("source_migration_status", "Read-only deterministic legacy source migration status."),
		emptyTool("session_begin", "Begin a durable tracked work-session checklist and return its ID."),
		tool("session_continue", "Continue a durable session explicitly.", properties(session), "session_id"),
		tool("session_finish", "Finish a durable session after observed maintenance is complete.", properties(session), "session_id"),
		tool("session_abandon", "Abandon a durable session with a reason.", properties(session, property("reason", "Required abandonment reason.")), "session_id", "reason"),
		emptyTool("session_list", "List active durable sessions."),
		tool("read_wiki_page", "Deprecated document compatibility: read a human Markdown page by path.", properties(
			property("path", "Relative path to the wiki page (e.g. \"index.md\")."), session), "path"),
		tool("update_wiki_page", "Deprecated document compatibility: write a human Markdown page, subject to governance.", properties(
			property("path", "Relative path to the wiki page."), property("content", "The full content to write."), session), "path", "content"),
		tool("create_draft", "Deprecated document compatibility: propose a governed protected-document change under drafts/.", properties(
			property("name", "Filename for the draft (stored under drafts/)."), property("content", "The draft content."), session), "name", "content"),
		tool("append_log", "Deprecated document compatibility: append to a configured log without overwriting.", properties(
			property("log_file", "Relative path to the log file."), property("content", "Text to append."), session), "log_file", "content"),
		emptyTool("setup_llm_wiki", "Deprecated compatibility setup for the standard llm-wiki structure; existing pages are not overwritten."),
		emptyTool("begin_session", "Deprecated compatibility alias for session_begin."),
		tool("continue_session", "Deprecated compatibility alias for session_continue.", properties(session)),
		tool("session_status", "Report one durable session's observed reads, writes, and pending maintenance.", properties(session)),
		tool("finish_session", "Deprecated compatibility alias for session_finish.", properties(session)),
		tool("abandon_session", "Deprecated compatibility alias for session_abandon.", properties(session, property("reason", "Required reason for abandonment.")), "reason"),
		emptyTool("list_active_sessions", "Deprecated compatibility alias for session_list."),
		emptyTool("lint_llm_wiki", "Deprecated compatibility lint with deterministic page checks and explicitly heuristic source-summary review leads."),
		emptyTool("adopt_llm_wiki", "Deprecated compatibility adoption scan for an existing Markdown wiki."),
		emptyTool("assess_source_migration", "Deprecated compatibility alias for deterministic legacy source migration assessment."),
		tool("apply_source_migration", "Deprecated compatibility apply operation for unambiguous legacy source records.", properties(session)),
		tool("rebuild_source_registry", "Deprecated compatibility operation that regenerates the Markdown source view.", properties(session)),
		tool("register_source", "Deprecated flat-parameter compatibility alias for source_register.", properties(
			property("source_id", "Stable source ID, e.g. SRC-YYYYMMDD-slug."),
			property("title", "Human-readable source title."),
			property("raw_path", "Original raw source path, e.g. raw/inbox/file.md."),
			property("source_type", "Closed source type: project_document, decision, repository_document, external_document, owner_input, other, or unknown."),
			property("origin", "Closed producer origin: project, owner, external, or unknown."),
			property("trust_level", "trusted-project, trusted-owner, external, or unknown."),
			property("received_date", "Date received in YYYY-MM-DD format."),
			property("ingest_date", "Date ingested in YYYY-MM-DD format."),
			property("summary", "Neutral summary for the source page."), session), "source_id", "title", "raw_path"),
		tool("create_page", "Deprecated document compatibility: create a wiki page without overwriting.", properties(
			property("path", "Relative path to the new wiki page."), property("content", "Initial page content."), session), "path", "content"),
		tool("move_page", "Deprecated document compatibility: move or rename a wiki page.", properties(
			property("from", "Existing relative page path."), property("to", "Destination relative page path."), session), "from", "to"),
		tool("archive_page", "Deprecated document compatibility: archive a wiki page under archive/.", properties(
			property("path", "Existing relative page path to archive."), property("archive_path", "Optional archive destination. Defaults to archive/<path>."), property("reason", "Optional reason for the archive operation."), session), "path"),
		emptyTool("capabilities", "Describe what Scrinium is, what tools are available, and what governance rules are active. Call this first to orient yourself."),
	}
	return map[string]any{"tools": tools}
}

func tool(name, description string, props map[string]any, required ...string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{"type": "object", "properties": props, "required": required},
	}
}

func emptyTool(name, description string) map[string]any {
	return map[string]any{
		"name":        name,
		"description": description,
		"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}, "required": []string{}},
	}
}

type propertyDefinition struct {
	name        string
	description string
	kind        string
}

func property(name, description string) propertyDefinition {
	return propertyDefinition{name: name, description: description, kind: "string"}
}

func objectProperty(name, description string) propertyDefinition {
	return propertyDefinition{name: name, description: description, kind: "object"}
}

func sessionProperty() propertyDefinition {
	return property("session_id", "Explicit durable session ID. Optional only when this MCP connection already selected one.")
}

func properties(values ...propertyDefinition) map[string]any {
	result := make(map[string]any, len(values))
	for _, value := range values {
		result[value.name] = map[string]any{"type": value.kind, "description": value.description}
	}
	return result
}

// Capabilities returns the existing agent-oriented compatibility payload.
func (s *Server) Capabilities() any {
	validators := make([]publicapi.ValidatorStatus, 0)
	for _, status := range s.app.ValidatorStatuses() {
		item := publicapi.ValidatorStatus{
			ID: status.ID, Optional: status.Optional, Available: status.Available,
			Reason: status.Reason, BindingSchemas: append([]string(nil), status.BindingSchemas...),
			TrustPresentation: validatorTrustPresentation(status.ID),
		}
		if status.Available {
			item.Descriptor = &publicapi.ValidatorDescriptor{
				ID: status.Descriptor.ID, Version: status.Descriptor.Version,
				SupportedBindingVersions: append([]string(nil), status.Descriptor.SupportedBindingVersions...),
				MaximumAssurance:         status.Descriptor.MaximumAssurance,
			}
		}
		validators = append(validators, item)
	}
	payload := map[string]any{
		"schema_version": publicapi.CapabilitiesSchema,
		"product":        "Scrinium evidence-backed project knowledge system for coding agents.",
		"version":        s.version,
		"instruction": "Store project assertions as canonical claims with explicit evidence and bounded validation. " +
			"Stored Markdown or JSON is not automatically true. Preserve lifecycle, assessment, and freshness as separate fields. " +
			"Use durable session IDs for observed work tracking; sessions are not authentication or proof of compliance.",
		"preferred_operations": []string{
			"claim_create", "claim_get", "claim_list", "claim_update", "claim_add_evidence", "claim_set_validation_policy", "claim_validate", "claim_supersede", "claim_withdraw", "claim_lint",
			"source_register", "source_get", "source_list", "source_refresh", "source_migration_status",
			"session_begin", "session_continue", "session_status", "session_finish", "session_abandon", "session_list",
		},
		"deprecated_compatibility": []string{
			"read_wiki_page", "update_wiki_page", "create_page", "move_page", "archive_page", "create_draft", "append_log",
			"register_source", "assess_source_migration", "apply_source_migration", "rebuild_source_registry", "lint_llm_wiki", "adopt_llm_wiki", "setup_llm_wiki",
			"begin_session", "continue_session", "finish_session", "abandon_session", "list_active_sessions", "enforce-agents",
		},
		"json_schemas": map[string]string{
			"claim_create": publicapi.ClaimCreateSchema, "claim_update": publicapi.ClaimUpdateSchema,
			"claim_add_evidence": publicapi.ClaimEvidenceSchema, "claim_set_validation_policy": publicapi.ClaimPolicySchema,
			"claim_validate": publicapi.ClaimValidationSchema, "claim_supersede": publicapi.ClaimSupersedeSchema,
			"claim_withdraw": publicapi.ClaimWithdrawSchema, "claim_result": publicapi.ClaimResultSchema,
			"claim_list": publicapi.ClaimListSchema, "claim_query": publicapi.ClaimQuerySchema,
			"claim_validation_run": publicapi.ClaimValidationRunSchema,
			"claim_lint":           publicapi.ClaimLintSchema, "source_register": publicapi.SourceRegisterSchema,
			"source_refresh": publicapi.SourceRefreshSchema, "source_result": publicapi.SourceResultSchema,
			"source_list": publicapi.SourceListSchema, "source_migration_status": publicapi.SourceMigrationSchema,
			"session_result": publicapi.SessionResultSchema, "session_list": publicapi.SessionListSchema,
			"capabilities": publicapi.CapabilitiesSchema, "error": publicapi.ErrorSchema,
		},
		"deterministic_guarantees": []string{
			"strict canonical claim and source JSON", "exact-byte revisions and compare-and-swap",
			"repository path confinement and regular-file enforcement", "derived claim state from recorded evidence and validation",
			"deterministic claim, source, lifecycle, reference, fingerprint, and validation-integrity lint",
		},
		"heuristic_review": map[string]any{
			"available": true,
			"scope":     "Legacy source-summary phrase review only; findings are heuristic review leads and never change claim state.",
		},
		"validators": validators,
		// Abstract target NAMES only: resolved paths are machine-local
		// detail and never travel in capability documents.
		"validation_targets": s.app.ValidationTargetNames(),
		"governance": map[string]any{
			"enabled": s.app.Governance().Enabled, "protected_files": s.app.Governance().ProtectedFiles,
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult("failed to serialize capabilities")
	}
	return TextResult(string(data))
}

func validatorTrustPresentation(id string) string {
	switch id {
	case "manual":
		return "Observation-grade only; never machine-verified."
	case "rulefloor":
		return "Static pass is observation-grade integrity evidence; authentic execute pass may be verification-grade for the selected rule only."
	case "gograph":
		return "Observation-grade Go structural evidence only; never proves runtime behavior or business correctness."
	default:
		return "Assurance is capped by the validator descriptor and explicit validation policy."
	}
}
