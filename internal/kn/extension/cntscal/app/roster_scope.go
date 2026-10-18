package app

import (
	"context"
	"errors"
	"strings"
	"time"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// rosterScope lấy tỉnh từ phạm vi do server giải; chỉ Admin chọn tỉnh ngoài phạm vi.
// legacy là đường đọc lịch chung cũ chỉ cho Admin, không có đường ghi.
func (s *Service) rosterScope(ctx context.Context, c caller, requested string, write bool) (caller, error) {
	requested = strings.TrimSpace(requested)
	if requested == "legacy" {
		if !c.admin() || write {
			return c, ErrOutOfScope
		}
		c.legacyRoster = true
		return c, nil
	}
	if s.Units == nil {
		return c, ErrDependency
	}
	provinces := map[string]bool{}
	if !c.admin() || requested == "" {
		for _, code := range c.k.Scope.Units {
			u, err := s.Units.Get(ctx, code, s.today())
			if errors.Is(err, partyport.ErrUnitNotFound) {
				continue
			}
			if err != nil {
				return c, err
			}
			if u.Level == partyport.LevelProvince {
				provinces[u.Code] = true
			} else if u.Level == partyport.LevelCommune && u.ParentCode != "" {
				provinces[u.ParentCode] = true
			}
		}
	}
	if requested == "" && len(provinces) == 1 {
		for code := range provinces {
			requested = code
		}
	}
	if requested == "" {
		return c, problem.Validation.WithField("province_code", "Chọn tỉnh để xem hoặc lập lịch trực")
	}
	u, err := s.Units.Get(ctx, requested, s.today())
	if errors.Is(err, partyport.ErrUnitNotFound) || (err == nil && u.Level != partyport.LevelProvince) {
		return c, problem.Validation.WithField("province_code", "phải là tỉnh còn hiệu lực")
	}
	if err != nil {
		return c, err
	}
	if !c.admin() && (!provinces[requested] || (c.k.Role != authz.KNXa && !c.k.Scope.Allows(requested))) {
		return c, ErrOutOfScope
	}
	c.province = requested
	return c, nil
}

func requestedProvince(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func (s *Service) rosterCommune(ctx context.Context, c caller, code string, at time.Time) (bool, error) {
	if c.legacyRoster {
		return c.admin(), nil
	}
	if !c.sees(code) {
		return false, nil
	}
	u, err := s.Units.Get(ctx, code, at)
	if errors.Is(err, partyport.ErrUnitNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return u.Level == partyport.LevelCommune && u.ParentCode == c.province, nil
}
