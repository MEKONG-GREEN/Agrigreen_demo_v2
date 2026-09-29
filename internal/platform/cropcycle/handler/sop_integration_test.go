package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// HTX US-CAT-01 AC-1/AC-6: tạo SOP; bước không liên tục bị từ chối; tên trùng bị chặn;
// phát hành cần param_approver khác người soạn (R-56) và phải có bước thu hoạch;
// phiên bản đã phát hành bất biến; phát pf.cropcycle.sop.published.
func TestSOPAuthoringAndPublish_HTX_US_CAT_01(t *testing.T) {
	f := newFixture(t)
	bad := steps()
	bad[2]["seq"] = 4
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sops", map[string]any{"code": "SOP-X", "name": "SOP lỗi", "steps": bad}),
		http.StatusUnprocessableEntity, "cropcycle.sop_step_order", "bước không liên tục")

	r := f.sopAdmin.adminPost("/v1/cropcycle/sops", map[string]any{"code": "SOP-OM", "name": "SOP lúa OM5451", "standard": "vietgap", "steps": steps()})
	expect(t, r, http.StatusCreated, "tạo SOP")
	tmpl, ver := str(r.body, "ref"), str(list(r.body, "versions")[0], "id")
	if str(r.body, "status") != "draft" || num(list(r.body, "versions")[0], "harvest_day_offset") != 95 {
		t.Fatalf("SOP mới: %s", r.raw)
	}
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sops", map[string]any{"code": "SOP-OM2", "name": "  sop lúa   OM5451 ", "steps": steps()}),
		http.StatusConflict, "cropcycle.sop_name_taken", "tên trùng (chuẩn hóa)")
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sops", map[string]any{"code": "SOP-OM", "name": "Tên khác", "steps": steps()}),
		http.StatusConflict, "cropcycle.sop_code_taken", "mã trùng")

	// Ứng dụng chưa thấy mẫu nháp.
	if n := len(list(f.managerA().get("/v1/cropcycle/sops").body, "items")); n != 0 {
		t.Fatalf("mẫu nháp không được hiện ở App API: %d", n)
	}
	// R-56: người soạn có cả vai param_approver cũng không tự phát hành.
	both := f.as(f.tenant, f.coopA, port.RoleSOPAdmin, authz.RoleParamApprover)
	r = both.adminPost("/v1/cropcycle/sops", map[string]any{"code": "SOP-SELF", "name": "SOP tự duyệt", "steps": steps()})
	expect(t, r, http.StatusCreated, "tạo SOP 2")
	expectCode(t, both.adminPost("/v1/cropcycle/sop-versions/"+str(list(r.body, "versions")[0], "id")+"/publish", nil),
		http.StatusForbidden, "self_approval_forbidden", "tự phát hành")
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+ver+"/publish", nil), http.StatusForbidden, "forbidden", "sop_admin không phát hành")

	// Thiếu bước thu hoạch → không phát hành được.
	noHarvest := steps()[:4]
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+ver+"/steps", map[string]any{"steps": noHarvest}), http.StatusOK, "sửa nháp")
	expectCode(t, f.approver.adminPost("/v1/cropcycle/sop-versions/"+ver+"/publish", nil), http.StatusUnprocessableEntity,
		"cropcycle.sop_harvest_step_missing", "thiếu bước thu hoạch")
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+ver+"/steps", map[string]any{"steps": steps()}), http.StatusOK, "sửa nháp lại")

	r = f.approver.adminPost("/v1/cropcycle/sop-versions/"+ver+"/publish", nil)
	expect(t, r, http.StatusOK, "phát hành")
	if str(r.body, "status") != "published" || num(r.body, "published_version") != 1 {
		t.Fatalf("sau phát hành: %s", r.raw)
	}
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+ver+"/steps", map[string]any{"steps": steps()}),
		http.StatusConflict, "cropcycle.sop_state", "sửa bản đã phát hành")
	// BR-07: trigger chặn sửa bản đã phát hành ngay cả khi đi thẳng vào DB bằng vai ứng dụng.
	err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE pf_cropcycle.sop_version SET step_count = 0 WHERE id = $1::uuid", ver)
		return err
	})
	if !errors.Is(ledger.Classify(err), ledger.ErrImmutable) {
		t.Fatalf("UPDATE bản đã phát hành phải bị trigger chặn: %v", err)
	}
	ev := f.outbox(port.EventSOPPublished)
	if len(ev) != 1 || str(ev[0], "template_ref") != tmpl || str(ev[0], "authored_by") == str(ev[0], "published_by") {
		t.Fatalf("sự kiện sop.published: %v", ev)
	}
	// Phiên bản mới chép bước; chỉ một bản nháp mỗi lúc.
	r = f.sopAdmin.adminPost("/v1/cropcycle/sops/"+tmpl+"/versions", map[string]any{"change_note": "Điều chỉnh cửa sổ bón phân"})
	expect(t, r, http.StatusCreated, "phiên bản 2")
	if len(list(r.body, "versions")) != 2 || len(list(list(r.body, "versions")[1], "steps")) != 5 {
		t.Fatalf("phiên bản 2 phải chép 5 bước: %s", r.raw)
	}
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sops/"+tmpl+"/versions", nil), http.StatusConflict, "cropcycle.sop_state", "hai bản nháp")
	// App API thấy mẫu đã ban hành và chỉ phiên bản đã phát hành.
	r = f.managerA().get("/v1/cropcycle/sops/" + tmpl)
	expect(t, r, http.StatusOK, "đọc SOP App API")
	if len(list(r.body, "versions")) != 1 {
		t.Fatalf("App API chỉ thấy phiên bản đã phát hành: %s", r.raw)
	}
	// Ngừng dùng (không xóa — HTX US-CAT-01 AC-2).
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sops/"+tmpl+"/retire", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity,
		"validation_failed", "lý do quá ngắn")
	r = f.sopAdmin.adminPost("/v1/cropcycle/sops/"+tmpl+"/retire", map[string]any{"reason": "Thay bằng quy trình SRP mới"})
	expect(t, r, http.StatusOK, "ngừng dùng")
	if str(r.body, "status") != "retired" {
		t.Fatalf("trạng thái: %s", r.raw)
	}
}

