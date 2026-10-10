package app

// Ngữ cảnh KN v7.7 (P1) cho pf/extension (P5 lịch công tác — FN-XA-03.08, FN-TI-02.07): vai trò, phòng ban,
// tỉnh, xã phụ trách và CẤP QUẢN LÝ lấy từ ngữ cảnh phiên (X-AG-Context) + sổ gán vai trò hiện hành, thay
// cho đường suy từ vai realm. Gốc lắp ráp nối bằng extension.UseKNContext(acc.ExtensionKNContext(),
// acc.ExtensionKNDirectory()).

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ExtensionKN thực hiện extension/port.KNContextResolver và KNDirectory trên sổ gán vai trò (Assignments)
// và cây tổ chức (Orgs — tỉnh của đơn vị sở hữu phòng ban).
type ExtensionKN struct {
	Assignments port.Assignments
	Orgs        partyport.Orgs
}

var (
	_ port.KNContextResolver = ExtensionKN{}
	_ port.KNDirectory       = ExtensionKN{}
)

// maxOrgHops giới hạn số bước đi lên cây tổ chức khi tìm TTKN tỉnh.
const maxOrgHops = 4

// provinceOf trả mã tỉnh (đơn vị hành chính của Nút TTKN tỉnh) của Nút ref hoặc tổ tiên gần nhất; rỗng với
// Cấp Quốc gia / Nút không thuộc tỉnh nào.
func (e ExtensionKN) provinceOf(ctx context.Context, ref uuid.UUID) (string, error) {
	for range maxOrgHops {
		if ref == uuid.Nil || e.Orgs == nil {
			return "", nil
		}
		o, err := e.Orgs.Get(ctx, ref)
		if errors.Is(err, partyport.ErrOrgNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		switch o.Kind {
		case partyport.OrgTTKNProvince:
			return o.AdminUnitCode, nil
		case partyport.OrgTTKNNational:
			return "", nil
		}
		ref = o.ParentRef
	}
	return "", nil
}

func unitOf(a port.Assignment) uuid.UUID {
	if a.UnitRef != uuid.Nil {
		return a.UnitRef
	}
	return a.OrgRef
}

// KNContext trả ngữ cảnh KN của request (ok = false khi request không mang X-AG-Context).
func (e ExtensionKN) KNContext(ctx context.Context) (port.KNContext, bool, error) {
	p, found := authz.FromContext(ctx)
	if !found || p.KN == nil {
		return port.KNContext{}, false, nil
	}
	k := p.KN
	c := port.KNContext{Role: k.Role, Department: k.Department, Communes: slices.Clone(k.Communes), MgmtLevel: k.MgmtLevel,
		InternalUnit: k.InternalUnit, ReadOnly: k.ReadOnly()}
	unit := k.UnitRef
	if unit == uuid.Nil {
		unit = k.OrgRef
	}
	var err error
	if c.ProvinceCode, err = e.provinceOf(ctx, unit); err != nil {
		return port.KNContext{}, false, err
	}
	// Cấp quản lý của lần gán (Q-05). Ngữ cảnh hỗ trợ không gắn tài khoản (AssignmentID Nil) → không biết.
	// ResolveContext (W11.07) đã đặt MgmtLevel; tra lại chỉ khi thiếu (ngữ cảnh dựng tay trong kiểm thử cũ).
	if c.MgmtLevel == "" && k.AssignmentID != uuid.Nil && e.Assignments != nil {
		list, err := e.Assignments.ActiveAssignments(ctx, k.Role, k.Department)
		if err != nil {
			return port.KNContext{}, false, err
		}
		for _, a := range list {
			if a.ID == k.AssignmentID {
				c.MgmtLevel = a.MgmtLevel
				break
			}
		}
	}
	return c, true, nil
}

// Officers liệt kê cán bộ Cấp Xã/Cấp Tỉnh còn hiệu lực của phòng ban × tỉnh (tab "Chưa đăng ký", người nhận
// nhắc, người duyệt). Tenant truyền tường minh (job của worker không có Principal). Một người có nhiều lần gán
// cùng cấp → gộp xã phụ trách; cấp quản lý lấy của lần gán Cấp Tỉnh (người duyệt).
func (e ExtensionKN) Officers(ctx context.Context, tenantID uuid.UUID, department, provinceCode string) ([]port.KNOfficer, error) {
	if e.Assignments == nil {
		return nil, nil
	}
	ctx = tenant.WithID(ctx, tenantID)
	list, err := e.Assignments.ActiveAssignments(ctx, "", department)
	if err != nil {
		return nil, err
	}
	provinces := map[uuid.UUID]string{}
	type key struct {
		actor uuid.UUID
		level string
	}
	idx := map[key]int{}
	var out []port.KNOfficer
	for _, a := range list {
		var level string
		switch a.Role {
		case port.KNXa:
			level = port.ScheduleLevelCommune
		case port.KNTinh:
			level = port.ScheduleLevelProvince
		default:
			continue
		}
		u := unitOf(a)
		prov, ok := provinces[u]
		if !ok {
			if prov, err = e.provinceOf(ctx, u); err != nil {
				return nil, err
			}
			provinces[u] = prov
		}
		if prov == "" || prov != provinceCode {
			continue
		}
		k := key{a.UserRef, level}
		if i, dup := idx[k]; dup {
			for _, c := range a.Communes {
				if !slices.Contains(out[i].Communes, c) {
					out[i].Communes = append(out[i].Communes, c)
				}
			}
			if out[i].MgmtLevel == "" {
				out[i].MgmtLevel = a.MgmtLevel
			}
			continue
		}
		idx[k] = len(out)
		out = append(out, port.KNOfficer{ActorRef: a.UserRef, Level: level, ProvinceCode: prov, Communes: slices.Clone(a.Communes), MgmtLevel: a.MgmtLevel})
	}
	return out, nil
}
