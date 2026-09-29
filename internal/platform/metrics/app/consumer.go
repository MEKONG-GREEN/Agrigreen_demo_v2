package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	ccport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// LayerEvents là các sự kiện nền tảng mà consumer pf.metrics.layer_log nhận để ghi
// nhật ký lớp GIS (GIS US-EVL-01). Mỗi lớp lấy từ module chủ của nó:
//
//	plot           pf.geo.plot.created, pf.geo.plot_boundary.recorded (chỉ phiên bản thành hiện hành)
//	coop_boundary  pf.geo.cooperative_boundary.recorded
//	facility       pf.geo.facility.recorded
//	season_stage   pf.cropcycle.season.opened / completed / corrected
//	cgh_balance    pf.mechanization.balance.computed (một đối tượng mỗi vụ)
//	waterway       pf.waterway.network.published (một đối tượng: mạng phát hành)
//
// Chưa có nguồn: thời tiết (FN-19 "giữ cảnh báo lớn nhất ngày" — bỏ qua), ngừng
// dùng/khôi phục thửa (pf/geo chưa phát sự kiện), hub MKG (van hub_* chỉ cho pf/geo).
var LayerEvents = []string{
	geoport.EventPlotCreated, geoport.EventPlotBoundaryRecorded, geoport.EventCooperativeBoundaryRecorded, geoport.EventFacilityRecorded,
	ccport.EventSeasonOpened, ccport.EventSeasonCompleted, ccport.EventSeasonCorrected,
	mechport.EventBalanceComputed, wwport.EventNetworkPublished,
}

// nsLayer sinh định danh ổn định cho đối tượng không có ref riêng (vụ CGH, mạng đường thủy).
var nsLayer = uuid.MustParse("0193a5c0-0000-7000-8000-6d6574726963")

type sourceEvent struct {
	ID     uuid.UUID
	Type   string
	Module string
	Time   time.Time
}

func eventTime(s string, fallback time.Time) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UTC()
	}
	return fallback.UTC()
}

func geomRef(kind string, id uuid.UUID) map[string]any {
	return map[string]any{"kind": kind, "id": id.String()}
}

func strp(p *string) any {
	if p == nil {
		return nil
	}
	return *p
}

func uuidp(p *uuid.UUID) any {
	if p == nil {
		return nil
	}
	return p.String()
}

// HandleLayerEvent là inbox.Handler của consumer pf.metrics.layer_log. Chạy trong
// giao dịch của inbox.Process (vai metrics_app); giao lặp → không ghi thêm (R-61).
func (s *Service) HandleLayerEvent(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	src := sourceEvent{ID: env.EventID, Type: env.EventType, Module: env.Producer, Time: env.OccurredAt}
	ps, t, err := patchesOf(env)
	if err != nil {
		return err
	}
	if !t.IsZero() {
		src.Time = t
	}
	if src.Time.IsZero() {
		src.Time = env.RecordedAt
	}
	return s.applyPatches(ctx, tx, env.TenantID, src, ps)
}

func decode[T any](env outbox.Envelope) (T, error) {
	var v T
	if err := json.Unmarshal(env.Payload, &v); err != nil {
		return v, fmt.Errorf("metrics: payload %s hỏng: %w", env.EventType, err)
	}
	return v, nil
}

func facilityOp(action string) port.LayerOp {
	switch action {
	case "retired":
		return port.LayerRetire
	case "restored":
		return port.LayerRestore
	case "updated":
		return port.LayerUpdate
	}
	return port.LayerCreate
}

