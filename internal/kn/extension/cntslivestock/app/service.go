// Package app: use case danh sách hộ chăn nuôi CNTS — CNTS-V4-HOUSEHOLD-LIST (#251), ADR-0056, BRD CNTS v4.0 FN-10
// (H1–H4, luồng phụ "Hộ từ BM-10", luồng ngoại lệ, BR-03…BR-08, AC-05…AC-09, bảng trường HCN-01…HCN-09) và ma trận quyền
// "Danh sách hộ chăn nuôi (master data)".
//
// Quyền theo ngữ cảnh phiên KN (knport.RequireSession), phòng Chăn nuôi – Thủy sản:
//   - Admin: thêm, sửa, check-in, vô hiệu hóa.
//   - Cấp Tỉnh: cán bộ tổng hợp (nhóm tong_hop) thêm, sửa, check-in, vô hiệu hóa (BR-04); nhóm khác (viên chức, chủ
//     nhiệm, lãnh đạo phòng, Ban Giám đốc) chỉ xem.
//   - Cấp Xã: chỉ thấy, thêm, sửa, check-in hộ của xã mình phụ trách (AC-06); không vô hiệu hóa.
//   - Cấp TW, Nông hộ: không có quyền.
//
// Họ tên, SĐT chủ hộ ở kho định danh (mục đích extension.contact); danh sách dùng Display (nhãn che bớt), chi tiết Resolve
// (kho ghi nhật ký) cho người sửa được hộ. Mọi lời gọi kho, danh mục, đơn vị hành chính chạy NGOÀI giao dịch DB.
package app

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntslivestock/store"
	exdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
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

// Mã lỗi (docs/errors/pf-extension-cnts-livestock.md).
var (
	ErrNotFound    = problem.New(http.StatusNotFound, "extension.cnts_livestock_household_not_found", "Không có hộ chăn nuôi")
	ErrInvalid     = problem.New(http.StatusUnprocessableEntity, "extension.cnts_livestock_household_invalid", "Hộ chăn nuôi không hợp lệ")
	ErrNeedCheckin = problem.New(http.StatusUnprocessableEntity, "extension.cnts_livestock_household_checkin_required",
		"Thêm hộ phải check-in tại nhà hộ (lấy vị trí GPS) trước khi lưu")
	ErrConflict = problem.New(http.StatusConflict, "extension.cnts_livestock_household_version_conflict",
		"Hộ đã có người cập nhật — tải lại bản mới")
	ErrDuplicate = problem.New(http.StatusConflict, "extension.cnts_livestock_household_duplicate",
		"Trùng họ tên chủ hộ, ấp, xã với hộ đang dùng — xác nhận là hộ khác để lưu")
	ErrDeactivated = problem.New(http.StatusConflict, "extension.cnts_livestock_household_deactivated",
		"Hộ đã vô hiệu hóa — không sửa được")
	ErrPhoneTaken = problem.New(http.StatusConflict, "extension.cnts_livestock_household_phone_taken",
		"Số điện thoại đã thuộc chủ thể khác trong kho định danh")
	ErrReadOnly      = problem.New(http.StatusForbidden, "extension.read_only", "Ngữ cảnh chỉ xem")
	ErrSupportReason = problem.New(http.StatusUnprocessableEntity, "extension.support_reason_required", "Chế độ hỗ trợ: thao tác ghi bắt buộc lý do")
	ErrDependency    = problem.New(http.StatusServiceUnavailable, "extension.dependency_unavailable",
		"Chưa nối danh mục CNTS, đơn vị hành chính hoặc kho định danh")
	ErrOutOfScope = problem.New(http.StatusForbidden, "extension.out_of_scope", "Ngoài phạm vi phụ trách")
	errBase       = problem.Validation.WithField("base_version", "bắt buộc (khóa lạc quan)")
	errIdem       = problem.Validation.WithField("Idempotency-Key", "bắt buộc")
)

// DeptCNTS là phòng ban sở hữu nghiệp vụ.
const DeptCNTS = partyport.DeptChanNuoiThuySan

// ParamAccuracy là ngưỡng "Sai số GPS" (FN-13) — vượt thì cảnh báo, vẫn lưu.
const ParamAccuracy = "cnts.gps_accuracy_m"

