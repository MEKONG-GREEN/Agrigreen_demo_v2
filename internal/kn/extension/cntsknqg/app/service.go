// Package app: use case mẫu biểu dự án khuyến nông quốc gia (KNQG) phòng Chăn nuôi – Thủy sản — W11.53, D4 CN13, BRD CNTS
// v3.7 FN-19, UR-TINH-20…24, UR-QG-01, Mục III.5, quy tắc liên biểu 9, 10, 11.
//
// Biểu gắn dự án loại "knqg" và điểm trình diễn CỦA KN (W11.47, extension/cntsmodel — chỉ đọc sổ của gói đó); thực tế Mẫu
// 05 lấy từ nhật ký nuôi đã xác thực (W11.48, extension/cntsdiary — chỉ đọc). KN không ghi dữ liệu HTX (ADR-0055, Q-KNS-04).
//
// Quyền theo ngữ cảnh phiên KN (knport.RequireSession), phòng Chăn nuôi – Thủy sản (BRD Mục VI.2, FN-19):
//   - Lập, sửa, trình duyệt/trình ký, ký (tải scan), điều chỉnh, gửi Mẫu 07: CHỦ NHIỆM của dự án, lãnh đạo phòng (Cấp Tỉnh,
//     trong tỉnh), Admin (FN-19 BR-01, AC-04 — chủ nhiệm dự án khác không sửa được).
//   - Duyệt/trả lại Mẫu 01: lãnh đạo phòng (không phải ngữ cảnh chỉ xem — Ban Giám đốc), Admin. Trả lại bắt buộc ý kiến.
//   - Cấp TW (TTKN Quốc gia): chỉ dự án KNQG; chỉ xem, trừ xác nhận / trả lại Mẫu 07 đã gửi (UR-QG-01). Mẫu 07 chưa gửi lần
//     nào không hiện với TW.
//   - Cấp Tỉnh khác (viên chức, tổng hợp, Ban Giám đốc): xem trong tỉnh. Cấp Xã: xem Mẫu 05 của điểm thuộc xã mình.
//
// Lỗi #156: không I/O ngoài (tên cán bộ, đơn vị hành chính, kho định danh, kho đối tượng, pf/export) bên trong giao dịch DB;
// mọi thao tác ghi tuần tự theo dự án (khóa tư vấn) và phát lại theo Idempotency-Key (#112).
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/store"
	mdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	mstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi (docs/errors/pf-extension-cnts-knqg.md).
var (
	ErrDocNotFound   = problem.New(http.StatusNotFound, "extension.cnts_knqg_not_found", "Không có biên bản/báo cáo dự án KNQG")
	ErrDocInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_knqg_invalid", "Mẫu biểu dự án KNQG không hợp lệ")
	ErrDocIncomplete = problem.New(http.StatusUnprocessableEntity, "extension.cnts_knqg_incomplete", "Biểu chưa đủ nội dung bắt buộc")
	ErrDocConflict   = problem.New(http.StatusConflict, "extension.cnts_knqg_version_conflict", "Biểu đã có người cập nhật — tải lại bản mới")
	ErrDocState      = problem.New(http.StatusConflict, "extension.cnts_knqg_state", "Thao tác không hợp lệ ở trạng thái hiện tại của biểu")
	ErrDocExists     = problem.New(http.StatusConflict, "extension.cnts_knqg_exists", "Đã có biểu hiệu lực cho dự án ở kỳ/điểm này — mở biểu đó (điều chỉnh nếu đã duyệt/ký)")
	ErrDocLocked     = problem.New(http.StatusConflict, "extension.cnts_knqg_locked", "Biểu đã duyệt/ký/xác nhận — không sửa được")
	ErrNeedSigned05  = problem.New(http.StatusConflict, "extension.cnts_knqg_needs_signed_05", "Mẫu 07 chỉ gửi khi mọi điểm có Mẫu 05 đã ký")
	ErrProjectKind   = problem.New(http.StatusUnprocessableEntity, "extension.cnts_knqg_project_kind", "Mẫu biểu KNQG chỉ lập cho dự án loại khuyến nông quốc gia")
	ErrProjectClose  = problem.New(http.StatusConflict, "extension.cnts_project_closed", "Dự án đã kết thúc — mở lại trước khi sửa")
	ErrScanInvalid   = problem.New(http.StatusUnprocessableEntity, "extension.cnts_knqg_scan_invalid", "Bản scan phải là PDF, JPG hoặc PNG, tối đa 10 MB")
	ErrScanNotFound  = problem.New(http.StatusNotFound, "extension.cnts_knqg_scan_not_found", "Biểu chưa có bản scan đã ký")
	ErrReadOnly      = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency    = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối kho tệp, pf/export hoặc đơn vị hành chính")
	errBaseVersion   = problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// Service là use case mẫu biểu dự án KNQG.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Units (tùy chọn): tên xã/tỉnh trên biểu, dòng địa danh Word.
	Units partyport.AdminUnits
	// Names (tùy chọn): tên chủ nhiệm.
	Names func() partyport.ActorNames
	// Vault (tùy chọn): nhãn chủ hộ (che bớt) cho Phụ lục I Mẫu 05. nil → bỏ trống tên.
	Vault func() client.Client
	// Store: kho đối tượng cho bản scan đã ký (cùng kho của kho văn bản phòng). nil → ký/tải scan trả 503.
	Store func() objstore.Store
	// Exporter: pf/export (Word đúng mẫu). nil → xuất Word trả 503.
	Exporter exportport.Exporter
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) objects() objstore.Store {
	if s.Store == nil {
		return nil
	}
	return s.Store()
}