// patchesOf chuyển sự kiện nguồn thành thay đổi lớp. Không mang dữ liệu cá nhân:
// bỏ actor_ref, household_ref.
func patchesOf(env outbox.Envelope) ([]patch, time.Time, error) {
	switch env.EventType {
	case geoport.EventPlotCreated:
		p, err := decode[geoport.PlotCreatedV1](env)
		return []patch{{Layer: port.LayerPlot, Feature: p.PlotRef, Op: port.LayerCreate, Attrs: map[string]any{
			"code": p.Code, "commune_code": p.CommuneCode, "cooperative_ref": uuidp(p.CooperativeRef), "status": p.Status,
			"boundary_id": p.BoundaryID.String(), "boundary_version": 1, "area_ha": p.AreaHa, "source": p.Source,
			"geometry_ref": geomRef("plot_boundary", p.BoundaryID),
		}}}, eventTime(p.EventTime, env.OccurredAt), err
	case geoport.EventPlotBoundaryRecorded:
		p, err := decode[geoport.PlotBoundaryRecordedV1](env)
		if err != nil || !p.BecameCurrent {
			return nil, time.Time{}, err // bản không thành hiện hành (xung đột) không đổi lớp
		}
		return []patch{{Layer: port.LayerPlot, Feature: p.PlotRef, Op: port.LayerUpdate, Attrs: map[string]any{
			"commune_code": p.CommuneCode, "cooperative_ref": uuidp(p.CooperativeRef), "boundary_id": p.BoundaryID.String(),
			"boundary_version": p.Version, "area_ha": p.AreaHa, "source": p.Source, "correction_reason": strp(p.CorrectionReason),
			"geometry_ref": geomRef("plot_boundary", p.BoundaryID),
		}}}, eventTime(p.EventTime, env.OccurredAt), nil
	case geoport.EventCooperativeBoundaryRecorded:
		p, err := decode[geoport.CooperativeBoundaryRecordedV1](env)
		op := port.LayerCreate
		switch p.Action {
		case "retired":
			op = port.LayerRetire
		case "restored":
			op = port.LayerRestore
		}
		return []patch{{Layer: port.LayerCoopBoundary, Feature: p.CooperativeRef, Op: op, Attrs: map[string]any{
			"cooperative_ref": p.CooperativeRef.String(), "commune_code": p.CommuneCode, "status": p.Status, "boundary_id": p.BoundaryID.String(),
			"boundary_version": p.Version, "area_ha": p.AreaHa, "source": p.Source, "correction_reason": strp(p.CorrectionReason),
			"geometry_ref": geomRef("cooperative_boundary", p.BoundaryID),
		}}}, eventTime(p.EventTime, env.OccurredAt), err
	case geoport.EventFacilityRecorded:
		p, err := decode[geoport.FacilityRecordedV1](env)
		attrs := map[string]any{
			"kind": p.Kind, "code": p.Code, "name": p.Name, "status": p.Status, "province_code": p.ProvinceCode, "commune_code": strp(p.CommuneCode),
			"lon": p.Lon, "lat": p.Lat, "capacity": p.Capacity, "unit": p.Unit, "version": p.Version, "correction_reason": strp(p.CorrectionReason),
			"geometry_ref": nil,
		}
		if p.HasPolygon {
			attrs["geometry_ref"] = geomRef("facility_event", p.EventID)
		}
		return []patch{{Layer: port.LayerFacility, Feature: p.FacilityRef, Op: facilityOp(p.Action), Attrs: attrs}},
			eventTime(p.EventTime, env.OccurredAt), err
	case ccport.EventSeasonOpened:
		p, err := decode[ccport.SeasonOpenedV1](env)
		return []patch{{Layer: port.LayerSeasonStage, Feature: p.SeasonRef, Op: port.LayerCreate, Attrs: map[string]any{
			"plot_ref": p.PlotRef.String(), "cooperative_ref": p.CooperativeRef.String(), "commune_code": p.CommuneCode,
			"season": port.SeasonDimension(p.SeasonCode, p.SeasonYear), "variety_code": p.VarietyCode, "sowing_date": p.SowingDate,
			"expected_harvest_date": p.ExpectedHarvestDate, "area_ha": p.AreaHa, "stage": "opened",
		}}}, eventTime(p.EventTime, env.OccurredAt), err
	case ccport.EventSeasonCompleted:
		p, err := decode[ccport.SeasonCompletedV1](env)
		return []patch{{Layer: port.LayerSeasonStage, Feature: p.SeasonRef, Op: port.LayerUpdate, Attrs: map[string]any{
			"plot_ref": p.PlotRef.String(), "cooperative_ref": p.CooperativeRef.String(), "commune_code": p.CommuneCode,
			"season": port.SeasonDimension(p.SeasonCode, p.SeasonYear), "harvest_date": p.HarvestDate, "area_ha": p.AreaHa, "stage": "completed",
		}}}, eventTime(p.EventTime, env.OccurredAt), err
	case ccport.EventSeasonCorrected:
		p, err := decode[ccport.SeasonCorrectedV1](env)
		op, stage := port.LayerUpdate, any(nil)
		if p.Action == "cancelled" {
			op, stage = port.LayerRetire, "cancelled"
		}
		attrs := map[string]any{
			"plot_ref": p.PlotRef.String(), "cooperative_ref": p.CooperativeRef.String(), "commune_code": p.CommuneCode,
			"season": port.SeasonDimension(p.SeasonCode, p.SeasonYear), "variety_code": p.VarietyCode, "sowing_date": p.SowingDate,
			"expected_harvest_date": p.ExpectedHarvestDate, "correction_reason": p.CorrectionReason,
		}
		if stage != nil {
			attrs["stage"] = stage
		}
		return []patch{{Layer: port.LayerSeasonStage, Feature: p.SeasonRef, Op: op, Attrs: attrs}}, eventTime(p.EventTime, env.OccurredAt), err
	case mechport.EventBalanceComputed:
		p, err := decode[mechport.BalanceComputedV1](env)
		season := port.SeasonDimension(p.SeasonCode, p.SeasonYear)
		return []patch{{Layer: port.LayerCGHBalance, Feature: uuid.NewSHA1(nsLayer, []byte("cgh_balance:"+season)), Op: port.LayerCreate, Attrs: map[string]any{
			"season": season, "run_id": p.RunID, "reference_date": p.ReferenceDate, "rule_version": p.RuleVersion,
			"result_count": p.ResultCount, "status_counts": p.StatusCounts, "missing_norms": p.MissingNorms,
		}}}, eventTime(p.ComputedAt, env.OccurredAt), err
	case wwport.EventNetworkPublished:
		p, err := decode[wwport.NetworkPublishedV1](env)
		return []patch{{Layer: port.LayerWaterway, Feature: uuid.NewSHA1(nsLayer, []byte("waterway:network")), Op: port.LayerCreate, Attrs: map[string]any{
			"network_version_id": p.NetworkVersionID, "version_no": p.VersionNo, "effective_from": p.EffectiveFrom,
			"graph_sha256": p.GraphSHA256, "segment_set_sha256": p.SegmentSetSHA256, "vertex_count": p.VertexCount,
			"edge_count": p.EdgeCount, "rule_version": p.RuleVersion,
		}}}, eventTime(p.EffectiveFrom, env.OccurredAt), err
	}
	return nil, time.Time{}, nil // sự kiện khác: bỏ qua
}
