// Package app: use case chương trình/dự án, điểm trình diễn, hộ mô hình và cấp quyền cán bộ xã/nông hộ của phòng Chăn
// nuôi – Thủy sản — W11.47, D4 CN7, BRD CNTS v3.7 FN-10 (phần hộ), FN-14, FN-19 (phần CT/DA), UR-XA-04, UR-TINH-09,
// UR-TINH-19.
//
// Quyền theo ngữ cảnh phiên KN (knport.RequireSession), phòng Chăn nuôi – Thủy sản:
//   - CT/DA: Cấp Tỉnh nhóm chủ nhiệm/lãnh đạo phòng (hoặc chưa phân nhóm) tạo; chỉ CHỦ NHIỆM của dự án, lãnh đạo phòng,
//     Admin sửa (FN-19 BR-01, AC-04). Cấp Tỉnh, Cấp TW xem; Cấp Xã xem điểm/hộ của xã mình.
//   - Điểm trình diễn: người sửa được dự án.
//   - Hộ mô hình: người sửa được dự án; Cấp Xã là cán bộ theo dõi của điểm hoặc có quyền "cán bộ theo dõi mô hình" (FN-14)
//     bao điểm/hộ (UR-XA-04).
//   - Cấp quyền (FN-14): Cấp Tỉnh (tổng hợp, chủ nhiệm, lãnh đạo phòng) và Admin; chủ nhiệm chỉ cấp vai theo dõi mô
//     hình/nông hộ trong dự án mình (BR-03); địa bàn ngoài tỉnh bị chặn.
//
// ADR-0055, Q-KNS-04: KN không ghi dữ liệu HTX — mọi bản ghi là của KN; HTX tham gia chỉ là mã đọc qua pf_cooperative_pub.
// Họ tên, SĐT chủ hộ ở kho định danh (vault); sổ chỉ giữ person_ref.
package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/txdeps"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi (docs/errors/pf-extension-cnts-model.md).
var (
	ErrProjectNotFound  = problem.New(http.StatusNotFound, "extension.cnts_project_not_found", "Không có chương trình, dự án")
	ErrProjectInvalid   = problem.New(http.StatusUnprocessableEntity, "extension.cnts_project_invalid", "Chương trình, dự án không hợp lệ")
	ErrProjectConflict  = problem.New(http.StatusConflict, "extension.cnts_project_version_conflict", "Dự án đã có người cập nhật — tải lại bản mới")
	ErrProjectCodeTaken = problem.New(http.StatusConflict, "extension.cnts_project_code_taken", "Mã dự án đã được dùng")
	ErrProjectClosed    = problem.New(http.StatusConflict, "extension.cnts_project_closed", "Dự án đã kết thúc — mở lại trước khi sửa")
	ErrSiteNotFound     = problem.New(http.StatusNotFound, "extension.cnts_site_not_found", "Không có điểm trình diễn")
	ErrSiteInvalid      = problem.New(http.StatusUnprocessableEntity, "extension.cnts_site_invalid", "Điểm trình diễn không hợp lệ")
	ErrSiteConflict     = problem.New(http.StatusConflict, "extension.cnts_site_version_conflict", "Điểm đã có người cập nhật — tải lại bản mới")
	ErrSiteClosed       = problem.New(http.StatusConflict, "extension.cnts_site_closed", "Điểm trình diễn đã đóng")
	ErrHouseholdNF      = problem.New(http.StatusNotFound, "extension.cnts_household_not_found", "Không có hộ mô hình")
	ErrHouseholdInvalid = problem.New(http.StatusUnprocessableEntity, "extension.cnts_household_invalid", "Hộ mô hình không hợp lệ")
	ErrHouseholdConfl   = problem.New(http.StatusConflict, "extension.cnts_household_version_conflict", "Hộ đã có người cập nhật — tải lại bản mới")
	ErrHouseholdExists  = problem.New(http.StatusConflict, "extension.cnts_household_exists", "Chủ hộ này đã có trong điểm trình diễn")
	ErrHouseholdOut     = problem.New(http.StatusConflict, "extension.cnts_household_withdrawn", "Hộ đã rút khỏi mô hình")
	ErrGrantNotFound    = problem.New(http.StatusNotFound, "extension.cnts_grant_not_found", "Không có quyền đã cấp")
	ErrGrantInvalid     = problem.New(http.StatusUnprocessableEntity, "extension.cnts_grant_invalid", "Cấp quyền không hợp lệ")
	ErrGrantConflict    = problem.New(http.StatusConflict, "extension.cnts_grant_version_conflict", "Quyền đã có người cập nhật — tải lại bản mới")
	ErrGrantExists      = problem.New(http.StatusConflict, "extension.cnts_grant_exists", "Tài khoản đã có quyền còn hiệu lực với vai này — sửa quyền đó")
	ErrGrantRevoked     = problem.New(http.StatusConflict, "extension.cnts_grant_revoked", "Quyền đã thu hồi")
	ErrReadOnly         = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason    = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency       = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối danh mục CNTS, đơn vị hành chính, kho định danh hoặc danh bạ Cấp Xã")
	ErrOutOfScope       = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	errBaseVersion      = problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)")
	errIdem             = problem.Validation.WithField("Idempotency-Key", "bắt buộc")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// Service là use case CT/DA, điểm, hộ mô hình, cấp quyền CNTS.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Units: danh mục đơn vị hành chính (xã thuộc tỉnh). nil → ghi trả 503.
	Units partyport.AdminUnits
	// Catalogs: danh mục CNTS (loại mô hình, cơ sở giết mổ — W11.41). nil → ghi điểm/quyền cơ sở trả 503.
	Catalogs knport.CNTSCatalogs
	// Programs (tùy chọn): danh sách CT/DA chủ của KHTC (D20). nil → không gắn program_ref được.
	Programs knport.KHTCPrograms
	// Officers: cán bộ Cấp Xã CNTS (pf/accounts) — người được cấp quyền, cán bộ theo dõi, người ghi nhật ký phải là tài
	// khoản Cấp Xã có sẵn (FN-14 "tài khoản đã có"). nil → các thao tác cần kiểm trả 503.
	Officers accport.XaScopes
	// Vault: kho định danh — họ tên, SĐT chủ hộ. nil → tạo hộ bằng họ tên trả 503; hiển thị không có nhãn.
	Vault client.Client
	// Names (tùy chọn): tên cán bộ.
	Names func() partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func day(t time.Time) time.Time {
	v := t.In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) today() time.Time { return day(s.now()) }

