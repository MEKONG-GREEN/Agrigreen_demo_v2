package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Nguồn khai báo sản lượng.
const (
	SourceApp   = "app"
	SourceWeb   = "web"
	SourceExcel = "excel"
	SourceSync  = "sync"
)

// HarvestInput khai báo sản lượng một vụ (HTX US-YIELD-01).
type HarvestInput struct {
	ID             uuid.UUID
	SeasonRef      uuid.UUID // hoặc PlotRef: vụ đang mở của thửa
	PlotRef        uuid.UUID
	HarvestDate    time.Time // rỗng → hôm nay (giờ Việt Nam)
	MassKg         mass.Kg
	MoisturePct    *decimal.Decimal
	ConfirmAnomaly bool // HTX US-YIELD-03: người dùng xác nhận cố ý
	GPS            domain.GPS
	EventTime      time.Time
	DeviceID       *string
	Source         string
	IdemKey        string
}

// HarvestResult là kết quả khai báo.
type HarvestResult struct {
	Harvest  store.Harvest
	Season   store.Season
	Replayed bool
}

// DeclareHarvest khai sản lượng: chỉ khi đến giai đoạn thu hoạch của SOP đã ghim
// (AC-1); đóng vụ ngay trong cùng giao dịch (AC-2); âm → từ chối; 0 hoặc vượt
// ngưỡng theo giống → cropcycle.yield_anomaly cần xác nhận (HTX US-YIELD-03).
func (s *Service) DeclareHarvest(ctx context.Context, in HarvestInput) (HarvestResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return HarvestResult{}, err
	}
	return s.declareHarvest(ctx, a, in)
}

// yieldCheck tính năng suất và kiểm bất thường.
type yieldCheck struct {
	yield     decimal.Decimal
	kind      string
	threshold decimal.Decimal
}

func (s *Service) checkYield(ctx context.Context, m mass.Kg, area decimal.Decimal, variety string, at time.Time, confirm bool) (yieldCheck, error) {
	if m.NonNegative() != nil {
		return yieldCheck{}, ErrYieldNegative
	}
	y := domain.YieldTPerHa(m, area)
	if !domain.ValidYield(y) {
		return yieldCheck{}, problem.Validation.WithField("mass_kg", "năng suất vượt giới hạn lưu trữ")
	}
	thr, err := s.anomalyThreshold(ctx, variety, at)
	if err != nil {
		return yieldCheck{}, err
	}
	c := yieldCheck{yield: y, threshold: thr, kind: domain.Anomaly(m, y, thr)}
	if c.kind != "" && !confirm {
		detail := "Sản lượng bằng 0 — xác nhận lại trước khi lưu."
		if c.kind == domain.AnomalyAbove {
			detail = "Năng suất " + y.StringFixed(2) + " tấn/ha vượt ngưỡng bất thường của giống " + variety + " (> " + thr.String() + " tấn/ha)."
		}
		return c, ErrYieldAnomaly.WithDetail(detail+" Gửi lại với confirm_anomaly = true nếu số liệu là cố ý.").
			With("needs_confirm", true).With("anomaly_kind", c.kind).With("yield_t_per_ha", y.StringFixed(4)).
			With("threshold_t_per_ha", thr.String())
	}
	return c, nil
}

// checkPHI chặn thu hoạch trước khi hết thời gian cách ly của lần phun thuốc gắn
// bước SOP có phi_days (nguyên mẫu BR-06 — an toàn thực phẩm, chặn chứ không cảnh báo).
func checkPHI(ctx context.Context, tx pgx.Tx, season store.Season, harvest time.Time) error {
	v, err := store.VersionByID(ctx, tx, season.SOPVersionID, false)
	if err != nil || v == nil {
		return err
	}
	logs, err := store.LiveLogsOfSeason(ctx, tx, season.SeasonRef)
	if err != nil {
		return err
	}
	for _, l := range logs {
		if l.ActivityCode != domain.ActPhunThuoc || l.SOPStepSeq == nil {
			continue
		}
		st, ok := domain.StepBySeq(v.Steps, *l.SOPStepSeq)
		if !ok || st.PHIDays == nil {
			continue
		}
		earliest := domain.AddDays(businessDay(l.EventTime), *st.PHIDays)
		if harvest.Before(earliest) {
			return ErrPHIViolation.WithDetail("Bước \""+st.Name+"\" ("+domain.FormatDay(businessDay(l.EventTime))+") yêu cầu cách ly "+
				itoa(*st.PHIDays)+" ngày, sớm nhất được thu hoạch từ "+domain.FormatDay(earliest)+".").
				With("earliest_harvest_date", domain.FormatDay(earliest))
		}
	}
	return nil
}

