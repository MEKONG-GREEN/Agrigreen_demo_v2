package main

// Bước "k_ref dựng lại khớp nguồn" của diễn tập phục hồi (W2.5, ADR-0029 §5, kiến trúc v4.1 §34): trên BẢN SAO
// cụm pf đã phục hồi, với mỗi tenant — chụp k_ref hiện có (trước), XÓA + DỰNG LẠI từ nguồn (pf/kref Rebuild:
// bao đóng tính bằng truy vấn đệ quy trong DB), chụp lại (sau), rồi so số dòng + băm theo quan hệ với một lần
// dựng ĐỘC LẬP từ nguồn (kref.Project: bao đóng tính trong Go). Sau ≠ mong đợi → diễn tập thất bại. Trước ≠
// mong đợi chỉ báo "lệch trước khi dựng" (k_ref trống sau phục hồi là bình thường — thứ tự §34).
//
//	drill kref -confirm-copy [-tenants uuid,uuid] [-report báo-cáo.json] [-out báo-cáo.md]
//
// Biến môi trường: AG_DB_URL (cụm pf của BẢN SAO), AG_DB_SUFFIX, AG_DB_PASSWORD_<VAI> hoặc AG_DB_APP_PASSWORD
// cho app_worker (ghi k_ref qua r_kref), party_app, masterdata_app, ag_ops_jobs.

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref"
	krefport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/kref/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// runKRefCmd là lệnh `drill kref`. Mã thoát: 0 khớp, 1 lệch/lỗi, 2 sai cấu hình.
func runKRefCmd(ctx context.Context, args []string, suffix string) (int, error) {
	fs := flag.NewFlagSet("drill kref", flag.ContinueOnError)
	confirm := fs.Bool("confirm-copy", false, "xác nhận database đích là BẢN SAO (lệnh xóa + dựng lại k_ref)")
	tenants := fs.String("tenants", "", "danh sách tenant (UUID, dấu phẩy); rỗng = mọi tenant có nguồn hoặc có dòng k_ref")
	reportPath := fs.String("report", "", "tệp báo cáo JSON (mặc định in stdout)")
	out := fs.String("out", "", "tệp báo cáo Markdown")
	fs.String("suffix", suffix, "hậu tố database (pgtest)")
	if err := fs.Parse(args); err != nil {
		return 2, fmt.Errorf("%w: %w", errConfig, err)
	}
	if !*confirm {
		return 2, fmt.Errorf("%w: thiếu -confirm-copy — lệnh xóa + dựng lại k_ref, chỉ chạy trên bản sao database", errConfig)
	}
	base := os.Getenv("AG_DB_URL")
	if base == "" {
		return 2, fmt.Errorf("%w: thiếu AG_DB_URL", errConfig)
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
		dsn, err := dbadmin.RoleURL(base, migrations.PF, suffix, role)
		if err != nil {
			return nil, err
		}
		p, err := db.Open(ctx, dsn, db.Options{AppName: "drill-kref", MaxConns: 2})
		if err != nil {
			return nil, err
		}
		pools[role] = p
		return p, nil
	}
	rep, err := RunKRef(ctx, KRefConfig{Pool: pool, Tenants: tids, Database: dbadmin.DatabaseName(migrations.PF, suffix)})
	if err != nil {
		return 1, err
	}
	raw, err := rep.JSON()
	if err != nil {
		return 1, err
	}
	if *reportPath != "" {
		if err := os.WriteFile(*reportPath, raw, 0o600); err != nil { //nolint:gosec // đường dẫn do người vận hành truyền vào
			return 1, err
		}
	} else {
		_, _ = os.Stdout.Write(append(raw, '\n'))
	}
	if *out != "" {
		if err := os.WriteFile(*out, []byte(rep.Markdown()), 0o600); err != nil { //nolint:gosec // như trên
			return 1, err
		}
	}
	if !rep.OK {
		return 1, fmt.Errorf("k_ref: %d quan hệ lệch nguồn sau dựng lại, %d lỗi", rep.Mismatches, rep.Errors)
	}
	return 0, nil
}

// KRefConfig là cấu hình bước k_ref. Pool mở pool theo vai trên cụm pf của bản sao.
type KRefConfig struct {
	Pool     func(role string) (*pgxpool.Pool, error)
	Tenants  []uuid.UUID // rỗng = mọi tenant có nguồn hoặc có dòng k_ref
	Database string
}

// KRefRelation là kết quả một quan hệ của một tenant.
type KRefRelation struct {
	Relation string      `json:"relation"`
	Before   kref.Digest `json:"before"`
	After    kref.Digest `json:"after"`
	Expected kref.Digest `json:"expected"`
	Match    bool        `json:"match"`        // After == Expected
	Drifted  bool        `json:"drift_before"` // Before != Expected (chỉ báo)
}

