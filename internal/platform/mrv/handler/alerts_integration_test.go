package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func asPg(err error, pe **pgconn.PgError) bool { return errors.As(err, pe) }

func photo() []any { return []any{map[string]any{"file_ref": ids.New().String(), "sha256": sha()}} }

func gps() map[string]any { return map[string]any{"lat": "10.0300000", "lon": "105.7800000"} }

func pest(unit, category string, severity int, areaHa string) map[string]any {
	return map[string]any{
		"evidence_type": port.EvidencePestDisaster, "admin_unit_code": unit, "plot_ref": ids.New().String(), "season_code": "2026-DX",
		"values":    map[string]any{"hazard_kind": "sau_benh", "category": category, "severity_level": severity, "affected_area_ha": areaHa},
		"file_refs": photo(), "gps": gps(), "event_time": time.Now().Add(-time.Hour).Format(time.RFC3339),
	}
}

func infra(unit string, plot *uuid.UUID, ready bool) map[string]any {
	b := map[string]any{
		"evidence_type": port.EvidenceIrrigationInfra, "admin_unit_code": unit,
		"values":    map[string]any{"infra_ready": ready, "monitoring_tube": ready, "bund_ok": true, "water_control": ready},
		"file_refs": photo(), "gps": gps(), "event_time": time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}
	if plot != nil {
		b["plot_ref"] = plot.String()
	}
	return b
}

// alertEvents đếm pf.mrv.alert.raised trong outbox của tenant (cùng giao dịch với cảnh báo).
func (f *fixture) alertEvents(tid uuid.UUID, kind string) int {
	f.t.Helper()
	var n int
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM shared.outbox WHERE event_type = $1 AND payload->>'kind' = $2`,
			port.EventAlertRaised, kind).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture) alertCount(tid uuid.UUID, kind string) int {
	f.t.Helper()
	var n int
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_mrv.alert WHERE kind = $1`, kind).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

