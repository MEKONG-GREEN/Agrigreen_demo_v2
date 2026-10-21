// Package app: use case nhật ký nuôi BM-08/BM-09/BM-19 của phòng Chăn nuôi – Thủy sản — W11.48, D4 CN8, BRD CNTS v3.7
// FN-10 (nhật ký), FN-13 (rà soát, xác thực), UR-XA-11, UR-XA-12, UR-TINH-12, Mục III.5.
//
// Nhật ký gắn hộ mô hình CỦA KN (W11.47, extension/cntsmodel — chỉ đọc sổ của gói đó): không ghi dữ liệu HTX (ADR-0055,
// Q-KNS-04). Quyền theo ngữ cảnh phiên KN (knport.RequireSession), phòng Chăn nuôi – Thủy sản:
//   - Ghi (lứa, lần ghi, ảnh): Cấp Xã là cán bộ theo dõi của điểm, người ghi nhật ký của hộ, hoặc có quyền "cán bộ theo dõi
//     mô hình" (FN-14) bao điểm/hộ; Admin (chế độ hỗ trợ, có lý do). Cấp Tỉnh không nhập đè số liệu gốc (FN-04 BR-04).
//   - Rà soát, xác thực, yêu cầu bổ sung (UR-TINH-12): Cấp Tỉnh là CHỦ NHIỆM của dự án hoặc lãnh đạo phòng; Admin. Người
//     xác thực khác người ghi.
//   - Xem: như hộ mô hình (Cấp Xã theo xã/hộ được giao; Cấp Tỉnh, TW trong tỉnh). Bản nháp chỉ người ghi thấy; tọa độ lúc
//     ghi và ảnh chỉ người ghi được, người rà soát được và Admin thấy.
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
	"github.com/shopspring/decimal"

	alertapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/store"
	mdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	mstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi (docs/errors/pf-extension-cnts-diary.md).
var (
	ErrCycleNotFound   = problem.New(http.StatusNotFound, "extension.cnts_diary_cycle_not_found", "Không có lứa nuôi")
	ErrCycleInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_diary_cycle_invalid", "Lứa nuôi không hợp lệ")
	ErrCycleConflict   = problem.New(http.StatusConflict, "extension.cnts_diary_cycle_version_conflict", "Lứa đã có người cập nhật — tải lại bản mới")
	ErrCycleClosed     = problem.New(http.StatusConflict, "extension.cnts_diary_cycle_closed", "Lứa đã kết thúc (đã hạch toán) — mở lại trước khi ghi")
	ErrCycleOpenExists = problem.New(http.StatusConflict, "extension.cnts_diary_cycle_open_exists", "Hộ đang có lứa chưa kết thúc")
	ErrEntryNotFound   = problem.New(http.StatusNotFound, "extension.cnts_diary_entry_not_found", "Không có lần ghi nhật ký")
	ErrEntryInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_diary_entry_invalid", "Lần ghi không hợp lệ")
	ErrEntryBlocked    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_diary_entry_blocked", "Lần ghi chưa qua kiểm tra — sửa lỗi chặn trước khi gửi")
	ErrEntryConflict   = problem.New(http.StatusConflict, "extension.cnts_diary_entry_version_conflict", "Lần ghi đã có người cập nhật — tải lại bản mới")
	ErrEntryState      = problem.New(http.StatusConflict, "extension.cnts_diary_entry_state", "Thao tác không hợp lệ ở trạng thái hiện tại của lần ghi")
	ErrEntryLocked     = problem.New(http.StatusConflict, "extension.cnts_diary_entry_locked", "Lần ghi đã khóa sửa (đã xác thực hoặc quá hạn sửa)")
	ErrEntryDayTaken   = problem.New(http.StatusConflict, "extension.cnts_diary_entry_day_taken", "Lứa đã có lần ghi của ngày này")
	ErrSelfVerify      = problem.New(http.StatusForbidden, "extension.cnts_diary_self_verify", "Người xác thực phải khác người ghi")
	ErrPhotoInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_diary_photo_invalid", "Ảnh không hợp lệ")
	ErrPhotoNoKey      = problem.New(http.StatusConflict, "extension.cnts_diary_photo_no_key", "Chủ hộ chưa có khóa mã hóa ở kho định danh")
	ErrPhotoNotFound   = problem.New(http.StatusNotFound, "extension.cnts_diary_photo_not_found", "Không có ảnh nhật ký")
	ErrReadOnly        = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối kho tệp, kho định danh hoặc đơn vị hành chính")
	errBaseVersion     = problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)")
	errIdem            = problem.Validation.WithField("Idempotency-Key", "bắt buộc")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// Tham số (pf/masterdata, masterdata_params.json của module) và mặc định.
