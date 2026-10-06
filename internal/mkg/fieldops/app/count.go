package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// CountInput là một bản ghi đếm cuộn (gốc hoặc bù).
type CountInput struct {
	// ID: UUIDv7 do máy sinh (app hiện trường, R-11); nil → server sinh.
	ID      *uuid.UUID
	OrderID uuid.UUID // bản gốc
	PlotRef uuid.UUID // bản gốc
	Stage   string    // bản gốc
	// Supersedes + CorrectionReason: bản ghi bù (RB-1). Lệnh gom/thửa/công đoạn lấy từ bản bị bù.
	Supersedes         *uuid.UUID
	CorrectionReason   string
	Count              domain.Count
	EventTime          time.Time
	DeviceID           *string
	HTXConfirmationRef *uuid.UUID
	IdemKey            string
	// Location (G3-2): dấu GPS tùy chọn của app hiện trường — ghi sổ roll_count_location,
	// không bao giờ chặn lưu (domain.ParseLocation không lỗi).
	Location *domain.Location
	// VesselCode (G4, proto FM-05): số hiệu ghe/sà lan tùy chọn khi xếp ghe (boat_load) — chuẩn hóa như danh
	// mục ghe (GH-01), ghi sổ roll_count_vessel, mang trong roll_count.recorded để mkg/logistics gán phương tiện.
	// Bản bù không gửi số hiệu → giữ số hiệu của bản bị bù.
	VesselCode *string
}

// CountResult là kết quả ghi số.
type CountResult struct {
	Count   store.RollCount
	Total   store.Total
	Recon   *store.Recon
	Exports []store.ValveExport
	// Harvest: kết quả kiểm ngày gặt (FM-01) — chỉ bản ghi cuộn gốc (công đoạn bale).
	Harvest *store.HarvestCheck
	// Warnings: cảnh báo không chặn (vd fieldops.harvest_date_unknown).
	Warnings []string
	// VesselCode: số hiệu ghe đã chuẩn hóa của bản ghi xếp ghe (G4), nil nếu không có.
	VesselCode *string
}

// RecordCount ghi số một công đoạn (field_collector hoặc field_supervisor).
func (s *Service) RecordCount(ctx context.Context, in CountInput) (CountResult, bool, error) {
	p, err := authz.Require(ctx, RoleCollector, RoleSupervisor)
	if err != nil {
		return CountResult{}, false, err
	}
	if err := requireIdem(in.IdemKey); err != nil {
		return CountResult{}, false, err
	}
	return s.count(ctx, p, in)
}

