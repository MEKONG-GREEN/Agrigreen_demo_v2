package app

// W11.46 (D4 CN6, BRD CNTS v3.7 FN-12, UR-XA-05, CV-07) — check-in GPS tại địa điểm công tác.
//
// Dữ liệu vị trí người là dữ liệu cá nhân (QĐ-8): sổ work_schedule_checkin chỉ giữ thời điểm, sai số, KHOẢNG CÁCH tới
// địa điểm đã đăng ký và các cờ; tọa độ (nếu cờ extension.work_schedule_checkin_store_point bật) nằm ở bảng
// work_schedule_checkin_point có hạn lưu, job extension_work_schedule xóa khi quá hạn.

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi check-in (docs/errors/pf-extension-cnts-schedules.md).
var (
	ErrCheckinDisabled      = problem.New(http.StatusConflict, "extension.checkin_disabled", "Phòng ban không dùng check-in lịch công tác")
	ErrCheckinExists        = problem.New(http.StatusConflict, "extension.checkin_exists", "Mục công tác đã check-in (check-in không sửa được)")
	ErrCheckinNotRegistered = problem.New(http.StatusConflict, "extension.checkin_not_registered", "Lịch chưa gửi duyệt — gửi lịch trước khi check-in")
	ErrScheduleItemNotFound = problem.New(http.StatusNotFound, "extension.schedule_item_not_found", "Không có mục công tác trong lịch hiện hành")
)

// CheckinRequest là thân "Check-in" (Mobile, có thể gửi muộn từ hàng đợi ngoại tuyến).
type CheckinRequest struct {
	ScheduleID uuid.UUID
	ItemID     uuid.UUID
	Lat, Lon   float64
	AccuracyM  *float64
	CapturedAt time.Time
	Mock       bool
	DeviceID   *string
	IdemKey    string
}

// CheckinResult là check-in đã ghi kèm cảnh báo (không chặn).
type CheckinResult struct {
	Checkin  store.Checkin
	Warnings []ScheduleWarning
	Replayed bool
}

func idemCheckin(key string) string { return "checkin:" + key }

// checkinWarnings: cảnh báo của một check-in (FN-12 luồng ngoại lệ, BRD Mục VIII định vị).
func checkinWarnings(c store.Checkin, st ScheduleSettings) []ScheduleWarning {
	var out []ScheduleWarning
	one := []uuid.UUID{c.ItemID}
	if c.Far {
		out = append(out, ScheduleWarning{Code: "checkin_far", ItemIDs: one,
			Message: "Check-in cách địa điểm đã đăng ký hơn " + formatM(st.CheckinFarM)})
	}
	if c.DistanceM == nil {
		out = append(out, ScheduleWarning{Code: "checkin_no_place", ItemIDs: one,
			Message: "Mục chưa có địa điểm trên bản đồ — không tính được khoảng cách"})
	}
	if c.WeakAccuracy {
		out = append(out, ScheduleWarning{Code: "checkin_weak_gps", ItemIDs: one,
			Message: "Độ chính xác GPS thấp (sai số lớn hơn " + formatM(st.CheckinAccM) + ")"})
	}
	if c.Mock {
		out = append(out, ScheduleWarning{Code: "checkin_mock_location", ItemIDs: one, Message: "Thiết bị báo vị trí giả lập"})
	}
	return out
}

func formatM(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + " m" }