// tx chạy fn trong giao dịch tenant. P0-POOL-KN-2 (#139): ctx mang txdeps (txdeps.With ở đầu thao tác ghi) → port của module
// khác (đơn vị hành chính, danh mục CNTS, cán bộ Cấp Xã, CT/DA KHTC) chỉ đọc NGOÀI giao dịch; thiếu thì hủy, nạp, chạy lại fn.
func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	err := txdeps.Run(ctx, func(body func(pgx.Tx) error) error { return tenant.WithTenantTx(ctx, s.DB, tid, body) }, fn)
	if errors.Is(err, txdeps.ErrExhausted) {
		return ErrDependency.WithDetail("danh mục, đơn vị hành chính hoặc danh bạ Cấp Xã thay đổi liên tục — thử lại").WithCause(err)
	}
	return err
}

// unit đọc đơn vị hành chính còn hiệu lực hôm nay (port module khác — qua txdeps).
func (s *Service) unit(ctx context.Context, code string) (partyport.AdminUnit, error) {
	day := s.today()
	return txdeps.Get(ctx, "unit:"+code+"@"+day.Format(time.DateOnly), func(ctx context.Context) (partyport.AdminUnit, error) {
		return s.Units.Get(ctx, code, day)
	})
}

type caller struct {
	p authz.Principal
	k knport.SessionContext
}

func (c caller) admin() bool    { return c.k.Role == knport.SessionRoleAdmin }
func (c caller) province() bool { return c.k.Role == knport.SessionRoleProvince }
func (c caller) commune() bool  { return c.k.Role == knport.SessionRoleCommune }
func (c caller) me() uuid.UUID  { return c.p.ActorRef }