func (s *Service) declareHarvest(ctx context.Context, a access, in HarvestInput) (HarvestResult, error) {
	if in.IdemKey == "" {
		return HarvestResult{}, problem.IdempotencyRequired
	}
	var err error
	if in.ID, err = newID("id", in.ID); err != nil {
		return HarvestResult{}, err
	}
	if in.Source == "" {
		in.Source = SourceApp
	}
	if in.MassKg.NonNegative() != nil {
		return HarvestResult{}, ErrYieldNegative
	}
	if in.MoisturePct != nil && (in.MoisturePct.IsNegative() || in.MoisturePct.GreaterThan(decimal.NewFromInt(100))) {
		return HarvestResult{}, problem.Validation.WithField("moisture_pct", "0–100")
	}
	if err := in.GPS.Normalize(); err != nil {
		return HarvestResult{}, fieldProblem(err)
	}
	if in.EventTime, err = s.eventTime(in.EventTime); err != nil {
		return HarvestResult{}, err
	}
	// Lặp cùng idem_key trả kết quả cũ trước mọi kiểm tra trạng thái vụ.
	if prev, err := s.replayHarvestTx(ctx, a.p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return HarvestResult{}, err
	}
	season, err := s.logSeason(ctx, a.p.TenantID, in.SeasonRef, in.PlotRef)
	if err != nil {
		return HarvestResult{}, err
	}
	if season == nil || !a.sees(season.CommuneCode, season.CooperativeRef, season.HouseholdRef) {
		if in.SeasonRef == uuid.Nil && season == nil {
			return HarvestResult{}, ErrSeasonNotOpen.WithDetail("Lô ruộng chưa mở vụ — không có vụ để khai sản lượng.")
		}
		return HarvestResult{}, ErrSeasonNotFound
	}
	if err := a.canWrite(season.CooperativeRef, season.HouseholdRef); err != nil {
		return HarvestResult{}, err
	}
	if season.Status != port.SeasonOpen {
		return HarvestResult{}, ErrSeasonState.WithDetail("vụ đã khai sản lượng hoặc đã hủy — dùng bản ghi bù để sửa sản lượng")
	}
	today := s.today()
	if today.Before(season.HarvestOpenDate) {
		return HarvestResult{}, ErrHarvestTooEarly.With("harvest_open_date", domain.FormatDay(season.HarvestOpenDate))
	}
	if in.HarvestDate.IsZero() {
		in.HarvestDate = today
	}
	if in.HarvestDate.After(today) || in.HarvestDate.Before(season.SowingDate) {
		return HarvestResult{}, problem.Validation.WithField("harvest_date", "trong khoảng ngày gieo sạ đến hôm nay")
	}
	yc, err := s.checkYield(ctx, in.MassKg, season.AreaHa, season.VarietyCode, in.EventTime, in.ConfirmAnomaly)
	if err != nil {
		return HarvestResult{}, err
	}
	loc, dist := s.locate(ctx, season.PlotRef, in.GPS, in.EventTime)
	now := s.now()
	return s.writeHarvest(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (HarvestResult, error) {
		if prev, err := store.HarvestByID(ctx, tx, in.ID); err != nil {
			return HarvestResult{}, err
		} else if prev != nil {
			return HarvestResult{}, ErrIDReused
		}
		cur, err := store.SeasonByRef(ctx, tx, season.SeasonRef, true)
		if err != nil {
			return HarvestResult{}, err
		}
		if cur.Status != port.SeasonOpen {
			return HarvestResult{}, ErrSeasonState.WithDetail("vụ đã khai sản lượng")
		}
		if err := checkPHI(ctx, tx, *cur, in.HarvestDate); err != nil {
			return HarvestResult{}, err
		}
		h := store.Harvest{
			ID: in.ID, TenantID: a.p.TenantID, SeasonRef: cur.SeasonRef, PlotRef: cur.PlotRef, HouseholdRef: cur.HouseholdRef,
			CooperativeRef: cur.CooperativeRef, CommuneCode: cur.CommuneCode, SeasonCode: cur.SeasonCode, SeasonYear: cur.SeasonYear,
			VarietyCode: cur.VarietyCode, HarvestDate: in.HarvestDate, MassKg: in.MassKg, AreaHa: cur.AreaHa, YieldTPerHa: yc.yield,
			MoisturePct: in.MoisturePct, AnomalyKind: yc.kind, AnomalyConfirmed: yc.kind != "", Source: in.Source, GPS: in.GPS,
			LocationStatus: loc, DistanceM: dist, OnBehalf: a.onBehalf(), EventTime: in.EventTime, ActorRef: a.p.ActorRef,
			DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}
		if yc.kind != "" {
			t := yc.threshold
			h.AnomalyThreshold = &t
		}
		if err := store.InsertHarvest(ctx, tx, &h); err != nil {
			return HarvestResult{}, err
		}
		// Đóng vụ trong cùng giao dịch (HTX US-YIELD-01 AC-2).
		prev, err := store.SeasonEventByID(ctx, tx, cur.CurrentEventID)
		if err != nil {
			return HarvestResult{}, err
		}
		ev := *prev
		ev.ID, ev.Seq, ev.Action, ev.HarvestID, ev.Reason = ids.New(), 0, "completed", h.ID, ""
		ev.SupersedesID, ev.CorrectionReason = nil, nil
		ev.GPS, ev.LocationStatus, ev.DistanceM = in.GPS, loc, dist
		ev.OnBehalf, ev.Channel, ev.EventTime, ev.ActorRef, ev.DeviceID, ev.IdemKey = a.onBehalf(), channelOf(in.Source), in.EventTime, a.p.ActorRef, in.DeviceID, in.IdemKey
		if err := store.InsertSeasonEvent(ctx, tx, &ev); err != nil {
			return HarvestResult{}, err
		}
		completed := store.ApplySeasonEvent(cur, ev)
		if err := store.SaveSeason(ctx, tx, a.p.TenantID, completed); err != nil {
			return HarvestResult{}, err
		}
		if err := afterSeasonChange(ctx, tx, a.p.TenantID, completed, now); err != nil {
			return HarvestResult{}, err
		}
		if err := enqueueHarvest(ctx, tx, h); err != nil {
			return HarvestResult{}, err
		}
		if err := enqueue(ctx, tx, a.p.TenantID, port.EventSeasonCompleted, ev.EventTime, port.SeasonCompletedV1{
			SeasonRef: ev.SeasonRef, EventID: ev.ID, PlotRef: ev.PlotRef, HouseholdRef: uuidPtr(ev.HouseholdRef), CooperativeRef: ev.CooperativeRef,
			CommuneCode: ev.CommuneCode, SeasonCode: ev.SeasonCode, SeasonYear: ev.SeasonYear, VarietyCode: ev.VarietyCode, HarvestID: h.ID,
			HarvestDate: domain.FormatDay(h.HarvestDate), AreaHa: h.AreaHa.StringFixed(4), ActorRef: ev.ActorRef, EventTime: ev.EventTime.Format(time.RFC3339),
		}); err != nil {
			return HarvestResult{}, err
		}
		sn, err := store.SeasonByRef(ctx, tx, cur.SeasonRef, false)
		if err != nil {
			return HarvestResult{}, err
		}
		return HarvestResult{Harvest: h, Season: *sn}, nil
	})
}

