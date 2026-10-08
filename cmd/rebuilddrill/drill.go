package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// PoolFor trả pool của một vai DB cụm mkg (mỗi module đọc/dựng lại bằng vai của chính nó — R-05).
type PoolFor func(role string) (*pgxpool.Pool, error)

// rebuildFn dựng lại bảng chiếu của một tenant (tenant tường minh — module.RebuildTenant).
type rebuildFn func(ctx context.Context, tid uuid.UUID) (map[string]int64, error)

// target là một module có bảng chiếu trong cụm mkg.
type target struct {
	Module string
	Schema string
	// DB: vai nhóm của module (W2.6): băm và dựng lại chạy SET LOCAL ROLE r_<module> trên pool vai đăng nhập
	// app_worker_internal (Config.Pool).
	DB    db.ModuleID
	build func(pool tenant.Beginner, clk clock.Clock) rebuildFn // nil = module CHƯA có hàm dựng lại (chỉ băm, báo thiếu)
}

// Targets là mọi module mkg có bảng chiếu (docs/dev/g4-conventions.md §7: hub, measurement, fieldops, costing,
// logistics, orders, acctbridge).
var Targets = []target{
	{Module: "hub", Schema: "mkg_hub", DB: hub.DBModule, build: func(p tenant.Beginner, c clock.Clock) rebuildFn { return hub.New(p, c).RebuildTenant }},
	{Module: "measurement", Schema: "mkg_measurement", DB: measurement.DBModule, build: func(p tenant.Beginner, c clock.Clock) rebuildFn {
		return measurement.New(p, c, measurement.Deps{}).RebuildTenant
	}},
	// fieldops (G4): weighed_loading_current, order_plot_harvest_current là bảng đệm nguồn ngoài — không dựng lại, băm vẫn phải khớp.
	{Module: "fieldops", Schema: "mkg_fieldops", DB: fieldops.DBModule, build: func(p tenant.Beginner, c clock.Clock) rebuildFn {
		return fieldops.New(p, c, fieldops.Options{}).RebuildTenant
	}},
	{Module: "logistics", Schema: "mkg_logistics", DB: logistics.DBModule, build: func(p tenant.Beginner, c clock.Clock) rebuildFn {
		return logistics.New(p, c, logistics.Options{}).RebuildTenant
	}},
	{Module: "orders", Schema: "mkg_orders", DB: orders.DBModule, build: func(p tenant.Beginner, c clock.Clock) rebuildFn { return orders.New(p, c).RebuildTenant }},
	{Module: "costing", Schema: "mkg_costing", DB: costing.DBModule, build: func(p tenant.Beginner, c clock.Clock) rebuildFn {
		return costing.New(p, c, costing.Deps{}).RebuildTenant
	}},
	{Module: "acctbridge", Schema: "mkg_acctbridge", DB: acctbridge.DBModule, build: func(p tenant.Beginner, c clock.Clock) rebuildFn {
		return acctbridge.New(p, c, acctbridge.Deps{}).RebuildTenant
	}},
}

// VolatileColumns là cột do DEFAULT now() đặt khi ghi bảng chiếu — không thuộc nội dung dẫn xuất từ sổ nên
// bỏ khỏi băm (như store.ProjectionDump của costing).
var VolatileColumns = []string{"updated_at"}

// TableResult là băm một bảng chiếu của một tenant trước / sau dựng lại.
type TableResult struct {
	Module     string    `json:"module"`
	Table      string    `json:"table"`
	Tenant     uuid.UUID `json:"tenant"`
	RowsBefore int64     `json:"rows_before"`
	RowsAfter  int64     `json:"rows_after"`
	SHABefore  string    `json:"sha256_before"`
	SHAAfter   string    `json:"sha256_after"`
	Rebuilt    bool      `json:"rebuilt"` // module dựng lại bảng này (có trong kết quả RebuildTenant)
	Equal      bool      `json:"equal"`
}

// ModuleResult là một lần dựng lại của một module cho một tenant.
type ModuleResult struct {
	Module     string           `json:"module"`
	Tenant     uuid.UUID        `json:"tenant"`
	DurationMS int64            `json:"duration_ms"`
	Counts     map[string]int64 `json:"counts,omitempty"`
	Supported  bool             `json:"supported"`
	Error      string           `json:"error,omitempty"`
}

