package app

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/google/uuid"
	"testing"
	"time"
)

func openIntentFixture(t *testing.T) store.AdhocOpenIntent {
	t.Helper()
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 12)
	due := to.AddDate(0, 0, 7)
	input := OpenInput{Request: domain.Request{Type: domain.TypeAdhoc, Year: 2026, Name: "Phát sinh", From: &from, To: &to}, Overrides: domain.Overrides{DueAt: &due, ExportDueAt: &due}, Departments: []string{"trong_trot"}}
	plan, err := domain.PlanOf(rc.DefaultRule, input.Request, clock.Zone())
	if err != nil {
		t.Fatal(err)
	}
	plan, err = plan.Apply(input.Overrides, clock.Zone())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	requestHash, err := forms.OperationHash(input)
	if err != nil {
		t.Fatal(err)
	}
	max := 3
	batch := forms.BatchInput{Profile: domain.ProfileKHTC, TemplateID: uuid.New(), UnitKind: forms.UnitDepartment, Units: input.Departments, MaxSubmissions: &max, Periods: []forms.PeriodDef{{Label: plan.Name, Kind: plan.FormsKind, Key: plan.FormsKey, From: plan.From, To: plan.To, Rule: plan.Rule, DueAt: plan.DueAt}}}
	hash, err := forms.OperationHash(batch)
	if err != nil {
		t.Fatal(err)
	}
	return store.AdhocOpenIntent{ID: uuid.New(), ActorRef: uuid.New(), PlannedPeriodID: uuid.New(), SessionID: uuid.New(), Input: raw, RequestHash: requestHash, BatchHash: hash, Plan: plan, Batch: batch, Departments: input.Departments}
}
func TestW1168OpenIntentCanonicalPlanAndDepartments(t *testing.T) {
	good := openIntentFixture(t)
	if !validOpenIntent(good) {
		t.Fatal("valid canonical intent rejected")
	}
	cases := map[string]func(*store.AdhocOpenIntent){
		"fixed plan":      func(x *store.AdhocOpenIntent) { x.Plan.Type = domain.TypeWeek },
		"other profile":   func(x *store.AdhocOpenIntent) { x.Batch.Profile = "kn_cnts" },
		"other unit kind": func(x *store.AdhocOpenIntent) { x.Batch.UnitKind = forms.UnitCommune },
		"metadata name":   func(x *store.AdhocOpenIntent) { x.Plan.Name = "Changed"; x.Batch.Periods[0].Label = "Changed" },
		"department swap": func(x *store.AdhocOpenIntent) {
			x.Departments = []string{"chan_nuoi_thuy_san"}
			x.Batch.Units = x.Departments
		},
		"no session":    func(x *store.AdhocOpenIntent) { x.SessionID = uuid.Nil },
		"request hash":  func(x *store.AdhocOpenIntent) { x.RequestHash = "wrong" },
		"date mutation": func(x *store.AdhocOpenIntent) { x.Batch.Periods[0].From = x.Batch.Periods[0].From.AddDate(0, 0, 1) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			raw, _ := json.Marshal(good)
			var x store.AdhocOpenIntent
			if err := json.Unmarshal(raw, &x); err != nil {
				t.Fatal(err)
			}
			change(&x)
			if validOpenIntent(x) {
				t.Fatal("forged intent accepted")
			}
		})
	}
}
