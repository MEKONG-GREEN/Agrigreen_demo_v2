package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
)

// InsertSupportRequest ghi yêu cầu hỗ trợ (chỉ ghi thêm).
func InsertSupportRequest(ctx context.Context, tx pgx.Tx, tid uuid.UUID, r domain.SupportRequest) error {
	var lat, lon *string
	if r.Location != nil {
		a, o := r.Location.Lat.String(), r.Location.Lon.String()
		lat, lon = &a, &o
	}
	photos := make([]string, 0, len(r.PhotoRefs))
	for _, p := range r.PhotoRefs {
		photos = append(photos, p.String())
	}
	_, err := tx.Exec(ctx, `INSERT INTO pf_extension.support_request
	    (tenant_id, id, household_ref, cooperative_ref, commune_code, province_code, plot_ref, category, urgency, description,
	     photo_refs, location, gps_accuracy_m, event_time, received_at, task_id, task_created_at, assignee_ref, assignment_rule,
	     submitted_by, device_id, idem_key)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::uuid[],
	     CASE WHEN $12::text IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint($13::text::float8, $12::text::float8), 4326) END,
	     $14::numeric, $15, $16, $17, $18, $19, $20, $21, $22, $23)`,
		tid, r.ID, r.HouseholdRef, r.CooperativeRef, r.CommuneCode, r.ProvinceCode, nilIfZero(r.PlotRef), r.Category, r.Urgency,
		r.Description, photos, lat, lon, decPtr(r.GPSAccuracyM), r.EventTime.UTC(), r.ReceivedAt.UTC(), r.TaskID,
		r.TaskCreatedAt.UTC(), nilIfZero(r.AssigneeRef), r.AssignmentRule, r.SubmittedBy, r.DeviceID, r.IdemKey)
	return err
}

const supportCols = `s.id, s.household_ref, s.cooperative_ref, s.commune_code, s.province_code, s.plot_ref, s.category, s.urgency,
  s.description, s.photo_refs::text[], ST_Y(s.location)::numeric(12,7)::text, ST_X(s.location)::numeric(12,7)::text, s.gps_accuracy_m::text,
  s.event_time, s.received_at, s.task_id, s.task_created_at, s.assignee_ref, s.assignment_rule, s.submitted_by, s.device_id, s.idem_key,
  coalesce(t.status, 'new'), coalesce(t.sync_seq, 0), coalesce(t.last_note, '')`

const supportFrom = ` FROM pf_extension.support_request s
  LEFT JOIN pf_extension.task_current t ON t.tenant_id = s.tenant_id AND t.task_id = s.task_id`

func scanSupport(row pgx.Row) (domain.SupportRequest, error) {
	var r domain.SupportRequest
	var plot, assignee *uuid.UUID
	var lat, lon, acc *string
	var status string
	var photos []string
	err := row.Scan(&r.ID, &r.HouseholdRef, &r.CooperativeRef, &r.CommuneCode, &r.ProvinceCode, &plot, &r.Category, &r.Urgency,
		&r.Description, &photos, &lat, &lon, &acc, &r.EventTime, &r.ReceivedAt, &r.TaskID, &r.TaskCreatedAt, &assignee,
		&r.AssignmentRule, &r.SubmittedBy, &r.DeviceID, &r.IdemKey, &status, &r.TaskSeq, &r.LastNote)
	if err != nil {
		return r, err
	}
	r.PlotRef, r.AssigneeRef, r.TaskStatus = refOrNil(plot), refOrNil(assignee), domain.Status(status)
	r.PhotoRefs = make([]uuid.UUID, 0, len(photos))
	for _, ph := range photos {
		u, err := uuid.Parse(ph)
		if err != nil {
			return r, err
		}
		r.PhotoRefs = append(r.PhotoRefs, u)
	}
	if lat != nil && lon != nil {
		la, e1 := decimal.NewFromString(*lat)
		lo, e2 := decimal.NewFromString(*lon)
		if err := errors.Join(e1, e2); err != nil {
			return r, err
		}
		r.Location = &domain.Point{Lat: la, Lon: lo}
	}
	r.GPSAccuracyM, err = parseDec(acc)
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

// SupportByIdemKey đọc yêu cầu theo khóa idem (máy gửi lặp).
func SupportByIdemKey(ctx context.Context, tx pgx.Tx, key string) (*domain.SupportRequest, error) {
	return oneSupport(tx.QueryRow(ctx, "SELECT "+supportCols+supportFrom+" WHERE s.idem_key = $1", key))
}

// SupportByID đọc yêu cầu theo mã.
func SupportByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*domain.SupportRequest, error) {
	return oneSupport(tx.QueryRow(ctx, "SELECT "+supportCols+supportFrom+" WHERE s.id = $1", id))
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

// SupportForHouseholds liệt kê yêu cầu của các nông hộ, mới nhất trước (cursor = id).
func SupportForHouseholds(ctx context.Context, tx pgx.Tx, households []uuid.UUID, before uuid.UUID, limit int) ([]domain.SupportRequest, error) {
	return collectSupport(tx.Query(ctx, "SELECT "+supportCols+supportFrom+`
	  WHERE s.household_ref = ANY($1::uuid[]) AND ($2::uuid = '00000000-0000-0000-0000-000000000000'::uuid OR s.id < $2)
	  ORDER BY s.id DESC LIMIT $3`, uuidStrings(households), before, limit))
}

// SupportChangesForHouseholds trả yêu cầu có nhiệm vụ đổi sau con trỏ after (sync_seq của nhiệm vụ).
func SupportChangesForHouseholds(ctx context.Context, tx pgx.Tx, households []uuid.UUID, after int64, limit int) ([]domain.SupportRequest, error) {
	return collectSupport(tx.Query(ctx, "SELECT "+supportCols+supportFrom+`
	  WHERE s.household_ref = ANY($1::uuid[]) AND t.sync_seq > $2 ORDER BY t.sync_seq LIMIT $3`, uuidStrings(households), after, limit))
}

// OpenTaskLoad đếm nhiệm vụ chưa hoàn thành của từng cán bộ (chia đều khi tự giao).
func OpenTaskLoad(ctx context.Context, tx pgx.Tx, actors []uuid.UUID) (map[uuid.UUID]int, error) {
	out := map[uuid.UUID]int{}
	if len(actors) == 0 {
		return out, nil
	}
	rows, err := tx.Query(ctx, `SELECT assignee_ref, count(*) FROM pf_extension.task_current
	  WHERE assignee_ref = ANY($1::uuid[]) AND status <> 'resolved' GROUP BY assignee_ref`, uuidStrings(actors))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a uuid.UUID
		var n int
		if err := rows.Scan(&a, &n); err != nil {
			return nil, err
		}
		out[a] = n
	}
	return out, rows.Err()
}
