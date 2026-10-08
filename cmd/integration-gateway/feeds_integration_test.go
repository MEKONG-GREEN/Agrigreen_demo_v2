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

// onboardFeed: khÃ¡ch hÃ ng API (Ä‘á» xuáº¥t â‰  duyá»‡t) + Ä‘Äƒng kÃ½ feed harvest_progress_by_commune (scope feed:<tÃªn>:read,
// bá»‘n máº¯t) + gÃ³i háº¡n má»©c + khÃ³a API HMAC. Tráº£ bÃ­ máº­t khÃ³a.
func (e *intEnv) onboardFeed() (clientID, key string) {
	t := e.t
	tenantB := ids.New()
	ad, ap := e.adminAs(e.operator, apiclientport.RoleAPIClientAdmin), e.adminAs(e.operator, apiclientport.RoleAPIClientApprover)
	r := ad(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": tenantB.String(), "display_name": "Äá»‘i tÃ¡c feed (thá»­)",
		"purpose": "Äiá»u phá»‘i gom rÆ¡m theo tiáº¿n Ä‘á»™ gáº·t tá»«ng xÃ£", "contact_ref": ids.New().String()})
	want(t, r, http.StatusCreated, "Ä‘á» xuáº¥t khÃ¡ch hÃ ng API")
	clientID = r.body["id"].(string)
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/approve", nil), http.StatusOK, "duyá»‡t khÃ¡ch hÃ ng")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": feedHP, "scopes": []string{"dp:x:read"}})
	want(t, r, http.StatusUnprocessableEntity, "scope láº¡ cho feed bá»‹ tá»« chá»‘i")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": feedHP,
		"scopes": []string{apiclientport.FeedScope(feedHP), apiclientport.ScopeUsageRead}})
	want(t, r, http.StatusCreated, "Ä‘á» xuáº¥t Ä‘Äƒng kÃ½ feed")
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions/"+r.body["id"].(string)+"/approve", nil), http.StatusOK, "duyá»‡t Ä‘Äƒng kÃ½ feed")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": 1000,
		"monthly_units": 10000, "max_export_rows": 1000})
	want(t, r, http.StatusCreated, "Ä‘á» xuáº¥t gÃ³i háº¡n má»©c")
	want(t, ap(http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", clientID, r.body["version"]), nil), http.StatusOK, "duyá»‡t gÃ³i")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/keys", nil)
	want(t, r, http.StatusCreated, "phÃ¡t hÃ nh khÃ³a")
	key, _ = r.body["api_key"].(string)
	return clientID, key
}

// buildFeeds cháº¡y job dá»±ng cá»§a pf/apiclient (nhÆ° cmd/worker) trÃªn module THáº¬T cá»§a cá»•ng: nguá»“n Ã´ thÃ´ pf/cropcycle
// (dá»¯ kiá»‡n thá»­a pf/cooperative â†’ pf/geo, Ä‘Æ¡n vá»‹ hÃ nh chÃ­nh + k á»Ÿ pf/masterdata), má»™t tenant Ä‘Ã³ng gÃ³p = bÃªn cáº¥p.
func (e *intEnv) buildFeeds() {
	e.t.Helper()
	pool, clk := e.pg.Pool(e.t, migrations.PF, dbadmin.AppPublic), clock.System{}
	md := masterdata.New(pool, clk)
	g := geo.New(pool, clk, geo.Deps{Params: md.Params(), AdminUnits: md.AdminUnits()})
	cc := cropcycle.New(pool, clk, cropcycle.Deps{Params: md.Params()})
	cc.UseCollectionVolume(cooperative.New(coopwire.Sources(coopwire.Legacy{PlotFacts: g.PlotFacts()})).Plots(), md.AdminUnits())
	e.mod.UseFeedSources(cc.AggregateSource(), apiclientport.StaticContributors{e.grantor}, md.Params())
	if _, err := e.mod.BuildFeeds(context.Background()); err != nil {
		e.t.Fatalf("dá»±ng feed: %v", err)
	}
}

