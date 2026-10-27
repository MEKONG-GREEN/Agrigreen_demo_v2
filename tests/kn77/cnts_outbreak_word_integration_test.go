package kn77_test

// BF-CN-OUTBREAK (PRD UP-08): "Xuất Word theo mẫu" Báo cáo tình hình dịch bệnh BM-10 qua pf/export THẬT (#17) trên
// PostgreSQL: số văn bản "{n}/BC-TTKN" tự cấp — một số mỗi ổ dịch, dùng lại khi xuất lại, KHÔNG trùng khi nhiều ổ dịch
// xuất đồng thời; dòng "<tỉnh>, ngày …"; nội dung đúng số liệu; lịch sử, tải lại (kiểm băm); phạm vi xã.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type obwUnits struct{}

func (obwUnits) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	switch code {
	case "30001", "30002":
		return partyport.AdminUnit{Code: code, Name: "Xã Hòa Thuận " + code, Level: partyport.LevelCommune, ParentCode: "89"}, nil
	case "89":
		return partyport.AdminUnit{Code: code, Name: "Tỉnh An Giang", Level: partyport.LevelProvince}, nil
	}
	return partyport.AdminUnit{}, partyport.ErrUnitNotFound
}
func (obwUnits) Descendants(context.Context, string, time.Time) ([]string, error) { return nil, nil }
func (obwUnits) Current(context.Context, string, time.Time) ([]string, error)     { return nil, nil }

type obwCatalogs struct{ extport.CNTSCatalogs }

var obwItems = map[string]mdport.CatalogItem{
	"cnts_disease:dtlcp": {Catalog: "cnts_disease", Code: "dtlcp", Name: "Dịch tả lợn châu Phi", Active: true, Attrs: map[string]string{"species": "heo"}},
	"cnts_species:heo":   {Catalog: "cnts_species", Code: "heo", Name: "Heo", Active: true},
}

func (obwCatalogs) ItemAt(_ context.Context, catalog, code string, _ time.Time) (mdport.CatalogItem, error) {
	if it, ok := obwItems[catalog+":"+code]; ok {
		return it, nil
	}
	return mdport.CatalogItem{}, fmt.Errorf("giả: %w", mdport.ErrCatalogItemNotFound)
}

func (obwCatalogs) ListAt(_ context.Context, catalog string, _ time.Time) ([]mdport.CatalogItem, error) {
	var out []mdport.CatalogItem
	for _, it := range obwItems {
		if it.Catalog == catalog {
			out = append(out, it)
		}
	}
	return out, nil
}

type obwFix struct {
	t      *testing.T
	iss    *authztest.FakeIssuer
	srv    *httptest.Server
	tenant uuid.UUID
	mu     sync.Mutex
	ctxs   map[uuid.UUID]*authz.KNContext
}

