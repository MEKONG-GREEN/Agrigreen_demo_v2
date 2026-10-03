package kn77_test

import (
	"context"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// recCP là điểm kiểm soát xuất giả: ghi lại mọi lần xuất; deny → báo vượt hạn mức.
type recCP struct {
	mu   sync.Mutex
	got  []meteringport.Egress
	deny bool
}

func (c *recCP) Pass(_ context.Context, e meteringport.Egress) (meteringport.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.got = append(c.got, e)
	return meteringport.Decision{Allowed: !c.deny, Mode: meteringport.ModeEnforce}, nil
}

var linkRe = regexp.MustCompile(`(https?://\S+/v1/public/report-links/[A-Za-z0-9_-]+)`)

// TestA4_ChiCucDownloadLink7Days — quyết định A4 (03/10/2026, FN-TI-02.06): tệp vượt giới hạn đính kèm → email mang
// liên kết do api-platform phục vụ, hạn reports.dispatch_link_ttl_hours (mặc định 168 = 7 ngày) thay URL ký sẵn
// 15 phút; mở liên kết không cần đăng nhập, ghi sổ truy cập (không PII, IP đã băm), qua điểm kiểm soát xuất (export_audit
// + pf/metering report_export); sửa mã → 404; hết hạn → 410; điểm kiểm soát từ chối → không phục vụ; Admin thu hồi → 410.
func TestA4_ChiCucDownloadLink7Days(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	c1 := f.communes89[0]
	c.issue(t, "T01")
	c.xa1.must(submit(c.xa1, str(c.task(t, c.xa1, "T01", c1, c.p1), "id"), "submitted", map[string]string{"F": "12.5"}), http.StatusCreated, "gửi")
	c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusCreated, "cập nhật")
	const addr = "trongtrot.chicuc@angiang.gov.vn"
	to := c.tinh.must(c.tinh.post("/v1/reports/dispatch-recipients", map[string]any{"label": "Chi cục Trồng trọt và BVTV", "address": addr, "kind": "to"}),
		http.StatusCreated, "người nhận")
	rid := str(to.body, "id")
	ex := c.tinh.must(c.tinh.post("/v1/reports/dispatches", map[string]any{"period_id": c.p1, "formats": []string{"xlsx"}}), http.StatusCreated, "xuất file")
	did := str(obj(ex.body, "dispatch"), "id")

	// Giới hạn đính kèm 0 MB → chỉ gửi liên kết; điểm kiểm soát dùng chung ghi nhận lần tải.
	f.repParams.set("reports.dispatch_attachment_limit_mb", "0")
	cp := &recCP{}
	f.rep.UseEgress(cp, meteringport.ModeEnforce)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{}), http.StatusAccepted, "gửi email")
	ev := f.events(repport.EventDispatchRequested)
	if err := tenant.WithTenantTx(context.Background(), f.repPool, f.tenant, func(tx pgx.Tx) error {
		return f.rep.DispatchHandler()(context.Background(), tx, ev[len(ev)-1])
	}); err != nil {
		t.Fatal(err)
	}
	ms := f.mail.all()
	if len(ms) != 1 || len(ms[0].Attachments) != 0 || !strings.Contains(ms[0].Text, "hết hạn") || strings.Contains(ms[0].Text, "files.test") {
		t.Fatalf("email chỉ mang liên kết api-platform (không URL ký sẵn của kho): %+v", ms)
	}
	m := linkRe.FindStringSubmatch(ms[0].Text)
	if m == nil || !strings.HasPrefix(m[1], f.app.URL) {
		t.Fatalf("liên kết tải: %q", ms[0].Text)
	}
	link := m[1]
	if g := c.tinh.must(c.tinh.get("/v1/reports/dispatches/"+did), http.StatusOK, "lần gửi"); obj(g.body, "dispatch")["link_only"] != true {
		t.Fatalf("link_only: %s", g.raw)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch_link WHERE tenant_id = $1 AND ttl_hours = 168
	    AND expires_at = created_at + interval '168 hours'`, f.tenant); n != 1 {
		t.Fatalf("liên kết hạn 7 ngày: %d", n)
	}

	get := func(u string) (int, []byte, http.Header) {
		t.Helper()
		r, err := http.Get(u) // không Authorization: mã thông báo là quyền truy cập
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = r.Body.Close() }()
		b, _ := io.ReadAll(r.Body)
		return r.StatusCode, b, r.Header
	}
	outcomes := func(o string) int {
		return f.count(`SELECT count(*) FROM pf_reports.dispatch_link_access WHERE tenant_id = $1 AND outcome = $2`, f.tenant, o)
	}

	// Mở liên kết → tệp xlsx, sổ "served", export_audit kind download, egress report_export bên nhận = người nhận.
	st, body, hdr := get(link)
	if st != http.StatusOK || !strings.HasPrefix(string(body), "PK") || !strings.Contains(hdr.Get("Content-Disposition"), ".xlsx") ||
		hdr.Get("Cache-Control") != "no-store" {
		t.Fatalf("tải qua liên kết: %d %v", st, hdr)
	}
	if outcomes("served") != 1 || f.count(`SELECT count(*) FROM pf_reports.dispatch_link_access WHERE tenant_id = $1 AND ip_hash ~ '^[0-9a-f]{64}$'
	    AND recipient_ref::text = $2`, f.tenant, rid) != 1 {
		t.Fatal("sổ truy cập: served, IP đã băm, người nhận")
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.export_audit WHERE tenant_id = $1 AND kind = 'download' AND job_id::text = $2`, f.tenant, did); n != 1 {
		t.Fatalf("export_audit tải: %d", n)
	}
	if len(cp.got) != 1 || cp.got[0].Channel != meteringport.ChannelReportExport || cp.got[0].CounterpartyRef.String() != rid || cp.got[0].Internal {
		t.Fatalf("điểm kiểm soát dùng chung: %+v", cp.got)
	}

	// Sửa mã → 404, sổ "invalid" (không băm IP).
	tok := link[strings.LastIndex(link, "/")+1:]
	bad := []byte(tok)
	if bad[70] == 'A' {
		bad[70] = 'B'
	} else {
		bad[70] = 'A'
	}
	if st, _, _ := get(link[:len(link)-len(tok)] + string(bad)); st != http.StatusNotFound {
		t.Fatalf("mã bị sửa: %d", st)
	}
	if st, _, _ := get(link[:len(link)-len(tok)] + "khong-phai-ma"); st != http.StatusNotFound {
		t.Fatalf("mã sai định dạng: %d", st)
	}
	if outcomes("invalid") != 1 {
		t.Fatal("sổ invalid")
	}

	// Điểm kiểm soát (enforce) từ chối → không phục vụ, sổ "egress_denied".
	cp.deny = true
	if st, _, _ := get(link); st != http.StatusTooManyRequests {
		t.Fatalf("điểm kiểm soát từ chối: %d", st)
	}
	cp.deny = false
	if outcomes("egress_denied") != 1 {
		t.Fatal("sổ egress_denied")
	}

	// Hết hạn (7 ngày) → 410 reports.link_expired.
	f.clk.Advance(168*time.Hour + time.Second)
	st, body, _ = get(link)
	if st != http.StatusGone || !strings.Contains(string(body), "reports.link_expired") || outcomes("expired") != 1 {
		t.Fatalf("hết hạn: %d %s", st, body)
	}
	f.clk.Advance(-(168*time.Hour + time.Second))

	// Admin thu hồi (lý do ≥ 10 ký tự) → 410 reports.link_revoked; dispatch_event links_revoked.
	adm := f.adminCaller()
	adm.fail(adm.adminPost("/v1/reports/dispatches/"+did+"/revoke-links", map[string]any{"reason": "ngắn"}), http.StatusUnprocessableEntity, "validation_failed",
		"lý do ngắn")
	c.tinh.fail(c.tinh.do(f.admin, http.MethodPost, "/v1/reports/dispatches/"+did+"/revoke-links", key(), map[string]any{"reason": "Gửi nhầm người nhận"}),
		http.StatusForbidden, "forbidden", "cán bộ tỉnh không thu hồi")
	rv := adm.must(adm.adminPost("/v1/reports/dispatches/"+did+"/revoke-links", map[string]any{"reason": "Gửi nhầm người nhận Chi cục"}), http.StatusOK, "thu hồi")
	if num(rv.body, "revoked") != 1 {
		t.Fatalf("thu hồi: %s", rv.raw)
	}
	st, body, _ = get(link)
	if st != http.StatusGone || !strings.Contains(string(body), "reports.link_revoked") || outcomes("revoked") != 1 {
		t.Fatalf("đã thu hồi: %d %s", st, body)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch_event WHERE tenant_id = $1 AND action = 'links_revoked'`, f.tenant); n != 1 {
		t.Fatalf("sổ lần gửi: %d", n)
	}
	lg := adm.must(adm.adminGet("/v1/reports/dispatches/"+did+"/link-access"), http.StatusOK, "sổ truy cập")
	if len(arr(lg.body, "items")) != 5 || strings.Contains(string(lg.raw), addr) || strings.Contains(string(lg.raw), "127.0.0.1") {
		t.Fatalf("sổ truy cập (5 dòng, không PII/IP thô): %s", lg.raw)
	}
	// Sổ truy cập là sổ chỉ thêm.
	if _, err := f.env.AdminPool(t, migrations.PF).Exec(context.Background(), `UPDATE pf_reports.dispatch_link_access SET outcome = 'served'`); err == nil {
		t.Fatal("sổ truy cập phải chỉ thêm")
	}
}
