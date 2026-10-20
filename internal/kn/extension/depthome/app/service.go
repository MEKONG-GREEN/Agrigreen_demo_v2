// Package app: use case Trang chủ phòng + kho văn bản phòng (W11.54 — D4 CN14; BRD CNTS v3.7 FN-17, FN-20; trang chủ
// tương ứng của các phòng KN).
//
// Quyền theo ngữ cảnh phiên KN (port.RequireSession) và phòng ban của phiên:
//   - Trang chủ (FN-17): Admin (chọn phòng), Cấp Tỉnh của phòng (kể cả nhóm chỉ xem — Ban Giám đốc, BR-02). Lãnh đạo
//     phòng (Manager) thấy thêm thẻ "chờ duyệt" và sửa giới thiệu, chức năng nhiệm vụ. Cấp Xã, Cấp TW không có (ma trận).
//   - Kho văn bản (FN-20): Admin, Cấp Tỉnh của phòng CRU; Lãnh đạo phòng (và Admin) thêm D = ẩn/hiện lại; Cấp Xã của
//     phòng chỉ xem văn bản có đối tượng "xa"; ngữ cảnh chỉ xem chỉ đọc. Cấp TW không có.
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

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/depthome/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors/pf-extension-dept-home.md).
var (
	ErrProfileInvalid  = problem.New(http.StatusUnprocessableEntity, "extension.dept_profile_invalid", "Giới thiệu phòng không hợp lệ")
	ErrProfileConflict = problem.New(http.StatusConflict, "extension.dept_profile_version_conflict", "Giới thiệu phòng đã có người cập nhật — tải lại bản mới")
	ErrFolderNotFound  = problem.New(http.StatusNotFound, "extension.dept_folder_not_found", "Không có thư mục này")
	ErrFolderInvalid   = problem.New(http.StatusUnprocessableEntity, "extension.dept_folder_invalid", "Thư mục không hợp lệ")
	ErrFolderExists    = problem.New(http.StatusConflict, "extension.dept_folder_exists", "Đã có thư mục cùng tên ở cùng cấp")
	ErrFolderNotEmpty  = problem.New(http.StatusConflict, "extension.dept_folder_not_empty", "Thư mục còn thư mục con hoặc văn bản đang hiện")
	ErrFolderConflict  = problem.New(http.StatusConflict, "extension.dept_folder_version_conflict", "Thư mục đã có người cập nhật — tải lại bản mới")
	ErrDocNotFound     = problem.New(http.StatusNotFound, "extension.dept_doc_not_found", "Không có văn bản này")
	ErrDocInvalid      = problem.New(http.StatusUnprocessableEntity, "extension.dept_doc_invalid", "Văn bản không hợp lệ")
	ErrDocConflict     = problem.New(http.StatusConflict, "extension.dept_doc_version_conflict", "Văn bản đã có người cập nhật — tải lại bản mới")
	ErrDocState        = problem.New(http.StatusConflict, "extension.dept_doc_state", "Văn bản đã ở trạng thái này")
	ErrFileType        = problem.New(http.StatusUnprocessableEntity, "extension.dept_doc_file_type", "Định dạng tệp không được phép")
	ErrFileTooLarge    = problem.New(http.StatusRequestEntityTooLarge, "extension.dept_doc_too_large", "Tệp vượt 20 MB")
	ErrReadOnly        = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason   = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency      = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Chưa nối kho đối tượng")
)

// Departments là các phòng KN có trang chủ, kho văn bản (BRD 5 phòng).
var Departments = []string{partyport.DeptTrongTrot, partyport.DeptChanNuoiThuySan, partyport.DeptKeHoachTaiChinh,
	partyport.DeptThongTinTruyenThong, partyport.DeptHanhChinhTongHop}

// Officer là một cán bộ Cấp Tỉnh của phòng cho sơ đồ tổ chức (lấy từ tài khoản — UR-TINH-06 "sau này từ Module HCTH").
type Officer struct {
	ActorRef  uuid.UUID
	MgmtLevel string
	Leader    bool
}

// ClosedReader là phần đọc của port.ClosedFigures mà trang chủ dùng.
type ClosedReader interface {
	ClosedFigures(ctx context.Context, tid uuid.UUID, q port.ClosedFiguresQuery) ([]port.ClosedFigure, error)
}

