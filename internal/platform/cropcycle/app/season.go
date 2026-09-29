package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Kênh ghi.
const (
	ChannelApp    = "app"
	ChannelWeb    = "web"
	ChannelSync   = "sync"
	ChannelImport = "import"
)

// OpenInput mở vụ mới cho một thửa (HTX US-SEASON-01 — 4 bước: vụ/năm, giống,
// ngày gieo sạ, xác nhận GPS).
type OpenInput struct {
	SeasonRef    uuid.UUID // UUIDv7 máy sinh (ngoại tuyến) hoặc Nil
	EventID      uuid.UUID // id sự kiện (ngoại tuyến = id bản ghi đồng bộ)
	PlotRef      uuid.UUID
	SeasonCode   string
	SeasonYear   int
	VarietyCode  string
	SowingDate   time.Time
	SOPVersionID uuid.UUID // Nil → tự gán theo giống (AC-1)
	CopyPrevious bool      // HTX US-SEASON-03: lấy giống + SOP của vụ vừa hoàn thành
	GPS          domain.GPS
	EventTime    time.Time
	DeviceID     *string
	Channel      string
	IdemKey      string
}

// SeasonResult là kết quả ghi vụ.
type SeasonResult struct {
	Season   store.Season
	Event    store.SeasonEvent
	Replayed bool
}

// OpenSeason mở vụ (nông dân tự mở, quản lý HTX mở hộ).
func (s *Service) OpenSeason(ctx context.Context, in OpenInput) (SeasonResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return SeasonResult{}, err
	}
	return s.openSeason(ctx, a, in)
}

// sopChoice là phiên bản SOP đã chọn cho vụ.
type sopChoice struct {
	template uuid.UUID
	version  store.Version
	harvest  domain.Step
}

// resolveSOP chọn phiên bản SOP: chỉ định rõ, hoặc gắn theo giống (AC-1).
func resolveSOP(ctx context.Context, tx pgx.Tx, versionID uuid.UUID, variety string, coop uuid.UUID) (sopChoice, error) {
	var v *store.Version
	var err error
	if versionID != uuid.Nil {
		if v, err = store.VersionByID(ctx, tx, versionID, false); err != nil {
			return sopChoice{}, err
		}
		if v == nil {
			return sopChoice{}, ErrSOPNotFound
		}
	} else {
		b, err := store.CurrentBinding(ctx, tx, variety, false)
		if err != nil {
			return sopChoice{}, err
		}
		if b == nil {
			return sopChoice{}, ErrNoSOPForVariety.WithDetail("Giống " + variety + " chưa được gắn quy trình SOP chuẩn — chọn SOP khi mở vụ")
		}
		if v, err = store.LatestPublished(ctx, tx, b.TemplateRef); err != nil {
			return sopChoice{}, err
		}
		if v == nil {
			return sopChoice{}, ErrNoSOPForVariety
		}
	}
	t, err := store.TemplateByRef(ctx, tx, v.TemplateRef, false)
	if err != nil {
		return sopChoice{}, err
	}
	if v.Status != domain.VersionPublished || t == nil || t.Status == domain.TemplateRetired {
		return sopChoice{}, ErrSOPNotPublished
	}
	if t.Scope == domain.ScopeHTX && t.CooperativeRef != coop {
		return sopChoice{}, ErrSOPNotFound.WithDetail("quy trình riêng của HTX khác")
	}
	h, ok := domain.HarvestStep(v.Steps)
	if !ok {
		return sopChoice{}, ErrSOPNoHarvestStep
	}
	return sopChoice{template: t.Ref, version: *v, harvest: h}, nil
}

func (s *Service) checkSeasonFields(ctx context.Context, code string, year int, variety string) (*mdport.CatalogItem, error) {
	if !domain.ValidSeasonCode(code) {
		return nil, problem.Validation.WithField("season_code", "bắt buộc, [A-Za-z0-9_]{1,40}")
	}
	if year < 2000 || year > 2100 {
		return nil, problem.Validation.WithField("season_year", "năm 2000–2100")
	}
	if _, err := s.catalogItem(ctx, mdport.CatalogSeason, "season_code", code); err != nil {
		return nil, err
	}
	return s.catalogItem(ctx, mdport.CatalogRiceVariety, "variety_code", variety)
}

