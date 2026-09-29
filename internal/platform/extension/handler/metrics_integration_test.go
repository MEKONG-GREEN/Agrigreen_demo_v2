package handler_test

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	metricsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

type asOfAt interface {
	AsOfAt(ctx context.Context, key metricsport.Key, d clock.Date, cutoff time.Time, sc metricsport.Scope) (metricsport.Value, error)
}

func (f *fixture) dbNow() time.Time {
	f.t.Helper()
	var t time.Time
	if err := f.pool.QueryRow(context.Background(), "SELECT clock_timestamp()").Scan(&t); err != nil {
		f.t.Fatal(err)
	}
	return t
}

func (f *fixture) metric(src asOfAt, key metricsport.Key, cutoff time.Time, units ...string) string {
	f.t.Helper()
	v, err := src.AsOfAt(context.Background(), key, clock.BusinessDate(t0), cutoff, metricsport.Scope{TenantID: f.tenant, Units: units})
	if err != nil {
		f.t.Fatalf("%s: %v", key, err)
	}
	return v.Value.String()
}

// KN US-DASH-01/02 (pf/metrics): giá trị ngày D dựng từ SỔ, chỉ dòng có recorded_at <
// cutoff. Phiên bản báo cáo / nhiệm vụ ghi nhận sau cutoff không đổi giá trị đã chốt.
func TestKNDash01MetricsAsOfExcludesLateRecords(t *testing.T) {
	f := newFixture(t)
	p := f.seedPeople()
	src := f.mod.MetricsSource().(asOfAt)
	before := f.dbNow()
	p.off30001a.must(p.off30001a.post("/v1/extension/field-reports", sowing("30001", f.org.c1.String(), "10")), http.StatusCreated, "báo cáo")
	p.off30001a.createTask("30001", nil)
	cut := f.dbNow()
	// Gửi lại cùng khóa (phiên bản 2) và thêm nhiệm vụ SAU cutoff — ghi nhận muộn.
	p.off30001a.must(p.off30001a.post("/v1/extension/field-reports", sowing("30001", f.org.c1.String(), "12")), http.StatusCreated, "gửi lại")
	p.off30001a.createTask("30001", nil)
	late := f.dbNow().Add(time.Second)

	for _, c := range []struct {
		key     metricsport.Key
		at      time.Time
		want    string
		comment string
	}{
		{"kn.sown_area_ha", before, "0", "trước mọi bản ghi"},
		{"kn.sown_area_ha", cut, "10", "phiên bản 2 ghi nhận muộn bị loại"},
		{"kn.sown_area_ha", late, "12", "sau khi ghi nhận phiên bản 2"},
		{"kn.cooperatives_reported", cut, "1", "một HTX"},
		{"kn.open_tasks", cut, "1", "nhiệm vụ thứ hai ghi nhận muộn bị loại"},
		{"kn.open_tasks", late, "2", ""},
	} {
		if got := f.metric(src, c.key, c.at); got != c.want {
			t.Fatalf("%s (%s): %s, mong %s", c.key, c.comment, got, c.want)
		}
	}
	// Phạm vi đơn vị: xã khác → 0; tenant khác → 0 (RLS).
	if got := f.metric(src, "kn.sown_area_ha", late, "31001"); got != "0" {
		t.Fatalf("xã 31001: %s", got)
	}
	v, err := src.AsOfAt(context.Background(), "kn.sown_area_ha", clock.BusinessDate(t0), late, metricsport.Scope{TenantID: ids.New()})
	if err != nil || !v.Value.IsZero() {
		t.Fatalf("tenant khác: %v %v", v.Value, err)
	}
	if v, err := src.AsOfAt(context.Background(), "kn.directive_ack_rate_pct", clock.BusinessDate(t0), late, metricsport.Scope{TenantID: f.tenant}); err != nil || !v.Value.IsZero() {
		t.Fatalf("tỷ lệ xác nhận khi chưa có chỉ đạo: %v %v", v.Value, err)
	}
	// Ngày chưa kết thúc (theo đồng hồ module) → ErrNotClosed; khóa lạ → ErrUnknownMetric.
	if _, err := f.mod.MetricsSource().AsOf(context.Background(), "kn.open_tasks", clock.BusinessDate(t0), metricsport.Scope{TenantID: f.tenant}); !errors.Is(err, metricsport.ErrNotClosed) {
		t.Fatalf("mong ErrNotClosed: %v", err)
	}
	if _, err := src.AsOfAt(context.Background(), "kn.nope", clock.BusinessDate(t0), late, metricsport.Scope{TenantID: f.tenant}); !errors.Is(err, metricsport.ErrUnknownMetric) {
		t.Fatalf("mong ErrUnknownMetric: %v", err)
	}
	// ScopeLister: toàn tenant + tỉnh có dữ liệu.
	scopes, err := f.mod.MetricsSource().(metricsport.ScopeLister).Scopes(context.Background(), "kn.open_tasks", clock.BusinessDate(time.Now()), f.tenant)
	if err != nil || len(scopes) < 2 || len(scopes[0].Units) != 0 {
		t.Fatalf("scopes: %v %v", scopes, err)
	}
}
