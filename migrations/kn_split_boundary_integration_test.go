package migrations_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// KNSPLIT-0 (ADR-0055 "Tách hệ thống Khuyến nông", CEO D2/ADR-0052, kiểm kê docs/kn/tach-he-thong/kiem-ke.md §2):
// kiểm thử ÂM về quyền dữ liệu của chủ thể KN đối với dữ liệu HTX, chạy được ngay với tài khoản KN còn ở tenant lưu ký.
//
// Chủ thể KN ở mức DB có hai dạng:
//  1. vai nhóm của module KN (r_extension, r_price, r_hrm, r_contact) — mã KN chạy dưới các vai này;
//  2. phiên người dùng KN trên route KN do module HTX phục vụ (/v1/kn/plots, /v1/kn/cooperatives… — vai r_cooperative)
//     với ngữ cảnh Nút KN và mục đích khuyen-nong. Ở mô hình đích (KNSPLIT-4) phiên này ở tenant KN riêng, đọc qua
//     p_share; hiện tại (ĐTK 1) nó ở tenant lưu ký, đọc qua p_tenant + p_scope.
//
// Ca nào hỏng HÔM NAY chỉ vì tenant lưu ký được đánh dấu XFAIL (xfail bên dưới): ca vẫn chạy, ghi rõ lý do và gói gỡ
// (KNSPLIT-4); khi vi phạm hết xảy ra ca HỎNG ("XPASS") để buộc đổi thành khẳng định cứng — không bỏ qua im lặng.

// knModules: module KN (đồng bộ với tools/knboundary/boundary.yaml kn_modules).
var knModules = []string{"pf/extension", "pf/price", "pf/hrm", "pf/contact"}

// htxModules: module chủ dữ liệu HTX (D2: đồng sở hữu MKG + HTX).
var htxModules = []string{"pf/cooperative", "pf/cropcycle", "pf/mechanization"}

// htxSchemas trả schema nội bộ (không _pub) và schema _pub của các module HTX theo manifest.
func htxSchemas(t *testing.T) (internal, pub []string) {
	t.Helper()
	for _, e := range modreg.Catalog() {
		if !slices.Contains(htxModules, e.Name) {
			continue
		}
		for _, s := range e.OwnsSchema {
			if strings.HasSuffix(s, "_pub") {
				pub = append(pub, s)
			} else {
				internal = append(internal, s)
			}
		}
	}
	if len(internal) < len(htxModules) {
		t.Fatalf("manifest: schema HTX %v — thiếu module %v", internal, htxModules)
	}
	return internal, pub
}

// xfail ghi ca "hỏng có chủ đích" do tenant lưu ký: violated = vi phạm còn xảy ra → log XFAIL; hết vi phạm → lỗi XPASS.
func xfail(t *testing.T, violated bool, rule, why string) {
	t.Helper()
	if violated {
		t.Logf("XFAIL %s — %s", rule, why)
		return
	}
	t.Errorf("XPASS %s — quy tắc đã đúng; đổi ca XFAIL này thành khẳng định cứng (ADR-0055, KNSPLIT-4)", rule)
}

func permissionDenied(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "42501"
}