// Sources là nguồn đọc của module khác/bộ phận khác của pf/extension, nối ở module_depthome.go. Trường nil → thẻ tương
// ứng rỗng (không lỗi).
type Sources struct {
	// Closed: số liệu đã chốt của phòng (họ closed_figures/v1 — hợp đồng _pub W11.10).
	Closed ClosedReader
	// Officers: cán bộ Cấp Tỉnh của phòng trong tỉnh của người gọi (pf/accounts KNDirectory qua gốc lắp ráp).
	Officers func(ctx context.Context, tid uuid.UUID, dept string) ([]Officer, error)
	// Names: tên hiển thị của tài khoản (danh bạ Contact/HCTH, dự phòng danh bạ trực).
	Names func() partyport.ActorNames
}

// Service là use case trang chủ + kho văn bản phòng.
type Service struct {
	DB      tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock   clock.Clock
	Store   objstore.Store // kho đối tượng của tệp văn bản; nil → tải lên/tải xuống 503
	Sources Sources
	// PriceProfile: hồ sơ bảng giá của view pf_price_pub (mặc định "kn").
	PriceProfile string
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// day là ngày (giờ Việt Nam) của t, dạng UTC 00:00.
func day(t time.Time) time.Time {
	v := t.In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) today() time.Time { return day(s.now()) }

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) readTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTxOptions(ctx, s.DB, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

type caller struct {
	p    authz.Principal
	k    port.SessionContext
	dept string // phòng của thao tác
}

func (c caller) admin() bool    { return c.k.Role == port.SessionRoleAdmin }
func (c caller) province() bool { return c.k.Role == port.SessionRoleProvince }
func (c caller) commune() bool  { return c.k.Role == port.SessionRoleCommune }

// manager: Admin hoặc Lãnh đạo phòng Cấp Tỉnh (W11.07 Manager) — không ở ngữ cảnh chỉ xem.
func (c caller) manager() bool {
	return !c.k.ReadOnly() && (c.admin() || (c.province() && c.k.Manager))
}

// leaderView: Admin hoặc Lãnh đạo phòng, kể cả Ban Giám đốc chỉ xem (BR-02: xem như Lãnh đạo phòng, không sửa).
func (c caller) leaderView() bool { return c.admin() || (c.province() && c.k.Manager) }

// writer: Admin, Cấp Tỉnh của phòng không ở ngữ cảnh chỉ xem.
func (c caller) writer() bool { return !c.k.ReadOnly() && (c.admin() || c.province()) }

func (c caller) checkWrite() error {
	switch {
	case c.k.ReadOnly():
		return ErrReadOnly
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return ErrSupportReason
	}
	return nil
}

// resolve giải người gọi và phòng: Admin chọn phòng bằng tham số (bắt buộc); vai khác dùng phòng của phiên — tham số
// khác phòng của phiên → 403.
func (s *Service) resolve(ctx context.Context, dept string, roles ...string) (caller, error) {
	p, k, err := port.RequireSession(ctx)
	if err != nil {
		return caller{}, err
	}
	c := caller{p: p, k: k}
	if !slices.Contains(roles, k.Role) {
		return c, problem.Forbidden.WithDetail("chức năng dành cho " + roleList(roles))
	}
	dept = strings.TrimSpace(dept)
	if c.admin() {
		if dept == "" {
			return c, problem.Validation.WithField("department", "Admin chọn phòng ban")
		}
	} else {
		if dept != "" && dept != k.Department {
			return c, problem.Forbidden.WithDetail("chỉ xem được phòng ban của ngữ cảnh đang chọn")
		}
		dept = k.Department
	}
	if !slices.Contains(Departments, dept) {
		return c, problem.Validation.WithField("department", "phòng ban KN không hợp lệ")
	}
	c.dept = dept
	return c, nil
}

func roleList(roles []string) string {
	names := map[string]string{port.SessionRoleAdmin: "Admin", port.SessionRoleProvince: "Cấp Tỉnh", port.SessionRoleCommune: "Cấp Xã",
		port.SessionRoleNational: "Cấp TW"}
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, names[r])
	}
	return strings.Join(out, ", ")
}

