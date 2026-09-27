package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// AreaInput là nhập tay diện tích canh tác và lịch mùa vụ theo HTX × vụ (FN-06 B3–B5, US-SEA-01).
type AreaInput struct {
	CooperativeRef uuid.UUID
	Season         domain.Season
	AreaHa         *decimal.Decimal
	Calendar       []domain.CalendarEntry
	Lock           bool // khóa ngay để đồng bộ App HTX không ghi đè (BR-03)
	IdemKey        string
}

// AreaResult là kết quả một thao tác ghi sổ diện tích vụ.
type AreaResult struct {
	Area     domain.SeasonArea
	Event    domain.AreaEvent
	Replayed bool
}

var _ port.AreaFeed = (*Service)(nil)

// season kiểm mã vụ trong danh mục và năm vụ.
func (s *Service) season(ctx context.Context, sn domain.Season) (*mdport.CatalogItem, error) {
	if strings.TrimSpace(sn.Code) == "" {
		return nil, problem.Validation.WithField("season_code", "bắt buộc")
	}
	if !domain.ValidYear(sn.Year) {
		return nil, problem.Validation.WithField("season_year", "năm vụ trong khoảng 2000–2100")
	}
	it, err := s.catalogItem(ctx, mdport.CatalogSeason, sn.Code)
	if err != nil {
		return nil, err
	}
	if it == nil {
		return nil, problem.Validation.WithField("season_code", "mã vụ không có trong danh mục season")
	}
	return it, nil
}

func (s *Service) areaReplay(ctx context.Context, tid uuid.UUID, key string) (*AreaResult, error) {
	var res *AreaResult
	err := s.tx(ctx, tid, func(tx pgx.Tx) error {
		ev, err := store.AreaEventByIdem(ctx, tx, key)
		if err != nil || ev == nil {
			return err
		}
		cur, err := store.AreaByID(ctx, tx, ev.Area.ID, false)
		if err != nil {
			return err
		}
		res = &AreaResult{Area: ev.Area, Event: *ev, Replayed: true}
		if cur != nil {
			res.Area = *cur
		}
		return nil
	})
	return res, err
}

// RecordArea nhập tay diện tích & lịch mùa vụ, gắn nhãn [Nhập tay] (US-SEA-01 AC-1).
func (s *Service) RecordArea(ctx context.Context, in AreaInput) (AreaResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return AreaResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return AreaResult{}, err
	}
	if _, err := s.season(ctx, in.Season); err != nil {
		return AreaResult{}, err
	}
	if in.AreaHa == nil || in.AreaHa.IsNegative() {
		return AreaResult{}, problem.Validation.WithField("area_ha", domain.ErrArea.Error())
	}
	for i, e := range in.Calendar {
		it, err := s.catalogItem(ctx, mdport.CatalogFarmStage, e.Stage)
		if err != nil {
			return AreaResult{}, err
		}
		if it == nil {
			return AreaResult{}, problem.Validation.WithField(fmt.Sprintf("calendar[%d].stage", i), "khâu không có trong danh mục farm_stage")
		}
	}
	if err := domain.ValidateCalendar(in.Calendar); err != nil {
		return AreaResult{}, fieldProblem(err)
	}
	ci, err := s.activeCoop(ctx, sc, in.CooperativeRef, "cooperative_ref")
	if err != nil {
		return AreaResult{}, err
	}
	if prev, err := s.areaReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return AreaResult{}, err
	}
	now := s.now()
	var res AreaResult
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.AreaByKey(ctx, tx, ci.Ref, in.Season, true)
		if err != nil {
			return err
		}
		a := domain.SeasonArea{ID: ids.New(), CooperativeRef: ci.Ref, Season: in.Season}
		if cur != nil {
			a = *cur
		}
		if ok, reason := domain.ResolveArea(cur, domain.AreaManual); !ok {
			return ErrState.WithDetail(reason)
		}
		a.CommuneCode, a.AreaHa, a.Source, a.SourceAt, a.Calendar = ci.CommuneCode, *in.AreaHa, domain.AreaManual, now, in.Calendar
		a.Locked = a.Locked || in.Lock
		r, err := writeAreaTx(ctx, tx, p, a, domain.AreaRecorded, "", now, in.IdemKey)
		res = r
		return err
	})
	return res, classify(err)
}

func writeAreaTx(ctx context.Context, tx pgx.Tx, p authz.Principal, a domain.SeasonArea, action domain.AreaAction, reason string, now time.Time, key string) (AreaResult, error) {
	ev := domain.AreaEvent{ID: ids.New(), TenantID: p.TenantID, Area: a, Action: action, Reason: reason, EventTime: now, ActorRef: p.ActorRef, IdemKey: key}
	if err := store.InsertAreaEvent(ctx, tx, &ev); err != nil {
		return AreaResult{}, err
	}
	a.LastSeq = ev.Seq
	if err := store.UpsertArea(ctx, tx, p.TenantID, a); err != nil {
		return AreaResult{}, err
	}
	return AreaResult{Area: a, Event: ev}, nil
}

// LockInput là khóa/mở khóa bản ghi nhập tay (FN-06 B5).
type LockInput struct {
	ID      uuid.UUID
	Reason  string
	IdemKey string
}

// LockArea khóa bản ghi nhập tay để đồng bộ App HTX không ghi đè (FN-06 BR-03, QT-03 mục 3).
func (s *Service) LockArea(ctx context.Context, in LockInput) (AreaResult, error) {
	return s.lockArea(ctx, in, true)
}

