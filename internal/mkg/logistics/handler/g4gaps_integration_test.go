package handler_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
)

// upload gửi multipart POST /v1/logistics/files (file + sha256), biên cố định để gọi lặp cùng thân.
func (c caller) upload(idem string, data []byte, name, sha string) resp {
	c.f.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.SetBoundary("agrigreen-test-boundary")
	if sha != "" {
		_ = mw.WriteField("sha256", sha)
	}
	w, _ := mw.CreateFormFile("file", name)
	_, _ = w.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, c.f.srv.URL+"/v1/logistics/files", &buf)
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Idempotency-Key", idem)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&m)
	return resp{status: r.StatusCode, body: m, raw: raw}
}

// pngBytes là một ảnh PNG tối thiểu (chữ ký 8 byte) — ảnh chụp ePOD.
func pngBytes(tag string) []byte {
	return append([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"), []byte(tag)...)
}

// L-11 (proto ePOD / e-bill, G4 đóng khoảng trống): chứng từ chuyến có người ký + thời điểm ký, tệp ở kho đối tượng
// trong lãnh thổ. Tải tệp (loại theo nội dung, SHA-256 khai báo, 503 khi chưa có kho, gọi lặp → bản cũ), ghi chứng
// từ (tài xế được giao hoặc điều phối; tài xế khác 403; tệp chưa tải 404; chữ ký ở tương lai 422; chuyến hủy 409),
// băm tệp chép vào sổ, bản bù supersedes_id + lý do (RB-1), danh sách; tài xế khác không thấy (404); sổ chỉ ghi thêm;
// tenant khác 404 (RB-2).
func TestLogisticsL11TripDocumentsSignedEPOD(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	drv, other := f.driver(), f.driver()
	trip := f.createTripFor(d, "road", ids.New(), "12000", drv.actor)
	img := pngBytes("epod-ky-nhan")
	sum := objstore.SHA256Hex(img)

	mustCode(t, drv.upload(key(), img, "epod.png", sum), 503, "logistics.file_store_unavailable", "chưa cấu hình kho")
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.Files = st
	mustCode(t, drv.upload(key(), []byte("plain text"), "a.txt", objstore.SHA256Hex([]byte("plain text"))), 415, "logistics.file_type", "loại")
	mustCode(t, drv.upload(key(), img, "epod.png", objstore.SHA256Hex([]byte("x"))), 422, "logistics.file_checksum", "băm sai")
	k := key()
	up := drv.upload(k, img, "epod.png", sum)
	mustStatus(t, up, http.StatusCreated, "tài xế tải ePOD")
	file := str(up.body, "file", "id")
	if str(up.body, "file", "purpose") != "trip_document" || str(up.body, "file", "content_type") != "image/png" {
		t.Fatalf("tệp: %s", up.raw)
	}
	if again := drv.upload(k, img, "epod.png", sum); again.status >= 300 || str(again.body, "file", "id") != file {
		t.Fatalf("gọi lặp: %s", again.raw)
	}

	signed := f.clk.Now().Add(-10 * time.Minute).Format(time.RFC3339)
	body := func(extra map[string]any) map[string]any {
		b := map[string]any{"kind": "epod", "file_ref": file, "signer_role": "consignee", "signer_ref": ids.New().String(), "signed_at": signed,
			"note": "Kho nhà máy ký nhận đủ 12 tấn"}
		for k, v := range extra {
			b[k] = v
		}
		return b
	}
	mustCode(t, other.post("/v1/logistics/trips/"+trip+"/documents", body(nil)), 403, "logistics.not_assigned_driver", "tài xế khác")
	mustCode(t, drv.post("/v1/logistics/trips/"+trip+"/documents", body(map[string]any{"file_ref": ids.New().String()})), 404,
		"logistics.file_not_found", "tệp chưa tải")
	mustStatus(t, drv.post("/v1/logistics/trips/"+trip+"/documents", body(map[string]any{"signed_at": f.clk.Now().Add(time.Hour).Format(time.RFC3339)})),
		http.StatusUnprocessableEntity, "ký ở tương lai")
	mustStatus(t, drv.post("/v1/logistics/trips/"+trip+"/documents", body(map[string]any{"kind": "photo"})), http.StatusUnprocessableEntity, "loại lạ")
	r := drv.post("/v1/logistics/trips/"+trip+"/documents", body(nil))
	mustStatus(t, r, http.StatusCreated, "ghi ePOD")
	doc := str(r.body, "document", "id")
	if str(r.body, "document", "file_sha256") != sum || str(r.body, "document", "signer_role") != "consignee" ||
		str(r.body, "document", "actor_ref") != drv.actor.String() || str(r.body, "document", "rule_version") != "logistics.trip_document.v1" {
		t.Fatalf("chứng từ: %s", r.raw)
	}
	// Bản bù: thiếu lý do → 422; có lý do → dòng mới trỏ dòng cũ.
	mustStatus(t, d.post("/v1/logistics/trips/"+trip+"/documents", body(map[string]any{"supersedes_id": doc})), http.StatusUnprocessableEntity,
		"bù thiếu lý do")
	mustCode(t, d.post("/v1/logistics/trips/"+trip+"/documents", body(map[string]any{"supersedes_id": ids.New().String(),
		"correction_reason": "Ghi nhầm người ký nhận"})), 404, "logistics.trip_document_not_found", "bù chứng từ lạ")
	c := d.post("/v1/logistics/trips/"+trip+"/documents", body(map[string]any{"supersedes_id": doc, "signer_role": "hub_operator",
		"correction_reason": "Ghi nhầm người ký nhận"}))
	mustStatus(t, c, http.StatusCreated, "bản bù")
	l := drv.get("/v1/logistics/trips/" + trip + "/documents")
	if items := arr(l.body, "items"); len(items) != 2 || str(items[1], "supersedes_id") != doc || str(items[0], "signer_role") != "consignee" {
		t.Fatalf("danh sách chứng từ: %s", l.raw)
	}
	mustCode(t, other.get("/v1/logistics/trips/"+trip+"/documents"), 404, "logistics.trip_not_found", "tài xế khác không thấy")
	mustCode(t, f.as(ids.New(), port.RoleDispatcher).get("/v1/logistics/files/"+file), 404, "logistics.file_not_found", "RB-2")
	mustStatus(t, d.get("/v1/logistics/files/"+file), http.StatusOK, "siêu dữ liệu")
	f.mustFail(f.pool, "RB-1 trip_document chỉ ghi thêm", "UPDATE mkg_logistics.trip_document SET note = 'x'")
	f.mustFail(f.pool, "RB-1 file_object chỉ ghi thêm", "DELETE FROM mkg_logistics.file_object")
	// Chuyến đã hủy → không ghi chứng từ.
	t2 := f.createTripFor(d, "road", ids.New(), "3000", drv.actor)
	mustStatus(t, d.post("/v1/logistics/trips/"+t2+"/cancel", map[string]any{"reason": "Khách hoãn nhận hàng"}), http.StatusCreated, "hủy")
	mustCode(t, drv.post("/v1/logistics/trips/"+t2+"/documents", body(nil)), 409, "logistics.trip_closed", "chuyến hủy")
}

// proto GH-01 (G4 đóng khoảng trống): lịch sử phiên bản danh mục phương tiện — version tăng dần, phân trang theo
// con trỏ version, chỉ điều phối, phương tiện lạ 404, tenant khác 404.
func TestLogisticsVesselVersionHistory_GH01(t *testing.T) {
	f := newFixture(t)
	d := f.dispatcher()
	r := d.post("/v1/logistics/vessels", map[string]any{"code": "CT-777", "kind": "ghe", "straw_payload_tons": "80"})
	mustStatus(t, r, http.StatusCreated, "thêm ghe")
	v := str(r.body, "vessel", "vessel_id")
	for i, tons := range []string{"85", "90"} {
		mustStatus(t, d.post("/v1/logistics/vessels/"+v+"/versions", map[string]any{"code": "CT-777", "kind": "ghe", "expected_version": i + 1,
			"straw_payload_tons": tons, "reason": "Đo lại tải thực chở sau sửa chữa"}), http.StatusCreated, "sửa ghe")
	}
	p1 := d.get("/v1/logistics/vessels/" + v + "/versions?limit=2")
	items := arr(p1.body, "items")
	if len(items) != 2 || str(items[0], "version") != "1" || str(items[0], "kind") != "created" || str(items[1], "straw_payload_tons") != "85" ||
		str(p1.body, "next_cursor") != "2" {
		t.Fatalf("trang 1: %s", p1.raw)
	}
	p2 := d.get("/v1/logistics/vessels/" + v + "/versions?limit=2&cursor=2")
	if items := arr(p2.body, "items"); len(items) != 1 || str(items[0], "version") != "3" || str(items[0], "reason") == "" ||
		p2.body["next_cursor"] != nil {
		t.Fatalf("trang 2: %s", p2.raw)
	}
	mustStatus(t, d.get("/v1/logistics/vessels/"+v+"/versions?cursor=x"), http.StatusUnprocessableEntity, "con trỏ sai")
	mustStatus(t, f.driver().get("/v1/logistics/vessels/"+v+"/versions"), http.StatusForbidden, "tài xế")
	mustCode(t, d.get("/v1/logistics/vessels/"+ids.New().String()+"/versions"), 404, "logistics.vessel_not_found", "lạ")
	mustCode(t, f.as(ids.New(), port.RoleDispatcher).get("/v1/logistics/vessels/"+v+"/versions"), 404, "logistics.vessel_not_found", "RB-2")
}
