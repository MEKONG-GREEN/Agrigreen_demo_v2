package store

// W11.46 — địa điểm công tác (sổ work_schedule_item_place) và check-in GPS (sổ work_schedule_checkin không tọa độ +
// bảng có hạn lưu work_schedule_checkin_point, QĐ-8).

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
)

func insertItemPlace(ctx context.Context, tx pgx.Tx, tid, scheduleID uuid.UUID, it domain.ScheduleItem, actor uuid.UUID, at time.Time,
	idem string, id uuid.UUID) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_item_place
	    (id, tenant_id, stream_key, schedule_id, version, item_row_id, item_id, point, place_source, event_time, actor_ref,
	     idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, ST_SetSRID(ST_MakePoint($8, $9), 4326), $10, $11, $12, $13, $14)`,
		id, tid, domain.ScheduleStreamKey(scheduleID), scheduleID, it.Version, it.RowID, it.ItemID, it.Place.Lon, it.Place.Lat,
		it.Place.Source, at.UTC(), actor, idem, domain.RuleVersion)
	return err
}

func placeOf(lat, lon *float64, src *string) *domain.GeoPoint {
	if lat == nil || lon == nil {
		return nil
	}
	p := &domain.GeoPoint{Lat: *lat, Lon: *lon, Source: domain.PlaceSourceMap}
	if src != nil {
		p.Source = *src
	}
	return p
}

// Checkin là một dòng sổ work_schedule_checkin (kèm tọa độ nếu bảng có hạn lưu còn giữ).
type Checkin struct {
	ID           uuid.UUID
	ScheduleID   uuid.UUID
	ItemID       uuid.UUID
	ItemVersion  int
	OwnerRef     uuid.UUID
	Department   string
	Day          time.Time
	CapturedAt   time.Time
	AccuracyM    *float64
	DistanceM    *float64
	Far          bool
	WeakAccuracy bool
	Mock         bool
	PointStored  bool
	DeviceID     *string
	EventTime    time.Time
	RecordedAt   time.Time
	IdemKey      string
	// Point: tọa độ còn trong hạn lưu (nil = không lưu hoặc đã xóa).
	Point *domain.GeoPoint
	// Photo: ảnh tại chỗ (BRD CNTS v4.0 CV-07). nil = dữ liệu cũ trước v4.0, không ảnh (giữ nguyên, không nhập bù).
	Photo *VisitPhoto
	// Checkout: check-out của mục (CV-08), nil = chưa check-out. Chỉ CheckinsOf gắn.
	Checkout *Checkout
	// NoCheckout: nhãn "Chưa check-out" (CNTS v4.0 BR-07) — dịch vụ suy ra theo đồng hồ khi đọc, không lưu.
	NoCheckout bool
}

// VisitPhoto là ảnh check-in/check-out ở kho đối tượng (sổ chỉ giữ khóa, kiểu, cỡ, SHA-256).
type VisitPhoto struct {
	Key         string
	ContentType string
	Size        int
	SHA256      string
}

// Checkout là một dòng sổ work_schedule_checkout (CNTS v4.0 CV-08) kèm tọa độ nếu bảng có hạn lưu còn giữ.
type Checkout struct {
	ID           uuid.UUID
	ScheduleID   uuid.UUID
	ItemID       uuid.UUID
	ItemVersion  int
	CheckinID    uuid.UUID
	OwnerRef     uuid.UUID
	Department   string
	Day          time.Time
	CapturedAt   time.Time
	AccuracyM    *float64
	DistanceM    *float64
	Far          bool
	WeakAccuracy bool
	Mock         bool
	PointStored  bool
	DeviceID     *string
	Photo        VisitPhoto
	EventTime    time.Time // thời điểm máy chủ nhận bản check-out (đồng bộ) theo đồng hồ dịch vụ
	RecordedAt   time.Time
	IdemKey      string
	Point        *domain.GeoPoint
}

// LockCheckinItem tuần tự hóa check-in của một mục.
func LockCheckinItem(ctx context.Context, tx pgx.Tx, item uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.work_schedule_checkin:' || $1::text, 0))", item)
	return err
}

// InsertCheckin ghi sự việc check-in kèm ảnh (bắt buộc từ CNTS v4.0); point khác nil → ghi tọa độ vào bảng có hạn
// lưu tới purgeAfter.
func InsertCheckin(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c Checkin, actor uuid.UUID, point *domain.GeoPoint, purgeAfter time.Time) error {
	if c.Photo == nil {
		return errors.New("store: check-in mới phải có ảnh (CNTS v4.0 CV-07)")
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin
	    (id, tenant_id, stream_key, schedule_id, item_id, item_version, owner_ref, department_code, day, captured_at,
	     accuracy_m, distance_m, far, weak_accuracy, mock_location, point_stored, device_id, event_time, actor_ref, idem_key,
	     rule_version, photo_key, photo_content_type, photo_size_bytes, photo_sha256)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::date, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23,
	    $24, $25)`,
		c.ID, tid, "wcheckin:"+c.ItemID.String(), c.ScheduleID, c.ItemID, c.ItemVersion, c.OwnerRef, c.Department,
		c.Day.Format(time.DateOnly), c.CapturedAt.UTC(), c.AccuracyM, c.DistanceM, c.Far, c.WeakAccuracy, c.Mock, point != nil,
		c.DeviceID, c.EventTime.UTC(), actor, c.IdemKey, domain.RuleVersion, c.Photo.Key, c.Photo.ContentType, c.Photo.Size,
		c.Photo.SHA256)
	if err != nil {
		return err
	}
	return insertVisitPoint(ctx, tx, tid, c.ID, point, c.CapturedAt, purgeAfter)
}

