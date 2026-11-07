package app

// Lớp bản đồ theo ngày (FN-18 B1–B2): vùng, ổ dịch BM-10 (W11.49), điểm trình diễn và hộ mô hình có tọa độ (W11.47),
// đối tượng trong vùng (vùng đệm 3 km, 10 km…): điểm, hộ, ổ dịch nằm trong hình của vùng.
//
// CNTS v4.0 (CR-CNTS-001, #254): lớp trang trại — hộ chăn nuôi Đang dùng loại trang trại, vị trí check-in hiện hành, khóa
// household_ref (ADR-0056 mục 9); vùng dịch, vùng bị uy hiếp, vùng đệm hiện số hộ bị ảnh hưởng và diện tích thủy sản bị
// ảnh hưởng của các ổ dịch trong vùng (FN-18 BR-02, FN-11 BR-03: lấy số của lần cập nhật diễn biến mới nhất, không cộng
// dồn các lần).

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/store"
	obdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// OutbreakView là ổ dịch trên bản đồ.
type OutbreakView struct {
	store.Outbreak
	Status string // reported | monitoring | closed (Đã gửi / Đang theo dõi / Đã kết thúc — W11.49)
}

// Households trả DB-15 của phiên bản; phiên bản v1 (dữ liệu cũ một hộ) tính 1 hộ (ADR-0056 mục 8).
func (o OutbreakView) Households() int {
	if o.AffectedHouseholds != nil {
		return *o.AffectedHouseholds
	}
	return 1
}

// Legacy: phiên bản ghi trước CNTS v4.0 (ổ dịch một hộ — ADR-0056 mục 8).
func (o OutbreakView) Legacy() bool { return o.RuleVersion != obdomain.RuleVersion }

// Impact là số ổ dịch, số hộ bị ảnh hưởng (DB-15) và diện tích thủy sản bị ảnh hưởng (DB-16: nuôi trồng, khai thác) của
// các ổ dịch nằm trong một vùng (FN-18 BR-02).
type Impact struct {
	Outbreaks    int
	Households   int
	AquaFarmedHa decimal.Decimal
	AquaWildHa   decimal.Decimal
}

// impactOf cộng số của phiên bản diễn biến mới nhất của từng ổ dịch (FN-11 BR-03 — mỗi ổ dịch một phiên bản, không cộng
// dồn các lần cập nhật). DB-16 trống (bệnh không thuộc nhóm thủy sản, dữ liệu cũ) tính 0.
func impactOf(obs []OutbreakView) Impact {
	im := Impact{AquaFarmedHa: decimal.Zero, AquaWildHa: decimal.Zero}
	for _, o := range obs {
		im.Outbreaks++
		im.Households += o.Households()
		if o.AquaFarmedHa != nil {
			im.AquaFarmedHa = im.AquaFarmedHa.Add(*o.AquaFarmedHa)
		}
		if o.AquaWildHa != nil {
			im.AquaWildHa = im.AquaWildHa.Add(*o.AquaWildHa)
		}
	}
	return im
}

// ZoneImpact là Impact của một vùng trên bản đồ.
type ZoneImpact struct {
	ZoneRef uuid.UUID
	Impact
}

func outbreakStatus(event string) string {
	switch event {
	case "closed":
		return "closed"
	case "reported":
		return "reported"
	}
	return "monitoring"
}

// Layers là dữ liệu bản đồ của một ngày.
type Layers struct {
	Day       time.Time
	Zones     []ZoneView
	Outbreaks []OutbreakView
	Places    []store.Place
	Farms     []store.Farm // lớp trang trại (FN-18, CR-CNTS-001)
	Impacts   []ZoneImpact // số hộ, diện tích bị ảnh hưởng của vùng dịch, vùng bị uy hiếp, vùng đệm (FN-18 BR-02)
	CanDraw   bool
	Catalog   map[string]string // mã danh mục (bệnh, loài, loại mô hình) → tên
}

// Layers đọc mọi lớp của bản đồ tại ngày day (rỗng = hôm nay) trong phạm vi người gọi.
func (s *Service) Layers(ctx context.Context, dayText string) (Layers, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Layers{}, err
	}
	d, err := s.parseDay(dayText)
	if err != nil {
		return Layers{}, err
	}
	var zs []store.Zone
	var obs []store.Outbreak
	var places []store.Place
	var farms []store.Farm
	var inZones []store.ZoneOutbreak
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if zs, e = store.ZonesAt(ctx, tx, store.ZoneFilter{Day: d}); e != nil {
			return e
		}
		if obs, e = store.OutbreaksAt(ctx, tx, d, nil); e != nil {
			return e
		}
		if places, e = store.Places(ctx, tx, nil); e != nil {
			return e
		}
		if farms, e = store.Farms(ctx, tx, nil); e != nil {
			return e
		}
		var zoneIDs []uuid.UUID
		for _, z := range zs {
			if domain.ImpactKind(z.Kind) && c.seesZone(z) {
				zoneIDs = append(zoneIDs, z.ID)
			}
		}
		inZones, e = store.ZoneOutbreaks(ctx, tx, d, zoneIDs)
		return e
	}); err != nil {
		return Layers{}, err
	}
	out := Layers{Day: d, Zones: []ZoneView{}, CanDraw: c.drawer(), Catalog: s.catalog(ctx)}
	for _, z := range zs {
		if c.seesZone(z) {
			out.Zones = append(out.Zones, c.view(z))
		}
	}
	out.Outbreaks, out.Places, out.Farms = c.outbreaks(obs), c.places(places), c.farms(farms)
	out.Impacts = zoneImpacts(out.Zones, out.Outbreaks, inZones)
	return out, nil
}

