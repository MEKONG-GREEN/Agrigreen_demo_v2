package kn77_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// text đọc một giá trị chuỗi (tài khoản quản trị, chỉ kiểm thử).
func (f *fixture) text(q string, args ...any) string {
	f.t.Helper()
	var s *string
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(context.Background(), q, args...).Scan(&s); err != nil {
		f.t.Fatal(err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

var errRollback = errors.New("rollback")

// tryExt chạy fn trong giao dịch tenant của vai pf/extension rồi luôn hoàn tác; trả lỗi của fn (nil nếu fn thành công).
func (f *fixture) tryExt(fn func(tx pgx.Tx) error) error {
	var got error
	err := tenant.WithTenantTx(context.Background(), f.extPool, f.tenant, func(tx pgx.Tx) error {
		got = fn(tx)
		return errRollback
	})
	if got == nil && !errors.Is(err, errRollback) {
		f.t.Fatalf("giao dịch: %v", err)
	}
	return got
}

func constraintOf(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		if pe.ConstraintName != "" {
			return pe.ConstraintName
		}
		return pe.Code
	}
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// TestW11_05b_PeriodTypesAndKeys: "Phát hành biểu" chia kỳ tuần KSGM (ranh giới theo tham số
// extension.ksgm_week_starts của tháng), kỳ ngày, kỳ tuần (week_year); loại kỳ ngoài danh sách của loại báo cáo và
// kỳ mới với lịch v1 bị từ chối; khóa kỳ LƯU ở sổ report_period_key lúc phát hành và ảnh chụp ghi song song
// cap/forms gửi khóa đã lưu (không tính lại khi tham số đổi). Cổng migration pf 000143: CHECK period_type mới đã
// VALIDATE, CHECK cũ bỏ; sổ khóa kỳ append-only, một dòng một kỳ, chỉ ghi cùng giao dịch phát hành, loại kỳ/quy tắc
// trùng đợt.
func TestW11_05b_PeriodTypesAndKeys(t *testing.T) {
	f := newFixture(t, fixtureOpts{})
	forms := f.publishForms("T01")
	tinh := f.kn(f.tenant, authz.KNTinh, dept, f.p89, nil)
	body := func(pt, from, to string) map[string]any {
		b := map[string]any{"report_type": "weekly", "template_ids": []string{forms["T01"]}, "scope": map[string]any{"mode": "province"},
			"from_date": from, "to_date": to, "season_label": "Đông Xuân 2026-2027"}
		if pt != "" {
			b["period_type"] = pt
		}
		return b
	}
	preview := func(pt, from, to string) map[string]any {
		return tinh.must(tinh.post("/v1/extension/report-batches/preview", body(pt, from, to)), http.StatusOK, "xem trước "+pt).body
	}

	// Lịch v1 (đợt bắt đầu trước 04/11/2026) không có kỳ ngày/KSGM/quý; loại kỳ phải thuộc loại báo cáo.
	tinh.fail(tinh.post("/v1/extension/report-batches/preview", body("ksgm_week", "2026-10-01", "2026-10-31")), http.StatusUnprocessableEntity,
		"validation_failed", "KSGM với lịch v1")
	tinh.fail(tinh.post("/v1/extension/report-batches/preview", body("quarter", "2026-11-04", "2026-12-31")), http.StatusUnprocessableEntity,
		"validation_failed", "quý với báo cáo tuần")

	// Kỳ ngày: hạn 09:00 hôm sau (giờ VN), khóa = ngày.
	day := preview("day", "2026-11-04", "2026-11-05")
	if ps := arr(day, "periods"); len(ps) != 2 || str(ps[0], "period_key") != "2026-11-04" || str(ps[0], "due_at") != "2026-11-05T02:00:00Z" ||
		ps[0]["week_year"] != nil {
		t.Fatalf("kỳ ngày: %v", ps)
	}
	// Tuần báo cáo v2 mang week_year.
	wk := preview("", "2026-11-04", "2026-11-10")
	if ps := arr(wk, "periods"); len(ps) != 1 || str(ps[0], "period_key") != "2026-W45" || num(ps[0], "week_year") != 2026 || num(ps[0], "iso_week") != 45 {
		t.Fatalf("tuần v2: %v", ps)
	}

	// Tuần KSGM tháng 12 theo tham số Cấp Tỉnh sửa: 1, 10, 16, 23.
	f.extParams.set(extport.ParamKSGMWeekStarts+":2026-12", "1,10,16,23")
	r := tinh.must(tinh.do(f.app, http.MethodPost, "/v1/extension/report-batches", "issue-ksgm-0001", body("ksgm_week", "2026-12-08", "2026-12-31")),
		http.StatusCreated, "phát hành KSGM")
	if str(obj(r.body, "batch"), "period_type") != "ksgm_week" {
		t.Fatalf("đợt KSGM: %v", obj(r.body, "batch"))
	}
	ps := arr(r.body, "periods")
	want := [][3]string{{"2026-12-K1", "2026-12-08", "2026-12-09"}, {"2026-12-K2", "2026-12-10", "2026-12-15"}, {"2026-12-K3", "2026-12-16", "2026-12-22"},
		{"2026-12-K4", "2026-12-23", "2026-12-31"}}
	if len(ps) != len(want) {
		t.Fatalf("kỳ KSGM: %v", ps)
	}
	for i, w := range want {
		if str(ps[i], "period_key") != w[0] || str(ps[i], "from_date") != w[1] || str(ps[i], "to_date") != w[2] || ps[i]["week_year"] != nil {
			t.Fatalf("kỳ KSGM %d: %v, muốn %v", i, ps[i], w)
		}
	}
	batchID, p1 := str(obj(r.body, "batch"), "id"), str(ps[0], "id")
	if n := f.count(`SELECT count(*) FROM pf_extension.report_period_key WHERE tenant_id = $1 AND batch_id = $2::uuid AND period_type = 'ksgm_week'
	    AND period_rule = 'kn.period_rule/v2;week_start=wed;week=year;month=week_end' AND week_year IS NULL`, f.tenant, batchID); n != 4 {
		t.Fatalf("sổ khóa kỳ: %d dòng", n)
	}

	// Tham số đổi về sau: khóa đã lưu giữ nguyên ở API và ở ảnh chụp cap/forms khi chốt (tính lại với ranh giới
	// mặc định 1, 8, 15, 22 sẽ ra 2026-12-K2 cho kỳ 08–09/12).
	f.extParams.set(extport.ParamKSGMWeekStarts+":2026-12", "1,8,15,22")
	got := tinh.must(tinh.get("/v1/extension/report-batches/"+batchID), http.StatusOK, "chi tiết đợt")
	if k := str(arr(got.body, "periods")[0], "period_key"); k != "2026-12-K1" {
		t.Fatalf("khóa đã lưu đổi theo tham số: %s", k)
	}
	tinh.must(tinh.post("/v1/extension/report-periods/"+p1+"/close", map[string]any{}), http.StatusOK, "chốt kỳ KSGM 1")
	if k := f.text(`SELECT s.content->>'period_key' FROM pf_forms.close_snapshot s WHERE s.tenant_id = $1 AND s.period_ref = $2::uuid AND s.seq = 1`,
		f.tenant, p1); k != "2026-12-K1" {
		t.Fatalf("cap/forms: ảnh chụp phải mang khóa đã lưu, nhận %s", k)
	}
	if pt := f.text(`SELECT s.content->>'period_type' FROM pf_forms.close_snapshot s WHERE s.tenant_id = $1 AND s.period_ref = $2::uuid`, f.tenant, p1); pt != "ksgm_week" {
		t.Fatalf("cap/forms: loại kỳ %s", pt)
	}

	// Trồng trọt cũ (lịch v1, tuần thứ Hai–Chủ nhật): khóa ISO cũng được lưu, nhãn "Tuần n" của đợt giữ nguyên.
	old := tinh.must(tinh.do(f.app, http.MethodPost, "/v1/extension/report-batches", "issue-v1-0001", body("", "2026-09-07", "2026-09-20")),
		http.StatusCreated, "phát hành v1")
	if ps := arr(old.body, "periods"); str(ps[0], "label") != "Tuần 1" || str(ps[0], "period_key") != "2026-W37" || num(ps[0], "week_year") != 2026 {
		t.Fatalf("đợt v1: %v", ps)
	}

	// ---- Cổng migration pf 000143 ----
	if s := f.text(`SELECT convalidated::text || ' ' || pg_get_constraintdef(oid) FROM pg_constraint
	    WHERE conrelid = 'pf_extension.report_batch'::regclass AND conname = 'report_batch_period_type_v2'`); s !=
		"true CHECK ((period_type = ANY (ARRAY['day'::text, 'week'::text, 'ksgm_week'::text, 'month'::text, 'quarter'::text, 'season'::text, 'year'::text, 'adhoc'::text])))" {
		t.Fatalf("CHECK period_type mới: %s", s)
	}
	if n := f.count(`SELECT count(*) FROM pg_constraint WHERE conrelid = 'pf_extension.report_batch'::regclass AND conname = 'report_batch_period_type_check'`); n != 0 {
		t.Fatal("CHECK period_type cũ chưa bỏ")
	}
	if err := f.tryExt(func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE pf_extension.report_batch SET period_type = 'fortnight' WHERE id = $1::uuid`, batchID)
		return err
	}); constraintOf(err) != "report_batch_period_type_v2" {
		t.Fatalf("loại kỳ lạ phải bị CHECK chặn: %v", err)
	}

	ctx := context.Background()
	p2 := str(ps[1], "id")
	rule := "kn.period_rule/v2;week_start=wed;week=year;month=week_end"
	// key ghi một dòng khóa kỳ bằng SQL thô của vai pf/extension (như store.InsertPeriodKey).
	key := func(tx pgx.Tx, period, pt, k string) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.report_period_key (id, tenant_id, stream_key, period_id, batch_id, period_type, period_key,
		    period_rule, event_time, actor_ref, idem_key, rule_version)
		  VALUES ($1, $2, 'period:' || $3, $3::uuid, $4::uuid, $5, $6, $7, now(), $8, $9, 'test')`,
			ids.New(), f.tenant, period, batchID, pt, k, rule, ids.New(), "gate-"+ids.New().String())
		return err
	}
	// issuedNow ghi một sự kiện 'issued' của đợt trong giao dịch (giả lập giao dịch phát hành).
	issuedNow := func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO pf_extension.report_batch_event (id, tenant_id, stream_key, batch_id, action, department_code, province_code,
		    payload, event_time, actor_ref, idem_key, rule_version)
		  SELECT $1, tenant_id, 'batch:' || id::text, id, 'issued', department_code, province_code, '{}'::jsonb, now(), $2, $3, 'test'
		    FROM pf_extension.report_batch WHERE id = $4::uuid`, ids.New(), ids.New(), "gate-"+ids.New().String(), batchID)
		return err
	}
	cases := []struct {
		name, want string
		fn         func(tx pgx.Tx) error
	}{
		{"ngoài giao dịch phát hành", "report_period_key_issue_tx", func(tx pgx.Tx) error {
			return key(tx, p2, "ksgm_week", "2026-12-K2")
		}},
		{"kỳ thứ hai của cùng kỳ", "report_period_key_one", func(tx pgx.Tx) error {
			if err := issuedNow(tx); err != nil {
				return err
			}
			return key(tx, p2, "ksgm_week", "2026-12-K2")
		}},
		{"loại kỳ khác đợt", "report_period_key_batch", func(tx pgx.Tx) error {
			if err := issuedNow(tx); err != nil {
				return err
			}
			return key(tx, p2, "month", "2026-12")
		}},
		{"kỳ không thuộc đợt", "report_period_key_period_fkey", func(tx pgx.Tx) error {
			return key(tx, ids.New().String(), "ksgm_week", "2026-12-K2")
		}},
		{"sửa sổ", "42501", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE pf_extension.report_period_key SET period_key = '2026-12-K3' WHERE period_id = $1`, p2)
			return err
		}},
		{"xóa sổ", "42501", func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `DELETE FROM pf_extension.report_period_key WHERE period_id = $1`, p2)
			return err
		}},
	}
	for _, c := range cases {
		if got := constraintOf(f.tryExt(c.fn)); got != c.want {
			t.Errorf("%s: muốn %s, nhận %s", c.name, c.want, got)
		}
	}
	// Cách ly tenant (RLS): tenant khác không thấy dòng khóa kỳ.
	var n int
	if err := tenant.WithTenantTx(ctx, f.extPool, ids.New(), func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM pf_extension.report_period_key`).Scan(&n)
	}); err != nil || n != 0 {
		t.Fatalf("RLS sổ khóa kỳ: %d %v", n, err)
	}
}
