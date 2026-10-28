package app

// Lớp bản đồ theo ngày (FN-18 B1–B2): vùng, ổ dịch BM-10 (W11.49), điểm trình diễn và hộ mô hình có tọa độ (W11.47),
// thống kê thửa HTX theo xã (qua công bố pf_cooperative_pub — KN không đọc bảng HTX). Đối tượng trong vùng (vùng đệm 3 km,
// 10 km…): điểm, hộ, ổ dịch nằm trong hình của vùng.

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
)

// OutbreakView là ổ dịch trên bản đồ.
type OutbreakView struct {
	store.Outbreak
	Status string // reported | monitoring | closed (Đã gửi / Đang theo dõi / Đã kết thúc — W11.49)
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
	Day        time.Time
	Zones      []ZoneView
	Outbreaks  []OutbreakView
	Places     []store.Place
	PlotStats  []knport.CommunePlots
	PlotsReady bool // đã nối cổng thống kê thửa HTX
	CanDraw    bool
	Catalog    map[string]string // mã danh mục (bệnh, loài, loại mô hình) → tên
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
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if zs, e = store.ZonesAt(ctx, tx, store.ZoneFilter{Day: d}); e != nil {
			return e
		}
		if obs, e = store.OutbreaksAt(ctx, tx, d, nil); e != nil {
			return e
		}
		places, e = store.Places(ctx, tx, nil)
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
	out.Outbreaks, out.Places = c.outbreaks(obs), c.places(places)
	if s.Plots != nil {
		var units []string
		if !c.admin() {
			units = append([]string{}, c.k.Scope.Units...)
		}
		if st, err := s.Plots.DigitizedByCommune(ctx, units); err == nil {
			out.PlotsReady = true
			for _, x := range st {
				if c.sees(x.CommuneCode) {
					out.PlotStats = append(out.PlotStats, x)
				}
			}
		}
	}
	return out, nil
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
	Outbreaks []OutbreakView
	Catalog   map[string]string
}

// Contents trả điểm trình diễn, hộ mô hình (có tọa độ) và ổ dịch nằm trong hình của vùng ref ở ngày day (phiên bản
// tại ngày đó — vùng đệm 3 km/10 km quanh ổ dịch), kèm khoảng cách tới tâm vùng.
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
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if zs, e = store.ZonesAt(ctx, tx, store.ZoneFilter{Day: d, Refs: []uuid.UUID{ref}, WithRetired: true}); e != nil || len(zs) == 0 {
			return e
		}
		id := zs[0].ID
		if obs, e = store.OutbreaksAt(ctx, tx, d, &id); e != nil {
			return e
		}
		places, e = store.Places(ctx, tx, &id)
		return e
	}); err != nil {
		return ZoneContents{}, err
	}
	if len(zs) == 0 || !c.seesZone(zs[0]) {
		return ZoneContents{}, ErrZoneNotFound.WithDetail("vùng chưa có ở ngày " + d.Format(time.DateOnly))
	}
	return ZoneContents{Zone: c.view(zs[0]), Day: d, Places: c.places(places), Outbreaks: c.outbreaks(obs), Catalog: s.catalog(ctx)}, nil
}