// caller là người gọi đã giải ngữ cảnh phiên KN.
type caller struct {
	p authz.Principal
	k knport.SessionContext
}

func (c caller) admin() bool    { return c.k.Role == knport.SessionRoleAdmin }
func (c caller) province() bool { return c.k.Role == knport.SessionRoleProvince }
func (c caller) commune() bool  { return c.k.Role == knport.SessionRoleCommune }
func (c caller) national() bool { return c.k.Role == knport.SessionRoleNational }
func (c caller) me() uuid.UUID  { return c.p.ActorRef }
func (c caller) sees(code string) bool {
	return c.admin() || c.k.Scope.Allows(code)
}

// manager: lãnh đạo phòng Cấp Tỉnh (không ở ngữ cảnh chỉ xem).
func (c caller) manager() bool {
	return c.province() && !c.k.ReadOnly() && (c.k.Manager || c.k.ProvinceGroup == accport.GroupLanhDaoPhong)
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
		return c, problem.Forbidden.WithDetail("Mẫu biểu dự án KNQG thuộc phòng Chăn nuôi – Thủy sản")
	}
	return c, nil
}

// writer: reader + kiểm ghi + khóa idem. national=true cho thao tác của Cấp TW (xác nhận/trả lại Mẫu 07) — TW là vai chỉ
// xem trừ thao tác này (UR-QG-01); ngữ cảnh ViewOnly vẫn bị chặn.
func (s *Service) writer(ctx context.Context, idem string, national bool) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	switch {
	case c.k.ViewOnly || (c.k.ReadOnly() && !(national && c.national())):
		return c, ErrReadOnly
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return c, ErrSupportReason
	case strings.TrimSpace(idem) == "":
		return c, problem.IdempotencyRequired
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
	for i, e := range es {
		if i >= 30 {
			break
		}
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

func versionOK(base *int64, seq int64) bool { return base != nil && *base == seq }

// ---------------------------------------------------------------------------
// Quyền theo dự án / biểu
// ---------------------------------------------------------------------------

// seesProject: Admin; Cấp Tỉnh trong tỉnh; Cấp TW trong tỉnh và chỉ dự án KNQG (BRD Mục VI.1).
func (c caller) seesProject(p mstore.Project) bool {
	switch {
	case c.admin():
		return true
	case c.province():
		return c.sees(p.Province)
	case c.national():
		return p.Kind == mdomain.KindNational && c.sees(p.Province)
	}
	return false
}

// seesDoc: như dự án; Cấp Xã chỉ Mẫu 05 của điểm thuộc xã mình; TW không thấy Mẫu 07 chưa gửi lần nào.
func (c caller) seesDoc(p mstore.Project, d store.Doc, siteCommune string) bool {
	if c.commune() {
		return d.Form == domain.Form05 && siteCommune != "" && c.sees(siteCommune)
	}
	if !c.seesProject(p) {
		return false
	}
	if c.national() && d.Form == domain.Form07 && d.Status == domain.StatusDraft {
		return false
	}
	return true
}

// editor: chủ nhiệm của dự án, lãnh đạo phòng (trong tỉnh), Admin (FN-19 BR-01, AC-04).
func (c caller) editor(p mstore.Project) bool {
	if c.k.ReadOnly() {
		return false
	}
	return c.admin() || (c.province() && c.sees(p.Province) && (c.manager() || p.ManagerRef == c.me()))
}

// approver: duyệt/trả lại Mẫu 01 — lãnh đạo phòng (trong tỉnh), Admin.
func (c caller) approver(p mstore.Project) bool {
	return (c.admin() && !c.k.ReadOnly()) || (c.manager() && c.sees(p.Province))
}

// nationalActor: Cấp TW xác nhận/trả lại Mẫu 07 (không ở ngữ cảnh ViewOnly).
func (c caller) nationalActor(p mstore.Project) bool {
	return (c.national() && !c.k.ViewOnly && p.Kind == mdomain.KindNational && c.sees(p.Province)) || (c.admin() && !c.k.ReadOnly())
}

// may cho biết người gọi được làm thao tác act với biểu (chưa xét trạng thái).
func (c caller) may(p mstore.Project, form, act string) bool {
	switch act {
	case domain.ActApprove:
		return c.approver(p)
	case domain.ActReturn:
		if form == domain.Form07 {
			return c.nationalActor(p)
		}
		return c.approver(p)
	case domain.ActConfirm:
		return c.nationalActor(p)
	}
	return c.editor(p)
}

// noPII chặn số điện thoại/CCCD trong văn bản tự do (dữ liệu cá nhân phải vào kho định danh).
func noPII(fields map[string]string) error {
	for f, v := range fields {
		if exdomain.ContainsPII(v) {
			return ErrDocInvalid.WithField(f, exdomain.ErrPIIInText.Error())
		}
	}
	return nil
}

// names tra tên hiển thị; lỗi tra cứu không chặn.
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

// unitName tra tên đơn vị hành chính (lỗi → mã).
func (s *Service) unitName(ctx context.Context, code string) string {
	if s.Units == nil || code == "" {
		return code
	}
	u, err := s.Units.Get(ctx, code, s.now())
	if err != nil || u.Name == "" {
		return code
	}
	return u.Name
}