// KN US-MRV-04: ghi nhận sâu bệnh/thiên tai; vượt ngưỡng nguy hiểm cấu hình trước → tự bật nhãn
// "Cảnh báo khẩn" (không cần cán bộ đánh dấu) + sự kiện cho thông báo cấp trên. Idempotent POST
// replay; nhãn gắn với phiên bản — bản ghi bù dưới ngưỡng không mang nhãn; danh sách loại kích
// hoạt là tham số có phiên bản; tenant khác / ngoài phạm vi không thấy.
func TestPestUrgentLabelKNUSMRV04(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	rev := f.as(f.tenant, f.p89, port.RoleReviewer)

	body := pest("30001", "ray_nau", 4, "0.5")
	idem := "k-" + ids.New().String()
	r := col.do(f.app.URL, http.MethodPost, "/v1/mrv/evidence", idem, body)
	expect(t, r, 201, "")
	id := r.str("id")
	expect(t, col.do(f.app.URL, http.MethodPost, "/v1/mrv/evidence", idem, body), 201, "")
	if n := f.alertCount(f.tenant, port.AlertPestUrgent); n != 1 {
		t.Fatalf("gọi lặp cùng khóa không được gắn nhãn hai lần: %d", n)
	}
	if n := f.alertEvents(f.tenant, port.AlertPestUrgent); n != 1 {
		t.Fatalf("mong 1 sự kiện pf.mrv.alert.raised, có %d", n)
	}
	v := rev.app(http.MethodGet, "/v1/mrv/evidence/"+id, nil)
	expect(t, v, 200, "")
	alerts, _ := v.body["alerts"].([]any)
	if v.body["urgent"] != true || len(alerts) != 1 {
		t.Fatalf("minh chứng vượt ngưỡng phải mang nhãn khẩn: %v", v.body)
	}
	a := alerts[0].(map[string]any)
	params, _ := a["params"].([]any)
	if a["kind"] != port.AlertPestUrgent || a["label"] != "Cảnh báo khẩn" || a["rule_version"] != port.RulePestUrgent || len(params) != 3 ||
		params[0].(map[string]any)["source"] != "default" {
		t.Fatalf("nhãn khẩn: %v", a)
	}

	low := col.app(http.MethodPost, "/v1/mrv/evidence", pest("30001", "dao_on", 2, "1"))
	expect(t, low, 201, "")
	if v := rev.app(http.MethodGet, "/v1/mrv/evidence/"+low.str("id"), nil); v.body["urgent"] != false {
		t.Fatalf("dưới ngưỡng không có nhãn: %v", v.body)
	}

	// Bản ghi bù hạ cấp gây hại: phiên bản mới không mang nhãn; phiên bản cũ giữ nhãn (lịch sử).
	fix := pest("30001", "ray_nau", 2, "0.5")
	fix["plot_ref"], fix["supersedes_id"], fix["correction_reason"] = body["plot_ref"], id, "Kiểm tra lại: cấp gây hại thực tế là cấp 2"
	fr := col.app(http.MethodPost, "/v1/mrv/evidence", fix)
	expect(t, fr, 201, "")
	if v := rev.app(http.MethodGet, "/v1/mrv/evidence/"+fr.str("id"), nil); v.body["urgent"] != false || len(v.body["alerts"].([]any)) != 1 {
		t.Fatalf("bản ghi bù dưới ngưỡng: không khẩn, vẫn thấy lịch sử nhãn của chuỗi: %v", v.body)
	}
	list := rev.app(http.MethodGet, "/v1/mrv/evidence?evidence_type="+port.EvidencePestDisaster, nil)
	for _, it := range items(list) {
		if it["urgent"] != false {
			t.Fatalf("bản hiện hành không vượt ngưỡng: %v", it)
		}
	}

	// Danh sách loại kích hoạt là tham số có phiên bản (đã duyệt v2): ray_nau không còn kích hoạt.
	f.params.setText(port.ParamUrgentCategories, "lu_lut, xam_nhap_man", 2)
	if v := rev.app(http.MethodGet, "/v1/mrv/evidence/"+col.app(http.MethodPost, "/v1/mrv/evidence", pest("30001", "ray_nau", 5, "9")).str("id"), nil); v.body["urgent"] != false {
		t.Fatalf("loại ngoài danh sách v2 không khẩn: %v", v.body)
	}
	flood := col.app(http.MethodPost, "/v1/mrv/evidence", pest("30001", "lu_lut", 3, "12.5"))
	fv := rev.app(http.MethodGet, "/v1/mrv/evidence/"+flood.str("id"), nil)
	if fv.body["urgent"] != true {
		t.Fatalf("lũ lụt 12,5 ha ≥ 5 ha phải khẩn: %v", fv.body)
	}
	p0 := fv.body["alerts"].([]any)[0].(map[string]any)["params"].([]any)[0].(map[string]any)
	if p0["key"] != port.ParamUrgentCategories || p0["version"] != 2.0 || p0["source"] != "approved" || p0["value"] != "lu_lut, xam_nhap_man" {
		t.Fatalf("cảnh báo ghim phiên bản tham số đã dùng: %v", p0)
	}

	// Danh sách cảnh báo: trong phạm vi; tenant khác không thấy gì.
	al := rev.app(http.MethodGet, "/v1/mrv/alerts?kind="+port.AlertPestUrgent, nil)
	expect(t, al, 200, "")
	if n := len(items(al)); n != 2 {
		t.Fatalf("mong 2 cảnh báo khẩn, có %d: %v", n, al.body)
	}
	if n := len(items(f.as(ids.New(), f.p89, port.RoleViewer).app(http.MethodGet, "/v1/mrv/alerts", nil))); n != 0 {
		t.Fatalf("tenant khác thấy %d cảnh báo", n)
	}
	expect(t, rev.app(http.MethodGet, "/v1/mrv/alerts?kind=khac", nil), 422, "validation_failed")
	expect(t, f.as(f.tenant, f.p89, "extension_officer").app(http.MethodGet, "/v1/mrv/alerts", nil), 403, "forbidden")
}

