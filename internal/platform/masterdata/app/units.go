package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// ImportResult tổng kết một lần nhập danh mục đơn vị hành chính.
type ImportResult struct {
	Created   int `json:"created"`
	Closed    int `json:"closed"`
	Unchanged int `json:"unchanged"`
}

// ImportUnits nhập tệp danh mục (định dạng agrigreen.admin_units/v1) trong MỘT
// giao dịch: thêm phiên bản mới, đóng hiệu lực phiên bản đang mở, bỏ qua dòng
// không đổi. Nhập lặp cùng tệp không đổi gì. Vai masterdata_admin.
func (s *Service) ImportUnits(ctx context.Context, raw []byte) (ImportResult, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return ImportResult{}, err
	}
	source, units, err := domain.ParseGazetteer(raw)
	if err != nil {
		return ImportResult{}, asProblem(err)
	}
	var res ImportResult
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		res, e = s.importUnits(ctx, tx, p.TenantID, p.ActorRef, source, units)
		return e
	})
	return res, err
}

func (s *Service) importUnits(ctx context.Context, tx pgx.Tx, tid, actor uuid.UUID, source string, units []domain.Unit) (ImportResult, error) {
	var res ImportResult
	if err := store.LockImport(ctx, tx, tid); err != nil {
		return res, err
	}
	now := s.Clock.Now()
	type change struct {
		u    domain.Unit
		kind string
	}
	var changes []change
	for i, u := range units {
		existing, err := store.UnitVersion(ctx, tx, u.Code, u.ValidFrom)
		if err != nil {
			return res, err
		}
		act, err := domain.PlanImport(existing, u)
		if err != nil {
			return res, ErrUnitConflict.WithDetail(fmt.Sprintf("units[%d]: %s từ %s", i, u.Code, u.ValidFrom.Format(time.DateOnly)))
		}
		switch act {
		case domain.ActionUnchanged:
			res.Unchanged++
		case domain.ActionCreate:
			if err := store.InsertUnit(ctx, tx, tid, u, source, actor); err != nil {
				var pg *pgconn.PgError
				if errors.As(err, &pg) && pg.Code == "23P01" {
					return res, ErrUnitOverlap.WithDetail(fmt.Sprintf("units[%d]: %s", i, u.Code))
				}
				return res, err
			}
			res.Created++
			changes = append(changes, change{u, "created"})
		case domain.ActionClose:
			if err := store.CloseUnit(ctx, tx, u, actor, now); err != nil {
				return res, err
			}
			res.Closed++
			changes = append(changes, change{u, "closed"})
		}
	}
	// Kiểm tham chiếu sau khi đã ghi cả tệp — thứ tự dòng trong tệp không quan trọng.
	for i, c := range changes {
		u := c.u
		if c.kind == "created" && u.ParentCode != "" {
			ok, err := store.ParentCovers(ctx, tx, u)
			if err != nil {
				return res, err
			}
			if !ok {
				return res, ErrUnitRef.WithField(fmt.Sprintf("units[%d].parent_code", i), u.Code+" → "+u.ParentCode)
			}
		}
		for _, sc := range u.Successors {
			next, err := store.UnitAt(ctx, tx, sc, *u.ValidTo)
			if err != nil {
				return res, err
			}
			if next == nil {
				return res, ErrUnitRef.WithField(fmt.Sprintf("units[%d].successors", i), u.Code+" → "+sc)
			}
			// ADR-0032 mục 4: successor_codes giữ một kỳ phát hành, bảng kế thừa ghi cùng
			// giao dịch. Tệp nhập chưa mang căn cứ pháp lý chính thức → 'pending_c9' (C-9);
			// dòng 'official' nạp ở W3.5 khi có nguồn chính thức.
			if err := store.InsertSuccession(ctx, tx, tid, port.Succession{
				PredecessorCode: u.Code, PredecessorValidFrom: u.ValidFrom,
				SuccessorCode: next.Code, SuccessorValidFrom: next.ValidFrom, EffectiveOn: *u.ValidTo,
				LegalBasis: source, BasisStatus: port.BasisPendingC9,
			}, actor); err != nil {
				return res, err
			}
		}
	}
	for _, c := range changes {
		if err := enqueueUnitChanged(ctx, tx, tid, c.u, c.kind); err != nil {
			return res, err
		}
	}
	return res, nil
}

