package public_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/public"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

const collPath = "/v1/data/collection-volumes"

func wantCode(t *testing.T, r resp, status int, code, what string) {
	t.Helper()
	if r.status != status || r.code() != code {
		t.Fatalf("%s: muốn %d %s, nhận %d %s", what, status, code, r.status, r.raw)
	}
}

// ADR-0022 mục 4–7 (E2E-G5-04): đọc thành công — hợp đồng, header giới hạn, meta k, một dòng sổ có
// bên cấp + số dòng; tenant bên gọi lấy từ khóa.
func TestReadCollectionVolumes_ADR0022(t *testing.T) {
	e := newEnv(t)
	r := e.do(http.MethodGet, collPath+"?season_code=2026-HT", e.key, nil)
	if r.status != http.StatusOK {
		t.Fatalf("muốn 200, nhận %d %s", r.status, r.raw)
	}
	for h, want := range map[string]string{port.HeaderDataContract: "collection_volume.v1", port.HeaderRateLimitLimit: "600",
		port.HeaderQuotaRemaining: "999"} {
		if got := r.header.Get(h); got != want {
			t.Fatalf("header %s = %q, muốn %q", h, got, want)
		}
	}
	if r.header.Get(port.HeaderRateLimitRemaining) == "" || r.header.Get(port.HeaderRateLimitReset) == "" || r.header.Get(port.HeaderRequestID) == "" {
		t.Fatal("thiếu RateLimit-Remaining / RateLimit-Reset / AG-Request-Id")
	}
	meta, _ := r.body["meta"].(map[string]any)
	if meta["k_applied"] != float64(10) || meta["suppressed_cells"] != float64(3) {
		t.Fatalf("meta sai: %v", meta)
	}
	if items, _ := r.body["items"].([]any); len(items) != 2 {
		t.Fatalf("muốn 2 ô, nhận %v", r.body["items"])
	}
	if e.coll.last.DataTenant != e.grantor || e.coll.last.K != 10 || e.coll.last.SeasonCode != "2026-HT" {
		t.Fatalf("nguồn phải đọc tenant bên cấp với k mặc định: %+v", e.coll.last)
	}
	calls, units := e.meter.snapshot()
	if len(calls) != 1 || units != 1 {
		t.Fatalf("muốn đúng 1 dòng sổ 1 đơn vị, nhận %d dòng %d đơn vị", len(calls), units)
	}
	c := calls[0]
	if c.Kind != meteringport.CallRead || c.Status != 200 || c.DataTenantID != e.grantor || c.Rows != 2 || c.Scope != "dp:collection_volume:read" ||
		c.RouteTemplate != "GET /v1/data/collection-volumes" || c.ClientTenantID != e.thirdParty || c.ContractVersion != 1 || c.Bytes == 0 {
		t.Fatalf("dòng sổ sai: %+v", c)
	}
	// P-07: tham số query / header không đổi được tenant bên gọi.
	_ = e.do(http.MethodGet, collPath+"?tenant_id="+ids.New().String(), e.key, nil, "X-Tenant-Id", ids.New().String())
	for _, g := range e.grants.grantees {
		if g != e.thirdParty {
			t.Fatalf("GrantLookup phải nhận tenant bên gọi từ khóa, nhận %s", g)
		}
	}
}

// P-19, E2E-G5-03: khóa thiếu / sai → 401, KHÔNG ghi sổ, log chỉ có prefix.
func TestUnauthenticatedNoLedger_ADR0022(t *testing.T) {
	e := newEnv(t)
	wantCode(t, e.do(http.MethodGet, collPath, "", nil), 401, port.ErrUnauthenticated.Code, "thiếu khóa")
	bad := "agk_zzzzzzzzzzzz_" + strings.Repeat("B", 43)
	r := e.do(http.MethodGet, collPath, bad, nil)
	wantCode(t, r, 401, port.ErrUnauthenticated.Code, "khóa sai")
	if r.header.Get("WWW-Authenticate") == "" {
		t.Fatal("401 phải có WWW-Authenticate")
	}
	if calls, _ := e.meter.snapshot(); len(calls) != 0 {
		t.Fatalf("lời gọi chưa xác thực không ghi sổ, nhận %d", len(calls))
	}
	if strings.Contains(e.logs.String(), strings.Repeat("B", 43)) || !strings.Contains(e.logs.String(), "zzzzzzzzzzzz") {
		t.Fatal("log chỉ có key_prefix, không có bí mật")
	}
}

