package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	appsvc "scrinium/internal/app"
	"scrinium/internal/publicapi"
)

type toolCall struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

// ToolCall decodes MCP parameters and dispatches one compatibility tool.
func (s *Server) ToolCall(ctx context.Context, raw json.RawMessage) (any, error) {
	var call toolCall
	if err := publicapi.DecodeToolCall(raw, &call); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if call.Name == "" {
		return nil, fmt.Errorf("missing tool name")
	}
	if call.Arguments == nil {
		call.Arguments = make(map[string]any)
	}
	result, err := s.CallTool(ctx, call.Name, call.Arguments)
	if err != nil && isPublicTool(call.Name) {
		return PublicErrorResult(call.Name, err), nil
	}
	return result, err
}

// CallTool adapts untyped MCP arguments to typed application requests.
func (s *Server) CallTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "claim_create", "claim_get", "claim_list", "claim_update", "claim_add_evidence", "claim_set_validation_policy", "claim_validate", "claim_supersede", "claim_withdraw", "claim_lint":
		return s.callClaimTool(ctx, name, args)
	case "source_register", "source_get", "source_list", "source_refresh", "source_migration_status":
		return s.callSourceTool(ctx, name, args)
	case "session_begin", "session_continue", "session_finish", "session_abandon", "session_list":
		return s.callPublicSessionTool(ctx, name, args)
	case "read_wiki_page", "update_wiki_page", "create_draft", "append_log":
		return s.callDocumentTool(ctx, name, args)
	case "setup_llm_wiki", "lint_llm_wiki", "adopt_llm_wiki":
		return s.callOperationalTool(ctx, name, args)
	case "begin_session", "continue_session", "session_status", "finish_session", "abandon_session", "list_active_sessions":
		return s.callSessionTool(ctx, name, args)
	case "register_source":
		return s.registerSource(ctx, args)
	case "assess_source_migration":
		report, err := s.app.AssessSourceMigration(ctx)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(report)
	case "apply_source_migration":
		report, err := s.app.ApplySourceMigration(ctx, s.sessionID(args))
		if err != nil {
			return nil, err
		}
		return jsonTextResult(report)
	case "rebuild_source_registry":
		if err := s.app.RebuildSourceRegistry(ctx, s.sessionID(args)); err != nil {
			return nil, err
		}
		return TextResult("Rebuilt source-registry.md from canonical source records"), nil
	case "create_page":
		return s.createPage(ctx, args)
	case "move_page":
		return s.movePage(ctx, args)
	case "archive_page":
		return s.archivePage(ctx, args)
	case "capabilities":
		return s.Capabilities(), nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
}

func isPublicTool(name string) bool {
	return strings.HasPrefix(name, "claim_") || strings.HasPrefix(name, "source_") || strings.HasPrefix(name, "session_")
}

func (s *Server) callDocumentTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "read_wiki_page":
		path, err := stringParam(args, "path")
		if err != nil {
			return nil, err
		}
		page, err := s.app.ReadPage(ctx, appsvc.PageRequest{Path: path, SessionID: s.sessionID(args)})
		if err != nil {
			return nil, err
		}
		return TextResult(page.Content), nil
	case "update_wiki_page":
		path, err := stringParam(args, "path")
		if err != nil {
			return nil, err
		}
		content, err := stringParam(args, "content")
		if err != nil {
			return nil, err
		}
		if err := s.app.UpdatePage(ctx, appsvc.WritePageRequest{Path: path, Content: content, SessionID: s.sessionID(args)}); err != nil {
			return nil, err
		}
		return TextResult(fmt.Sprintf("Successfully wrote %s", path)), nil
	case "create_draft":
		name, err := stringParam(args, "name")
		if err != nil {
			return nil, err
		}
		content, err := stringParam(args, "content")
		if err != nil {
			return nil, err
		}
		path, err := s.app.CreateDraft(ctx, appsvc.DraftRequest{Name: name, Content: content, SessionID: s.sessionID(args)})
		if err != nil {
			return nil, err
		}
		return TextResult(fmt.Sprintf("Draft created at %s", path)), nil
	case "append_log":
		path, err := stringParam(args, "log_file")
		if err != nil {
			return nil, err
		}
		content, err := stringParam(args, "content")
		if err != nil {
			return nil, err
		}
		if err := s.app.Append(ctx, appsvc.AppendRequest{Path: path, Content: content, SessionID: s.sessionID(args)}); err != nil {
			return nil, err
		}
		return TextResult(fmt.Sprintf("Appended to %s", path)), nil
	default:
		return nil, fmt.Errorf("unknown document tool: %s", name)
	}
}