// insertVisitPoint ghi tọa độ check-in/check-out (id của sự việc) vào bảng có hạn lưu; point nil → không ghi.
func insertVisitPoint(ctx context.Context, tx pgx.Tx, tid, id uuid.UUID, point *domain.GeoPoint, at, purgeAfter time.Time) error {
	if point == nil {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin_point (tenant_id, checkin_id, point, captured_at, purge_after)
	  VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5, $6::date)`,
		tid, id, point.Lon, point.Lat, at.UTC(), purgeAfter.Format(time.DateOnly))
	return err
}

const checkinCols = `c.id, c.schedule_id, c.item_id, c.item_version, c.owner_ref, c.department_code, c.day, c.captured_at,
  c.accuracy_m::float8, c.distance_m::float8, c.far, c.weak_accuracy, c.mock_location, c.point_stored, c.device_id, c.event_time,
  c.recorded_at, c.idem_key, ST_Y(p.point), ST_X(p.point), c.photo_key, c.photo_content_type, c.photo_size_bytes, c.photo_sha256`

const checkinFrom = ` FROM pf_extension.work_schedule_checkin c
  LEFT JOIN pf_extension.work_schedule_checkin_point p ON p.tenant_id = c.tenant_id AND p.checkin_id = c.id`

func scanCheckins(rows pgx.Rows) ([]Checkin, error) {
	defer rows.Close()
	var out []Checkin
	for rows.Next() {
		var c Checkin
		var lat, lon *float64
		var pKey, pType, pSHA *string
		var pSize *int
		if err := rows.Scan(&c.ID, &c.ScheduleID, &c.ItemID, &c.ItemVersion, &c.OwnerRef, &c.Department, &c.Day, &c.CapturedAt,
			&c.AccuracyM, &c.DistanceM, &c.Far, &c.WeakAccuracy, &c.Mock, &c.PointStored, &c.DeviceID, &c.EventTime, &c.RecordedAt,
			&c.IdemKey, &lat, &lon, &pKey, &pType, &pSize, &pSHA); err != nil {
			return nil, err
		}
		c.Day = dateUTC(c.Day)
		if lat != nil && lon != nil {
			c.Point = &domain.GeoPoint{Lat: *lat, Lon: *lon}
		}
		if pKey != nil && pType != nil && pSize != nil && pSHA != nil {
			c.Photo = &VisitPhoto{Key: *pKey, ContentType: *pType, Size: *pSize, SHA256: *pSHA}
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CheckinOfItem trả check-in của mục (nil = chưa có).
func CheckinOfItem(ctx context.Context, tx pgx.Tx, item uuid.UUID) (*Checkin, error) {
	rows, err := tx.Query(ctx, "SELECT "+checkinCols+checkinFrom+" WHERE c.item_id = $1 ORDER BY c.seq LIMIT 1", item)
	if err != nil {
		return nil, err
	}
	l, err := scanCheckins(rows)
	if err != nil || len(l) == 0 {
		return nil, err
	}
	return &l[0], nil
}

// CheckinByIdem trả check-in đã ghi với khóa idem (gọi lặp).
func CheckinByIdem(ctx context.Context, tx pgx.Tx, key string) (*Checkin, error) {
	rows, err := tx.Query(ctx, "SELECT "+checkinCols+checkinFrom+" WHERE c.idem_key = $1 LIMIT 1", key)
	if err != nil {
		return nil, err
	}
	l, err := scanCheckins(rows)
	if err != nil || len(l) == 0 {
		return nil, err
	}
	return &l[0], nil
}

// CheckinsOf trả check-in của các lịch, khóa theo item_id (mỗi mục một check-in), kèm check-out của mục (nếu có).
func CheckinsOf(ctx context.Context, tx pgx.Tx, schedules []uuid.UUID) (map[uuid.UUID]Checkin, error) {
	out := map[uuid.UUID]Checkin{}
	if len(schedules) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, "SELECT "+checkinCols+checkinFrom+" WHERE c.schedule_id = ANY ($1::uuid[]) ORDER BY c.seq",
		uuidStrings(schedules))
	if err != nil {
		return nil, err
	}
	l, err := scanCheckins(rows)
	if err != nil {
		return nil, err
	}
	for _, c := range l {
		if _, ok := out[c.ItemID]; !ok {
			out[c.ItemID] = c
		}
	}
	rows, err = tx.Query(ctx, "SELECT "+checkoutCols+checkoutFrom+" WHERE o.schedule_id = ANY ($1::uuid[]) ORDER BY o.seq",
		uuidStrings(schedules))
	if err != nil {
		return nil, err
	}
	outs, err := scanCheckouts(rows)
	for i := range outs {
		if c, ok := out[outs[i].ItemID]; ok && c.Checkout == nil {
			c.Checkout = &outs[i]
			out[c.ItemID] = c
		}
	}
	return out, err
}

// InsertCheckout ghi sự việc check-out (CV-08); point khác nil → ghi tọa độ vào bảng có hạn lưu tới purgeAfter.
func InsertCheckout(ctx context.Context, tx pgx.Tx, tid uuid.UUID, o Checkout, actor uuid.UUID, point *domain.GeoPoint, purgeAfter time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkout
	    (id, tenant_id, stream_key, schedule_id, item_id, item_version, checkin_id, owner_ref, department_code, day, captured_at,
	     accuracy_m, distance_m, far, weak_accuracy, mock_location, point_stored, device_id, photo_key, photo_content_type,
	     photo_size_bytes, photo_sha256, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::date, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23,
	    $24, $25, $26)`,
		o.ID, tid, "wcheckout:"+o.ItemID.String(), o.ScheduleID, o.ItemID, o.ItemVersion, o.CheckinID, o.OwnerRef, o.Department,
		o.Day.Format(time.DateOnly), o.CapturedAt.UTC(), o.AccuracyM, o.DistanceM, o.Far, o.WeakAccuracy, o.Mock, point != nil,
		o.DeviceID, o.Photo.Key, o.Photo.ContentType, o.Photo.Size, o.Photo.SHA256, o.EventTime.UTC(), actor, o.IdemKey,
		domain.RuleVersion)
	if err != nil {
		return err
	}
	return insertVisitPoint(ctx, tx, tid, o.ID, point, o.CapturedAt, purgeAfter)
}

