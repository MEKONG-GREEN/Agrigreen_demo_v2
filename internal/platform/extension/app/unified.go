package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
)

// ParamTargetSownAreaPrefix là khóa tham số mục tiêu diện tích gieo sạ theo đơn vị:
// "extension.target_sown_area_ha:<mã tỉnh/xã>" (ha, tra tại đầu kỳ — mục 16).
const ParamTargetSownAreaPrefix = "extension.target_sown_area_ha"

// UnifiedFigures là số liệu gộp của một đơn vị trên bảng điều hành (G-02).
type UnifiedFigures struct {
	SownAreaHa       decimal.Decimal  // pf/extension: diện tích đã sạ báo cáo trong kỳ
	TargetAreaHa     *decimal.Decimal // tham số mục tiêu; nil = chưa đặt
	Cooperatives     int64            // pf/party: HTX đang hoạt động
	Households       int64            // pf/party: hộ thành viên đang mở
	CoopsReporting   int64            // pf/extension: HTX có báo cáo gieo sạ trong kỳ
	DigitizedPlots   int64            // pf/geo: thửa đã số hoá (chưa ngừng dùng)
	DigitizedAreaHa  decimal.Decimal  // pf/geo: tổng diện tích thửa đã số hoá
	OpenTasks        int64
	OverdueTasks     int64
	ExpectedCommunes int64
	ReportedCommunes int64
	MissingCommunes  []string
	Provisional      bool // "Số liệu tạm tính": còn xã chưa báo cáo kỳ này
}

func (u *UnifiedFigures) add(o UnifiedFigures) {
	u.SownAreaHa = u.SownAreaHa.Add(o.SownAreaHa)
	u.Cooperatives += o.Cooperatives
	u.Households += o.Households
	u.CoopsReporting += o.CoopsReporting
	u.DigitizedPlots += o.DigitizedPlots
	u.DigitizedAreaHa = u.DigitizedAreaHa.Add(o.DigitizedAreaHa)
	u.OpenTasks += o.OpenTasks
	u.OverdueTasks += o.OverdueTasks
	u.ExpectedCommunes += o.ExpectedCommunes
	u.ReportedCommunes += o.ReportedCommunes
	u.MissingCommunes = append(u.MissingCommunes, o.MissingCommunes...)
	if o.TargetAreaHa != nil {
		t := o.TargetAreaHa.Add(decimal.Zero)
		if u.TargetAreaHa != nil {
			t = u.TargetAreaHa.Add(t)
		}
		u.TargetAreaHa = &t
	}
}

func (u *UnifiedFigures) finish() {
	sort.Strings(u.MissingCommunes)
	if u.MissingCommunes == nil {
		u.MissingCommunes = []string{}
	}
	u.Provisional = len(u.MissingCommunes) > 0
}

// UnifiedChild là số liệu gộp của một đơn vị con.
type UnifiedChild struct {
	Unit string
	Name string
	UnifiedFigures
}

// Unified là bảng điều hành gộp nhiều module (G-02, US-DASH-01): KPI của
// pf/extension, HTX/hộ của pf/party, thửa đã số hoá của pf/geo, mục tiêu diện
// tích (tham số). Tổng = tổng các con (toàn quốc = tổng các tỉnh), riêng mục tiêu
// của tỉnh/xã lấy tham số của chính đơn vị nếu có.
type Unified struct {
	Unit     string
	Level    string
	Period   string
	AsOf     time.Time
	Total    UnifiedFigures
	Children []UnifiedChild
	// Sources: module đã nối vào số liệu (thiếu module → số liệu của nó là 0).
	Sources []string
}