// ADR-0022 mục 6 (P-05, P-06): thiếu scope, thiếu cấp quyền, thu hồi cấp quyền → 403 + một dòng sổ.
func TestScopeAndGrantDenied_ADR0022(t *testing.T) {
	e := newEnv(t)
	// Thiếu scope dp:hub_capacity:read (cấp quyền có).
	wantCode(t, e.do(http.MethodGet, "/v1/data/hub-capacities", e.key, nil), 403, port.ErrForbidden.Code, "thiếu scope")
	// Có scope collection nhưng bên cấp khác (grantor không có cấp quyền).
	wantCode(t, e.do(http.MethodGet, collPath+"?grantor="+ids.New().String(), e.key, nil), 403, port.ErrForbidden.Code, "grantor không cấp quyền")
	// Thu hồi cấp quyền → 403 ngay ở lời gọi kế tiếp.
	if r := e.do(http.MethodGet, collPath, e.key, nil); r.status != 200 {
		t.Fatalf("trước thu hồi muốn 200, nhận %d", r.status)
	}
	e.grants.revoke(e.thirdParty, partyport.KindDPCollectionVolume)
	r := e.do(http.MethodGet, collPath, e.key, nil)
	wantCode(t, r, 403, port.ErrForbidden.Code, "sau thu hồi cấp quyền")
	if r.header.Get(port.HeaderDataContract) != "" || strings.Contains(string(r.raw), "items") {
		t.Fatal("403 không có dữ liệu")
	}
	calls, units := e.meter.snapshot()
	if len(calls) != 4 || units != 1 {
		t.Fatalf("muốn 4 dòng sổ (1 đơn vị), nhận %d dòng %d đơn vị", len(calls), units)
	}
	for _, i := range []int{0, 1, 3} {
		if calls[i].Status != 403 || calls[i].ProblemCode != port.ErrForbidden.Code || calls[i].DataTenantID.String() != "00000000-0000-0000-0000-000000000000" {
			t.Fatalf("dòng sổ 403 sai: %+v", calls[i])
		}
	}
	// M-05: usage:read — có scope → 200 (của chính khách hàng); thiếu → 403.
	if r := e.do(http.MethodGet, "/v1/usage?from=2026-10-01&to=2026-10-01", e.key, nil); r.status != 200 {
		t.Fatalf("usage có scope muốn 200, nhận %d %s", r.status, r.raw)
	}
	e2 := newEnv(t, withScopes("dp:collection_volume:read"))
	wantCode(t, e2.do(http.MethodGet, "/v1/usage?from=2026-10-01&to=2026-10-01", e2.key, nil), 403, port.ErrForbidden.Code, "thiếu usage:read")
	wantCode(t, e2.do(http.MethodPost, collPath+"/exports", e2.key, []byte(`{"format":"csv"}`), "Idempotency-Key", "k-12345678"),
		403, port.ErrForbidden.Code, "thiếu scope export")
}

func withScopes(scopes ...string) opt {
	return func(d *public.Deps) {
		fa := d.Auth.(fakeAuth)
		for k, c := range fa.callers {
			c.Scopes = scopes
			fa.callers[k] = c
		}
	}
}

// ADR-0022 mục 9 (P-18): hợp đồng draft không phục vụ ở production.
func TestDraftNotServedInProduction_ADR0022(t *testing.T) {
	p, err := dataproduct.Get(port.ProductCollectionVolume)
	if err != nil || p.Status != dataproduct.StatusDraft {
		t.Skip("hợp đồng collection_volume không còn là draft")
	}
	e := newEnv(t, func(d *public.Deps) { d.Production = true })
	wantCode(t, e.do(http.MethodGet, collPath, e.key, nil), 403, port.ErrForbidden.Code, "draft ở production")
	r := e.do(http.MethodGet, "/v1/data-products", e.key, nil)
	if items, _ := r.body["items"].([]any); r.status != 200 || len(items) != 0 {
		t.Fatalf("danh mục production không có sản phẩm draft: %d %s", r.status, r.raw)
	}
	// Ngoài production: danh mục có collection_volume kèm bên cấp.
	e2 := newEnv(t)
	r = e2.do(http.MethodGet, "/v1/data-products", e2.key, nil)
	items, _ := r.body["items"].([]any)
	if len(items) != 1 || !strings.Contains(string(r.raw), e2.grantor.String()) {
		t.Fatalf("danh mục dev: %s", r.raw)
	}
}

