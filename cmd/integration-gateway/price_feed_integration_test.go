package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/integration"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

const feedPrice = apiclientport.FeedPriceBoardDaily

// priceStub đóng vai pf/price (port.PublicBoardFeed — nguồn thật kiểm ở internal/kn/price/handler TestPricePublicFeed_W1181):
// trả bản ghi đã công bố hiện hành của đơn vị công bố.
type priceStub struct {
	recs map[uuid.UUID][]map[string]any
	err  error
}

func (s *priceStub) PublishedRecords(_ context.Context, dataset string, publisher uuid.UUID, code string) ([]json.RawMessage, error) {
	if s.err != nil {
		return nil, s.err
	}
	if dataset != feedPrice || code != "89" {
		return nil, fmt.Errorf("tập/mã lạ %s %s", dataset, code)
	}
	var out []json.RawMessage
	for _, r := range s.recs[publisher] {
		b, _ := json.Marshal(r)
		out = append(out, b)
	}
	return out, nil
}

func priceRec(item, lo, hi string, version int, updating bool) map[string]any {
	return map[string]any{"province_code": "89", "board_date": "2026-10-08", "item_code": item, "item_name": "Mặt hàng " + item,
		"group_code": "rau_mau", "unit_code": "kg", "price_min": lo, "price_max": hi, "price_avg": lo, "points_reported": 2,
		"board_version": version, "approved_at": "2026-10-08T06:05:00Z", "week_key": "2026-W41", "updating": updating}
}

// onboardPriceFeed: khách hàng API (website) + đăng ký feed price_board_daily (bốn mắt) + gói + khóa.
func (e *intEnv) onboardPriceFeed() (clientID, key string) {
	t := e.t
	ad, ap := e.adminAs(e.operator, apiclientport.RoleAPIClientAdmin), e.adminAs(e.operator, apiclientport.RoleAPIClientApprover)
	r := ad(http.MethodPost, "/v1/api-clients", map[string]any{"client_tenant_id": ids.New().String(), "display_name": "Website Khuyến nông (thử)",
		"purpose": "Đăng bảng giá nông sản đã duyệt lên website", "contact_ref": ids.New().String()})
	want(t, r, http.StatusCreated, "đề xuất khách hàng API")
	clientID = r.body["id"].(string)
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/approve", nil), http.StatusOK, "duyệt khách hàng")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions", map[string]any{"product": feedPrice,
		"scopes": []string{apiclientport.FeedScope(feedPrice)}})
	want(t, r, http.StatusCreated, "đề xuất đăng ký feed")
	want(t, ap(http.MethodPost, "/v1/api-clients/"+clientID+"/subscriptions/"+r.body["id"].(string)+"/approve", nil), http.StatusOK, "duyệt đăng ký")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/plans", map[string]any{"requests_per_minute": 600, "daily_units": 1000,
		"monthly_units": 10000, "max_export_rows": 1000})
	want(t, r, http.StatusCreated, "đề xuất gói hạn mức")
	want(t, ap(http.MethodPost, fmt.Sprintf("/v1/api-clients/%s/plans/%v/approve", clientID, r.body["version"]), nil), http.StatusOK, "duyệt gói")
	r = ad(http.MethodPost, "/v1/api-clients/"+clientID+"/keys", nil)
	want(t, r, http.StatusCreated, "phát hành khóa")
	key, _ = r.body["api_key"].(string)
	return clientID, key
}

