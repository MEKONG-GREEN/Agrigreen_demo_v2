package assembly

import (
	"context"
	"os"
	"slices"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	simport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client/clienttest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

// ---- giả lập port ----

// fakeSupply cài coopport.Seasons.Supply (cổng khác không dùng: nil nhúng).
type fakeSupply struct {
	coopport.Seasons
	rows []coopport.CoopSeasonSupply
}

func (f fakeSupply) Supply(ctx context.Context, year int) ([]coopport.CoopSeasonSupply, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return nil, err
	}
	var out []coopport.CoopSeasonSupply
	for _, r := range f.rows {
		if r.SeasonYear == year {
			out = append(out, r)
		}
	}
	return out, nil
}

// fakeCoops cài coopport.Cooperatives.Active + Centroids (ByUnit không dùng: nil nhúng).
type fakeCoops struct {
	coopport.Cooperatives
	sites     []coopport.CooperativeSite
	centroids map[uuid.UUID]coopport.CoopCentroid
}

func (f fakeCoops) Active(context.Context) ([]coopport.CooperativeSite, error) { return f.sites, nil }

func (f fakeCoops) Centroids(context.Context, []uuid.UUID) (map[uuid.UUID]coopport.CoopCentroid, error) {
	if f.centroids == nil {
		return nil, coopport.ErrUnavailable
	}
	return f.centroids, nil
}

type fakeCatalogs []mdport.CatalogItem

func (f fakeCatalogs) Get(context.Context, string, string) (mdport.CatalogItem, error) {
	return mdport.CatalogItem{}, mdport.ErrCatalogItemNotFound
}
func (f fakeCatalogs) List(context.Context, string) ([]mdport.CatalogItem, error) { return f, nil }

type fakeWW struct {
	current  int
	versions []int
}

func (f *fakeWW) CurrentVersion(context.Context) (int, error) { return f.current, nil }
func (f *fakeWW) RouteBetween(ctx context.Context, pts []wwport.LatLng, _ decimal.Decimal, v int) (wwport.Route, error) {
	f.versions = append(f.versions, v)
	if pts[0].Lat > 11 { // ngoài mạng
		return wwport.Route{VersionNo: v, Found: false, Source: wwport.SourceHaversine}, nil
	}
	src := wwport.SourceRouted
	if pts[0].Lat > 10.5 {
		src = wwport.SourceDigitised
	}
	return wwport.Route{VersionNo: v, Found: true, DistanceKm: decimal.RequireFromString("12.345"), Source: src}, nil
}

func dec(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

// Mô phỏng chọn Hub #3–#7 (simulation SupplySource): sản lượng HTX × vụ từ pf/cropcycle
// (kg → tấn), toạ độ = tâm ranh giới HTX (pf/geo) rồi vị trí hồ sơ (pf/party), số hộ
// (pf/party), lịch mùa vụ theo danh mục season; HTX không có vụ trong năm bị bỏ; vụ ngoài
// lịch mùa vụ bị bỏ.
func TestSupplySourceFromCropcyclePartyGeo(t *testing.T) {
	c1, c2, c3 := ids.New(), ids.New(), ids.New()
	src := SupplySource{
		Seasons: fakeSupply{rows: []coopport.CoopSeasonSupply{
			{CooperativeRef: c1, SeasonCode: "DX", SeasonYear: 2025, AreaHa: decimal.RequireFromString("120.5"), PaddyKg: decimal.RequireFromString("813375")},
			{CooperativeRef: c1, SeasonCode: "XX", SeasonYear: 2025, AreaHa: decimal.NewFromInt(1), PaddyKg: decimal.NewFromInt(1)},
			{CooperativeRef: c2, SeasonCode: "ht", SeasonYear: 2025, AreaHa: decimal.NewFromInt(10), PaddyKg: decimal.NewFromInt(60000)},
			{CooperativeRef: c3, SeasonCode: "DX", SeasonYear: 2024, AreaHa: decimal.NewFromInt(5), PaddyKg: decimal.NewFromInt(30000)},
		}},
		Coops: fakeCoops{sites: []coopport.CooperativeSite{
			{Ref: c1, Members: 42, Lon: dec("105.1"), Lat: dec("10.1")},
			{Ref: c2, Members: 7, Lon: dec("105.9"), Lat: dec("10.9")},
			{Ref: c3, Members: 3},
		}, centroids: map[uuid.UUID]coopport.CoopCentroid{c1: {CooperativeRef: c1, Lon: decimal.RequireFromString("105.5"), Lat: decimal.RequireFromString("10.5")}}},
		Catalogs: fakeCatalogs{
			{Catalog: "season", Code: "DX", Active: true, Attrs: map[string]string{"end_month": "3"}},
			{Catalog: "season", Code: "HT", Active: true, Attrs: map[string]string{"end_month": "8"}},
			{Catalog: "season", Code: "TD", Active: false, Attrs: map[string]string{"end_month": "12"}},
		},
	}
	got, err := src.Supply(context.Background(), ids.New(), simport.SupplyQuery{ReferenceYear: 2025})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Seasons) != 2 || got.Seasons[0].Key != "DX" || got.Seasons[0].EndMonth != 3 || got.Seasons[1].Key != "HT" {
		t.Fatalf("lịch mùa vụ: %+v", got.Seasons)
	}
	if len(got.Cooperatives) != 2 {
		t.Fatalf("HTX có vụ năm 2025: %+v", got.Cooperatives)
	}
	for _, c := range got.Cooperatives {
		switch c.CooperativeRef {
		case c1:
			if c.MemberCount != 42 || c.Location == nil || c.Location.Lat != 10.5 || len(c.Seasons) != 1 ||
				c.Seasons[0].PaddyTons.String() != "813.375" || c.Seasons[0].PlantedAreaHa.String() != "120.5" {
				t.Fatalf("c1 (tâm ranh giới, bỏ vụ lạ): %+v", c)
			}
		case c2:
			if c.Location == nil || c.Location.Lng != 105.9 || c.Seasons[0].SeasonKey != "HT" || c.Seasons[0].PaddyTons.String() != "60" {
				t.Fatalf("c2 (vị trí hồ sơ): %+v", c)
			}
		default:
			t.Fatalf("HTX lạ: %+v", c)
		}
	}
	// Không có danh mục → lịch mặc định ĐBSCL.
	src.Catalogs = nil
	got, _ = src.Supply(context.Background(), ids.New(), simport.SupplyQuery{ReferenceYear: 2025})
	if len(got.Seasons) != 3 {
		t.Fatalf("lịch mặc định: %+v", got.Seasons)
	}
}

