// Package app: use case bản đồ vùng nuôi, vùng dịch của phòng Chăn nuôi – Thủy sản — W11.52, D4 CN12, BRD CNTS v3.7 FN-18,
// UR-TINH-17.
//
// Quyền theo ngữ cảnh phiên KN (knport.RequireSession — phiên do module sở hữu, không dùng Principal.KN), phòng Chăn nuôi
// – Thủy sản:
//   - Xem: Admin; Cấp Tỉnh (toàn tỉnh phụ trách); Cấp Xã — chỉ vùng có xã mình, ổ dịch, điểm, hộ của xã mình (FN-18 AC-02).
//   - Vẽ, sửa, ngưng, khôi phục vùng: Admin và Cấp Tỉnh nhóm viên chức chuyên môn (cán bộ phụ trách dịch — ma trận FN-18
//     CRU) hoặc chưa phân nhóm; các nhóm khác của Cấp Tỉnh chỉ xem. Ngữ cảnh chỉ xem (Ban Giám đốc, lãnh đạo xem phòng
//     khác, Cấp TW) không ghi được (extension.read_only). Vùng chỉ chứa xã thuộc tỉnh và phạm vi người vẽ.
//
// Mọi thay đổi là phiên bản mới của sổ cnts_map_zone (có lý do, ngày áp dụng); không xóa cứng.
package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmap/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors/pf-extension-cnts-map.md).
var (
	ErrZoneNotFound = problem.New(http.StatusNotFound, "extension.cnts_zone_not_found", "Không có vùng trên bản đồ")
	ErrZoneInvalid  = problem.New(http.StatusUnprocessableEntity, "extension.cnts_zone_invalid", "Vùng không hợp lệ")
	ErrZoneConflict = problem.New(http.StatusConflict, "extension.cnts_zone_version_conflict", "Vùng đã có người cập nhật — tải lại bản mới")
	ErrZoneRetired  = problem.New(http.StatusConflict, "extension.cnts_zone_retired", "Vùng đã ngưng hiển thị — khôi phục trước khi sửa")
	ErrReadOnly     = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReq   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency   = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối đơn vị hành chính hoặc danh mục CNTS")
	ErrOutOfScope   = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	errBaseVersion  = problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)")
	errIdem         = problem.Validation.WithField("Idempotency-Key", "bắt buộc")
	errReason       = problem.Validation.WithField("reason", "5–1000 ký tự")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// WarnOutsideCommunes: vùng nằm hoàn toàn ngoài địa giới các xã đã chọn (FN-18 luồng ngoại lệ "vẽ ngoài ranh giới → cảnh
// báo"); chỉ kiểm khi đã nối ranh giới hành chính của pf/geo và xã đã có ranh giới.
const WarnOutsideCommunes = "outside_communes"

// Service là use case bản đồ vùng nuôi, vùng dịch.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Units: đơn vị hành chính (xã thuộc tỉnh). nil → ghi trả 503.
	Units partyport.AdminUnits
	// Catalogs (tùy chọn): danh mục CNTS (loài, bệnh — W11.41). nil → gửi species_code/disease_code trả 503; đọc không có tên.
	Catalogs knport.CNTSCatalogs
	// Plots (tùy chọn): thống kê thửa đã số hóa theo xã — đọc qua công bố pf_cooperative_pub (KN không đọc bảng HTX).
	Plots knport.PlotStatistics
	// Geometry (tùy chọn): phép tính trên ranh giới hành chính của pf/geo — cảnh báo vẽ ngoài địa giới.
	Geometry geoport.AdminGeometry
	// Names (tùy chọn): tên cán bộ.
	Names func() partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func day(t time.Time) time.Time {
	v := t.In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) today() time.Time { return day(s.now()) }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k knport.SessionContext
}

func (c caller) admin() bool    { return c.k.Role == knport.SessionRoleAdmin }
func (c caller) province() bool { return c.k.Role == knport.SessionRoleProvince }
func (c caller) me() uuid.UUID  { return c.p.ActorRef }

// sees: dữ liệu của đơn vị code thuộc phạm vi.
func (c caller) sees(code string) bool { return c.admin() || c.k.Scope.Allows(code) }

