package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Log GPS (HTX US-GPS-03): sổ chỉ ghi thêm, chỉ đọc với mọi người kể cả Admin,
// giữ ≥ 12 tháng (không có job xóa), ghi qua port.GpsLog trong giao dịch riêng.

var _ port.GpsLog = (*Service)(nil)

func invalidFix(msg string) error { return fmt.Errorf("%w: %s", port.ErrGpsFixInvalid, msg) }

// RecordFix thực hiện port.GpsLog. Tenant lấy từ context; không đòi vai (module gọi
// đã kiểm quyền nghiệp vụ). Gọi lặp cùng ID/IdemKey trả bản ghi cũ (Duplicate).
func (s *Service) RecordFix(ctx context.Context, fix port.GpsFix) (port.GpsFixReceipt, error) {
	tid, err := tenant.FromContext(ctx)
	if err != nil {
		return port.GpsFixReceipt{}, err
	}
	if fix.ActorRef == uuid.Nil {
		return port.GpsFixReceipt{}, invalidFix("thiếu actor_ref")
	}
	if !domain.KnownGpsPurpose(fix.Purpose) {
		return port.GpsFixReceipt{}, invalidFix("purpose phải là season_open, farm_log, harvest, support hoặc plot")
	}
	if !domain.KnownGpsSource(fix.Source) {
		return port.GpsFixReceipt{}, invalidFix("source phải là device, photo_exif hoặc manual_pick")
	}
	switch {
	case fix.ID == uuid.Nil && fix.IdemKey == "":
		return port.GpsFixReceipt{}, invalidFix("cần ID (UUIDv7 do máy sinh) hoặc IdemKey để gửi lại an toàn")
	case fix.ID == uuid.Nil:
		fix.ID = ids.New()
	case fix.ID.Version() != 7:
		return port.GpsFixReceipt{}, invalidFix("ID phải là UUIDv7")
	}
	if fix.IdemKey == "" {
		fix.IdemKey = "gps:" + fix.ID.String()
	}
	if fix.AccuracyM != nil && fix.AccuracyM.IsNegative() {
		return port.GpsFixReceipt{}, invalidFix("accuracy_m không được âm")
	}
	pointGJ := ""
	var lon, lat *float64
	switch {
	case fix.Lon != nil && fix.Lat != nil:
		pos, err := domain.NewPosition(*fix.Lon, *fix.Lat)
		if err != nil {
			return port.GpsFixReceipt{}, invalidFix("toạ độ ngoài miền WGS84")
		}
		pointGJ, lon, lat = domain.PointGeoJSON(pos), &pos[0], &pos[1]
	case fix.Lon != nil || fix.Lat != nil:
		return port.GpsFixReceipt{}, invalidFix("cần cả kinh độ và vĩ độ")
	case fix.Source != port.GpsSourceManualPick || fix.PlotRef == uuid.Nil:
		return port.GpsFixReceipt{}, invalidFix("không có toạ độ chỉ được khi chọn lô thủ công (manual_pick) kèm plot_ref")
	}
	at, err := s.eventTime(fix.CapturedAt)
	if err != nil {
		return port.GpsFixReceipt{}, invalidFix("captured_at không được ở tương lai")
	}
	row := store.GpsFix{
		ID: fix.ID, TenantID: tid, ActorRef: fix.ActorRef, DeviceID: strings.TrimSpace(fix.DeviceID), Purpose: fix.Purpose,
		TargetRef: fix.TargetRef, PlotRef: fix.PlotRef, CommuneCode: strings.TrimSpace(fix.CommuneCode), Lon: lon, Lat: lat,
		AccuracyM: fix.AccuracyM, Source: fix.Source, EventTime: at, IdemKey: fix.IdemKey,
	}
	receipt := func(f store.GpsFix, dup bool) port.GpsFixReceipt {
		return port.GpsFixReceipt{ID: f.ID, Seq: f.Seq, RecordedAt: f.RecordedAt, DistanceToPlotM: f.DistanceToPlotM, Duplicate: dup}
	}
	var out port.GpsFixReceipt
	err = s.inTx(ctx, tid, func(tx pgx.Tx) error {
		if prev, err := store.GpsFixByIdemKey(ctx, tx, fix.IdemKey); err != nil || prev != nil {
			if prev != nil {
				out = receipt(*prev, true)
			}
			return err
		}
		// Phạm vi đọc: xã/HTX của lô; không có lô → xã gửi lên; không có → xã chứa điểm.
		if row.PlotRef != uuid.Nil {
			p, err := store.PlotByRef(ctx, tx, row.PlotRef, false)
			if err != nil {
				return err
			}
			if p != nil {
				row.CommuneCode, row.CooperativeRef = p.CommuneCode, p.CooperativeRef
			}
		}
		if row.CommuneCode == "" && pointGJ != "" {
			c, err := store.CommuneAt(ctx, tx, pointGJ)
			if err != nil {
				return err
			}
			row.CommuneCode = c
		}
		if err := store.InsertGpsFix(ctx, tx, &row, pointGJ); err != nil {
			return err
		}
		out = receipt(row, false)
		return nil
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		rerr := s.inTx(ctx, tid, func(tx pgx.Tx) error {
			prev, e := store.GpsFixByIdemKey(ctx, tx, fix.IdemKey)
			if prev != nil {
				out = receipt(*prev, true)
			}
			return e
		})
		return out, rerr
	}
	return out, err
}

// GpsQueryInput là truy vấn log GPS (màn hình "Nhật ký lịch sử").
type GpsQueryInput struct {
	ActorRef, TargetRef, PlotRef uuid.UUID
	Purpose                      string
	Before                       uuid.UUID
	Limit                        int
}

// GpsPage là một trang log (mới nhất trước).
type GpsPage struct {
	Items []store.GpsFix
	Next  *uuid.UUID
}

// ListGpsFixes đọc log trong phạm vi: xã của người gọi; quản lý HTX chỉ log gắn
// với HTX mình. Log không xác định được xã chỉ phạm vi toàn quốc thấy.
func (s *Service) ListGpsFixes(ctx context.Context, in GpsQueryInput) (GpsPage, error) {
	a, err := s.access(ctx, layerReadRoles...)
	if err != nil {
		return GpsPage{}, err
	}
	if in.Limit <= 0 {
		in.Limit = 50
	}
	if in.Limit > 200 {
		return GpsPage{}, problem.Validation.WithField("limit", "từ 1 đến 200")
	}
	if in.Purpose != "" && !domain.KnownGpsPurpose(in.Purpose) {
		return GpsPage{}, problem.Validation.WithField("purpose", "phải là season_open, farm_log, harvest, support hoặc plot")
	}
	if a.sc.Empty() {
		return GpsPage{Items: []store.GpsFix{}}, nil
	}
	var items []store.GpsFix
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListGpsFixes(ctx, tx, store.GpsQuery{
			Filter: a.filter(), ActorRef: in.ActorRef, TargetRef: in.TargetRef, PlotRef: in.PlotRef, Purpose: in.Purpose,
			Before: in.Before, Limit: in.Limit + 1,
		})
		return e
	})
	if err != nil {
		return GpsPage{}, err
	}
	pg := GpsPage{Items: items}
	if len(items) > in.Limit {
		pg.Items = items[:in.Limit]
		last := pg.Items[in.Limit-1].ID
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []store.GpsFix{}
	}
	return pg, nil
}