func channelOf(source string) string {
	switch source {
	case SourceExcel:
		return ChannelImport
	case SourceSync:
		return ChannelSync
	case SourceWeb:
		return ChannelWeb
	}
	return ChannelApp
}

func enqueueHarvest(ctx context.Context, tx pgx.Tx, h store.Harvest) error {
	return enqueue(ctx, tx, h.TenantID, port.EventHarvestDeclared, h.EventTime, port.HarvestDeclaredV1{
		HarvestID: h.ID, SeasonRef: h.SeasonRef, PlotRef: h.PlotRef, HouseholdRef: uuidPtr(h.HouseholdRef), CooperativeRef: h.CooperativeRef,
		CommuneCode: h.CommuneCode, SeasonCode: h.SeasonCode, SeasonYear: h.SeasonYear, VarietyCode: h.VarietyCode,
		HarvestDate: domain.FormatDay(h.HarvestDate), MassKg: h.MassKg.String(), AreaHa: h.AreaHa.StringFixed(4),
		YieldTPerHa: h.YieldTPerHa.StringFixed(4), AnomalyKind: strPtr(h.AnomalyKind), AnomalyConfirmed: h.AnomalyConfirmed,
		Source: h.Source, SupersedesID: h.SupersedesID, ActorRef: h.ActorRef, EventTime: h.EventTime.Format(time.RFC3339),
	})
}

