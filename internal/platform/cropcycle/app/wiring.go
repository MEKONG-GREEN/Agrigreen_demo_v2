package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ port.SupplyStats = (*Service)(nil)

// SeasonSupply thực hiện port.SupplyStats. Tenant lấy từ context.
func (s *Service) SeasonSupply(ctx context.Context, year int) ([]port.CoopSeasonSupply, error) {
	var rows []store.SupplyRow
	err := s.fromCtx(ctx, func(tx pgx.Tx) error {
		var e error
		rows, e = store.SeasonSupply(ctx, tx, year)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]port.CoopSeasonSupply, 0, len(rows))
	for _, r := range rows {
		out = append(out, port.CoopSeasonSupply{CooperativeRef: r.CooperativeRef, SeasonCode: r.SeasonCode, SeasonYear: r.SeasonYear,
			AreaHa: r.AreaHa, PaddyKg: r.PaddyKg, HarvestedAreaHa: r.HarvestedAreaHa, Seasons: r.Seasons})
	}
	return out, nil
}

// HandleGpsLog là inbox.Handler của consumer port.GpsLogConsumer (HTX US-GPS-03): đọc
// toạ độ của vụ / nhật ký / khai sản lượng ở sổ pf_cropcycle (trong giao dịch inbox của
// vai cropcycle_app) rồi ghi log qua geo/port.GpsLog (giao dịch riêng của pf/geo, R-05).
// Khóa chống trùng theo bản ghi ("cropcycle:<loại>:<id>") — giao lặp an toàn. Bản ghi
// không có toạ độ (từ chối quyền / không có thiết bị) không có gì để ghi. Lỗi kiểm dữ
// liệu của geo không tự lành → bỏ qua; lỗi khác → giao lại (không chặn nghiệp vụ).
func (s *Service) HandleGpsLog(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if s.GpsLog == nil {
		return ErrDependency.WithDetail("chưa nối log GPS (pf/geo)")
	}
	fix, ok, err := s.gpsFixOf(ctx, tx, env)
	if err != nil || !ok {
		return err
	}
	_, err = s.GpsLog.RecordFix(tenant.WithID(ctx, env.TenantID), fix)
	if errors.Is(err, geoport.ErrGpsFixInvalid) {
		return nil
	}
	return err
}

func (s *Service) gpsFixOf(ctx context.Context, tx pgx.Tx, env outbox.Envelope) (geoport.GpsFix, bool, error) {
	var ids struct {
		EventID   uuid.UUID `json:"event_id"`
		LogID     uuid.UUID `json:"log_id"`
		HarvestID uuid.UUID `json:"harvest_id"`
	}
	if err := json.Unmarshal(env.Payload, &ids); err != nil {
		return geoport.GpsFix{}, false, fmt.Errorf("cropcycle: payload %s hỏng: %w", env.EventType, err)
	}
	fix := geoport.GpsFix{}
	var g domain.GPS
	var device *string
	switch env.EventType {
	case port.EventSeasonOpened:
		e, err := store.SeasonEventByID(ctx, tx, ids.EventID)
		if err != nil || e == nil {
			return fix, false, err
		}
		fix = geoport.GpsFix{ActorRef: e.ActorRef, Purpose: geoport.GpsPurposeSeasonOpen, TargetRef: e.SeasonRef, PlotRef: e.PlotRef,
			CommuneCode: e.CommuneCode, CapturedAt: e.EventTime, IdemKey: "cropcycle:season_event:" + e.ID.String()}
		g, device = e.GPS, e.DeviceID
	case port.EventFarmLogRecorded:
		l, err := store.FarmLogByID(ctx, tx, ids.LogID)
		if err != nil || l == nil {
			return fix, false, err
		}
		if l.SupersedesID != nil {
			return fix, false, nil // bản ghi bù: toạ độ và thời gian khóa sau khi lưu — đã ghi log ở bản đầu
		}
		fix = geoport.GpsFix{ActorRef: l.ActorRef, Purpose: geoport.GpsPurposeFarmLog, TargetRef: l.ID, PlotRef: l.PlotRef,
			CommuneCode: l.CommuneCode, CapturedAt: l.EventTime, IdemKey: "cropcycle:farm_log:" + l.ID.String()}
		g, device = l.GPS, l.DeviceID
	case port.EventHarvestDeclared:
		h, err := store.HarvestByID(ctx, tx, ids.HarvestID)
		if err != nil || h == nil {
			return fix, false, err
		}
		if h.SupersedesID != nil {
			return fix, false, nil
		}
		fix = geoport.GpsFix{ActorRef: h.ActorRef, Purpose: geoport.GpsPurposeHarvest, TargetRef: h.ID, PlotRef: h.PlotRef,
			CommuneCode: h.CommuneCode, CapturedAt: h.EventTime, IdemKey: "cropcycle:harvest:" + h.ID.String()}
		g, device = h.GPS, h.DeviceID
	default:
		return fix, false, nil
	}
	switch g.Source {
	case geoport.GpsSourceDevice, geoport.GpsSourcePhotoEXIF:
		if !g.HasPoint() {
			return fix, false, nil
		}
		fix.Lon, fix.Lat, fix.AccuracyM = g.Lon, g.Lat, g.AccuracyM
	case geoport.GpsSourceManualPick:
		fix.Lon, fix.Lat = g.Lon, g.Lat
	default:
		return fix, false, nil // denied | no_device: không có toạ độ để ghi
	}
	fix.Source = g.Source
	if device != nil {
		fix.DeviceID = *device
	}
	return fix, true, nil
}