// GetUnifiedDashboard dựng bảng điều hành gộp. Phạm vi, đơn vị mặc định và kỳ
// theo GetDashboard (kiểm vai, phạm vi ở đó).
func (s *Service) GetUnifiedDashboard(ctx context.Context, unit, period string) (Unified, error) {
	d, err := s.GetDashboard(ctx, unit, period)
	if err != nil {
		return Unified{}, err
	}
	out := Unified{Unit: d.Unit, Level: d.Level, Period: d.Period, AsOf: d.AsOf, Sources: []string{"pf/extension"}}
	children := map[string]*UnifiedChild{}
	child := func(code string) *UnifiedChild {
		if children[code] == nil {
			children[code] = &UnifiedChild{Unit: code, UnifiedFigures: UnifiedFigures{SownAreaHa: decimal.Zero, DigitizedAreaHa: decimal.Zero}}
		}
		return children[code]
	}
	for _, ch := range d.Children {
		c := child(ch.Unit)
		k := ch.KPI
		c.SownAreaHa, c.CoopsReporting, c.OpenTasks, c.OverdueTasks = k.SownAreaHa, k.Cooperatives, k.OpenTasks, k.OverdueTasks
		c.ExpectedCommunes, c.ReportedCommunes, c.MissingCommunes = k.ExpectedCommunes, k.ReportedCommunes, append([]string{}, k.MissingCommunes...)
	}

	// Đơn vị con của từng xã (tỉnh với toàn quốc, chính xã với tỉnh/xã).
	communes, parentOf, err := s.communesOf(ctx, d.Level, d.Unit)
	if err != nil {
		return Unified{}, err
	}
	childOf := func(commune string) (string, bool) {
		switch d.Level {
		case store.LevelNational:
			p, ok := parentOf(commune)
			return p, ok
		case store.LevelProvince:
			return commune, commune != d.Unit
		}
		return commune, commune == d.Unit
	}
	if s.Coops != nil {
		out.Sources = append(out.Sources, "pf/party")
		stats, err := s.Coops.ByUnit(ctx, communes)
		if err != nil {
			return Unified{}, ErrDependency.WithDetail("không đọc được thống kê HTX").WithCause(err)
		}
		for _, st := range stats {
			if k, ok := childOf(st.UnitCode); ok {
				c := child(k)
				c.Cooperatives += st.Cooperatives
				c.Households += st.Households
			}
		}
	}
	if s.Plots != nil {
		out.Sources = append(out.Sources, "pf/geo")
		stats, err := s.Plots.DigitizedByCommune(ctx, communes)
		if err != nil {
			return Unified{}, ErrDependency.WithDetail("không đọc được thống kê thửa").WithCause(err)
		}
		for _, st := range stats {
			if k, ok := childOf(st.CommuneCode); ok {
				c := child(k)
				c.DigitizedPlots += st.Plots
				c.DigitizedAreaHa = c.DigitizedAreaHa.Add(st.AreaHa)
			}
		}
	}

	from, _, err := periodBounds(d.Period)
	if err != nil {
		return Unified{}, err
	}
	keys := make([]string, 0, len(children))
	for k := range children {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out.Total = UnifiedFigures{SownAreaHa: decimal.Zero, DigitizedAreaHa: decimal.Zero}
	now := s.now()
	for _, k := range keys {
		c := children[k]
		if c.TargetAreaHa, err = s.targetArea(ctx, k, from); err != nil {
			return Unified{}, err
		}
		if s.Units != nil {
			if u, err := s.Units.Get(ctx, k, now); err == nil {
				c.Name = u.Name
			}
		}
		c.finish()
		out.Total.add(c.UnifiedFigures)
		out.Children = append(out.Children, *c)
	}
	out.Total.finish()
	if d.Level != store.LevelNational {
		// Tỉnh/xã: mục tiêu của chính đơn vị (nếu đã đặt) thay cho tổng mục tiêu của các con.
		own, err := s.targetArea(ctx, d.Unit, from)
		if err != nil {
			return Unified{}, err
		}
		if own != nil {
			out.Total.TargetAreaHa = own
		}
	}
	if out.Children == nil {
		out.Children = []UnifiedChild{}
	}
	return out, nil
}

// targetArea đọc mục tiêu diện tích của đơn vị tại đầu kỳ; chưa đặt → nil.
func (s *Service) targetArea(ctx context.Context, unit string, at time.Time) (*decimal.Decimal, error) {
	if s.Params == nil || unit == "" {
		return nil, nil
	}
	v, err := s.Params.At(ctx, ParamTargetSownAreaPrefix+":"+unit, at)
	if errors.Is(err, params.ErrParamNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrDependency.WithDetail("không đọc được tham số mục tiêu diện tích").WithCause(err)
	}
	t := v.Value
	return &t, nil
}

// communesOf trả danh sách xã cần đọc (nil = mọi xã, với toàn quốc) và hàm tìm
// tỉnh của một xã (chỉ dùng ở cấp toàn quốc).
func (s *Service) communesOf(ctx context.Context, level, unit string) ([]string, func(string) (string, bool), error) {
	if level != store.LevelNational {
		if s.Units == nil {
			return nil, nil, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
		}
		codes, err := s.Units.Descendants(ctx, unit, s.now())
		if err != nil {
			return nil, nil, ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
		}
		return codes, nil, nil
	}
	cache := map[string]string{}
	now := s.now()
	if tree, ok := s.Units.(partyport.AdminUnitTree); ok && s.Units != nil {
		provs, err := tree.Children(ctx, "", now)
		if err == nil {
			for _, p := range provs {
				cache[p.Code] = p.Code
				codes, err := s.Units.Descendants(ctx, p.Code, now)
				if err != nil {
					continue
				}
				for _, c := range codes {
					cache[c] = p.Code
				}
			}
		}
	}
	parentOf := func(commune string) (string, bool) {
		if p, ok := cache[commune]; ok {
			return p, p != ""
		}
		p := ""
		if s.Units != nil {
			if u, err := s.Units.Get(ctx, commune, now); err == nil {
				p = u.ParentCode
				if u.Level == partyport.LevelProvince {
					p = u.Code
				}
			}
		}
		cache[commune] = p
		return p, p != ""
	}
	return nil, parentOf, nil
}
