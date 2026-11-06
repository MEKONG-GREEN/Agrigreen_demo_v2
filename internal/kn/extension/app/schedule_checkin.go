package app

// W11.46 (D4 CN6, BRD CNTS v3.7 FN-12, UR-XA-05, CV-07) — check-in GPS tại địa điểm công tác.
// CNTS-V4-CHECKOUT-PHOTO (#249, BRD CNTS v4.0 FN-12 CV-07/CV-08, BR-06..08): check-in bắt buộc ảnh; check-out sau
// check-in kèm ảnh, GPS. Ảnh ở kho đối tượng của module (Service.Objects), sổ chỉ giữ khóa, kiểu, cỡ, SHA-256.
//
// Dữ liệu vị trí người là dữ liệu cá nhân (QĐ-8): sổ work_schedule_checkin chỉ giữ thời điểm, sai số, KHOẢNG CÁCH tới
// địa điểm đã đăng ký và các cờ; tọa độ (nếu cờ extension.work_schedule_checkin_store_point bật) nằm ở bảng
// work_schedule_checkin_point có hạn lưu, job extension_work_schedule xóa khi quá hạn.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi check-in (docs/errors/pf-extension-cnts-schedules.md).
var (
	ErrCheckinDisabled      = problem.New(http.StatusConflict, "extension.checkin_disabled", "Phòng ban không dùng check-in lịch công tác")
	ErrCheckinExists        = problem.New(http.StatusConflict, "extension.checkin_exists", "Mục công tác đã check-in (check-in không sửa được)")
	ErrCheckinNotRegistered = problem.New(http.StatusConflict, "extension.checkin_not_registered", "Lịch chưa gửi duyệt — gửi lịch trước khi check-in")
	ErrCheckinNotApproved   = problem.New(http.StatusConflict, "extension.checkin_not_approved", "Lịch chưa được duyệt — chỉ check-in theo lịch đã duyệt")
	ErrScheduleItemNotFound = problem.New(http.StatusNotFound, "extension.schedule_item_not_found", "Không có mục công tác trong lịch hiện hành")
)

// Mã lỗi check-out, ảnh (CNTS-V4-CHECKOUT-PHOTO, BRD CNTS v4.0 FN-12 CV-08).
var (
	ErrCheckoutNoCheckin = problem.New(http.StatusConflict, "extension.checkout_no_checkin", "Chưa check-in mục công tác — check-out chỉ sau khi đã check-in")
	ErrCheckoutExists    = problem.New(http.StatusConflict, "extension.checkout_exists", "Mục công tác đã check-out (check-out không sửa được)")
	ErrVisitPhotoMissing = problem.New(http.StatusNotFound, "extension.visit_photo_not_found", "Không có ảnh check-in/check-out này")
)

// Loại sự việc tại địa điểm công tác (ảnh, khóa kho).
const (
	VisitCheckin  = "checkin"
	VisitCheckout = "checkout"
)

// CheckinRequest là thân "Check-in" / "Check-out" (Mobile, có thể gửi muộn từ hàng đợi ngoại tuyến).
type CheckinRequest struct {
	ScheduleID uuid.UUID
	ItemID     uuid.UUID
	Lat, Lon   float64
	AccuracyM  *float64
	CapturedAt time.Time
	Mock       bool
	DeviceID   *string
	// Photo: ảnh chụp tại chỗ đã nén (≤ 500 KB) — bắt buộc (BRD CNTS v4.0 FN-12, AC-09).
	Photo   []byte
	IdemKey string
}

// CheckinResult là check-in đã ghi kèm cảnh báo (không chặn).
type CheckinResult struct {
	Checkin  store.Checkin
	Warnings []ScheduleWarning
	Replayed bool
}

// CheckoutResult là check-out đã ghi kèm check-in của mục và cảnh báo (không chặn).
type CheckoutResult struct {
	Checkout store.Checkout
	Checkin  store.Checkin
	Warnings []ScheduleWarning
	Replayed bool
}

func idemCheckin(key string) string  { return "checkin:" + key }
func idemCheckout(key string) string { return "checkout:" + key }

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

// checkoutWarnings: cảnh báo của một check-out (FN-12: cách địa điểm CV-06 > ngưỡng → cảnh báo như check-in).
func checkoutWarnings(o store.Checkout, st ScheduleSettings) []ScheduleWarning {
	var out []ScheduleWarning
	one := []uuid.UUID{o.ItemID}
	if o.Far {
		out = append(out, ScheduleWarning{Code: "checkout_far", ItemIDs: one,
			Message: "Check-out cách địa điểm đã đăng ký hơn " + formatM(st.CheckinFarM)})
	}
	if o.WeakAccuracy {
		out = append(out, ScheduleWarning{Code: "checkout_weak_gps", ItemIDs: one,
			Message: "Độ chính xác GPS lúc check-out thấp (sai số lớn hơn " + formatM(st.CheckinAccM) + ")"})
	}
	if o.Mock {
		out = append(out, ScheduleWarning{Code: "checkout_mock_location", ItemIDs: one, Message: "Thiết bị báo vị trí giả lập lúc check-out"})
	}
	return out
}