const (
	ParamDistance  = "cnts.location_distance_m"            // 200 m
	ParamAccuracy  = "cnts.gps_accuracy_m"                 // 50 m
	ParamPHMin     = "cnts.indicator_min:PH"               // 7,5
	ParamPHMax     = "cnts.indicator_max:PH"               // 8,5
	ParamDOMin     = "cnts.indicator_min:DO"               // 4 mg/l
	ParamMortality = knport.ParamCNTSMortalityPctPrefix    // + ":<loại mô hình>"; chưa cấu hình → không cảnh báo
	ParamEditDays  = knport.ParamCNTSDiaryEditDays         // 7 ngày
	ParamRetention = knport.ParamCNTSDiaryFixRetentionDays // 365 ngày
	SubjectModule  = "pf/extension"
	SubjectType    = "cnts_household" // chủ thể tệp ảnh nhật ký ở pf/evidence (R-DB-07)
	// Purpose là mục đích mở DEK ảnh nhật ký (danh mục OpUnwrap của kho định danh; pf/evidence không kiểm đồng ý của hộ —
	// cơ sở xử lý là nhiệm vụ theo dõi, xác thực mô hình của cơ quan, giả định tạm chờ DPIA Q-005).
	Purpose = "extension.diary"
)

// Alerts là cổng sinh cảnh báo dòng 6–9 (extension/cntsalert).
type Alerts interface {
	LogVerdict(ctx context.Context, tx pgx.Tx, tid uuid.UUID, a alertapp.LogAlert) error
}

// Service là use case nhật ký nuôi CNTS.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Units: tỉnh của xã (cảnh báo). nil → ghi trả 503.
	Units partyport.AdminUnits
	// Params (tùy chọn): ngưỡng; thiếu → mặc định trong mã.
	Params params.Params
	// Alerts (tùy chọn): cảnh báo dòng 6–9. nil → chỉ đánh dấu lần ghi.
	Alerts Alerts
	// Vault: khóa công khai của chủ hộ (mã hóa ảnh), nhãn chủ hộ. nil → tải ảnh trả 503.
	Vault client.Client
	// Files: kho tệp pf/evidence. nil → tải/xem ảnh trả 503.
	Files evport.Files
	// Names (tùy chọn): tên cán bộ.
	Names func() partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) today() time.Time { return domain.Day(s.now(), clock.Zone()) }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) param(ctx context.Context, key string, def string) decimal.Decimal {
	if v := s.optParam(ctx, key); v != nil {
		return *v
	}
	return decimal.RequireFromString(def)
}

func (s *Service) optParam(ctx context.Context, key string) *decimal.Decimal {
	if s.Params == nil {
		return nil
	}
	v, err := s.Params.At(ctx, key, s.now())
	if err != nil {
		return nil
	}
	d := v.Value
	return &d
}

func (s *Service) thresholds(ctx context.Context, modelType string) domain.Thresholds {
	phMin, phMax, doMin := s.param(ctx, ParamPHMin, "7.5"), s.param(ctx, ParamPHMax, "8.5"), s.param(ctx, ParamDOMin, "4")
	return domain.Thresholds{PHMin: &phMin, PHMax: &phMax, DOMin: &doMin, MortalityMaxPct: s.optParam(ctx, ParamMortality+":"+modelType)}
}

func (s *Service) editDays(ctx context.Context) int {
	return int(s.param(ctx, ParamEditDays, "7").IntPart())
}

// caller là người gọi đã giải ngữ cảnh phiên KN.
type caller struct {
	p authz.Principal
	k knport.SessionContext
}

func (c caller) admin() bool    { return c.k.Role == knport.SessionRoleAdmin }
func (c caller) province() bool { return c.k.Role == knport.SessionRoleProvince }
func (c caller) commune() bool  { return c.k.Role == knport.SessionRoleCommune }
func (c caller) me() uuid.UUID  { return c.p.ActorRef }
func (c caller) writable() bool { return !c.k.ReadOnly() }
func (c caller) sees(code string) bool {
	return c.admin() || c.k.Scope.Allows(code)
}

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
		return c, problem.Forbidden.WithDetail("Nhật ký nuôi thuộc phòng Chăn nuôi – Thủy sản")
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

func versionOK(base *int64, seq int64) bool { return base != nil && *base == seq }

// ---------------------------------------------------------------------------
// Ngữ cảnh hộ mô hình (W11.47 — chỉ đọc)
// ---------------------------------------------------------------------------