func newObwFix(t *testing.T) *obwFix {
	env := pgtest.Get(t)
	f := &obwFix{t: t, iss: authztest.New(t), tenant: ids.New(), ctxs: map[uuid.UUID]*authz.KNContext{}}
	clk := clock.NewFixed(time.Date(2027, 10, 13, 3, 0, 0, 0, time.UTC))
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := extension.New(pool, clk, extension.Deps{Units: obwUnits{}})
	mod.UseCNTSCatalogs(obwCatalogs{})
	ex, err := export.New(pool, clk, export.Deps{Access: exportport.AccessFunc(func(context.Context) bool { return false }),
		Templates: []exportport.TemplateRegistration{extension.ExportTemplates()}})
	if err != nil {
		t.Fatal(err)
	}
	mod.UseExporter(ex.Exporter())
	registry, err := authz.NewSessionRegistry(extension.SessionProjection())
	if err != nil {
		t.Fatal(err)
	}
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 4<<20)
	ops := env.Pool(t, migrations.PF, "platform_api")
	mw := func(c *gin.Context) {
		p, ok := authz.FromContext(c.Request.Context())
		id, err := uuid.Parse(c.GetHeader(authz.ContextHeader))
		f.mu.Lock()
		kc := f.ctxs[id]
		f.mu.Unlock()
		if !ok || err != nil || kc == nil {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		cp := *kc
		p.KN = &cp
		selected, err := registry.ResolveSelected(c.Request.Context(), p)
		if err != nil {
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		p.SelectedSession = &selected
		c.Request = c.Request.WithContext(authz.WithPrincipal(c.Request.Context(), p))
		c.Next()
	}
	mod.RegisterApp(eng.Group("/v1", httpx.Authenticate(f.iss.Verifier()), mw, idempotency.Middleware(ops)))
	f.srv = httptest.NewServer(eng)
	t.Cleanup(f.srv.Close)
	return f
}

type obwUser struct {
	f     *obwFix
	token string
	ctx   uuid.UUID
}

func (f *obwFix) user(kc authz.KNContext) obwUser {
	tok := f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: f.tenant, Roles: []string{"kn_user"}, MFA: true, TTL: 2 * time.Hour})
	kc.ID, kc.OrgRef = ids.New(), ids.New()
	f.mu.Lock()
	f.ctxs[kc.ID] = &kc
	f.mu.Unlock()
	return obwUser{f: f, token: tok, ctx: kc.ID}
}

func (u obwUser) do(method, path, idem string, body any) (int, []byte) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	r.Header.Set("Authorization", "Bearer "+u.token)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(authz.ContextHeader, u.ctx.String())
	if idem != "" {
		r.Header.Set("Idempotency-Key", idem)
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		u.f.t.Error(err)
		return 0, nil
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	return res.StatusCode, raw
}

func (u obwUser) report(t *testing.T, commune string) string {
	t.Helper()
	code, raw := u.do(http.MethodPost, "/v1/extension/cnts/outbreaks", "k-"+ids.New().String(), map[string]any{
		"commune_code": commune, "disease_code": "dtlcp", "species_code": "heo", "received_at": "2027-10-12T16:43:00+07:00",
		"source": "ubnd_xa", "locality": "Ấp Hòa Lợi", "location": map[string]any{"lat": "10.2534100", "lon": "105.9721000", "source": "gps"},
		"herd_size": 6, "herd_age_days": 60, "dead": 1, "symptoms": "Bỏ ăn, sốt, nổi mẩn đỏ trên da", "culled_head": 6, "culled_kg": "130",
		"chemical_liters": "1", "buffer_note": "Ấp Hòa Bình 25 hộ 219 con", "prevention_note": "Đề nghị Chi cục hỗ trợ hóa chất",
	})
	if code != http.StatusCreated {
		t.Fatalf("lập ổ dịch: %d %s", code, raw)
	}
	var o struct{ Ref string }
	_ = json.Unmarshal(raw, &o)
	return o.Ref
}

type obwRun struct {
	ID            string `json:"id"`
	DocNumber     string `json:"doc_number"`
	DocNumberAuto bool   `json:"doc_number_auto"`
	Template      string `json:"template"`
	FileName      string `json:"file_name"`
	BodyHash      string `json:"body_hash"`
}

