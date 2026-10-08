package app

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// G-05: ranh giới hành chính theo cấp hoặc đơn vị cha
// ---------------------------------------------------------------------------

// AdminBoundaryItem là hình hiện hành của một đơn vị kèm mã đơn vị cha.
type AdminBoundaryItem struct {
	store.AdminBoundary
	ParentCode string
}

// AdminBoundaryList là kết quả liệt kê: hình có được và mã đơn vị chưa có hình.
type AdminBoundaryList struct {
	Items   []AdminBoundaryItem
	Missing []string
}

// MaxAdminBoundaries giới hạn số đơn vị một lần đọc (tỉnh ĐBSCL ≈ 100 xã).
const MaxAdminBoundaries = 500

// ListAdminBoundaries trả ranh giới hiện hành của mọi đơn vị cấp level thuộc
// parent (rỗng: mọi tỉnh; với cấp xã bắt buộc có parent). Danh sách đơn vị lấy
// từ pf/masterdata (hiệu lực hiện tại); đơn vị chưa nhập hình nằm ở Missing.
// Ranh giới hành chính là dữ liệu tham chiếu chung — không lọc theo phạm vi.
func (s *Service) ListAdminBoundaries(ctx context.Context, level, parent string, zoom int) (AdminBoundaryList, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return AdminBoundaryList{}, err
	}
	switch level {
	case string(partyport.LevelProvince), string(partyport.LevelCommune):
	default:
		return AdminBoundaryList{}, problem.Validation.WithField("level", "phải là province hoặc commune")
	}
	if level == string(partyport.LevelCommune) && parent == "" {
		return AdminBoundaryList{}, problem.Validation.WithField("parent", "bắt buộc với level=commune (mã tỉnh)")
	}
	units, err := s.unitsOf(ctx, level, parent)
	if err != nil {
		return AdminBoundaryList{}, err
	}
	if len(units) > MaxAdminBoundaries {
		return AdminBoundaryList{}, problem.Validation.WithField("parent", "quá nhiều đơn vị, thu hẹp theo tỉnh")
	}
	codes := make([]string, 0, len(units))
	for _, u := range units {
		codes = append(codes, u.Code)
	}
	var bs []store.AdminBoundary
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var e error
		bs, e = store.AdminBoundaries(ctx, tx, codes, SimplifyTolerance(zoom))
		return e
	})
	if err != nil {
		return AdminBoundaryList{}, err
	}
	have := map[string]store.AdminBoundary{}
	for _, b := range bs {
		have[b.UnitCode] = b
	}
	out := AdminBoundaryList{Items: []AdminBoundaryItem{}, Missing: []string{}}
	for _, u := range units {
		b, ok := have[u.Code]
		if !ok {
			out.Missing = append(out.Missing, u.Code)
			continue
		}
		if u.Name != "" {
			b.Name = u.Name // tên hiện hành theo danh mục đơn vị hành chính
		}
		out.Items = append(out.Items, AdminBoundaryItem{AdminBoundary: b, ParentCode: u.ParentCode})
	}
	return out, nil
}

// unitsOf trả đơn vị cấp level thuộc parent, hiệu lực hiện tại, sắp theo mã.
func (s *Service) unitsOf(ctx context.Context, level, parent string) ([]partyport.AdminUnit, error) {
	if s.Units == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	now := s.now()
	if parent != "" {
		pu, err := s.Units.Get(ctx, parent, now)
		if errors.Is(err, partyport.ErrUnitNotFound) {
			return nil, problem.Validation.WithField("parent", "không có đơn vị hành chính còn hiệu lực với mã này")
		}
		if err != nil {
			return nil, ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
		}
		if level == string(partyport.LevelProvince) {
			if pu.Level != partyport.LevelProvince {
				return nil, problem.Validation.WithField("parent", "với level=province, parent phải là mã tỉnh")
			}
			return []partyport.AdminUnit{pu}, nil
		}
	}
	var out []partyport.AdminUnit
	if tree, ok := s.Units.(partyport.AdminUnitTree); ok {
		us, err := tree.Children(ctx, parent, now)
		if err != nil {
			return nil, ErrDependency.WithDetail("không liệt kê được đơn vị hành chính").WithCause(err)
		}
		for _, u := range us {
			if string(u.Level) == level {
				out = append(out, u)
			}
		}
	} else {
		if parent == "" {
			return nil, ErrDependency.WithDetail("danh mục đơn vị hành chính không hỗ trợ liệt kê")
		}
		codes, err := s.Units.Descendants(ctx, parent, now)
		if err != nil {
			return nil, ErrDependency.WithDetail("không liệt kê được đơn vị hành chính").WithCause(err)
		}
		for _, c := range codes {
			u, err := s.Units.Get(ctx, c, now)
			if err == nil && string(u.Level) == level && u.ParentCode == parent {
				out = append(out, u)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out, nil
}