// Simulation FN-04/FN-08 (mục 16): router ghim ĐÚNG phiên bản mạng của ảnh chụp; nhãn
// nguồn theo simulation; đường bộ và chặng không có đường thủy → Found = false (Haversine
// × #10 ở simulation); chưa có mạng → "none", không gọi waterway.
func TestWaterwayRouterPinsVersionAndLabels(t *testing.T) {
	ww := &fakeWW{current: 3}
	r := WaterwayRouter{WW: ww}
	tid := ids.New()
	v, err := r.NetworkVersion(context.Background(), tid)
	if err != nil || v != "3" {
		t.Fatalf("phiên bản: %q %v", v, err)
	}
	ww.current = 4 // phát hành bản mới giữa lúc dựng ảnh chụp → vẫn định tuyến bản 3
	reqs := []simport.RouteRequest{
		{From: simport.LatLng{Lat: 10.1, Lng: 105.1}, To: simport.LatLng{Lat: 10.2, Lng: 105.2}, Mode: simport.ModeWaterway},
		{From: simport.LatLng{Lat: 10.6, Lng: 105.1}, To: simport.LatLng{Lat: 10.2, Lng: 105.2}, Mode: simport.ModeWaterway},
		{From: simport.LatLng{Lat: 11.5, Lng: 105.1}, To: simport.LatLng{Lat: 10.2, Lng: 105.2}, Mode: simport.ModeWaterway},
		{From: simport.LatLng{Lat: 10.1, Lng: 105.1}, To: simport.LatLng{Lat: 10.2, Lng: 105.2}, Mode: simport.ModeRoad},
	}
	res, err := r.Route(context.Background(), tid, v, reqs)
	if err != nil || len(res) != 4 {
		t.Fatal(err)
	}
	if !res[0].Found || res[0].Source != simport.SourceRouted || res[0].DistanceKm.String() != "12.345" ||
		!res[1].Found || res[1].Source != simport.SourceDigitized || res[2].Found || res[3].Found {
		t.Fatalf("kết quả: %+v", res)
	}
	if !slices.Equal(ww.versions, []int{3, 3, 3}) {
		t.Fatalf("phiên bản ghim: %v", ww.versions)
	}
	ww.current, ww.versions = 0, nil
	if v, _ := r.NetworkVersion(context.Background(), tid); v != "" {
		t.Fatalf("chưa có mạng: %q", v)
	}
	if res, _ := r.Route(context.Background(), tid, "none", reqs); res[0].Found || len(ww.versions) != 0 {
		t.Fatalf("mạng none: %+v", res)
	}
}

// fakePlotHH cài coopport.Households.InCommunes (Membership không dùng: nil nhúng).
type fakePlotHH struct {
	coopport.Households
	m map[string][]uuid.UUID
}