// ADR-0023 mục 1 (M-02): ghi sổ lỗi → 503 metering.unavailable, KHÔNG có byte dữ liệu.
func TestLedgerFailureNoBody_ADR0023(t *testing.T) {
	e := newEnv(t)
	e.meter.err = errBoom
	r := e.do(http.MethodGet, collPath, e.key, nil)
	wantCode(t, r, 503, meteringport.ErrUnavailable.Code, "ghi sổ lỗi")
	if strings.Contains(string(r.raw), "30001") || strings.Contains(string(r.raw), "items") || r.header.Get(port.HeaderDataContract) != "" {
		t.Fatalf("503 không được có dữ liệu: %s", r.raw)
	}
	if strings.Contains(string(r.raw), errBoom.Error()) {
		t.Fatal("lỗi hạ tầng không lộ ra ngoài")
	}
}

// ADR-0022 mục 7 (P-10, E2E-G5-06): hết hạn mức → 429 metering.quota_exceeded + Retry-After; đơn vị
// đếm đúng (2 lời đọc = 2 đơn vị, lời bị chặn = 0 đơn vị, vẫn một dòng sổ).
func TestQuotaExhausted429_ADR0022(t *testing.T) {
	e := newEnv(t)
	e.meter.monthly = 2
	for i := 0; i < 2; i++ {
		if r := e.do(http.MethodGet, collPath, e.key, nil); r.status != 200 {
			t.Fatalf("lời %d muốn 200, nhận %d", i, r.status)
		}
	}
	r := e.do(http.MethodGet, collPath, e.key, nil)
	wantCode(t, r, 429, meteringport.ErrQuotaExceeded.Code, "hết hạn mức (kiểm sớm)")
	if r.header.Get("Retry-After") == "" || strings.Contains(string(r.raw), "items") {
		t.Fatalf("429 cần Retry-After, không dữ liệu: %v %s", r.header, r.raw)
	}
	calls, units := e.meter.snapshot()
	if len(calls) != 3 || units != 2 || calls[2].Status != 429 || meteringport.UnitsV1(calls[2]) != 0 {
		t.Fatalf("sổ: %d dòng, %d đơn vị, dòng cuối %+v", len(calls), units, calls[len(calls)-1])
	}
	// Thẩm quyền cuối là Ledger.Record: kiểm sớm cho qua nhưng Record từ chối → 429, không dữ liệu.
	e2 := newEnv(t, func(d *public.Deps) { d.Quotas = alwaysRemaining{} })
	e2.meter.monthly = 0
	r = e2.do(http.MethodGet, collPath, e2.key, nil)
	wantCode(t, r, 429, meteringport.ErrQuotaExceeded.Code, "Record từ chối")
	if strings.Contains(string(r.raw), "30001") || r.header.Get("Retry-After") == "" {
		t.Fatalf("429 từ sổ không có dữ liệu, có Retry-After: %s", r.raw)
	}
	if calls, units := e2.meter.snapshot(); len(calls) != 1 || units != 0 || calls[0].Status != 429 {
		t.Fatalf("Record từ chối: một dòng 429, 0 đơn vị: %+v", calls)
	}
}