// previous đọc vụ gần nhất của thửa cho HTX US-SEASON-03 (chỉ khi đã hoàn thành).
func (s *Service) previous(ctx context.Context, tid, plot uuid.UUID) (store.Season, error) {
	var last *store.Season
	if err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		last, e = store.LatestSeasonOfPlot(ctx, tx, plot)
		return e
	}); err != nil {
		return store.Season{}, err
	}
	if last == nil {
		return store.Season{}, ErrNoPreviousSeason
	}
	if last.Status != port.SeasonCompleted {
		return store.Season{}, ErrPreviousNotDone.WithDetail("Lô ruộng đang canh tác vụ hiện hành — chỉ sao chép được khi lô đã hoàn thành vụ")
	}
	return *last, nil
}

func (s *Service) openSeason(ctx context.Context, a access, in OpenInput) (SeasonResult, error) {
	if in.IdemKey == "" {
		return SeasonResult{}, problem.IdempotencyRequired
	}
	var err error
	if in.SeasonRef, err = newID("season_ref", in.SeasonRef); err != nil {
		return SeasonResult{}, err
	}
	if in.EventID, err = newID("id", in.EventID); err != nil {
		return SeasonResult{}, err
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	in.SeasonCode, in.VarietyCode = strings.TrimSpace(in.SeasonCode), strings.TrimSpace(in.VarietyCode)
	pl, err := s.plot(ctx, in.PlotRef)
	if err != nil {
		return SeasonResult{}, err
	}
	if err := a.canWrite(pl.CooperativeRef, pl.HouseholdRef); err != nil {
		return SeasonResult{}, err
	}
	var copied uuid.UUID
	if in.CopyPrevious {
		prev, err := s.previous(ctx, a.p.TenantID, pl.Ref)
		if err != nil {
			return SeasonResult{}, err
		}
		in.VarietyCode, in.SOPVersionID, copied = prev.VarietyCode, prev.SOPVersionID, prev.SeasonRef
	}
	variety, err := s.checkSeasonFields(ctx, in.SeasonCode, in.SeasonYear, in.VarietyCode)
	if err != nil {
		return SeasonResult{}, err
	}
	if in.SowingDate.IsZero() {
		return SeasonResult{}, problem.Validation.WithField("sowing_date", "bắt buộc (YYYY-MM-DD)")
	}
	if err := domain.CheckSowingDate(in.SowingDate, s.today()); err != nil {
		return SeasonResult{}, ErrSowingWindow
	}
	if err := s.checkCoop(ctx, pl.CooperativeRef); err != nil {
		return SeasonResult{}, err
	}
	if err := in.GPS.Normalize(); err != nil {
		return SeasonResult{}, fieldProblem(err)
	}
	if in.EventTime, err = s.eventTime(in.EventTime); err != nil {
		return SeasonResult{}, err
	}
	loc, dist := s.locate(ctx, pl.Ref, in.GPS, in.EventTime)
	now := s.now()
	return s.writeSeason(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (SeasonResult, error) {
		if prev, err := store.SeasonByRef(ctx, tx, in.SeasonRef, false); err != nil {
			return SeasonResult{}, err
		} else if prev != nil {
			return SeasonResult{}, ErrIDReused.WithField("season_ref", "đã dùng cho vụ khác")
		}
		choice, err := resolveSOP(ctx, tx, in.SOPVersionID, in.VarietyCode, pl.CooperativeRef)
		if err != nil {
			return SeasonResult{}, err
		}
		if err := store.LockPlot(ctx, tx, pl.Ref); err != nil {
			return SeasonResult{}, err
		}
		if open, err := store.OpenSeasonOfPlot(ctx, tx, pl.Ref, false); err != nil {
			return SeasonResult{}, err
		} else if open != nil {
			return SeasonResult{}, ErrSeasonAlreadyOpen.With("open_season_ref", open.SeasonRef.String())
		}
		dates := domain.DatesFor(in.SowingDate, choice.harvest)
		ev := store.SeasonEvent{
			ID: in.EventID, TenantID: a.p.TenantID, SeasonRef: in.SeasonRef, Action: "opened", PlotRef: pl.Ref, PlotCode: pl.Code,
			HouseholdRef: pl.HouseholdRef, CooperativeRef: pl.CooperativeRef, CommuneCode: pl.CommuneCode,
			SeasonCode: in.SeasonCode, SeasonYear: in.SeasonYear, VarietyCode: in.VarietyCode,
			SOPTemplateRef: choice.template, SOPVersionID: choice.version.ID, SOPVersionNo: choice.version.VersionNo,
			SowingDate: in.SowingDate, ExpectedHarvestDate: dates.ExpectedHarvest, HarvestOpenDate: dates.HarvestOpen,
			AreaHa: pl.AreaHa, ExpectedYield: expectedYield(variety), CopiedFrom: copied, GPS: in.GPS, LocationStatus: loc,
			DistanceM: dist, OnBehalf: a.onBehalf(), Channel: in.Channel, EventTime: in.EventTime, ActorRef: a.p.ActorRef,
			DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return SeasonResult{}, err
		}
		season := store.ApplySeasonEvent(nil, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, season); err != nil {
			if isUnique(err, "season_current_one_open") {
				return SeasonResult{}, ErrSeasonAlreadyOpen
			}
			return SeasonResult{}, err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, season, now); err != nil {
			return SeasonResult{}, err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventSeasonOpened, ev.EventTime, port.SeasonOpenedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, PlotRef: ev.PlotRef, PlotCode: ev.PlotCode, HouseholdRef: uuidPtr(ev.HouseholdRef),
			CooperativeRef: ev.CooperativeRef, CommuneCode: ev.CommuneCode, SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear,
			VarietyCode: ev.VarietyCode, SOPVersionID: ev.SOPVersionID, SowingDate: domain.FormatDay(ev.SowingDate),
			ExpectedHarvestDate: domain.FormatDay(ev.ExpectedHarvestDate), AreaHa: ev.AreaHa.StringFixed(4),
			LocationStatus: ev.LocationStatus, OnBehalf: ev.OnBehalf, ActorRef: ev.ActorRef, EventTime: ev.EventTime.Format(time.RFC3339),
		}); err != nil {
			return SeasonResult{}, err
		}
		return s.reloadSeason(ctx, tx, ev)
	})
}

