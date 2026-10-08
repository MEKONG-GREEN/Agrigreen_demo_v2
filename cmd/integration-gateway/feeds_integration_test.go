package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/internal/coopwire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const feedHP = apiclientport.FeedHarvestProgress

// onboardFeed: khách hàng API (đề xuất ≠ duyệt) + đăng ký feed harvest_progress_by_commune (scope feed:<tên>:read,
// bốn mắt) + gói hạn mức + khóa API HMAC. Trả bí mật khóa.
func (e *intEnv) onboardFeed() (clientID, key string) {
	t := e.t
	tenantB := ids.New()
	ad, ap := e.adminAs(e.operator, apiclientport.RoleAPIClientAdmin), e.adminAs(e.operator, apiclientport.RoleAPIClientApprover)
	r := ad(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": tenantB.String(), "display_name": "Đối tác feed (thử)",
		"purpose": "Điều phối gom rơm theo tiến độ gặt từng xã", "contact_ref": ids.New().String()})
	want(t, r, http.StatusCreated, "đề xuất khách hàng API")
	clientID = r.body["id"].(string)
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/approve", nil), http.StatusOK, "duyệt khách hàng")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": feedHP, "scopes": []string{"dp:x:read"}})
	want(t, r, http.StatusUnprocessableEntity, "scope lạ cho feed bị từ chối")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": feedHP,
		"scopes": []string{apiclientport.FeedScope(feedHP), apiclientport.ScopeUsageRead}})
	want(t, r, http.StatusCreated, "đề xuất đăng ký feed")
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions/"+r.body["id"].(string)+"/approve", nil), http.StatusOK, "duyệt đăng ký feed")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": 1000,
		"monthly_units": 10000, "max_export_rows": 1000})
	want(t, r, http.StatusCreated, "đề xuất gói hạn mức")
	want(t, ap(http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", clientID, r.body["version"]), nil), http.StatusOK, "duyệt gói")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/keys", nil)
	want(t, r, http.StatusCreated, "phát hành khóa")
	key, _ = r.body["api_key"].(string)
	return clientID, key
}

// buildFeeds chạy job dựng của pf/apiclient (như cmd/worker) trên module THẬT của cổng: nguồn ô thô pf/cropcycle
// (dữ kiện thửa pf/cooperative → pf/geo, đơn vị hành chính + k ở pf/masterdata), một tenant đóng góp = bên cấp.
func (e *intEnv) buildFeeds() {
	e.t.Helper()
	pool, clk := e.pg.Pool(e.t, migrations.PF, dbadmin.AppPublic), clock.System{}
	md := masterdata.New(pool, clk)
	g := geo.New(pool, clk, geo.Deps{Params: md.Params(), AdminUnits: md.AdminUnits()})
	cc := cropcycle.New(pool, clk, cropcycle.Deps{Params: md.Params()})
	cc.UseCollectionVolume(cooperative.New(coopwire.Sources(coopwire.Legacy{PlotFacts: g.PlotFacts()})).Plots(), md.AdminUnits())
	e.mod.UseFeedSources(cc.AggregateSource(), apiclientport.StaticContributors{e.grantor}, md.Params())
	if _, err := e.mod.BuildFeeds(context.Background()); err != nil {
		e.t.Fatalf("dựng feed: %v", err)
	}
}

