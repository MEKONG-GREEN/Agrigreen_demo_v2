package porttest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
)

// Ports là bộ cổng của một hiện thực pf/cooperative.
type Ports struct {
	Cooperatives port.Cooperatives
	Households   port.Households
	Plots        port.Plots
	Seasons      port.Seasons
	Diary        port.Diary // nil hoặc trả ErrUnavailable: bỏ phần nhật ký (W3.2 trên port cũ)
	Resolve      port.Resolve
}

// SampleSeed trả dữ liệu mẫu dùng cho RunContract: hai HTX, ba nông hộ, ba thửa (một nháp), số liệu vụ.
func SampleSeed() Seed {
	tid := uuid.MustParse("01900000-0000-7000-8000-000000000001")
	c1 := uuid.MustParse("01900000-0000-7000-8000-0000000000c1")
	c2 := uuid.MustParse("01900000-0000-7000-8000-0000000000c2")
	h1 := uuid.MustParse("01900000-0000-7000-8000-0000000000a1")
	h2 := uuid.MustParse("01900000-0000-7000-8000-0000000000a2")
	h3 := uuid.MustParse("01900000-0000-7000-8000-0000000000a3")
	p1 := uuid.MustParse("01900000-0000-7000-8000-0000000000b1")
	p2 := uuid.MustParse("01900000-0000-7000-8000-0000000000b2")
	p3 := uuid.MustParse("01900000-0000-7000-8000-0000000000b3")
	s1 := uuid.MustParse("01900000-0000-7000-8000-0000000000d1")
	l1 := uuid.MustParse("01900000-0000-7000-8000-0000000000e1")
	lon, lat := decimal.RequireFromString("105.75"), decimal.RequireFromString("10.03")
	area := decimal.RequireFromString("1.25")
	return Seed{
		Tenant: tid,
		Units:  []port.UnitCooperatives{{UnitCode: "31117", Cooperatives: 1, Households: 2}, {UnitCode: "31120", Cooperatives: 1, Households: 1}},
		Sites: []port.CooperativeSite{
			{Ref: c1, Code: "HTX-001", CommuneCode: "31117", Lon: &lon, Lat: &lat, Members: 2},
			{Ref: c2, Code: "HTX-002", CommuneCode: "31120", Members: 1},
		},
		Centroids: []port.CoopCentroid{{CooperativeRef: c1, Lon: lon, Lat: lat}},
		Members: []port.HouseholdMembership{
			{Ref: h1, CooperativeRef: c1, CommuneCode: "31117", Active: true},
			{Ref: h2, CooperativeRef: c1, CommuneCode: "31117", Active: true},
			{Ref: h3, CommuneCode: "31120"},
		},
		PlotHouseholds: map[string][]uuid.UUID{"31117": {h1, h2}, "31120": {h3}},
		Plots: []port.Plot{
			{Ref: p1, Code: "T-1", CommuneCode: "31117", CooperativeRef: c1, HouseholdRef: h1, Status: port.PlotActive, Version: 2, AreaHa: area},
			{Ref: p2, Code: "T-2", CommuneCode: "31117", CooperativeRef: c1, HouseholdRef: h2, Status: port.PlotActive, Version: 1, AreaHa: area},
			{Ref: p3, Code: "T-3", CommuneCode: "31120", HouseholdRef: h3, Status: port.PlotDraft, Version: 1, AreaHa: area},
		},
		CommuneStats: []port.PlotStat{{CommuneCode: "31117", Plots: 2, AreaHa: area.Add(area)}, {CommuneCode: "31120", Plots: 1, AreaHa: area}},
		CoopStats:    []port.PlotStat{{CooperativeRef: c1, Plots: 2, AreaHa: area.Add(area)}},
		Activity:     []port.CoopActivity{{CooperativeRef: c1, OpenSeasons: 2, PendingLogs: 3}},
		Areas: []port.CoopSeasonArea{
			{CooperativeRef: c1, SeasonCode: "DX", SeasonYear: 2026, AreaHa: area, Seasons: 1, OpenSeasons: 1},
			{CooperativeRef: c1, SeasonCode: "HT", SeasonYear: 2026, AreaHa: area, Seasons: 1, CompletedSeasons: 1},
		},
		Supply: []port.CoopSeasonSupply{{CooperativeRef: c1, SeasonCode: "DX", SeasonYear: 2026, AreaHa: area, PaddyKg: decimal.NewFromInt(8000), Seasons: 1}},
		Production: port.ProductionData{
			Rows:   []port.ProductionRow{{CooperativeRef: c1, ProductionFigures: port.ProductionFigures{Seasons: 2, OpenSeasons: 1, AreaHa: area}}},
			Totals: port.ProductionFigures{Seasons: 2, OpenSeasons: 1, AreaHa: area},
		},
		Diary: []port.DiaryEntry{{ID: l1, LogRef: l1, Version: 1, SeasonRef: s1, PlotRef: p1, HouseholdRef: h1, CooperativeRef: c1,
			CommuneCode: "31117", ActivityCode: "bon_phan", Status: port.LogApproved}},
	}
}