// Report là báo cáo diễn tập.
type Report struct {
	StartedAt  time.Time      `json:"started_at"`
	FinishedAt time.Time      `json:"finished_at"`
	Database   string         `json:"database"`
	Tenants    []uuid.UUID    `json:"tenants"`
	Modules    []ModuleResult `json:"modules"`
	Tables     []TableResult  `json:"tables"`
	Mismatches int            `json:"mismatches"`
	Errors     int            `json:"errors"`
	OK         bool           `json:"ok"`
}

// Config là cấu hình một lần diễn tập.
type Config struct {
	Pool     PoolFor
	Tenants  []uuid.UUID // rỗng → mọi tenant có sự kiện outbox (vai ag_ops_jobs)
	Lister   func(ctx context.Context) ([]uuid.UUID, error)
	Clock    clock.Clock
	Modules  []string // rỗng = mọi module
	Database string
}

func selectedModule(list []string, m string) bool {
	if len(list) == 0 {
		return true
	}
	for _, x := range list {
		if x == m {
			return true
		}
	}
	return false
}

// projectionTables liệt kê bảng chiếu của schema mà vai module thấy: *_current và mkg_hub.lot_balance.
func projectionTables(ctx context.Context, pool tenant.Beginner, schema string) ([]string, error) {
	// Trong giao dịch của module: information_schema chỉ liệt kê bảng vai hiện hành có quyền (r_<module>).
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT table_name FROM information_schema.tables
	  WHERE table_schema = $1 AND table_type = 'BASE TABLE' AND (table_name LIKE '%\_current' OR (table_schema = 'mkg_hub' AND table_name = 'lot_balance'))
	  ORDER BY table_name`, schema)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

// checksum băm tất định một bảng chiếu của tenant (RLS theo tenant): số dòng + SHA-256 của các dòng dạng jsonb
// (bỏ cột dễ biến), sắp theo văn bản.
func checksum(ctx context.Context, pool tenant.Beginner, tid uuid.UUID, schema, table string) (int64, string, error) {
	drop := ""
	for _, c := range VolatileColumns {
		drop += " - '" + c + "'"
	}
	q := fmt.Sprintf(`SELECT count(*), encode(sha256(convert_to(coalesce(string_agg(r, E'\n' ORDER BY r), ''), 'UTF8')), 'hex')
	  FROM (SELECT (to_jsonb(t)%s)::text AS r FROM %s.%s t) x`, drop, pgx.Identifier{schema}.Sanitize(), pgx.Identifier{table}.Sanitize())
	var n int64
	var h string
	err := tenant.WithTenantTx(ctx, pool, tid, func(tx pgx.Tx) error { return tx.QueryRow(ctx, q).Scan(&n, &h) })
	return n, h, err
}

// Run chạy diễn tập: với mỗi tenant, mỗi module — băm mọi bảng chiếu, gọi dựng lại từ sổ, băm lại, so.
// Diễn tập GHI bảng chiếu (xóa + dựng lại) — chỉ chạy trên BẢN SAO database (main kiểm cờ -confirm-copy).
func Run(ctx context.Context, cfg Config) (Report, error) {
	clk := cfg.Clock
	if clk == nil {
		clk = clock.System{}
	}
	rep := Report{StartedAt: clk.Now().UTC(), Database: cfg.Database}
	tids := cfg.Tenants
	if len(tids) == 0 && cfg.Lister != nil {
		var err error
		if tids, err = cfg.Lister(ctx); err != nil {
			return rep, err
		}
	}
	sort.Slice(tids, func(i, j int) bool { return tids[i].String() < tids[j].String() })
	rep.Tenants = tids
	for _, tg := range Targets {
		if !selectedModule(cfg.Modules, tg.Module) {
			continue
		}
		raw, err := cfg.Pool(dbadmin.AppWorkerInternal)
		if err != nil {
			return rep, fmt.Errorf("%s: pool %s: %w", tg.Module, dbadmin.AppWorkerInternal, err)
		}
		pool := db.ForModule(raw, tg.DB)
		tables, err := projectionTables(ctx, pool, tg.Schema)
		if err != nil {
			return rep, fmt.Errorf("%s: bảng chiếu: %w", tg.Module, err)
		}
		var rb rebuildFn
		if tg.build != nil {
			rb = tg.build(pool, clk)
		}
		for _, tid := range tids {
			before := map[string][2]any{}
			for _, t := range tables {
				n, h, err := checksum(ctx, pool, tid, tg.Schema, t)
				if err != nil {
					return rep, fmt.Errorf("%s.%s: băm trước: %w", tg.Schema, t, err)
				}
				before[t] = [2]any{n, h}
			}
			mr := ModuleResult{Module: tg.Module, Tenant: tid, Supported: rb != nil}
			if rb != nil {
				start := time.Now()
				mr.Counts, err = rb(ctx, tid)
				mr.DurationMS = time.Since(start).Milliseconds()
				if err != nil {
					mr.Error = err.Error()
					rep.Errors++
				}
			}
			rep.Modules = append(rep.Modules, mr)
			for _, t := range tables {
				n, h, err := checksum(ctx, pool, tid, tg.Schema, t)
				if err != nil {
					return rep, fmt.Errorf("%s.%s: băm sau: %w", tg.Schema, t, err)
				}
				b := before[t]
				_, rebuilt := mr.Counts[t]
				tr := TableResult{Module: tg.Module, Table: tg.Schema + "." + t, Tenant: tid, RowsBefore: b[0].(int64), SHABefore: b[1].(string),
					RowsAfter: n, SHAAfter: h, Rebuilt: rebuilt}
				tr.Equal = tr.RowsBefore == tr.RowsAfter && tr.SHABefore == tr.SHAAfter
				if !tr.Equal {
					rep.Mismatches++
				}
				rep.Tables = append(rep.Tables, tr)
			}
		}
	}
	rep.FinishedAt = clk.Now().UTC()
	rep.OK = rep.Mismatches == 0 && rep.Errors == 0
	return rep, nil
}

// JSON trả báo cáo dạng JSON thụt lề.
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Markdown trả báo cáo cho hồ sơ diễn tập (docs/runbooks/rebuild-projection.md "Diễn tập G4").
func (r Report) Markdown() string {
	var b strings.Builder
	verdict := "ĐẠT — mọi bảng chiếu dựng lại từ sổ bằng bảng chiếu hiện hành"
	if !r.OK {
		verdict = fmt.Sprintf("HỎNG — %d bảng lệch, %d lỗi dựng lại", r.Mismatches, r.Errors)
	}
	fmt.Fprintf(&b, "# Diễn tập dựng lại bảng chiếu (R-22)\n\n- Database: `%s`\n- Bắt đầu: %s, kết thúc: %s\n- Số tenant: %d\n- Kết quả: **%s**\n\n",
		r.Database, r.StartedAt.Format(time.RFC3339), r.FinishedAt.Format(time.RFC3339), len(r.Tenants), verdict)
	b.WriteString("## Module\n\n| Module | Tenant | Dựng lại | Thời gian (ms) | Số dòng | Lỗi |\n|---|---|---|---|---|---|\n")
	for _, m := range r.Modules {
		keys := make([]string, 0, len(m.Counts))
		for k := range m.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s=%d", k, m.Counts[k]))
		}
		sup := "có"
		if !m.Supported {
			sup = "**chưa có**"
		}
		fmt.Fprintf(&b, "| %s | `%s` | %s | %d | %s | %s |\n", m.Module, m.Tenant, sup, m.DurationMS, strings.Join(parts, ", "), m.Error)
	}
	b.WriteString("\n## Bảng chiếu\n\n| Bảng | Tenant | Dòng trước | Dòng sau | SHA-256 trước | SHA-256 sau | Được dựng lại | Khớp |\n|---|---|---|---|---|---|---|---|\n")
	for _, t := range r.Tables {
		eq, rb := "có", "có"
		if !t.Equal {
			eq = "**LỆCH**"
		}
		if !t.Rebuilt {
			rb = "không"
		}
		fmt.Fprintf(&b, "| `%s` | `%s` | %d | %d | `%s` | `%s` | %s | %s |\n", t.Table, t.Tenant, t.RowsBefore, t.RowsAfter, short(t.SHABefore),
			short(t.SHAAfter), rb, eq)
	}
	return b.String()
}

func short(h string) string {
	if len(h) > 16 {
		return h[:16]
	}
	return h
}
