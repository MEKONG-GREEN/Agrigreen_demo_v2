// Package app: use case quy tắc liên biểu + 12 cảnh báo của phòng Chăn nuôi – Thủy sản (W11.43 — D4 CN3, BRD CNTS v3.7
// FN-13, UR-TINH-08, UR-TINH-13, Mục III.5).
//
//   - Sinh cảnh báo sau commit (consumer inbox port.CNTSAlertConsumer): bài nộp biểu kỳ cap/forms profile kn_cnts
//     (BM-01, BM-04, BM-17 — dòng 1–5 và quy tắc liên biểu số 1) và ổ dịch mới (dòng 12). Ngưỡng: tham số có phiên bản
//     cnts.* (pf/masterdata, W11.41) tại thời điểm bài nộp. "Mỗi đối tượng, loại, kỳ chỉ một cảnh báo"; số liệu sửa hết
//     vi phạm → tự đóng.
//   - Vòng đời (nhiệm vụ SLA — cap/task đang cài ở pf/extension): Mới → Đã giao → Đang xử lý → Đã đóng (kết quả bắt
//     buộc); quá hạn xử lý → Quá hạn, báo Lãnh đạo phòng (job).
//   - Cấu hình nhắc nộp theo biểu (UR-TINH-08) và job nhắc trước hạn / khi trễ / nhắc lặp, trễ liên tiếp (FN-13 AC-06).
//
// Quyền theo ngữ cảnh phiên KN (port.RequireSession), phòng CNTS: Cấp Tỉnh xem, giao, đóng, cấu hình; Cấp Xã xem cảnh báo
// của các xã phụ trách và xử lý cảnh báo được giao; Admin toàn quyền; nhóm chỉ xem chỉ đọc.
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsalert/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// Mã lỗi (docs/errors/pf-extension-cnts-alert.md).
var (
	ErrAlertNotFound   = problem.New(http.StatusNotFound, "extension.cnts_alert_not_found", "Không có cảnh báo")
	ErrAlertTransition = problem.New(http.StatusConflict, "extension.cnts_alert_transition", "Trạng thái cảnh báo không cho phép thao tác này")
	ErrAlertConflict   = problem.New(http.StatusConflict, "extension.cnts_alert_version_conflict", "Cảnh báo đã có người cập nhật — tải lại bản mới")
	ErrAlertInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.cnts_alert_invalid", "Thao tác cảnh báo không hợp lệ")
	ErrConfigInvalid   = problem.New(http.StatusUnprocessableEntity, "extension.cnts_alert_config_invalid", "Cấu hình nhắc nhở / cảnh báo không hợp lệ")
	ErrReadOnly        = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối cap/forms hoặc danh bạ cán bộ")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// ProfileCNTS là profile cap/forms của biểu kỳ CNTS (W11.42).
const ProfileCNTS = "kn_cnts"

// SystemActor là người thao tác của sự việc do hệ thống ghi (consumer, job) — nhật ký hiện "Hệ thống".
var SystemActor = port.SystemActor

// Officer là một cán bộ của phòng (chọn người xử lý).
type Officer struct {
	ActorRef uuid.UUID
	Level    string // commune | province
	Communes []string
	Manager  bool
}

// Directory giải người nhận theo bảng Mục III.5 (gốc lắp ráp nối pf/accounts). Tenant truyền tường minh (consumer, job).
type Directory interface {
	// Officers: cán bộ Cấp Xã và Cấp Tỉnh còn hiệu lực của phòng CNTS × tỉnh.
	Officers(ctx context.Context, tid uuid.UUID, province string) ([]Officer, error)
	// FormOwners: Cấp Tỉnh CNTS có cờ nhiệm vụ "người phụ trách biểu" (W11.07).
	FormOwners(ctx context.Context, tid uuid.UUID) ([]uuid.UUID, error)
}

// Service là use case cảnh báo CNTS.
type Service struct {
	DB     tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock  clock.Clock
	Params params.Params // ngưỡng cnts.* — nil → mặc định BRD
	Units  partyport.AdminUnits
	// Feed: bài nộp cap/forms (đọc của hệ thống). nil → consumer bỏ qua bài nộp, job nhắc không chạy.
	Feed formsport.SubmissionFeed
	// Dir: người nhận (nil → thông báo theo chủ đề vai × đơn vị, không email).
	Dir Directory
	// Names (tùy chọn): tên cán bộ cho lịch sử, người xử lý.
	Names func() partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// tx chạy fn trong giao dịch tenant. P0-POOL-KN-2 (#139): ctx mang txdeps (job nhắc nộp, quá hạn xử lý) → đơn vị hành chính
// và danh bạ người nhận (port module khác) chỉ đọc NGOÀI giao dịch; thiếu thì hủy, nạp, chạy lại fn.
func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	err := txdeps.Run(ctx, func(body func(pgx.Tx) error) error { return tenant.WithTenantTx(ctx, s.DB, tid, body) }, fn)
	if errors.Is(err, txdeps.ErrExhausted) {
		return ErrDependency.WithDetail("đơn vị hành chính hoặc danh bạ cán bộ thay đổi liên tục — thử lại").WithCause(err)
	}
	return err
}

func dup(err error) bool { return errors.Is(ledger.Classify(err), ledger.ErrDuplicate) }

func newID() uuid.UUID { return ids.New() }

type caller struct {
	p authz.Principal
	k port.SessionContext
}

func (c caller) admin() bool { return c.k.Role == port.SessionRoleAdmin }
func (c caller) tinh() bool  { return c.k.Role == port.SessionRoleProvince }

// sees cho biết người gọi thấy cảnh báo a: Admin; Cấp Tỉnh trong phạm vi Nút (cảnh báo không gắn xã — mọi Cấp Tỉnh của
// phòng và đúng tỉnh); Cấp Xã: cảnh báo của các xã phụ trách hoặc được giao cho mình.
func (c caller) sees(a store.Alert) bool {
	switch {
	case c.admin():
		return true
	case a.AssigneeRef != nil && *a.AssigneeRef == c.p.ActorRef:
		return true
	case a.CommuneCode == "":
		return c.tinh() && a.ProvinceCode != "" && c.k.Scope.Allows(a.ProvinceCode)
	case c.tinh():
		return c.k.Scope.Allows(a.CommuneCode)
	}
	return slices.Contains(c.k.Communes, a.CommuneCode)
}

// reader: Admin; Cấp Tỉnh, Cấp Xã của phòng Chăn nuôi – Thủy sản (ma trận FN-13).
func (s *Service) reader(ctx context.Context) (caller, error) {
	p, k, err := port.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	c := caller{p: p, k: k}
	switch {
	case k.Role == port.SessionRoleAdmin:
	case (k.Role == port.SessionRoleProvince || k.Role == port.SessionRoleCommune) && k.AllowsDepartment(DeptCNTS):
	default:
		return c, problem.Forbidden.WithDetail("Cảnh báo CNTS thuộc phòng Chăn nuôi – Thủy sản (Admin, Cấp Tỉnh, Cấp Xã)")
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

// writer: người đọc được ghi (không chỉ xem, có lý do hỗ trợ).
func (s *Service) writer(ctx context.Context) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	return c, c.checkWrite()
}

// manager: Admin, Cấp Tỉnh CNTS — giao cảnh báo, cấu hình nhắc nhở và xử lý (FN-13 Cấp Tỉnh CRU).
func (s *Service) manager(ctx context.Context) (caller, error) {
	c, err := s.writer(ctx)
	if err != nil {
		return c, err
	}
	if !c.admin() && !c.tinh() {
		return c, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh giao cảnh báo, cấu hình nhắc nhở")
	}
	return c, nil
}

func (c caller) support() (*uuid.UUID, *string) {
	if c.k.Support == nil {
		return nil, nil
	}
	r := strings.TrimSpace(c.k.Support.Reason)
	if c.k.Support.OnBehalfOf != uuid.Nil {
		v := c.k.Support.OnBehalfOf
		return &v, &r
	}
	return nil, &r
}

// Actor là người thao tác hiển thị.
type Actor struct {
	Ref     uuid.UUID
	Display *string
}

func (s *Service) actors(ctx context.Context, refs []uuid.UUID) func(uuid.UUID) Actor {
	m := map[uuid.UUID]string{}
	if s.Names != nil && len(refs) > 0 {
		if n := s.Names(); n != nil {
			if got, err := n.DisplayNames(ctx, refs); err == nil {
				m = got
			}
		}
	}
	return func(r uuid.UUID) Actor {
		a := Actor{Ref: r}
		if n, ok := m[r]; ok && n != "" {
			nn := n
			a.Display = &nn
		}
		return a
	}
}

// provinceOf trả mã tỉnh của xã (rỗng nếu không tra được).
func (s *Service) provinceOf(ctx context.Context, commune string, at time.Time) string {
	if s.Units == nil || commune == "" {
		return ""
	}
	u, err := txdeps.Get(ctx, "unit:"+commune+"@"+at.UTC().Format(time.RFC3339Nano), func(ctx context.Context) (partyport.AdminUnit, error) {
		return s.Units.Get(ctx, commune, at)
	})
	if err != nil {
		return ""
	}
	return u.ParentCode
}