// KN FR-08: nhật ký App HTX ghi AWD nhưng thửa/xã chưa có minh chứng hạ tầng tưới tiêu đạt → tự cờ
// + sự kiện thông báo cán bộ xã phụ trách. Giao lặp/phát lại không cờ hai lần. Hạ tầng đạt của thửa,
// hoặc của cả xã (không gắn thửa), bỏ cờ; hạ tầng "chưa đạt" hoặc bị "Yêu cầu kiểm tra lại" không tính.
func TestAWDWithoutInfrastructureFlagKNFR08(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	rev := f.as(f.tenant, f.p89, port.RoleReviewer)
	now := time.Now()
	awdLog := func(s g3Season, act string) {
		f.t.Helper()
		env := envelope(f.tenant, port.EventFarmLogRecorded, logRecorded(s, ids.New(), ids.New(), 1, act, now.Add(-2*time.Hour)))
		f.must(f.farmLog(env))
		f.must(f.farmLog(env)) // giao lặp
		env.EventID = ids.New()
		f.must(f.farmLog(env)) // phát lại với event_id mới
	}
	want := func(n int, msg string) {
		f.t.Helper()
		if got := f.alertCount(f.tenant, port.AlertAWDNoInfrastructure); got != n {
			t.Fatalf("%s: mong %d cờ, có %d", msg, n, got)
		}
		if got := f.alertEvents(f.tenant, port.AlertAWDNoInfrastructure); got != n {
			t.Fatalf("%s: mong %d sự kiện, có %d", msg, n, got)
		}
	}

	s1 := f.openSeason(f.tenant, "30001", now.Add(-72*time.Hour))
	awdLog(s1, "tuoi_nuoc")
	want(1, "AWD chưa có hạ tầng")
	al := items(rev.app(http.MethodGet, "/v1/mrv/alerts?kind="+port.AlertAWDNoInfrastructure, nil))
	if len(al) != 1 || al[0]["subject_type"] != port.AlertSubjectFarmLog || al[0]["plot_ref"] != s1.plot.String() ||
		al[0]["season_ref"] != s1.ref.String() || al[0]["admin_unit_code"] != "30001" {
		t.Fatalf("cờ AWD: %v", al)
	}
	awdLog(s1, "bon_phan")
	want(1, "hoạt động không phải AWD")

	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", infra("30001", &s1.plot, true)), 201, "")
	awdLog(s1, "tuoi_nuoc")
	want(1, "thửa có hạ tầng đạt")

	s2 := f.openSeason(f.tenant, "30001", now.Add(-72*time.Hour))
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", infra("30001", &s2.plot, false)), 201, "")
	awdLog(s2, "tuoi_nuoc")
	want(2, "hạ tầng chưa đạt")

	s3 := f.openSeason(f.tenant, "30001", now.Add(-72*time.Hour))
	ev := col.app(http.MethodPost, "/v1/mrv/evidence", infra("30001", &s3.plot, true))
	expect(t, rev.app(http.MethodPost, "/v1/mrv/evidence/"+ev.str("id")+"/reviews",
		map[string]any{"decision": "rejected", "reason": "Ảnh không thấy ống quan trắc"}), 201, "")
	awdLog(s3, "tuoi_nuoc")
	want(3, "hạ tầng bị yêu cầu kiểm tra lại")

	// Xác nhận hạ tầng cho cả xã (không gắn thửa) → thửa mới của xã không bị cờ.
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", infra("30001", nil, true)), 201, "")
	s4 := f.openSeason(f.tenant, "30001", now.Add(-72*time.Hour))
	awdLog(s4, "tuoi_nuoc")
	want(3, "hạ tầng đạt của cả xã")

	// Mã hoạt động AWD là tham số có phiên bản.
	f.params.setText(port.ParamAWDActivityCodes, "tuoi_nuoc,awd_kho", 2)
	s5 := f.openSeason(f.tenant, "30002", now.Add(-72*time.Hour))
	awdLog(s5, "awd_kho")
	want(4, "mã AWD mới của tham số v2")
}