func (s *Service) count(ctx context.Context, p authz.Principal, in CountInput) (CountResult, bool, error) {
	correction := in.Supersedes != nil
	reason, err := domain.CheckReason("correction_reason", in.CorrectionReason, correction)
	if err != nil {
		return CountResult{}, false, mapErr(err)
	}
	if !correction && in.CorrectionReason != "" {
		return CountResult{}, false, problem.Validation.WithField("correction_reason", "chỉ dùng cho bản ghi bù")
	}
	if !correction && !domain.ValidStage(in.Stage) {
		return CountResult{}, false, problem.Validation.WithField("stage", "bale | gather | boat_load")
	}
	if err := domain.ValidateCount(in.Count, correction); err != nil {
		return CountResult{}, false, mapErr(err)
	}
	if !correction { // bản bù: công đoạn lấy từ bản bị bù, kiểm trong giao dịch
		if _, err := domain.CheckVesselCode("vessel_code", in.VesselCode, in.Stage); err != nil {
			return CountResult{}, false, mapErr(err)
		}
	}
	if err := domain.CheckEventTime("event_time", in.EventTime, s.Clock.Now()); err != nil {
		return CountResult{}, false, mapErr(err)
	}
	if in.ID != nil && in.ID.Version() != 7 {
		return CountResult{}, false, problem.Validation.WithField("id", "UUIDv7")
	}
	// FM-01: bản ghi cuộn gốc — đọc ngày gặt của thửa từ nền tảng NGOÀI giao dịch DB.
	var live *store.PlotHarvest
	if !correction && in.Stage == port.StageBale {
		var o *store.Order
		if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
			var e error
			o, e = store.GetOrder(ctx, tx, in.OrderID, false)
			return e
		}); err != nil {
			return CountResult{}, false, mapErr(err)
		}
		if o != nil && o.HasPlot(in.PlotRef) {
			h := s.fetchPlots(ctx, store.Order{OrderID: o.OrderID, WindowFrom: o.WindowFrom, PlotRefs: []uuid.UUID{in.PlotRef}})[0]
			live = &h
		}
	}
	var res CountResult
	replayed, err := s.write(ctx, p.TenantID, func(tx pgx.Tx) error {
		res = CountResult{}
		// id do máy sinh đã dùng cho bản ghi khác (khác idem_key) — kiểm trước mọi quy tắc nghiệp vụ.
		if in.ID != nil {
			if taken, err := store.IDTaken(ctx, tx, *in.ID); err != nil {
				return err
			} else if taken {
				return ErrIDReused
			}
		}
		orderID, plot, stage := in.OrderID, in.PlotRef, in.Stage
		var target *store.RollCount
		if correction {
			var err error
			if target, err = store.RollCountByID(ctx, tx, *in.Supersedes); err != nil {
				return err
			}
			if target == nil {
				return ErrRollCountNotFound
			}
			orderID, plot, stage = target.OrderID, target.PlotRef, target.Stage
		}
		o, err := store.GetOrder(ctx, tx, orderID, true) // khóa: mọi ghi của một lệnh gom tuần tự
		if err != nil {
			return err
		}
		if o == nil {
			return ErrOrderNotFound
		}
		if !domain.Countable(o.Status) {
			return ErrOrderNotOpen
		}
		if !o.HasPlot(plot) {
			return ErrPlotNotInOrder
		}
		cur, err := store.TotalFor(ctx, tx, orderID, plot, stage)
		if err != nil {
			return err
		}
		if correction {
			if cur == nil || cur.RollCountID != target.ID {
				return ErrAlreadyCorrected
			}
		} else if cur != nil {
			return ErrStageAlreadyCounted
		}
		prev, err := store.TotalFor(ctx, tx, orderID, plot, domain.PreviousStage(stage))
		if err != nil {
			return err
		}
		var prevCount *domain.Count
		if prev != nil {
			c := prev.Count()
			prevCount = &c
		}
		if err := domain.CheckAgainstPrevious(stage, in.Count, prevCount); err != nil {
			return err
		}
		vessel, err := domain.CheckVesselCode("vessel_code", in.VesselCode, stage)
		if err != nil {
			return err
		}
		if vessel == nil && correction && stage == port.StageBoatLoad {
			if vessel, err = store.VesselCodeOf(ctx, tx, target.ID); err != nil {
				return err
			}
		}
		if correction {
			next, err := store.TotalFor(ctx, tx, orderID, plot, domain.NextStage(stage))
			if err != nil {
				return err
			}
			if next != nil {
				c := next.Count()
				if err := domain.CheckAgainstNext(in.Count, &c); err != nil {
					return err
				}
			}
		}
		var hc *store.HarvestCheck
		if live != nil {
			h, source, err := resolveHarvest(ctx, tx, *live)
			if err != nil {
				return err
			}
			baleDay := domain.Day(in.EventTime)
			if h.Check == domain.HarvestOK {
				earliest := domain.EarliestHarvest(*h.Date, h.OpenDate)
				if err := domain.CheckBaleDate(baleDay, earliest); err != nil {
					return ErrBeforeHarvest.WithDetail("ngày cuộn " + baleDay.Format(dateLayout) + " trước ngày gặt " + earliest.Format(dateLayout) +
						" (mở cửa sổ gặt của vụ)")
				}
			}
			if live.Check != domain.HarvestUnavailable {
				if err := store.PutPlotHarvest(ctx, tx, p.TenantID, *live); err != nil {
					return err
				}
			}
			hc = &store.HarvestCheck{BaleDate: baleDay, Check: h.Check, HarvestDate: h.Date, OpenDate: h.OpenDate, SeasonRef: h.SeasonRef, Source: source}
			if w := harvestWarning(h.Check); w != "" {
				res.Warnings = append(res.Warnings, w)
			}
		}
		id := ids.New()
		if in.ID != nil {
			id = *in.ID
		}
		rc := store.RollCount{
			ID: id, TenantID: p.TenantID, OrderID: orderID, PlotRef: plot, HTXRef: o.HTXRef, Stage: stage,
			Rolls: in.Count.Rolls, LooseKg: in.Count.LooseKg, DiscrepancyReason: strPtr(in.Count.DiscrepancyReason),
			HTXConfirmationRef: in.HTXConfirmationRef, SupersedesID: in.Supersedes, CorrectionReason: strPtr(reason),
			EventTime: in.EventTime.UTC(), ActorRef: p.ActorRef, DeviceID: in.DeviceID, IdemKey: in.IdemKey,
		}
		if err := insert(store.InsertRollCount(ctx, tx, &rc)); err != nil {
			return err
		}
		if err := store.UpsertTotal(ctx, tx, p.TenantID, rc); err != nil {
			return err
		}
		if in.Location != nil {
			if err := store.InsertLocation(ctx, tx, rc, *in.Location); err != nil {
				return err
			}
		}
		if vessel != nil {
			if err := store.InsertVesselCode(ctx, tx, rc, *vessel); err != nil {
				return err
			}
			res.VesselCode = vessel
		}
		if hc != nil {
			if err := store.InsertHarvestCheck(ctx, tx, rc, hc); err != nil {
				return err
			}
			res.Harvest = hc
		}
		// Sự kiện NỘI BỘ MKG (ADR-0018): measurement (RB-6), costing (G4).
		if err := outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventRollCountRecorded, SchemaVersion: 1, OccurredAt: rc.EventTime,
			TraceID: telemetry.TraceID(ctx), Producer: Producer, Kind: outbox.KindInternal,
			Payload: port.RollCountRecordedV1{
				RollCountID: rc.ID, LotRef: rc.OrderID, PlotRef: rc.PlotRef, HTXRef: rc.HTXRef, Rolls: rc.Rolls, Stage: rc.Stage,
				HTXConfirmationRef: rc.HTXConfirmationRef, SupersedesID: rc.SupersedesID, DeviceID: rc.DeviceID,
				RuleVersion: rc.RuleVersion, EventTime: rc.EventTime, VesselCode: vessel,
			},
		}); err != nil {
			return err
		}
		res.Count = rc
		t, err := store.TotalFor(ctx, tx, orderID, plot, stage)
		if err != nil {
			return err
		}
		res.Total = *t
		if stage == port.StageBoatLoad {
			// G3-2: lô đã cân mà số xếp ghe/xe được sửa → bảng đệm học kg/cuộn theo số mới.
			if err := store.RefreshWeighedRolls(ctx, tx, orderID); err != nil {
				return err
			}
			return s.statement(ctx, tx, p, rc, &res)
		}
		return nil
	}, s.replayCount(ctx, in.IdemKey, &res))
	return res, replayed, err
}