// afterSeasonChange cập nhật các bảng chiếu phụ thuộc vụ trong cùng giao dịch (R-21).
func afterSeasonChange(ctx context.Context, tx pgx.Tx, tid uuid.UUID, season store.Season, at time.Time) error {
	if err := refreshArea(ctx, tx, tid, season.CooperativeRef, season.SeasonCode, season.SeasonYear, at); err != nil {
		return err
	}
	return store.RefreshYield(ctx, tx, season.SeasonRef)
}

func (s *Service) reloadSeason(ctx context.Context, tx pgx.Tx, ev store.SeasonEvent) (SeasonResult, error) {
	cur, err := store.SeasonByRef(ctx, tx, ev.SeasonRef, false)
	if err != nil {
		return SeasonResult{}, err
	}
	return SeasonResult{Season: *cur, Event: ev}, nil
}

// writeSeason chạy một lần ghi có chống lặp theo idem_key của sổ season_event.
func (s *Service) writeSeason(ctx context.Context, tid uuid.UUID, key string, fn func(pgx.Tx) (SeasonResult, error)) (SeasonResult, error) {
	replay := func(tx pgx.Tx) (*SeasonResult, error) {
		ev, err := store.SeasonEventByIdem(ctx, tx, key)
		if err != nil || ev == nil {
			return nil, err
		}
		r, err := s.reloadSeason(ctx, tx, *ev)
		if err != nil {
			return nil, err
		}
		r.Replayed = true
		return &r, nil
	}
	var res SeasonResult
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		if prev, err := replay(tx); err != nil {
			return err
		} else if prev != nil {
			res = *prev
			return nil
		}
		var err error
		res, err = fn(tx)
		return err
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *SeasonResult
		if rerr := s.inTx(ctx, tid, func(tx pgx.Tx) error {
			var e error
			prev, e = replay(tx)
			return e
		}); rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	if isUnique(err, "season_current_one_open") {
		return res, ErrSeasonAlreadyOpen
	}
	return res, err
}