// sees cho biết người gọi thấy dữ liệu của đơn vị code.
func (c caller) sees(code string) bool { return c.admin() || c.k.Scope.Allows(code) }

// writable: không ở ngữ cảnh chỉ xem.
func (c caller) writable() bool { return !c.k.ReadOnly() }

// manager: lãnh đạo phòng Cấp Tỉnh (hoặc cấp quản lý ≥ phó trưởng phòng).
func (c caller) manager() bool {
	return c.province() && (c.k.Manager || c.k.ProvinceGroup == accport.GroupLanhDaoPhong)
}

// reader: Admin; Cấp Tỉnh, Cấp Xã, Cấp TW phòng Chăn nuôi – Thủy sản.
func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := knport.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	c := caller{p: p, k: k}
	switch {
	case k.Role == knport.SessionRoleAdmin:
	case (k.Role == knport.SessionRoleProvince || k.Role == knport.SessionRoleCommune || k.Role == knport.SessionRoleNational) &&
		k.AllowsDepartment(DeptCNTS):
	default:
		return c, problem.Forbidden.WithDetail("Chương trình, dự án, hộ mô hình thuộc phòng Chăn nuôi – Thủy sản")
	}
	return c, nil
}

func (c caller) checkWrite() error {
	switch {
	case c.k.ReadOnly():
		return ErrReadOnly
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return ErrSupportReason
	}
	return nil
}

// writer: reader + kiểm ghi + khóa idem.
func (s *Service) writer(ctx context.Context, idem string) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if err := c.checkWrite(); err != nil {
		return c, err
	}
	if strings.TrimSpace(idem) == "" {
		return c, errIdem
	}
	if s.Units == nil {
		return c, ErrDependency
	}
	return c, nil
}

func (c caller) meta(at time.Time, idem string) store.Meta {
	m := store.Meta{ID: ids.New(), EventTime: at, ActorRef: c.p.ActorRef, IdemKey: idem}
	if c.k.Support != nil {
		r := strings.TrimSpace(c.k.Support.Reason)
		m.SupportReason = &r
		if c.k.Support.OnBehalfOf != uuid.Nil {
			v := c.k.Support.OnBehalfOf
			m.OnBehalfOf = &v
		}
	}
	return m
}

func dup(err error) bool { return errors.Is(ledger.Classify(err), ledger.ErrDuplicate) }

