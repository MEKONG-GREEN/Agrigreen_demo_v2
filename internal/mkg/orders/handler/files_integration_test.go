package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5"

	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// pdfBytes là một tệp PDF tối thiểu (nhận diện theo nội dung "%PDF-").
func pdfBytes(tag string) []byte { return []byte("%PDF-1.4\n% bien ban " + tag + "\n%%EOF\n") }

// upload gửi multipart POST /v1/orders/files (file + sha256 + purpose) với Idempotency-Key cho trước.
func (c caller) upload(path, idem string, data []byte, name, sha, purpose string) resp {
	c.f.t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.SetBoundary("agrigreen-test-boundary") // thân tất định: gọi lặp cùng Idempotency-Key = cùng thân
	if sha != "" {
		_ = mw.WriteField("sha256", sha)
	}
	if purpose != "" {
		_ = mw.WriteField("purpose", purpose)
	}
	w, _ := mw.CreateFormFile("file", name)
	_, _ = w.Write(data)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, c.base+path, &buf)
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

func (c caller) download(path string) (int, []byte, http.Header) {
	c.f.t.Helper()
	req, _ := http.NewRequest(http.MethodGet, c.base+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		c.f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, b, r.Header
}

// G4 (đóng khoảng trống mã, T-2 / O-07): tệp chứng từ vùng mkg — POST /v1/orders/files lưu kho đối tượng trong
// lãnh thổ (shared/objstore, khóa theo băm), kiểm loại theo nội dung, cỡ, SHA-256 khai báo; gọi lặp → bản cũ; phiếu
// nhận (biên bản chênh lệch) và giao nhận (ePOD) nhận file_ref đã tải, sai mục đích → 422; tải về đúng byte + băm;
// tenant khác 404 (RB-2); siêu dữ liệu chỉ ghi thêm (RB-1); chưa cấu hình kho → 503.
func TestOrdersEvidenceFileUpload_G4(t *testing.T) {
	f := newFixture(t)
	up := f.officer
	doc := pdfBytes("chenh-lech")
	sum := objstore.SHA256Hex(doc)

	mustCode(t, up.upload("/v1/orders/files", key(), doc, "bb.pdf", sum, port.DBRole), 422, "validation_failed", "mục đích lạ")
	mustCode(t, up.upload("/v1/orders/files", key(), doc, "bb.pdf", sum, "receipt_variance_report"), 503, "orders.file_store_unavailable",
		"chưa cấu hình kho")
	st, err := objstore.NewLocal(t.TempDir(), objstore.LocalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.Files = st
	mustCode(t, up.upload("/v1/orders/files", key(), []byte("MZ\x90\x00 không phải chứng từ"), "x.exe",
		objstore.SHA256Hex([]byte("MZ\x90\x00 không phải chứng từ")), "receipt_variance_report"), 415, "orders.file_type", "loại tệp")
	mustCode(t, up.upload("/v1/orders/files", key(), doc, "bb.pdf", objstore.SHA256Hex([]byte("khác")), "receipt_variance_report"),
		422, "orders.file_checksum", "băm sai")
	mustCode(t, up.upload("/v1/orders/files", key(), doc, "bb.pdf", "", "receipt_variance_report"), 422, "orders.file_checksum", "thiếu băm")
	mustCode(t, up.upload("/v1/orders/files", key(), bytes.Repeat([]byte("%PDF-"), (15<<20)/5+1), "lon.pdf", "", "receipt_variance_report"),
		413, "orders.file_too_large", "quá 15 MB")

	k := key()
	rawName := `C:\tmp\biên bản "chênh".pdf`
	r := up.upload("/v1/orders/files", k, doc, rawName, sum, "receipt_variance_report")
	mustStatus(t, r, http.StatusCreated, "tải biên bản")
	fid := str(r.body, "file", "id")
	if str(r.body, "file", "sha256") != sum || str(r.body, "file", "content_type") != "application/pdf" ||
		str(r.body, "file", "file_name") != "biên bản chênh.pdf" || str(r.body, "file", "purpose") != "receipt_variance_report" {
		t.Fatalf("siêu dữ liệu: %s", r.raw)
	}
	if again := up.upload("/v1/orders/files", k, doc, rawName, sum, "receipt_variance_report"); again.status >= 300 ||
		str(again.body, "file", "id") != fid {
		t.Fatalf("gọi lặp cùng khóa → bản cũ: %s", again.raw)
	}
	code, body, h := f.approver.download("/v1/orders/files/" + fid + "/content")
	if code != http.StatusOK || !bytes.Equal(body, doc) || h.Get("X-Content-SHA256") != sum || h.Get("Content-Type") != "application/pdf" {
		t.Fatalf("tải về: %d %q %v", code, body, h)
	}
	mustStatus(t, f.officer.get("/v1/orders/files/"+fid), http.StatusOK, "siêu dữ liệu")
	other := f.as(ids.New(), port.RoleSalesOfficer)
	mustCode(t, other.get("/v1/orders/files/"+fid), 404, "orders.file_not_found", "RB-2 tenant khác")
	if code, _, _ := other.download("/v1/orders/files/" + fid + "/content"); code != http.StatusNotFound {
		t.Fatalf("RB-2 tải nội dung tenant khác: %d", code)
	}

	// Phiếu nhận lệch > 3 % → biên bản chênh lệch với tệp đã tải.
	htx := ids.New()
	f.contract(htx, "per_tonne", "650000", -1, 30)
	po := f.po(map[string]any{"category": "straw", "supplier_ref": htx.String(), "ordered_qty": "10000"})
	lot := ids.New()
	rc := f.officer.post("/v1/orders/purchase-orders/"+po+"/receipts", map[string]any{"lot_ref": lot.String()})
	mustStatus(t, rc, http.StatusCreated, "phiếu nhận")
	rid := str(rc.body, "receipt", "receipt_id")
	f.weigh(lot, mport.PointHubIn, "10500", nil)
	ev := up.upload("/v1/orders/files", key(), pdfBytes("epod"), "epod.pdf", objstore.SHA256Hex(pdfBytes("epod")), "delivery_evidence")
	mustStatus(t, ev, http.StatusCreated, "tải ePOD")
	epod := str(ev.body, "file", "id")
	mustCode(t, f.officer.post("/v1/orders/receipts/"+rid+"/variance-report", map[string]any{"note": "Biên bản chênh lệch: HTX giao dư 500 kg",
		"file_ref": epod}), 422, "orders.file_purpose", "tệp giao nhận gắn vào phiếu nhận")
	vr := f.officer.post("/v1/orders/receipts/"+rid+"/variance-report", map[string]any{"note": "Biên bản chênh lệch: HTX giao dư 500 kg",
		"file_ref": fid})
	mustStatus(t, vr, http.StatusCreated, "biên bản chênh lệch có tệp")
	if str(vr.body, "receipt", "report_file_ref") != fid {
		t.Fatalf("phiếu nhận giữ file_ref: %s", vr.raw)
	}

	// Giao nhận: ePOD đã tải; tệp biên bản phiếu nhận → 422.
	order := f.confirmedSO("1000000")
	dl := ids.New()
	p := f.officer.post("/v1/orders/sales-orders/"+order+"/deliveries", map[string]any{"lot_ref": dl.String(), "route": "DIRECT",
		"planned_date": f.day(1), "planned_kg": "5000"})
	mustStatus(t, p, http.StatusCreated, "lịch giao")
	did := str(p.body, "delivery", "delivery_id")
	mustStatus(t, f.officer.post("/v1/orders/deliveries/"+did+"/dispatch", map[string]any{"trip_ref": ids.New().String()}), 201, "xuất giao")
	f.weigh(dl, mport.PointCustomerIn, "5000", nil)
	mustCode(t, f.officer.post("/v1/orders/deliveries/"+did+"/accept", map[string]any{"evidence": map[string]any{"file_ref": fid, "kind": "epod"}}),
		422, "orders.file_purpose", "biên bản phiếu nhận gắn vào giao nhận")
	a := f.officer.post("/v1/orders/deliveries/"+did+"/accept", map[string]any{"evidence": map[string]any{"file_ref": epod, "kind": "epod"}})
	mustStatus(t, a, http.StatusCreated, "chấp nhận với ePOD")
	if str(a.body, "delivery", "evidence_file_ref") != epod {
		t.Fatalf("giao nhận giữ ePOD: %s", a.raw)
	}

	// RB-1: siêu dữ liệu tệp chỉ ghi thêm (trigger chặn cả owner).
	admin := f.env.AdminPool(t, migrations.MKG)
	for _, q := range []string{"UPDATE mkg_orders.file_object SET file_name = 'x' WHERE tenant_id = $1",
		"DELETE FROM mkg_orders.file_object WHERE tenant_id = $1"} {
		if _, err := admin.Exec(context.Background(), q, f.tenant); err == nil {
			t.Fatalf("file_object phải chỉ ghi thêm: %s", q)
		}
	}
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), "UPDATE mkg_orders.file_object SET file_name = 'x'")
		return err
	}); err == nil {
		t.Fatal("orders_app không được UPDATE file_object")
	}
}
