// Package app: use case của pf/cropcycle — SOP, vụ mùa, nhật ký canh tác, duyệt,
// sản lượng, bảng điều hành và đồng bộ ngoại tuyến. Giao dịch mở ở đây
// (tenant.WithTenantTx); vai kiểm bằng authz.Require, phạm vi theo vai:
//   - platform_admin: toàn tenant, ghi được;
//   - coop_manager: HTX của mình (Principal.OrgRef), ghi hộ nông dân;
//   - farmer: thửa của các nông hộ liên kết với tài khoản (port.FarmerHouseholds);
//   - cán bộ KN (kn_officer, kn_province_admin, kn_national_admin): CHỈ ĐỌC theo
//     phạm vi xã của party ScopeResolver (ADR-0009).
package app

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của pf/cropcycle (docs/errors.md).
var (
	ErrSOPNotFound        = problem.New(http.StatusNotFound, "cropcycle.sop_not_found", "Không tìm thấy quy trình SOP")
	ErrSOPNameTaken       = problem.New(http.StatusConflict, "cropcycle.sop_name_taken", "Tên SOP này đã tồn tại, vui lòng chọn tên khác.")
	ErrSOPCodeTaken       = problem.New(http.StatusConflict, "cropcycle.sop_code_taken", "Mã SOP đã tồn tại")
	ErrSOPStepOrder       = problem.New(http.StatusUnprocessableEntity, "cropcycle.sop_step_order", domain.MsgStepOrder)
	ErrSOPNoHarvestStep   = problem.New(http.StatusUnprocessableEntity, "cropcycle.sop_harvest_step_missing", "Quy trình phải có bước thu hoạch để kế hoạch sản xuất có điểm kết thúc.")
	ErrSOPState           = problem.New(http.StatusConflict, "cropcycle.sop_state", "Trạng thái quy trình không cho phép thao tác này")
	ErrSOPNotPublished    = problem.New(http.StatusUnprocessableEntity, "cropcycle.sop_not_published", "Quy trình chưa được ban hành — không áp dụng vào vụ được")
	ErrNoSOPForVariety    = problem.New(http.StatusUnprocessableEntity, "cropcycle.no_sop_for_variety", "Chưa có quy trình SOP chuẩn đã ban hành cho giống lúa này")
	ErrSeasonNotFound     = problem.New(http.StatusNotFound, "cropcycle.season_not_found", "Không tìm thấy vụ")
	ErrPlotNotFound       = problem.New(http.StatusNotFound, "cropcycle.plot_not_found", "Không tìm thấy lô ruộng")
	ErrPlotInvalid        = problem.New(http.StatusUnprocessableEntity, "cropcycle.plot_invalid", "Lô ruộng chưa gán HTX hoặc đã ngừng dùng")
	ErrOutOfScope         = problem.New(http.StatusForbidden, "cropcycle.out_of_scope", "Nông hộ hoặc lô ruộng này không thuộc phạm vi quản lý của bạn.")
	ErrReadOnly           = problem.New(http.StatusForbidden, "cropcycle.read_only", "Vai của bạn chỉ được xem dữ liệu mùa vụ")
	ErrSowingWindow       = problem.New(http.StatusUnprocessableEntity, "cropcycle.sowing_date_out_of_window", domain.MsgSowingWindow)
	ErrSeasonAlreadyOpen  = problem.New(http.StatusConflict, "cropcycle.season_already_open", "Lô ruộng đang canh tác vụ khác, không thể mở vụ mới.")
	ErrPreviousNotDone    = problem.New(http.StatusConflict, "cropcycle.previous_season_not_completed", "Chỉ sao chép được cấu hình khi lô ruộng đã hoàn thành vụ trước")
	ErrNoPreviousSeason   = problem.New(http.StatusConflict, "cropcycle.no_previous_season", "Lô ruộng chưa có vụ nào đã hoàn thành để sao chép cấu hình")
	ErrCooperativeState   = problem.New(http.StatusUnprocessableEntity, "cropcycle.cooperative_inactive", "HTX không tồn tại hoặc đã ngừng hoạt động")
	ErrCatalogInvalid     = problem.New(http.StatusUnprocessableEntity, "cropcycle.catalog_invalid", "Mã danh mục không có hoặc đã ngừng dùng")
	ErrSeasonNotOpen      = problem.New(http.StatusConflict, "cropcycle.season_not_open", "Lô ruộng này chưa được mở vụ. Vui lòng mở vụ trước khi ghi nhật ký.")
	ErrSeasonState        = problem.New(http.StatusConflict, "cropcycle.season_state", "Trạng thái vụ không cho phép thao tác này")
	ErrConcurrentEdit     = problem.New(http.StatusConflict, "cropcycle.concurrent_edit", "Bản ghi đã được sửa bởi người khác — hãy tải phiên bản hiện hành rồi sửa lại")
	ErrLockedFields       = problem.New(http.StatusUnprocessableEntity, "cropcycle.locked_fields", "Tọa độ GPS và thời gian không thể chỉnh sửa sau khi lưu.")
	ErrLogNotFound        = problem.New(http.StatusNotFound, "cropcycle.log_not_found", "Không tìm thấy nhật ký")
	ErrLogState           = problem.New(http.StatusConflict, "cropcycle.log_state", "Nhật ký không ở trạng thái chờ duyệt")
	ErrSelfReview         = problem.New(http.StatusForbidden, "cropcycle.self_review", "Người ghi nhật ký không được tự duyệt")
	ErrBatchEmpty         = problem.New(http.StatusUnprocessableEntity, "cropcycle.batch_empty", "Không có lô ruộng hợp lệ trong lượt ghi hàng loạt")
	ErrHarvestTooEarly    = problem.New(http.StatusConflict, "cropcycle.harvest_too_early", "Lô ruộng chưa đến giai đoạn thu hoạch, chưa thể khai báo sản lượng.")
	ErrPHIViolation       = problem.New(http.StatusConflict, "cropcycle.phi_violation", "Chưa hết thời gian cách ly sau phun thuốc — chưa được thu hoạch")
	ErrYieldNegative      = problem.New(http.StatusUnprocessableEntity, "cropcycle.yield_negative", "Sản lượng không được âm")
	ErrYieldAnomaly       = problem.New(http.StatusConflict, "cropcycle.yield_anomaly", "Sản lượng bất thường — xác nhận lại trước khi lưu")
	ErrHarvestNotFound    = problem.New(http.StatusNotFound, "cropcycle.harvest_not_found", "Không tìm thấy khai báo sản lượng")
	ErrImportInvalid      = problem.New(http.StatusUnprocessableEntity, "cropcycle.import_invalid", "Tệp nhập không hợp lệ")
	ErrIDReused           = problem.New(http.StatusConflict, "cropcycle.id_reused", "id đã dùng cho bản ghi khác")
	ErrDependency         = problem.New(http.StatusServiceUnavailable, "cropcycle.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrBindingTemplateBad = problem.New(http.StatusUnprocessableEntity, "cropcycle.binding_invalid", "Chỉ gắn giống với mẫu SOP hệ thống đã ban hành, chưa ngừng dùng")
)

// Vai cán bộ khuyến nông (chỉ đọc theo phạm vi xã).
var knRoles = []string{partyport.RoleKNOfficer, partyport.RoleKNProvinceAdmin, partyport.RoleKNNationalAdmin}

// readRoles: vai đọc vụ, nhật ký, sản lượng, bảng điều hành.
var readRoles = append([]string{port.RoleFarmer, port.RoleCoopManager, authz.RolePlatformAdmin}, knRoles...)

// writeRoles: vai ghi vụ, nhật ký, sản lượng.
var writeRoles = []string{port.RoleFarmer, port.RoleCoopManager, authz.RolePlatformAdmin}

// managerRoles: vai quản lý (mở vụ hàng loạt, duyệt, nhập sản lượng).
var managerRoles = []string{port.RoleCoopManager, authz.RolePlatformAdmin}

// Service là use case của pf/cropcycle.
type Service struct {
	DB       tenant.Beginner // pool vai cropcycle_app
	Clock    clock.Clock
	Scope    partyport.ScopeResolver // phạm vi cán bộ KN; thiếu → vai KN nhận 503
	Orgs     partyport.Orgs          // tùy chọn: kiểm HTX đang hoạt động
	Plots    geoport.Plots           // bắt buộc cho ghi: thửa, diện tích, khoảng cách GPS
	Params   mdport.Params           // tùy chọn: ngưỡng GPS, ngưỡng năng suất bất thường
	Catalogs mdport.Catalogs         // tùy chọn: giống lúa, vụ, máy (thiếu → chỉ kiểm dạng mã)
	Farmers  port.FarmerHouseholds   // liên kết tài khoản ↔ nông hộ; thiếu → vai farmer nhận 503
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// today là ngày nghiệp vụ theo giờ Việt Nam (00:00 UTC của ngày đó).
func (s *Service) today() time.Time {
	d := clock.BusinessDate(s.now())
	return domain.Day(d.Year, d.Month, d.Day)
}

func businessDay(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return domain.Day(d.Year, d.Month, d.Day)
}

// fromCtx mở giao dịch với tenant lấy từ context (cổng cho module khác).
func (s *Service) fromCtx(ctx context.Context, fn func(pgx.Tx) error) error {
	return tenant.FromCtxTx(ctx, s.DB, fn)
}

func (s *Service) inTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// ---------------------------------------------------------------------------
// Quyền và phạm vi
// ---------------------------------------------------------------------------

type accessKind int

const (
	accAll accessKind = iota + 1
	accCoop
	accFarmer
	accKN
)

type access struct {
	p          authz.Principal
	kind       accessKind
	sc         authz.Scope
	households []uuid.UUID
}

// access tính quyền của người gọi có một trong roles.
func (s *Service) access(ctx context.Context, roles ...string) (access, error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return access{}, err
	}
	return s.accessFor(ctx, p, roles)
}

func (s *Service) accessFor(ctx context.Context, p authz.Principal, roles []string) (access, error) {
	has := func(r string) bool { return p.Has(r) && slices.Contains(roles, r) }
	switch {
	case has(authz.RolePlatformAdmin):
		return access{p: p, kind: accAll, sc: authz.Scope{All: true}}, nil
	case has(port.RoleCoopManager):
		return access{p: p, kind: accCoop}, nil
	case has(port.RoleFarmer):
		if s.Farmers == nil {
			return access{}, ErrDependency.WithDetail("chưa nối liên kết tài khoản ↔ nông hộ (pf/accounts)")
		}
		hh, err := s.Farmers.HouseholdsOf(ctx, p.ActorRef)
		if err != nil {
			return access{}, ErrDependency.WithDetail("không tra được nông hộ của tài khoản").WithCause(err)
		}
		return access{p: p, kind: accFarmer, households: hh}, nil
	}
	for _, r := range knRoles {
		if has(r) {
			if s.Scope == nil {
				return access{}, ErrDependency.WithDetail("chưa nối bộ tính phạm vi (pf/party)")
			}
			sc, err := s.Scope.Scope(ctx)
			if err != nil {
				return access{}, err
			}
			return access{p: p, kind: accKN, sc: sc}, nil
		}
	}
	return access{}, problem.Forbidden
}

// filter là điều kiện đọc cho store.
func (a access) filter() store.Filter {
	switch a.kind {
	case accAll:
		return store.Filter{Mode: store.FilterAll}
	case accCoop:
		if a.p.OrgRef == uuid.Nil {
			return store.Filter{Mode: store.FilterNone}
		}
		return store.Filter{Mode: store.FilterCoop, Coop: a.p.OrgRef}
	case accFarmer:
		return store.Filter{Mode: store.FilterHouseholds, Households: a.households}
	case accKN:
		if a.sc.All {
			return store.Filter{Mode: store.FilterAll}
		}
		return store.Filter{Mode: store.FilterUnits, Units: a.sc.Units}
	}
	return store.Filter{Mode: store.FilterNone}
}

// sees cho biết người gọi đọc được bản ghi thuộc xã/HTX/nông hộ này.
func (a access) sees(commune string, coop, hh uuid.UUID) bool {
	switch a.kind {
	case accAll:
		return true
	case accCoop:
		return a.p.OrgRef != uuid.Nil && coop == a.p.OrgRef
	case accFarmer:
		return hh != uuid.Nil && slices.Contains(a.households, hh)
	case accKN:
		return a.sc.Allows(commune)
	}
	return false
}

// canWrite kiểm quyền ghi vào bản ghi của HTX coop, nông hộ hh.
func (a access) canWrite(coop, hh uuid.UUID) error {
	switch a.kind {
	case accAll:
		return nil
	case accCoop:
		if a.p.OrgRef != uuid.Nil && coop == a.p.OrgRef {
			return nil
		}
	case accFarmer:
		if hh != uuid.Nil && slices.Contains(a.households, hh) {
			return nil
		}
	case accKN:
		return ErrReadOnly
	}
	return ErrOutOfScope
}

// onBehalf: người ghi không phải nông dân của hộ (quản lý HTX / quản trị ghi hộ).
func (a access) onBehalf() bool { return a.kind != accFarmer }

// visibleCoops trả HTX mà mẫu SOP riêng được thấy (nil + true = mọi mẫu).
func (a access) sopVisibility() (all bool, coop uuid.UUID) {
	if a.kind == accAll || a.kind == accKN {
		return true, uuid.Nil
	}
	return false, a.p.OrgRef
}

// ---------------------------------------------------------------------------
// Phụ thuộc
// ---------------------------------------------------------------------------

// plot đọc thửa qua pf/geo và kiểm thửa đã gán HTX, chưa ngừng dùng.
func (s *Service) plot(ctx context.Context, ref uuid.UUID) (geoport.Plot, error) {
	if s.Plots == nil {
		return geoport.Plot{}, ErrDependency.WithDetail("chưa nối pf/geo (thửa)")
	}
	pl, err := s.Plots.Get(ctx, ref)
	if errors.Is(err, geoport.ErrPlotNotFound) {
		return pl, ErrPlotNotFound
	}
	if err != nil {
		return pl, ErrDependency.WithDetail("không tra được thửa").WithCause(err)
	}
	if pl.Status != geoport.StatusActive || pl.CooperativeRef == uuid.Nil {
		return pl, ErrPlotInvalid
	}
	if !pl.AreaHa.IsPositive() {
		return pl, ErrPlotInvalid.WithDetail("thửa chưa có diện tích")
	}
	return pl, nil
}

// checkCoop kiểm HTX tồn tại, đúng loại, đang hoạt động (khi đã nối pf/party).
func (s *Service) checkCoop(ctx context.Context, coop uuid.UUID) error {
	if s.Orgs == nil {
		return nil
	}
	o, err := s.Orgs.Get(ctx, coop)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return ErrCooperativeState
	}
	if err != nil {
		return ErrDependency.WithDetail("không tra được HTX").WithCause(err)
	}
	if o.Kind != partyport.OrgCooperative || !o.Active {
		return ErrCooperativeState
	}
	return nil
}

