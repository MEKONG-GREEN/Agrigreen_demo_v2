// Package app là use case của pf/mechanization: kiểm vai và phạm vi, mở giao
// dịch bằng tenant.WithTenantTx, gọi domain và store.
package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// Mã lỗi của module (docs/errors.md).
var (
	ErrOwnerNotFound    = problem.New(http.StatusNotFound, "mechanization.owner_not_found", "Không tìm thấy chủ sở hữu")
	ErrMachineNotFound  = problem.New(http.StatusNotFound, "mechanization.machine_not_found", "Không tìm thấy máy")
	ErrAreaNotFound     = problem.New(http.StatusNotFound, "mechanization.area_not_found", "Không tìm thấy bản ghi diện tích vụ")
	ErrRunNotFound      = problem.New(http.StatusNotFound, "mechanization.run_not_found", "Không tìm thấy kết quả cân đối đã lưu")
	ErrSerialTaken      = problem.New(http.StatusConflict, "mechanization.serial_taken", "Số máy/SN đã tồn tại trên hệ thống")
	ErrChassisTaken     = problem.New(http.StatusConflict, "mechanization.chassis_taken", "Số khung đã tồn tại trên hệ thống")
	ErrOwnerDuplicate   = problem.New(http.StatusConflict, "mechanization.owner_duplicate", "Chủ sở hữu trùng định danh đã tồn tại")
	ErrOwnerHasMachines = problem.New(http.StatusConflict, "mechanization.owner_has_machines", "Chủ sở hữu còn máy đang hoạt động")
	ErrState            = problem.New(http.StatusConflict, "mechanization.state_conflict", "Trạng thái không cho phép thao tác này")
	ErrCoopInvalid      = problem.New(http.StatusUnprocessableEntity, "mechanization.cooperative_invalid", "HTX không tồn tại hoặc đã ngừng hoạt động")
	ErrOutOfScope       = problem.New(http.StatusForbidden, "mechanization.out_of_scope", "Ngoài phạm vi phụ trách")
	ErrDependency       = problem.New(http.StatusServiceUnavailable, "mechanization.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng")
	ErrImportInvalid    = problem.New(http.StatusUnprocessableEntity, "mechanization.import_invalid", "Tệp không đúng mẫu quy định, vui lòng tải mẫu chuẩn và thử lại")
)

// Vai được đọc (FN-03..FN-11: Cục KTHT xem, Admin xem).
var readRoles = []string{port.RoleCGHViewer, port.RoleCGHAdmin, authz.RolePlatformAdmin}

// Vai được ghi dữ liệu chủ và dữ liệu vụ mùa (Admin — ma trận phân quyền mục VI).
var writeRoles = []string{port.RoleCGHAdmin}

// Deps là port của module khác.
type Deps struct {
	Scope      partyport.ScopeResolver // phạm vi TW → Tỉnh → Xã (ADR-0009)
	Orgs       partyport.Orgs          // HTX: xã, trạng thái
	Params     params.Params           // định mức, ngưỡng theo thời điểm (mục 16)
	Catalogs   mdport.Catalogs         // machine_type, farm_stage (cờ thí điểm), season
	AdminUnits partyport.AdminUnits    // tỉnh của xã, tên tỉnh cho mã hiển thị
	Vault      client.Client           // định danh chủ máy cá nhân (R-04)
}

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai mechanization_app
	Clock clock.Clock
	Deps
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// today là ngày nghiệp vụ hiện tại (giờ Việt Nam), dạng 00:00 UTC.
func (s *Service) today() time.Time { return dayOf(s.now()) }

// dayOf trả ngày nghiệp vụ của t dạng 00:00 UTC.
func dayOf(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// dayStart là thời điểm bắt đầu (giờ Việt Nam) của ngày d (00:00 UTC mang ngày).
func dayStart(d time.Time) time.Time {
	return clock.StartOfBusinessDate(clock.Date{Year: d.Year(), Month: d.Month(), Day: d.Day()})
}

func (s *Service) ready() error {
	if s.Scope == nil || s.Orgs == nil || s.Catalogs == nil || s.AdminUnits == nil {
		return ErrDependency.WithDetail("chưa nối pf/party hoặc pf/masterdata")
	}
	return nil
}

// caller kiểm vai và tính phạm vi của người gọi.
func (s *Service) caller(ctx context.Context, roles []string) (authz.Principal, authz.Scope, error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return p, authz.Scope{}, err
	}
	if err := s.ready(); err != nil {
		return p, authz.Scope{}, err
	}
	sc, err := s.Scope.Scope(ctx)
	return p, sc, err
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

// fieldProblem ánh xạ domain.FieldError sang 422 theo trường.
func fieldProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Err.Error())
	}
	return err
}

// coopInfo là HTX kèm địa bàn tại một thời điểm.
type coopInfo struct {
	Ref           uuid.UUID
	Name          string
	Active        bool
	CommuneCode   string
	ProvinceCode  string
	ProvinceLabel string // "tên viết tắt tỉnh" cho mã hiển thị
}

