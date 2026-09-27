package app

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ConflictQuery là tham số liệt kê xung đột.
type ConflictQuery struct {
	Status string // "", open, resolved
	After  uuid.UUID
	Limit  int
}

// ConflictPage là một trang xung đột.
type ConflictPage struct {
	Items []store.Conflict
	Next  *uuid.UUID
}

// ListConflicts liệt kê xung đột ranh giới trong phạm vi người gọi.
func (s *Service) ListConflicts(ctx context.Context, q ConflictQuery) (ConflictPage, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return ConflictPage{}, err
	}
	if q.Status != "" && q.Status != "open" && q.Status != "resolved" {
		return ConflictPage{}, problem.Validation.WithField("status", "phải là open hoặc resolved")
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	if a.sc.Empty() {
		return ConflictPage{Items: []store.Conflict{}}, nil
	}
	var items []store.Conflict
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListConflicts(ctx, tx, store.ConflictFilter{
			All: a.sc.All, Units: a.sc.Units, CoopOnly: a.coopOnly, Coop: a.p.OrgRef, Status: q.Status, After: q.After, Limit: q.Limit + 1,
		})
		return e
	})
	if err != nil {
		return ConflictPage{}, err
	}
	pg := ConflictPage{Items: items}
	if len(items) > q.Limit {
		pg.Items = items[:q.Limit]
		last := pg.Items[q.Limit-1].ID
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []store.Conflict{}
	}
	return pg, nil
}

// ResolveConflict đóng xung đột kèm ghi chú xử lý (≥ 10 ký tự). Khi thửa không
// còn xung đột mở, cờ chờ xem xét được gỡ. Việc sửa hình (nếu cần) là một phiên
// bản ranh giới mới, không nằm ở đây.
func (s *Service) ResolveConflict(ctx context.Context, id uuid.UUID, note string) (store.Conflict, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return store.Conflict{}, err
	}
	if !domain.ValidReason(note) {
		return store.Conflict{}, problem.Validation.WithField("note", "bắt buộc, ít nhất 10 ký tự")
	}
	now := s.now()
	var out store.Conflict
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		c, err := store.ConflictByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if c == nil || !a.sc.Allows(c.CommuneCode) || (a.coopOnly && c.OwnerRef != uuid.Nil && c.OwnerRef != a.p.OrgRef) {
			return ErrConflictNotFound
		}
		if c.Status != "open" {
			return ErrConflictState
		}
		if err := store.ResolveConflict(ctx, tx, id, a.p.ActorRef, strings.TrimSpace(note), now); err != nil {
			return err
		}
		n, err := store.OpenConflicts(ctx, tx, c.PlotRef)
		if err != nil {
			return err
		}
		if n == 0 {
			if err := store.SetNeedsReview(ctx, tx, c.PlotRef, false); err != nil {
				return err
			}
		}
		cur, err := store.ConflictByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		out = *cur
		return nil
	})
	return out, err
}
