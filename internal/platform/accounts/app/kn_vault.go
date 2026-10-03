package app

// Quyết định A1 (03/10/2026, docs/cau-hoi-ton-dong.md): Cán bộ xã (vai trò KN kn_xa) được cấp MẶC ĐỊNH vai realm
// vault_registrar để ghi người đại diện HTX, chủ hộ, tên nông dân trên thửa (FN-XA-03.03, 03.05). Kho định danh tự
// kiểm vai trên token (vault/identity/client/roles.go) — vault_registrar đã gồm quyền xem nhãn che bớt (Display),
// không cần cấp thêm vault_display. Mọi lời gọi kho đều mang mục đích (client.Purpose, R-42) và kho ghi sổ truy cập
// theo mục đích — "ghi nhật ký mục đích".
//
// Ai cấp: dòng sổ account_event của lần cấp tự động mang Details.auto = autoVaultMark. Khi lần gán kn_xa đang hoạt
// động CUỐI CÙNG bị thu hồi, module chỉ thu vai nếu lần thêm vai gần nhất trong sổ là lần tự động — tài khoản giữ
// vai vì lý do khác (Admin cấp qua /assignment, có sẵn trước khi gán kn_xa) không bị đụng tới.

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// autoVaultMark là dấu "module tự cấp" trong Details của dòng sổ account_event.
const autoVaultMark = "kn_xa_default"

// KNXaVaultRole là vai kho định danh cấp mặc định cho Cán bộ xã.
const KNXaVaultRole = client.RoleRegistrar

const (
	reasonVaultGrant  = "Cấp mặc định vai kho định danh cho Cán bộ xã (A1) — mọi lời gọi kho ghi nhật ký mục đích"
	reasonVaultRevoke = "Thu vai kho định danh cấp mặc định — tài khoản không còn vai trò Cán bộ xã đang hoạt động (A1)"
)

// withRole trả bản sao roles có thêm (on) hoặc bỏ (!on) role.
func withRole(roles []string, role string, on bool) []string {
	out := slices.DeleteFunc(slices.Clone(roles), func(r string) bool { return r == role })
	if on {
		out = append(out, role)
	}
	return out
}

// grantKNXaVault cấp vai kho định danh khi tài khoản được gán kn_xa (chưa có vai thì cấp và ghi sổ dấu tự cấp; đã có
// vì lý do khác thì không ghi gì). acc là trạng thái tài khoản hiện tại ở pf.
func (s *Service) grantKNXaVault(ctx context.Context, tid, actorRef uuid.UUID, acc domain.Account, realm []string, key string) ([]string, error) {
	if slices.Contains(realm, KNXaVaultRole) {
		return realm, nil
	}
	if err := s.Dir.AddRealmRoles(ctx, acc.UserRef, []string{KNXaVaultRole}); err != nil {
		return realm, dirErr(err)
	}
	prev := acc.Roles
	acc.Roles = withRole(prev, KNXaVaultRole, true)
	ev := newEvent(tid, actorRef, acc, domain.ActionAssignmentChanged, s.now(), key, reasonVaultGrant)
	ev.Details = map[string]any{"auto": autoVaultMark, "prev_roles": nonNil(prev), "added": []string{KNXaVaultRole}, "removed": []string{}}
	if _, err := s.write(ctx, tid, &ev); err != nil {
		return realm, err
	}
	return append(slices.Clone(realm), KNXaVaultRole), nil
}

// releaseKNXaVault thu vai kho định danh khi tài khoản không còn lần gán kn_xa đang hoạt động và vai do module tự cấp.
func (s *Service) releaseKNXaVault(ctx context.Context, a *actor, user uuid.UUID, key string) error {
	tid := a.p.TenantID
	var still, auto bool
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		cur, err := store.AssignmentsOfUser(ctx, tx, user, false)
		if err != nil {
			return err
		}
		still = slices.ContainsFunc(cur, func(x port.Assignment) bool { return x.Role == port.KNXa })
		if still {
			return nil
		}
		evs, err := store.Events(ctx, tx, user)
		if err != nil {
			return err
		}
		auto = autoHeld(evs)
		return nil
	}); err != nil || still || !auto {
		return err
	}
	t, err := s.target(ctx, a, user)
	if err != nil {
		return err
	}
	if !slices.Contains(t.roles, KNXaVaultRole) {
		return nil
	}
	if err := s.Dir.RemoveRealmRoles(ctx, user, []string{KNXaVaultRole}); err != nil {
		return dirErr(err)
	}
	acc := t.state()
	prev := acc.Roles
	acc.Roles = withRole(prev, KNXaVaultRole, false)
	ev := newEvent(tid, a.p.ActorRef, acc, domain.ActionAssignmentChanged, s.now(), key, reasonVaultRevoke)
	ev.Details = map[string]any{"auto": autoVaultMark, "prev_roles": nonNil(prev), "added": []string{}, "removed": []string{KNXaVaultRole}}
	_, err = s.write(ctx, tid, &ev)
	return err
}

// autoHeld: theo sổ (thứ tự seq), lần thêm vai kho định danh gần nhất là do module tự cấp và chưa bị thu. Chỉ xét
// dòng tạo tài khoản (ảnh chụp vai lúc tạo) và dòng đổi vai (Details.added/removed) — các dòng khác (khóa, gửi liên
// kết) mang ảnh chụp có thể cũ.
func autoHeld(evs []domain.Event) bool {
	had, auto := false, false
	for _, e := range evs {
		switch {
		case e.Action == domain.ActionCreated:
			had, auto = slices.Contains(e.Account.Roles, KNXaVaultRole), false
		case e.Action != domain.ActionAssignmentChanged:
		case listed(e.Details["removed"]):
			had, auto = false, false
		case listed(e.Details["added"]):
			mark, _ := e.Details["auto"].(string)
			had, auto = true, mark == autoVaultMark
		}
	}
	return had && auto
}

// listed: v (mảng JSON đã giải mã hoặc []string) chứa vai kho định danh.
func listed(v any) bool {
	switch x := v.(type) {
	case []string:
		return slices.Contains(x, KNXaVaultRole)
	case []any:
		return slices.Contains(x, any(KNXaVaultRole))
	}
	return false
}