// ADR-0022 mục 7 (P-09, P-11): token bucket theo khách hàng → 429 apiclient.rate_limited + Retry-After + RateLimit-*.
func TestRateLimit_ADR0022(t *testing.T) {
	e := newEnv(t, func(d *public.Deps) {
		d.Plans = fakePlans{plan: port.Plan{RequestsPerMinute: 3, DailyUnits: 100, MonthlyUnits: 100, MaxExportRows: 10}}
	})
	for i := 0; i < 3; i++ {
		r := e.do(http.MethodGet, collPath, e.key, nil)
		if r.status != 200 || r.header.Get(port.HeaderRateLimitRemaining) != []string{"2", "1", "0"}[i] {
			t.Fatalf("lời %d: %d remaining=%q", i, r.status, r.header.Get(port.HeaderRateLimitRemaining))
		}
	}
	r := e.do(http.MethodGet, collPath, e.key, nil)
	wantCode(t, r, 429, port.ErrRateLimited.Code, "vượt tần suất")
	if r.header.Get("Retry-After") == "" || r.header.Get(port.HeaderRateLimitLimit) != "3" || r.header.Get(port.HeaderRateLimitRemaining) != "0" {
		t.Fatalf("header 429: %v", r.header)
	}
	if calls, units := e.meter.snapshot(); len(calls) != 4 || units != 3 || calls[3].Status != 429 {
		t.Fatalf("lời bị giới hạn vẫn một dòng sổ, 0 đơn vị: %d %d", len(calls), units)
	}
	// Sau 20 giây nạp lại một lượt (3/phút).
	e.clk.Advance(21 * time.Second)
	if r := e.do(http.MethodGet, collPath, e.key, nil); r.status != 200 {
		t.Fatalf("sau khi nạp lại muốn 200, nhận %d", r.status)
	}
}

// ADR-0022 mục 9 (P-17): mặc định từ chối — phần tử có trường ngoài hợp đồng không bao giờ được trả.
func TestDefaultDenyExtraField_ADR0022(t *testing.T) {
	p, err := dataproduct.Get(port.ProductCollectionVolume)
	if err != nil {
		t.Fatal(err)
	}
	type leaky struct {
		port.CollectionVolumeV1
		HouseholdRef string `json:"household_ref"` // trường lạ (vd mã nông hộ)
	}
	_, err = app.EncodeItems(p, []leaky{{CollectionVolumeV1: cell("30001"), HouseholdRef: "hh-1"}})
	if err == nil || !strings.Contains(err.Error(), port.ErrContractViolation.Code) {
		t.Fatalf("trường lạ phải bị từ chối: %v", err)
	}
	raws, err := app.EncodeItems(p, []port.CollectionVolumeV1{cell("30001")})
	if err != nil || len(raws) != 1 {
		t.Fatalf("phần tử đúng hợp đồng phải qua: %v", err)
	}
	// Sai kiểu (area không đúng mẫu) → không trả gì qua route, 500 contract_violation.
	e := newEnv(t)
	bad := cell("30001")
	bad.AreaHaCollected = "35.4"
	e.coll.items = []port.CollectionVolumeV1{cell("30002"), bad}
	r := e.do(http.MethodGet, collPath, e.key, nil)
	wantCode(t, r, 500, port.ErrContractViolation.Code, "phần tử sai hợp đồng")
	if strings.Contains(string(r.raw), "30002") {
		t.Fatal("không trả phần tử nào khi một phần tử vi phạm")
	}
	var anyItems map[string]json.RawMessage
	_ = json.Unmarshal(r.raw, &anyItems)
	if _, ok := anyItems["items"]; ok {
		t.Fatal("thân lỗi không có items")
	}
}

// P-13: POST thiếu Idempotency-Key → 400 (vẫn một dòng sổ, 0 đơn vị).
func TestExportRequiresIdempotencyKey_ADR0022(t *testing.T) {
	e := newEnv(t)
	r := e.do(http.MethodPost, collPath+"/exports", e.key, []byte(`{"format":"csv"}`))
	wantCode(t, r, 400, "idempotency_key_required", "thiếu Idempotency-Key")
	if calls, units := e.meter.snapshot(); len(calls) != 1 || units != 0 || calls[0].Kind != meteringport.CallExportRequest {
		t.Fatalf("sổ: %+v", calls)
	}
}

type alwaysRemaining struct{}

func (alwaysRemaining) Remaining(context.Context, uuid.UUID, uuid.UUID, string, time.Time) (meteringport.Remaining, error) {
	return meteringport.Remaining{DailyUnits: 1, MonthlyUnits: 1}, nil
}