// RunContract kiểm hợp đồng cổng trên SampleSeed: mk dựng hiện thực chứa đúng dữ liệu của seed;
// withTenant đặt tenant của seed vào context (kiểm thử gọi tenant.WithID — gói này không phải _test.go
// nên không được tự đổi tenant, archlint agtenantgate).
func RunContract(t *testing.T, withTenant func(context.Context, uuid.UUID) context.Context, mk func(t *testing.T, s Seed) Ports) {
	t.Helper()
	s := SampleSeed()
	p := mk(t, s)
	ctx := withTenant(context.Background(), s.Tenant)
	c1, h1, p1, p3 := s.Sites[0].Ref, s.Members[0].Ref, s.Plots[0].Ref, s.Plots[2].Ref
	missing := uuid.MustParse("01900000-0000-7000-8000-00000000ffff")

	t.Run("Cooperatives", func(t *testing.T) {
		all, err := p.Cooperatives.ByUnit(ctx, nil)
		if err != nil || len(all) != 2 {
			t.Fatalf("ByUnit(nil) = %v, %v", all, err)
		}
		one, err := p.Cooperatives.ByUnit(ctx, []string{"31120"})
		if err != nil || len(one) != 1 || one[0] != s.Units[1] {
			t.Fatalf("ByUnit(31120) = %v, %v", one, err)
		}
		none, err := p.Cooperatives.ByUnit(ctx, []string{})
		if err != nil || len(none) != 0 {
			t.Fatalf("ByUnit([]) = %v, %v", none, err)
		}
		sites, err := p.Cooperatives.Active(ctx)
		if err != nil || len(sites) != 2 || !reflect.DeepEqual(sites[0], s.Sites[0]) {
			t.Fatalf("Active = %+v, %v", sites, err)
		}
		cs, err := p.Cooperatives.Centroids(ctx, nil)
		if err != nil || len(cs) != 1 || !cs[c1].Lon.Equal(s.Centroids[0].Lon) {
			t.Fatalf("Centroids(nil) = %v, %v", cs, err)
		}
		cs, err = p.Cooperatives.Centroids(ctx, []uuid.UUID{s.Sites[1].Ref})
		if err != nil || len(cs) != 0 {
			t.Fatalf("Centroids(HTX chưa có vùng) = %v, %v", cs, err)
		}
	})

	t.Run("Households", func(t *testing.T) {
		m, err := p.Households.Membership(ctx, h1)
		if err != nil || m != s.Members[0] {
			t.Fatalf("Membership = %+v, %v", m, err)
		}
		if _, err := p.Households.Membership(ctx, missing); !errors.Is(err, port.ErrHouseholdNotFound) {
			t.Fatalf("Membership(không có) err = %v", err)
		}
		if _, err := p.Households.Membership(ctx, uuid.Nil); !errors.Is(err, port.ErrHouseholdNotFound) {
			t.Fatalf("Membership(Nil) err = %v", err)
		}
		hh, err := p.Households.InCommunes(ctx, []string{"31117"})
		if err != nil || len(hh) != 2 {
			t.Fatalf("InCommunes(31117) = %v, %v", hh, err)
		}
		hh, err = p.Households.InCommunes(ctx, []string{})
		if err != nil || len(hh) != 0 {
			t.Fatalf("InCommunes([]) = %v, %v", hh, err)
		}
	})

	t.Run("Plots", func(t *testing.T) {
		pl, err := p.Plots.Get(ctx, p1)
		if err != nil || pl.Code != "T-1" || pl.CooperativeRef != c1 || pl.Status != port.PlotActive || !pl.AreaHa.Equal(s.Plots[0].AreaHa) {
			t.Fatalf("Get = %+v, %v", pl, err)
		}
		if _, err := p.Plots.Get(ctx, missing); !errors.Is(err, port.ErrPlotNotFound) {
			t.Fatalf("Get(không có) err = %v", err)
		}
		if _, err := p.Plots.DistanceM(ctx, missing, decimal.Zero, decimal.Zero); !errors.Is(err, port.ErrPlotNotFound) {
			t.Fatalf("DistanceM(không có) err = %v", err)
		}
		facts, err := p.Plots.Facts(ctx, []uuid.UUID{p1, p3, missing})
		if err != nil || len(facts) != 2 || facts[p3].CooperativeRef != uuid.Nil {
			t.Fatalf("Facts = %v, %v", facts, err)
		}
		if facts, err := p.Plots.Facts(ctx, nil); err != nil || len(facts) != 0 {
			t.Fatalf("Facts(nil) = %v, %v", facts, err)
		}
		st, err := p.Plots.DigitizedByCommune(ctx, []string{"31117"})
		if err != nil || len(st) != 1 || st[0].Plots != 2 {
			t.Fatalf("DigitizedByCommune = %v, %v", st, err)
		}
		bc, err := p.Plots.DigitizedByCooperative(ctx, []uuid.UUID{c1, s.Sites[1].Ref})
		if err != nil || len(bc) != 1 || bc[c1].Plots != 2 {
			t.Fatalf("DigitizedByCooperative = %v, %v", bc, err)
		}
	})

	t.Run("Seasons", func(t *testing.T) {
		a, err := p.Seasons.ActivityByCooperative(ctx, []uuid.UUID{c1})
		if err != nil || a[c1].OpenSeasons != 2 || a[c1].PendingLogs != 3 {
			t.Fatalf("ActivityByCooperative = %v, %v", a, err)
		}
		if a, err := p.Seasons.ActivityByCooperative(ctx, []uuid.UUID{}); err != nil || len(a) != 0 {
			t.Fatalf("ActivityByCooperative([]) = %v, %v", a, err)
		}
		ar, err := p.Seasons.Areas(ctx, c1, "DX", 2026)
		if err != nil || len(ar) != 1 || ar[0].OpenSeasons != 1 {
			t.Fatalf("Areas = %v, %v", ar, err)
		}
		sp, err := p.Seasons.Supply(ctx, 2026)
		if err != nil || len(sp) != 1 || !sp[0].PaddyKg.Equal(decimal.NewFromInt(8000)) {
			t.Fatalf("Supply = %v, %v", sp, err)
		}
		pd, err := p.Seasons.Production(ctx, port.ProductionQuery{TenantID: s.Tenant, ByCooperative: true})
		if err != nil || len(pd.Rows) != 1 || pd.Rows[0].CooperativeRef != c1 || pd.Totals.Seasons != 2 {
			t.Fatalf("Production = %+v, %v", pd, err)
		}
		if hd, err := p.Seasons.HarvestDates(ctx, nil); err != nil || len(hd) != 0 {
			t.Fatalf("HarvestDates(nil) = %v, %v", hd, err)
		}
		if sc, err := p.Seasons.Codes(ctx, nil); err != nil || len(sc) != 0 {
			t.Fatalf("Codes(nil) = %v, %v", sc, err)
		}
		if _, err := p.Seasons.Progress(ctx, time.Time{}); err != nil {
			t.Fatalf("Progress = %v", err)
		}
	})

	diary := p.Diary != nil
	if diary {
		if _, err := p.Diary.Get(ctx, missing); errors.Is(err, port.ErrUnavailable) {
			diary = false
		}
	}
	if diary {
		t.Run("Diary", func(t *testing.T) {
			e, err := p.Diary.Get(ctx, s.Diary[0].LogRef)
			if err != nil || e.CooperativeRef != c1 || e.Status != port.LogApproved {
				t.Fatalf("Get = %+v, %v", e, err)
			}
			if _, err := p.Diary.Get(ctx, missing); !errors.Is(err, port.ErrDiaryEntryNotFound) {
				t.Fatalf("Get(không có) err = %v", err)
			}
			es, err := p.Diary.BySeason(ctx, s.Diary[0].SeasonRef, uuid.Nil, 0)
			if err != nil || len(es) != 1 {
				t.Fatalf("BySeason = %v, %v", es, err)
			}
		})
	}

	t.Run("Resolve", func(t *testing.T) {
		sub, err := p.Resolve.Resolve(ctx, port.SubjectPlot, p1)
		if err != nil || sub.TenantID != s.Tenant || sub.NodeRef != c1 || sub.HouseholdRef != h1 || sub.CommuneCode != "31117" || sub.Status != port.PlotActive {
			t.Fatalf("Resolve(plot) = %+v, %v", sub, err)
		}
		sub, err = p.Resolve.Resolve(ctx, port.SubjectPlot, p3)
		if err != nil || sub.NodeRef != uuid.Nil {
			t.Fatalf("Resolve(thửa nháp) = %+v, %v", sub, err)
		}
		sub, err = p.Resolve.Resolve(ctx, port.SubjectHousehold, h1)
		if err != nil || sub.NodeRef != c1 || sub.Status != "active" {
			t.Fatalf("Resolve(household) = %+v, %v", sub, err)
		}
		sub, err = p.Resolve.Resolve(ctx, port.SubjectCooperative, c1)
		if err != nil || sub.NodeRef != c1 || sub.CommuneCode != "31117" {
			t.Fatalf("Resolve(cooperative) = %+v, %v", sub, err)
		}
		for _, typ := range []port.SubjectType{port.SubjectPlot, port.SubjectHousehold, port.SubjectCooperative} {
			if _, err := p.Resolve.Resolve(ctx, typ, missing); !errors.Is(err, port.ErrSubjectNotFound) {
				t.Fatalf("Resolve(%s không có) err = %v", typ, err)
			}
		}
		if _, err := p.Resolve.Resolve(ctx, "tractor", p1); !errors.Is(err, port.ErrSubjectTypeUnsupported) {
			t.Fatalf("Resolve(loại lạ) err = %v", err)
		}
		if diary {
			sub, err = p.Resolve.Resolve(ctx, port.SubjectDiaryEntry, s.Diary[0].LogRef)
			if err != nil || sub.NodeRef != c1 || sub.Status != port.LogApproved {
				t.Fatalf("Resolve(diary_entry) = %+v, %v", sub, err)
			}
		}
		if _, err := p.Resolve.Resolve(context.Background(), port.SubjectPlot, p1); err == nil {
			t.Fatal("Resolve không có tenant trong context phải lỗi")
		}
	})
}
