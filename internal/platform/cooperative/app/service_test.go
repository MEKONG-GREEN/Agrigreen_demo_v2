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
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mặt tiền trên bản giả trong bộ nhớ qua bộ kiểm hợp đồng cổng.
func TestFacadeContract(t *testing.T) {
	porttest.RunContract(t, tenant.WithID, func(_ *testing.T, s porttest.Seed) porttest.Ports {
		mem := porttest.Memory{S: s}
		m := cooperative.New(cooperative.Sources{Cooperatives: mem, Households: mem, Plots: mem, Seasons: mem, Diary: porttest.MemoryDiary{S: s}})
		return porttest.Ports{Cooperatives: m.Cooperatives(), Households: m.Households(), Plots: m.Plots(), Seasons: m.Seasons(),
			Diary: m.Diary(), Resolve: m.Resolve()}
	})
}

// Nguồn chưa nối → ErrUnavailable trên mọi cổng (bên gọi trả 503), không panic.
func TestFacadeUnwired(t *testing.T) {
	m := cooperative.New(cooperative.Sources{})
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

// Nối dần bằng Use* (gốc lắp ráp dựng nguồn vụ sau pf/cropcycle).
func TestFacadeUseLate(t *testing.T) {
	s := porttest.SampleSeed()
	m := cooperative.New(cooperative.Sources{})
	m.UseSeasons(porttest.Memory{S: s})
	m.UsePlots(porttest.Memory{S: s})
	ctx := tenant.WithID(context.Background(), s.Tenant)
	if rows, err := m.Seasons().Supply(ctx, 2026); err != nil || len(rows) != 1 {
		t.Fatalf("Supply sau UseSeasons = %v, %v", rows, err)
	}
	if _, err := m.Plots().Get(ctx, s.Plots[0].Ref); err != nil {
		t.Fatalf("Get sau UsePlots: %v", err)
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
	m := cooperative.New(cooperative.Sources{Diary: spy})
	for in, want := range map[int]int{-1: app.DefaultDiaryLimit, 0: app.DefaultDiaryLimit, 7: 7, 10_000: app.MaxDiaryLimit} {
		if _, err := m.Diary().BySeason(context.Background(), uuid.New(), uuid.Nil, in); err != nil {
			t.Fatal(err)
		}
		if spy.limit != want {
			t.Fatalf("limit %d → %d, muốn %d", in, spy.limit, want)
		}
	}
}

// Season, harvest chưa phân giải được ở W3.2 (store riêng W3.3).
func TestResolveSeasonUnsupportedInW32(t *testing.T) {
	s := porttest.SampleSeed()
	mem := porttest.Memory{S: s}
	m := cooperative.New(cooperative.Sources{Cooperatives: mem, Households: mem, Plots: mem, Seasons: mem})
	ctx := tenant.WithID(context.Background(), s.Tenant)
	for _, typ := range []port.SubjectType{port.SubjectSeason, port.SubjectHarvest} {
		if _, err := m.Resolve().Resolve(ctx, typ, uuid.New()); !errors.Is(err, port.ErrSubjectTypeUnsupported) {
			t.Fatalf("%s: err = %v", typ, err)
		}
	}
}