// catalogItem tra mục danh mục đang dùng; Catalogs nil → chỉ kiểm dạng mã.
func (s *Service) catalogItem(ctx context.Context, catalog, field, code string) (*mdport.CatalogItem, error) {
	if !domain.ValidCatalogCode(code) {
		return nil, problem.Validation.WithField(field, "mã không hợp lệ")
	}
	if s.Catalogs == nil {
		return nil, nil
	}
	it, err := s.Catalogs.Get(ctx, catalog, code)
	if errors.Is(err, mdport.ErrCatalogItemNotFound) || (err == nil && !it.Active) {
		return nil, ErrCatalogInvalid.WithField(field, "không có mã "+code+" đang dùng trong danh mục "+catalog)
	}
	if err != nil {
		return nil, ErrDependency.WithDetail("không tra được danh mục").WithCause(err)
	}
	return &it, nil
}

// expectedYield đọc năng suất dự kiến (t/ha) của giống từ danh mục.
func expectedYield(it *mdport.CatalogItem) *decimal.Decimal {
	if it == nil || it.Attrs["expected_yield_t_per_ha"] == "" {
		return nil
	}
	d, err := it.Decimal("expected_yield_t_per_ha")
	if err != nil || d.IsNegative() {
		return nil
	}
	return &d
}