func (c caller) meta(event string, at time.Time, idem string) store.Meta {
	m := store.Meta{ID: ids.New(), Event: event, EventTime: at, ActorRef: c.p.ActorRef, IdemKey: idem}
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

// names tra tên hiển thị; thiếu nguồn hoặc lỗi → bản đồ rỗng (giao diện hiện mã).
func (s *Service) names(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{}
	if s.Sources.Names == nil || len(refs) == 0 {
		return out
	}
	n := s.Sources.Names()
	if n == nil {
		return out
	}
	if got, err := n.DisplayNames(ctx, refs); err == nil {
		return got
	}
	return out
}

// ---------------------------------------------------------------------------
// Giới thiệu, chức năng nhiệm vụ (FN-17 luồng phụ)
// ---------------------------------------------------------------------------

// ProfileView là phiên bản giới thiệu kèm tên người cập nhật.
type ProfileView struct {
	store.ProfileRow
	ActorName string
}

// ProfileInput là lần lưu giới thiệu phòng.
type ProfileInput struct {
	Department  string
	Intro       string
	Duties      string
	BaseVersion int64 // 0 = chưa có bản nào
}

// SaveProfile ghi một phiên bản giới thiệu phòng (Admin, Lãnh đạo phòng).
func (s *Service) SaveProfile(ctx context.Context, in ProfileInput, idem string) (ProfileView, error) {
	if idem == "" {
		return ProfileView{}, problem.IdempotencyRequired
	}
	c, err := s.resolve(ctx, in.Department, port.SessionRoleAdmin, port.SessionRoleProvince)
	if err != nil {
		return ProfileView{}, err
	}
	if err := c.checkWrite(); err != nil {
		return ProfileView{}, err
	}
	if !c.manager() {
		return ProfileView{}, problem.Forbidden.WithDetail("chỉ Lãnh đạo phòng sửa giới thiệu, chức năng nhiệm vụ (FN-17)")
	}
	pr := domain.Profile{Intro: in.Intro, Duties: in.Duties}
	if es := pr.Validate(); len(es) > 0 {
		return ProfileView{}, fieldsProblem(ErrProfileInvalid, es)
	}
	var out store.ProfileRow
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ProfileByIdem(ctx, tx, idem); err != nil || prev != nil {
			if prev != nil {
				out = *prev
			}
			return err
		}
		if err := store.Lock(ctx, tx, "profile:"+c.dept); err != nil {
			return err
		}
		hist, err := store.ProfileHistory(ctx, tx, c.dept, 1)
		if err != nil {
			return err
		}
		var cur int64
		if len(hist) > 0 {
			cur = hist[0].Seq
		}
		if in.BaseVersion != cur {
			return ErrProfileConflict.With("current_version", cur)
		}
		out = store.ProfileRow{Department: c.dept, Profile: pr, Meta: c.meta(domain.EventProfileSaved, s.now(), idem)}
		return store.InsertProfile(ctx, tx, c.p.TenantID, &out)
	})
	if dup(err) {
		return ProfileView{}, ErrProfileConflict
	}
	if err != nil {
		return ProfileView{}, err
	}
	return ProfileView{ProfileRow: out, ActorName: s.names(ctx, []uuid.UUID{out.ActorRef})[out.ActorRef]}, nil
}

// ProfileHistory trả các phiên bản giới thiệu phòng (mới nhất trước, tối đa 50).
func (s *Service) ProfileHistory(ctx context.Context, dept string) ([]ProfileView, error) {
	c, err := s.resolve(ctx, dept, port.SessionRoleAdmin, port.SessionRoleProvince)
	if err != nil {
		return nil, err
	}
	var rows []store.ProfileRow
	if err := s.readTx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var err error
		rows, err = store.ProfileHistory(ctx, tx, c.dept, 50)
		return err
	}); err != nil {
		return nil, err
	}
	refs := make([]uuid.UUID, 0, len(rows))
	for _, r := range rows {
		refs = append(refs, r.ActorRef)
	}
	nm := s.names(ctx, refs)
	out := make([]ProfileView, 0, len(rows))
	for _, r := range rows {
		out = append(out, ProfileView{ProfileRow: r, ActorName: nm[r.ActorRef]})
	}
	return out, nil
}
