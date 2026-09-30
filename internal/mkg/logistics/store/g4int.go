package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// ---------------------------------------------------------------------------
// Lọc danh sách chuyến (G4 tích hợp: "chuyến của tôi", delta theo recorded_at)
// ---------------------------------------------------------------------------

// TripFilter là bộ lọc danh sách chuyến.
type TripFilter struct {
	Status    string
	DriverRef *uuid.UUID // chỉ chuyến giao cho tài xế này (driver_ref = sub của token)
	// UpdatedSince: chế độ delta — chuyến có recorded_at (sự kiện cuối) > mốc, sắp (recorded_at, trip_id) TĂNG dần,
	// con trỏ (AfterAt, AfterID) theo recorded_at. Nil: sắp (planned_at, trip_id) giảm dần như trước.
	UpdatedSince *time.Time
	AfterAt      *time.Time
	AfterID      uuid.UUID
	Limit        int
}

// ListTripsFiltered liệt kê chuyến theo bộ lọc.
func ListTripsFiltered(ctx context.Context, tx pgx.Tx, f TripFilter) ([]Trip, error) {
	if f.UpdatedSince != nil {
		return queryTrips(ctx, tx, "SELECT "+tripCols+` FROM mkg_logistics.trip_current
		    WHERE ($1 = '' OR status = $1) AND ($2::uuid IS NULL OR driver_ref = $2) AND recorded_at > $3
		      AND ($4::timestamptz IS NULL OR (recorded_at, trip_id) > ($4, $5))
		    ORDER BY recorded_at, trip_id LIMIT $6`, f.Status, f.DriverRef, f.UpdatedSince.UTC(), utcPtr(f.AfterAt), f.AfterID, f.Limit)
	}
	return queryTrips(ctx, tx, "SELECT "+tripCols+` FROM mkg_logistics.trip_current
	    WHERE ($1 = '' OR status = $1) AND ($2::uuid IS NULL OR driver_ref = $2)
	      AND ($3::timestamptz IS NULL OR (planned_at, trip_id) < ($3, $4))
	    ORDER BY planned_at DESC, trip_id DESC LIMIT $5`, f.Status, f.DriverRef, utcPtr(f.AfterAt), f.AfterID, f.Limit)
}

// ---------------------------------------------------------------------------
// Độ chính xác GPS của mốc chuyến (sổ trip_milestone_fix, mkg 000020)
// ---------------------------------------------------------------------------

// RuleMilestoneFix là phiên bản quy tắc của dòng độ chính xác GPS.
const RuleMilestoneFix = "logistics.milestone_fix.v1"

// InsertMilestoneFix ghi accuracy_m của một mốc chuyến (cùng giao dịch với dòng trip_event).
func InsertMilestoneFix(ctx context.Context, tx pgx.Tx, e TripEvent, accuracy decimal.Decimal) error {
	_, err := tx.Exec(ctx, `INSERT INTO mkg_logistics.trip_milestone_fix
	    (id, tenant_id, stream_key, trip_id, trip_event_id, accuracy_m, event_time, actor_ref, device_id, idem_key, rule_version)
	  VALUES ($1, $2, $3, $4, $5, $6::numeric, $7, $8, $9, $10, $11)`,
		ids.New(), e.TenantID, "fix:"+e.ID.String(), e.TripID, e.ID, accuracy.Round(2).String(), e.EventTime.UTC(), e.ActorRef, e.DeviceID,
		e.IdemKey+":fix", RuleMilestoneFix)
	return err
}

// MilestoneFixes đọc accuracy_m theo trip_event_id của một chuyến.
func MilestoneFixes(ctx context.Context, tx pgx.Tx, trip uuid.UUID) (map[uuid.UUID]decimal.Decimal, error) {
	rows, err := tx.Query(ctx, `SELECT trip_event_id, accuracy_m::text FROM mkg_logistics.trip_milestone_fix WHERE trip_id = $1`, trip)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]decimal.Decimal{}
	for rows.Next() {
		var id uuid.UUID
		var s string
		if err := rows.Scan(&id, &s); err != nil {
			return nil, err
		}
		d, err := decimal.NewFromString(s)
		if err != nil {
			return nil, err
		}
		out[id] = d
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------------------
// Thông báo cảnh báo L-08 (bảng vận hành alert_notice, mkg 000020)
// ---------------------------------------------------------------------------

// Notice là một thông báo cảnh báo đã gửi.
type Notice struct {
	ID           uuid.UUID
	Kind         string
	SubjectRef   uuid.UUID
	SubjectCode  string
	DedupeKey    string
	Audience     []string
	Detail       map[string]string
	ParamVersion string
	RuleVersion  string
	NoticedAt    time.Time
	RecordedAt   time.Time
}

// InsertNotice ghi thông báo nếu (tenant, dedupe_key) chưa có — trả true khi là thông báo MỚI.
func InsertNotice(ctx context.Context, tx pgx.Tx, tid uuid.UUID, n *Notice) (bool, error) {
	detail, err := json.Marshal(n.Detail)
	if err != nil {
		return false, err
	}
	tag, err := tx.Exec(ctx, `INSERT INTO mkg_logistics.alert_notice
	    (tenant_id, id, kind, subject_ref, subject_code, dedupe_key, audience, detail, param_version, rule_version, noticed_at)
	  VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) ON CONFLICT (tenant_id, dedupe_key) DO NOTHING`,
		tid, n.ID, n.Kind, n.SubjectRef, n.SubjectCode, n.DedupeKey, n.Audience, string(detail), n.ParamVersion, n.RuleVersion, n.NoticedAt.UTC())
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// Notices liệt kê thông báo mới nhất trước, sau con trỏ (noticed_at, id).
func Notices(ctx context.Context, tx pgx.Tx, kind string, afterAt *time.Time, afterID uuid.UUID, limit int) ([]Notice, error) {
	rows, err := tx.Query(ctx, `SELECT id, kind, subject_ref, subject_code, dedupe_key, audience, detail, param_version, rule_version,
	    noticed_at, recorded_at FROM mkg_logistics.alert_notice
	  WHERE ($1 = '' OR kind = $1) AND ($2::timestamptz IS NULL OR (noticed_at, id) < ($2, $3))
	  ORDER BY noticed_at DESC, id DESC LIMIT $4`, kind, utcPtr(afterAt), afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Notice
	for rows.Next() {
		var n Notice
		var detail []byte
		if err := rows.Scan(&n.ID, &n.Kind, &n.SubjectRef, &n.SubjectCode, &n.DedupeKey, &n.Audience, &detail, &n.ParamVersion,
			&n.RuleVersion, &n.NoticedAt, &n.RecordedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(detail, &n.Detail); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