// seesAny: một trong các xã thuộc phạm vi.
func (c caller) seesAny(codes []string) bool {
	for _, x := range codes {
		if c.sees(x) {
			return true
		}
	}
	return false
}

// drawer: Admin; Cấp Tỉnh viên chức chuyên môn (cán bộ phụ trách dịch) hoặc chưa phân nhóm — không ở ngữ cảnh chỉ xem.
func (c caller) drawer() bool {
	if c.k.ReadOnly() {
		return false
	}
	return c.admin() || (c.province() && (c.k.ProvinceGroup == "" || c.k.ProvinceGroup == accport.GroupVienChuc))
}

// reader: Admin; Cấp Tỉnh, Cấp Xã phòng Chăn nuôi – Thủy sản (FN-18). Cấp TW không có chức năng (BRD CNTS v3.7 §2).
func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := knport.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	c := caller{p: p, k: k}
	switch {
	case k.Role == knport.SessionRoleAdmin:
	case (k.Role == knport.SessionRoleProvince || k.Role == knport.SessionRoleCommune) && k.AllowsDepartment(DeptCNTS):
	default:
		return c, problem.Forbidden.WithDetail("Bản đồ vùng nuôi, vùng dịch thuộc phòng Chăn nuôi – Thủy sản (Admin, Cấp Tỉnh, Cấp Xã)")
	}
	return c, nil
}

// writer: reader + quyền vẽ + kiểm ghi + khóa idem.
func (s *Service) writer(ctx context.Context, idem string) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	switch {
	case c.k.ReadOnly():
		return c, ErrReadOnly
	case !c.drawer():
		return c, problem.Forbidden.WithDetail("Chỉ cán bộ phụ trách dịch Cấp Tỉnh (viên chức chuyên môn) hoặc Admin vẽ vùng (FN-18)")
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return c, ErrSupportReq
	case strings.TrimSpace(idem) == "":
		return c, errIdem
	case s.Units == nil:
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

// provinceOf trả mã tỉnh của xã code còn hiệu lực ("" nếu không phải xã).
func (s *Service) provinceOf(ctx context.Context, code string) (string, error) {
	u, err := s.Units.Get(ctx, code, s.today())
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
			u, err := s.Units.Get(ctx, code, s.today())
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
	u, err := s.Units.Get(ctx, requested, s.today())
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

// communesIn kiểm mọi xã thuộc tỉnh province và trong phạm vi người gọi.
func (s *Service) communesIn(ctx context.Context, c caller, codes []string, province string) error {
	for _, code := range codes {
		p, err := s.provinceOf(ctx, code)
		if err != nil {
			return err
		}
		if p == "" {
			return ErrZoneInvalid.WithField("communes", "không phải xã/phường còn hiệu lực: "+code)
		}
		if p != province || !c.sees(code) {
			return ErrOutOfScope.WithField("communes", "xã "+code+" ngoài tỉnh/phạm vi phụ trách")
		}
	}
	return nil
}

// checkCatalog kiểm mã danh mục CNTS còn dùng (bỏ qua khi giữ nguyên mã của phiên bản trước).
func (s *Service) checkCatalog(ctx context.Context, catalog, field, code, prev string) error {
	if code == "" || code == prev {
		return nil
	}
	if s.Catalogs == nil {
		return ErrDependency
	}
	it, err := s.Catalogs.ItemAt(ctx, catalog, code, s.today())
	if err != nil || !it.Active {
		return ErrZoneInvalid.WithField(field, "không có (hoặc đã ngừng) trong danh mục CNTS")
	}
	return nil
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
	if m, err := n.DisplayNames(ctx, refs); err == nil && m != nil {
		return m
	}
	return out
}

func newRef() uuid.UUID { return ids.New() }

func versionOK(base *int64, seq int64) bool { return base != nil && *base == seq }

// parseDay đọc ngày YYYY-MM-DD; rỗng = hôm nay.
func (s *Service) parseDay(v string) (time.Time, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return s.today(), nil
	}
	d, err := time.Parse(time.DateOnly, v)
	if err != nil {
		return time.Time{}, problem.Validation.WithField("date", "dạng YYYY-MM-DD")
	}
	return d, nil
}