func fieldsProblem(base *problem.Problem, es domain.Errors) *problem.Problem {
	p := base
	for _, e := range es {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

// noPII chặn số điện thoại/CCCD trong văn bản tự do (dữ liệu cá nhân phải vào kho định danh).
func noPII(base *problem.Problem, fields map[string]string) error {
	for f, v := range fields {
		if exdomain.ContainsPII(v) {
			return base.WithField(f, exdomain.ErrPIIInText.Error())
		}
	}
	return nil
}

// provinceOf trả mã tỉnh của xã code còn hiệu lực ("" nếu không phải xã).
func (s *Service) provinceOf(ctx context.Context, code string) (string, error) {
	u, err := s.unit(ctx, code)
	if errors.Is(err, partyport.ErrUnitNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if u.Level != partyport.LevelCommune {
		return "", nil
	}
	return u.ParentCode, nil
}

// callerProvince trả tỉnh của người gọi Cấp Tỉnh (suy từ phạm vi do server giải). Admin: requested bắt buộc.
func (s *Service) callerProvince(ctx context.Context, c caller, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	provinces := map[string]bool{}
	if !c.admin() {
		for _, code := range c.k.Scope.Units {
			u, err := s.unit(ctx, code)
			if errors.Is(err, partyport.ErrUnitNotFound) {
				continue
			}
			if err != nil {
				return "", err
			}
			if u.Level == partyport.LevelProvince {
				provinces[u.Code] = true
			}
		}
		if requested == "" && len(provinces) == 1 {
			for p := range provinces {
				requested = p
			}
		}
	}
	if requested == "" {
		return "", problem.Validation.WithField("province_code", "chọn tỉnh")
	}
	u, err := s.unit(ctx, requested)
	if errors.Is(err, partyport.ErrUnitNotFound) || (err == nil && u.Level != partyport.LevelProvince) {
		return "", problem.Validation.WithField("province_code", "phải là tỉnh còn hiệu lực")
	}
	if err != nil {
		return "", err
	}
	if !c.admin() && !provinces[requested] {
		return "", ErrOutOfScope.WithField("province_code", "ngoài tỉnh phụ trách")
	}
	return requested, nil
}

// communeIn kiểm xã code thuộc tỉnh province và trong phạm vi người gọi.
func (s *Service) communeIn(ctx context.Context, c caller, base *problem.Problem, field, code, province string) error {
	if !domain.ValidUnitCode(code) {
		return base.WithField(field, "mã xã không hợp lệ")
	}
	p, err := s.provinceOf(ctx, code)
	if err != nil {
		return err
	}
	if p == "" {
		return base.WithField(field, "không phải xã/phường còn hiệu lực")
	}
	if p != province || !c.sees(code) {
		return ErrOutOfScope.WithField(field, "xã "+code+" ngoài tỉnh/phạm vi phụ trách (FN-14: gán địa bàn ngoài tỉnh bị chặn)")
	}
	return nil
}

// officers trả danh bạ cán bộ Cấp Xã CNTS theo mã tài khoản.
func (s *Service) officers(ctx context.Context, c caller) (map[uuid.UUID]accport.XaScope, error) {
	if s.Officers == nil {
		return nil, ErrDependency
	}
	list, err := txdeps.Get(ctx, "xa_scopes:"+c.p.TenantID.String(), func(ctx context.Context) ([]accport.XaScope, error) {
		return s.Officers.XaScopes(ctx, c.p.TenantID, DeptCNTS)
	})
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]accport.XaScope, len(list))
	for _, x := range list {
		out[x.ActorRef] = x
	}
	return out, nil
}

// checkOfficer: ref là cán bộ Cấp Xã CNTS còn hiệu lực phụ trách xã commune.
func (s *Service) checkOfficer(ctx context.Context, c caller, base *problem.Problem, field string, ref uuid.UUID, commune string) error {
	idx, err := s.officers(ctx, c)
	if err != nil {
		return err
	}
	x, ok := idx[ref]
	if !ok {
		return base.WithField(field, "không phải tài khoản Cấp Xã phòng Chăn nuôi – Thủy sản còn hiệu lực")
	}
	if commune != "" && !slices.Contains(x.Communes, commune) {
		return base.WithField(field, "cán bộ không phụ trách xã "+commune)
	}
	return nil
}

// names tra tên hiển thị; lỗi tra cứu không chặn đọc.
func (s *Service) names(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Names == nil || len(refs) == 0 {
		return out
	}
	n := s.Names()
	if n == nil {
		return out
	}
	if m, err := n.DisplayNames(ctx, uniq(refs)); err == nil && m != nil {
		return m
	}
	return out
}

// catalogNames trả tên mục của danh mục (đang dùng hôm nay).
func (s *Service) catalogNames(ctx context.Context, catalog string) map[string]string {
	out := map[string]string{}
	if s.Catalogs == nil {
		return out
	}
	items, err := s.Catalogs.ListAt(ctx, catalog, s.today())
	if err != nil {
		return out
	}
	for _, it := range items {
		out[it.Code] = it.Name
	}
	return out
}

func uniq(refs []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(refs))
	for _, r := range refs {
		if r != uuid.Nil && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// myFollowerGrants trả quyền "cán bộ theo dõi mô hình" còn hiệu lực của người gọi.
func (s *Service) myFollowerGrants(ctx context.Context, tx pgx.Tx, c caller) ([]store.Grant, error) {
	if !c.commune() {
		return nil, nil
	}
	gs, err := store.ListGrants(ctx, tx, &c.p.ActorRef)
	if err != nil {
		return nil, err
	}
	out := gs[:0]
	for _, g := range gs {
		if g.Role == domain.RoleFollower && domain.GrantStatus(g.Event, g.ValidFrom, g.ValidTo, s.today()) == domain.StatusActive {
			out = append(out, g)
		}
	}
	return out, nil
}

func versionOK(base *int64, seq int64) bool { return base != nil && *base == seq }