// statement cập nhật đối soát chiều mua và xuất bảng kê htx_collection_statement
// khi số xếp ghe/xe (công đoạn cuối) của một thửa được ghi hoặc sửa.
func (s *Service) statement(ctx context.Context, tx pgx.Tx, p authz.Principal, rc store.RollCount, res *CountResult) error {
	r, err := store.GetRecon(ctx, tx, rc.OrderID, rc.PlotRef)
	if err != nil {
		return err
	}
	if r == nil {
		r = &store.Recon{OrderID: rc.OrderID, PlotRef: rc.PlotRef, HTXRef: rc.HTXRef, StatementRef: ids.New()}
	}
	r.StatementRevision++
	r.MKGRolls, r.MKGRollCountID = rc.Rolls, rc.ID
	r.Status = domain.ReconStatus(r.MKGRolls, r.HTXRolls)
	if err := store.PutRecon(ctx, tx, p.TenantID, *r); err != nil {
		return err
	}
	res.Recon = r
	x, err := s.export(ctx, tx, p.TenantID, p.ActorRef, statementPayload(*r, rc.EventTime), r.StatementRef, rc.OrderID, rc.EventTime,
		rc.IdemKey+":valve:statement")
	if err != nil {
		return err
	}
	res.Exports = append(res.Exports, x)
	return nil
}

