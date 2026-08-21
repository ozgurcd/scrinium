package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"scrinium/internal/session"
)

// CreatePage creates a new governed page and never overwrites.
func (s *Service) CreatePage(ctx context.Context, req WritePageRequest) error {
	if strings.TrimSpace(req.Path) == "" {
		return appError(ErrorInvalid, "missing path parameter", nil)
	}
	if strings.TrimSpace(req.Content) == "" {
		return appError(ErrorInvalid, "missing content parameter", nil)
	}
	exists, err := s.store.Exists(ctx, req.Path)
	if err != nil {
		return storageError(err, "failed to stat %s: %v", req.Path, err)
	}
	if exists {
		return appError(ErrorConflict, fmt.Sprintf("create_page rejected: %s already exists", req.Path), nil)
	}
	if !s.governance.AllowsWrite(req.Path) {
		return protectedError(req.Path)
	}
	return translateSessionError(s.sessions.DoWrite(ctx, req.SessionID, []session.Write{{Path: req.Path, ExistedBefore: false}}, func() (bool, error) {
		if err := s.store.Write(ctx, req.Path, []byte(req.Content), 0644); err != nil {
			return false, storageError(err, "failed to create %s: %v", req.Path, err)
		}
		return true, nil
	}))
}

// MovePage moves one governed page without overwriting its destination.
func (s *Service) MovePage(ctx context.Context, req MovePageRequest) error {
	if strings.TrimSpace(req.From) == "" {
		return appError(ErrorInvalid, "missing from parameter", nil)
	}
	if strings.TrimSpace(req.To) == "" {
		return appError(ErrorInvalid, "missing to parameter", nil)
	}
	fromExists, err := s.store.Exists(ctx, req.From)
	if err != nil {
		return storageError(err, "failed to stat %s: %v", req.From, err)
	}
	if !fromExists {
		return appError(ErrorConflict, fmt.Sprintf("move_page rejected: %s does not exist", req.From), nil)
	}
	toExists, err := s.store.Exists(ctx, req.To)
	if err != nil {
		return storageError(err, "failed to stat %s: %v", req.To, err)
	}
	if toExists {
		return appError(ErrorConflict, fmt.Sprintf("move_page rejected: destination %s already exists", req.To), nil)
	}
	if !s.governance.AllowsWrite(req.From) {
		return protectedError(req.From)
	}
	if !s.governance.AllowsWrite(req.To) {
		return protectedError(req.To)
	}
	writes := []session.Write{{Path: req.From, ExistedBefore: true}, {Path: req.To, ExistedBefore: false}}
	return translateSessionError(s.sessions.DoWrite(ctx, req.SessionID, writes, func() (bool, error) {
		if err := s.store.Move(ctx, req.From, req.To); err != nil {
			return false, storageError(err, "failed to move %s to %s: %v", req.From, req.To, err)
		}
		return true, nil
	}))
}

// ArchivePage moves a page to its requested or default archive path.
func (s *Service) ArchivePage(ctx context.Context, req ArchivePageRequest) (string, error) {
	destination := req.ArchivePath
	if destination == "" {
		destination = filepath.ToSlash(filepath.Join("archive", normalizePath(req.Path)))
	}
	if err := s.MovePage(ctx, MovePageRequest{From: req.Path, To: destination, SessionID: req.SessionID}); err != nil {
		return "", err
	}
	return destination, nil
}