// ---------------------------------------------------------------------------
// Mở vụ hàng loạt (HTX US-SEASON-02, US-SEASON-04 AC-2)
// ---------------------------------------------------------------------------

// BulkOpenInput mở vụ cho nhiều thửa trong một lần xác nhận.
type BulkOpenInput struct {
	PlotRefs     []uuid.UUID
	SeasonCode   string
	SeasonYear   int
	VarietyCode  string
	SowingDate   time.Time
	SOPVersionID uuid.UUID
	IdemKey      string
}

// Lý do bỏ qua thửa (HTX US-SEASON-04 AC-2).
const (
	SkipBusy       = "Lô đang canh tác vụ khác"
	SkipOutOfScope = "Lô không thuộc quyền quản lý"
)

// BulkItem là kết quả của một thửa.
type BulkItem struct {
	PlotRef   uuid.UUID
	Status    string // opened | skipped
	Code      string // mã lỗi khi bỏ qua
	Reason    string
	SeasonRef uuid.UUID
}

// MaxBulk là số thửa tối đa mỗi lượt hàng loạt (xử lý đồng bộ dưới 2 giây, R-52).
const MaxBulk = 200

// BulkOpen mở vụ cho các thửa hợp lệ, bỏ qua thửa lỗi kèm lý do.
func (s *Service) BulkOpen(ctx context.Context, in BulkOpenInput) ([]BulkItem, error) {
	a, err := s.access(ctx, managerRoles...)
	if err != nil {
		return nil, err
	}
	if in.IdemKey == "" {
		return nil, problem.IdempotencyRequired
	}
	if len(in.PlotRefs) == 0 || len(in.PlotRefs) > MaxBulk {
		return nil, problem.Validation.WithField("plot_refs", "1–200 lô ruộng")
	}
	var out []BulkItem
	seen := map[uuid.UUID]bool{}
	for _, plot := range in.PlotRefs {
		if seen[plot] {
			continue
		}
		seen[plot] = true
		r, err := s.openSeason(ctx, a, OpenInput{PlotRef: plot, SeasonCode: in.SeasonCode, SeasonYear: in.SeasonYear,
			VarietyCode: in.VarietyCode, SowingDate: in.SowingDate, SOPVersionID: in.SOPVersionID, GPS: domain.GPS{Source: domain.GPSManualPick},
			Channel: ChannelWeb, IdemKey: in.IdemKey + ":" + plot.String()})
		if err != nil {
			item, fatal := skipItem(plot, err)
			if fatal {
				return nil, err
			}
			out = append(out, item)
			continue
		}
		out = append(out, BulkItem{PlotRef: plot, Status: "opened", SeasonRef: r.Season.SeasonRef})
	}
	return out, nil
}

// skipItem quy lỗi nghiệp vụ của một thửa về dòng bỏ qua; lỗi hệ thống → fatal.
func skipItem(plot uuid.UUID, err error) (BulkItem, bool) {
	pr := problem.From(err)
	if pr.Status >= 500 {
		return BulkItem{}, true
	}
	reason := pr.Title
	switch pr.Code {
	case ErrSeasonAlreadyOpen.Code:
		reason = SkipBusy
	case ErrOutOfScope.Code, ErrPlotNotFound.Code, ErrReadOnly.Code:
		reason = SkipOutOfScope
	default:
		if len(pr.Errors) > 0 {
			reason = pr.Errors[0].Field + ": " + pr.Errors[0].Reason
		} else if pr.Detail != "" {
			reason = pr.Detail
		}
	}
	return BulkItem{PlotRef: plot, Status: "skipped", Code: pr.Code, Reason: reason}, false
}