// HTX US-CAT-03: nhân bản giữ nguyên bước và thứ tự; sửa bản sao không đổi nguồn;
// tên bản sao trùng → "Tên SOP này đã tồn tại".
func TestSOPClone_HTX_US_CAT_03(t *testing.T) {
	f := newFixture(t)
	src, srcVer := f.publishedSOP("SOP chuẩn OM5451", "")
	expectCode(t, f.sopAdmin.adminPost("/v1/cropcycle/sops/"+src+"/clone", map[string]any{"name": "SOP chuẩn OM5451"}),
		http.StatusConflict, "cropcycle.sop_name_taken", "tên bản sao trùng nguồn")
	r := f.sopAdmin.adminPost("/v1/cropcycle/sops/"+src+"/clone", map[string]any{"name": "SOP ST25 (bản sao)"})
	expect(t, r, http.StatusCreated, "nhân bản")
	clone := list(r.body, "versions")[0]
	if str(r.body, "source_template_ref") != src || str(clone, "source_version_ref") != srcVer || str(r.body, "status") != "draft" {
		t.Fatalf("nguồn của bản sao: %s", r.raw)
	}
	cs := list(clone, "steps")
	if len(cs) != 5 || str(cs[0], "name") != "Làm đất" || str(cs[4], "activity_code") != "thu_hoach" {
		t.Fatalf("bản sao phải giữ bước và thứ tự: %v", cs)
	}
	edited := steps()
	edited[0]["name"] = "Cày ải"
	expect(t, f.sopAdmin.adminPost("/v1/cropcycle/sop-versions/"+str(clone, "id")+"/steps", map[string]any{"steps": edited}), http.StatusOK, "sửa bản sao")
	r = f.sopAdmin.adminGet("/v1/cropcycle/sop-versions/" + srcVer)
	expect(t, r, http.StatusOK, "đọc nguồn")
	if str(list(get(r.body, "version").(map[string]any), "steps")[0], "name") != "Làm đất" {
		t.Fatalf("sửa bản sao làm đổi nguồn: %s", r.raw)
	}
}