// Checkin ghi check-in của chính người đăng ký cho một mục của lịch hiện hành (CV-07). Một mục một check-in; gọi lặp
// cùng khóa trả kết quả lần đầu. Không sửa, không xóa qua API (FN-12 BR-02).
func (s *Service) Checkin(ctx context.Context, in CheckinRequest) (CheckinResult, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return CheckinResult{}, err
	}
	if err := a.registrant(); err != nil {
		return CheckinResult{}, err
	}
	if a.KN != nil && a.KN.ReadOnly() {
		return CheckinResult{}, problem.Forbidden.WithDetail("ngữ cảnh chỉ xem")
	}
	if strings.TrimSpace(in.IdemKey) == "" {
		return CheckinResult{}, problem.Validation.WithField("Idempotency-Key", "bắt buộc")
	}
	now := s.now()
	var sched *domain.Schedule
	var res CheckinResult
	if err := tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		if c, e := store.CheckinByIdem(ctx, tx, idemCheckin(in.IdemKey)); e != nil || c != nil {
			if c != nil {
				res.Checkin, res.Replayed = *c, true
			}
			return e
		}
		sched, e = store.ScheduleByID(ctx, tx, in.ScheduleID, false)
		return e
	}); err != nil {
		return CheckinResult{}, err
	}
	if res.Replayed {
		if res.Checkin.OwnerRef != a.ActorRef {
			return CheckinResult{}, ErrScheduleNotFound
		}
		st, err := s.scheduleSettings(ctx, res.Checkin.Department, a.ProvinceCode, now)
		if err != nil {
			return CheckinResult{}, err
		}
		res.Warnings = checkinWarnings(res.Checkin, st)
		return res, nil
	}
	if sched == nil || sched.OwnerRef != a.ActorRef {
		return CheckinResult{}, ErrScheduleNotFound
	}
	if sched.Status == domain.ScheduleDraft {
		return CheckinResult{}, ErrCheckinNotRegistered
	}
	st, err := s.scheduleSettings(ctx, sched.Department, sched.ProvinceCode, now)
	if err != nil {
		return CheckinResult{}, err
	}
	if !st.Checkin {
		return CheckinResult{}, ErrCheckinDisabled
	}
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		items, err := store.ScheduleItems(ctx, tx, sched.ID, sched.Version)
		if err != nil {
			return err
		}
		var item *domain.ScheduleItem
		for i := range items {
			if items[i].ItemID == in.ItemID {
				item = &items[i]
			}
		}
		if item == nil {
			return ErrScheduleItemNotFound
		}
		if err := store.LockCheckinItem(ctx, tx, item.ItemID); err != nil {
			return err
		}
		if prev, err := store.CheckinOfItem(ctx, tx, item.ItemID); err != nil {
			return err
		} else if prev != nil {
			return ErrCheckinExists.With("checkin_id", prev.ID.String())
		}
		ev, err := domain.EvaluateCheckin(domain.CheckinInput{Point: domain.GeoPoint{Lat: in.Lat, Lon: in.Lon}, AccuracyM: in.AccuracyM,
			CapturedAt: in.CapturedAt, Mock: in.Mock}, item.Day, item.Place, now, clock.Zone(), st.CheckinFarM, st.CheckinAccM)
		if err != nil {
			return fieldProblem(err)
		}
		c := store.Checkin{ID: ids.New(), ScheduleID: sched.ID, ItemID: item.ItemID, ItemVersion: sched.Version, OwnerRef: sched.OwnerRef,
			Department: sched.Department, Day: item.Day, CapturedAt: in.CapturedAt.UTC(), AccuracyM: in.AccuracyM, DistanceM: ev.DistanceM,
			Far: ev.Far, WeakAccuracy: ev.WeakAccuracy, Mock: in.Mock, DeviceID: in.DeviceID, EventTime: now, RecordedAt: now,
			IdemKey: idemCheckin(in.IdemKey)}
		var point *domain.GeoPoint
		if st.StorePoint {
			p := domain.GeoPoint{Lat: in.Lat, Lon: in.Lon}
			point, c.Point, c.PointStored = &p, &p, true
		}
		l := now.In(clock.Zone())
		purge := time.Date(l.Year(), l.Month(), l.Day()+st.RetentionDays, 0, 0, 0, 0, time.UTC)
		if err := store.InsertCheckin(ctx, tx, a.TenantID, c, a.ActorRef, point, purge); err != nil {
			return err
		}
		res.Checkin = c
		return nil
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		return s.Checkin(ctx, in) // gọi lặp đồng thời: đọc lại bản đã ghi
	}
	if err != nil {
		return CheckinResult{}, err
	}
	res.Warnings = checkinWarnings(res.Checkin, st)
	return res, nil
}

// decorateCheckins gắn check-in của lịch vào chi tiết và cảnh báo "ngày công tác ngoài cơ quan chưa check-in".
func (s *Service) decorateCheckins(ctx context.Context, a knActor, d *ScheduleDetail, st ScheduleSettings, now time.Time) {
	d.CheckinEnabled = st.Checkin
	if !st.Checkin {
		return
	}
	var m map[uuid.UUID]store.Checkin
	if err := tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		m, e = store.CheckinsOf(ctx, tx, []uuid.UUID{d.Schedule.ID})
		return e
	}); err != nil {
		return
	}
	applyCheckins(a, d, st, now, m)
}

// applyCheckins gắn check-in m (khóa item_id) vào d.
func applyCheckins(a knActor, d *ScheduleDetail, st ScheduleSettings, now time.Time, m map[uuid.UUID]store.Checkin) {
	d.CheckinEnabled = st.Checkin
	if !st.Checkin {
		return
	}
	// Tọa độ chỉ cho người đăng ký và người duyệt được (theo dõi check-in, UR-TINH-18).
	showPoint := d.Schedule.OwnerRef == a.ActorRef || a.canReview(d.Schedule, st) == nil
	d.Checkins = map[uuid.UUID]store.Checkin{}
	var missed []uuid.UUID
	for _, it := range d.Items {
		c, ok := m[it.ItemID]
		if ok {
			if !showPoint {
				c.Point = nil
			}
			d.Checkins[it.ItemID] = c
			d.Warnings = append(d.Warnings, checkinWarnings(c, st)...)
		}
		if domain.MissedCheckin(it, ok, now, clock.Zone()) {
			missed = append(missed, it.ItemID)
		}
	}
	if len(missed) > 0 {
		d.Warnings = append(d.Warnings, ScheduleWarning{Code: "checkin_missed", ItemIDs: missed,
			Message: "Ngày công tác ngoài cơ quan chưa check-in"})
	}
}
