package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ port.StaffNames = (*Service)(nil)

// DisplayNames thực hiện port.StaffNames: tên hiển thị của cán bộ trong danh
// bạ theo actor_ref (kể cả cán bộ đã ngừng hoạt động). Tenant lấy từ context.
func (s *Service) DisplayNames(ctx context.Context, refs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(refs) == 0 {
		return out, nil
	}
	err := tenant.FromCtxTx(ctx, s.DB, func(tx pgx.Tx) error {
		var e error
		out, e = store.StaffNames(ctx, tx, refs)
		return e
	})
	return out, err
}

// TaskNames là tên hiển thị kèm danh sách nhiệm vụ (web-backend-gaps G-09).
type TaskNames struct {
	Assignees    map[uuid.UUID]string // cán bộ (danh bạ trực)
	Cooperatives map[uuid.UUID]string // HTX (pf/party)
	Communes     map[string]string    // xã (pf/masterdata)
}

// NamesFor tra tên cán bộ được giao, tên HTX và tên xã của các nhiệm vụ. Lỗi tra
// cứu không chặn danh sách: tên thiếu thì trả rỗng (web hiện mã).
func (s *Service) NamesFor(ctx context.Context, tasks []domain.Task) TaskNames {
	n := TaskNames{Assignees: map[uuid.UUID]string{}, Cooperatives: map[uuid.UUID]string{}, Communes: map[string]string{}}
	var actors []uuid.UUID
	seenA := map[uuid.UUID]bool{}
	for _, t := range tasks {
		if t.AssigneeRef != uuid.Nil && !seenA[t.AssigneeRef] {
			seenA[t.AssigneeRef] = true
			actors = append(actors, t.AssigneeRef)
		}
	}
	if m, err := s.DisplayNames(ctx, actors); err == nil {
		n.Assignees = m
	}
	now := s.now()
	for _, t := range tasks {
		if t.CooperativeRef != uuid.Nil && s.Orgs != nil {
			if _, done := n.Cooperatives[t.CooperativeRef]; !done {
				name := ""
				if o, err := s.Orgs.Get(ctx, t.CooperativeRef); err == nil {
					name = o.Name
				}
				n.Cooperatives[t.CooperativeRef] = name
			}
		}
		if t.CommuneCode != "" && s.Units != nil {
			if _, done := n.Communes[t.CommuneCode]; !done {
				name := ""
				if u, err := s.Units.Get(ctx, t.CommuneCode, now); err == nil {
					name = u.Name
				}
				n.Communes[t.CommuneCode] = name
			}
		}
	}
	return n
}