// PreviousConfig là cấu hình vụ vừa hoàn thành của thửa (HTX US-SEASON-03 AC-1).
type PreviousConfig struct {
	SeasonRef    uuid.UUID
	SeasonCode   string
	SeasonYear   int
	VarietyCode  string
	SOPTemplate  uuid.UUID
	SOPVersionID uuid.UUID
	SOPVersionNo int
}

// PreviousSeasonConfig trả giống + SOP của vụ vừa hoàn thành; chặn khi lô đang canh tác (AC-2).
func (s *Service) PreviousSeasonConfig(ctx context.Context, plot uuid.UUID) (PreviousConfig, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return PreviousConfig{}, err
	}
	pl, err := s.plot(ctx, plot)
	if err != nil {
		return PreviousConfig{}, err
	}
	if err := a.canWrite(pl.CooperativeRef, pl.HouseholdRef); err != nil {
		return PreviousConfig{}, err
	}
	prev, err := s.previous(ctx, a.p.TenantID, plot)
	if err != nil {
		return PreviousConfig{}, err
	}
	return PreviousConfig{SeasonRef: prev.SeasonRef, SeasonCode: prev.SeasonCode, SeasonYear: prev.SeasonYear, VarietyCode: prev.VarietyCode,
		SOPTemplate: prev.SOPTemplateRef, SOPVersionID: prev.SOPVersionID, SOPVersionNo: prev.SOPVersionNo}, nil
}

// ---------------------------------------------------------------------------
// Sửa / hủy vụ (bản ghi bù)
// ---------------------------------------------------------------------------

// CorrectSeasonInput sửa vụ đang mở. Trường nil = giữ nguyên.
type CorrectSeasonInput struct {
	EventID      uuid.UUID
	SeasonRef    uuid.UUID
	SupersedesID uuid.UUID
	Reason       string
	VarietyCode  *string
	SowingDate   *time.Time
	SOPVersionID *uuid.UUID
	Cancel       bool // hủy vụ mở nhầm (không có khai báo sản lượng)
	EventTime    time.Time
	DeviceID     *string
	Channel      string
	IdemKey      string
}

// CorrectSeason ghi bản ghi bù thay sự kiện hiện hành của vụ (sửa hoặc hủy).
// supersedes_id phải là sự kiện hiện hành, nếu không → cropcycle.concurrent_edit.
func (s *Service) CorrectSeason(ctx context.Context, in CorrectSeasonInput) (SeasonResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return SeasonResult{}, err
	}
	return s.correctSeason(ctx, a, in)
}

