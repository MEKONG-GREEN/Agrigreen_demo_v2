package handler_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type fakeCheckpoint struct {
	mu    sync.Mutex
	allow bool
	err   error
	seen  []meter.Egress
}

func (c *fakeCheckpoint) Pass(_ context.Context, e meter.Egress) (meter.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, e)
	if c.err != nil {
		return meter.Decision{}, c.err
	}
	return meter.Decision{Allowed: c.allow}, nil
}

func (c *fakeCheckpoint) calls() []meter.Egress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]meter.Egress(nil), c.seen...)
}

func (f *fixture) audits(kind string) int {
	f.t.Helper()
	var n int
	if err := f.env.AdminPool(f.t, migrations.PF).QueryRow(context.Background(),
		"SELECT count(*) FROM pf_reports.export_audit WHERE tenant_id = $1 AND kind = $2", f.tenant, kind).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// ADR-0024 mục 3 (L-04): lối yêu cầu xuất và lối tải đi qua điểm kiểm soát DÙNG CHUNG kênh
// report_export NGAY SAU điểm kiểm soát cục bộ (G1, giữ nguyên): một Egress mỗi lần xuất (tenant dữ
// liệu = tenant người xuất, bên nhận = người xuất); observe không từ chối, kể cả khi checkpoint lỗi;
// enforce + từ chối → 429 metering.egress_quota_exceeded, không có dòng export_audit; enforce +
// checkpoint lỗi → 503 metering.egress_unavailable; hạn mức người/ngày cục bộ vẫn là thẩm quyền
// trước (reports.export_quota_exceeded, checkpoint chung không được gọi); nil → hành vi cũ
// (TestQuotaAndAnomaly không đổi).
func TestReportExportEgress_ADR0024(t *testing.T) {
	f := newFixture(t, false)
	cp := &fakeCheckpoint{allow: false}
	f.mod.UseEgress(cp, meter.ModeObserve)
	u := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	body := map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx"}
	id := str(u.must(u.post("/v1/reports/exports", body), http.StatusAccepted, "observe + vượt hạn mức kênh vẫn xuất").body, "id")
	calls := cp.calls()
	if len(calls) != 1 {
		t.Fatalf("một Egress: %+v", calls)
	}
	if e := calls[0]; e.Channel != meter.ChannelReportExport || e.DataTenantID != f.tenant || e.CounterpartyKind != meter.CounterpartyActor ||
		e.CounterpartyRef != u.actor || e.ActorRef != u.actor || e.Product != "kn.sowing_progress" || e.Records != 1 || e.SourceID == uuid.Nil {
		t.Fatalf("Egress yêu cầu xuất: %+v", e)
	}
	f.runWorker()
	cp.err = errors.New("metering down")
	u.must(u.get("/v1/reports/exports/"+id+"/download"), http.StatusOK, "observe + checkpoint lỗi vẫn tải")
	cp.err = nil
	if len(cp.calls()) != 2 || f.audits("download") != 1 {
		t.Fatalf("lối tải qua checkpoint chung: %d, nhật ký tải %d", len(cp.calls()), f.audits("download"))
	}

	f.mod.UseEgress(cp, meter.ModeEnforce)
	exports := f.audits("export")
	if r := u.post("/v1/reports/exports", body); r.status != http.StatusTooManyRequests || code(r) != "metering.egress_quota_exceeded" {
		t.Fatalf("enforce + từ chối: %d %v", r.status, r.body)
	}
	if f.audits("export") != exports {
		t.Fatal("enforce từ chối → không có dòng export_audit")
	}
	if r := u.get("/v1/reports/exports/" + id + "/download"); r.status != http.StatusTooManyRequests || code(r) != "metering.egress_quota_exceeded" {
		t.Fatalf("enforce + từ chối lối tải: %d %v", r.status, r.body)
	}
	cp.err = errors.New("metering down")
	if r := u.post("/v1/reports/exports", body); r.status != http.StatusServiceUnavailable || code(r) != "metering.egress_unavailable" {
		t.Fatalf("enforce + checkpoint lỗi: %d %v", r.status, r.body)
	}
	cp.err = nil

	// Hạn mức cục bộ (người/ngày) chặn trước, mã cũ, checkpoint chung không được gọi.
	cp.allow = true
	f.params.set(port.ParamDailyExportQuota, "1")
	n := len(cp.calls())
	if r := u.post("/v1/reports/exports", body); r.status != http.StatusTooManyRequests || code(r) != "reports.export_quota_exceeded" {
		t.Fatalf("hạn mức cục bộ giữ nguyên: %d %v", r.status, r.body)
	}
	if len(cp.calls()) != n {
		t.Fatal("bị hạn mức cục bộ từ chối → không qua checkpoint chung")
	}
	o := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	o.must(o.post("/v1/reports/exports", body), http.StatusAccepted, "enforce + trong hạn mức")

	f.mod.UseEgress(nil, meter.ModeEnforce)
	f.params.set(port.ParamDailyExportQuota, "50")
	n = len(cp.calls())
	o.must(o.post("/v1/reports/exports", body), http.StatusAccepted, "chưa nối checkpoint")
	if len(cp.calls()) != n {
		t.Fatal("nil → hành vi cũ")
	}
}

// ADR-0024 mục 3 (L-04): lối "lịch" (US-DASH-03) qua checkpoint chung — bên nhận = người lập lịch;
// enforce + từ chối → bỏ mốc (không tạo việc xuất), không lỗi job.
func TestScheduledExportEgress_ADR0024(t *testing.T) {
	f := newFixture(t, true)
	cp := &fakeCheckpoint{allow: false}
	f.mod.UseEgress(cp, meter.ModeEnforce)
	mgr := f.as(f.org.pAG, extport.RoleKNProvinceAdmin)
	r1 := ids.New()
	f.dir.emails[r1] = "can.bo@ttkn-angiang.test"
	f.clk.Advance(-time.Hour)
	mgr.must(mgr.adminPost("/v1/reports/schedules", map[string]any{"template_key": "kn.sowing_progress", "format": "xlsx",
		"frequency": "weekly", "weekday": 1, "hour": 10, "recipients": []string{r1.String()}}), http.StatusCreated, "lập lịch")
	job := f.mod.Jobs(func(context.Context) ([]uuid.UUID, error) { return []uuid.UUID{f.tenant}, nil }, nil)[0]
	f.clk.Advance(2 * time.Hour)
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("enforce từ chối → bỏ mốc, job không lỗi: %v", err)
	}
	if n := len(f.events(port.EventExportRequested)); n != 0 {
		t.Fatalf("mốc bị từ chối không tạo việc xuất: %d", n)
	}
	calls := cp.calls()
	if len(calls) != 1 || calls[0].Channel != meter.ChannelReportExport || calls[0].CounterpartyRef != mgr.actor {
		t.Fatalf("Egress lối lịch: %+v", calls)
	}
	// Mốc tuần sau, trong hạn mức → tạo việc xuất.
	cp.allow = true
	f.clk.Advance(7 * 24 * time.Hour)
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(f.events(port.EventExportRequested)); n != 1 {
		t.Fatalf("mốc trong hạn mức: %d", n)
	}
}
