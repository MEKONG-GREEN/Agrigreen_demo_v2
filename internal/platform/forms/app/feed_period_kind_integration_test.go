package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

func TestFeedExcludesPeriodKindsBeforeLimit(t *testing.T) {
	f := newFx(t)
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	publisher := f.as(f.tid, uuid.Nil, "tinh", ids.New(), "U1", "U2")
	leaf := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U1", "U2")
	templates, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "fixtures")
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	publish := func(kind string, n int, unit string) uuid.UUID {
		t.Helper()
		var period *PeriodKey
		if kind != "" {
			from, to := day(n), day(n)
			period = &PeriodKey{Kind: kind, Key: fmt.Sprintf("p-%d", n), From: &from, To: &to}
		}
		p, err := f.svc.Publish(publisher, PublishInput{Profile: "fx", TemplateID: templates[1].ID, Label: fmt.Sprintf("period %d", n), Units: []string{unit}, DueAt: f.clk.Now().AddDate(0, 1, 0), Period: period}, "")
		f.must(err, "publish")
		_, err = f.svc.Submit(leaf, "fx", p.Period.ID, unit, fx02, "")
		f.must(err, "submit")
		return p.Period.ID
	}
	regular := publish("week", 2, "U1")
	_, err = f.svc.Submit(leaf, "fx", regular, "U1", map[string]string{"pest": "updated", "area_infected": "8"}, "")
	f.must(err, "resubmit regular")
	latest := uuid.Nil
	for n := 10; n < 15; n++ {
		latest = publish("adhoc", n, "U1")
	}
	other := publish("week", 20, "U2")
	legacy := publish("", 1, "U1")
	feed := Feed{Svc: f.svc}
	query := port.SubmissionQuery{Profile: "fx", TemplateCode: "FX-02", Units: []string{"U1"}, Limit: 1}
	for label, kinds := range map[string][]string{"nil": nil, "empty": {}, "literal": {"adhoc' OR 1=1 --"}} {
		t.Run("legacy-"+label, func(t *testing.T) {
			q := query
			q.ExcludePeriodKinds = kinds
			rows, err := feed.Latest(f.ctx, f.tid, q)
			if err != nil || len(rows) != 1 || rows[0].PeriodID != latest {
				t.Fatalf("unfiltered feed changed: %+v %v", rows, err)
			}
		})
	}
	query.ExcludePeriodKinds = []string{"adhoc"}
	rows, err := feed.Latest(f.ctx, f.tid, query)
	if err != nil || len(rows) != 1 || rows[0].PeriodID != regular || rows[0].Version != 2 || rows[0].Values["area_infected"] != "8" {
		t.Fatalf("excluded campaigns consumed limit or lost latest version: %+v %v", rows, err)
	}
	q := query
	q.Units = nil
	q.Parents = []string{"U2"}
	rows, err = feed.Latest(f.ctx, f.tid, q)
	if err != nil || len(rows) != 1 || rows[0].PeriodID != other {
		t.Fatalf("parent filter changed: %+v %v", rows, err)
	}
	q = query
	from, to := day(3), day(15)
	q.From, q.To = &from, &to
	rows, err = feed.Latest(f.ctx, f.tid, q)
	if err != nil || len(rows) != 0 {
		t.Fatalf("date filter changed: %+v %v", rows, err)
	}
	q = query
	q.ExcludePeriodKinds = []string{"adhoc", "week"}
	rows, err = feed.Latest(f.ctx, f.tid, q)
	if err != nil || len(rows) != 1 || rows[0].PeriodID != legacy {
		t.Fatalf("generic kinds/legacy null semantics: %+v %v", rows, err)
	}
	q.ExcludePeriodKinds = append(q.ExcludePeriodKinds, "")
	rows, err = feed.Latest(f.ctx, f.tid, q)
	if err != nil || len(rows) != 0 {
		t.Fatalf("explicit empty kind must exclude legacy: %+v %v", rows, err)
	}
	rows, err = feed.Latest(f.ctx, ids.New(), query)
	if err != nil || len(rows) != 0 {
		t.Fatalf("foreign tenant feed: %+v %v", rows, err)
	}
	q = query
	q.Profile = "prod"
	rows, err = feed.Latest(f.ctx, f.tid, q)
	if err != nil || len(rows) != 0 {
		t.Fatalf("different profile feed: %+v %v", rows, err)
	}
}
