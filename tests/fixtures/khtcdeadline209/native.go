// Package khtcdeadline209 is a test-only composition root for consumer CAS.
// It exposes public native ports and a synthetic trusted owner projection only.
package khtcdeadline209

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"testing"
	"time"
)

type Native struct {
	module *forms.Module
	Pool   *pgxpool.Pool
	DB     tenant.Beginner
	Clock  *clock.Fixed
}

func New(t testing.TB) *Native {
	env := pgtest.Get(t)
	pool := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	clk := clock.NewFixed(time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC))
	var profile port.Profile
	for _, p := range extension.FormProfiles() {
		if p.Name == "kn_khtc" {
			profile = p
		}
	}
	module, e := forms.New(pool, clk, forms.Deps{Profiles: []port.Profile{profile}})
	if e != nil {
		t.Fatal(e)
	}
	return &Native{module: module, Pool: pool, DB: db.ForModule(pool, extension.DBModule), Clock: clk}
}
func (f *Native) Publisher() port.Publisher   { return f.module.Publisher() }
func (f *Native) Control() port.PeriodControl { return f.module.Control() }
func (f *Native) ConditionalControl() port.ConditionalPeriodControl {
	return f.module.ConditionalControl()
}

// Bind is a fixture owner registry, not production account revalidation.
func Bind(t testing.TB, p authz.Principal) authz.Principal {
	t.Helper()
	if p.KN == nil {
		return p
	}
	k := *p.KN
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	p.KN = &k
	registry, e := authz.NewSessionRegistry(extension.SessionProjection())
	if e != nil {
		t.Fatal(e)
	}
	s, e := registry.ResolveSelected(context.Background(), p)
	if e != nil {
		t.Fatal(e)
	}
	p.SelectedSession = &s
	return p
}
