package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Trạng thái SLA của xung đột (US-SYNC-03, C-8).
const (
	SLAOnTrack        = "on_track"
	SLADueSoon        = "due_soon"
	SLAOverdue        = "overdue"
	SLAResolvedOnTime = "resolved_on_time"
	SLAResolvedLate   = "resolved_late"
)

// DueSoonWindow: xung đột còn hạn dưới 24 giờ là "sắp quá hạn".
const DueSoonWindow = 24 * time.Hour

// Cách xử lý xung đột (US-SYNC-03).
const (
	ResolutionKeepVersion = "keep_version" // giữ một trong các bản ghi của xung đột
	ResolutionCorrection  = "correction"   // tạo bản ghi bù ở module sở hữu entity
)

// ErrResolutionInvalid: cách xử lý không khớp dữ liệu xung đột.
var ErrResolutionInvalid = problem.New(http.StatusUnprocessableEntity, "sync.conflict_resolution_invalid",
	"Cách xử lý xung đột không hợp lệ")

// SLAStatus tính trạng thái SLA của xung đột tại now.
func SLAStatus(c domain.Conflict, now time.Time) string {
	if c.Status == domain.ConflictResolved {
		if c.Overdue(now) {
			return SLAResolvedLate
		}
		return SLAResolvedOnTime
	}
	switch {
	case now.After(c.DueAt):
		return SLAOverdue
	case c.DueAt.Sub(now) <= DueSoonWindow:
		return SLADueSoon
	}
	return SLAOnTrack
}

func applySLAFilter(f *store.ConflictFilter, sla string, now time.Time) error {
	switch sla {
	case "":
	case SLAOverdue:
		f.OverdueAt = &now
	case SLADueSoon:
		w := DueSoonWindow
		f.DueWindow = &w
	case SLAOnTrack:
		t := now.Add(DueSoonWindow)
		f.OnTrackAt = &t
	default:
		return problem.Validation.WithField("sla", "on_track, due_soon hoặc overdue")
	}
	return nil
}

// ConflictDetail là xung đột kèm các bản ghi máy đã gửi (không payload) và các
// phiên bản do module sở hữu entity cung cấp (không dữ liệu cá nhân).
type ConflictDetail struct {
	Conflict domain.Conflict
	Records  []store.Inbound
	Versions []port.ConflictVersion
	// VersionsAvailable = false: module sở hữu không hỗ trợ xem phiên bản.
	VersionsAvailable bool
}

// GetConflictDetail đọc chi tiết một xung đột để đối soát (US-SYNC-03).
func (s *Service) GetConflictDetail(ctx context.Context, id uuid.UUID) (ConflictDetail, error) {
	p, err := authz.Require(ctx, port.RoleConflictResolver)
	if err != nil {
		return ConflictDetail{}, err
	}
	var d ConflictDetail
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		c, err := store.ConflictByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrConflictNotFound
		}
		d.Conflict = *c
		d.Records, err = store.InboundByConflict(ctx, tx, id)
		return err
	})
	if err != nil {
		return ConflictDetail{}, err
	}
	d.Versions = []port.ConflictVersion{}
	if v, ok := s.sinks[d.Conflict.Entity].(port.ConflictViewer); ok {
		d.VersionsAvailable = true
		sc, err := s.scope(ctx)
		if err != nil {
			return ConflictDetail{}, err
		}
		vs, err := v.ConflictVersions(ctx, sc, id)
		switch {
		case errors.Is(err, port.ErrConflictUnknown):
		case err != nil:
			return ConflictDetail{}, err
		default:
			d.Versions = vs
		}
	}
	return d, nil
}

// ResolutionOf suy ra cách xử lý của xung đột đã đóng từ bản ghi được chọn.
func ResolutionOf(c domain.Conflict, records []store.Inbound) string {
	if c.ResolvedRecordID == nil {
		return ""
	}
	for _, r := range records {
		if r.RecordID != nil && *r.RecordID == *c.ResolvedRecordID {
			return ResolutionKeepVersion
		}
	}
	return ResolutionCorrection
}

// ResolveConflictAs đóng xung đột theo cách xử lý đã chọn (US-SYNC-03):
//   - keep_version: resolved_record_id bắt buộc, phải là một bản ghi của xung đột;
//   - correction: resolved_record_id (tùy chọn) là bản ghi bù đã tạo ở module sở hữu,
//     không được là bản ghi của chính xung đột;
//   - rỗng: như trước — chỉ đóng kèm ghi chú.
func (s *Service) ResolveConflictAs(ctx context.Context, id uuid.UUID, note string, record *uuid.UUID, resolution string) (domain.Conflict, error) {
	if resolution == "" {
		return s.resolve(ctx, id, note, record, "")
	}
	p, err := authz.Require(ctx, port.RoleConflictResolver)
	if err != nil {
		return domain.Conflict{}, err
	}
	var records []store.Inbound
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		records, e = store.InboundByConflict(ctx, tx, id)
		return e
	}); err != nil {
		return domain.Conflict{}, err
	}
	inConflict := func(r uuid.UUID) bool {
		for _, in := range records {
			if in.RecordID != nil && *in.RecordID == r {
				return true
			}
		}
		return false
	}
	switch resolution {
	case ResolutionKeepVersion:
		if record == nil || !inConflict(*record) {
			return domain.Conflict{}, ErrResolutionInvalid.WithField("resolved_record_id", "bắt buộc, phải là một bản ghi của xung đột")
		}
	case ResolutionCorrection:
		if record != nil && inConflict(*record) {
			return domain.Conflict{}, ErrResolutionInvalid.WithField("resolved_record_id", "bản ghi bù phải là bản ghi mới, không phải bản ghi của xung đột")
		}
	default:
		return domain.Conflict{}, problem.Validation.WithField("resolution", "keep_version hoặc correction")
	}
	return s.resolve(ctx, id, note, record, resolution)
}
