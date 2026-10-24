package handler_test

import (
	"context"
	"net/http"
	"testing"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
)

// This scoped port stub models the real Plot port's RLS-hidden response; it
// does not expose another HTX's metadata to classify a failed season lookup.
type logScopePlots struct {
	port.Plots
	hidden map[uuid.UUID]bool
}

func (p logScopePlots) Get(ctx context.Context, ref uuid.UUID) (port.Plot, error) {
	if p.hidden[ref] {
		return port.Plot{}, port.ErrPlotNotFound
	}
	return p.Plots.Get(ctx, ref)
}

func TestFarmLogHiddenAndUnknownPlotsHaveSameDenial(t *testing.T) {
	f := newFixture(t)
	f.publishedSOP("SOP log scope", "OM5451")
	f.managerA().mustOpen(f.p1)
	f.mod.svc.Plots = logScopePlots{Plots: f.plots, hidden: map[uuid.UUID]bool{f.p1: true, f.p3: true}}
	for _, c := range []caller{f.managerB(), f.farmer(f.h4)} {
		for _, ref := range []uuid.UUID{f.p1, f.p3, ids.New()} {
			expectCode(t, c.log(ref, nil), http.StatusForbidden, "cropcycle.out_of_scope", "hidden/open, hidden/unopened and unknown plots use same denial")
		}
		expectCode(t, c.log(f.p4, nil), http.StatusConflict, "cropcycle.season_not_open", "own plot proven, no open season")
		body := map[string]any{"season_ref": ids.New().String(), "plot_ref": f.p4.String(), "activity_code": "bon_phan", "gps": map[string]any{"lon": "105.6", "lat": "10.05"}}
		expectCode(t, c.post("/v1/cropcycle/farm-logs", body), http.StatusForbidden, "cropcycle.out_of_scope", "unseen season cannot borrow ownership of client plot")
	}
	// A visible plot of another household is still not writable by a farmer.
	f.mod.svc.Plots = f.plots
	expectCode(t, f.farmer(f.h1).log(f.p3, nil), http.StatusForbidden, "cropcycle.out_of_scope", "other household without an open season")
	r := f.managerA().post("/v1/cropcycle/farm-logs/batch", map[string]any{"plot_refs": []string{f.p1.String(), f.p4.String(), ids.New().String()}, "activity_code": "bon_phan"})
	expect(t, r, http.StatusCreated, "batch retains permitted line")
	if num(r.body, "accepted") != 1 || len(list(r.body, "skipped")) != 2 {
		t.Fatalf("batch scope: %s", r.raw)
	}
	for _, skipped := range list(r.body, "skipped") {
		if str(skipped, "code") != "cropcycle.out_of_scope" || str(skipped, "reason") != "Lô không thuộc quyền quản lý" {
			t.Fatalf("scope denial leaked existence/state: %v", skipped)
		}
	}
}