// Cảnh báo không chặn của check-in.
const (
	WarnWeakGPS = "gps_accuracy" // sai số GPS > ngưỡng
	WarnMocked  = "mocked_location"
)

// Giới hạn đọc.
const (
	maxOwnerLabels = 300 // lời gọi Display mỗi trang (như hộ mô hình)
	maxDupScan     = 500 // số hộ so trùng tối đa (ADR-0056 mục 3)
	maxPage        = 200
	defaultPage    = 50
	maxOffset      = 100_000
)

// Service là use case danh sách hộ chăn nuôi.
type Service struct {
	DB    tenant.Beginner // pool vai extension_app (db.ForModule)
	Clock clock.Clock
	// Units: đơn vị hành chính (xã hiệu lực, FN-01). nil → ghi trả 503.
	Units partyport.AdminUnits
	// Catalogs: danh mục vật nuôi cnts_species (FN-06). nil → ghi trả 503.
	Catalogs knport.CNTSCatalogs
	// Params (tùy chọn): ngưỡng sai số GPS; thiếu → 50 m.
	Params params.Params
	// Vault: kho định danh — họ tên, SĐT chủ hộ. nil → ghi trả 503; đọc không có nhãn.
	Vault client.Client
	// Names (tùy chọn): tên cán bộ ở lịch sử.
	Names func() partyport.ActorNames
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) today() time.Time {
	v := s.now().In(clock.Zone())
	return time.Date(v.Year(), v.Month(), v.Day(), 0, 0, 0, 0, time.UTC)
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

type caller struct {
	p authz.Principal
	k knport.SessionContext
}

func (c caller) admin() bool    { return c.k.Role == knport.SessionRoleAdmin }
func (c caller) province() bool { return c.k.Role == knport.SessionRoleProvince }
func (c caller) commune() bool  { return c.k.Role == knport.SessionRoleCommune }

// sees: dữ liệu của xã code thuộc phạm vi người gọi.
func (c caller) sees(code string) bool { return c.admin() || c.k.Scope.Allows(code) }

// summary: cán bộ tổng hợp Cấp Tỉnh (ma trận quyền FN-10: CRU, vô hiệu hóa).
func (c caller) summary() bool { return c.province() && c.k.ProvinceGroup == accport.GroupTongHop }

// canWrite: thêm, sửa, check-in hộ của xã code.
func (c caller) canWrite(code string) bool {
	if c.k.ReadOnly() {
		return false
	}
	switch {
	case c.admin():
		return true
	case c.commune(), c.summary():
		return c.sees(code)
	}
	return false
}

// canDeactivate: chỉ cán bộ tổng hợp Cấp Tỉnh và Admin (BR-04).
func (c caller) canDeactivate(code string) bool {
	return !c.k.ReadOnly() && (c.admin() || (c.summary() && c.sees(code)))
}

// reader: Admin; Cấp Tỉnh, Cấp Xã phòng Chăn nuôi – Thủy sản (Cấp TW không có quyền — ma trận FN-10).
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
		return c, problem.Forbidden.WithDetail("Danh sách hộ chăn nuôi thuộc phòng Chăn nuôi – Thủy sản (Cấp Tỉnh, Cấp Xã)")
	}
	return c, nil
}

