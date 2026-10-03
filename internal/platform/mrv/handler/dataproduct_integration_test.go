package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mrv/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type fakeCheckpoint struct {
	mu    sync.Mutex
	allow bool
	err   error
	seen  []meteringport.Egress
}

func (c *fakeCheckpoint) Pass(_ context.Context, e meteringport.Egress) (meteringport.Decision, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seen = append(c.seen, e)
	if c.err != nil {
		return meteringport.Decision{}, c.err
	}
	return meteringport.Decision{Allowed: c.allow}, nil
}

func (c *fakeCheckpoint) calls() []meteringport.Egress {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]meteringport.Egress(nil), c.seen...)
}

func pcode(err error) string {
	var p *problem.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	return ""
}

// G5 D-03 (mrv_package_status v1; KN US-MRV-05 — đơn vị thẩm định chỉ xem): nguồn pf/mrv trả
// trạng thái, băm gói, băm ảnh chụp, số bằng chứng, kiểm lại; không người yêu cầu, khóa đối tượng,
// toạ độ; khớp hợp đồng; tenant khác → rỗng / 404. ADR-0024 (L-03): yêu cầu xuất và cấp URL tải gói
// đi qua Guard kênh mrv_package; observe không từ chối; enforce + từ chối → 429
// metering.egress_quota_exceeded, không tạo job / không cấp URL; nil → hành vi cũ
// (TestExportEndToEnd không đổi).
func TestMRVPackageProductAndEgress_G5(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.approveSeed(f.tenant)
	col := f.as(f.tenant, f.p89, port.RoleCollector)
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", awd("30001")), 201, "")
	expect(t, col.app(http.MethodPost, "/v1/mrv/evidence", awd("30002")), 201, "")
	cp := &fakeCheckpoint{allow: false}
	f.mod.UseEgress(cp, meteringport.ModeObserve, nil)
	exp := f.as(f.tenant, f.p89, port.RoleExporter)

	body := map[string]any{"filters": map[string]any{"evidence_type": "awd_log", "season_code": "2026-DX"}}
	j := exp.app(http.MethodPost, "/v1/mrv/exports", body)
	expect(t, j, 202, "")
	id := j.str("id")
	calls := cp.calls()
	if len(calls) != 1 {
		t.Fatalf("yêu cầu xuất: một Egress: %+v", calls)
	}
	if e := calls[0]; e.Channel != meteringport.ChannelMRVPackage || e.SourceID.String() != id || e.DataTenantID != f.tenant ||
		e.CounterpartyKind != meteringport.CounterpartyActor || e.CounterpartyRef != exp.actor || e.Records != 2 {
		t.Fatalf("Egress yêu cầu xuất (số bằng chứng ước = 2): %+v", e)
	}
	if _, err := inbox.Process(ctx, f.pool, port.ExportConsumer, f.requestedEvent(f.tenant, id), f.mod.ExportHandler()); err != nil {
		t.Fatal(err)
	}
	expect(t, exp.app(http.MethodGet, "/v1/mrv/exports/"+id+"/package", nil), 200, "")
	calls = cp.calls()
	if len(calls) != 2 || calls[1].Channel != meteringport.ChannelMRVPackage || calls[1].Records != 2 || calls[1].Bytes <= 0 ||
		calls[1].SourceID.String() == id {
		t.Fatalf("cấp URL tải gói: Egress mới, byte = cỡ gói: %+v", calls)
	}

	src := f.mod.PackageSource()
	p, err := dataproduct.Get(apiclientport.ProductMRVPackageStatus)
	if err != nil {
		t.Fatal(err)
	}
	check := func(it apiclientport.MRVPackageStatusV1) {
		t.Helper()
		raw, _ := json.Marshal(it)
		if err := p.Validate(raw); err != nil {
			t.Fatalf("ngoài hợp đồng: %v %s", err, raw)
		}
		for _, banned := range []string{exp.actor.String(), "packages/", "gps", "lat", "household", "url"} {
			if strings.Contains(string(raw), banned) {
				t.Fatalf("phần tử không được có %q: %s", banned, raw)
			}
		}
	}
	list, next, err := src.Packages(ctx, f.tenant, "", "", 0)
	if err != nil || len(list) != 1 || next != "" {
		t.Fatalf("danh sách gói: %+v %q %v", list, next, err)
	}
	check(list[0])
	if it := list[0]; it.ExportID != id || it.Status != "completed" || it.EvidenceCount != 2 || it.PackageSHA256 == nil ||
		it.SnapshotSHA256 == nil || it.CompletedAt == nil || it.LastVerifyMatch != nil || it.SeasonCode == nil || *it.SeasonCode != "2026-DX" {
		t.Fatalf("phần tử danh sách: %+v", it)
	}
	one, err := src.Package(ctx, f.tenant, uuid.MustParse(id))
	if err != nil || one.LastVerifyMatch == nil || !*one.LastVerifyMatch || *one.PackageSHA256 != *list[0].PackageSHA256 {
		t.Fatalf("một gói + kiểm lại: %+v %v", one, err)
	}
	check(one)
	// Cách ly tenant + lỗi.
	if l, _, err := src.Packages(ctx, ids.New(), "", "", 0); err != nil || len(l) != 0 {
		t.Fatalf("tenant khác: %+v %v", l, err)
	}
	if _, err := src.Package(ctx, ids.New(), uuid.MustParse(id)); pcode(err) != "mrv.export_not_found" {
		t.Fatalf("gói của tenant khác → 404: %v", err)
	}
	if _, err := src.Package(ctx, f.tenant, ids.New()); pcode(err) != "mrv.export_not_found" {
		t.Fatalf("gói không có → 404: %v", err)
	}
	if _, _, err := src.Packages(ctx, f.tenant, "deleted", "", 0); err == nil {
		t.Fatal("trạng thái lạ phải lỗi")
	}
	if l, _, err := src.Packages(ctx, f.tenant, "queued", "", 0); err != nil || len(l) != 0 {
		t.Fatalf("lọc queued: %+v %v", l, err)
	}

	// Gói thứ hai (queued) → phân trang con trỏ.
	expect(t, exp.app(http.MethodPost, "/v1/mrv/exports", map[string]any{"filters": map[string]any{}}), 202, "")
	p1, n1, err := src.Packages(ctx, f.tenant, "", "", 1)
	if err != nil || len(p1) != 1 || n1 == "" || p1[0].ExportID != id {
		t.Fatalf("trang 1: %+v %q %v", p1, n1, err)
	}
	p2, n2, err := src.Packages(ctx, f.tenant, "", n1, 1)
	if err != nil || len(p2) != 1 || n2 != "" || p2[0].Status != "queued" || p2[0].PackageSHA256 != nil {
		t.Fatalf("trang 2: %+v %q %v", p2, n2, err)
	}
	check(p2[0])

	// enforce + từ chối → 429, không tạo job, không cấp URL.
	f.mod.UseEgress(cp, meteringport.ModeEnforce, nil)
	jobs := f.count(f.tenant, "export_job")
	expect(t, exp.app(http.MethodPost, "/v1/mrv/exports", map[string]any{"filters": map[string]any{}}), 429, "metering.egress_quota_exceeded")
	if f.count(f.tenant, "export_job") != jobs {
		t.Fatal("enforce từ chối → không tạo job")
	}
	expect(t, exp.app(http.MethodGet, "/v1/mrv/exports/"+id+"/package", nil), 429, "metering.egress_quota_exceeded")
	cp.err = errors.New("metering down")
	expect(t, exp.app(http.MethodGet, "/v1/mrv/exports/"+id+"/package", nil), 503, "metering.egress_unavailable")
	cp.err = nil

	// Van mrv_collection_evidence (L-05): enforce + từ chối → lỗi, không ghi; observe → ghi.
	s := f.openSeason(f.tenant, "30001", time.Now().Add(-48*time.Hour))
	env := envelope(f.mkg, port.EventMKGCollectionEvid, collection(s, ids.New(), 1, "2000-5000"))
	before := f.count(f.tenant, "collection_evidence")
	if _, err := f.valve(env); pcode(err) != "metering.egress_quota_exceeded" {
		t.Fatalf("van enforce + từ chối: %v", err)
	}
	if f.count(f.tenant, "collection_evidence") != before {
		t.Fatal("van bị chặn không được ghi")
	}
	f.mod.UseEgress(cp, meteringport.ModeObserve, nil)
	if _, err := f.valve(env); err != nil {
		t.Fatalf("van observe: %v", err)
	}
	last := cp.calls()[len(cp.calls())-1]
	if last.Channel != meteringport.ChannelValve || last.SourceID != env.EventID || last.DataTenantID != f.mkg || last.CounterpartyRef != f.tenant ||
		last.Product != port.ContractCollectionEvidence || f.count(f.tenant, "collection_evidence") != before+1 {
		t.Fatalf("Egress van MRV: %+v", last)
	}
	// nil → hành vi cũ.
	f.mod.UseEgress(nil, meteringport.ModeEnforce, nil)
	n := len(cp.calls())
	expect(t, exp.app(http.MethodPost, "/v1/mrv/exports", map[string]any{"filters": map[string]any{}}), 202, "")
	if len(cp.calls()) != n {
		t.Fatal("chưa nối checkpoint → không gọi")
	}
}