// UnlockArea mở khóa: lần đồng bộ App HTX hợp lệ kế tiếp được ưu tiên trở lại.
func (s *Service) UnlockArea(ctx context.Context, in LockInput) (AreaResult, error) {
	return s.lockArea(ctx, in, false)
}

func (s *Service) lockArea(ctx context.Context, in LockInput, lock bool) (AreaResult, error) {
	p, sc, err := s.caller(ctx, writeRoles)
	if err != nil {
		return AreaResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return AreaResult{}, err
	}
	var cur *domain.SeasonArea
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		cur, e = store.AreaByID(ctx, tx, in.ID, false)
		return e
	}); err != nil {
		return AreaResult{}, err
	}
	if cur == nil || s.coopVisible(ctx, sc, cur.CooperativeRef) != nil {
		return AreaResult{}, ErrAreaNotFound
	}
	if prev, err := s.areaReplay(ctx, p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return AreaResult{}, err
	}
	now := s.now()
	var res AreaResult
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		a, err := store.AreaByID(ctx, tx, in.ID, true)
		if err != nil {
			return err
		}
		if a == nil {
			return ErrAreaNotFound
		}
		switch {
		case a.Locked == lock:
			return ErrState.WithDetail(map[bool]string{true: "bản ghi đã khóa", false: "bản ghi chưa khóa"}[lock])
		case lock && a.Source != domain.AreaManual:
			return ErrState.WithDetail("chỉ khóa được bản ghi nhập tay")
		}
		a.Locked = lock
		action := domain.AreaUnlocked
		if lock {
			action = domain.AreaLocked
		}
		r, err := writeAreaTx(ctx, tx, p, *a, action, strings.TrimSpace(in.Reason), now, in.IdemKey)
		res = r
		return err
	})
	return res, classify(err)
}

// ApplySyncedArea thực hiện port.AreaFeed: áp diện tích từ App HTX theo QT-03 —
// ưu tiên App HTX, TRỪ bản ghi Admin đã khóa (giữ nguyên, không ghi sổ).
func (s *Service) ApplySyncedArea(ctx context.Context, in port.SyncedArea) (port.AreaFeedResult, error) {
	p, sc, err := s.caller(ctx, []string{port.RoleCGHSync, port.RoleCGHAdmin})
	if err != nil {
		return port.AreaFeedResult{}, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return port.AreaFeedResult{}, err
	}
	sn := domain.Season{Code: in.SeasonCode, Year: in.SeasonYear}
	if _, err := s.season(ctx, sn); err != nil {
		return port.AreaFeedResult{}, err
	}
	if in.AreaHa.IsNegative() {
		return port.AreaFeedResult{}, problem.Validation.WithField("area_ha", domain.ErrArea.Error())
	}
	ci, err := s.activeCoop(ctx, sc, in.CooperativeRef, "cooperative_ref")
	if err != nil {
		return port.AreaFeedResult{}, err
	}
	now := s.now()
	at := in.SourceAt
	if at.IsZero() {
		at = now
	}
	out := port.AreaFeedResult{}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.AreaEventByIdem(ctx, tx, in.IdemKey); err != nil {
			return err
		} else if prev != nil {
			out.Applied = true
			return nil
		}
		cur, err := store.AreaByKey(ctx, tx, ci.Ref, sn, true)
		if err != nil {
			return err
		}
		if ok, reason := domain.ResolveArea(cur, domain.AreaFromAppHTX); !ok {
			out.Reason = reason
			return nil
		}
		a := domain.SeasonArea{ID: ids.New(), CooperativeRef: ci.Ref, Season: sn}
		if cur != nil {
			a = *cur
		}
		a.CommuneCode, a.AreaHa, a.Source, a.SourceAt, a.Locked = ci.CommuneCode, in.AreaHa, domain.AreaFromAppHTX, at, false
		_, err = writeAreaTx(ctx, tx, p, a, domain.AreaSynced, "", now, in.IdemKey)
		out.Applied = err == nil
		return err
	})
	return out, classify(err)
}

// AreaQuery là tham số liệt kê diện tích vụ.
type AreaQuery struct {
	Season         domain.Season
	CooperativeRef uuid.UUID
}

// ListAreas liệt kê diện tích & lịch mùa vụ của một vụ kèm nhãn nguồn (FN-06 B2).
func (s *Service) ListAreas(ctx context.Context, q AreaQuery) ([]domain.SeasonArea, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return nil, err
	}
	if _, err := s.season(ctx, q.Season); err != nil {
		return nil, err
	}
	if !sc.All && (q.CooperativeRef == uuid.Nil || s.coopVisible(ctx, sc, q.CooperativeRef) != nil) {
		return []domain.SeasonArea{}, nil
	}
	var out []domain.SeasonArea
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.AreasOfSeason(ctx, tx, q.Season, q.CooperativeRef)
		return e
	})
	if out == nil {
		out = []domain.SeasonArea{}
	}
	return out, err
}

// AreaHistory trả Lịch sử thay đổi của một bản ghi diện tích vụ (FN-06 B7, AC-03).
func (s *Service) AreaHistory(ctx context.Context, id uuid.UUID) ([]domain.AreaEvent, error) {
	p, sc, err := s.caller(ctx, readRoles)
	if err != nil {
		return nil, err
	}
	var cur *domain.SeasonArea
	var evs []domain.AreaEvent
	if err := s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if cur, e = store.AreaByID(ctx, tx, id, false); e != nil || cur == nil {
			return e
		}
		evs, e = store.AreaEvents(ctx, tx, id)
		return e
	}); err != nil {
		return nil, err
	}
	if cur == nil || s.coopVisible(ctx, sc, cur.CooperativeRef) != nil {
		return nil, ErrAreaNotFound
	}
	return evs, nil
}
