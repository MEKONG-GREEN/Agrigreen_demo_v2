// Package app: use case vòng đời báo cáo KN v7.7 (gói P4) — phát hành biểu, công việc, bài nộp có phiên bản,
// rà soát, chốt/mở lại kỳ, tổng hợp các xã, dashboard Tỉnh/TW, báo cáo của xã. Quyền theo ngữ cảnh phiên KN
// (authz.RequireKN: vai, phòng ban, phạm vi Nút/xã phụ trách — P1), không theo vai realm.
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors.md, mục pf/extension — vòng đời báo cáo KN v7.7).
var (
	ErrBatchNotFound        = problem.New(http.StatusNotFound, "extension.report_batch_not_found", "Không tìm thấy đợt phát hành")
	ErrTaskNotFound         = problem.New(http.StatusNotFound, "extension.report_task_not_found", "Không tìm thấy công việc")
	ErrPeriodNotFound       = problem.New(http.StatusNotFound, "extension.report_period_not_found", "Không tìm thấy kỳ báo cáo")
	ErrSubmissionNotFound   = problem.New(http.StatusNotFound, "extension.submission_not_found", "Không tìm thấy bài nộp")
	ErrRunNotFound          = problem.New(http.StatusNotFound, "extension.consolidation_not_found", "Kỳ chưa có dữ liệu tổng hợp")
	ErrFormNotAssigned      = problem.New(http.StatusUnprocessableEntity, "extension.form_not_assigned", "Mẫu chưa được gán cho loại báo cáo của phòng ban")
	ErrFormNoStructure      = problem.New(http.StatusUnprocessableEntity, "extension.form_no_structure", "Mẫu chưa có cấu trúc cột nhập liệu")
	ErrSlotsExceeded        = problem.New(http.StatusUnprocessableEntity, "extension.period_slots_exceeded", "Số kỳ vượt số khe kỳ của mẫu")
	ErrNoCommunes           = problem.New(http.StatusUnprocessableEntity, "extension.no_communes", "Chưa chọn xã nhận hoặc xã không thuộc tỉnh")
	ErrBatchHasSubmissions  = problem.New(http.StatusConflict, "extension.batch_has_submissions", "Đợt đã có xã nộp — không thu hồi được")
	ErrBatchWithdrawn       = problem.New(http.StatusConflict, "extension.batch_withdrawn", "Đợt đã thu hồi")
	ErrNoFuturePeriod       = problem.New(http.StatusConflict, "extension.deadline_no_future_period", "Không còn kỳ chưa đến hạn để sửa hạn nộp")
	ErrPeriodClosed         = problem.New(http.StatusConflict, "extension.period_closed", "Kỳ đã chốt — không gửi/sửa được nếu chưa mở lại kỳ")
	ErrPeriodOpen           = problem.New(http.StatusConflict, "extension.period_not_closed", "Kỳ chưa chốt")
	ErrSubmissionInvalid    = problem.New(http.StatusUnprocessableEntity, "extension.submission_invalid", "Số liệu chưa đạt yêu cầu của mẫu")
	ErrNotSubmitted         = problem.New(http.StatusConflict, "extension.task_not_submitted", "Xã chưa gửi báo cáo")
	ErrAdjustmentAfterClose = problem.New(http.StatusConflict, "extension.adjustment_after_close", "Kỳ đã chốt — mở lại kỳ trước khi yêu cầu điều chỉnh")
	ErrReadOnly             = problem.New(http.StatusForbidden, "extension.read_only", "Cấp Quốc gia chỉ xem")
	ErrOutOfScope           = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrSupportReason        = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency           = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
)