// hctx là hộ mô hình kèm điểm, dự án và quyền theo dõi của người gọi.
type hctx struct {
	h      mstore.Household
	x      mstore.Site
	p      mstore.Project
	grants []mstore.Grant
}

// loadHousehold đọc hộ, điểm, dự án (nil nếu không có / người gọi không thấy).
func (s *Service) loadHousehold(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) (*hctx, error) {
	h, err := mstore.LatestHousehold(ctx, tx, ref)
	if err != nil || h == nil || !c.seesHousehold(*h) {
		return nil, err
	}
	x, err := mstore.LatestSite(ctx, tx, h.SiteRef)
	if err != nil || x == nil {
		return nil, err
	}
	p, err := mstore.LatestProject(ctx, tx, h.ProjectRef)
	if err != nil || p == nil {
		return nil, err
	}
	out := &hctx{h: *h, x: *x, p: *p}
	if c.commune() {
		gs, err := mstore.ListGrants(ctx, tx, &c.p.ActorRef)
		if err != nil {
			return nil, err
		}
		for _, g := range gs {
			if g.Role == mdomain.RoleFollower && mdomain.GrantStatus(g.Event, g.ValidFrom, g.ValidTo, s.today()) == mdomain.StatusActive {
				out.grants = append(out.grants, g)
			}
		}
	}
	return out, nil
}

// seesHousehold: theo xã; Cấp Xã có giới hạn hộ (W11.07) chỉ thấy hộ được giao.
func (c caller) seesHousehold(h mstore.Household) bool {
	if !c.sees(h.CommuneCode) {
		return false
	}
	return !c.commune() || c.k.AllowsHousehold(h.Ref)
}

// keeper: người gọi là người ghi nhật ký của hộ, cán bộ theo dõi điểm, hoặc có quyền theo dõi mô hình bao điểm/hộ.
func (c caller) keeper(hc *hctx) bool {
	if hc == nil || !c.writable() {
		return false
	}
	if c.admin() {
		return true
	}
	if !c.commune() || !c.sees(hc.h.CommuneCode) {
		return false
	}
	if (hc.h.KeeperRef != nil && *hc.h.KeeperRef == c.me()) || (hc.x.OfficerRef != nil && *hc.x.OfficerRef == c.me()) {
		return true
	}
	for _, g := range hc.grants {
		if g.Covers(hc.x.CommuneCode, hc.x.Ref, hc.h.Ref) {
			return true
		}
	}
	return false
}

// open: dự án, điểm chưa kết thúc và hộ chưa rút.
func (hc *hctx) open() bool {
	return hc.p.Event != mdomain.EventClosed && hc.x.Event != mdomain.EventClosed && hc.h.Event != mdomain.EventWithdrawn
}

// reviewer: chủ nhiệm của dự án hoặc lãnh đạo phòng (Cấp Tỉnh, trong tỉnh), Admin (UR-TINH-12: "Chủ nhiệm chỉ rà soát
// chương trình mình phụ trách").
func (c caller) reviewer(p mstore.Project) bool {
	if !c.writable() {
		return false
	}
	return c.admin() || (c.province() && c.sees(p.Province) && (c.manager() || p.ManagerRef == c.me()))
}

// privileged: người thấy tọa độ lúc ghi và ảnh (người ghi được, người rà soát được, Admin).
func (c caller) privileged(hc *hctx) bool { return c.keeper(hc) || c.reviewer(hc.p) || c.admin() }

// provinceOf trả mã tỉnh của xã (rỗng nếu không tra được).
func (s *Service) provinceOf(ctx context.Context, code string) string {
	if s.Units == nil {
		return ""
	}
	u, err := s.Units.Get(ctx, code, s.today())
	if err != nil || u.Level != partyport.LevelCommune {
		return ""
	}
	return u.ParentCode
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
	seen := map[uuid.UUID]bool{}
	var u []uuid.UUID
	for _, r := range refs {
		if r != uuid.Nil && !seen[r] {
			seen[r] = true
			u = append(u, r)
		}
	}
	if m, err := n.DisplayNames(ctx, u); err == nil && m != nil {
		return m
	}
	return out
}

// ownerLabel trả nhãn chủ hộ (che bớt) từ kho định danh; lỗi kho không chặn đọc.
func (s *Service) ownerLabel(ctx context.Context, person uuid.UUID) string {
	if s.Vault == nil {
		return ""
	}
	d, err := s.Vault.Display(ctx, client.PurposeFieldSupport, person)
	if err != nil {
		return ""
	}
	return d.Label
}