func formatM(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + " m" }

// visitActor kiểm người gọi ghi được check-in/check-out của chính mình.
func (s *Service) visitActor(ctx context.Context, idemKey string) (knActor, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return a, err
	}
	if err := a.registrant(); err != nil {
		return a, err
	}
	if a.ReadOnly {
		return a, problem.Forbidden.WithDetail("ngữ cảnh chỉ xem")
	}
	if strings.TrimSpace(idemKey) == "" {
		return a, problem.Validation.WithField("Idempotency-Key", "bắt buộc")
	}
	return a, nil
}

// visitTarget kiểm lịch của người gọi cho check-in/check-out: đã gửi, có bản đã duyệt (log bug TT-07/CN-06 — đang có
// điều chỉnh chờ duyệt lại thì đối chiếu mục của bản đã duyệt trước đó), phòng ban dùng check-in. Trả phiên bản mục
// dùng để đối chiếu và tham số.
func (s *Service) visitTarget(ctx context.Context, a knActor, sched *domain.Schedule, now time.Time) (int, ScheduleSettings, error) {
	if sched == nil || sched.OwnerRef != a.ActorRef {
		return 0, ScheduleSettings{}, ErrScheduleNotFound
	}
	if sched.Status == domain.ScheduleDraft {
		return 0, ScheduleSettings{}, ErrCheckinNotRegistered
	}
	version := sched.Version
	if sched.Status != domain.ScheduleApproved {
		if sched.ApprovedVersion == 0 {
			return 0, ScheduleSettings{}, ErrCheckinNotApproved
		}
		version = sched.ApprovedVersion
	}
	st, err := s.scheduleSettings(ctx, sched.Department, sched.ProvinceCode, now)
	if err != nil {
		return 0, ScheduleSettings{}, err
	}
	if !st.Checkin {
		return 0, ScheduleSettings{}, ErrCheckinDisabled
	}
	return version, st, nil
}

// visitItem tìm mục của bản version và khóa tuần tự check-in/check-out của mục.
func visitItem(ctx context.Context, tx pgx.Tx, scheduleID uuid.UUID, version int, itemID uuid.UUID) (*domain.ScheduleItem, error) {
	items, err := store.ScheduleItems(ctx, tx, scheduleID, version)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].ItemID == itemID {
			return &items[i], store.LockCheckinItem(ctx, tx, itemID)
		}
	}
	return nil, ErrScheduleItemNotFound
}

// putVisitPhoto ghi ảnh check-in/check-out vào kho đối tượng của module (ngoài giao dịch DB). Khóa theo băm nội dung:
// gửi lại cùng ảnh ghi đè đúng khóa cũ.
func (s *Service) putVisitPhoto(ctx context.Context, tid, item uuid.UUID, kind string, data []byte) (store.VisitPhoto, error) {
	ct, err := domain.VisitPhotoType(data)
	if err != nil {
		return store.VisitPhoto{}, fieldProblem(err)
	}
	if s.Objects == nil {
		return store.VisitPhoto{}, ErrDependency.WithDetail("chưa nối kho tệp (ảnh check-in/check-out)")
	}
	sum := objstore.SHA256Hex(data)
	key := "cnts-visit-photo/" + tid.String() + "/" + item.String() + "/" + kind + "/" + sum
	if _, err := s.Objects.Put(ctx, key, bytes.NewReader(data), int64(len(data)), sum, ct); err != nil && !errors.Is(err, objstore.ErrExists) {
		return store.VisitPhoto{}, err
	}
	return store.VisitPhoto{Key: key, ContentType: ct, Size: len(data), SHA256: sum}, nil
}

func purgeAfter(now time.Time, st ScheduleSettings) time.Time {
	l := now.In(clock.Zone())
	return time.Date(l.Year(), l.Month(), l.Day()+st.RetentionDays, 0, 0, 0, 0, time.UTC)
}