func (f fakePlotHH) InCommunes(ctx context.Context, communes []string) ([]uuid.UUID, error) {
	if _, err := tenant.FromContext(ctx); err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for c, hh := range f.m {
		if communes == nil || slices.Contains(communes, c) {
			out = append(out, hh...)
		}
	}
	return out, nil
}

type fakeFarmerAcc map[uuid.UUID]uuid.UUID

func (f fakeFarmerAcc) SubjectsForHouseholds(_ context.Context, hh []uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, h := range hh {
		if u, ok := f[h]; ok {
			out = append(out, u)
		}
	}
	return out, nil
}

var _ accport.FarmerAccounts = fakeFarmerAcc{}

// fakeStats cài coopport.Seasons.ActivityByCooperative (cổng khác không dùng: nil nhúng).
type fakeStats struct {
	coopport.Seasons
	m map[uuid.UUID]coopport.CoopActivity
}

func (f fakeStats) ActivityByCooperative(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]coopport.CoopActivity, error) {
	out := map[uuid.UUID]coopport.CoopActivity{}
	for _, c := range coops {
		if a, ok := f.m[c]; ok {
			out[c] = a
		}
	}
	return out, nil
}

// HTX US-NEWS-03 (nông hộ có thửa của pf/cooperative → tài khoản pf/accounts) và HTX US-HTXSTATUS-01 AC-1
// (vụ mở / nhật ký chờ duyệt của pf/cooperative cho pf/party).
func TestAreaFarmersAndSeasonActivityAdapters(t *testing.T) {
	h1, h2, h3, u1, u2 := ids.New(), ids.New(), ids.New(), ids.New(), ids.New()
	a := AreaFarmers{Households: fakePlotHH{m: map[string][]uuid.UUID{"31117": {h1, h2}, "31120": {h3}}}, Accounts: fakeFarmerAcc{h1: u1, h3: u2}}
	got, err := a.FarmersInCommunes(context.Background(), ids.New(), []string{"31117"})
	if err != nil || !slices.Equal(got, []uuid.UUID{u1}) {
		t.Fatalf("nông dân xã 31117 (h2 chưa có tài khoản): %v %v", got, err)
	}
	if got, _ := a.FarmersInCommunes(context.Background(), ids.New(), []string{"99999"}); len(got) != 0 {
		t.Fatalf("xã không có thửa: %v", got)
	}
	coop := ids.New()
	sa := SeasonActivity{Seasons: fakeStats{m: map[uuid.UUID]coopport.CoopActivity{coop: {CooperativeRef: coop, OpenSeasons: 4, PendingLogs: 9}}}}
	if o, p, err := sa.CoopActivity(context.Background(), coop); err != nil || o != 4 || p != 9 {
		t.Fatalf("hoạt động HTX: %d %d %v", o, p, err)
	}
	if o, p, _ := sa.CoopActivity(context.Background(), ids.New()); o != 0 || p != 0 {
		t.Fatal("HTX không có hoạt động → 0")
	}
}

// Gốc lắp ráp: Build nối mọi consumer liên module (handler khác nil), nguồn cung và router
// của mô phỏng chạy được trên DB thật (tenant rỗng → không có HTX, chưa có mạng).
func TestBuildWiresG2Consumers(t *testing.T) {
	env := pgtest.Get(t)
	mods, _, err := Build(context.Background(), func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return env.Pool(t, migrations.PF, role), nil
	}, clock.System{}, External{Vault: clienttest.New()})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, c := range Consumers {
		if names[c.Name] || !c.Module.Valid() || len(c.Events) == 0 || c.Handler(mods) == nil {
			t.Fatalf("consumer %s không hợp lệ", c.Name)
		}
		names[c.Name] = true
	}
	tid := ids.New()
	src := SupplySource{Seasons: mods.Cooperative.Seasons(), Coops: mods.Cooperative.Cooperatives(), Catalogs: mods.MasterData.Catalogs()}
	sup, err := src.Supply(context.Background(), tid, simport.SupplyQuery{ReferenceYear: 2026})
	if err != nil || len(sup.Cooperatives) != 0 || len(sup.Seasons) == 0 {
		t.Fatalf("nguồn cung tenant rỗng: %+v %v", sup, err)
	}
	if v, err := (WaterwayRouter{WW: mods.Waterway.Router()}).NetworkVersion(context.Background(), tid); err != nil || v != "" {
		t.Fatalf("chưa có mạng: %q %v", v, err)
	}
	af := AreaFarmers{Households: mods.Cooperative.Households(), Accounts: mods.Accounts.FarmerAccounts()}
	if got, err := af.FarmersInCommunes(context.Background(), tid, nil); err != nil || len(got) != 0 {
		t.Fatalf("nông dân tenant rỗng: %v %v", got, err)
	}
}
