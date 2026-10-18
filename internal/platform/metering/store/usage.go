// Package store: truy vấn SQL của pf/metering (schema pf_metering, vai metering_app). Mọi hàm nhận
// pgx.Tx đã đặt ngữ cảnh tenant (tenant.WithTenantTx / tenant.Within) — RLS lọc theo tenant.
package store

import (
	"context"
	"encoding/hex"
	"errors"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// UsageRow là một dòng sổ sử dụng.
type UsageRow struct {
	ID             uuid.UUID
	StreamKey      string
	Seq            int64
	ClientID       uuid.UUID
	ClientTenantID uuid.UUID
	KeyPrefix      string
	AuthMethod     string
	CallKind       string
	Product        string
	ContractVer    int
	Scope          string
	RouteTemplate  string
	Status         int
	ProblemCode    *string
	DataTenantID   *uuid.UUID
	Units          int64
	RowsOut        int64
	BytesOut       int64
	LatencyMS      int64
	Replayed       bool
	JobID          *uuid.UUID
	TraceID        *string
	EventTime      time.Time
	RecordedAt     time.Time
	IdemKey        string
	RuleVersion    string
	RowHash        []byte
}

const usageCols = `id, stream_key, seq, client_id, client_tenant_id, key_prefix, auth_method, call_kind, product,
  contract_version, scope, route_template, status, problem_code, data_tenant_id, units, rows_out, bytes_out, latency_ms,
  replayed, job_id, trace_id, event_time, recorded_at, idem_key, rule_version, row_hash`

func scanUsage(r pgx.Row) (UsageRow, error) {
	var u UsageRow
	var lat int32
	err := r.Scan(&u.ID, &u.StreamKey, &u.Seq, &u.ClientID, &u.ClientTenantID, &u.KeyPrefix, &u.AuthMethod, &u.CallKind, &u.Product,
		&u.ContractVer, &u.Scope, &u.RouteTemplate, &u.Status, &u.ProblemCode, &u.DataTenantID, &u.Units, &u.RowsOut, &u.BytesOut, &lat,
		&u.Replayed, &u.JobID, &u.TraceID, &u.EventTime, &u.RecordedAt, &u.IdemKey, &u.RuleVersion, &u.RowHash)
	u.LatencyMS = int64(lat)
	return u, err
}

// UsageByIdem đọc dòng sổ theo idem_key (request_id); nil khi chưa có.
func UsageByIdem(ctx context.Context, tx pgx.Tx, idem string) (*UsageRow, error) {
	u, err := scanUsage(tx.QueryRow(ctx, `SELECT `+usageCols+` FROM pf_metering.usage_event WHERE idem_key = $1`+ledger.NewestByIdem(""), idem))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// InsertUsage ghi một dòng sổ; seq/prev_hash/row_hash do shared.fn_chain tính.
func InsertUsage(ctx context.Context, tx pgx.Tx, tid uuid.UUID, u *UsageRow) error {
	return tx.QueryRow(ctx, `INSERT INTO pf_metering.usage_event (id, tenant_id, stream_key, client_id, client_tenant_id, key_prefix,
	    auth_method, call_kind, product, contract_version, scope, route_template, status, problem_code, data_tenant_id, units, rows_out,
	    bytes_out, latency_ms, replayed, job_id, trace_id, event_time, actor_ref, idem_key, rule_version)
	  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$4,$24,$25)
	  RETURNING seq, recorded_at, row_hash`,
		u.ID, tid, u.StreamKey, u.ClientID, u.ClientTenantID, u.KeyPrefix, u.AuthMethod, u.CallKind, u.Product, u.ContractVer, u.Scope,
		u.RouteTemplate, u.Status, u.ProblemCode, u.DataTenantID, u.Units, u.RowsOut, u.BytesOut, int32(u.LatencyMS), u.Replayed, u.JobID,
		u.TraceID, u.EventTime, u.IdemKey, u.RuleVersion).Scan(&u.Seq, &u.RecordedAt, &u.RowHash)
}

// UsageEvents liệt kê dòng sổ của một khách hàng (mới nhất trước, cursor = id).
func UsageEvents(ctx context.Context, tx pgx.Tx, client uuid.UUID, after uuid.UUID, limit int) ([]UsageRow, error) {
	rows, err := tx.Query(ctx, `SELECT `+usageCols+` FROM pf_metering.usage_event
	  WHERE client_id = $1 AND ($2::uuid IS NULL OR id < $2) ORDER BY id DESC LIMIT $3`, client, nullID(after), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageRow
	for rows.Next() {
		u, err := scanUsage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Counter là một dòng bộ đếm sử dụng.
type Counter struct {
	Units int64
	Calls int64
}

// LockCounter tạo (nếu chưa có) và khóa dòng bộ đếm (khách hàng, sản phẩm, cửa sổ) — mọi lời gọi cùng
// khách hàng xếp hàng ở dòng '*' (thứ tự khóa cố định: '*' ngày → '*' tháng → sản phẩm ngày → tháng).
func LockCounter(ctx context.Context, tx pgx.Tx, tid, client uuid.UUID, product, kind string, start time.Time) (Counter, error) {
	if _, err := tx.Exec(ctx, `INSERT INTO pf_metering.usage_counter_current (tenant_id, client_id, product, window_kind, window_start, units, calls)
	  VALUES ($1,$2,$3,$4,$5,0,0) ON CONFLICT DO NOTHING`, tid, client, product, kind, start); err != nil {
		return Counter{}, err
	}
	var c Counter
	err := tx.QueryRow(ctx, `SELECT units, calls FROM pf_metering.usage_counter_current
	  WHERE client_id = $1 AND product = $2 AND window_kind = $3 AND window_start = $4 FOR UPDATE`, client, product, kind, start).Scan(&c.Units, &c.Calls)
	return c, err
}

// BumpCounter cộng đơn vị và một lời gọi vào dòng bộ đếm đã khóa.
func BumpCounter(ctx context.Context, tx pgx.Tx, client uuid.UUID, product, kind string, start time.Time, units int64) error {
	_, err := tx.Exec(ctx, `UPDATE pf_metering.usage_counter_current SET units = units + $5, calls = calls + 1, updated_at = now()
	  WHERE client_id = $1 AND product = $2 AND window_kind = $3 AND window_start = $4`, client, product, kind, start, units)
	return err
}

// ReadCounter đọc bộ đếm đã commit (0 khi chưa có).
func ReadCounter(ctx context.Context, tx pgx.Tx, client uuid.UUID, product, kind string, start time.Time) (Counter, error) {
	var c Counter
	err := tx.QueryRow(ctx, `SELECT units, calls FROM pf_metering.usage_counter_current
	  WHERE client_id = $1 AND product = $2 AND window_kind = $3 AND window_start = $4`, client, product, kind, start).Scan(&c.Units, &c.Calls)
	if errors.Is(err, pgx.ErrNoRows) {
		return Counter{}, nil
	}
	return c, err
}

// UsageDayRow là tổng một ngày × sản phẩm.
type UsageDayRow struct {
	Date    time.Time
	Product string
	Calls   int64
	Units   int64
}

// UsageDays đọc bộ đếm ngày (không gồm dòng tổng '*') của khách hàng trong [from, to] (ngày giờ VN).
func UsageDays(ctx context.Context, tx pgx.Tx, client uuid.UUID, from, to time.Time) ([]UsageDayRow, error) {
	rows, err := tx.Query(ctx, `SELECT window_start, product, calls, units FROM pf_metering.usage_counter_current
	  WHERE client_id = $1 AND window_kind = 'day' AND product <> '*' AND window_start BETWEEN $2 AND $3
	  ORDER BY window_start, product`, client, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UsageDayRow
	for rows.Next() {
		var d UsageDayRow
		if err := rows.Scan(&d.Date, &d.Product, &d.Calls, &d.Units); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func nullID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// HexHash trả hex của row_hash.
func HexHash(b []byte) string { return hex.EncodeToString(b) }