// Checkin ghi check-in (kèm ảnh bắt buộc) của chính người đăng ký cho một mục của lịch hiện hành (CV-07). Một mục một
// check-in; gọi lặp cùng khóa trả kết quả lần đầu. Không sửa, không xóa qua API (FN-12 BR-02, BR-06).
func (s *Service) Checkin(ctx context.Context, in CheckinRequest) (CheckinResult, error) {
	a, err := s.visitActor(ctx, in.IdemKey)
	if err != nil {
		return CheckinResult{}, err
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
	version, st, err := s.visitTarget(ctx, a, sched, now)
	if err != nil {
		return CheckinResult{}, err
	}
	photo, err := s.putVisitPhoto(ctx, a.TenantID, in.ItemID, VisitCheckin, in.Photo)
	if err != nil {
		return CheckinResult{}, err
	}
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		item, err := visitItem(ctx, tx, sched.ID, version, in.ItemID)
		if err != nil {
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
		c := store.Checkin{ID: ids.New(), ScheduleID: sched.ID, ItemID: item.ItemID, ItemVersion: version, OwnerRef: sched.OwnerRef,
			Department: sched.Department, Day: item.Day, CapturedAt: in.CapturedAt.UTC(), AccuracyM: in.AccuracyM, DistanceM: ev.DistanceM,
			Far: ev.Far, WeakAccuracy: ev.WeakAccuracy, Mock: in.Mock, DeviceID: in.DeviceID, EventTime: now, RecordedAt: now,
			IdemKey: idemCheckin(in.IdemKey), Photo: &photo}
		var point *domain.GeoPoint
		if st.StorePoint {
			p := domain.GeoPoint{Lat: in.Lat, Lon: in.Lon}
			point, c.Point, c.PointStored = &p, &p, true
		}
		if err := store.InsertCheckin(ctx, tx, a.TenantID, c, a.ActorRef, point, purgeAfter(now, st)); err != nil {
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

// Checkout ghi check-out (kèm ảnh bắt buộc, GPS, thời điểm thiết bị, độ chính xác) của chính người đăng ký khi rời địa
// điểm (CNTS v4.0 CV-08, FN-12 B4b). Chỉ sau check-in của mục; một mục một check-out; không sửa được (BR-06). Bản ngoại
// tuyến gửi muộn giữ thời điểm lúc bấm (AC-11), phải thuộc ngày công tác (BR-08); đến sau ngày công tác → "Đồng bộ trễ".
func (s *Service) Checkout(ctx context.Context, in CheckinRequest) (CheckoutResult, error) {
	a, err := s.visitActor(ctx, in.IdemKey)
	if err != nil {
		return CheckoutResult{}, err
	}
	now := s.now()
	var sched *domain.Schedule
	var res CheckoutResult
	if err := tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		o, e := store.CheckoutByIdem(ctx, tx, idemCheckout(in.IdemKey))
		if e != nil {
			return e
		}
		if o != nil {
			res.Checkout, res.Replayed = *o, true
			c, e := store.CheckinOfItem(ctx, tx, o.ItemID)
			if e == nil && c != nil {
				res.Checkin = *c
			}
			return e
		}
		sched, e = store.ScheduleByID(ctx, tx, in.ScheduleID, false)
		return e
	}); err != nil {
		return CheckoutResult{}, err
	}
	if res.Replayed {
		if res.Checkout.OwnerRef != a.ActorRef {
			return CheckoutResult{}, ErrScheduleNotFound
		}
		st, err := s.scheduleSettings(ctx, res.Checkout.Department, a.ProvinceCode, now)
		if err != nil {
			return CheckoutResult{}, err
		}
		res.Warnings = checkoutWarnings(res.Checkout, st)
		return res, nil
	}
	version, st, err := s.visitTarget(ctx, a, sched, now)
	if err != nil {
		return CheckoutResult{}, err
	}
	photo, err := s.putVisitPhoto(ctx, a.TenantID, in.ItemID, VisitCheckout, in.Photo)
	if err != nil {
		return CheckoutResult{}, err
	}
	err = tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		item, err := visitItem(ctx, tx, sched.ID, version, in.ItemID)
		if err != nil {
			return err
		}
		ci, err := store.CheckinOfItem(ctx, tx, item.ItemID)
		if err != nil {
			return err
		}
		if ci == nil {
			return ErrCheckoutNoCheckin
		}
		if prev, err := store.CheckoutOfItem(ctx, tx, item.ItemID); err != nil {
			return err
		} else if prev != nil {
			return ErrCheckoutExists.With("checkout_id", prev.ID.String())
		}
		ev, err := domain.EvaluateCheckin(domain.CheckinInput{Point: domain.GeoPoint{Lat: in.Lat, Lon: in.Lon}, AccuracyM: in.AccuracyM,
			CapturedAt: in.CapturedAt, Mock: in.Mock}, item.Day, item.Place, now, clock.Zone(), st.CheckinFarM, st.CheckinAccM)
		if err != nil {
			return fieldProblem(err)
		}
		if err := domain.CheckCheckoutAfter(ci.CapturedAt, in.CapturedAt); err != nil {
			return fieldProblem(err)
		}
		o := store.Checkout{ID: ids.New(), ScheduleID: sched.ID, ItemID: item.ItemID, ItemVersion: version, CheckinID: ci.ID,
			OwnerRef: sched.OwnerRef, Department: sched.Department, Day: item.Day, CapturedAt: in.CapturedAt.UTC(), AccuracyM: in.AccuracyM,
			DistanceM: ev.DistanceM, Far: ev.Far, WeakAccuracy: ev.WeakAccuracy, Mock: in.Mock, DeviceID: in.DeviceID, Photo: photo,
			EventTime: now, RecordedAt: now, IdemKey: idemCheckout(in.IdemKey)}
		var point *domain.GeoPoint
		if st.StorePoint {
			p := domain.GeoPoint{Lat: in.Lat, Lon: in.Lon}
			point, o.Point, o.PointStored = &p, &p, true
		}
		if err := store.InsertCheckout(ctx, tx, a.TenantID, o, a.ActorRef, point, purgeAfter(now, st)); err != nil {
			return err
		}
		res.Checkout, res.Checkin = o, *ci
		return nil
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		return s.Checkout(ctx, in) // gọi lặp đồng thời: đọc lại bản đã ghi
	}
	if err != nil {
		return CheckoutResult{}, err
	}
	res.Warnings = checkoutWarnings(res.Checkout, st)
	return res, nil
}

// VisitPhotoFile là ảnh check-in/check-out để tải.
type VisitPhotoFile struct {
	ContentType string
	Body        io.ReadCloser
}

// VisitPhoto trả ảnh check-in (kind = checkin) hoặc check-out (kind = checkout) của một mục: người đăng ký lịch và người
// duyệt được lịch (theo dõi check-in, check-out, ảnh — UR-TINH-18), như quyền xem tọa độ. Check-in cũ không ảnh → 404.
func (s *Service) VisitPhoto(ctx context.Context, scheduleID, itemID uuid.UUID, kind string) (VisitPhotoFile, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return VisitPhotoFile{}, err
	}
	if err := a.readable(); err != nil {
		return VisitPhotoFile{}, err
	}
	var sched *domain.Schedule
	var photo *store.VisitPhoto
	if err := tenant.WithTenantTx(ctx, s.DB, a.TenantID, func(tx pgx.Tx) error {
		var e error
		if sched, e = store.ScheduleByID(ctx, tx, scheduleID, false); e != nil || sched == nil || !a.canView(*sched) {
			sched = nil
			return e
		}
		switch kind {
		case VisitCheckin:
			c, e := store.CheckinOfItem(ctx, tx, itemID)
			if e == nil && c != nil && c.ScheduleID == scheduleID {
				photo = c.Photo
			}
			return e
		case VisitCheckout:
			o, e := store.CheckoutOfItem(ctx, tx, itemID)
			if e == nil && o != nil && o.ScheduleID == scheduleID {
				photo = &o.Photo
			}
			return e
		}
		return nil
	}); err != nil {
		return VisitPhotoFile{}, err
	}
	if sched == nil {
		return VisitPhotoFile{}, ErrScheduleNotFound
	}
	if sched.OwnerRef != a.ActorRef {
		st, err := s.scheduleSettings(ctx, sched.Department, sched.ProvinceCode, s.now())
		if err != nil {
			return VisitPhotoFile{}, err
		}
		if err := a.canReview(*sched, st); err != nil {
			return VisitPhotoFile{}, err
		}
	}
	if photo == nil {
		return VisitPhotoFile{}, ErrVisitPhotoMissing
	}
	if s.Objects == nil {
		return VisitPhotoFile{}, ErrDependency.WithDetail("chưa nối kho tệp (ảnh check-in/check-out)")
	}
	rc, _, err := s.Objects.Get(ctx, photo.Key)
	if errors.Is(err, objstore.ErrNotFound) {
		return VisitPhotoFile{}, ErrVisitPhotoMissing
	}
	if err != nil {
		return VisitPhotoFile{}, err
	}
	return VisitPhotoFile{ContentType: photo.ContentType, Body: rc}, nil
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
			if c.Checkout != nil {
				o := *c.Checkout
				if !showPoint {
					o.Point = nil
				}
				c.Checkout = &o
				d.Warnings = append(d.Warnings, checkoutWarnings(o, st)...)
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