// Service là use case của vòng đời báo cáo.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app
	Clock clock.Clock
	Log   *slog.Logger

	Forms       repport.FormEngine      // P3: mẫu, phiên bản, Evaluate, Totals
	Regions     partyport.Regions       // P2: khu vực của Nút xã
	Orgs        partyport.Orgs          // tỉnh của Nút trong ngữ cảnh
	Units       partyport.AdminUnits    // tên xã/tỉnh
	Params      params.Params           // tham số nhắc, đánh số tuần (tạm, Q-15, Q-17)
	Assignments accport.Assignments     // P1: cán bộ xã phụ trách cùng phòng ban
	Names       partyport.ActorNames    // tên người thao tác trong Log (tùy chọn)
	Dispatches  repport.DispatchHistory // pf/reports: kỳ đã gửi Chi cục (tùy chọn)
	Freezer     formsport.Freezer       // W6.2 cap/forms: ghi song song chốt/mở lại + ảnh chụp (tùy chọn)
	FormsNative formsport.Publisher     // W11.42 cap/forms: cài biểu, phát hành lô kỳ native (biểu kỳ CNTS)
	// W11.44: số liệu kỳ native theo lô (tổng hợp BM-02, BM-18) và danh mục cơ sở giết mổ (tên, mộc dấu — tùy chọn).
	FormsFigures   formsport.FiguresReader
	CNTSFacilities CNTSFacilityCatalog
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.New(slog.DiscardHandler)
	}
	return s.Log
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// actor là người gọi trong ngữ cảnh phiên KN.
type actor struct {
	p        authz.Principal
	k        authz.KNContext
	province string // mã tỉnh của Nút (Cấp Tỉnh/Xã); rỗng = mọi tỉnh (Cấp Quốc gia, Admin)
}

func (a actor) dept() string {
	if a.k.Role == authz.KNAdmin {
		return ""
	}
	return a.k.Department
}

// onBehalf trả on_behalf_of của chế độ hỗ trợ (nil nếu không hỗ trợ hoặc không gắn tài khoản).
func (a actor) onBehalf() *uuid.UUID {
	if a.k.Support == nil || a.k.Support.OnBehalfOf == uuid.Nil {
		return nil
	}
	v := a.k.Support.OnBehalfOf
	return &v
}

// supportInfo trả phiên hỗ trợ + lý do; lỗi nếu đang hỗ trợ mà thiếu lý do (middleware P1 đã kiểm X-AG-Reason).
func (a actor) supportInfo() (*uuid.UUID, *string, error) {
	if a.k.Support == nil {
		return nil, nil, nil
	}
	if len([]rune(a.k.Support.Reason)) < 10 {
		return nil, nil, ErrSupportReason
	}
	id, r := a.k.Support.SessionID, a.k.Support.Reason
	return &id, &r, nil
}

// knActor kiểm vai trong ngữ cảnh phiên (rỗng = mọi vai) và tìm tỉnh của Nút.
func (s *Service) knActor(ctx context.Context, roles ...string) (actor, error) {
	p, k, err := authz.RequireKN(ctx, roles...)
	if err != nil {
		return actor{}, err
	}
	a := actor{p: p, k: k}
	if k.Role == authz.KNTW || k.Role == authz.KNAdmin || k.OrgRef == uuid.Nil {
		return a, nil
	}
	if s.Orgs == nil {
		return a, ErrDependency.WithDetail("chưa nối cây tổ chức (pf/party)")
	}
	org, err := s.Orgs.Get(ctx, k.OrgRef)
	if err != nil {
		return a, err
	}
	switch org.Kind {
	case partyport.OrgTTKNProvince:
		a.province = org.AdminUnitCode
	case partyport.OrgTTKNNational:
	default:
		a.province = org.AdminUnitCode
		if s.Units != nil {
			if u, err := s.Units.Get(ctx, org.AdminUnitCode, s.now()); err == nil && u.ParentCode != "" {
				a.province = u.ParentCode
			}
		}
	}
	return a, nil
}

// writer kiểm vai ghi (Cấp Quốc gia chỉ xem).
func (s *Service) writer(ctx context.Context, roles ...string) (actor, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return a, err
	}
	if a.k.ReadOnly() {
		return a, ErrReadOnly
	}
	if !slices.Contains(roles, a.k.Role) {
		return a, problem.Forbidden.WithDetail("vai trò của ngữ cảnh phiên không được thao tác này")
	}
	return a, nil
}