// KRefTenant là kết quả của một tenant.
type KRefTenant struct {
	TenantID  uuid.UUID      `json:"tenant_id"`
	Relations []KRefRelation `json:"relations"`
	Error     string         `json:"error,omitempty"`
}

// KRefReport là báo cáo bước k_ref.
type KRefReport struct {
	Database   string       `json:"database"`
	Tenants    []KRefTenant `json:"tenants"`
	Mismatches int          `json:"mismatches"`
	Drifted    int          `json:"drifted_before"`
	Errors     int          `json:"errors"`
	OK         bool         `json:"ok"`
}

// RunKRef chạy bước dựng lại k_ref + so khớp nguồn.
func RunKRef(ctx context.Context, cfg KRefConfig) (KRefReport, error) {
	rep := KRefReport{Database: cfg.Database}
	partyPool, err := cfg.Pool(party.DBRole)
	if err != nil {
		return rep, err
	}
	mdPool, err := cfg.Pool(masterdata.DBRole)
	if err != nil {
		return rep, err
	}
	workerPool, err := cfg.Pool(dbadmin.AppWorker)
	if err != nil {
		return rep, err
	}
	clk := clock.System{}
	pty, md := party.New(partyPool, clk), masterdata.New(mdPool, clk)
	src := krefport.Sources{Orgs: pty.KRefOrgs(), ShareGrants: pty.KRefShareGrants(), AdminUnits: md.KRefAdminUnits()}
	tids := cfg.Tenants
	if len(tids) == 0 {
		pub, err := cfg.Pool(dbadmin.OpsJobs)
		if err != nil {
			return rep, err
		}
		if tids, err = kref.UnionTenants(party.KRefTenants(pub), masterdata.KRefTenants(pub), kref.ProjectedTenants(pub))(ctx); err != nil {
			return rep, err
		}
	}
	mod := kref.New(workerPool, clk)
	for _, tid := range tids {
		t := KRefTenant{TenantID: tid}
		if err := krefTenant(ctx, mod, src, tid, &t); err != nil {
			t.Error = err.Error()
			rep.Errors++
		}
		for _, r := range t.Relations {
			if !r.Match {
				rep.Mismatches++
			}
			if r.Drifted {
				rep.Drifted++
			}
		}
		rep.Tenants = append(rep.Tenants, t)
	}
	rep.OK = rep.Mismatches == 0 && rep.Errors == 0
	return rep, nil
}

func krefTenant(ctx context.Context, mod *kref.Module, src krefport.Sources, tid uuid.UUID, t *KRefTenant) error {
	before, err := mod.Snapshot(ctx, tid)
	if err != nil {
		return err
	}
	if _, err := mod.RebuildTenant(ctx, src, tid); err != nil {
		return err
	}
	after, err := mod.Snapshot(ctx, tid)
	if err != nil {
		return err
	}
	// Lần dựng độc lập: đọc nguồn LẠI sau khi dựng (không dùng lại bản Rebuild đã dùng), tính trong Go.
	want, err := kref.Expected(ctx, src, tid)
	if err != nil {
		return err
	}
	db, da, dw := before.Digests(), after.Digests(), want.Digests()
	for _, r := range krefport.Relations {
		t.Relations = append(t.Relations, KRefRelation{Relation: r, Before: db[r], After: da[r], Expected: dw[r],
			Match: da[r] == dw[r], Drifted: db[r] != dw[r]})
	}
	return nil
}

// JSON trả báo cáo dạng JSON thụt lề.
func (r KRefReport) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Markdown trả báo cáo dạng bảng Markdown.
func (r KRefReport) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Diễn tập k_ref dựng lại khớp nguồn — %s\n\n", r.Database)
	status := "ĐẠT"
	if !r.OK {
		status = "KHÔNG ĐẠT"
	}
	fmt.Fprintf(&b, "Kết quả: **%s** — %d tenant, %d quan hệ lệch sau dựng lại, %d lỗi, %d quan hệ lệch trước khi dựng (chỉ báo).\n\n",
		status, len(r.Tenants), r.Mismatches, r.Errors, r.Drifted)
	b.WriteString("| Tenant | Quan hệ | Trước | Sau | Mong đợi | Khớp |\n|---|---|---|---|---|---|\n")
	for _, t := range r.Tenants {
		if t.Error != "" {
			fmt.Fprintf(&b, "| %s | — | lỗi: %s | | | không |\n", t.TenantID, t.Error)
			continue
		}
		for _, rel := range t.Relations {
			ok := "có"
			if !rel.Match {
				ok = "**không**"
			}
			fmt.Fprintf(&b, "| %s | %s | %d | %d | %d | %s |\n", t.TenantID, rel.Relation, rel.Before.Rows, rel.After.Rows, rel.Expected.Rows, ok)
		}
	}
	return b.String()
}
