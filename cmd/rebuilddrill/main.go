// Lệnh rebuilddrill: diễn tập dựng lại bảng chiếu từ sổ (R-22, cổng G4 — docs/runbooks/rebuild-projection.md
// "Diễn tập G4") cho cụm mkg. Với mỗi tenant và mỗi module có bảng chiếu (hub, measurement, fieldops, logistics,
// orders, costing, acctbridge): băm mọi bảng chiếu (*_current, mkg_hub.lot_balance), gọi hàm dựng lại của module
// (module.RebuildTenant — cùng mã với POST …/projections/rebuild), băm lại và so. Hai băm phải bằng nhau.
//
// Lệnh GHI bảng chiếu (xóa + dựng lại trong giao dịch của module) → CHỈ chạy trên BẢN SAO database (bản phục hồi
// từ sao lưu vào môi trường sạch, hoặc bản sao ẩn danh ở staging — R-95); bắt buộc cờ -confirm-copy.
//
//	rebuilddrill -confirm-copy [-tenants uuid,uuid] [-modules hub,costing] [-out báo-cáo.md] [-json báo-cáo.json]
//
// Biến môi trường:
//
//	AG_MKG_DB_URL          URL gốc tới database cụm mkg của BẢN SAO (host, cổng, sslmode) — vai ghép theo module
//	AG_DB_SUFFIX           (tùy chọn) hậu tố database (như pgtest)
//	AG_DB_PASSWORD_<VAI>   hoặc AG_DB_APP_PASSWORD — vai của từng module + ag_ops_jobs (tìm tenant)
//
// Mã thoát: 0 mọi bảng khớp, 1 có bảng lệch / lỗi dựng lại, 2 sai cấu hình.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/residency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

var errConfig = errors.New("cấu hình")

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "rebuilddrill:", err)
	}
	os.Exit(code)
}

// OutboxTenants liệt kê tenant có sự kiện trên outbox cụm mkg (vai ag_ops_jobs, chỉ cột tenant_id).
func OutboxTenants(pool *pgxpool.Pool) func(ctx context.Context) ([]uuid.UUID, error) {
	return func(ctx context.Context) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, "SELECT DISTINCT tenant_id FROM shared.outbox")
		if err != nil {
			return nil, err
		}
		return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	}
}

func run(args []string) (int, error) {
	fs := flag.NewFlagSet("rebuilddrill", flag.ContinueOnError)
	confirm := fs.Bool("confirm-copy", false, "xác nhận database đích là BẢN SAO (lệnh ghi bảng chiếu)")
	tenants := fs.String("tenants", "", "danh sách tenant (UUID, dấu phẩy); rỗng = mọi tenant có sự kiện outbox")
	modules := fs.String("modules", "", "module (dấu phẩy); rỗng = mọi module")
	out := fs.String("out", "", "tệp báo cáo Markdown")
	jsonOut := fs.String("json", "", "tệp báo cáo JSON (mặc định in stdout)")
	if err := fs.Parse(args); err != nil {
		return 2, err
	}
	if !*confirm {
		return 2, fmt.Errorf("%w: thiếu -confirm-copy — lệnh xóa + dựng lại bảng chiếu, chỉ chạy trên bản sao database", errConfig)
	}
	if err := residency.CheckEnv(); err != nil { // RB-7
		return 2, err
	}
	base := os.Getenv("AG_MKG_DB_URL")
	if base == "" {
		return 2, fmt.Errorf("%w: thiếu AG_MKG_DB_URL", errConfig)
	}
	var tids []uuid.UUID
	for _, s := range strings.Split(*tenants, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return 2, fmt.Errorf("%w: tenant %q", errConfig, s)
		}
		tids = append(tids, id)
	}
	var mods []string
	for _, m := range strings.Split(*modules, ",") {
		if m = strings.TrimSpace(m); m != "" {
			mods = append(mods, m)
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	suffix := os.Getenv("AG_DB_SUFFIX")
	pools := map[string]*pgxpool.Pool{}
	defer func() {
		for _, p := range pools {
			p.Close()
		}
	}()
	pool := func(role string) (*pgxpool.Pool, error) {
		if p, ok := pools[role]; ok {
			return p, nil
		}
		dsn, err := dbadmin.RoleURL(base, migrations.MKG, suffix, role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, db.Options{AppName: "rebuilddrill", MaxConns: 4})
		if err != nil {
			return nil, err
		}
		pools[role] = p
		return p, nil
	}
	cfg := Config{Pool: pool, Tenants: tids, Modules: mods, Clock: clock.System{}, Database: dbadmin.DatabaseName(migrations.MKG, suffix)}
	if len(tids) == 0 {
		pub, err := pool(dbadmin.OpsJobs)
		if err != nil {
			return 2, err
		}
		cfg.Lister = OutboxTenants(pub)
	}
	rep, err := Run(ctx, cfg)
	if err != nil {
		return 1, err
	}
	raw, err := rep.JSON()
	if err != nil {
		return 1, err
	}
	if *jsonOut != "" {
		if err := os.WriteFile(*jsonOut, raw, 0o600); err != nil {
			return 1, err
		}
	} else {
		_, _ = os.Stdout.Write(append(raw, '\n'))
	}
	if *out != "" {
		if err := os.WriteFile(*out, []byte(rep.Markdown()), 0o600); err != nil {
			return 1, err
		}
	}
	if !rep.OK {
		return 1, fmt.Errorf("%d bảng chiếu lệch, %d lỗi dựng lại", rep.Mismatches, rep.Errors)
	}
	return 0, nil
}