// Vai nhóm module KN không có quyền nào trên bảng/view/chuỗi số của schema nội bộ HTX, và câu SELECT/INSERT thật dưới
// vai đó bị từ chối (42501). Không phụ thuộc tenant — đúng ngay hôm nay.
func TestKNRolesNoHTXInternalAccess_KNSPLIT0(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	internal, _ := htxSchemas(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	type rel struct{ name, kind string }
	rows, err := tx.Query(ctx, `SELECT c.oid::regclass::text, c.relkind::text FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
	  WHERE n.nspname = ANY($1) AND c.relkind IN ('r', 'p', 'v', 'm', 'S') AND NOT c.relispartition ORDER BY 1`, internal)
	if err != nil {
		t.Fatal(err)
	}
	rels, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (rel, error) {
		var x rel
		return x, r.Scan(&x.name, &x.kind)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(rels) < 10 {
		t.Fatalf("schema HTX %v chỉ có %d quan hệ — migration chưa tới head?", internal, len(rels))
	}
	for _, m := range knModules {
		role := modreg.DBRole(m)
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)`, role).Scan(&exists); err != nil || !exists {
			t.Fatalf("vai %s không có (%v)", role, err)
		}
		for _, schema := range internal {
			var usage bool
			if err := tx.QueryRow(ctx, `SELECT has_schema_privilege($1, $2, 'USAGE')`, role, schema).Scan(&usage); err != nil {
				t.Fatal(err)
			}
			if usage {
				t.Errorf("%s có USAGE trên schema HTX %s", role, schema)
			}
		}
		for _, r := range rels {
			privs := []string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"}
			fn := "has_table_privilege"
			if r.kind == "S" {
				privs, fn = []string{"USAGE", "SELECT", "UPDATE"}, "has_sequence_privilege"
			}
			for _, p := range privs {
				var has bool
				if err := tx.QueryRow(ctx, `SELECT `+fn+`($1, $2::regclass, $3)`, role, r.name, p).Scan(&has); err != nil {
					t.Fatal(err)
				}
				if has {
					t.Errorf("%s có %s trên %s (dữ liệu HTX chỉ đọc qua _pub/cấp phép — D2)", role, p, r.name)
				}
			}
		}
		// Câu thật dưới vai KN: đọc và ghi bảng HTX đều 42501.
		for _, q := range []string{
			`SELECT 1 FROM pf_coop.plot LIMIT 0`,
			`SELECT 1 FROM pf_coop.household LIMIT 0`,
			`SELECT 1 FROM pf_coop.cooperative LIMIT 0`,
			`SELECT 1 FROM pf_mechanization.machine LIMIT 0`,
			`INSERT INTO pf_coop.plot_event DEFAULT VALUES`,
		} {
			scopeExec(ctx, t, tx, `SAVEPOINT kn`)
			scopeExec(ctx, t, tx, `SELECT set_config('role', $1, true)`, role)
			_, err := tx.Exec(ctx, q)
			scopeExec(ctx, t, tx, `ROLLBACK TO SAVEPOINT kn`)
			if !permissionDenied(err) {
				t.Errorf("%s: %s → %v, muốn 42501 permission denied", role, q, err)
			}
		}
	}
}

// Không vai nhóm module KN nào ghi được sổ (bảng có trg_chain) của module HTX: không quyền INSERT/UPDATE/DELETE và câu
// INSERT thật bị 42501. Đúng ngay hôm nay.
func TestKNRolesCannotWriteHTXLedgers_KNSPLIT0(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	internal, _ := htxSchemas(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `SELECT DISTINCT c.oid::regclass::text FROM pg_trigger tg JOIN pg_class c ON c.oid = tg.tgrelid
	  JOIN pg_namespace n ON n.oid = c.relnamespace
	  WHERE tg.tgname = 'trg_chain' AND n.nspname = ANY($1) AND NOT c.relispartition ORDER BY 1`, internal)
	if err != nil {
		t.Fatal(err)
	}
	ledgers, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"pf_coop.plot_event", "pf_mechanization.machine_event"} {
		if !slices.Contains(ledgers, want) {
			t.Fatalf("sổ HTX %v thiếu %s — trg_chain đổi tên?", ledgers, want)
		}
	}
	for _, m := range knModules {
		role := modreg.DBRole(m)
		for _, l := range ledgers {
			for _, p := range []string{"INSERT", "UPDATE", "DELETE"} {
				var has bool
				if err := tx.QueryRow(ctx, `SELECT has_table_privilege($1, $2::regclass, $3)`, role, l, p).Scan(&has); err != nil {
					t.Fatal(err)
				}
				if has {
					t.Errorf("%s có %s trên sổ HTX %s", role, p, l)
				}
			}
			scopeExec(ctx, t, tx, `SAVEPOINT kn`)
			scopeExec(ctx, t, tx, `SELECT set_config('role', $1, true)`, role)
			_, err := tx.Exec(ctx, `INSERT INTO `+l+` DEFAULT VALUES`)
			scopeExec(ctx, t, tx, `ROLLBACK TO SAVEPOINT kn`)
			if !permissionDenied(err) {
				t.Errorf("%s: INSERT %s → %v, muốn 42501", role, l, err)
			}
		}
	}
}

// insertPlotEventSQL: một dòng sổ thửa tối thiểu (trg_chain tính seq/hash; giá trị giữ chỗ bị ghi đè).
const insertPlotEventSQL = `INSERT INTO pf_coop.plot_event (id, tenant_id, stream_key, seq, plot_ref, action, code, commune_code,
    cooperative_ref, status, event_time, actor_ref, idem_key, rule_version, prev_hash, row_hash)
  VALUES (gen_random_uuid(), $1, 'plot:' || $2::uuid::text, 0, $2::uuid, 'created', 'TKNSPLIT', $3, $4::uuid, 'active', now(), gen_random_uuid(),
    'knsplit0-' || gen_random_uuid()::text, 'KNSPLIT-0', '\x', '\x')`

// Phiên người dùng KN trên route KN do module HTX phục vụ (vai r_cooperative, mục đích khuyen-nong):
//
//	đích (KN ở tenant riêng, đọc qua cấp phép):  không đọc bảng HTX ngoài cấp phép; không ghi bảng/sổ HTX; mất quyền
//	                                              ngay khi cấp phép bị thu hồi hoặc hết hạn — khẳng định cứng.
//	hiện tại (KN ở tenant lưu ký, Nút KN tỉnh):   đọc/ghi qua p_scope, thu hồi cấp phép không cắt — XFAIL (KNSPLIT-4).
func TestKNPrincipalHTXDataAccess_KNSPLIT0(t *testing.T) {
	ctx := context.Background()
	env := pgtest.Get(t)
	tx, err := env.AdminPool(t, migrations.PF).Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s := seedScope(ctx, t, tx, true) // tenant lưu ký: P (TT KN tỉnh) → X1, X2 (tổ KN xã) → C1, C2 (HTX)
	v := seedScope(ctx, t, tx, true) // tenant KN riêng (mô hình đích)
	g := ids.New()                   // cấp phép theo thỏa thuận HTX C1 (basis_ref = agreement_ref), mục đích khuyen-nong
	for _, key := range []string{"pf_coop.household", "pf_coop.plot"} {
		grantScope(ctx, t, tx, s.tid, v.tid, s.c1, g, key, sharePurpose, "-1 day", "+1 year")
	}
	sid := s.tid.String()
	plots := `SELECT ref FROM pf_coop.plot WHERE tenant_id = '` + sid + `'`
	households := `SELECT household_ref FROM pf_coop.household WHERE tenant_id = '` + sid + `'`
	coops := `SELECT org_ref FROM pf_coop.cooperative WHERE tenant_id = '` + sid + `'`
	revoke := func() {
		t.Helper()
		// Thu hồi như worker pf.kref: xóa dòng share_scope của cấp phép trong một giao dịch.
		scopeExec(ctx, t, tx, `DELETE FROM k_ref.share_scope WHERE grant_id = $1`, g)
	}
	expire := func() {
		t.Helper()
		scopeExec(ctx, t, tx, `UPDATE k_ref.share_scope SET valid_from = now() - interval '2 days', valid_to = now() - interval '1 second'
		  WHERE grant_id = $1`, g)
	}
	inSavepoint := func(name string, fn func()) {
		t.Helper()
		scopeExec(ctx, t, tx, `SAVEPOINT `+name)
		fn()
		asAdmin(ctx, t, tx)
		scopeExec(ctx, t, tx, `ROLLBACK TO SAVEPOINT `+name)
	}

	t.Run("dich_tenant_KN_rieng", func(t *testing.T) {
		// Có cấp phép: đúng phạm vi C1, không thấy bảng HTX không khai shares (hồ sơ HTX), không thấy C2.
		asViewer(ctx, t, tx, v.tid, v.p, sharePurpose)
		if got := visible(ctx, t, tx, plots); !sameSet(got, s.plot1) {
			t.Errorf("KN có cấp phép C1 thấy thửa %v, muốn chỉ thửa C1", got)
		}
		if got := visible(ctx, t, tx, households); !sameSet(got, s.hh1) {
			t.Errorf("KN có cấp phép C1 thấy hộ %v, muốn chỉ hộ C1", got)
		}
		if got := visible(ctx, t, tx, coops); len(got) != 0 {
			t.Errorf("KN đọc hồ sơ HTX (ngoài _pub/cấp phép): %v", got)
		}
		// Không ghi bảng hay sổ HTX của tenant lưu ký.
		if _, err := tryExec(ctx, t, tx, insertPlotSQL, s.tid, ids.New(), "TKNW1", xa1, s.c1); !rlsViolation(err) {
			t.Errorf("KN chèn thửa HTX: %v, muốn 42501", err)
		}
		if n, err := tryExec(ctx, t, tx, `UPDATE pf_coop.plot SET name = 'KNSPLIT-0' WHERE ref = $1`, s.plot1); err != nil || n != 0 {
			t.Errorf("KN sửa thửa chia sẻ: %d dòng, %v — muốn 0", n, err)
		}
		if _, err := tryExec(ctx, t, tx, insertPlotEventSQL, s.tid, s.plot1, xa1, s.c1); !rlsViolation(err) {
			t.Errorf("KN ghi sổ thửa HTX: %v, muốn 42501", err)
		}
		asAdmin(ctx, t, tx)

		// Thu hồi → mất quyền ngay (giao dịch kế tiếp).
		inSavepoint("rv", func() {
			revoke()
			asViewer(ctx, t, tx, v.tid, v.p, sharePurpose)
			if got := visible(ctx, t, tx, plots); len(got) != 0 {
				t.Errorf("sau thu hồi KN vẫn thấy thửa %v", got)
			}
			if got := visible(ctx, t, tx, households); len(got) != 0 {
				t.Errorf("sau thu hồi KN vẫn thấy hộ %v", got)
			}
		})
		// Hết hạn → mất quyền ngay trong chính sách, không chờ worker.
		inSavepoint("ex", func() {
			expire()
			asViewer(ctx, t, tx, v.tid, v.p, sharePurpose)
			if got := visible(ctx, t, tx, plots); len(got) != 0 {
				t.Errorf("cấp phép hết hạn KN vẫn thấy thửa %v", got)
			}
		})
	})

	t.Run("hien_tai_tenant_luu_ky", func(t *testing.T) {
		const why = "tài khoản KN còn ở tenant lưu ký (ADR-0052 §1, ĐTK 1): đọc/ghi theo p_tenant + p_scope của Nút KN, " +
			"không qua cấp phép — gỡ ở KNSPLIT-4 (chuyển tài khoản KN sang tenant KN + cấp phép hàng loạt)"
		// Cán bộ KN tỉnh: Nút P, mục đích khuyen-nong, cùng tenant với dữ liệu HTX.
		asViewer(ctx, t, tx, s.tid, s.p, sharePurpose)
		got := visible(ctx, t, tx, coops)
		xfail(t, len(got) > 0, "đọc bảng HTX ngoài _pub/cấp phép (pf_coop.cooperative)",
			fmt.Sprintf("KN tỉnh thấy %d hồ sơ HTX; %s", len(got), why))
		got = visible(ctx, t, tx, plots)
		xfail(t, slices.Contains(got, s.plot2), "đọc thửa HTX ngoài phạm vi cấp phép (C2)",
			fmt.Sprintf("KN tỉnh thấy %d thửa gồm C2; %s", len(got), why))
		_, err := tryExec(ctx, t, tx, insertPlotEventSQL, s.tid, s.plot1, xa1, s.c1)
		xfail(t, err == nil, "ghi sổ HTX (pf_coop.plot_event)", fmt.Sprintf("INSERT lỗi = %v; %s", err, why))
		if err != nil && !rlsViolation(err) {
			t.Errorf("ghi sổ thửa: lỗi không phải RLS — câu thử hỏng? %v", err)
		}
		asAdmin(ctx, t, tx)
		inSavepoint("rvc", func() {
			revoke()
			asViewer(ctx, t, tx, s.tid, s.p, sharePurpose)
			got := visible(ctx, t, tx, plots)
			xfail(t, slices.Contains(got, s.plot1), "mất quyền khi thu hồi cấp phép",
				fmt.Sprintf("sau thu hồi KN tỉnh vẫn thấy thửa C1 (%d thửa); %s", len(got), why))
		})
		inSavepoint("exc", func() {
			expire()
			asViewer(ctx, t, tx, s.tid, s.p, sharePurpose)
			got := visible(ctx, t, tx, plots)
			xfail(t, slices.Contains(got, s.plot1), "mất quyền khi cấp phép hết hạn",
				fmt.Sprintf("cấp phép hết hạn KN tỉnh vẫn thấy thửa C1; %s", why))
		})
	})
}
