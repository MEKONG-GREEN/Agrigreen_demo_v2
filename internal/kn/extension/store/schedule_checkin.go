package store

// W11.46 — địa điểm công tác (sổ work_schedule_item_place) và check-in GPS (sổ work_schedule_checkin không tọa độ +
// bảng có hạn lưu work_schedule_checkin_point, QĐ-8).

import (
	"context"
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
}

// LockCheckinItem tuần tự hóa check-in của một mục.
func LockCheckinItem(ctx context.Context, tx pgx.Tx, item uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_extension.work_schedule_checkin:' || $1::text, 0))", item)
	return err
}

// InsertCheckin ghi sự việc check-in; point khác nil → ghi tọa độ vào bảng có hạn lưu tới purgeAfter.
func InsertCheckin(ctx context.Context, tx pgx.Tx, tid uuid.UUID, c Checkin, actor uuid.UUID, point *domain.GeoPoint, purgeAfter time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin
	    (id, tenant_id, stream_key, schedule_id, item_id, item_version, owner_ref, department_code, day, captured_at,
	     accuracy_m, distance_m, far, weak_accuracy, mock_location, point_stored, device_id, event_time, actor_ref, idem_key,
	     rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::date, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)`,
		c.ID, tid, "wcheckin:"+c.ItemID.String(), c.ScheduleID, c.ItemID, c.ItemVersion, c.OwnerRef, c.Department,
		c.Day.Format(time.DateOnly), c.CapturedAt.UTC(), c.AccuracyM, c.DistanceM, c.Far, c.WeakAccuracy, c.Mock, point != nil,
		c.DeviceID, c.EventTime.UTC(), actor, c.IdemKey, domain.RuleVersion)
	if err != nil || point == nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO pf_extension.work_schedule_checkin_point (tenant_id, checkin_id, point, captured_at, purge_after)
	  VALUES ($1, $2, ST_SetSRID(ST_MakePoint($3, $4), 4326), $5, $6::date)`,
		tid, c.ID, point.Lon, point.Lat, c.CapturedAt.UTC(), purgeAfter.Format(time.DateOnly))
	return err
}

const checkinCols = `c.id, c.schedule_id, c.item_id, c.item_version, c.owner_ref, c.department_code, c.day, c.captured_at,
  c.accuracy_m::float8, c.distance_m::float8, c.far, c.weak_accuracy, c.mock_location, c.point_stored, c.device_id, c.event_time,
  c.recorded_at, c.idem_key, ST_Y(p.point), ST_X(p.point)`

const checkinFrom = ` FROM pf_extension.work_schedule_checkin c
  LEFT JOIN pf_extension.work_schedule_checkin_point p ON p.tenant_id = c.tenant_id AND p.checkin_id = c.id`

func scanCheckins(rows pgx.Rows) ([]Checkin, error) {
	defer rows.Close()
	var out []Checkin
	for rows.Next() {
		var c Checkin
		var lat, lon *float64
		if err := rows.Scan(&c.ID, &c.ScheduleID, &c.ItemID, &c.ItemVersion, &c.OwnerRef, &c.Department, &c.Day, &c.CapturedAt,
			&c.AccuracyM, &c.DistanceM, &c.Far, &c.WeakAccuracy, &c.Mock, &c.PointStored, &c.DeviceID, &c.EventTime, &c.RecordedAt,
			&c.IdemKey, &lat, &lon); err != nil {
			return nil, err
		}
		c.Day = dateUTC(c.Day)
		if lat != nil && lon != nil {
			c.Point = &domain.GeoPoint{Lat: *lat, Lon: *lon}
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

// CheckinsOf trả check-in của các lịch, khóa theo item_id (mỗi mục một check-in).
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
	for _, c := range l {
		if _, ok := out[c.ItemID]; !ok {
			out[c.ItemID] = c
		}
	}
	return out, err
}

// PurgeCheckinPoints gọi pf_extension.purge_checkin_points: xóa một lô tọa độ check-in quá hạn lưu (purge_after < today)
// của tenant hiện hành (bảng có hạn lưu, không phải sổ — QĐ-8).
func PurgeCheckinPoints(ctx context.Context, tx pgx.Tx, today time.Time, limit int) (int, error) {
	var n int64
	err := tx.QueryRow(ctx, "SELECT pf_extension.purge_checkin_points($1::date, $2::int)", today.Format(time.DateOnly), limit).Scan(&n)
	return int(n), err
}
