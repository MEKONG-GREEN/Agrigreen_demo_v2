package khtcplan225

import (
	"context"
	"encoding/json"
	"errors"
	periodapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

// LegacyIntent225 exposes only the public HTTP arrangement; owned storage and
// canonical private composition remain inside this test fixture.
type LegacyIntent225 struct {
	ID, PlannedPeriodID uuid.UUID
	Name                string
	DueAt               time.Time
	Departments         []string
	value               ps.AdhocOpenIntent
}
type IntentSnapshot225 struct{ value ps.AdhocOpenIntent }

func NewLegacyIntent225(ctx context.Context, clk clock.Clock, publisher forms.Publisher, year int) (LegacyIntent225, error) {
	var out LegacyIntent225
	p, k, err := ext.RequireSession(ctx)
	if err != nil {
		return out, err
	}
	if k.Support != nil || !k.Scope.All || len(k.Scope.Units)+len(k.Communes)+len(k.Powers)+len(k.Duties)+len(k.Facilities)+len(k.Households) != 0 {
		return out, errors.New("simple current Admin fixture required")
	}
	canonical := k
	canonical.ID = uuid.Nil
	canonical.ExpiresAt = time.Time{}
	canonical.DefaultRoute = ""
	canonical.Scope.Units = []string{}
	canonical.Communes = []string{}
	canonical.Powers = []string{}
	canonical.Duties = []string{}
	canonical.Facilities = []uuid.UUID{}
	canonical.Households = []uuid.UUID{}
	contextHash, err := forms.OperationHash(canonical)
	if err != nil {
		return out, err
	}
	from := time.Date(year, 10, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 12)
	due := to.AddDate(0, 0, 7)
	input := periodapp.OpenInput{Request: pd.Request{Type: pd.TypeAdhoc, Year: year, Name: "Intent trước kế hoạch225", From: &from, To: &to}, Overrides: pd.Overrides{DueAt: &due, ExportDueAt: &due}, Departments: []string{"trong_trot"}}
	plan, err := pd.PlanOf(rc.DefaultRule, input.Request, clock.Zone())
	if err != nil {
		return out, err
	}
	plan, err = plan.Apply(input.Overrides, clock.Zone())
	if err != nil {
		return out, err
	}
	template, err := publisher.LatestTemplate(ctx, pd.ProfileKHTC, pd.TemplateCode)
	if err != nil {
		return out, err
	}
	max := 3
	batch := forms.BatchInput{Profile: pd.ProfileKHTC, TemplateID: template.ID, UnitKind: forms.UnitDepartment, Units: input.Departments, MaxSubmissions: &max, Periods: []forms.PeriodDef{{Label: plan.Name, DueAt: plan.DueAt, Kind: plan.FormsKind, Key: plan.FormsKey, From: plan.From, To: plan.To, Rule: plan.Rule}}}
	raw, err := json.Marshal(input)
	if err != nil {
		return out, err
	}
	requestHash, err := forms.OperationHash(input)
	if err != nil {
		return out, err
	}
	batchHash, err := forms.OperationHash(batch)
	if err != nil {
		return out, err
	}
	value := ps.AdhocOpenIntent{ID: ids.New(), PlannedPeriodID: ids.New(), ActorRef: p.ActorRef, SessionID: k.ID, EffectiveSessionID: k.ID, ContextHash: contextHash, SessionExpiresAt: k.ExpiresAt, EffectiveExpiresAt: k.ExpiresAt, NativeKey: "khtc:" + ids.New().String(), RequestHash: requestHash, BatchHash: batchHash, Input: raw, Plan: plan, Batch: batch, Departments: input.Departments, Sources: []ext.KHTCSourceRef{}, At: clk.Now()}
	return LegacyIntent225{ID: value.ID, PlannedPeriodID: value.PlannedPeriodID, Name: input.Request.Name, DueAt: due, Departments: append([]string{}, input.Departments...), value: value}, nil
}
func (x LegacyIntent225) Insert(ctx context.Context, db tenant.Beginner) error {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return errors.New("fixture principal missing")
	}
	return tenant.WithTenantTx(ctx, db, p.TenantID, func(tx pgx.Tx) error { return ps.InsertAdhocOpen(ctx, tx, p.TenantID, x.value) })
}
func (x LegacyIntent225) Snapshot(ctx context.Context, db tenant.Beginner) (IntentSnapshot225, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return IntentSnapshot225{}, errors.New("fixture principal missing")
	}
	var stored *ps.AdhocOpenIntent
	err := tenant.WithTenantTx(ctx, db, p.TenantID, func(tx pgx.Tx) error { var e error; stored, e = ps.AdhocOpenByID(ctx, tx, x.ID); return e })
	if err != nil {
		return IntentSnapshot225{}, err
	}
	if stored == nil {
		return IntentSnapshot225{}, errors.New("fixture intent missing")
	}
	return IntentSnapshot225{value: *stored}, nil
}
func CalendarCounts225(ctx context.Context, db tenant.Beginner) (intents, leases, periods int, err error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return 0, 0, 0, errors.New("fixture principal missing")
	}
	err = tenant.WithTenantTx(ctx, db, p.TenantID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM pf_extension.khtc_adhoc_open_intent),(SELECT count(*) FROM pf_extension.khtc_adhoc_open_lease),(SELECT count(*) FROM pf_extension.khtc_period)`).Scan(&intents, &leases, &periods)
	})
	return
}
