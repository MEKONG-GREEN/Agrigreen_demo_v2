package adhocintegration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
		t.Fatalf("global hot-card open_all must count 106 authorized nonterminal periods across years, not the filtered/capped page: %+v", dashboard.Counts)
	}
	if dashboard.Counts.Open != 0 {
		t.Fatalf("filtered status projection changed: %+v", dashboard.Counts)
	}
	for _, tc := range []struct {
		name, role, dept string
		want, status     int
	}{
		{"KHTC", authz.KNTinh, khtcDept, 106, http.StatusOK},
		{"TW hides adhoc", authz.KNTW, khtcDept, 105, http.StatusOK},
		{"another department", authz.KNTinh, dTT, 0, http.StatusForbidden},
		{"commune", authz.KNXa, dTT, 0, http.StatusForbidden},
		{"admin", authz.KNAdmin, "", 106, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, b := f.do(http.MethodGet, "/v1/extension/khtc/open-period-count", tc.role, tc.dept, f.khtcUser, "", nil)
			if code != tc.status {
				t.Fatalf("count read: want HTTP%d got%d %s", tc.status, code, b)
			}
			if code != http.StatusOK {
				return
			}
			var count struct {
				OpenCount int `json:"open_count"`
			}
			if err := json.Unmarshal(b, &count); err != nil {
				t.Fatal(err)
			}
			if count.OpenCount != tc.want {
				t.Fatalf("count=%d want%d", count.OpenCount, tc.want)
			}
		})
	}
	if control.calls.Load() != 0 {
		t.Fatalf("metadata count projected unrelated native periods: %d Progress calls", control.calls.Load())
	}
	other := newKHTCFormFxWith(t, nil)
	var isolated struct {
		OpenCount int `json:"open_count"`
	}
	status, body = other.khtc(http.MethodGet, "/open-period-count", other.khtcUser, "", nil)
	other.ok(status, body, &isolated)
	if isolated.OpenCount != 0 {
		t.Fatalf("cross-tenant count leaked: %d", isolated.OpenCount)
	}
}