// checkMachines kiểm máy móc cơ giới trong danh mục machine_type (HTX US-LOG-01 AC-2).
func (s *Service) checkMachines(ctx context.Context, codes []string) ([]string, error) {
	if len(codes) > domain.MaxMachines {
		return nil, problem.Validation.WithField("machine_codes", "tối đa 20 máy")
	}
	out := make([]string, 0, len(codes))
	for _, c := range codes {
		if slices.Contains(out, c) {
			continue
		}
		if _, err := s.catalogItem(ctx, mdport.CatalogMachineType, "machine_codes", c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// param đọc tham số số học tại thời điểm at; không có → def.
func (s *Service) param(ctx context.Context, key string, at time.Time, def decimal.Decimal) (decimal.Decimal, bool, error) {
	if s.Params == nil {
		return def, false, nil
	}
	v, err := s.Params.At(ctx, key, at)
	if errors.Is(err, mdport.ErrParamNotFound) {
		return def, false, nil
	}
	if err != nil {
		return def, false, ErrDependency.WithDetail("không đọc được tham số " + key).WithCause(err)
	}
	return v.Value, true, nil
}

// locate gắn nhãn vị trí (HTX US-GPS-01/02). Thiếu GPS, không đo được hay lỗi
// phụ thuộc KHÔNG bao giờ chặn lưu — nhãn "missing".
func (s *Service) locate(ctx context.Context, plot uuid.UUID, g domain.GPS, at time.Time) (string, *decimal.Decimal) {
	if !g.HasPoint() || s.Plots == nil {
		return domain.LocMissing, nil
	}
	tol, _, err := s.param(ctx, port.ParamGPSMatchToleranceM, at, domain.DefaultGPSToleranceM)
	if err != nil || !tol.IsPositive() {
		tol = domain.DefaultGPSToleranceM
	}
	d, err := s.Plots.DistanceM(ctx, plot, *g.Lon, *g.Lat)
	if err != nil {
		// HTX US-GPS-01 AC-4: không đo được (thửa chưa có hình, pf/geo lỗi) thì không chặn lưu.
		return domain.LocMissing, nil
	}
	return domain.LocationStatus(&d, tol), &d
}

// anomalyThreshold: ngưỡng năng suất bất thường theo giống (HTX US-YIELD-03 AC-2):
// tham số theo giống → thuộc tính giống yield_anomaly_max_t_per_ha → tham số chung → 12.
func (s *Service) anomalyThreshold(ctx context.Context, variety string, at time.Time) (decimal.Decimal, error) {
	if v, ok, err := s.param(ctx, port.ParamYieldAnomalyMaxTPerHa+":"+variety, at, decimal.Zero); err != nil {
		return decimal.Zero, err
	} else if ok && v.IsPositive() {
		return v, nil
	}
	if s.Catalogs != nil {
		if it, err := s.Catalogs.Get(ctx, mdport.CatalogRiceVariety, variety); err == nil && it.Attrs["yield_anomaly_max_t_per_ha"] != "" {
			if d, err := it.Decimal("yield_anomaly_max_t_per_ha"); err == nil && d.IsPositive() {
				return d, nil
			}
		}
	}
	v, _, err := s.param(ctx, port.ParamYieldAnomalyMaxTPerHa, at, domain.DefaultYieldAnomalyMax)
	if err != nil {
		return decimal.Zero, err
	}
	if !v.IsPositive() {
		return domain.DefaultYieldAnomalyMax, nil
	}
	return v, nil
}

// eventTime chuẩn hóa thời điểm sự việc: rỗng → hiện tại; không vượt hiện tại + 5 phút.
func (s *Service) eventTime(t time.Time) (time.Time, error) {
	now := s.now()
	if t.IsZero() {
		return now, nil
	}
	if t.After(now.Add(5 * time.Minute)) {
		return time.Time{}, problem.Validation.WithField("event_time", "không được ở tương lai")
	}
	return t.UTC(), nil
}

// newID kiểm id máy sinh (UUIDv7) hoặc sinh mới.
func newID(field string, id uuid.UUID) (uuid.UUID, error) {
	if id == uuid.Nil {
		return ids.New(), nil
	}
	if id.Version() != 7 {
		return uuid.Nil, problem.Validation.WithField(field, "phải là UUIDv7")
	}
	return id, nil
}

// fieldProblem quy lỗi domain về problem.
func fieldProblem(err error) error {
	var so domain.StepOrderError
	if errors.As(err, &so) {
		return ErrSOPStepOrder.WithField("steps", domain.MsgStepOrder)
	}
	if fe, ok := domain.AsFieldError(err); ok {
		return problem.Validation.WithField(fe.Field, fe.Reason)
	}
	return err
}

// isUnique cho biết err là vi phạm ràng buộc duy nhất constraint.
func isUnique(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && (constraint == "" || pg.ConstraintName == constraint)
}

func uuidPtr(u uuid.UUID) *uuid.UUID {
	if u == uuid.Nil {
		return nil
	}
	return &u
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func enqueue(ctx context.Context, tx pgx.Tx, tid uuid.UUID, typ string, at time.Time, payload any) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: tid, Type: typ, SchemaVersion: 1, OccurredAt: at,
		TraceID: telemetry.TraceID(ctx), Producer: "pf/cropcycle", Payload: payload,
	})
}

// refreshArea tính lại diện tích vụ của HTX và phát coop_season_area.changed khi đổi.
func refreshArea(ctx context.Context, tx pgx.Tx, tid, coop uuid.UUID, season string, year int, at time.Time) error {
	a, changed, err := store.RefreshCoopArea(ctx, tx, tid, coop, season, year)
	if err != nil || !changed {
		return err
	}
	return enqueue(ctx, tx, tid, port.EventCoopSeasonAreaChanged, at, port.CoopSeasonAreaChangedV1{
		CooperativeRef: coop, SeasonCode: season, SeasonYear: year, AreaHa: a.AreaHa.StringFixed(4), Seasons: a.Seasons,
		OpenSeasons: a.OpenSeasons, CompletedSeasons: a.CompletedSeasons, SourceAt: at.UTC().Format(time.RFC3339),
	})
}

// Page là một trang kết quả có cursor.
type Page[T any] struct {
	Items []T
	Next  *uuid.UUID
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}