// W11.81 (FN-TI-02.07; ADR-0033 §2, R-INT-05/06): GET /v1/feeds/price_board_daily qua cổng THẬT + kho feed PostgreSQL thật:
//   - không có API công khai: thiếu khóa → 401; hợp đồng proposed → 403 khi chưa bật cờ; thiếu scope → 403;
//   - 200 phong bì feed_page.v1, header AG-Data-Contract, dataset.contributors = số đơn vị công bố, ảnh chụp chỉ là href;
//   - duyệt lại (v2) → upsert cùng entity_key; mặt hàng bỏ khỏi phiên bản mới → retract; mở duyệt → updating = true;
//   - bản ghi lệch hợp đồng (trường lạ, vd dữ liệu cá nhân) → job từ chối cả phiên bản; nguồn lỗi → không retract nhầm;
//   - mỗi lời gọi một dòng sổ đo lường + egress_event public_api; cursor quá hạn giữ → 410 kèm snapshot.
func TestPriceBoardFeedOverGateway_W1181(t *testing.T) {
	e := newIntEnv(t)
	ctx := context.Background()
	kn := ids.New()
	src := &priceStub{recs: map[uuid.UUID][]map[string]any{kn: {priceRec("MH00001", "8000", "10000", 1, false), priceRec("MH00002", "38000", "40000", 1, false)}}}
	e.mod.UsePublishedFeedSources(map[string]apiclientport.PublishedSource{feedPrice: src},
		map[string][]apiclientport.Publisher{feedPrice: {{Tenant: kn, Code: "89"}}})
	e.buildFeeds() // nguồn W5.3 thật (để BuildFeeds không lỗi vì thiếu nguồn tập tổng hợp); chưa có bảng giá → chưa dựng giá
	build := func() error { _, err := e.mod.BuildFeeds(ctx); return err }
	if err := build(); err != nil {
		t.Fatalf("dựng: %v", err)
	}
	cid, key := e.onboardPriceFeed()

	if r := e.call(http.MethodGet, "/v1/feeds/"+feedPrice, "", nil); r.status != http.StatusUnauthorized {
		t.Fatalf("không khóa API → 401 (không có API công khai): %d %s", r.status, r.raw)
	}
	if r := e.call(http.MethodGet, "/v1/feeds/"+feedPrice, key, nil); r.status != http.StatusForbidden || r.code() != apiclientport.ErrContractNotServable.Code {
		t.Fatalf("proposed, chưa bật cờ → 403: %d %s", r.status, r.raw)
	}
	d := e.deps
	d.AllowUnapproved = true
	srv := httptest.NewServer(gateway.NewHandler(e.mod, d, nil, e.log))
	t.Cleanup(srv.Close)
	get := func(path, k string) intResp {
		return doReq(t, http.MethodGet, srv.URL+path, map[string]string{apiclientport.KeyHeader: k}, nil)
	}
	env, err := integration.EnvelopeFor(integration.KindFeed)
	if err != nil {
		t.Fatal(err)
	}
	page := func(cursor string) intResp {
		t.Helper()
		p := "/v1/feeds/" + feedPrice
		if cursor != "" {
			p += "?cursor=" + cursor
		}
		r := get(p, key)
		want(t, r, http.StatusOK, "đọc feed giá")
		if env.Validate(r.raw) != nil || r.header.Get(apiclientport.HeaderDataContract) != feedPrice+".v1" || r.header.Get("AG-Contract-Status") != "proposed" {
			t.Fatalf("phong bì/header: %v %s", r.header, r.raw)
		}
		return r
	}
	ops := func(r intResp) map[string]string {
		out := map[string]string{}
		for _, it := range r.body["items"].([]any) {
			m := it.(map[string]any)
			v := m["op"].(string)
			if rec, ok := m["record"].(map[string]any); ok {
				v += fmt.Sprintf("|v%v|%v|%v", rec["board_version"], rec["price_min"], rec["updating"])
			}
			out[m["entity_key"].(string)] = v
		}
		return out
	}

	r := page("")
	if o := ops(r); len(o) != 2 || o["89:2026-10-08:MH00001"] != "upsert|v1|8000|false" || r.body["max_age"] != "PT1H" {
		t.Fatalf("trang đầu: %v %s", o, r.raw)
	}
	ds := r.body["dataset"].(map[string]any)
	if ds["contributors"] != float64(1) || ds["version"] != float64(1) || r.body["snapshot"].(map[string]any)["href"] != "/v1/snapshots/"+feedPrice+"/1" {
		t.Fatalf("dataset/snapshot: %s", r.raw)
	}
	if strings.Contains(string(r.raw), kn.String()) {
		t.Fatalf("trang feed không lộ tenant đơn vị công bố: %s", r.raw)
	}
	next := r.body["next_cursor"].(string)
	first := next // seq 2 — dưới phần sẽ dọn ở cuối kiểm thử
	_, _, otherKey := e.onboard(100)
	if r := get("/v1/feeds/"+feedPrice, otherKey); r.status != http.StatusForbidden || r.code() != apiclientport.ErrForbidden.Code {
		t.Fatalf("thiếu scope feed → 403: %d %s", r.status, r.raw)
	}

	// Mở duyệt: cùng số, updating = true → upsert; duyệt lại v2 bỏ MH00002 → upsert v2 + retract.
	src.recs[kn] = []map[string]any{priceRec("MH00001", "8000", "10000", 1, true), priceRec("MH00002", "38000", "40000", 1, true)}
	if err := build(); err != nil {
		t.Fatal(err)
	}
	r = page(next)
	if o := ops(r); len(o) != 2 || o["89:2026-10-08:MH00001"] != "upsert|v1|8000|true" {
		t.Fatalf("mở duyệt: %v", o)
	}
	next = r.body["next_cursor"].(string)
	src.recs[kn] = []map[string]any{priceRec("MH00001", "8800", "9000", 2, false)}
	if err := build(); err != nil {
		t.Fatal(err)
	}
	r = page(next)
	if o := ops(r); len(o) != 2 || o["89:2026-10-08:MH00001"] != "upsert|v2|8800|false" || o["89:2026-10-08:MH00002"] != "retract" {
		t.Fatalf("duyệt lại v2: %v", o)
	}
	next = r.body["next_cursor"].(string)

	// Bản ghi có trường lạ (vd tên cán bộ) → cả phiên bản bị từ chối; nguồn lỗi → không dựng, không retract.
	bad := priceRec("MH00001", "8800", "9000", 2, false)
	bad["officer_name"] = "Nguyễn Văn A"
	src.recs[kn] = []map[string]any{bad}
	if err := build(); err == nil || !strings.Contains(err.Error(), apiclientport.ErrRecordRejected.Code) {
		t.Fatalf("bản ghi lệch hợp đồng phải bị từ chối: %v", err)
	}
	src.recs[kn], src.err = nil, errors.New("pf/price tạm lỗi")
	if err := build(); err == nil {
		t.Fatal("nguồn lỗi phải trả lỗi dựng")
	}
	src.err = nil
	if r = page(next); len(r.body["items"].([]any)) != 0 || strings.Contains(string(r.raw), "Nguyễn") {
		t.Fatalf("không mục mới sau dựng lỗi: %s", r.raw)
	}

	// Sổ đo lường + điểm kiểm soát xuất (egress_event public_api) cho mỗi trang 2xx.
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event WHERE client_id = $1::uuid AND product = $2::text`, uuid.MustParse(cid), feedPrice); n < 4 {
		t.Fatalf("sổ đo lường: %d", n)
	}
	if n := e.count(e.operator, `SELECT count(*) FROM pf_metering.egress_event WHERE channel = 'public_api' AND counterparty_ref = $1::uuid`, cid); n < 4 {
		t.Fatalf("egress_event public_api: %d", n)
	}

	// Hạn giữ 30 ngày: phần đầu feed đã dọn → cursor cũ 410 integration.cursor_expired kèm ảnh chụp mới nhất.
	admin := e.pg.AdminPool(t, migrations.PF)
	if _, err := admin.Exec(ctx, `UPDATE pf_apiclient.feed_head SET purged_through = 3 WHERE tenant_id = $1::uuid AND feed = $2::text`, e.operator, feedPrice); err != nil {
		t.Fatal(err)
	}
	r = get("/v1/feeds/"+feedPrice+"?cursor="+first, key)
	if r.status != http.StatusGone || r.code() != apiclientport.ErrCursorExpired.Code || !strings.Contains(string(r.raw), "/v1/snapshots/"+feedPrice+"/3") {
		t.Fatalf("cursor quá hạn → 410 kèm snapshot: %d %s", r.status, r.raw)
	}
	if r = page(""); r.body["items"].([]any)[0].(map[string]any)["seq"] != float64(4) {
		t.Fatalf("đọc từ đầu bắt đầu sau phần đã dọn: %s", r.raw)
	}
}