// W5.3 (R-INT-05, R-INT-06, ADR-0033 §2, ADR-0035): GET /v1/feeds/{feed}?cursor= qua cổng THẬT với khóa API HMAC:
//   - hợp đồng còn proposed → 403 integration.contract_not_servable khi chưa bật AG_INTEGRATION_ALLOW_UNAPPROVED;
//   - bật (ngoài production) → 200 phong bì feed_page.v1, ô dưới k không bao giờ có, k_used + tổng kiểm, ảnh chụp
//     chỉ là href; phân trang theo cursor; cursor sai → 400;
//   - thiếu scope feed:<tên>:read → 403; mỗi lời gọi một dòng sổ đo lường;
//   - URL ký ảnh chụp cấp lúc yêu cầu (≤ 15 phút), đo theo kích thước ảnh chụp;
//   - bản ghi trong kho lệch hợp đồng → 500 apiclient.contract_violation, không byte dữ liệu.
func TestFeedsOverGateway_W53Integration(t *testing.T) {
	e := newIntEnv(t)
	e.seedCollection()
	e.buildFeeds()
	cid, key := e.onboardFeed()

	r := e.call(http.MethodGet, "/v1/feeds/"+feedHP, key, nil)
	if r.status != http.StatusForbidden || r.code() != apiclientport.ErrContractNotServable.Code {
		t.Fatalf("hợp đồng proposed, chưa bật cờ → 403 integration.contract_not_servable: %d %s", r.status, r.raw)
	}

	d := e.deps
	d.AllowUnapproved = true
	srv := httptest.NewServer(gateway.NewHandler(e.mod, d, nil, e.log))
	t.Cleanup(srv.Close)
	get := func(path, k string) intResp {
		return doReq(t, http.MethodGet, srv.URL+path, map[string]string{apiclientport.KeyHeader: k}, nil)
	}

	r = get("/v1/feeds/"+feedHP, key)
	want(t, r, http.StatusOK, "đọc feed")
	if r.header.Get(apiclientport.HeaderDataContract) != feedHP+".v1" || r.header.Get("AG-Contract-Status") != "proposed" {
		t.Fatalf("header hợp đồng: %v", r.header)
	}
	env, err := integration.EnvelopeFor(integration.KindFeed)
	if err != nil || env.Validate(r.raw) != nil {
		t.Fatalf("trang phải khớp phong bì feed_page.v1: %v %s", err, r.raw)
	}
	items, _ := r.body["items"].([]any)
	if len(items) != 1 || !strings.Contains(string(r.raw), `"area_code":"31117"`) || strings.Contains(string(r.raw), "31120") {
		t.Fatalf("chỉ xã 31117 (10 nông hộ = k); 31120 (3) và dòng withheld (3 < k) bị ẩn: %s", r.raw)
	}
	ds, _ := r.body["dataset"].(map[string]any)
	snap, _ := r.body["snapshot"].(map[string]any)
	if ds["k_used"] != float64(10) || ds["contributors"] != float64(1) || ds["version"] != float64(1) || snap["href"] != "/v1/snapshots/"+feedHP+"/1" {
		t.Fatalf("dataset + snapshot href: %s", r.raw)
	}
	for _, banned := range []string{"household", "plot_ref", "sig=", "X-Amz"} {
		if strings.Contains(string(r.raw), banned) {
			t.Fatalf("trang feed không được có %q: %s", banned, r.raw)
		}
	}
	next := r.body["next_cursor"].(string)
	r = get("/v1/feeds/"+feedHP+"?cursor="+next, key)
	if r.status != http.StatusOK || len(r.body["items"].([]any)) != 0 || r.body["next_cursor"] != next || r.body["cursor"] != next {
		t.Fatalf("đọc tiếp sau cursor: %d %s", r.status, r.raw)
	}
	if r = get("/v1/feeds/"+feedHP+"?cursor=@@", key); r.status != http.StatusBadRequest || r.code() != apiclientport.ErrInvalidCursor.Code {
		t.Fatalf("cursor sai → 400 integration.invalid_cursor: %d %s", r.status, r.raw)
	}
	if r = get("/v1/feeds/no_such_feed", key); r.status != http.StatusNotFound || r.code() != apiclientport.ErrUnknownFeed.Code {
		t.Fatalf("feed không có → 404: %d %s", r.status, r.raw)
	}
	// Khách hàng chỉ có scope sản phẩm dữ liệu → 403 với feed.
	_, _, otherKey := e.onboard(100)
	if r = get("/v1/feeds/"+feedHP, otherKey); r.status != http.StatusForbidden || r.code() != apiclientport.ErrForbidden.Code {
		t.Fatalf("thiếu scope feed → 403 apiclient.forbidden: %d %s", r.status, r.raw)
	}

	// Ảnh chụp: URL ký cấp lúc yêu cầu, ≤ 15 phút; đo theo kích thước ảnh chụp.
	r = get("/v1/snapshots/"+feedHP+"/1", key)
	want(t, r, http.StatusOK, "cấp URL ảnh chụp")
	exp, err := time.Parse(time.RFC3339, fmt.Sprint(r.body["expires_at"]))
	if err != nil || time.Until(exp) > 15*time.Minute+time.Second || r.body["url"] == "" || r.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("URL ký ≤ 15 phút: %s", r.raw)
	}
	size, _ := r.body["size"].(float64)
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND call_kind = 'file_download'
	    AND product = $2::text AND bytes_out = $3::bigint`, uuid.MustParse(cid), feedHP, int64(size)); n != 1 {
		t.Fatalf("sổ đo lường lần cấp URL theo kích thước ảnh chụp: %d", n)
	}
	if r = get("/v1/snapshots/"+feedHP+"/9", key); r.status != http.StatusNotFound || r.code() != apiclientport.ErrSnapshotNotFound.Code {
		t.Fatalf("phiên bản không có → 404: %d %s", r.status, r.raw)
	}
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid`, uuid.MustParse(cid)); n < 7 {
		t.Fatalf("mỗi lời gọi đã xác thực một dòng sổ đo lường: %d", n)
	}

	// Bản ghi lệch hợp đồng trong kho (ghi thẳng bằng vai quản trị) → cổng không trả trang.
	ctx := context.Background()
	admin := e.pg.AdminPool(t, migrations.PF)
	if _, err := admin.Exec(ctx, `INSERT INTO pf_apiclient.feed_entry (tenant_id, feed, partition_key, seq, event_id, entity_key, op, contract_version, record, recorded_at)
	  SELECT tenant_id, feed, partition_key, last_seq + 1, $3::uuid, '31117:2026-DX', 'upsert', 1, '{"area_code":"31117","household_ref":"x"}'::jsonb, now()
	  FROM pf_apiclient.feed_head WHERE tenant_id = $1::uuid AND feed = $2::text`, e.operator, feedHP, ids.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `UPDATE pf_apiclient.feed_head SET last_seq = last_seq + 1 WHERE tenant_id = $1::uuid AND feed = $2::text`, e.operator, feedHP); err != nil {
		t.Fatal(err)
	}
	r = get("/v1/feeds/"+feedHP+"?cursor="+next, key)
	if r.status != http.StatusInternalServerError || r.code() != apiclientport.ErrContractViolation.Code || strings.Contains(string(r.raw), "household_ref") {
		t.Fatalf("bản ghi lệch hợp đồng → 500 apiclient.contract_violation, không dữ liệu: %d %s", r.status, r.raw)
	}
}
