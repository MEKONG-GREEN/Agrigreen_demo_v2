package adhocintegration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type countControl230 struct {
	forms.PeriodControl
	calls atomic.Int64
}

func (c *countControl230) Progress(ctx context.Context, profile string, id uuid.UUID) (forms.PeriodProgress, error) {
	c.calls.Add(1)
	return c.PeriodControl.Progress(ctx, profile, id)
}

func TestKHTCOpenCount230_GlobalMetadataAndDashboard(t *testing.T) {
	var control *countControl230
	f := newKHTCFormFxWith(t, func(c forms.PeriodControl) forms.PeriodControl {
		control = &countControl230{PeriodControl: c}
		return control
	})
	pool := pgtest.Get(t).Pool(t, migrations.PF, extension.DBRole)
	// Valid KHTC ledger fixtures deliberately have opaque native references.
	// A metadata count must not need Forms Progress or a per-period source projection.
	err := tenant.WithTenantTx(context.Background(), pool, f.tid, func(tx pgx.Tx) error {
		for i := 0; i < 108; i++ {
			year, no := 2023+i/50, i%50+1
			at := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, (no-1)*7)
			key := fmt.Sprintf("%04d-W%02d", year, no)
			p := ps.Period{ID: ids.New(), Type: pd.TypeWeek, Year: year, No: &no, Key: &key, Name: key,
				From: at, To: at.AddDate(0, 0, 6), Cutoff: at.AddDate(0, 0, 6), DueAt: at.AddDate(0, 0, 9), ExportDueAt: at.AddDate(0, 0, 10),
				MaxSubmissions: 3, Departments: []string{dTT}, FormsPeriodRef: ids.New(), TemplateCode: "KHTC-PHIEU",
				Rule: "open-count-230", ActorRef: f.khtcUser, EventTime: f.clk.Now()}
			if i == 107 {
				p.Type = pd.TypeAdhoc
				p.No = nil
				p.Key = nil
				p.Name = "Kỳ phát sinh 230"
			}
			if e := ps.InsertPeriod(context.Background(), tx, f.tid, &p, "open-count-230:"+p.ID.String()); e != nil {
				return e
			}
			if i < 2 {
				action := ps.ActionClosed
				if i == 1 {
					action = ps.ActionCancelled
				}
				reason := "Sự kiện kết thúc kỳ kiểm thử 230"
				e := ps.Event{ID: ids.New(), PeriodID: p.ID, Action: action, Reason: &reason, State: nil, ActorRef: f.khtcUser, EventTime: f.clk.Now()}
				if err := ps.InsertEvent(context.Background(), tx, f.tid, &e, "open-count-230-event:"+e.ID.String()); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	control.calls.Store(0)
	var dashboard struct {
		Counts struct {
			Open    int  `json:"open"`
			OpenAll *int `json:"open_all"`
		} `json:"counts"`
	}
	status, body := f.khtc(http.MethodGet, "/dashboard?year=2020&quarter=1", f.khtcUser, "", nil)
	f.ok(status, body, &dashboard)
	if dashboard.Counts.OpenAll == nil || *dashboard.Counts.OpenAll != 106 {
		t.Fatalf("global hot-card must count 106 authorized nonterminal periods across years; response=%s", body)
	}
	if dashboard.Counts.Open != 0 {
		t.Fatalf("filtered status projection changed: %+v", dashboard.Counts)
	}
	for _, tc := range []struct {
		name, role, dept string
		want, status     int
		viewOnly, noMFA  bool
	}{
		{name: "KHTC", role: authz.KNTinh, dept: khtcDept, want: 106, status: http.StatusOK},
		{name: "TW hides adhoc", role: authz.KNTW, dept: khtcDept, want: 105, status: http.StatusOK},
		{name: "readonly hides adhoc", role: authz.KNTinh, dept: khtcDept, want: 105, status: http.StatusOK, viewOnly: true},
		{name: "another department", role: authz.KNTinh, dept: dTT, status: http.StatusForbidden},
		{name: "commune", role: authz.KNXa, dept: dTT, status: http.StatusForbidden},
		{name: "admin", role: authz.KNAdmin, want: 106, status: http.StatusOK},
		{name: "admin without MFA hides adhoc", role: authz.KNAdmin, want: 105, status: http.StatusOK, noMFA: true},
		{name: "restored KHTC current authority", role: authz.KNTinh, dept: khtcDept, want: 106, status: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := requestOpenCount230(t, f, tc.role, tc.dept, tc.viewOnly, tc.noMFA)
			if response.Code != tc.status {
				t.Fatalf("count read: want HTTP%d got%d %s", tc.status, response.Code, response.Body.Bytes())
			}
			if response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("counter is cacheable: %v", response.Header())
			}
			var count struct {
				OpenCount *int `json:"open_count"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &count); err != nil {
				t.Fatal(err)
			}
			if response.Code != http.StatusOK {
				if count.OpenCount != nil {
					t.Fatal("denial exposed a counter")
				}
				return
			}
			if count.OpenCount == nil || *count.OpenCount != tc.want {
				t.Fatalf("count=%v want%d", count.OpenCount, tc.want)
			}
		})
	}
	if control.calls.Load() != 0 {
		t.Fatalf("metadata count projected unrelated native periods: %d Progress calls", control.calls.Load())
	}
	err = tenant.WithTenantTx(context.Background(), pool, f.tid, func(tx pgx.Tx) error {
		var periods, events int
		if e := tx.QueryRow(context.Background(), `SELECT (SELECT count(*) FROM pf_extension.khtc_period),(SELECT count(*) FROM pf_extension.khtc_period_event)`).Scan(&periods, &events); e != nil {
			return e
		}
		if periods != 108 || events != 2 {
			t.Fatalf("metadata reads wrote KHTC ledger: periods%d events%d", periods, events)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	other := newKHTCFormFxWith(t, nil)
	var isolated struct {
		OpenCount *int `json:"open_count"`
	}
	status, body = other.khtc(http.MethodGet, "/open-period-count", other.khtcUser, "", nil)
	other.ok(status, body, &isolated)
	if isolated.OpenCount == nil || *isolated.OpenCount != 0 {
		t.Fatalf("cross-tenant count missing or leaked: %v", isolated.OpenCount)
	}
}

func requestOpenCount230(t *testing.T, f *kfFx, role, dept string, viewOnly, noMFA bool) *httptest.ResponseRecorder {
	t.Helper()
	k := authz.KNContext{Role: role, Department: dept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}, ViewOnly: viewOnly}
	if role == authz.KNAdmin {
		k = authz.KNContext{Role: role, Scope: authz.Scope{All: true}, ViewOnly: viewOnly}
	}
	raw, err := json.Marshal(k)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/extension/khtc/open-period-count", nil)
	request.Header.Set("X-Test-KN", string(raw))
	request.Header.Set("X-Test-Actor", f.khtcUser.String())
	if noMFA {
		request.Header.Set("X-Test-No-MFA", "true")
	}
	response := httptest.NewRecorder()
	f.eng.ServeHTTP(response, request)
	return response
}

func TestKHTCOpenCount230_ActualNativeCloseAndCancel(t *testing.T) {
	f := newKHTCFormFxWith(t, nil)
	count := func(want int) {
		t.Helper()
		response := requestOpenCount230(t, f, authz.KNTinh, khtcDept, false, false)
		if response.Code != http.StatusOK {
			t.Fatalf("count HTTP%d %s", response.Code, response.Body.Bytes())
		}
		var value struct {
			OpenCount *int `json:"open_count"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value.OpenCount == nil || *value.OpenCount != want {
			t.Fatalf("count %v want%d", value.OpenCount, want)
		}
	}
	count(0)
	period := f.open(41, dTT)
	count(1)
	status, body := f.send(period.ID, dTT, "submit", 0, ttSheet(f.item, "25"))
	f.ok(status, body, nil)
	count(1)
	status, body = f.khtc(http.MethodPost, "/periods/"+period.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(status, body, nil)
	count(0)
	var closed khtcPeriod
	status, body = f.khtc(http.MethodGet, "/periods/"+period.ID, f.khtcUser, "", nil)
	f.ok(status, body, &closed)
	if closed.State != "closed" || closed.FormsStatus != "closed" {
		t.Fatalf("native and KHTC close not applied: %+v", closed)
	}
	period = f.open(42, dTT)
	count(1)
	status, body = f.khtc(http.MethodPost, "/periods/"+period.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ kiểm thử số đếm 230"})
	f.ok(status, body, nil)
	count(0)
	var cancelled khtcPeriod
	status, body = f.khtc(http.MethodGet, "/periods/"+period.ID, f.khtcUser, "", nil)
	f.ok(status, body, &cancelled)
	if cancelled.State != "cancelled" || cancelled.FormsStatus != "cancelled" {
		t.Fatalf("native and KHTC cancel not applied: %+v", cancelled)
	}
}
