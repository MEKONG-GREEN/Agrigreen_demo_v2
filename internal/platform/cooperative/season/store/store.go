// Package store: SQL vụ mùa của pf/cooperative (schema pf_coop, W3.3 — chuyển từ pf/cropcycle), chỉ chạy trong giao dịch dưới vai r_cooperative
// (R-05) và luôn trong giao dịch có ngữ cảnh tenant (tenant.WithTenantTx) — RLS
// áp dụng. Tham số luôn ép kiểu tường minh vì pool dùng chế độ thực thi của
// PgBouncer (QueryExecModeExec). Số (diện tích, khối lượng) đi vào/ra dạng chuỗi
// decimal, không bao giờ float ở Go (R-10).
package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
)

func nilIfZero(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func deref(u *uuid.UUID) uuid.UUID {
	if u == nil {
		return uuid.Nil
	}
	return *u
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func decPtr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func parseDecPtr(s *string) (*decimal.Decimal, error) {
	if s == nil {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

func day(t time.Time) string { return domain.FormatDay(t) }

// uuids chuyển danh sách UUID sang []string cho tham số ::uuid[] (chế độ thực thi
// của PgBouncer mã hóa tham số dạng văn bản).
func uuids(u []uuid.UUID) []string {
	out := make([]string, 0, len(u))
	for _, x := range u {
		out = append(out, x.String())
	}
	return out
}

func parseUUIDs(s []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(s))
	for _, x := range s {
		u, err := uuid.Parse(x)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, nil
}

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// lockSync tuần tự hóa việc cấp sync_seq của một bảng chiếu trong tenant tới khi
// giao dịch kết thúc: giá trị sequence lấy sau khóa nên thứ tự commit trùng thứ
// tự sync_seq, con trỏ chiều xuống không bỏ sót dòng commit muộn (mẫu pf/geo).
func lockSync(ctx context.Context, tx pgx.Tx, table string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_coop.'||$1::text||'_sync:' || current_setting('app.tenant_id', true), 0))", table)
	return err
}

// LockPlot khóa tư vấn theo thửa trong giao dịch: tuần tự hóa mở vụ trên cùng
// thửa (HTX US-SEASON-04 — một vụ mở mỗi thửa).
func LockPlot(ctx context.Context, tx pgx.Tx, plot uuid.UUID) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('pf_coop.plot:' || current_setting('app.tenant_id', true) || ':' || $1::text, 0))", plot.String())
	return err
}

// gpsInsertSQL dựng điểm GPS từ hai tham số chuỗi decimal (NULL → NULL).
func gpsInsertSQL(lon, lat string) string {
	return "CASE WHEN " + lon + "::text IS NULL THEN NULL ELSE ST_SetSRID(ST_MakePoint(" + lon + "::text::numeric::float8, " + lat + "::text::numeric::float8), 4326) END"
}

// gpsSelect đọc lon, lat của cột gps dạng chuỗi decimal.
const gpsSelect = "ST_X(gps)::numeric(11,7)::text, ST_Y(gps)::numeric(10,7)::text"

// Filter là phạm vi đọc đã tính ở app.
type Filter struct {
	Units      []string    // xã (cán bộ KN)
	Coop       uuid.UUID   // quản lý HTX
	Households []uuid.UUID // nông dân
	Mode       int         // FilterAll | FilterUnits | FilterCoop | FilterHouseholds
}

// Chế độ lọc.
const (
	FilterNone = iota
	FilterAll
	FilterUnits
	FilterCoop
	FilterHouseholds
)

// scopeSQL trả điều kiện phạm vi trên bảng alias a với tham số bắt đầu từ $n
// (dùng 4 tham số: mode, units, coop, households).
func scopeSQL(a string, n int) string {
	p := func(i int) string { return "$" + itoa(n+i) }
	return "(" + p(0) + "::int = 1 OR (" + p(0) + "::int = 2 AND " + a + ".commune_code = ANY(" + p(1) + "::text[]))" +
		" OR (" + p(0) + "::int = 3 AND " + a + ".cooperative_ref = " + p(2) + "::uuid)" +
		" OR (" + p(0) + "::int = 4 AND " + a + ".household_ref = ANY(" + p(3) + "::uuid[])))"
}

func (f Filter) args() []any {
	return []any{f.Mode, strs(f.Units), f.Coop, uuids(f.Households)}
}

func itoa(n int) string { return decimal.NewFromInt(int64(n)).String() }
