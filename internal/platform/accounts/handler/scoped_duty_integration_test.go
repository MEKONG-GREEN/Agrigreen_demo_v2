package handler_test

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type scopedDutyClock struct {
	at    time.Time
	calls int
}

func (c *scopedDutyClock) Now() time.Time { c.calls++; return c.at }

type scopedDutyObserver struct {
	tenant.Beginner
	calls    int
	readOnly bool
	role     string
}

func (o *scopedDutyObserver) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	o.calls++
	tx, err := o.Beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	var mode string
	if err = tx.QueryRow(ctx, `SELECT current_user,current_setting('transaction_read_only')`).Scan(&o.role, &mode); err != nil {
		_ = tx.Rollback(ctx)
		return nil, err
	}
	o.readOnly = opts.AccessMode == pgx.ReadOnly && mode == "on"
	return tx, nil
}

// Current projections are seeded directly to cover legacy null/zero UnitRef,
// which current HTTP writers no longer produce. Full handler regression also
// exercises the actual assignment/attribute/delegation writers and MFA guards.
type scopedDutyRow struct {
	tid, actor      uuid.UUID
	unit, org       any
	role, dept      string
	duties          []string
	attrs, inactive bool
	from, to        time.Time
}

func seedScopedDutyRow(t *testing.T, b tenant.Beginner, now time.Time, r scopedDutyRow) {
	t.Helper()
	if r.role == "" {
		r.role = "kn_tinh"
	}
	if r.dept == "" {
		r.dept = "fixture_department"
	}
	if r.from.IsZero() {
		r.from = now.Add(-time.Hour)
	}
	var to any
	if !r.to.IsZero() {
		to = r.to
	}
	if r.duties == nil {
		r.duties = []string{}
	}
	aid, event, by := ids.New(), ids.New(), ids.New()
	err := tenant.WithTenantTx(context.Background(), b, r.tid, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_accounts.role_assignment_current
          (tenant_id,assignment_id,user_ref,role,org_ref,unit_ref,department_codes,valid_from,valid_to,active,last_event_id,last_seq,created_by,created_at,updated_by,updated_at)
          VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,1,$12,$13,$12,$13)`, r.tid, aid, r.actor, r.role, r.org, r.unit, []string{r.dept}, r.from, to, !r.inactive, event, by, now)
		if err != nil || !r.attrs {
			return err
		}
		_, err = tx.Exec(context.Background(), `INSERT INTO pf_accounts.assignment_attr_current
          (tenant_id,assignment_id,user_ref,role,duties,last_event_id,last_seq,updated_by,updated_at)
          VALUES($1,$2,$3,$4,$5,$6,1,$7,$8)`, r.tid, aid, r.actor, r.role, r.duties, event, by, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}
func sortedScopedRefs(xs ...uuid.UUID) []uuid.UUID {
	slices.SortFunc(xs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(xs)
}

func TestScopedDutyHoldersSameAssignmentAndNode(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	clk := &scopedDutyClock{at: now}
	f := newFixtureAt(t, clk)
	raw := f.env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	writer := db.ForModule(raw, accounts.DBModule)
	obs := &scopedDutyObserver{Beginner: writer}
	mod := accounts.New(obs, clk, accounts.Deps{})
	reader := mod.ScopedDutyHolders()
	a, b, foreign := ids.New(), ids.New(), ids.New()
	bridge, direct, fallbackNull, fallbackZero, unitWins := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	expectedA := sortedScopedRefs(bridge, direct, fallbackNull, fallbackZero)
	delegated := ids.New()
	rows := []scopedDutyRow{
		{tid: f.tenant, actor: delegated, unit: a},
		{tid: f.tenant, actor: bridge, unit: a, org: b, attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: bridge, unit: b, org: a, attrs: true, duties: []string{"another_duty"}},
		{tid: f.tenant, actor: direct, unit: a, attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: direct, unit: a, attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: fallbackNull, org: a, attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: fallbackZero, unit: uuid.Nil, org: a, attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: unitWins, unit: b, org: a, attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: ids.New(), unit: a, role: "kn_xa", attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: ids.New(), unit: a, dept: "another_department", attrs: true, duties: []string{"fixture_duty"}},
		{tid: f.tenant, actor: ids.New(), unit: a, attrs: true, duties: []string{"another_duty"}},
		{tid: f.tenant, actor: ids.New(), unit: a},
		{tid: f.tenant, actor: ids.New(), unit: a, attrs: true, duties: []string{"fixture_duty"}, inactive: true},
		{tid: f.tenant, actor: ids.New(), unit: a, attrs: true, duties: []string{"fixture_duty"}, from: now.Add(time.Hour)},
		{tid: f.tenant, actor: ids.New(), unit: a, attrs: true, duties: []string{"fixture_duty"}, to: now.Add(-time.Minute)},
		{tid: f.tenant, actor: ids.New(), unit: a, attrs: true, duties: []string{"fixture_duty"}, to: now},
		{tid: f.tenant, actor: ids.New(), attrs: true, duties: []string{"fixture_duty"}},
		{tid: foreign, actor: ids.New(), unit: a, attrs: true, duties: []string{"fixture_duty"}},
	}
	// An effective assignment starting exactly now must be included.
	rows[3].from = now
	for _, r := range rows {
		seedScopedDutyRow(t, writer, now, r)
	}
	// A live delegated power is not an assignment duty, even at the same node.
	err := tenant.WithTenantTx(context.Background(), writer, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `INSERT INTO pf_accounts.delegation_current
          (tenant_id,delegation_id,grantor_ref,grantee_ref,unit_ref,department_code,power,valid_from,valid_to,revoked,reason,last_event_id,last_seq,created_by,created_at,updated_at)
          VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,false,$10,$11,1,$3,$8,$8)`,
			f.tenant, ids.New(), direct, delegated, a, "fixture_department", authz.PowerReopenPeriod,
			now.Add(-time.Hour), now.Add(time.Hour), "fixture delegated power", ids.New())
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	check := func(node uuid.UUID, want []uuid.UUID) {
		t.Helper()
		clk.calls = 0
		obs.calls = 0
		job := context.Background()
		got, err := reader.ScopedDutyHolders(job, f.tenant, node, "kn_tinh", "fixture_department", "fixture_duty")
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("node result: %v %v want%v", got, err, want)
		}
		if clk.calls != 1 || obs.calls != 1 || !obs.readOnly || obs.role != "r_accounts" {
			t.Fatalf("snapshot/transaction: clock%d tx%d readonly%v role%s", clk.calls, obs.calls, obs.readOnly, obs.role)
		}
		if _, ok := authz.FromContext(job); ok {
			t.Fatal("job acquired a Principal")
		}
		if len(got) > 0 {
			got[0] = uuid.Nil
		}
		again, err := reader.ScopedDutyHolders(job, f.tenant, node, "kn_tinh", "fixture_department", "fixture_duty")
		if err != nil || !reflect.DeepEqual(again, want) {
			t.Fatal("caller mutation changed query result")
		}
	}
	check(a, expectedA)
	check(b, sortedScopedRefs(unitWins)) // bridge has duty only at A, not at B.
	// Old tenant-wide ports remain available with their original semantics.
	old, err := mod.RoleHolders().DutyHolders(context.Background(), f.tenant, "kn_tinh", "fixture_department", "fixture_duty")
	if err != nil || !slices.Contains(old, bridge) || !slices.Contains(old, unitWins) {
		t.Fatal("legacy duty query changed", err)
	}
	all, err := mod.RoleHolders().RoleHolders(context.Background(), f.tenant, "kn_tinh", "fixture_department")
	if err != nil || !slices.Contains(all, bridge) {
		t.Fatal("legacy role query changed", err)
	}
}

func TestScopedDutyHoldersValidityRevocationAndTenantGuards(t *testing.T) {
	now := time.Date(2026, 10, 21, 2, 0, 0, 0, time.UTC)
	clk := &scopedDutyClock{at: now}
	f := newFixtureAt(t, clk)
	writer := db.ForModule(f.env.Pool(t, migrations.PF, dbadmin.AppPlatform), accounts.DBModule)
	obs := &scopedDutyObserver{Beginner: writer}
	mod := accounts.New(obs, clk, accounts.Deps{})
	reader := mod.ScopedDutyHolders()
	node, otherTenant, actor := ids.New(), ids.New(), ids.New()
	seedScopedDutyRow(t, writer, now, scopedDutyRow{tid: f.tenant, actor: actor, unit: node, attrs: true, duties: []string{"fixture_duty"}, from: now.Add(time.Hour), to: now.Add(2 * time.Hour)})
	read := func() []uuid.UUID {
		t.Helper()
		got, err := reader.ScopedDutyHolders(context.Background(), f.tenant, node, "kn_tinh", "fixture_department", "fixture_duty")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := read(); len(got) != 0 {
		t.Fatal("future assignment included", got)
	}
	clk.at = now.Add(time.Hour)
	if got := read(); !reflect.DeepEqual(got, []uuid.UUID{actor}) {
		t.Fatal("valid_from equality excluded", got)
	}
	clk.at = now.Add(2 * time.Hour)
	if got := read(); len(got) != 0 {
		t.Fatal("valid_to equality included", got)
	}
	clk.at = now.Add(time.Hour)
	err := tenant.WithTenantTx(context.Background(), writer, f.tenant, func(tx pgx.Tx) error {
		_, err := tx.Exec(context.Background(), `UPDATE pf_accounts.assignment_attr_current SET duties='{}' WHERE user_ref=$1`, actor)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 0 {
		t.Fatal("removed duty included", got)
	}
	err = tenant.WithTenantTx(context.Background(), writer, f.tenant, func(tx pgx.Tx) error {
		if _, err := tx.Exec(context.Background(), `UPDATE pf_accounts.assignment_attr_current SET duties=ARRAY['fixture_duty'] WHERE user_ref=$1`, actor); err != nil {
			return err
		}
		_, err := tx.Exec(context.Background(), `UPDATE pf_accounts.role_assignment_current SET active=false WHERE user_ref=$1`, actor)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := read(); len(got) != 0 {
		t.Fatal("revoked assignment included", got)
	}
	cases := []struct {
		name             string
		ctx              context.Context
		tid, node        uuid.UUID
		role, dept, duty string
		want             error
	}{
		{"zero tenant", context.Background(), uuid.Nil, node, "kn_tinh", "fixture_department", "fixture_duty", tenant.ErrMissing},
		{"zero node", context.Background(), f.tenant, uuid.Nil, "kn_tinh", "fixture_department", "fixture_duty", port.ErrScopedDutyQuery},
		{"blank role", context.Background(), f.tenant, node, " ", "fixture_department", "fixture_duty", port.ErrScopedDutyQuery},
		{"blank department", context.Background(), f.tenant, node, "kn_tinh", "\t", "fixture_duty", port.ErrScopedDutyQuery},
		{"blank duty", context.Background(), f.tenant, node, "kn_tinh", "fixture_department", "", port.ErrScopedDutyQuery},
		{"foreign Principal", authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: actor, TenantID: otherTenant}), f.tenant, node, "kn_tinh", "fixture_department", "fixture_duty", port.ErrScopedDutyTenant},
		{"foreign tenant context", tenant.WithID(context.Background(), otherTenant), f.tenant, node, "kn_tinh", "fixture_department", "fixture_duty", port.ErrScopedDutyTenant},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			obs.calls = 0
			clk.calls = 0
			got, err := reader.ScopedDutyHolders(c.ctx, c.tid, c.node, c.role, c.dept, c.duty)
			if !errors.Is(err, c.want) || got != nil || obs.calls != 0 || clk.calls != 0 {
				t.Fatalf("guard returned data or reached DB: %v %v tx%d clock%d", got, err, obs.calls, clk.calls)
			}
		})
	}
	seedScopedDutyRow(t, writer, now, scopedDutyRow{tid: otherTenant, actor: actor, unit: node, attrs: true, duties: []string{"fixture_duty"}})
	if got := read(); len(got) != 0 {
		t.Fatal("foreign tenant leaked", got)
	}
	raw := authz.Principal{ActorRef: ids.New(), TenantID: otherTenant, OrgRef: ids.New(), Roles: []string{"platform_admin"}, MFA: true}
	ctx := authz.WithPrincipal(tenant.WithID(context.Background(), otherTenant), raw)
	got, err := reader.ScopedDutyHolders(ctx, otherTenant, node, "kn_tinh", "fixture_department", "fixture_duty")
	if err != nil || !reflect.DeepEqual(got, []uuid.UUID{actor}) {
		t.Fatal("same tenant query failed", got, err)
	}
	after, ok := authz.FromContext(ctx)
	if !ok || !reflect.DeepEqual(raw, after) {
		t.Fatal("query rewrote authenticated identity")
	}
}