// zoneImpacts tính Impact cho mỗi vùng dịch, vùng bị uy hiếp, vùng đệm trong zones, chỉ từ ổ dịch người gọi thấy (Cấp Xã:
// ổ dịch xã mình — FN-18 AC-02).
func zoneImpacts(zones []ZoneView, visible []OutbreakView, in []store.ZoneOutbreak) []ZoneImpact {
	byRef := make(map[uuid.UUID]OutbreakView, len(visible))
	for _, o := range visible {
		byRef[o.Ref] = o
	}
	byZone := map[uuid.UUID][]OutbreakView{}
	for _, x := range in {
		if o, ok := byRef[x.OutbreakRef]; ok {
			byZone[x.ZoneID] = append(byZone[x.ZoneID], o)
		}
	}
	out := []ZoneImpact{}
	for _, z := range zones {
		if domain.ImpactKind(z.Kind) {
			out = append(out, ZoneImpact{ZoneRef: z.Ref, Impact: impactOf(byZone[z.ID])})
		}
	}
	return out
}

func (c caller) outbreaks(in []store.Outbreak) []OutbreakView {
	out := []OutbreakView{}
	for _, o := range in {
		if c.sees(o.Commune) {
			out = append(out, OutbreakView{Outbreak: o, Status: outbreakStatus(o.Event)})
		}
	}
	return out
}

func (c caller) places(in []store.Place) []store.Place {
	out := []store.Place{}
	for _, p := range in {
		if c.sees(p.Commune) {
			out = append(out, p)
		}
	}
	return out
}

func (c caller) farms(in []store.Farm) []store.Farm {
	out := []store.Farm{}
	for _, f := range in {
		if c.sees(f.Commune) {
			out = append(out, f)
		}
	}
	return out
}

func (s *Service) catalog(ctx context.Context) map[string]string {
	out := map[string]string{}
	for _, cat := range []string{knport.CatalogCNTSDisease, knport.CatalogCNTSSpecies, knport.CatalogCNTSModelType} {
		for k, v := range s.catalogNames(ctx, cat) {
			out[cat+":"+k] = v
		}
	}
	return out
}

// ZoneContents là đối tượng nằm trong một vùng tại một ngày.
type ZoneContents struct {
	Zone      ZoneView
	Day       time.Time
	Places    []store.Place
	Farms     []store.Farm // trang trại trong vùng (FN-18, CR-CNTS-001)
	Outbreaks []OutbreakView
	Impact    *Impact // vùng dịch, vùng bị uy hiếp, vùng đệm (FN-18 BR-02); nil với loại vùng khác
	Catalog   map[string]string
}

// Contents trả điểm trình diễn, hộ mô hình (có tọa độ), trang trại và ổ dịch nằm trong hình của vùng ref ở ngày day
// (phiên bản tại ngày đó — vùng đệm 3 km/10 km quanh ổ dịch), kèm khoảng cách tới tâm vùng và số hộ, diện tích bị ảnh
// hưởng (FN-18 BR-02).
func (s *Service) Contents(ctx context.Context, ref uuid.UUID, dayText string) (ZoneContents, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ZoneContents{}, err
	}
	d, err := s.parseDay(dayText)
	if err != nil {
		return ZoneContents{}, err
	}
	var zs []store.Zone
	var obs []store.Outbreak
	var places []store.Place
	var farms []store.Farm
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if zs, e = store.ZonesAt(ctx, tx, store.ZoneFilter{Day: d, Refs: []uuid.UUID{ref}, WithRetired: true}); e != nil || len(zs) == 0 {
			return e
		}
		id := zs[0].ID
		if obs, e = store.OutbreaksAt(ctx, tx, d, &id); e != nil {
			return e
		}
		if places, e = store.Places(ctx, tx, &id); e != nil {
			return e
		}
		farms, e = store.Farms(ctx, tx, &id)
		return e
	}); err != nil {
		return ZoneContents{}, err
	}
	if len(zs) == 0 || !c.seesZone(zs[0]) {
		return ZoneContents{}, ErrZoneNotFound.WithDetail("vùng chưa có ở ngày " + d.Format(time.DateOnly))
	}
	out := ZoneContents{Zone: c.view(zs[0]), Day: d, Places: c.places(places), Farms: c.farms(farms), Outbreaks: c.outbreaks(obs),
		Catalog: s.catalog(ctx)}
	if domain.ImpactKind(zs[0].Kind) {
		im := impactOf(out.Outbreaks)
		out.Impact = &im
	}
	return out, nil
}