// allowsProvince: dữ liệu của tỉnh × phòng ban thuộc ngữ cảnh (Cấp Tỉnh/Xã: tỉnh mình; TW/Admin: mọi tỉnh).
func (a actor) allowsProvince(province, dept string) bool {
	if !a.k.AllowsDepartment(dept) {
		return false
	}
	return a.province == "" || a.province == province
}

// allowsTask: Cấp Xã chỉ xã phụ trách; vai khác theo tỉnh.
func (a actor) allowsTask(t store.Task) bool {
	if !a.allowsProvince(t.ProvinceCode, t.DepartmentCode) {
		return false
	}
	if a.k.Role == authz.KNXa {
		return slices.Contains(a.k.Communes, t.CommuneCode)
	}
	return true
}

// paramInt đọc tham số số nguyên; chưa có → mặc định.
func (s *Service) paramInt(ctx context.Context, key string, def int) int {
	if s.Params == nil {
		return def
	}
	v, err := s.Params.At(ctx, key, s.now())
	if err != nil {
		return def
	}
	return int(v.Value.IntPart())
}

// CalendarAt trả lịch tuần của phòng dept (D8) có hiệu lực lúc 00:00 giờ VN của ngày from: tham số
// port.ParamPeriodRule+":"+dept → domain.BuiltinCalendars → port.ParamPeriodRule → domain.LegacyRule
// (domain.ResolveCalendar). Lịch mới chỉ áp dụng cho đợt/kỳ bắt đầu từ ngày hiệu lực của phiên bản tham số; kỳ đã
// phát hành giữ period_rule đã ghi (tag mang cal=<phòng>:v<n>, đọc lại bằng domain.CalendarOf). Không kiểm quyền —
// chỉ đọc tham số (PFPUB dùng để biết lịch hiện hành của phòng).
func (s *Service) CalendarAt(ctx context.Context, dept string, from time.Time) domain.Calendar {
	at := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, clock.Zone())
	text := func(key string) string {
		if s.Params == nil {
			return ""
		}
		v, err := s.Params.At(ctx, key, at)
		if err != nil {
			return ""
		}
		return v.Text
	}
	return domain.ResolveCalendar(dept, text(port.ParamPeriodRule+":"+dept), text(port.ParamPeriodRule))
}

// ksgmStarts đọc ranh giới tuần KSGM từng tháng (port.ParamKSGMWeekStarts:YYYY-MM) có hiệu lực tại ngày 1 của tháng.
func (s *Service) ksgmStarts(ctx context.Context) domain.KSGMStarts {
	if s.Params == nil {
		return nil
	}
	return func(y int, m time.Month) []int {
		first := domain.Date(y, m, 1)
		v, err := s.Params.At(ctx, port.ParamKSGMWeekStarts+":"+first.Format("2006-01"), time.Date(y, m, 1, 0, 0, 0, 0, clock.Zone()))
		if err != nil || v.Text == "" {
			return nil
		}
		st, err := domain.ParseKSGMStarts(v.Text)
		if err != nil {
			return nil
		}
		return st
	}
}

func idemOr(key, suffix string) string {
	if key == "" {
		key = "gen-" + ids.New().String()
	}
	if suffix != "" {
		key += ":" + suffix
	}
	return key
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func refStrings(rs []uuid.UUID) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.String())
	}
	return out
}

func ptrString(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

// names tra tên hiển thị (RB-3: tra lúc đọc, không chép vào sổ); lỗi tra tên không chặn màn hình.
func (s *Service) names(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	out := map[uuid.UUID]string{port.SystemActor: "Hệ thống"}
	if s.Names == nil || len(refs) == 0 {
		return out
	}
	m, err := s.Names.DisplayNames(ctx, refs)
	if err != nil {
		s.log().Warn("không tra được tên người thao tác", "event", "extension.names", "outcome", "error")
		return out
	}
	for k, v := range m {
		out[k] = v
	}
	return out
}

var errStop = errors.New("stop")
