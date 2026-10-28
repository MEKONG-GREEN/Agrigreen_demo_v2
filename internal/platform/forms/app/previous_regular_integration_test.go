package app

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestPreviousRegularIgnoresAdhocAndPreservesLegacy(t *testing.T) {
	f := newFx(t)
	admin := f.as(f.tid, uuid.Nil, "admin", uuid.Nil)
	node := ids.New()
	publisher := f.as(f.tid, uuid.Nil, "tinh", node, "U1", "U2")
	leaf := f.as(f.tid, uuid.Nil, "xa", ids.New(), "U1")
	templates, err := f.svc.InstallFixtures(admin, "fx", "")
	f.must(err, "install fixture")
	template := templates[1].ID
	day := func(n int) time.Time { return time.Date(2026, 9, n, 0, 0, 0, 0, time.UTC) }
	publish := func(kind, key string, start int) uuid.UUID {
		t.Helper()
		var period *PeriodKey
		if kind != "" {
			from, to := day(start), day(start+1)
			period = &PeriodKey{Kind: kind, Key: strings.ReplaceAll(key, "_", "-"), From: &from, To: &to, Rule: "fixture"}
		}
		view, err := f.svc.Publish(publisher, PublishInput{Profile: "fx", TemplateID: template, Label: key, Units: []string{"U1", "U2"}, DueAt: f.clk.Now().AddDate(0, 1, 0), Period: period}, "")
		f.must(err, "publish "+key)
		return view.Period.ID
	}
	previous := publish("week", "regular-old", 2)
	_, err = f.svc.Submit(leaf, "fx", previous, "U1", fx02, "")
	f.must(err, "submit regular")
	campaign := publish("adhoc", "campaign-old", 10)
	_, err = f.svc.Submit(leaf, "fx", campaign, "U1", map[string]string{"pest": "fixture campaign", "area_infected": "99"}, "")
	f.must(err, "submit campaign")
	current := uuid.Nil
	for _, kind := range []string{"day", "week", "ksgm_week", "month", "quarter", "season", "year"} {
		id := publish(kind, "current-"+kind, 20)
		if kind == "week" {
			current = id
		}
		t.Run(kind, func(t *testing.T) {
			got, err := f.svc.Previous(leaf, "fx", id, "U1")
			if err != nil || got == nil || got.PeriodID != previous || got.Submission.Values["area_infected"] != "3.5" {
				t.Errorf("regular must ignore newer adhoc and retain older submitted period: %+v %v", got, err)
			}
		})
	}
	adhoc := publish("adhoc", "campaign-current", 25)
	legacy := publish("", "legacy-current", 0)
	for label, id := range map[string]uuid.UUID{"adhoc": adhoc, "legacy": legacy} {
		t.Run(label, func(t *testing.T) {
			got, err := f.svc.Previous(leaf, "fx", id, "U1")
			if err != nil || got == nil || got.PeriodID != campaign {
				t.Errorf("legacy/adhoc semantics changed: %+v %v", got, err)
			}
		})
	}
	_, err = f.svc.Previous(leaf, "fx", current, "U2")
	f.want(err, "forbidden", "previous outside leaf scope")
	foreign := f.as(ids.New(), uuid.Nil, "xa", ids.New(), "U1")
	_, err = f.svc.Previous(foreign, "fx", current, "U1")
	f.want(err, port.ErrPeriodNotFound.Code, "foreign tenant cannot see previous")
	_, err = f.svc.Previous(leaf, "prod", current, "U1")
	f.want(err, port.ErrPeriodNotFound.Code, "different profile cannot see previous")
}
