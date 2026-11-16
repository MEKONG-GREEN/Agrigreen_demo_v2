package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
)

// InsertSupportRequest ghi yêu cầu hỗ trợ (chỉ ghi thêm, #313). sync_seq lấy sau khóa tuần tự của bảng nên thứ tự commit
// trùng thứ tự con trỏ chiều xuống.
func InsertSupportRequest(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r domain.SupportRequest) error {
	if err := lockSync(ctx, tx, "support_request"); err != nil {
		return err
	}
	var lat, lon *string
	if r.Location != nil {
		a, o := r.Location.Lat.String(), r.Location.Lon.String()
		lat, lon = &a, &o
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_coop.support_request
	    (tenant_id, id, household_ref, cooperative_ref, commune_code, province_code, plot_ref, category, urgency, description,
	     photo_refs, location, gps_accuracy_m, event_time, received_at, submitted_by, device_id, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::uuid[], `+gpsInsertSQL("$12", "$13")+`, $14::numeric, $15, $16, $17, $18, $19)`,
		tid, r.ID, r.HouseholdRef, r.CooperativeRef, r.CommuneCode, r.ProvinceCode, nilIfZero(r.PlotRef), r.Category, r.Urgency,
		r.Description, uuids(r.PhotoRefs), lon, lat, decPtr(r.GPSAccuracyM), r.EventTime.UTC(), r.ReceivedAt.UTC(), r.SubmittedBy,
		r.DeviceID, r.IdemKey)
	return err
}

const supportCols = `s.id, s.household_ref, s.cooperative_ref, s.commune_code, s.province_code, s.plot_ref, s.category, s.urgency,
  s.description, s.photo_refs::text[], ST_Y(s.location)::numeric(12,7)::text, ST_X(s.location)::numeric(12,7)::text,
  s.gps_accuracy_m::text, s.event_time, s.received_at, s.submitted_by, s.device_id, s.idem_key, s.sync_seq,
  coalesce(s.legacy_task_status, '') FROM pf_coop.support_request s`

func scanSupport(row pgx.Row) (domain.SupportRequest, error) {
	var r domain.SupportRequest
	var plot *uuid.UUID
	var lat, lon, acc *string
	var photos []string
	err := row.Scan(&r.ID, &r.HouseholdRef, &r.CooperativeRef, &r.CommuneCode, &r.ProvinceCode, &plot, &r.Category, &r.Urgency,
		&r.Description, &photos, &lat, &lon, &acc, &r.EventTime, &r.ReceivedAt, &r.SubmittedBy, &r.DeviceID, &r.IdemKey,
		&r.SyncSeq, &r.LegacyTaskStatus)
	if err != nil {
		return r, err
	}
	r.PlotRef = deref(plot)
	if r.PhotoRefs, err = parseUUIDs(photos); err != nil {
		return r, err
	}
	if lat != nil && lon != nil {
		la, e1 := decimal.NewFromString(*lat)
		lo, e2 := decimal.NewFromString(*lon)
		if err := errors.Join(e1, e2); err != nil {
			return r, err
		}
		r.Location = &domain.Point{Lat: la, Lon: lo}
	}
	r.GPSAccuracyM, err = parseDecPtr(acc)
	return r, err
}

func oneSupport(row pgx.Row) (*domain.SupportRequest, error) {
	r, err := scanSupport(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func collectSupport(rows pgx.Rows, err error) ([]domain.SupportRequest, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SupportRequest{}
	for rows.Next() {
		r, err := scanSupport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SupportByIdemKey đọc yêu cầu theo khóa idem (máy gửi lặp).
func SupportByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.SupportRequest, error) {
	return oneSupport(tx.QueryRow(ctx, "SELECT "+supportCols+" WHERE s.idem_key = $1", key))
}

// SupportByID đọc yêu cầu theo mã.
func SupportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.SupportRequest, error) {
	return oneSupport(tx.QueryRow(ctx, "SELECT "+supportCols+" WHERE s.id = $1", id))
}

// SupportForHouseholds liệt kê yêu cầu của các nông hộ, mới nhất trước (cursor = id, UUIDv7).
func SupportForHouseholds(ctx context.Context, tx pgx.Tx, households []uuid.UUID, before uuid.UUID, limit int) ([]domain.SupportRequest, error) {
	return collectSupport(tx.Query(ctx, "SELECT "+supportCols+`
	  WHERE s.household_ref = ANY($1::uuid[]) AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR s.id < $2::uuid)
	  ORDER BY s.id DESC LIMIT $3`, uuids(households), before, limit))
}

// SupportChangesForHouseholds trả yêu cầu của các nông hộ ghi sau con trỏ after (sync_seq).
func SupportChangesForHouseholds(ctx context.Context, tx pgx.Tx, households []uuid.UUID, after int64, limit int) ([]domain.SupportRequest, error) {
	return collectSupport(tx.Query(ctx, "SELECT "+supportCols+`
	  WHERE s.household_ref = ANY($1::uuid[]) AND s.sync_seq > $2 ORDER BY s.sync_seq LIMIT $3`, uuids(households), after, limit))
}

// SupportFilter lọc danh sách yêu cầu cho quản lý HTX / quản trị.
type SupportFilter struct {
	Scope       Filter // FilterAll (quản trị) hoặc FilterCoop (quản lý HTX)
	Cooperative uuid.UUID
	Category    string
	Urgency     string
	CommuneCode string
	GPSMissing  *bool
	Before      uuid.UUID // con trỏ: id < Before (mới nhất trước)
	Limit       int
}

// SupportForManagers liệt kê yêu cầu trong phạm vi, mới nhất trước (cursor = id, UUIDv7).
func SupportForManagers(ctx context.Context, tx pgx.Tx, f SupportFilter) ([]domain.SupportRequest, error) {
	args := append(f.Scope.args(), nilIfZero(f.Cooperative), f.Category, f.Urgency, f.CommuneCode, f.GPSMissing, f.Before, f.Limit)
	return collectSupport(tx.Query(ctx, "SELECT "+supportCols+`
	  WHERE `+scopeSQL("s", 1)+`
	    AND ($5::uuid IS NULL OR s.cooperative_ref = $5::uuid)
	    AND ($6::text = '' OR s.category = $6::text)
	    AND ($7::text = '' OR s.urgency = $7::text)
	    AND ($8::text = '' OR s.commune_code = $8::text)
	    AND ($9::boolean IS NULL OR (s.location IS NULL) = $9::boolean)
	    AND ($10::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR s.id < $10::uuid)
	  ORDER BY s.id DESC LIMIT $11`, args...))
}
