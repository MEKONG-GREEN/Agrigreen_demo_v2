package app

// W11.23 (Contact FN-13): port.Directory — nguồn cán bộ thuộc tỉnh của danh bạ dùng chung pf/contact.

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var _ port.Directory = (*Service)(nil)

// StaffDirectory thực hiện port.Directory: mọi hồ sơ (kể cả đã nghỉ — Contact tự lọc) chiếu tại hôm nay. Cổng
// AG_HRM_RECORDS tắt hoặc chưa UseRecords → port.ErrRecordsDisabled.
func (s *Service) StaffDirectory(ctx context.Context) ([]port.DirectoryEntry, error) {
	if s.Rec == nil || !s.Rec.Enabled {
		return nil, port.ErrRecordsDisabled
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return nil, problem.Unauthenticated
	}
	day := s.day(s.Clock.Now())
	var out []port.DirectoryEntry
	err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		heads, err := store.CurrentStaff(ctx, tx)
		if err != nil {
			return err
		}
		evs, err := store.Events(ctx, tx, uuid.Nil)
		if err != nil {
			return err
		}
		pos, err := store.Current(ctx, tx, string(domain.KindPosition))
		if err != nil {
			return err
		}
		names := make(map[string]string, len(pos))
		for _, r := range pos {
			names[r.Code] = r.Name
		}
		by := map[uuid.UUID][]domain.Event{}
		for _, r := range evs {
			by[r.StaffID] = append(by[r.StaffID], r.Event)
		}
		for _, h := range heads {
			snap := domain.Project(by[h.StaffID], day, nil)
			e := port.DirectoryEntry{StaffID: h.StaffID, SubjectRef: h.SubjectRef, Code: h.Code, Type: string(h.Type),
				Status: snap.Status, Unit: snap.Unit, MgmtUnit: snap.MgmtUnit, Commune: snap.Commune, Position: snap.Position,
				PositionName: names[snap.Position], Title: snap.Title}
			if h.AccountRef != nil {
				e.AccountRef = *h.AccountRef
			}
			out = append(out, e)
		}
		return nil
	})
	return out, err
}
