// Package app: use case lịch trực KSGM (BM-03) và lịch tiêm phòng (BM-07) — W11.46, D4 CN6, BRD CNTS v3.7 FN-15,
// UR-TINH-10, UR-TINH-11, UR-XA-03.
//
// Quyền theo ngữ cảnh phiên KN (knport.RequireSession), phòng Chăn nuôi – Thủy sản (ma trận FN-15: Admin CRUD, Cấp Tỉnh
// CRUD, Cấp Xã R + ghi ngày tiêm thực tế; Cấp TW không có). Trong Cấp Tỉnh (ma trận theo nhóm): cán bộ tổng hợp lập
// lịch trực, chủ nhiệm lập lịch tiêm; viên chức, lãnh đạo phòng chỉ xem; Ban Giám đốc (chỉ xem) không ghi. Lần gán
// Cấp Tỉnh CHƯA đặt nhóm (dữ liệu trước W11.07) được ghi cả hai — tạm, chờ Admin gán nhóm.
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntscal/store"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"

	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi (docs/errors/pf-extension-cnts-schedules.md).
var (
	ErrRosterInvalid   = problem.New(http.StatusUnprocessableEntity, "extension.cnts_roster_invalid", "Lịch trực KSGM không hợp lệ")
	ErrRosterConflict  = problem.New(http.StatusConflict, "extension.cnts_roster_version_conflict", "Lịch trực đã có người cập nhật — tải lại bản mới")
	ErrBatchNotFound   = problem.New(http.StatusNotFound, "extension.cnts_vaccination_not_found", "Không có lịch tiêm của lứa này")
	ErrBatchInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_vaccination_invalid", "Lịch tiêm phòng không hợp lệ")
	ErrBatchConflict   = problem.New(http.StatusConflict, "extension.cnts_vaccination_version_conflict", "Lịch tiêm đã có người cập nhật — tải lại bản mới")
	ErrBatchCancelled  = problem.New(http.StatusConflict, "extension.cnts_vaccination_cancelled", "Lịch tiêm của lứa đã hủy")
	ErrNoSchedule      = problem.New(http.StatusUnprocessableEntity, "extension.cnts_vaccination_no_procedure", "Không có quy trình tiêm cho loại mô hình/giống tại ngày nhận giống")
	ErrReadOnly        = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối danh mục CNTS hoặc danh mục đơn vị hành chính")
	ErrOutOfScope      = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	errBaseVersionNeed = problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// Service là use case lịch CNTS.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Units: danh mục đơn vị hành chính (xã của lứa). nil → ghi lịch tiêm trả 503.
	Units partyport.AdminUnits
	// Catalogs: danh mục CNTS theo ngày (cơ sở giết mổ, loại mô hình, giống, quy trình tiêm — W11.41). nil → 503.
	Catalogs knport.CNTSCatalogs
	// Officers (tùy chọn): cán bộ Cấp Xã CNTS kèm phạm vi (W11.07) — danh sách viên chức của lưới và cảnh báo "chưa được
	// cấp quyền cơ sở". nil → lưới không gợi ý viên chức, không cảnh báo quyền.
	Officers accport.XaScopes
	// Names (tùy chọn): tên cán bộ.
	Names func() partyport.ActorNames
	// Vault (tùy chọn, BF-CN-VACC #168): nhãn chủ hộ mô hình đã che bớt (kho định danh) cho danh sách lứa, bảng và tệp
	// BM-07. nil → không có nhãn chủ hộ.
	Vault client.Client
	// Contacts (tùy chọn, BF-CN-DUTY): SĐT công vụ của viên chức (pf/accounts) đọc lúc dựng lưới, nạp/xuất BM-03; không
	// lưu, không ghi log. Lưới chỉ trả SĐT đã che; tệp xuất chỉ có SĐT đầy đủ với người lập lịch. nil → không có cột ĐT.
	Contacts accport.StaffContacts
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// day là ngày (giờ Việt Nam) của t, dạng UTC 00:00.
func day(t time.Time) time.Time {
	v := t.In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) today() time.Time { return day(s.now()) }

// Today là ngày nghiệp vụ hôm nay (giờ Việt Nam) — cờ "quá hạn" của mũi tiêm.
func (s *Service) Today() time.Time { return s.today() }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	province     string // lịch trực tách theo tỉnh, suy từ Scope ở server
	legacyRoster bool   // chỉ Admin đọc lịch chung cũ
	p            authz.Principal
	k            knport.SessionContext
}

func (c caller) admin() bool { return c.k.Role == knport.SessionRoleAdmin }

// sees cho biết người gọi thấy dữ liệu của xã code.
func (c caller) sees(code string) bool { return c.admin() || c.k.Scope.Allows(code) }

// reader: Admin; Cấp Tỉnh, Cấp Xã phòng Chăn nuôi – Thủy sản (FN-15).
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
		return c, problem.Forbidden.WithDetail("Lịch trực KSGM, lịch tiêm phòng thuộc phòng Chăn nuôi – Thủy sản (Admin, Cấp Tỉnh, Cấp Xã)")
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

// planner: Admin hoặc Cấp Tỉnh thuộc một trong các nhóm groups (nhóm rỗng = chưa phân nhóm, được phép — tạm).
func (s *Service) planner(ctx context.Context, what string, groups ...string) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	if !c.admin() && (c.k.Role != knport.SessionRoleProvince || (c.k.ProvinceGroup != "" && !slices.Contains(groups, c.k.ProvinceGroup))) {
		return c, problem.Forbidden.WithDetail(what)
	}
	if err := c.checkWrite(); err != nil {
		return c, err
	}
	if s.Catalogs == nil {
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

// catalogNames trả tên mục đang dùng hôm nay của các danh mục.
func (s *Service) catalogNames(ctx context.Context, catalogs ...string) map[string]string {
	out := map[string]string{}
	if s.Catalogs == nil {
		return out
	}
	for _, cat := range catalogs {
		items, err := s.Catalogs.ListAt(ctx, cat, s.today())
		if err != nil {
			continue
		}
		for _, it := range items {
			out[cat+":"+it.Code] = it.Name
		}
	}
	return out
}
