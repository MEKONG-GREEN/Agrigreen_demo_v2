package handler_test

// #172 WEB-MULTIPART-IDEMKEY: màn quản trị bài viết gửi ảnh bìa bằng thân multipart TẤT ĐỊNH (apps/web-ops/src/api/
// multipart.ts) — bấm lại sau mất phản hồi gửi đúng từng byte cũ với cùng Idempotency-Key. Máy chủ phải phát lại tệp đã
// lưu (cùng id), không lưu tệp thứ hai; cùng khóa mà khác ảnh → idempotency_key_reused.

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func coverBody(t *testing.T, name string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary("----AgFormBoundary172cover"); err != nil {
		t.Fatal(err)
	}
	w, _ := mw.CreateFormFile("file", name)
	_, _ = w.Write(data)
	_ = mw.WriteField("sha256", objstore.SHA256Hex(data))
	_ = mw.Close()
	return buf.Bytes(), mw.FormDataContentType()
}

func TestAttachmentMultipartReplay(t *testing.T) {
	f := newFixture(t)
	a := f.actors()
	d := createDoc(t, a.natEditor, procedureBody("Bài có ảnh bìa (#172)"))
	path := "/v1/documents/" + d.ref + "/attachments"
	img := pngBytes(64 << 10)
	body, ct := coverBody(t, "bia.png", img)
	k := key()
	r1 := a.natEditor.raw(f.admin.URL, http.MethodPost, path, k, ct, body)
	expect(t, r1, http.StatusCreated, "")
	r2 := a.natEditor.raw(f.admin.URL, http.MethodPost, path, k, ct, body)
	expect(t, r2, http.StatusCreated, "")
	if r2.hdr.Get(idempotency.ReplayedHeader) != "true" || str(r1.body, "id") == "" || str(r2.body, "id") != str(r1.body, "id") {
		t.Fatalf("phải phát lại đúng tệp đã lưu: %v / %v", r1.body, r2.body)
	}
	other, oct := coverBody(t, "bia.png", pngBytes(64<<10+1))
	expect(t, a.natEditor.raw(f.admin.URL, http.MethodPost, path, k, oct, other), http.StatusUnprocessableEntity, "idempotency_key_reused")
	var n int
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_content.attachment WHERE document_ref = $1", uuid.MustParse(d.ref)).Scan(&n)
	}); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("phát lại không được lưu tệp thứ hai: %d", n)
	}
}