// W5.3 (R-INT-05, R-INT-06, ADR-0033 Â§2, ADR-0035): GET /v1/feeds/{feed}?cursor= qua cá»•ng THáº¬T vá»›i khÃ³a API HMAC:
//   - há»£p Ä‘á»“ng cÃ²n proposed â†’ 403 integration.contract_not_servable khi chÆ°a báº­t AG_GATEWAY_FEED_ALLOW_PROPOSED;
//   - báº­t (ngoÃ i production) â†’ 200 phong bÃ¬ feed_page.v1, Ã´ dÆ°á»›i k khÃ´ng bao giá» cÃ³, k_used + tá»•ng kiá»ƒm, áº£nh chá»¥p
//     chá»‰ lÃ  href; phÃ¢n trang theo cursor; cursor sai â†’ 400;
//   - thiáº¿u scope feed:<tÃªn>:read â†’ 403; má»—i lá»i gá»i má»™t dÃ²ng sá»• Ä‘o lÆ°á»ng;
//   - URL kÃ½ áº£nh chá»¥p cáº¥p lÃºc yÃªu cáº§u (â‰¤ 15 phÃºt), Ä‘o theo kÃ­ch thÆ°á»›c áº£nh chá»¥p;
//   - báº£n ghi trong kho lá»‡ch há»£p Ä‘á»“ng â†’ 500 apiclient.contract_violation, khÃ´ng byte dá»¯ liá»‡u.
func TestFeedsOverGateway_W53Integration(t *testing.T) {
	e := newIntEnv(t)
	e.seedCollection()
	e.buildFeeds()
	cid, key := e.onboardFeed()

	r := e.call(http.MethodGet, "/v1/feeds/"+feedHP, key, nil)
	if r.status != http.StatusForbidden || r.code() != apiclientport.ErrContractNotServable.Code {
		t.Fatalf("há»£p Ä‘á»“ng proposed, chÆ°a báº­t cá» â†’ 403 integration.contract_not_servable: %d %s", r.status, r.raw)
	}

	d := e.deps
	d.AllowUnapproved = true
	srv := httptest.NewServer(gateway.NewHandler(e.mod, d, nil, e.log))
	t.Cleanup(srv.Close)
	get := func(path, k string) intResp {
		return doReq(t, http.MethodGet, srv.URL+path, map[string]string{apiclientport.KeyHeader: k}, nil)
	}

	r = get("/v1/feeds/"+feedHP, key)
	want(t, r, http.StatusOK, "Ä‘á»c feed")
	if r.header.Get(apiclientport.HeaderDataContract) != feedHP+".v1" || r.header.Get("AG-Contract-Status") != "proposed" {
		t.Fatalf("header há»£p Ä‘á»“ng: %v", r.header)
	}
	env, err := integration.EnvelopeFor(integration.KindFeed)
	if err != nil || env.Validate(r.raw) != nil {
		t.Fatalf("trang pháº£i khá»›p phong bÃ¬ feed_page.v1: %v %s", err, r.raw)
	}
	items, _ := r.body["items"].([]any)
	if len(items) != 1 || !strings.Contains(string(r.raw), `"area_code":"31117"`) || strings.Contains(string(r.raw), "31120") {
		t.Fatalf("chá»‰ xÃ£ 31117 (10 nÃ´ng há»™ = k); 31120 (3) vÃ  dÃ²ng withheld (3 < k) bá»‹ áº©n: %s", r.raw)
	}
	ds, _ := r.body["dataset"].(map[string]any)
	snap, _ := r.body["snapshot"].(map[string]any)
	if ds["k_used"] != float64(10) || ds["contributors"] != float64(1) || ds["version"] != float64(1) || snap["href"] != "/v1/snapshots/"+feedHP+"/1" {
		t.Fatalf("dataset + snapshot href: %s", r.raw)
	}
	for _, banned := range []string{"household", "plot_ref", "sig=", "X-Amz"} {
		if strings.Contains(string(r.raw), banned) {
			t.Fatalf("trang feed khÃ´ng Ä‘Æ°á»£c cÃ³ %q: %s", banned, r.raw)
		}
	}
	next := r.body["next_cursor"].(string)
	r = get("/v1/feeds/"+feedHP+"?cursor="+next, key)
	if r.status != http.StatusOK || len(r.body["items"].([]any)) != 0 || r.body["next_cursor"] != next || r.body["cursor"] != next {
		t.Fatalf("Ä‘á»c tiáº¿p sau cursor: %d %s", r.status, r.raw)
	}
	if r = get("/v1/feeds/"+feedHP+"?cursor=@@", key); r.status != http.StatusBadRequest || r.code() != apiclientport.ErrInvalidCursor.Code {
		t.Fatalf("cursor sai â†’ 400 integration.invalid_cursor: %d %s", r.status, r.raw)
	}
	if r = get("/v1/feeds/no_such_feed", key); r.status != http.StatusNotFound || r.code() != apiclientport.ErrUnknownFeed.Code {
		t.Fatalf("feed khÃ´ng cÃ³ â†’ 404: %d %s", r.status, r.raw)
	}
	// KhÃ¡ch hÃ ng chá»‰ cÃ³ scope sáº£n pháº©m dá»¯ liá»‡u â†’ 403 vá»›i feed.
	_, _, otherKey := e.onboard(100)
	if r = get("/v1/feeds/"+feedHP, otherKey); r.status != http.StatusForbidden || r.code() != apiclientport.ErrForbidden.Code {
		t.Fatalf("thiáº¿u scope feed â†’ 403 apiclient.forbidden: %d %s", r.status, r.raw)
	}

	// áº¢nh chá»¥p: URL kÃ½ cáº¥p lÃºc yÃªu cáº§u, â‰¤ 15 phÃºt; Ä‘o theo kÃ­ch thÆ°á»›c áº£nh chá»¥p.
	r = get("/v1/snapshots/"+feedHP+"/1", key)
	want(t, r, http.StatusOK, "cáº¥p URL áº£nh chá»¥p")
	exp, err := time.Parse(time.RFC3339, fmt.Sprint(r.body["expires_at"]))
	if err != nil || time.Until(exp) > 15*time.Minute+time.Second || r.body["url"] == "" || r.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("URL kÃ½ â‰¤ 15 phÃºt: %s", r.raw)
	}
	size, _ := r.body["size"].(float64)
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND call_kind = 'file_download'
	    AND product = $2::text AND bytes_out = $3::bigint`, uuid.MustParse(cid), feedHP, int64(size)); n != 1 {
		t.Fatalf("sá»• Ä‘o lÆ°á»ng láº§n cáº¥p URL theo kÃ­ch thÆ°á»›c áº£nh chá»¥p: %d", n)
	}
	if r = get("/v1/snapshots/"+feedHP+"/9", key); r.status != http.StatusNotFound || r.code() != apiclientport.ErrSnapshotNotFound.Code {
		t.Fatalf("phiÃªn báº£n khÃ´ng cÃ³ â†’ 404: %d %s", r.status, r.raw)
	}
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid`, uuid.MustParse(cid)); n < 7 {
		t.Fatalf("má»—i lá»i gá»i Ä‘Ã£ xÃ¡c thá»±c má»™t dÃ²ng sá»• Ä‘o lÆ°á»ng: %d", n)
	}

	// Báº£n ghi lá»‡ch há»£p Ä‘á»“ng trong kho (ghi tháº³ng báº±ng vai quáº£n trá»‹) â†’ cá»•ng khÃ´ng tráº£ trang.
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
		t.Fatalf("báº£n ghi lá»‡ch há»£p Ä‘á»“ng â†’ 500 apiclient.contract_violation, khÃ´ng dá»¯ liá»‡u: %d %s", r.status, r.raw)
	}
}
