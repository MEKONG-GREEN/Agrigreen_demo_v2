package handler

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// Thân cố định như stableMultipart của web; giữ nguyên byte/biên khi mạng mất phản hồi.
func retryMultipart(t *testing.T, meta any, data string) ([]byte, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.SetBoundary("----AgFormBoundary183fixed"); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		raw, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err := mw.WriteField("meta", string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	fw, err := mw.CreateFormFile("file", "scan.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(data)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), mw.FormDataContentType()
}

func TestMultipartRetry183_HRMHTTP(t *testing.T) {
	f := newRecFixture(t, true)
	files := withDocs(t, f)
	f.call("admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	f.call("admin", http.MethodPost, "/v1/hrm/docs/folders/defaults", nil, http.StatusCreated)
	folder := folderByKey(t, f.call("hcth", http.MethodGet, "/v1/hrm/docs/folders", nil, http.StatusOK), "khac")
	staff := f.call("hcth", http.MethodPost, "/v1/hrm/staff", staffBody("vien_chuc", "Nguyễn Văn Kiểm", "0913000183", "nam", "1980-03-04", nil,
		ev("unit", "2015-03-01", map[string]any{"unit": "trong_trot"}),
		ev("position", "2015-03-01", map[string]any{"position": "vien_chuc"}),
		ev("work_start", "2015-03-01", map[string]any{}),
		ev("grade", "2015-03-01", map[string]any{"grade": "01.003", "step": "2", "step_since": "2024-06-01"})), http.StatusCreated)["id"].(string)
	decision := f.call("hcth", http.MethodPost, "/v1/hrm/promotion/decisions", map[string]any{
		"kind": "nltx", "decision_no": "183/QĐ-TTKN", "decided_on": "2026-03-01", "issuer": "UBND tỉnh An Giang",
		"people": []any{map[string]any{"staff_id": staff, "to_step": 3, "effective_on": "2026-03-01"}},
	}, http.StatusCreated)["id"].(string)
	// Nối middleware thật với pool PostgreSQL thật; fixture chỉ giả danh tính/kho mã hóa.
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if p, ok := f.who[c.GetHeader("X-Test-As")]; ok {
			c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), bindTestSession(t, p)))
		}
	}, idempotency.Middleware(f.env.Pool(t, migrations.PF, "platform_api")))
	Handler{Svc: f.svc}.RegisterApp(r.Group("/v1"))
	for _, tc := range []struct {
		name, path string
		meta       any
	}{
		{"docs", "/v1/hrm/docs", map[string]any{"folder_id": folder, "title": "Thử retry", "doc_type": "khac"}},
		{"decision", "/v1/hrm/promotion/decisions/" + decision + "/file", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := len(files.files)
			body, ct := retryMultipart(t, tc.meta, "%PDF-1.7\nscan183")
			send := func(raw []byte, as string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(raw))
				req.Header.Set("Content-Type", ct)
				req.Header.Set("Idempotency-Key", "multipart-183-"+tc.name)
				req.Header.Set("X-Test-As", as)
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}
			first := send(body, "hcth") // máy chủ commit; client có thể không nhận phản hồi này
			if first.Code != http.StatusCreated {
				t.Fatalf("first=%d %s", first.Code, first.Body.String())
			}
			replay := send(body, "hcth")
			if replay.Code != first.Code || !bytes.Equal(replay.Body.Bytes(), first.Body.Bytes()) || replay.Header().Get(idempotency.ReplayedHeader) != "true" {
				t.Fatalf("replay=%d %s", replay.Code, replay.Body.String())
			}
			changed, _ := retryMultipart(t, tc.meta, "%PDF-1.7\nscan184")
			if got := send(changed, "hcth"); got.Code != http.StatusUnprocessableEntity || !bytes.Contains(got.Body.Bytes(), []byte(`"code":"idempotency_key_reused"`)) {
				t.Fatalf("changed=%d %s", got.Code, got.Body.String())
			}
			if got := send(body, "hcth2"); got.Code != http.StatusUnprocessableEntity || !bytes.Contains(got.Body.Bytes(), []byte(`"code":"idempotency_key_reused"`)) {
				t.Fatalf("other actor replay=%d", got.Code)
			}
			if len(files.files) != before+1 {
				t.Fatalf("Attach tăng khi retry/reject: trước=%d sau=%d", before, len(files.files))
			}
			list := f.call("hcth", http.MethodGet, "/v1/hrm/docs", nil, http.StatusOK)
			if list["total"] != json.Number(strconv.Itoa(before+1)) {
				t.Fatalf("doc count=%v", list["total"])
			}
		})
	}
}