// KN US-MRV-06: hồ sơ quá hạn xử lý tự leo thang — quá SLA → người duyệt (mức 1), quá hạn leo
// thang → quản trị TTKN tỉnh (mức 2). Idempotent theo hồ sơ + mức; hồ sơ đã duyệt thôi leo thang;
// SLA là tham số có phiên bản; job tìm tenant bằng ag_outbox_publisher chỉ đọc hai cột.
func TestReviewOverdueEscalationKNUSMRV06(t *testing.T) {
	f := newFixture(t)
	f.approveSeed(f.tenant)
	ctx := context.Background()
	col := f.as(f.tenant, f.t30001, port.RoleCollector)
	rev := f.as(f.tenant, f.p89, port.RoleReviewer)
	e1 := col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")).str("id")
	e2 := col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")).str("id")
	now := time.Now()
	step := func(at time.Duration, want int, msg string) {
		t.Helper()
		n, err := f.mod.EscalateOverdue(ctx, f.tenant, now.Add(at))
		if err != nil || n != want {
			t.Fatalf("%s: leo thang %d (%v), mong %d", msg, n, err, want)
		}
	}
	step(0, 0, "chưa quá hạn")
	step(73*time.Hour, 2, "quá SLA 72 giờ → mức 1 cho cả hai hồ sơ")
	step(74*time.Hour, 0, "chạy lại không ghi thêm")
	expect(t, rev.app(http.MethodPost, "/v1/mrv/evidence/"+e2+"/reviews", map[string]any{"decision": "accepted", "reason": "Đủ ảnh và toạ độ"}), 201, "")
	step(121*time.Hour, 1, "quá 120 giờ → mức 2 chỉ cho hồ sơ còn chờ")
	step(500*time.Hour, 0, "đã tới mức 2")
	if n := f.alertEvents(f.tenant, port.AlertReviewOverdue); n != 3 {
		t.Fatalf("mong 3 sự kiện leo thang, có %d", n)
	}
	al := items(rev.app(http.MethodGet, "/v1/mrv/alerts?kind="+port.AlertReviewOverdue, nil))
	levels := map[string][]float64{}
	for _, a := range al {
		levels[a["subject_id"].(string)] = append(levels[a["subject_id"].(string)], a["level"].(float64))
	}
	if len(al) != 3 || len(levels[e1]) != 2 || len(levels[e2]) != 1 {
		t.Fatalf("leo thang theo hồ sơ + mức: %v", al)
	}

	// SLA là tham số có phiên bản (đã duyệt v3 = 1 giờ).
	f.params.set(port.ParamReviewSLAHours, "1", 3)
	e3 := col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")).str("id")
	n, err := f.mod.EscalateOverdue(ctx, f.tenant, time.Now().Add(2*time.Hour))
	if err != nil || n != 1 {
		t.Fatalf("SLA v3: %d %v", n, err)
	}
	v := rev.app(http.MethodGet, "/v1/mrv/evidence/"+e3, nil)
	a3 := v.body["alerts"].([]any)[0].(map[string]any)
	if a3["level"] != 1.0 || a3["params"].([]any)[0].(map[string]any)["version"] != 3.0 {
		t.Fatalf("cảnh báo ghim SLA v3: %v", a3)
	}

	// Tenant khác: không đụng hồ sơ của tenant này.
	if n, err := f.mod.EscalateOverdue(ctx, ids.New(), now.Add(900*time.Hour)); err != nil || n != 0 {
		t.Fatalf("tenant khác: %d %v", n, err)
	}

	// Job: tìm tenant bằng ag_ops_jobs (chỉ tenant_id, review_status).
	pub := pgtest.Get(t).Pool(t, migrations.PF, "ag_ops_jobs")
	tids, err := mrv.PendingTenants(pub)(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tid := range tids {
		found = found || tid == f.tenant
	}
	if !found {
		t.Fatalf("PendingTenants thiếu tenant có hồ sơ chờ: %v", tids)
	}
	_, err = pub.Exec(ctx, "SELECT head_id FROM pf_mrv.evidence_current LIMIT 1")
	var pe *pgconn.PgError
	if !asPg(err, &pe) || pe.Code != "42501" {
		t.Fatalf("ag_ops_jobs không được đọc cột khác của evidence_current: %v", err)
	}
}
