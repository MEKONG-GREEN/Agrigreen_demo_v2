package app

import (
	"context"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
)

// nameBook là cấu trúc khu vực + tên xã của một tỉnh tại thời điểm thao tác (ảnh chụp lưu vào lần tổng hợp).
type nameBook struct {
	regions   map[string]partyport.Region
	byCommune map[string]partyport.CommuneRegion
	communes  map[string]string
}

func (s *Service) regionNames(ctx context.Context, province string) (*nameBook, error) {
	nb := &nameBook{regions: map[string]partyport.Region{}, byCommune: map[string]partyport.CommuneRegion{}, communes: map[string]string{}}
	if s.Regions == nil {
		return nb, nil
	}
	rs, err := s.Regions.ProvinceRegions(ctx, province, true)
	if err != nil {
		return nil, err
	}
	for _, r := range rs {
		nb.regions[r.Code] = r
	}
	if nb.byCommune, err = s.Regions.CommuneRegions(ctx, province); err != nil {
		return nil, err
	}
	return nb, nil
}

func (n *nameBook) region(code string) string {
	if code == "" {
		return "Chưa gán khu vực"
	}
	if r, ok := n.regions[code]; ok {
		return r.Name
	}
	return code
}

func (n *nameBook) regionOf(commune string) string { return n.byCommune[commune].RegionCode }

func (n *nameBook) commune(ctx context.Context, s *Service, code string) string {
	if v, ok := n.communes[code]; ok {
		return v
	}
	name := code
	if s.Units != nil {
		if u, err := s.Units.Get(ctx, code, s.now()); err == nil && u.Name != "" {
			name = u.Name
		}
	}
	n.communes[code] = name
	return name
}