// writer: reader + kiểm ghi + khóa idem + phụ thuộc.
func (s *Service) writer(ctx context.Context, idem string) (caller, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return c, err
	}
	switch {
	case c.k.ReadOnly():
		return c, ErrReadOnly
	case c.k.Support != nil && utf8.RuneCountInString(strings.TrimSpace(c.k.Support.Reason)) < 10:
		return c, ErrSupportReason
	case strings.TrimSpace(idem) == "":
		return c, errIdem
	case s.Units == nil || s.Catalogs == nil || s.Vault == nil:
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

func idemKey(k string) string { return "lhh:" + k }

func dup(err error) bool { return errors.Is(ledger.Classify(err), ledger.ErrDuplicate) }

func fieldsProblem(base *problem.Problem, es domain.Errors) *problem.Problem {
	p := base
	for _, e := range es {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

// checkFields kiểm trường HCN-04…HCN-06: ấp không chứa SĐT/CCCD, xã còn hiệu lực trong phạm vi được ghi, vật nuôi thuộc
// danh mục FN-06 đang dùng hôm nay. Gọi NGOÀI giao dịch.
func (s *Service) checkFields(ctx context.Context, c caller, f *domain.Fields) error {
	if es := f.Normalize(); len(es) > 0 {
		return fieldsProblem(ErrInvalid, es)
	}
	if exdomain.ContainsPII(f.Hamlet) {
		return ErrInvalid.WithField(domain.FieldHamlet, exdomain.ErrPIIInText.Error())
	}
	u, err := s.Units.Get(ctx, f.CommuneCode, s.today())
	if errors.Is(err, partyport.ErrUnitNotFound) || (err == nil && u.Level != partyport.LevelCommune) {
		return ErrInvalid.WithField(domain.FieldCommune, "không phải xã/phường còn hiệu lực (FN-01)")
	}
	if err != nil {
		return err
	}
	if !c.canWrite(f.CommuneCode) {
		return ErrOutOfScope.WithField(domain.FieldCommune, "chỉ chọn xã phụ trách")
	}
	for i, a := range f.Animals {
		it, err := s.Catalogs.ItemAt(ctx, knport.CatalogCNTSSpecies, a.Species, s.today())
		if err != nil || !it.Active {
			if err == nil || errors.Is(err, mdport.ErrCatalogItemNotFound) {
				return ErrInvalid.WithField(animalField(i), "không có trong danh mục vật nuôi (FN-06)")
			}
			return err
		}
	}
	return nil
}

func exdomainPII(s string) bool { return exdomain.ContainsPII(s) }

func animalField(i int) string { return domain.FieldAnimals + "[" + strconv.Itoa(i) + "].species_code" }

// CheckinInput là check-in tại nhà hộ từ thiết bị.
type CheckinInput struct {
	Lat, Lon   float64
	AccuracyM  string // chuỗi thập phân thường, ≤ 2 chữ số lẻ
	Mocked     bool
	CapturedAt time.Time
}

// checkin kiểm và chuyển check-in; trả cảnh báo không chặn (sai số GPS > ngưỡng, vị trí giả lập). Gọi NGOÀI giao dịch.
func (s *Service) checkin(ctx context.Context, in CheckinInput) (domain.Checkin, []string, error) {
	acc, ok := domain.ParseAccuracy(in.AccuracyM)
	if !ok {
		return domain.Checkin{}, nil, ErrInvalid.WithField("checkin.accuracy_m", "độ chính xác GPS: số mét, tối đa 2 chữ số lẻ")
	}
	ck := domain.Checkin{Lat: in.Lat, Lon: in.Lon, AccuracyM: acc, Mocked: in.Mocked, CapturedAt: in.CapturedAt.UTC()}
	if es := domain.ValidCheckin(ck, s.now()); len(es) > 0 {
		return domain.Checkin{}, nil, fieldsProblem(ErrInvalid, es)
	}
	limit, err := s.accuracyLimit(ctx)
	if err != nil {
		return domain.Checkin{}, nil, err
	}
	var warn []string
	if acc.GreaterThan(limit) {
		warn = append(warn, WarnWeakGPS)
	}
	if in.Mocked {
		warn = append(warn, WarnMocked)
	}
	return ck, warn, nil
}

// accuracyLimit: tham số cnts.gps_accuracy_m đã duyệt; chưa có (hoặc chưa nối Params) → 50 m; lỗi tra cứu → 503.
func (s *Service) accuracyLimit(ctx context.Context) (decimal.Decimal, error) {
	def := decimal.NewFromInt(50)
	if s.Params == nil {
		return def, nil
	}
	v, err := s.Params.At(ctx, ParamAccuracy, s.now())
	if errors.Is(err, params.ErrParamNotFound) {
		return def, nil
	}
	if err != nil {
		return decimal.Decimal{}, ErrDependency.WithDetail("không đọc được tham số " + ParamAccuracy).WithCause(err)
	}
	return v.Value, nil
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
