package assembly

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// W11.23: Contact nối hồ sơ nhân sự thật (pf/hrm) qua gốc lắp ráp — AC-02 (sửa SĐT ở hồ sơ → Contact thấy số mới), AC-03
// (lọc cấp Xã), AC-04 (năm sinh 1990), AC-06 (thêm hồ sơ cán bộ xã → có trong Contact với 3 trường), cổng AG_HRM_RECORDS.
func TestContactOverHRM_W1123(t *testing.T) {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	clk := clock.NewFixed(time.Date(2026, 10, 13, 3, 0, 0, 0, time.UTC))
	vault := clienttest.New()
	tid := ids.New()
	who := map[string]authz.Principal{
		"admin": {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: authz.KNAdmin, Scope: authz.Scope{All: true}}},
		"hcth":  {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: authz.KNTinh, Department: "hanh_chinh_tong_hop"}},
		"tw":    {ActorRef: ids.New(), TenantID: tid, KN: &authz.KNContext{Role: authz.KNTW}},
	}
	router := func(enabled bool) *gin.Engine {
		hr, err := hrm.New(pool, clk, KNHRMAccess)
		if err != nil {
			t.Fatal(err)
		}
		hr.UseRecords(vault, nil, enabled)
		ct, err := contact.New(pool, clk, KNContactAccess, vault)
		if err != nil {
			t.Fatal(err)
		}
		ct.UseStaff(KNContactStaff{Dir: hr.Directory()}, nil)
		gin.SetMode(gin.TestMode)
		r := gin.New()
		r.Use(func(c *gin.Context) {
			if p, ok := who[c.GetHeader("X-Test-As")]; ok {
				k := *p.KN
				k.ID = ids.New() // synthetic fixture ID; production gets the server-resolved ID
				p.KN = &k
				registry, err := authz.NewSessionRegistry(extension.SessionProjection())
				if err != nil {
					t.Fatal(err)
				}
				selected, err := registry.ResolveSelected(context.Background(), p)
				if err != nil {
					t.Fatal(err)
				}
				p.SelectedSession = &selected
				c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
			}
		})
		g := r.Group("/v1")
		hr.RegisterApp(g)
		ct.RegisterApp(g)
		return r
	}
	call := func(r *gin.Engine, as, method, path string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		if body == nil {
			raw = nil
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("X-Test-As", as)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != want {
			t.Fatalf("%s %s %s: %d (muốn %d) %s", as, method, path, w.Code, want, w.Body.String())
		}
		var m map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &m)
		return m
	}
	on := router(true)
	call(on, "admin", http.MethodPost, "/v1/hrm/defaults", nil, http.StatusCreated)
	ev := func(kind, from string, p map[string]any) map[string]any {
		return map[string]any{"kind": kind, "valid_from": from, "payload": p}
	}
	xa := call(on, "hcth", http.MethodPost, "/v1/hrm/staff", map[string]any{"type": "can_bo_xa",
		"profile": map[string]any{"full_name": "Nguyễn Văn Xã", "phone": "0912000333", "address": "Ấp 1", "birth_date": "1990-03-15",
			"gender": "nam", "email": "xa@ttkn.vn"},
		"events": []any{ev("work_start", "2022-01-01", map[string]any{}),
			ev("unit", "2022-01-01", map[string]any{"commune": "30292", "mgmt_unit": "trong_trot"}),
			ev("position", "2022-01-01", map[string]any{"position": "vien_chuc"})}}, http.StatusCreated)

	// AC-06, AC-04: Cấp TW thấy đúng 3 trường; năm sinh 1990.
	l := call(on, "tw", http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK)
	items := l["items"].([]any)
	if l["total"] != float64(1) || len(items) != 1 {
		t.Fatalf("Contact sau khi thêm hồ sơ: %v", l)
	}
	row := items[0].(map[string]any)
	if row["full_name"] != "Nguyễn Văn Xã" || row["birth_year"] != "1990" || row["phone"] != "0912000333" || len(row) != 4 {
		t.Fatalf("dòng Cấp TW: %v", row)
	}
	// AC-03: Cấp Tỉnh lọc cấp Xã; chức danh = tên chức vụ danh mục; sửa ở Contact bị chặn.
	x := call(on, "hcth", http.MethodGet, "/v1/contacts/persons?level=xa", nil, http.StatusOK)["items"].([]any)
	if len(x) != 1 || x[0].(map[string]any)["level"] != "xa" || x[0].(map[string]any)["email"] != "xa@ttkn.vn" {
		t.Fatalf("lọc cấp Xã: %v", x)
	}
	call(on, "hcth", http.MethodPost, "/v1/contacts/persons/"+row["id"].(string)+"/revise", map[string]any{"title": "X"}, http.StatusUnprocessableEntity)
	// AC-02: sửa SĐT ở hồ sơ FN-07 → Contact hiện số mới.
	call(on, "hcth", http.MethodPost, "/v1/hrm/staff/"+xa["id"].(string)+"/profile", map[string]any{"phone": "0912999888"}, http.StatusOK)
	row = call(on, "tw", http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK)["items"].([]any)[0].(map[string]any)
	if row["phone"] != "0912999888" {
		t.Fatalf("AC-02: %v", row)
	}
	// Cổng AG_HRM_RECORDS tắt: không còn cán bộ thuộc tỉnh; liên hệ ngoài tỉnh vẫn thêm được.
	off := router(false)
	if l := call(off, "tw", http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK); l["total"] != float64(0) || l["staff_source"] != "disabled" {
		t.Fatalf("cổng tắt: %v", l)
	}
	call(off, "hcth", http.MethodPost, "/v1/contacts/persons", map[string]any{"full_name": "Liên Hệ TW", "phone": "0243000111", "level": "tw"}, http.StatusCreated)
	if l := call(off, "tw", http.MethodGet, "/v1/contacts/persons", nil, http.StatusOK); l["total"] != float64(1) {
		t.Fatalf("cổng tắt, liên hệ ngoài tỉnh: %v", l)
	}
}
