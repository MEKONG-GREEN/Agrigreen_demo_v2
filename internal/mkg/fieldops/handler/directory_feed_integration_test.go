package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/adapter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// gatewayStub là cổng tích hợp giả phục vụ GET /v1/feeds/{feed} đúng phong bì feed_page.v1 từ cùng dữ liệu đã seed vào
// FakeDirectory (bề mặt cũ) — để so hai đường trên CÙNG dữ liệu. Cursor = "s<seq>"; status ≠ 0 → trả lỗi đó.
type gatewayStub struct {
	mu     sync.Mutex
	items  map[string][]map[string]any
	asOf   time.Time
	key    string
	status int
	code   string
	pages  int
}

func (g *gatewayStub) add(feed string, ref uuid.UUID, rec map[string]any) {
	g.mu.Lock()
	defer g.mu.Unlock()
	op := "upsert"
	if rec == nil {
		op = "retract"
	}
	seq := len(g.items[feed]) + 1
	g.items[feed] = append(g.items[feed], map[string]any{"seq": seq, "event_id": ids.New().String(), "entity_key": ref.String(), "op": op,
		"recorded_at": g.asOf.Format(time.RFC3339Nano), "record": rec})
}

func (g *gatewayStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.Header.Get("X-API-Key") != g.key {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if g.status != 0 {
		w.WriteHeader(g.status)
		_, _ = fmt.Fprintf(w, `{"code":%q}`, g.code)
		return
	}
	feed := strings.TrimPrefix(r.URL.Path, "/v1/feeds/")
	after := 0
	if c := r.URL.Query().Get("cursor"); c != "" {
		after, _ = strconv.Atoi(strings.TrimPrefix(c, "s"))
	}
	all := g.items[feed]
	end := min(after+g.pages, len(all))
	page := all[after:end]
	var cur any
	if c := r.URL.Query().Get("cursor"); c != "" {
		cur = c
	}
	maxAge := "PT6H"
	_ = json.NewEncoder(w).Encode(map[string]any{"feed": feed, "contract_version": 1, "cursor": cur, "next_cursor": fmt.Sprintf("s%d", end),
		"has_more": end < len(all), "as_of": g.asOf.Format(time.RFC3339Nano), "max_age": maxAge, "dataset": nil, "snapshot": nil, "items": page})
}

func coopRecord(ref uuid.UUID, commune string, active bool) map[string]any {
	return map[string]any{"cooperative_ref": ref.String(), "code": "HTX-" + ref.String()[:4], "name": "HTX thử", "commune_code": commune,
		"active": active, "lon": nil, "lat": nil, "updated_at": "2026-10-08T00:00:00Z"}
}

func plotRecord(ref uuid.UUID, coop *uuid.UUID, commune, status string) map[string]any {
	var c any
	if coop != nil {
		c = coop.String()
	}
	return map[string]any{"plot_ref": ref.String(), "code": "T-" + ref.String()[:4], "commune_code": commune, "cooperative_ref": c,
		"status": status, "area_ha": "1.5000", "revision": 1, "updated_at": "2026-10-08T00:00:00Z"}
}

func (f *fixture) countFeed(q string) int {
	f.t.Helper()
	var n int
	f.inTenant(func(tx pgx.Tx) error { return tx.QueryRow(context.Background(), q).Scan(&n) })
	return n
}

// W5.5 (ADR-0033 §2, §5; R-INT-02): bộ điều hợp feed danh bạ chạy song song với /v1/partner/* trên CÙNG dữ liệu:
//   - kéo hai feed qua HTTP (khóa API ở header) vào bảng mkg 000036, cursor + as_of + max_age lưu;
//   - đối soát với bản sao cũ: khớp hoàn toàn (parity); lệch → khác biệt có loại + trường;
//   - cờ nguồn "feed": lập lệnh gom cho cùng kết quả như nguồn cũ (chấp nhận, thửa HTX khác, HTX lạ) mà không gọi nền
//     tảng; bản sao feed quá max_age → directory_stale;
//   - thu hồi cấp quyền (403 party.share_grant_missing) → xóa bản sao feed, lệnh gom bị từ chối như bề mặt cũ.
func TestDirectoryFeedParity_W55(t *testing.T) {
	f := newFixture(t, nil)
	sup := f.supervisor()
	ctx := context.Background()

	// Dữ liệu chung: HTX chính (2 thửa của fixture) + HTX thứ hai có 1 thửa + 1 thửa nháp.
	other, otherPlot, draft := ids.New(), ids.New(), ids.New()
	f.dir.AddHTX(other, "31118")
	f.dir.AddPlot(otherPlot, other, "31118")
	gw := &gatewayStub{items: map[string][]map[string]any{}, asOf: f.clk.Now(), key: "agk_mkg_thu", pages: 2}
	gw.add(port.FeedCooperativeDirectory, f.htx, coopRecord(f.htx, "31117", true))
	gw.add(port.FeedCooperativeDirectory, other, coopRecord(other, "31118", true))
	for _, p := range f.plots {
		h := f.htx
		gw.add(port.FeedPlotDirectory, p, plotRecord(p, &h, "31117", "active"))
	}
	gw.add(port.FeedPlotDirectory, otherPlot, plotRecord(otherPlot, &other, "31118", "active"))
	gw.add(port.FeedPlotDirectory, draft, plotRecord(draft, nil, "31117", "draft"))
	srv := httptest.NewServer(gw)
	t.Cleanup(srv.Close)
	f.svc.DirectoryFeed = adapter.NewGatewayFeed(adapter.GatewayConfig{BaseURL: srv.URL, APIKey: gw.key})

	// Đường cũ: lập lệnh (tra /v1/partner/* giả) + lệnh tham chiếu thửa của HTX khác → bản sao cũ có 2 HTX, 3 thửa.
	mustWarnings(t, sup.post("/v1/fieldops/orders", f.planBody()), "nguồn cũ: lập")
	bad := f.planBody()
	bad["plot_refs"] = []string{f.plots[0].String(), otherPlot.String()}
	mustCode(t, sup.post("/v1/fieldops/orders", bad), http.StatusUnprocessableEntity, "fieldops.plot_other_htx", "nguồn cũ: thửa HTX khác")
	stranger := f.planBody()
	stranger["htx_ref"] = ids.New().String()
	mustCode(t, sup.post("/v1/fieldops/orders", stranger), http.StatusUnprocessableEntity, "fieldops.htx_not_found", "nguồn cũ: HTX lạ")
	if _, err := f.svc.RefreshDirectory(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}

	// Feed: kéo qua nhiều trang, cursor + as_of + max_age lưu.
	rs, err := f.svc.SyncDirectoryFeeds(ctx, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rs {
		if !r.CaughtUp || r.Stale || r.Revoked {
			t.Fatalf("kéo feed: %+v", r)
		}
	}
	if n := f.countFeed(`SELECT count(*) FROM mkg_fieldops.directory_feed_record`); n != 6 {
		t.Fatalf("2 HTX + 4 thửa trong bản sao feed: %d", n)
	}
	if n := f.countFeed(`SELECT count(*) FROM mkg_fieldops.directory_feed_state WHERE caught_up AND max_age_seconds = 21600
	    AND feed_cursor IS NOT NULL AND as_of IS NOT NULL AND revoked_at IS NULL`); n != 2 {
		t.Fatalf("trạng thái hai feed: %d", n)
	}

	// Parity: mọi mục bản sao cũ khớp feed.
	cmp, err := f.svc.CompareDirectorySources(ctx, f.tenant)
	if err != nil {
		t.Fatal(err)
	}
	if cmp.Skipped || cmp.Compared < 5 || cmp.Matched != cmp.Compared || len(cmp.Diffs) != 0 {
		t.Fatalf("đối soát hai nguồn phải khớp: %+v", cmp)
	}

	// Nguồn "feed": cùng kết quả như nguồn cũ, KHÔNG gọi nền tảng.
	f.svc.DirectorySource = port.DirectorySourceFeed
	f.dir.Down = true
	r := sup.post("/v1/fieldops/orders", f.planBody())
	mustWarnings(t, r, "nguồn feed: lập")
	if r.body["directory_as_of"] == nil {
		t.Fatalf("directory_as_of = as_of của feed: %s", r.raw)
	}
	mustCode(t, sup.post("/v1/fieldops/orders", bad), http.StatusUnprocessableEntity, "fieldops.plot_other_htx", "nguồn feed: thửa HTX khác")
	mustCode(t, sup.post("/v1/fieldops/orders", stranger), http.StatusUnprocessableEntity, "fieldops.htx_not_found", "nguồn feed: HTX lạ")

	// Thửa đổi HTX trên nền tảng → feed upsert → đối soát thấy bản sao cũ lệch (chưa làm mới) với trường cooperative_ref.
	h := other
	gw.add(port.FeedPlotDirectory, f.plots[1], plotRecord(f.plots[1], &h, "31118", "active"))
	if _, err := f.svc.SyncDirectoryFeeds(ctx, f.tenant); err != nil {
		t.Fatal(err)
	}
	if cmp, err = f.svc.CompareDirectorySources(ctx, f.tenant); err != nil || cmp.Mismatch != 1 || len(cmp.Diffs) != 1 ||
		cmp.Diffs[0].Ref != f.plots[1] || cmp.Diffs[0].Field != "cooperative_ref" {
		t.Fatalf("đối soát thấy lệch: %+v %v", cmp, err)
	}
	mustCode(t, sup.post("/v1/fieldops/orders", f.planBody()), http.StatusUnprocessableEntity, "fieldops.plot_other_htx", "nguồn feed: sau upsert")

	// Quá max_age (PT6H) → lập theo bản sao feed, gắn cờ dữ liệu cũ.
	ok := f.planBody()
	ok["plot_refs"] = []string{f.plots[0].String()}
	f.clk.Advance(6*time.Hour + time.Minute)
	mustWarnings(t, sup.post("/v1/fieldops/orders", ok), "nguồn feed: quá max_age", "fieldops.directory_stale")

	// Thu hồi cấp quyền → feed dừng: bản sao feed bị xóa, lệnh gom bị từ chối như bề mặt cũ (403 → không có).
	gw.mu.Lock()
	gw.status, gw.code = http.StatusForbidden, "party.share_grant_missing"
	gw.mu.Unlock()
	rs, err = f.svc.SyncDirectoryFeeds(ctx, f.tenant)
	if err != nil || len(rs) != 2 || !rs[0].Revoked || !rs[1].Revoked {
		t.Fatalf("thu hồi: %+v %v", rs, err)
	}
	if n := f.countFeed(`SELECT count(*) FROM mkg_fieldops.directory_feed_record`); n != 0 {
		t.Fatalf("thu hồi → xóa bản sao feed: %d", n)
	}
	if n := f.countFeed(`SELECT count(*) FROM mkg_fieldops.directory_feed_state WHERE revoked_at IS NOT NULL AND NOT caught_up AND feed_cursor IS NULL`); n != 2 {
		t.Fatalf("trạng thái thu hồi: %d", n)
	}
	mustCode(t, sup.post("/v1/fieldops/orders", ok), http.StatusUnprocessableEntity, "fieldops.htx_not_found", "nguồn feed: đã thu hồi")
	if cmp, err = f.svc.CompareDirectorySources(ctx, f.tenant); err != nil || !cmp.Skipped {
		t.Fatalf("thu hồi → không đối soát: %+v %v", cmp, err)
	}
}
