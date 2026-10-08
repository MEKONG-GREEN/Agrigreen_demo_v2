package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// seedDirectory ghi danh bạ HTX (pf/party) + thửa (pf/geo) tối thiểu của bên cấp: 2 HTX (một có mã xã + tọa độ hồ sơ,
// một không), 3 thửa (2 của HTX thứ nhất, 1 nháp chưa gán HTX).
func (e *intEnv) seedDirectory() (coops, plots []uuid.UUID) {
	t := e.t
	ctx := context.Background()
	actor := ids.New()
	coops = []uuid.UUID{ids.New(), ids.New()}
	plots = []uuid.UUID{ids.New(), ids.New(), ids.New()}
	exec := func(role string, f func(tx pgx.Tx) error) {
		t.Helper()
		if err := tenant.WithTenantTx(ctx, e.pg.Pool(t, migrations.PF, role), e.grantor, f); err != nil {
			t.Fatalf("seed %s: %v", role, err)
		}
	}
	exec("party_app", func(tx pgx.Tx) error {
		for i, c := range coops {
			var unit *string
			if i == 0 {
				u := "89123"
				unit = &u
			}
			if _, err := tx.Exec(ctx, `INSERT INTO pf_party.org (tenant_id, ref, kind, code, name, admin_unit_code, active, last_event_id, last_seq)
			  VALUES ($1, $2, 'cooperative', $3, $4, $5, true, $6, 1)`, e.grantor, c, fmt.Sprintf("HTX-%d", i), fmt.Sprintf("HTX Thử %d", i), unit, ids.New()); err != nil {
				return err
			}
		}
		return nil
	})
	exec("geo_app", func(tx pgx.Tx) error {
		for i, p := range plots {
			var coop, hh *uuid.UUID
			status := "active"
			if i < 2 {
				h := ids.New()
				coop, hh = &coops[0], &h
			} else {
				status = "draft"
			}
			if _, err := tx.Exec(ctx, `INSERT INTO pf_geo.plot (tenant_id, ref, code, commune_code, cooperative_ref, household_ref, status, current_version,
			    current_boundary_id, geom, area_ha, source, created_by)
			  VALUES ($1, $2, $3, '89123', $4, $5, $6, 1, $7, ST_GeomFromText('MULTIPOLYGON(((105.7 10.0,105.701 10.0,105.701 10.001,105.7 10.0)))', 4326),
			    1.2500::numeric, 'kiểm thử W5.5', $8)`,
				e.grantor, p, fmt.Sprintf("W55-%02d", i), coop, hh, status, ids.New(), actor); err != nil {
				return err
			}
		}
		return nil
	})
	return coops, plots
}

// grantDirectory: bên cấp đề xuất + duyệt (hai người) cấp quyền cooperative_directory + plot_directory cho e.third.
func (e *intEnv) grantDirectory() string {
	pa, pb := e.adminAs(e.grantor, authz.RolePlatformAdmin), e.adminAs(e.grantor, authz.RolePlatformAdmin)
	r := pa(http.MethodPost, "/v1/share-grants", map[string]any{"grantee_tenant_id": e.third.String(),
		"kinds":    []string{partyport.KindCooperativeDirectory, partyport.KindPlotDirectory},
		"purpose":  "MKG đối chiếu HTX/thửa khi lập lệnh gom (ADR-0020, ADR-0033 §5 — W5.5)",
		"valid_to": time.Now().Add(90 * 24 * time.Hour).UTC().Format(time.RFC3339)})
	want(e.t, r, http.StatusCreated, "đề xuất cấp quyền danh bạ")
	id := r.body["id"].(string)
	want(e.t, pb(http.MethodPost, "/v1/share-grants/"+id+"/approve", nil), http.StatusCreated, "duyệt cấp quyền danh bạ")
	return id
}

// onboardDirectoryFeeds: khách hàng API tenant e.third + đăng ký hai feed danh bạ (bốn mắt) + gói + khóa.
func (e *intEnv) onboardDirectoryFeeds() (clientID, key string) {
	t := e.t
	ad, ap := e.adminAs(e.operator, apiclientport.RoleAPIClientAdmin), e.adminAs(e.operator, apiclientport.RoleAPIClientApprover)
	r := ad(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": e.third.String(), "display_name": "MKG (thử)",
		"purpose": "Bản sao danh bạ HTX/thửa cho lệnh gom", "contact_ref": ids.New().String()})
	want(t, r, http.StatusCreated, "đề xuất khách hàng API")
	clientID = r.body["id"].(string)
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/approve", nil), http.StatusOK, "duyệt khách hàng")
	for _, f := range []string{apiclientport.FeedCooperativeDirectory, apiclientport.FeedPlotDirectory} {
		r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": f, "scopes": []string{apiclientport.FeedScope(f)}})
		want(t, r, http.StatusCreated, "đề xuất đăng ký "+f)
		want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions/"+r.body["id"].(string)+"/approve", nil), http.StatusOK, "duyệt đăng ký "+f)
	}
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": 1000,
		"monthly_units": 10000, "max_export_rows": 1000})
	want(t, r, http.StatusCreated, "đề xuất gói hạn mức")
	want(t, ap(http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", clientID, r.body["version"]), nil), http.StatusOK, "duyệt gói")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/keys", nil)
	want(t, r, http.StatusCreated, "phát hành khóa")
	key, _ = r.body["api_key"].(string)
	return clientID, key
}