func (s *Server) callOperationalTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "setup_llm_wiki":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		result, err := s.app.SetupWiki(ctx)
		if err != nil {
			return nil, err
		}
		return TextResult(fmt.Sprintf("Initialized llm-wiki structure. Created: %s. Existing unchanged: %s", strings.Join(result.Created, ", "), strings.Join(result.Skipped, ", "))), nil
	case "lint_llm_wiki":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		report, err := s.app.Lint(ctx)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(report)
	case "adopt_llm_wiki":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		report, err := s.app.Adopt(ctx)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(report)
	default:
		return nil, fmt.Errorf("unknown operational tool: %s", name)
	}
}

func (s *Server) callSessionTool(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "begin_session":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		status, err := s.app.BeginSession(ctx)
		if err != nil {
			return nil, err
		}
		s.setCurrentSession(status.SessionID)
		return jsonTextResult(publicapi.SessionResult{SchemaVersion: publicapi.SessionResultSchema, SessionStatus: status})
	case "continue_session":
		id, err := requiredSessionID(s, args)
		if err != nil {
			return nil, err
		}
		status, err := s.app.ContinueSession(ctx, id)
		if err != nil {
			return nil, err
		}
		s.setCurrentSession(id)
		return jsonTextResult(publicapi.SessionResult{SchemaVersion: publicapi.SessionResultSchema, SessionStatus: status})
	case "session_status":
		id, err := requiredSessionID(s, args)
		if err != nil {
			return nil, err
		}
		status, err := s.app.SessionStatus(ctx, id)
		if err != nil {
			return nil, err
		}
		return jsonTextResult(publicapi.SessionResult{SchemaVersion: publicapi.SessionResultSchema, SessionStatus: status})
	case "finish_session", "abandon_session":
		return s.closeSession(ctx, name, args)
	case "list_active_sessions":
		if err := noParams(name, args); err != nil {
			return nil, err
		}
		statuses, err := s.app.ListActiveSessions(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]map[string]any, 0, len(statuses))
		for _, status := range statuses {
			items = append(items, sessionStatusMap(status))
		}
		return jsonTextResult(map[string]any{"schema_version": publicapi.SessionListSchema, "sessions": items})
	default:
		return nil, fmt.Errorf("unknown session tool: %s", name)
	}
}

func (s *Server) closeSession(ctx context.Context, name string, args map[string]any) (any, error) {
	id, err := requiredSessionID(s, args)
	if err != nil {
		return nil, err
	}
	var status appsvc.SessionStatus
	if name == "finish_session" {
		status, err = s.app.FinishSession(ctx, id)
	} else {
		reason, reasonErr := requiredString(args, "reason")
		if reasonErr != nil {
			return nil, reasonErr
		}
		status, err = s.app.AbandonSession(ctx, id, reason)
	}
	if err != nil {
		return nil, err
	}
	if s.sessionID(nil) == id {
		s.setCurrentSession("")
	}
	return jsonTextResult(sessionStatusMap(status))
}

