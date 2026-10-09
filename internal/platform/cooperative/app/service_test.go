package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port/porttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/shareread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mặt tiền trên bản giả trong bộ nhớ qua bộ kiểm hợp đồng cổng.
func TestFacadeContract(t *testing.T) {
	porttest.RunContract(t, tenant.WithID, func(_ *testing.T, s porttest.Seed) porttest.Ports {
		mem := porttest.Memory{S: s}
		m := cooperative.NewFacade(cooperative.Sources{Cooperatives: mem, Households: mem, Plots: mem, Seasons: mem, Diary: porttest.MemoryDiary{S: s}})
		return porttest.Ports{Cooperatives: m.Cooperatives(), Households: m.Households(), Plots: m.Plots(), Seasons: m.Seasons(),
			Diary: m.Diary(), Resolve: m.Resolve()}
	})
}

// Nguồn chưa nối → ErrUnavailable trên mọi cổng (bên gọi trả 503), không panic.
func TestFacadeUnwired(t *testing.T) {
	m := cooperative.NewFacade(cooperative.Sources{})
	ctx := tenant.WithID(context.Background(), uuid.New())
	ref := uuid.New()
	checks := map[string]error{}
	_, checks["ByUnit"] = m.Cooperatives().ByUnit(ctx, nil)
	_, checks["Active"] = m.Cooperatives().Active(ctx)
	_, checks["Centroids"] = m.Cooperatives().Centroids(ctx, nil)
	_, checks["Membership"] = m.Households().Membership(ctx, ref)
	_, checks["InCommunes"] = m.Households().InCommunes(ctx, nil)
	_, checks["Get"] = m.Plots().Get(ctx, ref)
	_, checks["DistanceM"] = m.Plots().DistanceM(ctx, ref, decimal.Zero, decimal.Zero)
	_, checks["Facts"] = m.Plots().Facts(ctx, []uuid.UUID{ref})
	_, checks["DigitizedByCommune"] = m.Plots().DigitizedByCommune(ctx, nil)
	_, checks["DigitizedByCooperative"] = m.Plots().DigitizedByCooperative(ctx, nil)
	_, checks["ActivityByCooperative"] = m.Seasons().ActivityByCooperative(ctx, nil)
	_, checks["Areas"] = m.Seasons().Areas(ctx, ref, "", 0)
	_, checks["Supply"] = m.Seasons().Supply(ctx, 2026)
	_, checks["Production"] = m.Seasons().Production(ctx, port.ProductionQuery{})
	_, checks["HarvestDates"] = m.Seasons().HarvestDates(ctx, []uuid.UUID{ref})
	_, checks["Diary.Get"] = m.Diary().Get(ctx, ref)
	_, checks["Diary.BySeason"] = m.Diary().BySeason(ctx, ref, uuid.Nil, 10)
	_, checks["Resolve"] = m.Resolve().Resolve(ctx, port.SubjectPlot, ref)
	for name, err := range checks {
		if !errors.Is(err, port.ErrUnavailable) {
			t.Errorf("%s: err = %v, muốn ErrUnavailable", name, err)
		}
	}
}

// Mặt tiền trên nguồn đã nối (vụ, thửa).
func TestFacadeSources(t *testing.T) {
	s := porttest.SampleSeed()
	m := cooperative.NewFacade(cooperative.Sources{Seasons: porttest.Memory{S: s}, Plots: porttest.Memory{S: s}})
	ctx := tenant.WithID(context.Background(), s.Tenant)
	if rows, err := m.Seasons().Supply(ctx, 2026); err != nil || len(rows) != 1 {
		t.Fatalf("Supply = %v, %v", rows, err)
	}
	if _, err := m.Plots().Get(ctx, s.Plots[0].Ref); err != nil {
		t.Fatalf("Get: %v", err)
	}
}

type spyDiary struct{ limit int }

func (d *spyDiary) Get(context.Context, uuid.UUID) (port.DiaryEntry, error) {
	return port.DiaryEntry{}, nil
}
func (d *spyDiary) BySeason(_ context.Context, _, _ uuid.UUID, limit int) ([]port.DiaryEntry, error) {
	d.limit = limit
	return nil, nil
}

