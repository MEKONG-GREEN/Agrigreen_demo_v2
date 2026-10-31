package handler_test

// #172 WEB-MULTIPART-IDEMKEY: web gửi thân multipart TẤT ĐỊNH (apps/web-ops/src/api/multipart.ts) — bấm lại sau mất phản hồi
// gửi đúng từng byte cũ với cùng Idempotency-Key. Máy chủ phải phát lại kết quả đã ghi (thêm văn bản, tải tệp mới), không
// ghi văn bản/phiên bản thứ hai; cùng khóa mà khác tệp → idempotency_key_reused.

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// fixedMultipart dựng thân multipart với boundary cố định (như web) để gửi lại đúng từng byte.
func fixedMultipart(t *testing.T, fields [][2]string, name string, data []byte) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.SetBoundary("----AgFormBoundary172fixed"); err != nil {
		t.Fatal(err)
	}
	for _, f := range fields {
		_ = w.WriteField(f[0], f[1])
	}
	fw, _ := w.CreateFormFile("file", name)
	_, _ = fw.Write(data)
	_ = w.Close()
	return buf.Bytes(), w.FormDataContentType()
}

func (u user) sendRaw(path, k, ctype string, body []byte) resp {
	return u.send(http.MethodPost, path, k, ctype, bytes.NewReader(body))
}

func (f *fix) docRows(title string) int {
	f.t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(DISTINCT doc_ref) FROM pf_extension.dept_doc WHERE tenant_id = $1 AND title = $2`,
		f.tenant, title).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestFN20_DeptLibraryMultipartReplay(t *testing.T) {
	f := newFix(t)
	vc := f.user(tinh(cnts, false))

	// Thêm văn bản: lần đầu đã ghi (phản hồi mất) → gửi lại đúng thân + cùng khóa → phát lại, chỉ một văn bản.
	const title = "Quy trình nuôi tôm thẻ (#172)"
	meta, _ := json.Marshal(map[string]any{"title": title, "kind": "quy_trinh_ky_thuat"})
	body, ct := fixedMultipart(t, [][2]string{{"meta", string(meta)}}, "qt.pdf", pdf("tom the"))
	k := key()
	first := vc.sendRaw("/v1/extension/dept-docs", k, ct, body)
	d1 := want(t, first, 201, "", "thêm văn bản")
	again := vc.sendRaw("/v1/extension/dept-docs", k, ct, body)
	d2 := want(t, again, 201, "", "bấm lại sau mất phản hồi")
	if again.header.Get(idempotency.ReplayedHeader) != "true" || str(d2, "ref") != str(d1, "ref") || !bytes.Equal(again.raw, first.raw) {
		t.Fatalf("phải phát lại đúng phản hồi lần đầu: %s / %s", first.raw, again.raw)
	}
	if n := f.docRows(title); n != 1 {
		t.Fatalf("phát lại không được tạo văn bản thứ hai: %d", n)
	}
	other, oct := fixedMultipart(t, [][2]string{{"meta", string(meta)}}, "qt.pdf", pdf("tom the khac"))
	want(t, vc.sendRaw("/v1/extension/dept-docs", k, oct, other), 422, "idempotency_key_reused", "cùng khóa, khác tệp")
	if n := f.docRows(title); n != 1 {
		t.Fatalf("khóa dùng lại không được ghi: %d", n)
	}

	// Tải tệp mới: phát lại không thêm phiên bản tệp.
	ref := str(d1, "ref")
	fb, fct := fixedMultipart(t, [][2]string{{"base_version", "1"}}, "qt-v2.pdf", pdf("ban 2"))
	fk := key()
	r1 := want(t, vc.sendRaw("/v1/extension/dept-docs/"+ref+"/file", fk, fct, fb), 200, "", "tải tệp mới")
	r2resp := vc.sendRaw("/v1/extension/dept-docs/"+ref+"/file", fk, fct, fb)
	r2 := want(t, r2resp, 200, "", "bấm lại tải tệp mới")
	if r2resp.header.Get(idempotency.ReplayedHeader) != "true" || num(r2, "version") != num(r1, "version") || num(obj(r2, "file"), "version") != "2" {
		t.Fatalf("phát lại tải tệp: %v / %v", r1, r2)
	}
	cur := want(t, vc.get("/v1/extension/dept-docs/"+ref), 200, "", "sau phát lại")
	if num(cur, "version") != num(r1, "version") || len(list(cur, "versions")) != 2 || num(obj(cur, "file"), "version") != "2" {
		t.Fatalf("phát lại không được ghi phiên bản thứ ba: %v", cur)
	}
}