func (s *Server) registerSource(ctx context.Context, args map[string]any) (any, error) {
	sourceID, err := requiredString(args, "source_id")
	if err != nil {
		return nil, err
	}
	title, err := requiredString(args, "title")
	if err != nil {
		return nil, err
	}
	rawPath, err := requiredString(args, "raw_path")
	if err != nil {
		return nil, err
	}
	request := appsvc.RegisterSourceRequest{
		SessionID: s.sessionID(args),
		SourceID:  sourceID, Title: title, RawPath: rawPath,
		SourceType:   optionalString(args, "source_type", "unknown"),
		Origin:       optionalString(args, "origin", ""),
		TrustLevel:   optionalString(args, "trust_level", "unknown"),
		ReceivedDate: optionalString(args, "received_date", "unknown"),
		IngestDate:   optionalString(args, "ingest_date", "unknown"),
		Summary:      optionalString(args, "summary", "Summary pending."),
	}
	summaryPath, err := s.app.RegisterSource(ctx, request)
	if err != nil {
		return nil, err
	}
	return TextResult(fmt.Sprintf("Registered source %s and wrote %s", sourceID, summaryPath)), nil
}

func (s *Server) createPage(ctx context.Context, args map[string]any) (any, error) {
	path, err := requiredString(args, "path")
	if err != nil {
		return nil, err
	}
	content, err := requiredString(args, "content")
	if err != nil {
		return nil, err
	}
	if err := s.app.CreatePage(ctx, appsvc.WritePageRequest{Path: path, Content: content, SessionID: s.sessionID(args)}); err != nil {
		return nil, err
	}
	return TextResult(fmt.Sprintf("Created %s", path)), nil
}

func (s *Server) movePage(ctx context.Context, args map[string]any) (any, error) {
	from, err := requiredString(args, "from")
	if err != nil {
		return nil, err
	}
	to, err := requiredString(args, "to")
	if err != nil {
		return nil, err
	}
	if err := s.app.MovePage(ctx, appsvc.MovePageRequest{From: from, To: to, SessionID: s.sessionID(args)}); err != nil {
		return nil, err
	}
	return TextResult(fmt.Sprintf("Moved %s to %s", from, to)), nil
}

func (s *Server) archivePage(ctx context.Context, args map[string]any) (any, error) {
	path, err := requiredString(args, "path")
	if err != nil {
		return nil, err
	}
	destination, err := s.app.ArchivePage(ctx, appsvc.ArchivePageRequest{Path: path, ArchivePath: optionalString(args, "archive_path", ""), SessionID: s.sessionID(args)})
	if err != nil {
		return nil, err
	}
	return TextResult(fmt.Sprintf("Archived %s to %s. Archived content is historical only: remove it from active working context, do not cite it for current requirements, re-read index.md and the replacement/current page if one exists, update index.md, and append log.md.", path, destination)), nil
}

func noParams(name string, args map[string]any) error {
	if len(args) != 0 {
		return fmt.Errorf("%s does not accept parameters", name)
	}
	return nil
}

func stringParam(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("missing %s parameter", key)
	}
	return value, nil
}

func requiredString(args map[string]any, key string) (string, error) {
	value, err := stringParam(args, key)
	if err != nil || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("missing %s parameter", key)
	}
	return value, nil
}

func optionalString(args map[string]any, key, fallback string) string {
	value, ok := args[key].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func requiredSessionID(s *Server, args map[string]any) (string, error) {
	id := s.sessionID(args)
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("missing session_id parameter; pass an explicit session ID or call begin_session on this MCP connection")
	}
	return id, nil
}

func sessionStatusMap(status appsvc.SessionStatus) map[string]any {
	return map[string]any{
		"session_id":               status.SessionID,
		"repository_identity":      status.RepositoryIdentity,
		"created_at":               status.CreatedAt,
		"updated_at":               status.UpdatedAt,
		"status":                   status.Status,
		"active":                   status.Active,
		"pages_read":               status.PagesRead,
		"pages_written":            status.PagesWritten,
		"new_pages":                status.NewPages,
		"documents_written":        status.DocumentsWritten,
		"claims_written":           status.ClaimsWritten,
		"new_resources":            status.NewResources,
		"missing_required_reads":   status.MissingRequiredReads,
		"needs_log":                status.NeedsLog,
		"needs_index":              status.NeedsIndex,
		"needs_source_registry":    status.NeedsSourceRegistry,
		"abandon_reason":           status.AbandonReason,
		"observed_operations_only": status.ObservedOperations,
	}
}
