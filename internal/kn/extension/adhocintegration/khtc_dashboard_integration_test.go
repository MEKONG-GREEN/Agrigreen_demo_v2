package adhocintegration_test

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/google/uuid"
	"net/http"
	"strings"
	"testing"
)

func TestKHTCDashboardLiveMau5(t *testing.T) {
	f := newReportFx(t)
	week := f.open(41, dTT)
	code, body := f.send(week.ID, dTT, "submit", 0, ttSheet(f.item, "25"))
	f.ok(code, body, nil)
	var p khtcPeriod
	code, body = f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "program", "year": 2026, "as_of_date": "2026-10-13", "departments": []string{dTT}, "due_at": "2026-10-20T03:00:00Z", "export_due_at": "2026-10-21T09:00:00Z"})
	f.ok(code, body, &p)
	code, body = f.send(p.ID, dTT, "submit", 0, map[string]any{"texts": map[string]string{"cong_tac_khac": "Tổng hợp tiến độ"}})
	f.ok(code, body, nil)
	read := func() app.Dashboard {
		t.Helper()
		var d app.Dashboard
		code, body = f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4&department="+dTT, f.khtcUser, "", nil)
		f.ok(code, body, &d)
		return d
	}
	d := read()
	if d.ProgressPeriod == nil || d.ProgressPeriod.ID.String() != p.ID || d.Counts.Sent != 2 {
		t.Fatalf("live period and sent counts %+v", d)
	}
	found := false
	for _, r := range d.Progress.Rows {
		if r.Ref.String() == f.item {
			found = true
			if r.YearToDate != "25" {
				t.Fatalf("M5 total %+v", r)
			}
		}
	}
	if !found {
		t.Fatal("missing program item")
	}
	code, body = f.khtc(http.MethodPost, "/periods/"+week.ID+"/forms/"+dTT+"/review", f.khtcUser, "", map[string]any{"decision": "returned", "note": "Bổ sung số liệu tiến độ"})
	f.ok(code, body, nil)
	d = read()
	if d.Counts.Sent != 1 {
		t.Fatalf("returned form counted as sent: %+v", d.Counts)
	}
	code, body = f.send(week.ID, dTT, "submit", 1, ttSheet(f.item, "35"))
	f.ok(code, body, nil)
	d = read()
	for _, r := range d.Progress.Rows {
		if r.Ref.String() == f.item && r.YearToDate != "35" {
			t.Fatalf("dashboard remained stale after submit %+v", r)
		}
	}
	// Readers cannot select arbitrary tenant, and another department cannot read the aggregate.
	code, body = f.do(http.MethodGet, "/v1/extension/khtc/dashboard?year=2026&quarter=4", authz.KNTinh, dTT, uuid.Nil, "", nil)
	if code != http.StatusForbidden {
		t.Fatalf("other department %d %s", code, body)
	}
	for _, q := range []string{"year=1e100000", "year=2026&quarter=1e-100000", "year=2026&quarter=5", "year=0", "year=2026&quarter=0", "year=" + strings.Repeat("9", 100000)} {
		code, body = f.khtc(http.MethodGet, "/dashboard?"+q, f.khtcUser, "", nil)
		if code != 422 {
			t.Fatalf("numeric %s: %d %s", q, code, body)
		}
	}
	other := newReportFx(t)
	var isolated app.Dashboard
	code, body = other.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4", other.khtcUser, "", nil)
	other.ok(code, body, &isolated)
	if isolated.Counts.Open != 0 || isolated.ProgressPeriod != nil || len(isolated.Status) != 0 {
		t.Fatalf("dashboard leaked another tenant: %+v", isolated)
	}
}

func TestKHTCDashboardSourceStates(t *testing.T) {
	f := newKHTCFormFxWith(t, nil)
	p := f.open(41, dTT, dCNTS)
	for _, tc := range []struct {
		name               string
		closed, unreadable bool
		want               string
	}{
		{"closed", true, false, "closed"}, {"open", false, false, "open"}, {"unavailable", false, true, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.cw.closed, f.cw.unreadable = tc.closed, tc.unreadable
			var d app.Dashboard
			code, body := f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4", f.khtcUser, "", nil)
			f.ok(code, body, &d)
			count := 0
			for _, row := range d.Status {
				if row.Kind == "source" {
					count++
					if row.PeriodID.String() != p.ID || row.Department != dCNTS || row.State != tc.want {
						t.Fatalf("source state %+v", row)
					}
				}
			}
			if count != 1 {
				t.Fatalf("expected one source, got %d: %+v", count, d.Status)
			}
			code, body = f.khtc(http.MethodGet, "/dashboard?year=2026&quarter=4&department="+dTT, f.khtcUser, "", nil)
			f.ok(code, body, &d)
			for _, row := range d.Status {
				if row.Kind == "source" {
					t.Fatalf("source leaked department filter: %+v", row)
				}
			}
		})
	}
}