func (s *Service) replayHarvest(ctx context.Context, tx pgx.Tx, key string) (*HarvestResult, error) {
	h, err := store.HarvestByIdem(ctx, tx, key)
	if err != nil || h == nil {
		return nil, err
	}
	sn, err := store.SeasonByRef(ctx, tx, h.SeasonRef, false)
	if err != nil || sn == nil {
		return nil, err
	}
	return &HarvestResult{Harvest: *h, Season: *sn, Replayed: true}, nil
}

func (s *Service) replayHarvestTx(ctx context.Context, tid uuid.UUID, key string) (*HarvestResult, error) {
	var prev *HarvestResult
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		prev, e = s.replayHarvest(ctx, tx, key)
		return e
	})
	return prev, err
}

func (s *Service) writeHarvest(ctx context.Context, tid uuid.UUID, key string, fn func(pgx.Tx) (HarvestResult, error)) (HarvestResult, error) {
	var res HarvestResult
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		if prev, err := s.replayHarvest(ctx, tx, key); err != nil {
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
		if prev, rerr := s.replayHarvestTx(ctx, tid, key); rerr == nil && prev != nil {
			return *prev, nil
		}
	}
	return res, err
}

// CorrectHarvestInput sửa khai báo sản lượng bằng bản ghi bù.
type CorrectHarvestInput struct {
	ID             uuid.UUID
	SupersedesID   uuid.UUID
	Reason         string
	HarvestDate    *time.Time
	MassKg         mass.Kg
	MoisturePct    *decimal.Decimal
	ConfirmAnomaly bool
	EventTime      time.Time
	DeviceID       *string
	Source         string
	IdemKey        string
}

// CorrectHarvest ghi khai báo mới thay khai báo hiện hành của vụ (RB-1). Không
// phải khai báo hiện hành → cropcycle.concurrent_edit. Giữ vị trí GPS của bản gốc.
func (s *Service) CorrectHarvest(ctx context.Context, in CorrectHarvestInput) (HarvestResult, error) {
	a, err := s.access(ctx, writeRoles...)
	if err != nil {
		return HarvestResult{}, err
	}
	return s.correctHarvest(ctx, a, in)
}