func TestDiaryLimitClamped(t *testing.T) {
	spy := &spyDiary{}
	m := cooperative.NewFacade(cooperative.Sources{Diary: spy})
	for in, want := range map[int]int{-1: app.DefaultDiaryLimit, 0: app.DefaultDiaryLimit, 7: 7, 10_000: app.MaxDiaryLimit} {
		if _, err := m.Diary().BySeason(context.Background(), uuid.New(), uuid.Nil, in); err != nil {
			t.Fatal(err)
		}
		if spy.limit != want {
			t.Fatalf("limit %d → %d, muốn %d", in, spy.limit, want)
		}
	}
}

// W3.3: season, harvest phân giải qua store vụ (Sources.Season); chưa nối → ErrUnavailable; loại lạ → ErrSubjectTypeUnsupported.
func TestResolveSeasonNeedsStore(t *testing.T) {
	s := porttest.SampleSeed()
	mem := porttest.Memory{S: s}
	m := cooperative.NewFacade(cooperative.Sources{Cooperatives: mem, Households: mem, Plots: mem, Seasons: mem})
	ctx := tenant.WithID(context.Background(), s.Tenant)
	for _, typ := range []port.SubjectType{port.SubjectSeason, port.SubjectHarvest} {
		if _, err := m.Resolve().Resolve(ctx, typ, uuid.New()); !errors.Is(err, port.ErrUnavailable) {
			t.Fatalf("%s: err = %v", typ, err)
		}
	}
	if _, err := m.Resolve().Resolve(ctx, port.SubjectType("tractor"), uuid.New()); !errors.Is(err, port.ErrSubjectTypeUnsupported) {
		t.Fatalf("loại lạ: err = %v", err)
	}
}

// W4.2 (ADR-0030 mục 4): mặt tiền đặt cờ ViaShareGrant cho bản ghi có tenant ≠ tenant phiên và ghi vào nhật ký
// gộp của yêu cầu (shareread) — bản ghi của chính đơn vị và bản ghi không rõ tenant (bản giả) không tính.
func TestFacadeFlagsShareReads_W42(t *testing.T) {
	s := porttest.SampleSeed()
	viewer, owner := uuid.New(), uuid.New()
	s.Plots[0].OwnerTenant, s.Plots[1].OwnerTenant = owner, viewer
	s.Diary[0].OwnerTenant = owner
	s.Members[0].OwnerTenant = owner
	mem := porttest.Memory{S: s}
	m := cooperative.NewFacade(cooperative.Sources{Plots: mem, Households: mem, Diary: porttest.MemoryDiary{S: s}})
	ctx, reads := shareread.WithCollector(tenant.WithID(context.Background(), viewer))

	if p, err := m.Plots().Get(ctx, s.Plots[0].Ref); err != nil || !p.ViaShareGrant {
		t.Fatalf("thửa của đơn vị chủ: %+v %v", p, err)
	}
	if p, err := m.Plots().Get(ctx, s.Plots[1].Ref); err != nil || p.ViaShareGrant {
		t.Fatalf("thửa của chính đơn vị: %+v %v", p, err)
	}
	facts, err := m.Plots().Facts(ctx, []uuid.UUID{s.Plots[0].Ref, s.Plots[1].Ref, s.Plots[2].Ref})
	if err != nil || !facts[s.Plots[0].Ref].ViaShareGrant || facts[s.Plots[1].Ref].ViaShareGrant || facts[s.Plots[2].Ref].ViaShareGrant {
		t.Fatalf("Facts: %+v %v", facts, err)
	}
	if e, err := m.Diary().Get(ctx, s.Diary[0].LogRef); err != nil || !e.ViaShareGrant {
		t.Fatalf("nhật ký: %+v %v", e, err)
	}
	if es, err := m.Diary().BySeason(ctx, s.Diary[0].SeasonRef, uuid.Nil, 10); err != nil || len(es) != 1 || !es[0].ViaShareGrant {
		t.Fatalf("nhật ký theo vụ: %+v %v", es, err)
	}
	if h, err := m.Households().Membership(ctx, s.Members[0].Ref); err != nil || !h.ViaShareGrant {
		t.Fatalf("hộ: %+v %v", h, err)
	}
	got := map[string]int{}
	for _, r := range reads() {
		if r.OwnerTenant != owner {
			t.Fatalf("đơn vị chủ sai: %+v", r)
		}
		got[r.ModuleTable] = r.Rows
	}
	if got[app.SharePlot] != 2 || got[app.ShareDiaryEntry] != 2 || got[app.ShareHousehold] != 1 || len(got) != 3 {
		t.Fatalf("nhật ký gộp: %v", got)
	}
}
