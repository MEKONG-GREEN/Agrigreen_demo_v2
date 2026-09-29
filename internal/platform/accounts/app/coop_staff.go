package app

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Chức vụ hiển thị trong danh bạ cán bộ HTX (HTX US-SUPPORT-02).
const (
	TitleCoopManager = "Quản lý HTX"
	TitleCoopStaff   = "Cán bộ HTX"
)

// CoopStaff là một cán bộ HTX trong danh bạ của nông dân. Tên là thông tin danh bạ
// công vụ, đọc từ Keycloak lúc xem (không lưu ở pf); SĐT KHÔNG có ở đây — đọc riêng
// lúc bấm gọi/copy (CoopStaffPhone), giống danh bạ trực KN US-DIR-02.
type CoopStaff struct {
	UserRef        uuid.UUID
	CooperativeRef uuid.UUID
	DisplayName    string `pii:"true"`
	Title          string
	HasPhone       bool // US-SUPPORT-02 AC-2: không có số hợp lệ → ẩn nút gọi nhanh
}

// coopsOfFarmer trả HTX của các nông hộ liên kết với nông dân gọi.
func (s *Service) coopsOfFarmer(ctx context.Context) (authz.Principal, []uuid.UUID, error) {
	p, err := authz.Require(ctx, port.RoleFarmer)
	if err != nil {
		return p, nil, err
	}
	if s.Dir == nil {
		return p, nil, ErrDependency.WithCause(errNoDirectoryYet)
	}
	links, err := s.HouseholdsForSubject(ctx, p.ActorRef)
	if err != nil {
		return p, nil, err
	}
	var coops []uuid.UUID
	for _, l := range links {
		if !slices.Contains(coops, l.CooperativeRef) {
			coops = append(coops, l.CooperativeRef)
		}
	}
	return p, coops, nil
}

// staffOf đọc tài khoản đang bật thuộc HTX coop (thuộc tính org_ref), bỏ tài khoản nông dân.
func (s *Service) staffOf(ctx context.Context, p authz.Principal, coop uuid.UUID) ([]CoopStaff, error) {
	users, err := s.Dir.FindUsers(ctx, port.Query{Attributes: map[string]string{
		port.AttrTenantID: p.TenantID.String(), port.AttrOrgRef: coop.String()}, Max: 200})
	if err != nil {
		return nil, dirErr(err)
	}
	refs := make([]uuid.UUID, 0, len(users))
	for _, u := range users {
		refs = append(refs, u.ID)
	}
	var farmers map[uuid.UUID]bool
	var accs map[uuid.UUID]domain.Account
	if err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if farmers, e = store.FarmerUsers(ctx, tx, refs); e != nil {
			return e
		}
		accs, e = store.Accounts(ctx, tx, refs)
		return e
	}); err != nil {
		return nil, err
	}
	out := []CoopStaff{}
	for _, u := range users {
		if !u.Enabled || u.TenantID != p.TenantID || u.OrgRef != coop || farmers[u.ID] {
			continue
		}
		roles := accs[u.ID].Roles
		if acc, ok := accs[u.ID]; ok && !acc.Enabled {
			continue
		} else if !ok {
			// Tài khoản tạo ngoài nền tảng (console Keycloak): đọc vai trực tiếp.
			if roles, err = s.Dir.RealmRoles(ctx, u.ID); err != nil {
				return nil, dirErr(err)
			}
		}
		if slices.Contains(roles, port.RoleFarmer) {
			continue
		}
		title := TitleCoopStaff
		if slices.Contains(roles, partyport.RoleCoopManager) {
			title = TitleCoopManager
		}
		name := strings.TrimSpace(strings.TrimSpace(u.LastName) + " " + strings.TrimSpace(u.FirstName))
		out = append(out, CoopStaff{UserRef: u.ID, CooperativeRef: coop, DisplayName: name, Title: title,
			HasPhone: domain.ValidPhone(domain.NormalizePhone(u.Phone))})
	}
	return out, nil
}

// fold bỏ dấu, chữ thường — tìm theo tên hoặc chức vụ không phân biệt dấu.
func fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		if r == 'đ' {
			r = 'd'
		}
		b.WriteRune(r)
	}
	return b.String()
}

// CoopStaffDirectory: nông dân tra cứu danh bạ cán bộ HTX của mình theo tên hoặc
// chức vụ (HTX US-SUPPORT-02). Không có SĐT trong danh sách.
func (s *Service) CoopStaffDirectory(ctx context.Context, q string) ([]CoopStaff, error) {
	p, coops, err := s.coopsOfFarmer(ctx)
	if err != nil {
		return nil, err
	}
	q = fold(strings.TrimSpace(q))
	out := []CoopStaff{}
	for _, c := range coops {
		staff, err := s.staffOf(ctx, p, c)
		if err != nil {
			return nil, err
		}
		for _, st := range staff {
			if q == "" || strings.Contains(fold(st.DisplayName), q) || strings.Contains(fold(st.Title), q) {
				out = append(out, st)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Title != out[j].Title {
			return out[i].Title == TitleCoopManager
		}
		return fold(out[i].DisplayName) < fold(out[j].DisplayName)
	})
	return out, nil
}

// CoopStaffPhone trả SĐT của một cán bộ HTX của nông dân lúc bấm gọi/copy (US-SUPPORT-02
// AC-1). Đọc từ Keycloak, không lưu, không ghi log (R-14); handler đặt Cache-Control: no-store.
func (s *Service) CoopStaffPhone(ctx context.Context, user uuid.UUID) (string, error) {
	p, coops, err := s.coopsOfFarmer(ctx)
	if err != nil {
		return "", err
	}
	u, err := s.Dir.GetUser(ctx, user)
	if errors.Is(err, port.ErrUserNotFound) {
		return "", ErrStaffNotFound
	}
	if err != nil {
		return "", dirErr(err)
	}
	if u.TenantID != p.TenantID || !slices.Contains(coops, u.OrgRef) {
		return "", ErrStaffNotFound // không lộ tài khoản ngoài HTX của nông dân
	}
	staff, err := s.staffOf(ctx, p, u.OrgRef)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(staff, func(st CoopStaff) bool { return st.UserRef == user })
	if i < 0 {
		return "", ErrStaffNotFound
	}
	phone := domain.NormalizePhone(u.Phone)
	if !domain.ValidPhone(phone) {
		return "", ErrNoContact
	}
	return phone, nil
}
