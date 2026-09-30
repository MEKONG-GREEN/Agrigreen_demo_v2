package app

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mã lỗi của sổ tổ gom, phân công, ngày gặt, tham số (docs/errors.md, mục mkg/fieldops).
var (
	ErrCrewNotFound     = problem.New(404, "fieldops.crew_not_found", "Không tìm thấy tổ gom")
	ErrCrewUnknown      = problem.New(422, "fieldops.crew_unknown", "Tổ gom không có trong sổ tổ gom của tenant")
	ErrCrewInactive     = problem.New(409, "fieldops.crew_inactive", "Tổ gom đã ngừng hoạt động — không nhận việc (proto FM-07)")
	ErrCrewCodeTaken    = problem.New(409, "fieldops.crew_code_taken", "Mã tổ gom đã dùng cho tổ khác")
	ErrCrewVersion      = problem.New(409, "fieldops.crew_version_conflict", "Tổ gom đã có phiên bản mới hơn — tải lại rồi sửa")
	ErrCrewTransition   = problem.New(409, "fieldops.crew_transition", "Tổ gom không ở trạng thái cho phép thao tác này")
	ErrOverCapacity     = problem.New(409, "fieldops.crew_over_capacity", "Vượt năng lực/ngày của tổ gom (proto FM-06) — chọn tổ khác, đổi ngày, hoặc giám sát ghi override_reason")
	ErrWorkloadRequired = problem.New(422, "fieldops.workload_required", "Thiếu khối lượng việc theo đơn vị năng lực của tổ (expected_bales hoặc expected_ha)")
	ErrBeforeHarvest    = problem.New(422, "fieldops.before_harvest", "Không cuộn rơm trước ngày gặt của thửa (proto FM-01)")
	ErrParamNotFound    = problem.New(404, "fieldops.param_not_found", "Không có phiên bản tham số")
	ErrParamDecided     = problem.New(409, "fieldops.param_already_decided", "Phiên bản tham số đã được duyệt hoặc từ chối")
)

// CrewInput là nội dung tạo / sửa tổ gom.
type CrewInput struct {
	Spec domain.CrewSpec
	// ExpectedVersion: phiên bản client đã xem (sửa, ngừng, bật lại); nil = không kiểm.
	ExpectedVersion *int
	Reason          string
	IdemKey         string
}

// CrewResult là kết quả một thao tác trên tổ gom.
type CrewResult struct {
	Crew  store.Crew
	Event store.CrewEvent
}

// CreateCrew thêm tổ gom (active, phiên bản 1). Vai field_supervisor.
func (s *Service) CreateCrew(ctx context.Context, in CrewInput) (CrewResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return CrewResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return CrewResult{}, false, err
	}
	spec, err := domain.NormalizeCrew(in.Spec)
	if err != nil {
		return CrewResult{}, false, mapErr(err)
	}
	var res CrewResult
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		id := ids.New()
		if taken, err := store.CrewCodeTaken(ctx, tx, spec.Code, id); err != nil {
			return err
		} else if taken {
			return ErrCrewCodeTaken
		}
		e := store.CrewEvent{ID: ids.New(), TenantID: p.TenantID, CrewID: id, Kind: domain.CrewCreated, Version: 1, Spec: spec,
			Status: domain.CrewActive, EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		return s.applyCrew(ctx, tx, &e, &res)
	}, s.replayCrew(ctx, in.IdemKey, &res))
	return res, replayed, err
}

// ReviseCrew ghi phiên bản mới của tổ (tên, thành viên, năng lực, xã phục vụ). Không đổi
// trạng thái. Máy của thành viên cũ/mới nhận thay đổi lệnh gom ở lần pull sau.
func (s *Service) ReviseCrew(ctx context.Context, crewID uuid.UUID, in CrewInput) (CrewResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return CrewResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return CrewResult{}, false, err
	}
	spec, err := domain.NormalizeCrew(in.Spec)
	if err != nil {
		return CrewResult{}, false, mapErr(err)
	}
	return s.crewChange(ctx, p, crewID, domain.CrewRevised, in, func(c store.Crew, e *store.CrewEvent) error {
		e.Spec, e.Status = spec, c.Status
		return nil
	})
}

