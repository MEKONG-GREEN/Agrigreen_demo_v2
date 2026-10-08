package handler_test

import (
	"context"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// fakeScope thay party.ScopeResolver: phạm vi theo OrgRef của người gọi.
type fakeScope struct {
	mu     sync.Mutex
	scopes map[uuid.UUID]authz.Scope
}

func newFakeScope() *fakeScope { return &fakeScope{scopes: map[uuid.UUID]authz.Scope{}} }

func (f *fakeScope) Scope(ctx context.Context) (authz.Scope, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Scope{}, problem.Unauthenticated
	}
	if p.Has(authz.RolePlatformAdmin) {
		return authz.Scope{All: true}, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.scopes[p.OrgRef], nil
}

// fakeUnits là danh mục đơn vị hành chính giả: tỉnh Cần Thơ (92) với hai xã, An Giang (89) một xã.
type fakeUnits struct {
	units map[string]partyport.AdminUnit
}

func newFakeUnits() *fakeUnits {
	f := &fakeUnits{units: map[string]partyport.AdminUnit{}}
	add := func(code, name string, lvl partyport.Level, parent string) {
		f.units[code] = partyport.AdminUnit{Code: code, Name: name, Level: lvl, ParentCode: parent}
	}
	add("92", "Cần Thơ", partyport.LevelProvince, "")
	add("31117", "Xã Thới Lai", partyport.LevelCommune, "92")
	add("31120", "Xã Trường Xuân", partyport.LevelCommune, "92")
	add("89", "An Giang", partyport.LevelProvince, "")
	add("30001", "Xã A", partyport.LevelCommune, "89")
	return f
}

func (f *fakeUnits) Get(_ context.Context, code string, _ time.Time) (partyport.AdminUnit, error) {
	u, ok := f.units[code]
	if !ok {
		return partyport.AdminUnit{}, partyport.ErrUnitNotFound
	}
	return u, nil
}

func (f *fakeUnits) Descendants(_ context.Context, code string, _ time.Time) ([]string, error) {
	out := []string{code}
	for c, u := range f.units {
		if u.ParentCode == code {
			out = append(out, c)
		}
	}
	return out, nil
}

func (f *fakeUnits) Current(_ context.Context, code string, _ time.Time) ([]string, error) {
	return []string{code}, nil
}

// fakeParams trả ngưỡng chồng lấn đã "phê duyệt"; nil = chưa có phiên bản (mặc định 1%).
// values: tham số khác đã "phê duyệt" (G2: ngưỡng widget Hub, zoom lớp thửa).
type fakeParams struct {
	tolerance *decimal.Decimal
	values    *paramValues
}

// paramValues là bảng tham số đổi được giữa các request của kiểm thử.
type paramValues struct {
	mu sync.Mutex
	m  map[string]decimal.Decimal
}

func (p *paramValues) get(key string) (decimal.Decimal, bool) {
	if p == nil {
		return decimal.Zero, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	v, ok := p.m[key]
	return v, ok
}

func (f fakeParams) At(_ context.Context, key string, _ time.Time) (params.ParamValue, error) {
	if v, ok := f.values.get(key); ok {
		return params.ParamValue{Key: key, Value: v, Version: 1}, nil
	}
	if key != "geo.overlap_tolerance_pct" || f.tolerance == nil {
		return params.ParamValue{}, params.ErrParamNotFound
	}
	return params.ParamValue{Key: key, Value: *f.tolerance, Unit: "%", Version: 1}, nil
}

// Hình học kiểm thử quanh Cần Thơ.
const (
	baseLon = 105.60
	baseLat = 10.05
)

// degPerMetre trả số độ vĩ/kinh tương ứng 1 m tại vĩ độ lat trên ellipsoid WGS84
// (bán kính cong kinh tuyến M và vòng thẳng đứng N) — dùng dựng hình chữ nhật có
// diện tích biết trước để kiểm diện tích trắc địa của PostGIS.
func degPerMetre(lat float64) (dLat, dLon float64) {
	const a, e2 = 6378137.0, 0.00669437999014
	phi := lat * math.Pi / 180
	s := math.Sin(phi)
	w := 1 - e2*s*s
	m := a * (1 - e2) / math.Pow(w, 1.5)
	n := a / math.Sqrt(w)
	return 180 / (math.Pi * m), 180 / (math.Pi * n * math.Cos(phi))
}

// rect dựng Polygon hình chữ nhật: góc tây nam cách gốc (east, north) mét, rộng w, cao h (m).
func rect(east, north, w, h float64) map[string]any {
	dLat, dLon := degPerMetre(baseLat)
	x0, y0 := baseLon+east*dLon, baseLat+north*dLat
	x1, y1 := x0+w*dLon, y0+h*dLat
	return map[string]any{"type": "Polygon", "coordinates": [][][]float64{{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}, {x0, y0}}}}
}

// denyCP là điểm kiểm soát xuất giả luôn báo vượt hạn mức (A5: xuất nội bộ vẫn phải đi qua và không bị chặn).
type denyCP struct {
	mu  sync.Mutex
	got []meter.Egress
}

func (d *denyCP) Pass(_ context.Context, e meter.Egress) (meter.Decision, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.got = append(d.got, e)
	return meter.Decision{Allowed: false, Reason: "quota_exceeded", Mode: meter.ModeEnforce}, nil
}

func (f *fakeScope) set(org uuid.UUID, sc authz.Scope) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes[org] = sc
}