// buildDirectory chạy job dựng feed lớp partner (như cmd/worker) trên module THẬT của cổng.
func (e *intEnv) buildDirectory() {
	e.t.Helper()
	pool, clk := e.pg.Pool(e.t, migrations.PF, dbadmin.AppPublic), clock.System{}
	md := masterdata.New(pool, clk)
	g := geo.New(pool, clk, geo.Deps{Params: md.Params(), AdminUnits: md.AdminUnits()})
	gl := grantlookup.New(pool, clk)
	e.mod.UsePartnerFeedSources(map[string]apiclientport.PartnerSource{apiclientport.FeedCooperativeDirectory: gl,
		apiclientport.FeedPlotDirectory: g.PartnerFeedSource()}, gl)
	for _, f := range []string{apiclientport.FeedCooperativeDirectory, apiclientport.FeedPlotDirectory} {
		if _, err := e.mod.Service().BuildPartner(context.Background(), f); err != nil {
			e.t.Fatalf("dựng feed %s: %v", f, err)
		}
	}
}

// W5.5 (ADR-0033 §2, §5; ADR-0020): feed lớp partner cooperative_directory / plot_directory qua cổng THẬT:
//   - phân vùng theo bên cấp, chỉ dựng cho bên cấp có cấp quyền cho bên nhận có đăng ký feed;
//   - trang khớp phong bì + hợp đồng, nội dung khớp đúng truy vấn của GET /v1/partner/cooperatives (store
//     CooperativeEntries) — không người đại diện / nông hộ / hình học;
//   - mỗi trang 2xx một dòng sổ đọc partner_read_log (loại = grant_kind) ở tenant bên cấp;
//   - lần dựng không đổi → không mục mới, as_of tiến; đổi một thửa → một upsert;
//   - thu hồi cấp quyền → lời gọi kế tiếp 403 party.share_grant_missing (feed dừng); bên nhận không có cấp quyền → 403.
func TestDirectoryFeedsOverGateway_W55Integration(t *testing.T) {
	e := newIntEnv(t)
	coops, plots := e.seedDirectory()
	grantID := e.grantDirectory()
	_, key := e.onboardDirectoryFeeds()
	e.buildDirectory()

	d := e.deps
	d.AllowUnapproved = true
	srv := httptest.NewServer(gateway.NewHandler(e.mod, d, nil, e.log))
	t.Cleanup(srv.Close)
	get := func(path string) intResp {
		return doReq(t, http.MethodGet, srv.URL+path, map[string]string{apiclientport.KeyHeader: key}, nil)
	}
	env, err := integration.EnvelopeFor(integration.KindFeed)
	if err != nil {
		t.Fatal(err)
	}

	r := get("/v1/feeds/" + apiclientport.FeedCooperativeDirectory)
	want(t, r, http.StatusOK, "đọc feed cooperative_directory")
	if env.Validate(r.raw) != nil || r.body["max_age"] != "PT6H" || r.body["dataset"] != nil {
		t.Fatalf("phong bì feed_page.v1, max_age của hợp đồng, không theo phiên bản: %s", r.raw)
	}
	var page struct {
		AsOf  time.Time `json:"as_of"`
		Next  string    `json:"next_cursor"`
		Items []struct {
			Op     string          `json:"op"`
			Key    string          `json:"entity_key"`
			Record json.RawMessage `json:"record"`
		} `json:"items"`
	}
	if err := json.Unmarshal(r.raw, &page); err != nil {
		t.Fatal(err)
	}
	// Parity với bề mặt cũ: cùng truy vấn danh bạ của /v1/partner/cooperatives (party/store.CooperativeEntries).
	type coopRow struct {
		Ref         uuid.UUID
		Code        string
		CommuneCode *string
		Active      bool
	}
	var old []coopRow
	if err := tenant.WithTenantTx(context.Background(), e.pg.Pool(t, migrations.PF, "party_app"), e.grantor, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT ref, code, admin_unit_code, active FROM pf_party.org WHERE kind = 'cooperative' ORDER BY ref`)
		if err != nil {
			return err
		}
		old, err = pgx.CollectRows(rows, pgx.RowToStructByPos[coopRow])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != len(old) || len(old) != 2 {
		t.Fatalf("feed %d mục, bề mặt cũ %d HTX: %s", len(page.Items), len(old), r.raw)
	}
	for i, it := range page.Items {
		var rec struct {
			Ref     uuid.UUID `json:"cooperative_ref"`
			Code    string    `json:"code"`
			Commune *string   `json:"commune_code"`
			Active  bool      `json:"active"`
		}
		_ = json.Unmarshal(it.Record, &rec)
		o := old[i]
		if it.Op != "upsert" || rec.Ref != o.Ref || rec.Code != o.Code || rec.Active != o.Active || deref(rec.Commune) != deref(o.CommuneCode) {
			t.Fatalf("mục %d lệch bề mặt cũ: %s vs %+v", i, it.Record, o)
		}
	}
	if !slices.Contains([]uuid.UUID{old[0].Ref, old[1].Ref}, coops[0]) {
		t.Fatal("HTX đã seed phải có")
	}
	if n := e.count(e.grantor, `SELECT count(*) FROM pf_party.partner_read_log WHERE kind = 'cooperative_directory' AND grantee_tenant_id = $1::uuid
	    AND endpoint = 'GET /v1/feeds/{feed}'`, e.third); n != 1 {
		t.Fatalf("sổ đọc bên cấp cho trang feed: %d", n)
	}

	r = get("/v1/feeds/" + apiclientport.FeedPlotDirectory + "?grantor=" + e.grantor.String())
	want(t, r, http.StatusOK, "đọc feed plot_directory với grantor")
	if items, _ := r.body["items"].([]any); len(items) != len(plots) {
		t.Fatalf("3 thửa: %s", r.raw)
	}
	for _, banned := range []string{"household", "geometry", "MULTIPOLYGON", "coordinates"} {
		if contains(r.raw, banned) {
			t.Fatalf("feed thửa không được có %q: %s", banned, r.raw)
		}
	}
	plotNext := r.body["next_cursor"].(string)

	// Dựng lại không đổi → không mục mới, as_of tiến. Đổi trạng thái một thửa → đúng một upsert.
	time.Sleep(10 * time.Millisecond)
	e.buildDirectory()
	r = get("/v1/feeds/" + apiclientport.FeedCooperativeDirectory + "?cursor=" + page.Next)
	want(t, r, http.StatusOK, "đọc tiếp")
	asOf, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(r.body["as_of"]))
	if items, _ := r.body["items"].([]any); len(items) != 0 || !asOf.After(page.AsOf) {
		t.Fatalf("không đổi → 0 mục, as_of tiến (%s → %s): %s", page.AsOf, asOf, r.raw)
	}
	if err := tenant.WithTenantTx(context.Background(), e.pg.Pool(t, migrations.PF, "geo_app"), e.grantor, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE pf_geo.plot SET status = 'retired', updated_at = now() WHERE ref = $1`, plots[1])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	e.buildDirectory()
	r = get("/v1/feeds/" + apiclientport.FeedPlotDirectory + "?cursor=" + plotNext)
	items, _ := r.body["items"].([]any)
	if r.status != http.StatusOK || len(items) != 1 || !contains(r.raw, plots[1].String()) || !contains(r.raw, `"retired"`) {
		t.Fatalf("thửa đổi → một upsert: %d %s", r.status, r.raw)
	}

	// Bên nhận khác (không có cấp quyền danh bạ) với cùng scope → 403.
	third := e.third
	e.third = ids.New()
	_, otherKey := e.onboardDirectoryFeeds()
	e.third = third
	if r = doReq(t, http.MethodGet, srv.URL+"/v1/feeds/"+apiclientport.FeedPlotDirectory, map[string]string{apiclientport.KeyHeader: otherKey}, nil); r.status != http.StatusForbidden ||
		r.code() != partyport.ErrShareGrantMissing.Code {
		t.Fatalf("không có cấp quyền → 403 party.share_grant_missing: %d %s", r.status, r.raw)
	}

	// Thu hồi → feed dừng ngay ở lời gọi kế tiếp.
	rv := e.adminAs(e.grantor, authz.RolePlatformAdmin)(http.MethodPost, "/v1/share-grants/"+grantID+"/revoke", map[string]any{"reason": "Kết thúc hợp đồng (thử W5.5)"})
	if rv.status >= 300 {
		t.Fatalf("thu hồi cấp quyền: %d %s", rv.status, rv.raw)
	}
	for _, f := range []string{apiclientport.FeedCooperativeDirectory, apiclientport.FeedPlotDirectory} {
		if r = get("/v1/feeds/" + f); r.status != http.StatusForbidden || r.code() != partyport.ErrShareGrantMissing.Code || contains(r.raw, "items") {
			t.Fatalf("thu hồi → 403 party.share_grant_missing, không dữ liệu (%s): %d %s", f, r.status, r.raw)
		}
	}
}

func contains(b []byte, sub string) bool { return strings.Contains(string(b), sub) }

func deref(p *string) string {
	if p == nil {
		return "<nil>"
	}
	return *p
}
