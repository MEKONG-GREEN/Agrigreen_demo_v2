package reportstest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

const xlsxCT = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

func obj(r resp, k string) map[string]any { return sub(r.body, k) }

func sub(m map[string]any, k string) map[string]any { v, _ := m[k].(map[string]any); return v }

// doRaw gửi thân nhị phân (nạp Excel).
func (c caller) doRaw(path, ct, idemKey string, body []byte) resp {
	c.f.t.Helper()
	req, _ := http.NewRequest(http.MethodPost, c.f.admin.URL+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("Idempotency-Key", idemKey)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp{status: r.StatusCode, body: m}
}

// formsByCode gọi danh sách mẫu Admin và trả id theo mã.
func formsByCode(c caller) map[string]string {
	r := c.must(c.adminGet("/v1/reports/forms?department=trong_trot"), http.StatusOK, "danh sách mẫu")
	out := map[string]string{}
	for _, it := range items(r) {
		t := it["template"].(map[string]any)
		out[t["code"].(string)] = t["id"].(string)
	}
	return out
}

// TestFN_AD_04_06_FormLifecycleSoD: seed 14 mẫu → đề nghị → tự phát hành bị chặn (R-56) →
// người khác phát hành → bất biến (app + trigger DB) → phiên bản 2 áp dụng từ kỳ sau, kỳ
// cũ giữ phiên bản 1 → nhật ký.
func TestFN_AD_04_06_FormLifecycleSoD(t *testing.T) {
	f := newFixture(t, false)
	a := f.as(f.org.national, authz.RolePlatformAdmin)
	b := f.as(f.org.national, port.RoleReportsAdmin)
	seedResp := a.must(a.adminPost("/v1/reports/forms/seed", map[string]any{}), http.StatusOK, "seed")
	if n := len(seedResp.body["created"].([]any)); n != 14 {
		t.Fatalf("seed phải tạo 14 mẫu, tạo %d", n)
	}
	again := a.must(a.adminPost("/v1/reports/forms/seed", map[string]any{"department_code": "trong_trot"}), http.StatusOK, "seed lần 2")
	if len(again.body["created"].([]any)) != 0 || len(again.body["skipped"].([]any)) != 14 {
		t.Fatalf("seed lần 2 phải bỏ qua mẫu đã có: %v", again.body)
	}
	codes := formsByCode(a)
	if len(codes) != 14 {
		t.Fatalf("muốn 14 mẫu, có %v", codes)
	}
	weekly := a.must(a.adminGet("/v1/reports/forms?department=trong_trot&report_type=weekly"), http.StatusOK, "mẫu tuần")
	if len(items(weekly)) != 7 {
		t.Fatalf("T01–T07 gán Báo cáo tuần: %d", len(items(weekly)))
	}
	t01 := codes["T01"]
	base := "/v1/reports/forms/" + t01 + "/versions/1"
	v1 := a.must(a.adminGet(base), http.StatusOK, "đọc v1")
	if str(obj(v1, "version"), "status") != port.FormDraft || str(sub(obj(v1, "version"), "definition"), "code") != "T01" {
		t.Fatalf("v1 nháp: %v", v1.body)
	}

	// Đề nghị thiếu áp dụng từ kỳ → 422.
	if r := a.adminPost(base+"/propose", map[string]any{}); r.status != http.StatusUnprocessableEntity || code(r) != "reports.form_effective_invalid" {
		t.Fatalf("thiếu áp dụng từ kỳ: %d %v", r.status, r.body)
	}
	a.must(a.adminPost(base+"/propose", map[string]any{"effective_from": "2026-09-07", "effective_label": "Tuần 37/2026"}), http.StatusOK, "đề nghị")
	// Đã đề nghị: nội dung khóa.
	if r := a.adminPost(base+"/draft", map[string]any{"effective_label": "x"}); r.status != http.StatusConflict || code(r) != "reports.form_version_locked" {
		t.Fatalf("sửa khi đã đề nghị: %d %v", r.status, r.body)
	}
	// R-56: người đề nghị không tự phát hành.
	if r := a.adminPost(base+"/publish", map[string]any{}); r.status != http.StatusForbidden || code(r) != "self_approval_forbidden" {
		t.Fatalf("tự phát hành phải bị chặn: %d %v", r.status, r.body)
	}
	pub := b.must(b.adminPost(base+"/publish", map[string]any{}), http.StatusOK, "phát hành")
	if str(pub.body, "status") != port.FormPublished || str(pub.body, "published_by") != b.actor.String() || str(pub.body, "proposed_by") != a.actor.String() {
		t.Fatalf("phát hành: %v", pub.body)
	}
	ev := f.events(port.EventFormVersionPublished)
	var pl port.FormVersionPublishedV1
	if len(ev) != 1 || json.Unmarshal(ev[0].Payload, &pl) != nil || pl.Code != "T01" || pl.PublishedBy == pl.ProposedBy || pl.EffectiveFrom != "2026-09-07" {
		t.Fatalf("phải có đúng một sự kiện %s: %d %+v", port.EventFormVersionPublished, len(ev), pl)
	}
	// Bất biến: app.
	if r := b.adminPost(base+"/ops", map[string]any{"ops": []map[string]any{{"op": "delete_column", "letter": "O"}}}); r.status != http.StatusConflict || code(r) != "reports.form_version_locked" {
		t.Fatalf("sửa phiên bản đã phát hành: %d %v", r.status, r.body)
	}
	// Bất biến: trigger DB (kể cả vai ứng dụng gọi SQL trực tiếp).
	ctx := context.Background()
	err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE pf_reports.form_template_version SET definition = '{}'::jsonb WHERE template_id = $1::uuid AND version = 1`, t01)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "bất biến") {
		t.Fatalf("trigger phải chặn sửa phiên bản đã phát hành: %v", err)
	}
	err = tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM pf_reports.form_template_version WHERE template_id = $1::uuid`, t01)
		return err
	})
	if err == nil {
		t.Fatal("không được xóa phiên bản mẫu")
	}
	// R-56 ở DB: CHECK published_by <> proposed_by.
	t02 := codes["T02"]
	a.must(a.adminPost("/v1/reports/forms/"+t02+"/versions/1/propose", map[string]any{"effective_from": "2026-09-07"}), http.StatusOK, "đề nghị T02")
	err = tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE pf_reports.form_template_version SET status = 'published', published_by = proposed_by, published_at = now()
		  WHERE template_id = $1::uuid AND version = 1`, t02)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "check") {
		t.Fatalf("CHECK R-56 ở DB phải chặn: %v", err)
	}

	// Phiên bản 2: nháp từ v1, thêm cột, áp dụng từ kỳ sau.
	v2 := a.must(a.adminPost("/v1/reports/forms/"+t01+"/versions", map[string]any{}), http.StatusCreated, "nháp v2")
	if v2.body["version"].(float64) != 2 || str(v2.body, "origin") != "clone" {
		t.Fatalf("v2: %v", v2.body)
	}
	if r := a.adminPost("/v1/reports/forms/"+t01+"/versions", map[string]any{}); r.status != http.StatusConflict || code(r) != "reports.form_draft_exists" {
		t.Fatalf("chỉ một nháp mỗi mẫu: %d %v", r.status, r.body)
	}
	base2 := "/v1/reports/forms/" + t01 + "/versions/2"
	ops := []map[string]any{{"op": "insert_column", "at": "O", "side": "after", "column": map[string]any{"title": "Ghi chú", "kind": "input", "dtype": "text"}},
		{"op": "set_header_cell", "row": 1, "letter": "P", "text": "Ghi chú"}, {"op": "merge", "range": "P1:P2"}}
	opsResp := a.must(a.adminPost(base2+"/ops", map[string]any{"ops": ops}), http.StatusOK, "thao tác lưới")
	if cols := obj(opsResp, "definition")["columns"].([]any); len(cols) != 16 {
		t.Fatalf("thêm cột P: %d", len(cols))
	}
	// Xóa cột đang được công thức tham chiếu → reports.formula_invalid_ref.
	bad := []map[string]any{{"op": "update_column", "letter": "P", "column": map[string]any{"title": "Tỷ lệ", "kind": "formula", "dtype": "number", "formula": "IF(E#=0,0,M#/E#)"}}, {"op": "delete_column", "letter": "E"}}
	if r := a.adminPost(base2+"/ops", map[string]any{"ops": bad}); r.status != http.StatusUnprocessableEntity || code(r) != "reports.formula_invalid_ref" {
		t.Fatalf("xóa cột được tham chiếu: %d %v", r.status, r.body)
	}
	if r := a.adminPost(base2+"/propose", map[string]any{"effective_from": "2026-09-01"}); r.status != http.StatusUnprocessableEntity || code(r) != "reports.form_effective_invalid" {
		t.Fatalf("áp dụng sớm hơn phiên bản đã phát hành: %d %v", r.status, r.body)
	}
	a.must(a.adminPost(base2+"/propose", map[string]any{"effective_from": "2026-09-14", "effective_label": "Tuần 38/2026"}), http.StatusOK, "đề nghị v2")
	b.must(b.adminPost(base2+"/publish", map[string]any{}), http.StatusOK, "phát hành v2")

	// Application API: kỳ tuần 37 dùng v1, tuần 38 dùng v2.
	tinh := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	for at, want := range map[string]float64{"2026-09-10": 1, "2026-09-20": 2} {
		r := tinh.must(tinh.get("/v1/reports/forms/available?department=trong_trot&level=commune&report_type=weekly&at="+at), http.StatusOK, "mẫu khả dụng")
		if len(items(r)) != 1 || items(r)[0]["version"].(map[string]any)["version"].(float64) != want {
			t.Fatalf("ngày %s: muốn T01 v%v, nhận %v", at, want, r.body)
		}
	}
	lay := tinh.must(tinh.get("/v1/reports/forms/"+t01+"/versions/1/layout?period_label="+url.QueryEscape("Tuần 36")+"&period_label="+url.QueryEscape("Tuần 37")+"&season="+url.QueryEscape("THU ĐÔNG 2026")), http.StatusOK, "layout")
	cols := obj(lay, "definition")["columns"].([]any)
	if cols[5].(map[string]any)["title"] != "Tuần 36" || cols[7].(map[string]any)["inactive"] != true {
		t.Fatalf("layout đặt nhãn kỳ: %v", cols[5:8])
	}
	// Người đọc không thấy nháp.
	a.must(a.adminPost("/v1/reports/forms/"+t01+"/versions", map[string]any{"from_version": 1, "restore": true}), http.StatusCreated, "khôi phục v1 thành nháp v3")
	if r := tinh.get("/v1/reports/forms/" + t01 + "/versions/3/layout"); r.status != http.StatusNotFound {
		t.Fatalf("nháp không lộ ra Application API: %d", r.status)
	}
	// Ngừng dùng phiên bản cần lý do.
	if r := a.adminPost(base+"/retire", map[string]any{"reason": "ngắn"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("lý do ngắn: %d", r.status)
	}
	a.must(a.adminPost(base+"/retire", map[string]any{"reason": "Thay bằng phiên bản 2 từ Tuần 38"}), http.StatusOK, "ngừng dùng v1")

	// Nhật ký (cursor).
	log1 := a.must(a.adminGet("/v1/reports/forms/"+t01+"/log?limit=3"), http.StatusOK, "nhật ký")
	if len(items(log1)) != 3 || log1.body["next_cursor"] == nil || items(log1)[0]["event"] != "version_retired" {
		t.Fatalf("nhật ký trang 1: %v", log1.body)
	}
	all := []string{}
	cur := ""
	for i := 0; i < 10; i++ {
		q := "/v1/reports/forms/" + t01 + "/log?limit=3"
		if cur != "" {
			q += "&cursor=" + cur
		}
		r := a.must(a.adminGet(q), http.StatusOK, "nhật ký trang")
		for _, it := range items(r) {
			all = append(all, it["event"].(string))
		}
		if r.body["next_cursor"] == nil {
			break
		}
		cur = r.body["next_cursor"].(string)
	}
	want := "version_retired,draft_created,published,proposed,draft_ops_applied,draft_created,published,proposed,assigned,seeded"
	if got := strings.Join(all, ","); got != want {
		t.Fatalf("nhật ký: %s / muốn %s", got, want)
	}
}

// TestFN_AD_04_06_UnassignedCannotPublish: mẫu mới chưa gán loại báo cáo → không phát hành
// được; gán, gán trùng, bỏ gán cần lý do; mẫu đột xuất.
func TestFN_AD_04_06_UnassignedCannotPublish(t *testing.T) {
	f := newFixture(t, false)
	a := f.as(f.org.national, authz.RolePlatformAdmin)
	created := a.must(a.adminPost("/v1/reports/forms", map[string]any{"code": "DX01", "name": "Báo cáo đột xuất hạn mặn", "department_code": "trong_trot", "entry_level": "commune"}), http.StatusCreated, "tạo mẫu")
	id := str(obj(created, "template"), "id")
	if r := a.adminPost("/v1/reports/forms", map[string]any{"code": "DX01", "name": "x", "department_code": "trong_trot", "entry_level": "commune"}); r.status != http.StatusConflict || code(r) != "reports.form_code_taken" {
		t.Fatalf("trùng mã: %d", r.status)
	}
	base := "/v1/reports/forms/" + id + "/versions/1"
	if r := a.adminPost(base+"/propose", map[string]any{"effective_from": "2026-10-05"}); r.status != http.StatusConflict || code(r) != "reports.form_not_assigned" {
		t.Fatalf("chưa gán: %d %v", r.status, r.body)
	}
	asg := a.must(a.adminPost("/v1/reports/form-assignments", map[string]any{"template_id": id, "report_type": "adhoc"}), http.StatusCreated, "gán")
	if r := a.adminPost("/v1/reports/form-assignments", map[string]any{"template_id": id, "report_type": "adhoc"}); r.status != http.StatusConflict || code(r) != "reports.form_already_assigned" {
		t.Fatalf("gán trùng: %d", r.status)
	}
	if r := a.adminPost("/v1/reports/form-assignments", map[string]any{"template_id": id, "report_type": "yearly"}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("loại báo cáo lạ: %d", r.status)
	}
	// Công thức tham chiếu ô không tồn tại → không lưu.
	def := obj(created, "version")["definition"].(map[string]any)
	cols := def["columns"].([]any)
	cols = append(cols, map[string]any{"letter": "E", "title": "Tổng", "kind": "formula", "dtype": "number", "formula": "D#+Z#"})
	def["columns"] = cols
	if r := a.adminPost(base+"/draft", map[string]any{"definition": def}); r.status != http.StatusUnprocessableEntity || code(r) != "reports.formula_invalid_ref" {
		t.Fatalf("tham chiếu không tồn tại: %d %v", r.status, r.body)
	}
	a.must(a.adminPost(base+"/propose", map[string]any{"effective_from": "2026-10-05"}), http.StatusOK, "đề nghị sau khi gán")
	a.must(a.adminPost(base+"/withdraw", map[string]any{}), http.StatusOK, "rút đề nghị")
	if r := a.adminPost("/v1/reports/form-assignments/"+str(asg.body, "id")+"/revoke", map[string]any{}); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("bỏ gán cần lý do: %d", r.status)
	}
	a.must(a.adminPost("/v1/reports/form-assignments/"+str(asg.body, "id")+"/revoke", map[string]any{"reason": "Không còn yêu cầu báo cáo đột xuất"}), http.StatusOK, "bỏ gán")
	if r := a.adminGet("/v1/reports/form-assignments?template_id=" + id); len(items(r)) != 0 {
		t.Fatalf("gán đã bỏ không còn hiệu lực: %v", r.body)
	}
	// Vai không phải Admin không vào được trình thiết kế.
	xa := f.as(f.org.tBinhDuc, extport.RoleKNOfficer)
	if r := xa.adminPost("/v1/reports/forms", map[string]any{"code": "X1", "name": "x", "department_code": "trong_trot", "entry_level": "commune"}); r.status != http.StatusForbidden {
		t.Fatalf("Cán bộ xã không tạo mẫu: %d", r.status)
	}
	// Gọi lặp cùng Idempotency-Key trả cùng kết quả, không tạo mẫu thứ hai.
	k := key()
	r1 := a.do(f.admin, http.MethodPost, "/v1/reports/forms", k, map[string]any{"code": "DX02", "name": "Đột xuất 2", "department_code": "trong_trot", "entry_level": "province"})
	r2 := a.do(f.admin, http.MethodPost, "/v1/reports/forms", k, map[string]any{"code": "DX02", "name": "Đột xuất 2", "department_code": "trong_trot", "entry_level": "province"})
	if r1.status != http.StatusCreated || r2.status != http.StatusCreated || str(obj(r1, "template"), "id") != str(obj(r2, "template"), "id") {
		t.Fatalf("gọi lặp: %d %d", r1.status, r2.status)
	}
}

// TestFN_AD_04_06_FormTenantIsolation: tenant khác không thấy, không sửa được mẫu; cổng P4
// theo tenant tường minh.
func TestFN_AD_04_06_FormTenantIsolation(t *testing.T) {
	f := newFixture(t, false)
	a := f.as(f.org.national, authz.RolePlatformAdmin)
	a.must(a.adminPost("/v1/reports/forms/seed", map[string]any{}), http.StatusOK, "seed")
	t01 := formsByCode(a)["T01"]
	other := ids.New()
	o := f.asTenant(other, f.org.national, authz.RolePlatformAdmin)
	if len(formsByCode(o)) != 0 {
		t.Fatal("tenant khác không thấy mẫu")
	}
	if r := o.adminGet("/v1/reports/forms/" + t01); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác đọc mẫu: %d", r.status)
	}
	if r := o.adminPost("/v1/reports/forms/"+t01+"/versions/1/draft", map[string]any{"effective_label": "x"}); r.status != http.StatusNotFound {
		t.Fatalf("tenant khác sửa mẫu: %d", r.status)
	}
	eng := f.mod.Forms()
	ctx := context.Background()
	var vid string
	r := a.must(a.adminGet("/v1/reports/forms/"+t01+"/versions/1"), http.StatusOK, "v1")
	vid = str(obj(r, "version"), "id")
	if _, err := eng.FormVersion(ctx, f.tenant, mustUUID(t, vid)); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.FormVersion(ctx, other, mustUUID(t, vid)); !errors.Is(err, port.ErrFormNotFound) {
		t.Fatalf("cổng P4 phải cách ly tenant: %v", err)
	}
}

// TestFN_XA_03_02_FormEnginePortForP4: P4 lấy mẫu đã gán + phiên bản áp dụng theo kỳ, kiểm và
// tính lưới bài nộp, dựng dòng tổng — qua port.FormEngine (không đọc bảng pf_reports).
func TestFN_XA_03_02_FormEnginePortForP4(t *testing.T) {
	f := newFixture(t, false)
	a := f.as(f.org.national, authz.RolePlatformAdmin)
	b := f.as(f.org.national, authz.RolePlatformAdmin)
	a.must(a.adminPost("/v1/reports/forms/seed", map[string]any{}), http.StatusOK, "seed")
	codes := formsByCode(a)
	for _, c := range []string{"T01", "T03"} {
		base := "/v1/reports/forms/" + codes[c] + "/versions/1"
		a.must(a.adminPost(base+"/propose", map[string]any{"effective_from": "2026-09-07"}), http.StatusOK, "đề nghị "+c)
		b.must(b.adminPost(base+"/publish", map[string]any{}), http.StatusOK, "phát hành "+c)
	}
	eng := f.mod.Forms()
	ctx := context.Background()
	forms, err := eng.AssignedForms(ctx, f.tenant, "trong_trot", port.LevelCommune, port.ReportWeekly, time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err != nil || len(forms) != 2 || forms[0].Code != "T01" || forms[1].Code != "T03" {
		t.Fatalf("mẫu đã gán: %v %v", forms, err)
	}
	if none, _ := eng.AssignedForms(ctx, f.tenant, "trong_trot", port.LevelCommune, port.ReportWeekly, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); len(none) != 0 {
		t.Fatalf("trước kỳ áp dụng không có phiên bản: %v", none)
	}
	t01 := forms[0]
	res := eng.Evaluate(t01.Definition, port.EvalInput{Mode: port.EvalSubmit, OpenSlots: []int{2}, Rows: []port.EvalRow{
		{Commune: cBinhDuc, Region: "KV1", System: map[string]string{"D": "500"}, Input: map[string]string{"G": "120.5"}},
		{Commune: cMyThoi, Region: "KV1", System: map[string]string{"D": "300"}, Input: map[string]string{"G": "80"}},
	}})
	if !res.Valid || res.Rows[0].Cells["M"] != "120.5" {
		t.Fatalf("tính T01: %+v", res)
	}
	tot := eng.Totals(t01.Definition, res.Rows)
	if len(tot) != 2 || tot[1].Cells["M"] != "200.5" || tot[1].Cells["D"] != "800" {
		t.Fatalf("tổng: %+v", tot)
	}
	// T03 liên biểu: Σ trà phải bằng lũy kế T01 cùng xã cùng kỳ.
	t03 := forms[1]
	bad := eng.Evaluate(t03.Definition, port.EvalInput{Mode: port.EvalSubmit, Rows: []port.EvalRow{
		{Commune: cBinhDuc, Input: map[string]string{"E": "100"}, XRefs: map[string]map[string]string{"T01": {"M": res.Rows[0].Cells["M"]}}},
	}})
	if bad.Valid || bad.Issues[0].Code != "reports.rule_failed" || bad.Issues[0].Letter != "J" {
		t.Fatalf("T03 vi phạm ràng buộc liên biểu: %+v", bad)
	}
	// Application API tính thử cùng kết quả (server là nguồn đúng).
	tinh := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	r := tinh.must(tinh.post("/v1/reports/form-versions/"+t01.VersionID.String()+"/evaluate", map[string]any{"mode": "draft", "rows": []map[string]any{
		{"commune": cBinhDuc, "region": "KV1", "input": map[string]string{"F": "1.25", "G": "2"}},
	}}), http.StatusOK, "tính thử")
	rows := r.body["rows"].([]any)
	if r.body["valid"] != true || rows[0].(map[string]any)["cells"].(map[string]any)["M"] != "3.25" || len(r.body["totals"].([]any)) != 2 {
		t.Fatalf("tính thử: %v", r.body)
	}
	// Tải mẫu Excel đúng bố cục.
	req, _ := http.NewRequest(http.MethodGet, f.app.URL+"/v1/reports/forms/"+codes["T01"]+"/versions/1/template.xlsx", nil)
	req.Header.Set("Authorization", "Bearer "+tinh.token)
	hr, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	xb, _ := io.ReadAll(hr.Body)
	_ = hr.Body.Close()
	if hr.StatusCode != http.StatusOK || hr.Header.Get("Content-Type") != xlsxCT {
		t.Fatalf("tải mẫu Excel: %d %s", hr.StatusCode, hr.Header.Get("Content-Type"))
	}
	def, rep, err := reports.ImportFormXLSX(bytes.NewReader(xb), port.FormImportOptions{Code: "T01"})
	if err != nil || len(def.Columns) != 15 || def.Columns[12].Formula != "SUM(F#:L#)" || !rep.KindRow {
		t.Fatalf("tệp mẫu tải về nạp lại được: %v %+v", err, rep)
	}
}

// TestFN_AD_04_06_ImportEndpoint: nạp .xlsx qua Admin API → mẫu + nháp (lần nạp được ghi);
// tệp hỏng → reports.form_import_invalid, lần nạp lỗi vẫn ghi; gọi lặp không tạo bản thứ hai.
func TestFN_AD_04_06_ImportEndpoint(t *testing.T) {
	f := newFixture(t, false)
	a := f.as(f.org.national, authz.RolePlatformAdmin)
	var t04 port.FormDefinition
	for _, seedTemplate := range extension.ReportFormSeeds().Templates {
		d := seedTemplate.Def
		if d.Code == "T04" {
			t04 = d
		}
	}
	xb, err := reports.ExportFormXLSX(t04)
	if err != nil {
		t.Fatal(err)
	}
	q := "/v1/reports/forms/import?code=T04&name=" + url.QueryEscape("Xuống giống, thu hoạch rau màu (PL 03)") + "&department_code=trong_trot&file_name=PL03.xlsx"
	k := key()
	r := a.doRaw(q, xlsxCT, k, xb)
	if r.status != http.StatusCreated {
		t.Fatalf("nạp: %d %v", r.status, r.body)
	}
	ver := obj(r, "version")
	if ver["origin"] != "import" || ver["import_id"] != r.body["import_id"] || ver["column_count"].(float64) != 226 {
		t.Fatalf("nháp từ tệp: %v", ver)
	}
	if again := a.doRaw(q, xlsxCT, k, xb); again.status != http.StatusCreated || again.body["import_id"] != r.body["import_id"] {
		t.Fatalf("gọi lặp: %d %v", again.status, again.body["import_id"])
	}
	tid := str(obj(r, "template"), "id")
	// Nạp nháp mới cho mẫu có sẵn khi còn nháp → 409.
	if r2 := a.doRaw("/v1/reports/forms/import?template_id="+tid, xlsxCT, key(), xb); r2.status != http.StatusConflict || code(r2) != "reports.form_draft_exists" {
		t.Fatalf("còn nháp: %d %v", r2.status, r2.body)
	}
	bad := a.doRaw("/v1/reports/forms/import?code=T09&name=x&department_code=trong_trot", xlsxCT, key(), []byte("không phải xlsx"))
	if bad.status != http.StatusUnprocessableEntity || code(bad) != "reports.form_import_invalid" {
		t.Fatalf("tệp hỏng: %d %v", bad.status, bad.body)
	}
	if r3 := a.doRaw(q, "text/csv", key(), xb); r3.status != http.StatusUnprocessableEntity {
		t.Fatalf("sai Content-Type: %d", r3.status)
	}
	var parsed, failed int
	ctx := context.Background()
	if err := tenant.WithTenantTx(ctx, f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'parsed'), count(*) FILTER (WHERE status = 'failed') FROM pf_reports.form_import`).Scan(&parsed, &failed)
	}); err != nil {
		t.Fatal(err)
	}
	if parsed != 1 || failed != 1 {
		t.Fatalf("form_import: parsed %d failed %d", parsed, failed)
	}
	lg := a.must(a.adminGet("/v1/reports/forms/"+tid+"/log"), http.StatusOK, "nhật ký")
	evs := fmt.Sprint(items(lg))
	if !strings.Contains(evs, "imported") || !strings.Contains(evs, "template_created") {
		t.Fatalf("nhật ký nạp: %s", evs)
	}
}

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