func (s *Service) correctHarvest(ctx context.Context, a access, in CorrectHarvestInput) (HarvestResult, error) {
	if in.IdemKey == "" {
		return HarvestResult{}, problem.IdempotencyRequired
	}
	if !domain.ValidReason(in.Reason) {
		return HarvestResult{}, problem.Validation.WithField("correction_reason", "bắt buộc, ít nhất 10 ký tự")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	var err error
	if in.ID, err = newID("id", in.ID); err != nil {
		return HarvestResult{}, err
	}
	if in.EventTime, err = s.eventTime(in.EventTime); err != nil {
		return HarvestResult{}, err
	}
	if in.MassKg.NonNegative() != nil {
		return HarvestResult{}, ErrYieldNegative
	}
	if prev, err := s.replayHarvestTx(ctx, a.p.TenantID, in.IdemKey); err != nil || prev != nil {
		if prev != nil {
			return *prev, nil
		}
		return HarvestResult{}, err
	}
	var old *store.Harvest
	if err := s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		old, e = store.HarvestByID(ctx, tx, in.SupersedesID)
		return e
	}); err != nil {
		return HarvestResult{}, err
	}
	if old == nil || !a.sees(old.CommuneCode, old.CooperativeRef, old.HouseholdRef) {
		return HarvestResult{}, ErrHarvestNotFound
	}
	if err := a.canWrite(old.CooperativeRef, old.HouseholdRef); err != nil {
		return HarvestResult{}, err
	}
	hd := old.HarvestDate
	if in.HarvestDate != nil {
		hd = *in.HarvestDate
	}
	if hd.After(s.today()) {
		return HarvestResult{}, problem.Validation.WithField("harvest_date", "không được ở tương lai")
	}
	moist := old.MoisturePct
	if in.MoisturePct != nil {
		moist = in.MoisturePct
	}
	yc, err := s.checkYield(ctx, in.MassKg, old.AreaHa, old.VarietyCode, in.EventTime, in.ConfirmAnomaly)
	if err != nil {
		return HarvestResult{}, err
	}
	src := in.Source
	if src == "" {
		src = old.Source
	}
	return s.writeHarvest(ctx, a.p.TenantID, in.IdemKey, func(tx pgx.Tx) (HarvestResult, error) {
		cur, err := store.SeasonByRef(ctx, tx, old.SeasonRef, true)
		if err != nil {
			return HarvestResult{}, err
		}
		if cur.HarvestID != old.ID {
			return HarvestResult{}, ErrConcurrentEdit.With("current_harvest_id", cur.HarvestID.String())
		}
		if hd.Before(cur.SowingDate) {
			return HarvestResult{}, problem.Validation.WithField("harvest_date", "không được trước ngày gieo sạ")
		}
		if err := checkPHI(ctx, tx, *cur, hd); err != nil {
			return HarvestResult{}, err
		}
		sup, reason := old.ID, in.Reason
		h := *old
		h.ID, h.Seq, h.HarvestDate, h.MassKg, h.YieldTPerHa, h.MoisturePct = in.ID, 0, hd, in.MassKg, yc.yield, moist
		h.AnomalyKind, h.AnomalyConfirmed, h.AnomalyThreshold = yc.kind, yc.kind != "", nil
		if yc.kind != "" {
			t := yc.threshold
			h.AnomalyThreshold = &t
		}
		h.Source, h.OnBehalf, h.EventTime, h.ActorRef, h.DeviceID, h.IdemKey = src, a.onBehalf(), in.EventTime, a.p.ActorRef, in.DeviceID, in.IdemKey
		h.SupersedesID, h.CorrectionReason = &sup, &reason
		if err := store.InsertHarvest(ctx, tx, &h); err != nil {
			return HarvestResult{}, err
		}
		if err := store.SetSeasonHarvest(ctx, tx, cur.SeasonRef, h.ID); err != nil {
			return HarvestResult{}, err
		}
		if err := store.RefreshYield(ctx, tx, cur.SeasonRef); err != nil {
			return HarvestResult{}, err
		}
		if err := enqueueHarvest(ctx, tx, h); err != nil {
			return HarvestResult{}, err
		}
		sn, err := store.SeasonByRef(ctx, tx, cur.SeasonRef, false)
		if err != nil {
			return HarvestResult{}, err
		}
		return HarvestResult{Harvest: h, Season: *sn}, nil
	})
}
