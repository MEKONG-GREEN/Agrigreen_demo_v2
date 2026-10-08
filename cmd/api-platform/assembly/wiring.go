package assembly

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	simport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	wwport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Bộ chuyển giữa port của các module (R-02: module chỉ thấy port của nhau; gốc lắp ráp
// ghép). Không có logic nghiệp vụ ngoài việc đổi dạng dữ liệu.

// ---------------------------------------------------------------------------
// Cảnh báo khẩn tới nông dân theo địa bàn (HTX US-NEWS-03): pf/cooperative + pf/accounts → pf/notify
// ---------------------------------------------------------------------------

// AreaFarmers ghép nông hộ có thửa trong xã (pf/cooperative) với tài khoản nông dân liên kết
// (pf/accounts) thành notify/port.AreaFarmers.
type AreaFarmers struct {
	Households coopport.Households
	Accounts   accport.FarmerAccounts
}

// FarmersInCommunes thực hiện notify/port.AreaFarmers.
func (a AreaFarmers) FarmersInCommunes(ctx context.Context, tid uuid.UUID, communes []string) ([]uuid.UUID, error) {
	ctx = tenant.WithID(ctx, tid)
	hh, err := a.Households.InCommunes(ctx, communes)
	if err != nil || len(hh) == 0 {
		return nil, err
	}
	return a.Accounts.SubjectsForHouseholds(ctx, hh)
}

// ---------------------------------------------------------------------------
// Cảnh báo trước khi vô hiệu hóa HTX (HTX US-HTXSTATUS-01 AC-1): pf/cooperative → pf/party
// ---------------------------------------------------------------------------

// SeasonActivity chuyển cooperative/port.Seasons sang party/port.SeasonActivity (party là kernel,
// không import cooperativeport; W3.3 thay bằng OrgImpactSource do pf/cooperative hiện thực).
type SeasonActivity struct{ Seasons coopport.Seasons }

// CoopActivity thực hiện party/port.SeasonActivity.
func (s SeasonActivity) CoopActivity(ctx context.Context, coop uuid.UUID) (int64, int64, error) {
	m, err := s.Seasons.ActivityByCooperative(ctx, []uuid.UUID{coop})
	if err != nil {
		return 0, 0, err
	}
	a := m[coop]
	return a.OpenSeasons, a.PendingLogs, nil
}

// ---------------------------------------------------------------------------
// Nguồn cung của mô phỏng chọn Hub: pf/cooperative + pf/masterdata
// ---------------------------------------------------------------------------

// SupplySource thực hiện simulation/port.SupplySource:
//   - #4 diện tích, #5 sản lượng lúa theo HTX × vụ của năm tham chiếu: Seasons.Supply
//     (vụ đã khai sản lượng → thực tế; chưa khai → dự kiến theo năng suất kỳ vọng của giống);
//   - #3 toạ độ: tâm vùng HTX (Cooperatives.Centroids), không có → vị trí hồ sơ HTX
//     (Cooperatives.Active), không có nữa → nil (simulation loại trước khi định tuyến);
//   - #6 số hộ: hộ thành viên đang mở (Cooperatives.Active);
//   - #7 lịch mùa vụ: danh mục season (pf/masterdata, attrs end_month), thứ tự theo danh mục.
type SupplySource struct {
	Seasons  coopport.Seasons
	Coops    coopport.Cooperatives
	Catalogs mdport.Catalogs
}

var kgPerTon = decimal.NewFromInt(1000)

// Supply thực hiện simulation/port.SupplySource.
func (s SupplySource) Supply(ctx context.Context, tid uuid.UUID, q simport.SupplyQuery) (simport.SupplyData, error) {
	if s.Seasons == nil || s.Coops == nil {
		return simport.SupplyData{}, fmt.Errorf("assembly: nguồn cung chưa nối pf/cooperative")
	}
	ctx = tenant.WithID(ctx, tid)
	defs, err := s.seasonDefs(ctx)
	if err != nil {
		return simport.SupplyData{}, err
	}
	known := map[string]bool{}
	for _, d := range defs {
		known[d.Key] = true
	}
	rows, err := s.Seasons.Supply(ctx, q.ReferenceYear)
	if err != nil {
		return simport.SupplyData{}, err
	}
	byCoop := map[uuid.UUID][]simport.CoopSeason{}
	for _, r := range rows {
		key := strings.ToUpper(r.SeasonCode)
		if !known[key] {
			continue // vụ không có trong lịch mùa vụ: không xếp được theo tháng
		}
		byCoop[r.CooperativeRef] = append(byCoop[r.CooperativeRef], simport.CoopSeason{
			SeasonKey: key, PaddyTons: r.PaddyKg.Div(kgPerTon).Round(6), PlantedAreaHa: r.AreaHa})
	}
	sites, err := s.Coops.Active(ctx)
	if err != nil {
		return simport.SupplyData{}, err
	}
	centroids, err := s.Coops.Centroids(ctx, nil)
	if err != nil && !errors.Is(err, coopport.ErrUnavailable) { // chưa nối vùng HTX: dùng vị trí hồ sơ
		return simport.SupplyData{}, err
	}
	out := simport.SupplyData{Seasons: defs}
	for _, site := range sites {
		seasons := byCoop[site.Ref]
		if len(seasons) == 0 {
			continue
		}
		c := simport.CooperativeSupply{CooperativeRef: site.Ref, MemberCount: int(site.Members), Seasons: seasons}
		if ct, ok := centroids[site.Ref]; ok {
			c.Location = &simport.LatLng{Lat: ct.Lat.InexactFloat64(), Lng: ct.Lon.InexactFloat64()}
		} else if site.Lat != nil && site.Lon != nil {
			c.Location = &simport.LatLng{Lat: site.Lat.InexactFloat64(), Lng: site.Lon.InexactFloat64()}
		}
		out.Cooperatives = append(out.Cooperatives, c)
	}
	return out, nil
}

// defaultSeasons là lịch mùa vụ ĐBSCL khi chưa có danh mục season (khớp seeddata).
var defaultSeasons = []simport.SeasonDef{
	{Key: "DX", Type: "DX", EndMonth: 3, SortOrder: 1},
	{Key: "HT", Type: "HT", EndMonth: 8, SortOrder: 2},
	{Key: "TD", Type: "TD", EndMonth: 12, SortOrder: 3},
}

func (s SupplySource) seasonDefs(ctx context.Context) ([]simport.SeasonDef, error) {
	if s.Catalogs == nil {
		return defaultSeasons, nil
	}
	items, err := s.Catalogs.List(ctx, mdport.CatalogSeason)
	if err != nil {
		return nil, err
	}
	var out []simport.SeasonDef
	for i, it := range items {
		if !it.Active {
			continue
		}
		m, err := strconv.Atoi(strings.TrimSpace(it.Attrs["end_month"]))
		if err != nil || m < 1 || m > 12 {
			continue
		}
		code := strings.ToUpper(it.Code)
		out = append(out, simport.SeasonDef{Key: code, Type: code, EndMonth: m, SortOrder: i + 1})
	}
	if len(out) == 0 {
		return defaultSeasons, nil
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
	return out, nil
}

// ---------------------------------------------------------------------------
// Bộ định tuyến của mô phỏng: pf/waterway Router (phiên bản mạng ghim theo ảnh chụp)
// ---------------------------------------------------------------------------

// WaterwayRouter thực hiện simulation/port.Router trên waterway/port.Router:
//   - NetworkVersion = số phiên bản mạng đang phát hành lúc dựng ảnh chụp ("" nếu chưa
//     có → simulation ghi "none"); Route định tuyến ĐÚNG phiên bản đó (ghim, kể cả khi
//     đã có phiên bản mới phát hành giữa chừng);
//   - chặng đường thủy: có đường liên thông → khoảng cách thực + nhãn "routing_thuc_te" /
//     "tuyen_so_hoa_noi_bo"; không có → Found = false (simulation dùng Haversine × #10,
//     coi như không tiếp giáp mạng, FN-08 BR-04b);
//   - chặng đường bộ: chưa có bộ định tuyến đường bộ ở nền tảng → Found = false
//     (Haversine × hệ số #10 đường bộ, FN-04 BR-03).
type WaterwayRouter struct{ WW wwport.Router }

// NetworkVersion thực hiện simulation/port.Router.
func (r WaterwayRouter) NetworkVersion(ctx context.Context, tid uuid.UUID) (string, error) {
	v, err := r.WW.CurrentVersion(tenant.WithID(ctx, tid))
	if err != nil || v == 0 {
		return "", err
	}
	return strconv.Itoa(v), nil
}

// Route thực hiện simulation/port.Router.
func (r WaterwayRouter) Route(ctx context.Context, tid uuid.UUID, networkVersion string, reqs []simport.RouteRequest) ([]simport.RouteResult, error) {
	out := make([]simport.RouteResult, len(reqs))
	v, err := strconv.Atoi(networkVersion)
	if err != nil || v <= 0 {
		return out, nil // chưa có mạng phát hành ("none"): mọi chặng Haversine
	}
	ctx = tenant.WithID(ctx, tid)
	for i, q := range reqs {
		if q.Mode != simport.ModeWaterway {
			continue
		}
		rt, err := r.WW.RouteBetween(ctx, []wwport.LatLng{{Lat: q.From.Lat, Lng: q.From.Lng}, {Lat: q.To.Lat, Lng: q.To.Lng}}, decimal.Zero, v)
		if err != nil {
			return nil, err
		}
		if !rt.Found {
			continue
		}
		src := simport.SourceRouted
		if rt.Source == wwport.SourceDigitised {
			src = simport.SourceDigitized
		}
		out[i] = simport.RouteResult{Found: true, DistanceKm: rt.DistanceKm, Source: src}
	}
	return out, nil
}