const checkoutCols = `o.id, o.schedule_id, o.item_id, o.item_version, o.checkin_id, o.owner_ref, o.department_code, o.day,
  o.captured_at, o.accuracy_m::float8, o.distance_m::float8, o.far, o.weak_accuracy, o.mock_location, o.point_stored, o.device_id,
  o.photo_key, o.photo_content_type, o.photo_size_bytes, o.photo_sha256, o.event_time, o.recorded_at, o.idem_key,
  ST_Y(p.point), ST_X(p.point)`

const checkoutFrom = ` FROM pf_extension.work_schedule_checkout o
  LEFT JOIN pf_extension.work_schedule_checkin_point p ON p.tenant_id = o.tenant_id AND p.checkin_id = o.id`

func scanCheckouts(rows pgx.Rows) ([]Checkout, error) {
	defer rows.Close()
	var out []Checkout
	for rows.Next() {
		var o Checkout
		var lat, lon *float64
		if err := rows.Scan(&o.ID, &o.ScheduleID, &o.ItemID, &o.ItemVersion, &o.CheckinID, &o.OwnerRef, &o.Department, &o.Day,
			&o.CapturedAt, &o.AccuracyM, &o.DistanceM, &o.Far, &o.WeakAccuracy, &o.Mock, &o.PointStored, &o.DeviceID, &o.Photo.Key,
			&o.Photo.ContentType, &o.Photo.Size, &o.Photo.SHA256, &o.EventTime, &o.RecordedAt, &o.IdemKey, &lat, &lon); err != nil {
			return nil, err
		}
		o.Day = dateUTC(o.Day)
		if lat != nil && lon != nil {
			o.Point = &domain.GeoPoint{Lat: *lat, Lon: *lon}
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func oneCheckout(ctx context.Context, tx pgx.Tx, where string, arg any) (*Checkout, error) {
	rows, err := tx.Query(ctx, "SELECT "+checkoutCols+checkoutFrom+" WHERE "+where+" ORDER BY o.seq LIMIT 1", arg)
	if err != nil {
		return nil, err
	}
	l, err := scanCheckouts(rows)
	if err != nil || len(l) == 0 {
		return nil, err
	}
	return &l[0], nil
}

// CheckoutOfItem trả check-out của mục (nil = chưa có).
func CheckoutOfItem(ctx context.Context, tx pgx.Tx, item uuid.UUID) (*Checkout, error) {
	return oneCheckout(ctx, tx, "o.item_id = $1", item)
}

// CheckoutByIdem trả check-out đã ghi với khóa idem (gọi lặp).
func CheckoutByIdem(ctx context.Context, tx pgx.Tx, key string) (*Checkout, error) {
	return oneCheckout(ctx, tx, "o.idem_key = $1", key)
}

// PurgeCheckinPoints gọi pf_extension.purge_checkin_points: xóa một lô tọa độ check-in quá hạn lưu (purge_after < today)
// của tenant hiện hành (bảng có hạn lưu, không phải sổ — QĐ-8).
func PurgeCheckinPoints(ctx context.Context, tx pgx.Tx, today time.Time, limit int) (int, error) {
	var n int64
	err := tx.QueryRow(ctx, "SELECT pf_extension.purge_checkin_points($1::date, $2::int)", today.Format(time.DateOnly), limit).Scan(&n)
	return int(n), err
}

// UnclosedCheckin là check-in có ảnh (từ CNTS v4.0) của cán bộ xã chưa có check-out, kèm lịch và mục (BR-07 "Chưa
// check-out" — cảnh báo cho người theo dõi lịch cán bộ xã, Mục III.5 dòng 13).
type UnclosedCheckin struct {
	CheckinID    uuid.UUID
	ScheduleID   uuid.UUID
	ItemID       uuid.UUID
	OwnerRef     uuid.UUID
	Department   string
	ProvinceCode string
	CommuneCode  string // xã của mục; mục cũ không ghi xã → xã đầu của lịch, rồi tỉnh
	Content      string
	Day          time.Time
	CapturedAt   time.Time
}

// UnclosedCheckins trả một trang tối đa limit check-in của phòng ban dept, ngày công tác trong [from, to], lịch Cấp Xã, chưa
// có check-out, theo (ngày, mã check-in) tăng dần sau after (nil = từ đầu). Check-in cũ trước CNTS v4.0 (không ảnh) không
// xét: khi đó chưa có check-out, không bắt nhập bù (PLAN mục 5).
func UnclosedCheckins(ctx context.Context, tx pgx.Tx, dept string, from, to time.Time, after *UnclosedCheckin, limit int) ([]UnclosedCheckin, error) {
	afterDay, afterID := from.AddDate(0, 0, -1), uuid.Nil
	if after != nil {
		afterDay, afterID = after.Day, after.CheckinID
	}
	rows, err := tx.Query(ctx, `SELECT c.id, c.schedule_id, c.item_id, c.owner_ref, c.department_code, s.province_code,
	    coalesce(nullif(i.commune_code, ''), s.commune_codes[1], s.province_code), i.content, c.day, c.captured_at
	  FROM pf_extension.work_schedule_checkin c
	  JOIN pf_extension.work_schedule_current s ON s.tenant_id = c.tenant_id AND s.schedule_id = c.schedule_id
	  JOIN pf_extension.work_schedule_item i ON i.tenant_id = c.tenant_id AND i.schedule_id = c.schedule_id
	    AND i.version = c.item_version AND i.item_id = c.item_id
	  WHERE c.department_code = $1 AND c.day BETWEEN $2::date AND $3::date AND c.photo_key IS NOT NULL
	    AND s.owner_level = 'commune' AND (c.day, c.id) > ($4::date, $5::uuid)
	    AND NOT EXISTS (SELECT 1 FROM pf_extension.work_schedule_checkout o WHERE o.tenant_id = c.tenant_id AND o.item_id = c.item_id)
	  ORDER BY c.day, c.id LIMIT $6`, dept, from.Format(time.DateOnly), to.Format(time.DateOnly), afterDay.Format(time.DateOnly),
		afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UnclosedCheckin
	for rows.Next() {
		var u UnclosedCheckin
		if err := rows.Scan(&u.CheckinID, &u.ScheduleID, &u.ItemID, &u.OwnerRef, &u.Department, &u.ProvinceCode, &u.CommuneCode,
			&u.Content, &u.Day, &u.CapturedAt); err != nil {
			return nil, err
		}
		u.Day = dateUTC(u.Day)
		out = append(out, u)
	}
	return out, rows.Err()
}
