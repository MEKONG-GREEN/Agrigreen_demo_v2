package adhocintegration_test

import (
	"context"
	"fmt"
	report "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"net/http"
	"sync/atomic"
	"testing"
)

type dashboard189Control struct {
	forms.PeriodControl
	calls atomic.Int64
}

func (c *dashboard189Control) Progress(ctx context.Context, profile string, id uuid.UUID) (forms.PeriodProgress, error) {
	c.calls.Add(1)
	return c.PeriodControl.Progress(ctx, profile, id)
}

func TestDashboard189PagesAndNativeProjectionScope(t *testing.T) {
	var control *dashboard189Control
	f := newReportFxWith(t, func(c forms.PeriodControl) forms.PeriodControl {
		control = &dashboard189Control{PeriodControl: c}
		return control
	})
	// Each of these periods is opened through the real KN/native forms workflow,
	// not fabricated SQL rows or fake native progress. More than one page is active.
	for i := 0; i < 105; i++ {
		f.openAdhoc(fmt.Sprintf("Dashboard phân trang %03d", i))
	}
	week := f.open(41, dCNTS)
	closed := f.open(1, dTT)
	cancelled := f.open(2, dTT)
	f.open(3, dTT)
	code, body := f.khtcMFA(http.MethodPost, "/periods/"+closed.ID+"/close", f.khtcUser, "", map[string]any{"reason": "Đóng kỳ quá hạn để kiểm phạm vi dashboard"})
	f.ok(code, body, nil)
	code, body = f.khtc(http.MethodPost, "/periods/"+cancelled.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ để kiểm phạm vi dashboard"})
	f.ok(code, body, nil)
	read := func(department, role string) report.Dashboard {
		t.Helper()
		var d report.Dashboard
		code, body := f.do(http.MethodGet, "/v1/extension/khtc/dashboard?year=2026&quarter=4&department="+department, role, khtcDept, f.khtcUser, "", nil)
		f.ok(code, body, &d)
		return d
	}
	control.calls.Store(0)
	d := read("", authz.KNTinh)
	if d.Counts.Open != 106 || d.Counts.Forms != 211 || d.Counts.Sent != 0 {
		t.Fatalf("annual pages lost or terminal/outside-quarter included: %+v", d.Counts)
	}
	if control.calls.Load() != 106 {
		t.Fatalf("projection read irrelevant periods: %d", control.calls.Load())
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range d.Status {
		seen[r.PeriodID] = true
	}
	if len(seen) != 106 || !seen[uuid.MustParse(week.ID)] {
		t.Fatalf("cross-page duplicate/missing rows: %d", len(seen))
	}
	control.calls.Store(0)
	d = read(dCNTS, authz.KNTinh)
	if d.Counts.Open != 1 || d.Counts.Forms != 1 || control.calls.Load() != 1 {
		t.Fatalf("department filter applied after paging/projection: %+v calls%d", d.Counts, control.calls.Load())
	}
	d = read(dTTTT, authz.KNTinh)
	if d.Counts.Open != 105 || d.Counts.Forms != 105 {
		t.Fatalf("cross-page department aggregate %+v", d.Counts)
	}
	for _, r := range d.Status {
		if r.Department != dTTTT {
			t.Fatal("department filter leaked", r)
		}
	}
	// National readers keep their real context and do not receive province adhoc records.
	d = read("", authz.KNTW)
	if d.Counts.Open != 1 || d.Counts.Forms != 1 || d.Status[0].PeriodID.String() != week.ID {
		t.Fatalf("restricted adhoc leaked %+v", d.Counts)
	}
	for _, role := range []string{authz.KNXa} {
		code, body = f.do(http.MethodGet, "/v1/extension/khtc/dashboard?year=2026&quarter=4", role, khtcDept, f.khtcUser, "", nil)
		if code != 403 {
			t.Fatalf("unauthorized dashboard %d %s", code, body)
		}
	}
	other := newReportFx(t)
	var isolated report.Dashboard
	code, body = other.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4", other.khtcUser, "", nil)
	other.ok(code, body, &isolated)
	if isolated.Counts.Open != 0 || len(isolated.Status) != 0 {
		t.Fatal("cross-tenant paging leaked")
	}
}

func TestDashboard189AnnualReportStateOutsideQuarter(t *testing.T) {
	var control *dashboard189Control
	f := newReportFxWith(t, func(c forms.PeriodControl) forms.PeriodControl {
		control = &dashboard189Control{PeriodControl: c}
		return control
	})
	var period khtcPeriod
	code, body := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "quarter", "year": 2026, "no": 1, "scope": "quarter", "departments": []string{dTT}})
	f.ok(code, body, &period)
	control.calls.Store(0)
	var d report.Dashboard
	code, body = f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4", f.khtcUser, "", nil)
	f.ok(code, body, &d)
	if len(d.Reports) != 1 || d.Reports[0].ID.String() != period.ID || d.Reports[0].State != "late" || control.calls.Load() != 1 {
		t.Fatalf("annual report state must retain native projection outside selected quarter: %+v calls%d", d.Reports, control.calls.Load())
	}
	if d.Counts.Open != 0 || len(d.Status) != 0 {
		t.Fatal("annual table row polluted current-quarter status")
	}
}