func enqueueUnitChanged(ctx context.Context, tx pgx.Tx, tid uuid.UUID, u domain.Unit, kind string) error {
	pl := port.AdminUnitChangedV1{
		Code: u.Code, Name: u.Name, Level: string(u.Level), ValidFrom: u.ValidFrom.Format(time.DateOnly),
		SuccessorCodes: u.Successors, Change: kind,
	}
	if pl.SuccessorCodes == nil {
		pl.SuccessorCodes = []string{}
	}
	if u.ParentCode != "" {
		pc := u.ParentCode
		pl.ParentCode = &pc
	}
	occurred := domain.DayStart(u.ValidFrom)
	if u.ValidTo != nil {
		vt := u.ValidTo.Format(time.DateOnly)
		pl.ValidTo = &vt
		if kind == "closed" {
			occurred = domain.DayStart(*u.ValidTo)
		}
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: port.EventAdminUnitChanged, SchemaVersion: 1,
		OccurredAt: occurred, TraceID: telemetry.TraceID(ctx), Producer: Producer, Payload: pl,
	})
}

// GetUnit đọc đơn vị có hiệu lực tại at.
func (s *Service) GetUnit(ctx context.Context, code string, at time.Time) (domain.Unit, error) {
	p, err := reader(ctx)
	if err != nil {
		return domain.Unit{}, err
	}
	return s.getUnit(ctx, p.TenantID, code, at)
}

func (s *Service) getUnit(ctx context.Context, tid uuid.UUID, code string, at time.Time) (domain.Unit, error) {
	var u *domain.Unit
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		u, e = store.UnitAt(ctx, tx, code, domain.DayOf(at))
		return e
	})
	if err != nil {
		return domain.Unit{}, err
	}
	if u == nil {
		return domain.Unit{}, ErrUnitNotFound
	}
	return *u, nil
}

// Children liệt kê đơn vị con trực tiếp có hiệu lực tại at; parent rỗng → các tỉnh.
func (s *Service) Children(ctx context.Context, parent string, at time.Time, after string, limit int) (Page[domain.Unit], error) {
	p, err := reader(ctx)
	if err != nil {
		return Page[domain.Unit]{}, err
	}
	limit = clampLimit(limit)
	var items []domain.Unit
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if parent != "" {
			if u, e := store.UnitAt(ctx, tx, parent, domain.DayOf(at)); e != nil {
				return e
			} else if u == nil {
				return ErrUnitNotFound
			}
		}
		var e error
		items, e = store.Children(ctx, tx, parent, domain.DayOf(at), after, limit+1)
		return e
	})
	if err != nil {
		return Page[domain.Unit]{}, err
	}
	var pg Page[domain.Unit]
	if len(items) > limit {
		items = items[:limit]
		last := items[limit-1].Code
		pg.Next = &last
	}
	pg.Items = items
	return pg, nil
}

// Tree trả đơn vị code và mọi đơn vị con cháu có hiệu lực tại at (gốc đứng đầu).
func (s *Service) Tree(ctx context.Context, code string, at time.Time) ([]domain.Unit, error) {
	p, err := reader(ctx)
	if err != nil {
		return nil, err
	}
	return s.descendants(ctx, p.TenantID, code, at)
}

func (s *Service) descendants(ctx context.Context, tid uuid.UUID, code string, at time.Time) ([]domain.Unit, error) {
	var us []domain.Unit
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		us, e = store.Descendants(ctx, tx, code, domain.DayOf(at))
		return e
	})
	if err != nil {
		return nil, err
	}
	if len(us) == 0 {
		return nil, ErrUnitNotFound
	}
	return us, nil
}

// CurrentCodes trả mã có hiệu lực tại at tương ứng với mã (có thể đã cũ) code.
func (s *Service) CurrentCodes(ctx context.Context, code string, at time.Time) ([]string, error) {
	p, err := reader(ctx)
	if err != nil {
		return nil, err
	}
	return s.current(ctx, p.TenantID, code, at)
}

func (s *Service) current(ctx context.Context, tid uuid.UUID, code string, at time.Time) ([]string, error) {
	var codes []string
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		codes, e = domain.ResolveCurrent(code, domain.DayOf(at),
			func(c string, d time.Time) (*domain.Unit, error) { return store.UnitAt(ctx, tx, c, d) },
			func(c string, d time.Time) (*domain.Unit, error) { return store.UnitLatestFrom(ctx, tx, c, d) })
		return e
	})
	if errors.Is(err, partyport.ErrUnitNotFound) {
		return nil, ErrUnitNotFound
	}
	return codes, err
}
