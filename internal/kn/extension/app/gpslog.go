package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// HandleGpsLog là inbox.Handler của consumer port.GpsLogConsumer (HTX US-GPS-03, mục
// đích support): sau khi yêu cầu hỗ trợ đã commit, đọc vị trí ở pf_extension.support_request
// (trong giao dịch inbox của vai extension_app) và ghi log qua geo/port.GpsLog (giao dịch
// riêng của pf/geo). Khóa chống trùng "extension:support_request:<id>"; yêu cầu thiếu
// vị trí không có gì để ghi; lỗi kiểm dữ liệu của geo bỏ qua, lỗi khác giao lại.
//
// P0-POOL-SHARED-2 (#143, bàn giao từ #139): ghi log qua pf/geo (giao dịch riêng trên cùng pool) chạy NGOÀI giao dịch
// inbox qua txdeps.Get — inbox.Process hủy giao dịch, ghi (lũy đẳng theo IdemKey), chạy lại handler để commit inbox.
func (s *Service) HandleGpsLog(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	if env.EventType != port.EventSupportRequestSubmitted {
		return nil
	}
	if s.GpsLog == nil {
		return ErrDependency.WithDetail("chưa nối log GPS (pf/geo)")
	}
	var p port.SupportRequestSubmittedV1
	if err := json.Unmarshal(env.Payload, &p); err != nil {
		return fmt.Errorf("extension: payload %s hỏng: %w", env.EventType, err)
	}
	r, err := store.SupportByID(ctx, tx, p.SupportRequestID)
	if err != nil || r == nil || r.Location == nil {
		return err
	}
	lon, lat := r.Location.Lon, r.Location.Lat
	fix := geoport.GpsFix{ActorRef: r.SubmittedBy, Purpose: geoport.GpsPurposeSupport, TargetRef: r.ID, PlotRef: r.PlotRef,
		CommuneCode: r.CommuneCode, Lon: &lon, Lat: &lat, AccuracyM: r.GPSAccuracyM, Source: geoport.GpsSourceDevice,
		CapturedAt: r.EventTime, IdemKey: "extension:support_request:" + r.ID.String()}
	if r.DeviceID != nil {
		fix.DeviceID = *r.DeviceID
	}
	_, err = txdeps.Get(ctx, "geo.gps_fix:"+fix.IdemKey, func(ctx context.Context) (geoport.GpsFixReceipt, error) {
		return s.GpsLog.RecordFix(tenant.WithID(ctx, env.TenantID), fix)
	})
	if errors.Is(err, geoport.ErrGpsFixInvalid) {
		return nil
	}
	return err
}