func (s *Service) replayCount(ctx context.Context, idem string, res *CountResult) func(pgx.Tx) (bool, error) {
	return func(tx pgx.Tx) (bool, error) {
		rc, err := store.RollCountByIdem(ctx, tx, idem)
		if err != nil || rc == nil {
			return false, err
		}
		res.Count = *rc
		if res.VesselCode, err = store.VesselCodeOf(ctx, tx, rc.ID); err != nil {
			return false, err
		}
		t, err := store.TotalFor(ctx, tx, rc.OrderID, rc.PlotRef, rc.Stage)
		if err != nil {
			return false, err
		}
		if rc.Stage == port.StageBale {
			hs, err := store.HarvestChecksOf(ctx, tx, rc.OrderID)
			if err != nil {
				return false, err
			}
			if h, ok := hs[rc.ID]; ok {
				res.Harvest = &h
				if w := harvestWarning(h.Check); w != "" {
					res.Warnings = append(res.Warnings, w)
				}
			}
		}
		if t != nil {
			res.Total = *t
		}
		if rc.Stage == port.StageBoatLoad {
			if res.Recon, err = store.GetRecon(ctx, tx, rc.OrderID, rc.PlotRef); err != nil {
				return false, err
			}
			xs, err := store.ValveExports(ctx, tx, rc.OrderID, 200)
			if err != nil {
				return false, err
			}
			for _, x := range xs {
				if x.IdemKey == idem+":valve:statement" {
					res.Exports = append(res.Exports, x)
				}
			}
		}
		return true, nil
	}
}

// CountsView là sổ đếm cuộn và số hiệu lực của một lệnh gom.
type CountsView struct {
	Counts    []store.RollCount
	Totals    []store.Total
	Locations map[uuid.UUID]store.Location     // dấu GPS theo roll_count_id (G3-2)
	Harvest   map[uuid.UUID]store.HarvestCheck // kiểm ngày gặt theo roll_count_id (FM-01)
	Vessels   map[uuid.UUID]string             // số hiệu ghe của bản ghi xếp ghe theo roll_count_id (G4)
}

// Counts đọc sổ đếm cuộn (cả bản bị bù) và số hiệu lực theo thửa/công đoạn.
func (s *Service) Counts(ctx context.Context, orderID uuid.UUID) (CountsView, error) {
	var v CountsView
	found := false
	err := s.read(ctx, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, orderID, false)
		if err != nil || o == nil {
			return err
		}
		found = true
		if v.Counts, err = store.RollCounts(ctx, tx, orderID); err != nil {
			return err
		}
		if v.Totals, err = store.Totals(ctx, tx, orderID); err != nil {
			return err
		}
		if v.Locations, err = store.LocationsOf(ctx, tx, orderID); err != nil {
			return err
		}
		if v.Vessels, err = store.VesselCodesOf(ctx, tx, orderID); err != nil {
			return err
		}
		v.Harvest, err = store.HarvestChecksOf(ctx, tx, orderID)
		return err
	})
	if err == nil && !found {
		err = ErrOrderNotFound
	}
	return v, err
}

// LotRolls cài đặt port.RollTotals: số hiệu lực ở công đoạn cuối có dữ liệu của lô gom.
func (s *Service) LotRolls(ctx context.Context, tenantID, lotRef uuid.UUID) (port.LotRolls, error) {
	out := port.LotRolls{LotRef: lotRef, PerPlot: map[uuid.UUID]int{}}
	err := tenant.WithTenantTx(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		o, err := store.GetOrder(ctx, tx, lotRef, false)
		if err != nil {
			return err
		}
		if o == nil {
			return port.ErrLotNotFound
		}
		out.HTXRef = o.HTXRef
		ts, err := store.Totals(ctx, tx, lotRef)
		if err != nil {
			return err
		}
		rank := map[string]int{}
		for i, st := range domain.Stages {
			rank[st] = i
		}
		for _, t := range ts {
			if out.Stage == "" || rank[t.Stage] > rank[out.Stage] {
				out.Stage = t.Stage
			}
		}
		plots := make([]uuid.UUID, 0, len(ts))
		for _, t := range ts {
			if t.Stage == out.Stage {
				out.PerPlot[t.PlotRef] = t.Rolls
				out.Rolls += t.Rolls
				plots = append(plots, t.PlotRef)
			}
		}
		out.HTXConfirmed, err = store.AllMatched(ctx, tx, lotRef, plots)
		return err
	})
	if errors.Is(err, port.ErrLotNotFound) {
		return out, port.ErrLotNotFound
	}
	return out, err
}
