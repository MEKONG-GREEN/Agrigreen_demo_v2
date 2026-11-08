package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func supportFixture274(t *testing.T) *fixture {
	f := newFixture(t)
	p := f.who["admin"]
	p.Roles = []string{authz.RolePlatformAdmin, client.RoleRegistrar, client.RoleEvidenceViewer}
	p.MFA = true
	f.who["admin"] = p
	created, err := f.vault.Create(authz.WithPrincipal(context.Background(), p), client.PurposeRegistryAdmin, client.Identity{Kind: client.SubjectPerson, FullName: "Kho hỗ trợ KN", AreaCode: "kn_support"})
	if err != nil {
		t.Fatal(err)
	}
	f.svc.Support = app.SupportConfig{TenantID: f.tid, SubjectRef: created.Ref, PublicKey: created.PublicKey}
	Handler{Svc: f.svc}.RegisterPublic(f.r.Group("/v1/public"))
	return f
}

func supportCall274(f *fixture, method, path, key, contentType string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var raw []byte
	if s, ok := body.(string); ok {
		raw = []byte(s)
	} else {
		raw, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Idempotency-Key", key)
	// Each case is a separate synthetic source IP; the limiter has a dedicated same-IP test below.
	req.RemoteAddr = fmt.Sprintf("192.0.2.%d:4000", len(raw)%240+1)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func TestSupport274PublicIntake(t *testing.T) {
	f := supportFixture274(t)
	body := map[string]any{"name": "Người thử 274", "contact": "274@example.test", "message": "Tài khoản chưa có phòng ban áp dụng"}
	key := ids.New().String()
	for i := 0; i < 2; i++ {
		w := supportCall274(f, http.MethodPost, "/v1/public/contacts/support-requests", key, "application/json", body)
		if w.Code != http.StatusAccepted || w.Body.String() != `{"status":"accepted"}` || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("intake: %d %s", w.Code, w.Body.String())
		}
	}
	body["message"] = "Nội dung đã đổi cho cùng khóa thao tác"
	if w := supportCall274(f, http.MethodPost, "/v1/public/contacts/support-requests", key, "application/json", body); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("changed retry: %d %s", w.Code, w.Body.String())
	}
}

func TestSupport274BoundedHostileInput(t *testing.T) {
	f := supportFixture274(t)
	valid := `{"name":"Fixture 274","contact":"274@example.test","message":"Chưa có phòng ban đang áp dụng"}`
	for _, tc := range []struct {
		name, body, ctype string
		want              int
	}{
		{"tenant", strings.TrimSuffix(valid, "}") + `,"tenant_id":"00000000-0000-0000-0000-000000000001"}`, "application/json", 422},
		{"actor", strings.TrimSuffix(valid, "}") + `,"actor_ref":"00000000-0000-0000-0000-000000000001"}`, "application/json", 422},
		{"role", strings.TrimSuffix(valid, "}") + `,"role":"admin"}`, "application/json", 422},
		{"trailing", valid + ` {}`, "application/json", 422},
		{"oversize", strings.Repeat(" ", supportBodyLimit+1), "application/json", 413},
		{"file", valid, "multipart/form-data; boundary=test", 415},
		{"contact", `{"name":"Fixture","contact":"<img src=x>","message":"Không nhập được báo giá"}`, "application/json", 422},
		{"encoded", `{"name":"` + strings.Repeat(">", 100) + `","contact":"274@example.test","message":"` + strings.Repeat("<", 2000) + `"}`, "application/json", 422},
		{"long", `{"name":"Fixture","contact":"274@example.test","message":"` + strings.Repeat("a", 2001) + `"}`, "application/json", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := supportCall274(f, http.MethodPost, "/v1/public/contacts/support-requests", ids.New().String(), tc.ctype, tc.body)
			if w.Code != tc.want {
				t.Fatalf("%s: %d %s", tc.name, w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "274@example.test") {
				t.Fatal("PII echoed in validation")
			}
		})
	}
}

func TestSupport274IPLimit(t *testing.T) {
	f := supportFixture274(t)
	for i := 0; i < 6; i++ {
		req := httptest.NewRequest(http.MethodPost, "/v1/public/contacts/support-requests", strings.NewReader(`{}`))
		req.RemoteAddr = "198.51.100.1:4000"
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", ids.New().String())
		w := httptest.NewRecorder()
		f.r.ServeHTTP(w, req)
		if i == 5 && w.Code != http.StatusTooManyRequests {
			t.Fatalf("limit: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestSupport274UnknownLengthBodyBound(t *testing.T) {
	f := supportFixture274(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/public/contacts/support-requests", strings.NewReader(strings.Repeat(" ", supportBodyLimit+1)))
	req.ContentLength = -1
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", ids.New().String())
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("unknown length: %d %s", w.Code, w.Body.String())
	}
}