func (s *Service) correctSeason(ctx context.Context, a access, in CorrectSeasonInput) (SeasonResult, error) {
	if in.IdemKey == "" {
		return SeasonResult{}, problem.IdempotencyRequired
	}
	if !domain.ValidReason(in.Reason) {
		return SeasonResult{}, problem.Validation.WithField("correction_reason", "bắt buộc, ít nhất 10 ký tự")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	var err error
	if in.EventID, err = newID("id", in.EventID); err != nil {
		return SeasonResult{}, err
	}
	if in.Channel == "" {
		in.Channel = ChannelApp
	}
	if in.EventTime, err = s.eventTime(in.EventTime); err != nil {
		return SeasonResult{}, err
	}
	var variety *mdport.CatalogItem
	if in.VarietyCode != nil {
		v := strings.TrimSpace(*in.VarietyCode)
		in.VarietyCode = &v
		if variety, err = s.catalogItem(ctx, mdport.CatalogRiceVariety, "variety_code", v); err != nil {
			return SeasonResult{}, err
		}
	}
	if in.SowingDate != nil {
		if err := domain.CheckSowingDate(*in.SowingDate, s.today()); err != nil {
			return SeasonResult{}, ErrSowingWindow
		}
	}
	now := s.now()
	return s.writeSeason(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (SeasonResult, error) {
		cur, err := store.SeasonByRef(ctx, tx, in.SeasonRef, true)
		if err != nil {
			return SeasonResult{}, err
		}
		if cur == nil || !a.sees(cur.CommuneCode, cur.CooperativeRef, cur.HouseholdRef) {
			return SeasonResult{}, ErrSeasonNotFound
		}
		if err := a.canWrite(cur.CooperativeRef, cur.HouseholdRef); err != nil {
			return SeasonResult{}, err
		}
		if cur.Status != port.SeasonOpen {
			return SeasonResult{}, ErrSeasonState.WithDetail("chỉ sửa/hủy được vụ đang mở; vụ đã khai sản lượng thì sửa khai báo sản lượng")
		}
		if in.SupersedesID != cur.CurrentEventID {
			return SeasonResult{}, ErrConcurrentEdit.With("current_event_id", cur.CurrentEventID.String()).With("current_version", cur.Version)
		}
		prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return SeasonResult{}, err
		}
		ev := *prev
		ev.ID, ev.Seq, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey, ev.Channel = in.EventID, 0, in.EventTime, a.p.ActorRef, in.DeviceID, in.IdemKey, in.Channel
		ev.OnBehalf = a.onBehalf()
		sup, reason := cur.CurrentEventID, in.Reason
		ev.SupersedesID, ev.CorrectionReason, ev.HarvestID = &sup, &reason, uuid.Nil
		ev.Action = "corrected"
		if in.Cancel {
			ev.Action, ev.Reason = "cancelled", in.Reason
		} else {
			if in.VarietyCode != nil && *in.VarietyCode != ev.VarietyCode {
				ev.VarietyCode, ev.ExpectedYield = *in.VarietyCode, expectedYield(variety)
				if in.SOPVersionID == nil {
					none := uuid.Nil
					in.SOPVersionID = &none // đổi giống → gán lại SOP theo giống mới
				}
			}
			if in.SOPVersionID != nil {
				choice, err := resolveSOP(ctx, tx, *in.SOPVersionID, ev.VarietyCode, ev.CooperativeRef)
				if err != nil {
					return SeasonResult{}, err
				}
				ev.SOPTemplateRef, ev.SOPVersionID, ev.SOPVersionNo = choice.template, choice.version.ID, choice.version.VersionNo
			}
			if in.SowingDate != nil {
				ev.SowingDate = *in.SowingDate
			}
			v, err := store.VersionByID(ctx, tx, ev.SOPVersionID, false)
			if err != nil {
				return SeasonResult{}, err
			}
			h, _ := domain.HarvestStep(v.Steps)
			d := domain.DatesFor(ev.SowingDate, h)
			ev.ExpectedHarvestDate, ev.HarvestOpenDate = d.ExpectedHarvest, d.HarvestOpen
		}
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return SeasonResult{}, err
		}
		season := store.ApplySeasonEvent(cur, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, season); err != nil {
			return SeasonResult{}, err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, season, now); err != nil {
			return SeasonResult{}, err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventSeasonCorrected, ev.EventTime, port.SeasonCorrectedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, Action: ev.Action, Status: season.Status, PlotRef: ev.PlotRef,
			HouseholdRef: uuidPtr(ev.HouseholdRef), CooperativeRef: ev.CooperativeRef, CommuneCode: ev.CommuneCode,
			SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear, VarietyCode: ev.VarietyCode, SOPVersionID: ev.SOPVersionID,
			SowingDate: domain.FormatDay(ev.SowingDate), ExpectedHarvestDate: domain.FormatDay(ev.ExpectedHarvestDate),
			SupersedesID: sup, CorrectionReason: reason, ActorRef: ev.ActorRef, EventTime: ev.EventTime.Format(time.RFC3339),
		}); err != nil {
			return SeasonResult{}, err
		}
		return s.reloadSeason(ctx, tx, ev)
	})
}

// ---------------------------------------------------------------------------
// Đọc
// ---------------------------------------------------------------------------