// coop tra HTX qua pf/party và tỉnh qua pf/masterdata tại thời điểm at.
func (s *Service) coop(ctx context.Context, ref uuid.UUID, at time.Time) (*coopInfo, error) {
	o, err := s.Orgs.Get(ctx, ref)
	if errors.Is(err, partyport.ErrOrgNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrDependency.WithDetail("không tra được HTX").WithCause(err)
	}
	if o.Kind != partyport.OrgCooperative {
		return nil, nil
	}
	ci := &coopInfo{Ref: o.Ref, Name: o.Name, Active: o.Active, CommuneCode: o.AdminUnitCode, ProvinceLabel: domain.NoProvince}
	if ci.CommuneCode == "" {
		return ci, nil
	}
	u, err := s.unitAt(ctx, ci.CommuneCode, at)
	if err != nil || u == nil {
		return ci, err
	}
	prov := *u
	if u.Level != partyport.LevelProvince && u.ParentCode != "" {
		pu, err := s.unitAt(ctx, u.ParentCode, at)
		if err != nil {
			return ci, err
		}
		if pu == nil {
			ci.ProvinceCode = u.ParentCode
			return ci, nil
		}
		prov = *pu
	}
	ci.ProvinceCode, ci.ProvinceLabel = prov.Code, domain.ProvinceLabel(prov.Name)
	return ci, nil
}

// unitAt tra đơn vị tại at; không có thì thử thời điểm hiện tại (đơn vị mới lập sau vụ).
func (s *Service) unitAt(ctx context.Context, code string, at time.Time) (*partyport.AdminUnit, error) {
	for _, t := range []time.Time{at, s.now()} {
		u, err := s.AdminUnits.Get(ctx, code, t)
		if err == nil {
			return &u, nil
		}
		if !errors.Is(err, partyport.ErrUnitNotFound) {
			return nil, ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
		}
	}
	return nil, nil
}

// activeCoop tra HTX đang hoạt động, trong phạm vi ghi của người gọi.
func (s *Service) activeCoop(ctx context.Context, sc authz.Scope, ref uuid.UUID, field string) (*coopInfo, error) {
	if ref == uuid.Nil {
		return nil, problem.Validation.WithField(field, "Vui lòng chọn HTX")
	}
	ci, err := s.coop(ctx, ref, s.now())
	if err != nil {
		return nil, err
	}
	if ci == nil || !ci.Active {
		return nil, ErrCoopInvalid.WithField(field, "HTX không tồn tại hoặc đã ngừng hoạt động")
	}
	if !visible(sc, ci) {
		return nil, ErrOutOfScope
	}
	return ci, nil
}

// visible cho biết HTX thuộc phạm vi của người gọi.
func visible(sc authz.Scope, ci *coopInfo) bool {
	if sc.All {
		return true
	}
	return ci != nil && ((ci.CommuneCode != "" && sc.Allows(ci.CommuneCode)) || (ci.ProvinceCode != "" && sc.Allows(ci.ProvinceCode)))
}

// coopVisible kiểm quyền đọc dữ liệu của một HTX (ngoài phạm vi → 404, không lộ sự tồn tại).
func (s *Service) coopVisible(ctx context.Context, sc authz.Scope, ref uuid.UUID) error {
	if sc.All {
		return nil
	}
	ci, err := s.coop(ctx, ref, s.now())
	if err != nil {
		return err
	}
	if !visible(sc, ci) {
		return ErrOutOfScope
	}
	return nil
}

// uniqueViolation trả tên ràng buộc nếu err là lỗi trùng khóa duy nhất.
func uniqueViolation(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return pg.ConstraintName
	}
	return ""
}

// classify quy lỗi DB về lỗi nghiệp vụ.
func classify(err error) error {
	switch uniqueViolation(err) {
	case store.ConstraintSerial:
		return ErrSerialTaken
	case store.ConstraintChassis:
		return ErrChassisTaken
	case store.ConstraintPerson, store.ConstraintCoop:
		return ErrOwnerDuplicate
	}
	return ledger.Classify(err)
}

// catalogItem tra mục danh mục; không có → nil.
func (s *Service) catalogItem(ctx context.Context, catalog, code string) (*mdport.CatalogItem, error) {
	it, err := s.Catalogs.Get(ctx, catalog, code)
	if errors.Is(err, mdport.ErrCatalogItemNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrDependency.WithDetail("không tra được danh mục").WithCause(err)
	}
	return &it, nil
}

// paramAt tra tham số đã duyệt tại at; chưa cấu hình → nil.
func (s *Service) paramAt(ctx context.Context, key string, at time.Time) (*domain.ParamRef, error) {
	if s.Params == nil {
		return nil, ErrDependency.WithDetail("chưa nối tham số pf/masterdata")
	}
	v, err := s.Params.At(ctx, key, at)
	if errors.Is(err, params.ErrParamNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, ErrDependency.WithDetail("không tra được tham số").WithCause(err)
	}
	return &domain.ParamRef{Key: key, Value: v.Value, Version: v.Version, SourceDoc: v.SourceDoc}, nil
}

// requireIdem kiểm Idempotency-Key (R-51).
func requireIdem(key string) error {
	if key == "" {
		return problem.IdempotencyRequired
	}
	return nil
}
