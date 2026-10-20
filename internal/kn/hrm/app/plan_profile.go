package app

// W11.27 (D3 H7): đăng ký profile "hrm_plan" của cap/forms — cấu hình và chính sách phiên thuộc pf/hrm (ứng dụng), gốc lắp
// ráp chỉ nối vào danh sách Profile của pf/forms (docs/dev/module-registrations.md "Ngữ cảnh của biểu mẫu").

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type planUnitKey struct{}

// asPlanUnit đánh dấu ctx: người gọi hành động với tư cách lãnh đạo ĐƠN VỊ (lập/gửi kế hoạch của đơn vị mình). Cần cho
// lãnh đạo Phòng HCTH — cùng phiên vừa là cấp Trung tâm (tổng hợp, chốt) vừa là lãnh đạo đơn vị HCTH. Chỉ đặt ở máy chủ.
func asPlanUnit(ctx context.Context) context.Context {
	return context.WithValue(ctx, planUnitKey{}, true)
}

func planUnitMode(ctx context.Context) bool { v, _ := ctx.Value(planUnitKey{}).(bool); return v }

// planUnitOf trả đơn vị nội bộ của phiên (KNContext.InternalUnit; vắng → mã phòng ban — đơn vị loại phòng mang mã phòng
// ban P2 ở seed W11.07).
func planUnitOf(k *authz.KNContext) string {
	if k.InternalUnit != "" {
		return k.InternalUnit
	}
	return k.Department
}

// planRole suy vai hrm_plan từ ngữ cảnh phiên KN: Admin (ManageBy) và Cấp Tỉnh – HCTH (ReadBy ở phòng ban chủ, không chỉ
// xem) là cấp Trung tâm; lãnh đạo đơn vị (Cấp Tỉnh, Manager) là cấp đơn vị của đơn vị nội bộ của phiên. asUnit: ưu tiên
// tư cách đơn vị khi người gọi vừa là cấp Trung tâm vừa là lãnh đạo đơn vị. ok = false: không có vai trong kế hoạch.
func planRole(acc port.Access, k *authz.KNContext, asUnit bool) (role, unit string, ok bool) {
	manager := k.Role == authz.KNTinh && k.Manager && planUnitOf(k) != ""
	switch {
	case asUnit && manager:
		return port.PlanRoleManager, planUnitOf(k), true
	case slices.Contains(acc.ManageBy, k.Role):
		return port.PlanRoleAdmin, "", true
	case slices.Contains(acc.ReadBy, k.Role) && k.Department == acc.Department && !k.ViewOnly:
		return port.PlanRoleHCTH, "", true
	case manager:
		return port.PlanRoleManager, planUnitOf(k), true
	}
	return "", "", false
}

// PlanFormProfile là cấu hình cap/forms của kế hoạch nhân lực năm (FN-11): Trung tâm (TT: Phòng HCTH, Admin) mở kỳ năm,
// rà soát ("yêu cầu điều chỉnh" = trả lại có ghi chú), chốt (ảnh chụp đóng băng); đơn vị nội bộ (DV, lá — mã đơn vị W11.07,
// loại đơn vị lá "department") lập và gửi. Gửi lại không giới hạn đến khi chốt; gửi sau hạn được (nhãn "Trễ hạn"); chốt
// không đòi rà soát xong (prototype: chốt khi còn phòng chưa gửi, có cảnh báo).
func PlanFormProfile(acc port.Access) formsport.Profile {
	return formsport.Profile{
		Name:      port.PlanProfile,
		Hierarchy: []string{port.PlanLevelCenter, port.PlanLevelUnit},
		Roles: map[string]string{port.PlanRoleAdmin: port.PlanLevelCenter, port.PlanRoleHCTH: port.PlanLevelCenter,
			port.PlanRoleManager: port.PlanLevelUnit},
		PublishBy: port.PlanLevelCenter, CloseBy: port.PlanLevelCenter, ReviewBy: port.PlanLevelCenter,
		DefineBy:     []string{port.PlanRoleAdmin, port.PlanRoleHCTH},
		UnitKinds:    []string{formsport.UnitDepartment},
		ResolveActor: planActorResolver(acc),
	}
}

// planActorResolver chiếu phiên KN vào cap/forms. Không có ngữ cảnh KN → không áp (forms chỉ xét vai realm — không vai realm
// nào ánh xạ vào hrm_plan nên bị từ chối). Có ngữ cảnh mà không có vai → từ chối, không rơi về realm.
func planActorResolver(acc port.Access) formsport.ActorResolver {
	return func(ctx context.Context, p authz.Principal, _ formsport.Profile) (formsport.ResolvedActor, bool, error) {
		if p.KN == nil {
			return formsport.ResolvedActor{}, false, nil
		}
		role, unit, ok := planRole(acc, p.KN, planUnitMode(ctx))
		if !ok {
			return formsport.ResolvedActor{}, true, problem.Forbidden.WithDetail("kế hoạch nhân lực: chỉ Phòng HCTH, Admin và lãnh đạo các đơn vị (FN-11)")
		}
		if role == port.PlanRoleManager {
			return formsport.ResolvedActor{Role: role, Department: unit, Node: p.KN.OrgRef,
				AllowsTarget: func(string, uuid.UUID) bool { return false }}, true, nil
		}
		return formsport.ResolvedActor{Role: role, Department: acc.Department, Scope: authz.Scope{All: true}, Node: p.KN.OrgRef}, true, nil
	}
}

// PlanValidator là quy tắc chặn của bài nộp hrm_plan (hình dạng nhu cầu theo vị trí, giới hạn số) — nối vào
// forms.Deps.Validators. Số hiện có/dự kiến nghỉ hưu do pf/hrm điền khi gửi qua /v1/hrm/plans (BR-01).
type PlanValidator struct{}

var _ formsport.Validator = PlanValidator{}

// Validate thực hiện formsport.Validator.
func (PlanValidator) Validate(_ context.Context, templateCode string, values map[string]string) []formsport.Issue {
	var out []formsport.Issue
	for _, x := range domain.ValidatePlanForm(templateCode, values) {
		out = append(out, formsport.Issue{Field: x[0], Reason: x[1]})
	}
	return out
}
