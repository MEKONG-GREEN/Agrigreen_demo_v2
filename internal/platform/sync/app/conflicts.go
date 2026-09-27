package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ConflictQuery lọc danh sách xung đột (Admin API).
type ConflictQuery struct {
	Status      string // open | resolved | rỗng
	OverdueOnly bool
	OwnerOrgRef *uuid.UUID
	After       uuid.UUID
	Limit       int
}

// ConflictPage là một trang xung đột.
type ConflictPage struct {
	Items []domain.Conflict
	Next  *uuid.UUID
	Now   time.Time
}

// ListConflicts liệt kê sổ đăng ký xung đột của tenant.
func (s *Service) ListConflicts(ctx context.Context, q ConflictQuery) (ConflictPage, error) {
	p, err := authz.Require(ctx, port.RoleConflictResolver)
	if err != nil {
		return ConflictPage{}, err
	}
	if q.Status != "" && q.Status != domain.ConflictOpen && q.Status != domain.ConflictResolved {
		return ConflictPage{}, problem.Validation.WithField("status", "open hoặc resolved")
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	now := s.Clock.Now()
	f := store.ConflictFilter{Status: q.Status, OwnerOrgRef: q.OwnerOrgRef}
	if q.OverdueOnly {
		f.OverdueAt = &now
	}
	var items []domain.Conflict
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListConflicts(ctx, tx, f, q.After, q.Limit+1)
		return e
	})
	if err != nil {
		return ConflictPage{}, err
	}
	pg := ConflictPage{Now: now}
	if len(items) > q.Limit {
		items = items[:q.Limit]
		last := items[q.Limit-1].ID
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}

// GetConflict đọc một xung đột.
func (s *Service) GetConflict(ctx context.Context, id uuid.UUID) (domain.Conflict, error) {
	p, err := authz.Require(ctx, port.RoleConflictResolver)
	if err != nil {
		return domain.Conflict{}, err
	}
	var c *domain.Conflict
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		c, e = store.ConflictByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return domain.Conflict{}, err
	}
	if c == nil {
		return domain.Conflict{}, ErrConflictNotFound
	}
	return *c, nil
}

// ResolveConflict đóng xung đột. Quyết định nghiệp vụ (giữ bản ghi nào) do module
// sở hữu entity áp bằng bản ghi bù khi nhận pf.sync.conflict.resolved.
func (s *Service) ResolveConflict(ctx context.Context, id uuid.UUID, note string, record *uuid.UUID) (domain.Conflict, error) {
	p, err := authz.Require(ctx, port.RoleConflictResolver)
	if err != nil {
		return domain.Conflict{}, err
	}
	note = strings.TrimSpace(note)
	if !domain.ValidReason(note) {
		return domain.Conflict{}, problem.Validation.WithField("note", "bắt buộc, 10–500 ký tự")
	}
	now := s.Clock.Now()
	var c domain.Conflict
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		prev, err := store.ConflictByID(ctx, tx, id, true)
		switch {
		case err != nil:
			return err
		case prev == nil:
			return ErrConflictNotFound
		case prev.Status != domain.ConflictOpen:
			return ErrConflictState
		}
		if err := store.ResolveConflict(ctx, tx, id, p.ActorRef, note, record, now); err != nil {
			return err
		}
		c = *prev
		by := p.ActorRef
		c.Status, c.ResolvedAt, c.ResolvedBy, c.ResolutionNote, c.ResolvedRecordID = domain.ConflictResolved, &now, &by, &note, record
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventConflictResolved, SchemaVersion: 1,
			OccurredAt: now, TraceID: telemetry.TraceID(ctx), Producer: "pf/sync",
			Payload: port.ConflictResolvedV1{ConflictID: id, Entity: c.Entity, ResolvedBy: by, ResolvedAt: now.Format(time.RFC3339),
				ResolvedRecordID: record, Overdue: c.Overdue(now)},
		})
	})
	return c, err
}

// ConflictSLA đếm xung đột đang mở và quá hạn của một tenant tại thời điểm hiện
// tại — hàm cho worker đo chỉ số nghiệp vụ bắt buộc có cảnh báo (mục 19).
// Không cần Principal: worker gọi theo từng tenant.
func (s *Service) ConflictSLA(ctx context.Context, tenantID uuid.UUID) (port.ConflictSLA, error) {
	var out port.ConflictSLA
	now := s.Clock.Now()
	err := tenant.WithTenantTx(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ConflictSLA(ctx, tx, now)
		return e
	})
	return out, err
}
