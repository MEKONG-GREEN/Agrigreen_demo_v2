package extension

import (
	"context"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"slices"

	"github.com/google/uuid"

	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// FormProfiles returns fresh application-owned configuration for cap/forms.
func FormProfiles() []formsport.Profile {
	// KNProfile là cấu hình cap/forms của App Khuyến nông: TW → Tỉnh → Xã, Tỉnh phát hành, rà soát, chốt/mở lại; Xã nộp.
	// Vai là vai ngữ cảnh phiên KN (port.SessionRole*); Admin KN và reports_admin định nghĩa biểu. Không cho biểu mẫu thử:
	// 14 biểu thật (Q-01) phải được định nghĩa trước khi Tỉnh phát hành kỳ trên /v1/forms (bật production chờ Q-01).
	base := formsport.Profile{
		Name:      "kn",
		Hierarchy: []string{"TW", "TINH", "XA"},
		Roles:     map[string]string{extport.SessionRoleNational: "TW", extport.SessionRoleProvince: "TINH", extport.SessionRoleCommune: "XA"},
		PublishBy: "TINH",
		CloseBy:   "TINH",
		ReviewBy:  "TINH",
		DefineBy:  []string{extport.SessionRoleAdmin, "reports_admin"},
	}

	// KNCNTSProfile là cấu hình cap/forms của Phòng Chăn nuôi – Thủy sản (W11.05, BRD CNTS v3.7): Tỉnh phát hành, rà soát,
	// chốt/mở lại; Xã nhập cho đơn vị lá là xã, cơ sở giết mổ (BM-01) hoặc hộ/điểm (nhật ký) — cơ sở/hộ lọc phạm vi theo
	// mã xã cha. Chỉ phiên của phòng CNTS (Admin định nghĩa biểu — Q-056 đề xuất "theo Profile từng phòng"). Không giới
	// hạn số lần gửi (CNTS BR-02 "được sửa và gửi lại đến khi chốt kỳ"). W11.42: gửi sau hạn nộp phải ghi lý do trễ (H05,
	// H09); quy tắc chặn của biểu kỳ (extension CNTSValidator), cơ sở giết mổ kiểm theo danh mục CNTS (pf/masterdata).
	livestock := formsport.Profile{
		Name:              "kn_cnts",
		Hierarchy:         []string{"TW", "TINH", "XA"},
		Roles:             map[string]string{extport.SessionRoleNational: "TW", extport.SessionRoleProvince: "TINH", extport.SessionRoleCommune: "XA"},
		PublishBy:         "TINH",
		CloseBy:           "TINH",
		ReviewBy:          "TINH",
		DefineBy:          []string{extport.SessionRoleAdmin, "reports_admin"},
		UnitKinds:         []string{formsport.UnitCommune, formsport.UnitFacility, formsport.UnitHousehold},
		Departments:       []string{partyport.DeptChanNuoiThuySan},
		RequireLateReason: true,
	}

	// KNKHTCProfile là cấu hình cap/forms của phiếu "Báo cáo cho KHTC" (W11.05, BRD KHTC v2.2): đơn vị lá là PHÒNG BAN.
	// Cấp Tỉnh của Phòng KHTC (khóa vai "kn_tinh@ke_hoach_tai_chinh") mở kỳ, rà soát (yêu cầu bổ sung/xác nhận), đóng và
	// hủy kỳ; Cấp Tỉnh các phòng khác nộp phiếu của phòng mình. Tối đa 3 lần gửi, lần 3 tự khóa (FN-TI-02.03 — Q-081 đề
	// xuất "tối đa 3, tham số"). KHTC không mở lại kỳ đã đóng: route mở lại vẫn có nhưng màn KHTC (W11.62) không dùng.
	planning := formsport.Profile{
		AuthorizeOperation: extport.AuthorizeKHTCOperation,
		Name:               "kn_khtc",
		Hierarchy:          []string{"TINH", "PHONG"},
		Roles: map[string]string{
			extport.SessionRoleProvince + "@" + partyport.DeptKeHoachTaiChinh: "TINH",
			extport.SessionRoleProvince:                                       "PHONG",
		},
		PublishBy:      "TINH",
		CloseBy:        "TINH",
		ReviewBy:       "TINH",
		DefineBy:       []string{extport.SessionRoleAdmin, "reports_admin"},
		UnitKinds:      []string{formsport.UnitDepartment},
		MaxSubmissions: 3,
	}

	profiles := []formsport.Profile{base, livestock, planning}
	for i := range profiles {
		profiles[i].ResolveActor = resolveFormActor
	}
	return profiles
}

func resolveFormActor(ctx context.Context, p authz.Principal, profile formsport.Profile) (formsport.ResolvedActor, bool, error) {
	// The legacy marker only distinguishes an unbound selected request from a native realm request.
	// Permissions are always decoded from the registered owner payload.
	if p.SelectedSession == nil && p.KN == nil {
		return formsport.ResolvedActor{}, false, nil
	}
	_, k, err := extport.RequireSession(authz.WithPrincipal(ctx, p))
	if err != nil {
		return formsport.ResolvedActor{}, true, err
	}
	if len(profile.Departments) > 0 && k.Role != extport.SessionRoleAdmin && !slices.Contains(profile.Departments, k.Department) {
		return formsport.ResolvedActor{}, true, problem.Forbidden.WithDetail("cấu hình biểu " + profile.Name + " không dành cho phòng ban của phiên")
	}
	if slices.Contains(profile.Departments, partyport.DeptChanNuoiThuySan) {
		// #71 (FN-14 AC-01…AC-03, BR-02): biểu kỳ CNTS dùng phạm vi quyền FN-14 trong payload chủ. Chưa từng được cấp →
		// AllowsTemplate nil + giới hạn W11.07 như cũ; tường minh → biểu, xã, cơ sở, hộ chỉ theo quyền đang hiệu lực
		// (rỗng = chặn). Closure đọc bản sao bất biến của payload request này.
		a := formsport.ResolvedActor{Role: k.Role, Department: k.Department, Scope: k.CNTSScope(), Node: k.OrgRef,
			AllowsTarget: func(kind string, ref uuid.UUID) bool {
				switch kind {
				case formsport.UnitFacility:
					return k.AllowsCNTSFacility(ref)
				case formsport.UnitHousehold:
					return k.AllowsCNTSHousehold(ref)
				default:
					return true
				}
			}}
		if k.Role == extport.SessionRoleCommune && k.CNTSGrant != nil {
			a.AllowsTemplate = k.AllowsCNTSForm
		}
		return a, true, nil
	}
	return formsport.ResolvedActor{
		Role: k.Role, Department: k.Department, Scope: k.Scope, Node: k.OrgRef,
		AllowsTarget: func(kind string, ref uuid.UUID) bool {
			switch kind {
			case formsport.UnitFacility:
				return k.AllowsFacility(ref)
			case formsport.UnitHousehold:
				return k.AllowsHousehold(ref)
			default:
				return true
			}
		},
	}, true, nil
}