// SeasonDetail là vụ kèm lịch sử, tiến độ và sản lượng.
type SeasonDetail struct {
	Season  store.Season
	Events  []store.SeasonEvent
	Yield   *store.YieldRow
	Steps   []domain.Step
	Harvest *store.Harvest
}

// GetSeason đọc vụ trong phạm vi.
func (s *Service) GetSeason(ctx context.Context, ref uuid.UUID) (SeasonDetail, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return SeasonDetail{}, err
	}
	var d SeasonDetail
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cur, err := store.SeasonByRef(ctx, tx, ref, false)
		if err != nil {
			return err
		}
		if cur == nil || !a.sees(cur.CommuneCode, cur.CooperativeRef, cur.HouseholdRef) {
			return ErrSeasonNotFound
		}
		d.Season = *cur
		if d.Events, err = store.SeasonEvents(ctx, tx, ref); err != nil {
			return err
		}
		if d.Yield, err = store.YieldOf(ctx, tx, ref); err != nil {
			return err
		}
		v, err := store.VersionByID(ctx, tx, cur.SOPVersionID, false)
		if err != nil {
			return err
		}
		if v != nil {
			d.Steps = v.Steps
		}
		if cur.HarvestID != uuid.Nil {
			d.Harvest, err = store.HarvestByID(ctx, tx, cur.HarvestID)
		}
		return err
	})
	return d, err
}

// ListSeasonsInput lọc danh sách vụ.
type ListSeasonsInput struct {
	CooperativeRef uuid.UUID
	PlotRef        uuid.UUID
	Status         string
	SeasonCode     string
	SeasonYear     int
	VarietyCode    string
	After          uuid.UUID
	Limit          int
}

// ListSeasons liệt kê vụ trong phạm vi (cursor).
func (s *Service) ListSeasons(ctx context.Context, in ListSeasonsInput) (Page[store.Season], error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return Page[store.Season]{}, err
	}
	limit := clampLimit(in.Limit)
	var items []store.Season
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListSeasons(ctx, tx, store.SeasonFilter{Scope: a.filter(), CooperativeRef: in.CooperativeRef, PlotRef: in.PlotRef,
			Status: in.Status, SeasonCode: in.SeasonCode, SeasonYear: in.SeasonYear, VarietyCode: in.VarietyCode, After: in.After, Limit: limit + 1})
		return e
	})
	if err != nil {
		return Page[store.Season]{}, err
	}
	return pageOf(items, limit, func(x store.Season) uuid.UUID { return x.SeasonRef }), nil
}

// ---------------------------------------------------------------------------
// Cổng cho module khác (port.Stats)
// ---------------------------------------------------------------------------

var _ port.Stats = (*Service)(nil)

// ActivityByCooperative thực hiện port.Stats. Tenant lấy từ context.
func (s *Service) ActivityByCooperative(ctx context.Context, coops []uuid.UUID) (map[uuid.UUID]port.CoopActivity, error) {
	var rows []store.Activity
	err := s.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		rows, e = store.ActivityByCooperative(ctx, tx, coops == nil, coops)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]port.CoopActivity, len(rows))
	for _, r := range rows {
		out[r.CooperativeRef] = port.CoopActivity{CooperativeRef: r.CooperativeRef, OpenSeasons: r.OpenSeasons, PendingLogs: r.PendingLogs}
	}
	return out, nil
}

// SeasonAreas thực hiện port.Stats. Tenant lấy từ context.
func (s *Service) SeasonAreas(ctx context.Context, coop uuid.UUID, seasonCode string, year int) ([]port.CoopSeasonArea, error) {
	var rows []store.CoopArea
	err := s.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SeasonAreas(ctx, tx, coop, seasonCode, year)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]port.CoopSeasonArea, 0, len(rows))
	for _, r := range rows {
		out = append(out, port.CoopSeasonArea{CooperativeRef: r.CooperativeRef, SeasonCode: r.SeasonCode, SeasonYear: r.SeasonYear,
			AreaHa: r.AreaHa, Seasons: r.Seasons, OpenSeasons: r.OpenSeasons, CompletedSeasons: r.CompletedSeasons})
	}
	return out, nil
}