func TestBFCNOutbreak_WordBM10Numbering(t *testing.T) {
	f := newObwFix(t)
	xa1 := f.user(authz.KNContext{Role: authz.KNXa, Department: "chan_nuoi_thuy_san", Communes: []string{"30001"}, Scope: authz.Scope{Units: []string{"30001"}}})
	xa2 := f.user(authz.KNContext{Role: authz.KNXa, Department: "chan_nuoi_thuy_san", Communes: []string{"30002"}, Scope: authz.Scope{Units: []string{"30002"}}})
	tinh := f.user(authz.KNContext{Role: authz.KNTinh, Department: "chan_nuoi_thuy_san", Scope: authz.Scope{Units: []string{"89", "30001", "30002"}}})

	ref := xa1.report(t, "30001")
	word := func(u obwUser, ref, key string) (int, obwRun, []byte) {
		code, raw := u.do(http.MethodPost, "/v1/extension/cnts/outbreaks/"+ref+"/word", key, map[string]any{})
		var r obwRun
		_ = json.Unmarshal(raw, &r)
		return code, r, raw
	}
	if code, _, raw := word(xa2, ref, "k-"+ids.New().String()); code != http.StatusNotFound {
		t.Fatalf("xã khác không xuất được: %d %s", code, raw)
	}
	if code, _, _ := word(xa1, ref, ""); code < 400 {
		t.Fatalf("thiếu Idempotency-Key phải lỗi: %d", code)
	}
	code, w1, raw := word(xa1, ref, "k-"+ids.New().String())
	if code != http.StatusCreated || w1.DocNumber != "1/BC-TTKN" || !w1.DocNumberAuto || w1.Template != "cnts.bm10" || !strings.HasSuffix(w1.FileName, "-v1.docx") {
		t.Fatalf("Word đầu: %d %s", code, raw)
	}
	// Xuất lại (Cấp Tỉnh) dùng lại số của ổ dịch.
	if _, w2, raw := word(tinh, ref, "k-"+ids.New().String()); w2.DocNumber != "1/BC-TTKN" {
		t.Fatalf("xuất lại giữ số: %s", raw)
	}
	// Nội dung .docx đúng mẫu và số liệu.
	code, body := xa1.do(http.MethodGet, "/v1/extension/cnts/outbreaks/"+ref+"/words/"+w1.ID+"/download", "", nil)
	if code != http.StatusOK {
		t.Fatalf("tải lại: %d %s", code, body)
	}
	content, err := docx.Extract(body)
	if err != nil {
		t.Fatal(err)
	}
	text := content.Text()
	for _, s := range []string{"Số: 1/BC-TTKN", "An Giang, ngày", "Tình hình bệnh Dịch tả lợn châu Phi", "Xã Hòa Thuận 30001", "16:43 ngày 12/10/2027",
		"Quy mô đàn heo tại thời điểm phát hiện: 6 con (khoảng 60 ngày tuổi)", "Số con chết: 1 con", "6 con, tổng trọng lượng 130 kg",
		"Ấp Hòa Bình 25 hộ 219 con", "Đề nghị Chi cục hỗ trợ hóa chất", "Chờ xác nhận"} {
		if !strings.Contains(text, s) {
			t.Fatalf("Word thiếu %q:\n%s", s, text)
		}
	}
	code, raw = xa1.do(http.MethodGet, "/v1/extension/cnts/outbreaks/"+ref+"/words", "", nil)
	var hist struct{ Items []obwRun }
	_ = json.Unmarshal(raw, &hist)
	if code != http.StatusOK || len(hist.Items) != 2 {
		t.Fatalf("lịch sử 2 lần xuất: %d %s", code, raw)
	}

	// Đồng thời: 8 ổ dịch xuất cùng lúc → 8 số khác nhau, liên tiếp 2..9 (không trùng, không hở).
	refs := make([]string, 8)
	for i := range refs {
		refs[i] = xa1.report(t, "30001")
	}
	nums := make([]string, len(refs))
	var wg sync.WaitGroup
	for i, r := range refs {
		wg.Add(1)
		go func(i int, r string) {
			defer wg.Done()
			code, w, raw := word(xa1, r, "k-"+ids.New().String())
			if code != http.StatusCreated {
				t.Errorf("xuất đồng thời %d: %d %s", i, code, raw)
			}
			nums[i] = w.DocNumber
		}(i, r)
	}
	wg.Wait()
	seen := map[string]bool{}
	for _, n := range nums {
		if n == "" || seen[n] {
			t.Fatalf("số văn bản trùng hoặc rỗng khi xuất đồng thời: %v", nums)
		}
		seen[n] = true
	}
	for i := 2; i <= 9; i++ {
		if !seen[fmt.Sprintf("%d/BC-TTKN", i)] {
			t.Fatalf("số liên tiếp 2..9: %v", nums)
		}
	}
}