// SetCrewStatus ngừng (deactivate) hoặc bật lại (reactivate) tổ gom — không bao giờ xóa (RB-1).
// Bắt buộc lý do. Lệnh gom đang giao giữ nguyên; tổ ngừng không nhận việc mới (FM-07).
func (s *Service) SetCrewStatus(ctx context.Context, crewID uuid.UUID, active bool, in CrewInput) (CrewResult, bool, error) {
	p, err := authz.Require(ctx, RoleSupervisor)
	if err != nil {
		return CrewResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return CrewResult{}, false, err
	}
	r, err := domain.CheckReason("reason", in.Reason, true)
	if err != nil {
		return CrewResult{}, false, mapErr(err)
	}
	kind, from, to := domain.CrewDeactivated, domain.CrewActive, domain.CrewInactive
	if active {
		kind, from, to = domain.CrewReactivated, domain.CrewInactive, domain.CrewActive
	}
	return s.crewChange(ctx, p, crewID, kind, in, func(c store.Crew, e *store.CrewEvent) error {
		if c.Status != from {
			return ErrCrewTransition
		}
		e.Spec, e.Status, e.Reason = c.Spec, to, &r
		return nil
	})
}

func (s *Service) crewChange(ctx context.Context, p authz.Principal, crewID uuid.UUID, kind string, in CrewInput,
	mutate func(store.Crew, *store.CrewEvent) error) (CrewResult, bool, error) {
	var res CrewResult
	now := s.Clock.Now()
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		c, err := store.GetCrew(ctx, tx, crewID, true)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrCrewNotFound
		}
		if in.ExpectedVersion != nil && *in.ExpectedVersion != c.Version {
			return ErrCrewVersion.WithDetail("phiên bản hiện hành " + strconv.Itoa(c.Version))
		}
		e := store.CrewEvent{ID: ids.New(), TenantID: p.TenantID, CrewID: crewID, Kind: kind, Version: c.Version + 1,
			EventTime: now, ActorRef: p.ActorRef, IdemKey: in.IdemKey}
		if err := mutate(*c, &e); err != nil {
			return err
		}
		if kind == domain.CrewRevised {
			if taken, err := store.CrewCodeTaken(ctx, tx, e.Spec.Code, crewID); err != nil {
				return err
			} else if taken {
				return ErrCrewCodeTaken
			}
		}
		if err := s.applyCrew(ctx, tx, &e, &res); err != nil {
			return err
		}
		// Thành viên có thể đã đổi: lệnh đang giao cho tổ phải đi xuống lại máy (lọc pull).
		return store.TouchOrdersOfCrew(ctx, tx, crewID)
	}, s.replayCrew(ctx, in.IdemKey, &res))
	return res, replayed, err
}

func (s *Service) applyCrew(ctx context.Context, tx pgx.Tx, e *store.CrewEvent, res *CrewResult) error {
	if err := insert(store.InsertCrewEvent(ctx, tx, e)); err != nil {
		return err
	}
	if err := store.ApplyCrewEvent(ctx, tx, *e); err != nil {
		return err
	}
	c, err := store.GetCrew(ctx, tx, e.CrewID, false)
	if err != nil {
		return err
	}
	res.Crew, res.Event = *c, *e
	return nil
}

func (s *Service) replayCrew(ctx context.Context, idem string, res *CrewResult) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		e, err := store.CrewEventByIdem(ctx, tx, idem)
		if err != nil || e == nil {
			return false, err
		}
		c, err := store.GetCrew(ctx, tx, e.CrewID, false)
		if err != nil || c == nil {
			return false, err
		}
		res.Crew, res.Event = *c, *e
		return true, nil
	}
}

// Crew đọc tổ gom hiện hành và mọi phiên bản.
func (s *Service) Crew(ctx context.Context, id uuid.UUID) (store.Crew, []store.CrewEvent, error) {
	var c *store.Crew
	var hist []store.CrewEvent
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		if c, e = store.GetCrew(ctx, tx, id, false); e != nil || c == nil {
			return e
		}
		hist, e = store.CrewEvents(ctx, tx, id)
		return e
	})
	if err != nil {
		return store.Crew{}, nil, err
	}
	if c == nil {
		return store.Crew{}, nil, ErrCrewNotFound
	}
	return *c, hist, nil
}

// CrewPage là một trang tổ gom.
type CrewPage struct {
	Items []store.Crew
	Next  *string
}

// Crews liệt kê tổ gom theo mã (cursor = mã cuối).
func (s *Service) Crews(ctx context.Context, status, after string, limit int) (CrewPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if status != "" && status != domain.CrewActive && status != domain.CrewInactive {
		return CrewPage{}, problem.Validation.WithField("status", "active | inactive")
	}
	var items []store.Crew
	err := s.read(ctx, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListCrews(ctx, tx, status, after, limit+1)
		return e
	})
	var pg CrewPage
	if err != nil {
		return pg, err
	}
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].Spec.Code
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}
