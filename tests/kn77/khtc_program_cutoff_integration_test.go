package kn77_test

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"net/http"
	"testing"
	"time"
)

func TestKH_M5_ProgramCutoffHTTP(t *testing.T) {
	f := newReportFx(t)
	f.clk.Advance(-7 * 24 * time.Hour)
	before := f.open(40, dTT, dTTTT)
	f.submit(before.ID, "10")
	f.clk.Advance(7 * 24 * time.Hour)
	after := f.open(41, dTT, dTTTT)
	f.submit(after.ID, "90")
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{
		"report_type": "program", "year": 2026, "as_of_date": "2026-10-13", "cutoff_date": "2026-10-06",
		"due_at": "2026-12-31T03:00:00Z", "departments": []string{dTT, dTTTT}})
	f.ok(c, b, &p)
	d := f.refresh(p.ID, 0)
	if row := reportRow(t, d, f.item); row.InPeriod != "10" {
		t.Errorf("manual weekly figures after cutoff entered appendix: %+v", row)
	}
	if row := reportRow(t, d, f.itemMod); row.InPeriod != "100" {
		t.Errorf("module week after cutoff entered appendix: %+v", row)
	}
}

func TestKH_M5_LiveProgramSourceCrosswalk(t *testing.T) {
	f := newKHTCFormFx(t)
	ctx := authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: f.khtcUser, TenantID: f.tid})
	from, _ := time.Parse(time.DateOnly, "2025-12-31")
	to, _ := time.Parse(time.DateOnly, "2026-10-06")
	if _, err := f.ext.KHTCSourceCrosswalk().Sources(ctx, "adhoc", "2026-CT-1013", from, to, []string{dTT, dTTTT}); err != nil {
		t.Fatalf("production program crosswalk: %v", err)
	}
}
