// Package app: use case của pf/waterway. Giao dịch mở ở đây; kiểm vai trước khi
// ghi. Mạng đường thủy là lớp dữ liệu nền DÙNG CHUNG của tenant (GIS FN-09,
// "cập nhật vào dữ liệu bản đồ dùng chung") nên không lọc theo cây TW → Tỉnh → Xã.
package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer là tên nhà phát sự kiện.
const Producer = "pf/waterway"

// Mã lỗi của module (docs/errors.md).
var (
	ErrSegmentNotFound       = problem.New(http.StatusNotFound, "waterway.segment_not_found", "Không tìm thấy đoạn sông/kênh")
	ErrStructureNotFound     = problem.New(http.StatusNotFound, "waterway.structure_not_found", "Không tìm thấy công trình")
	ErrRevisionConflict      = problem.New(http.StatusConflict, "waterway.revision_conflict", "Đã có bản sửa mới hơn — tải lại rồi sửa trên bản hiện hành")
	ErrSegmentRetired        = problem.New(http.StatusUnprocessableEntity, "waterway.segment_retired", "Đoạn đã ngừng dùng")
	ErrStructureOffSegment   = problem.New(http.StatusUnprocessableEntity, "waterway.structure_off_segment", "Công trình phải nằm trên tuyến")
	ErrBuildNotFound         = problem.New(http.StatusNotFound, "waterway.build_not_found", "Không tìm thấy yêu cầu dựng mạng")
	ErrVersionNotFound       = problem.New(http.StatusNotFound, "waterway.network_version_not_found", "Không có phiên bản mạng với số này")
	ErrVersionNotPublishable = problem.New(http.StatusConflict, "waterway.version_not_publishable", "Chỉ phát hành được phiên bản đang ở trạng thái built")
	ErrParamsInvalid         = problem.New(http.StatusUnprocessableEntity, "waterway.params_invalid", "Tham số dựng mạng không hợp lệ")
	ErrDependencyUnavailable = problem.New(http.StatusServiceUnavailable, "waterway.dependency_unavailable", "Chưa nối CSDL hoặc pf/masterdata")
)

// Service là use case của module.
type Service struct {
	DB tenant.Beginner // pool vai waterway_app; nil khi chỉ dùng làm handler của worker
	// Read là pool bản sao đọc cho truy vấn đồ thị (mục 16); nil → DB.
	Read     tenant.Beginner
	Clock    clock.Clock
	Params   mdport.Params
	Catalogs mdport.Catalogs
	// Sharing (G4-0): cổng cấp quyền chia sẻ của pf/party cho bề mặt đối tác
	// /v1/partner/waterway/route (loại waterway_routes, ADR-0020). Nil → 403 party.share_grant_missing.
	Sharing partyport.PartnerAccess
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return ErrDependencyUnavailable
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) readTx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	db := s.Read
	if db == nil {
		db = s.DB
	}
	if db == nil {
		return ErrDependencyUnavailable
	}
	return tenant.WithTenantTxOptions(ctx, db, tid, pgx.TxOptions{AccessMode: pgx.ReadOnly}, fn)
}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func validation(errs []problem.FieldError) error {
	if len(errs) == 0 {
		return nil
	}
	p := problem.Validation
	for _, e := range errs {
		p = p.WithField(e.Field, e.Reason)
	}
	return p
}

// ClampLimit giới hạn cỡ trang.
func ClampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

var adminReaders = []string{port.RoleEditor, port.RolePublisher, authz.RolePlatformAdmin}

// reader: Application API — mọi người dùng đã xác thực của tenant đọc được lớp
// đường thủy đã phát hành (dữ liệu nền không định danh).
func reader(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, problem.Unauthenticated
	}
	return p, nil
}

// ---------------------------------------------------------------------------
// Tham số (pf/masterdata; thiếu → mặc định trong mã)

func (s *Service) decParam(ctx context.Context, key string, def decimal.Decimal, at time.Time) (decimal.Decimal, error) {
	if s.Params == nil {
		return def, nil
	}
	v, err := s.Params.At(ctx, key, at)
	if errors.Is(err, mdport.ErrParamNotFound) {
		return def, nil
	}
	if err != nil {
		return def, ErrDependencyUnavailable.WithDetail("không đọc được tham số " + key).WithCause(err)
	}
	return v.Value, nil
}

// BuildParamsAt đọc ảnh chụp tham số dựng có hiệu lực tại at.
func (s *Service) BuildParamsAt(ctx context.Context, at time.Time) (domain.BuildParams, error) {
	p := domain.DefaultParams()
	var err error
	for _, x := range []struct {
		key string
		dst *decimal.Decimal
	}{
		{port.ParamSnapToleranceM, &p.Tolerances.SnapM},
		{port.ParamAutoJoinMaxM, &p.Tolerances.AutoJoinMaxM},
		{port.ParamReportGapMaxM, &p.Tolerances.ReportGapMaxM},
		{port.ParamOnSegmentTolM, &p.Tolerances.OnSegmentM},
		{port.ParamWidthFactor, &p.Safety.WidthFactor},
		{port.ParamUnderKeelM, &p.Safety.UnderKeelM},
		{port.ParamAirGapM, &p.Safety.AirGapM},
	} {
		if *x.dst, err = s.decParam(ctx, x.key, *x.dst, at); err != nil {
			return p, err
		}
	}
	if p.Vessels, err = s.vessels(ctx, at); err != nil {
		return p, err
	}
	domain.SortVessels(p.Vessels)
	if err := p.Validate(); err != nil {
		return p, ErrParamsInvalid.WithDetail(err.Error())
	}
	return p, nil
}

// vessels đọc lớp phương tiện: danh mục vessel_class (attrs tons, beam_m, draft_m,
// air_draft_m) nếu có; không có → ba lớp mặc định, mỗi kích thước có thể ghi đè
// bằng tham số waterway.vessel:<mã>:<thuộc tính>.
func (s *Service) vessels(ctx context.Context, at time.Time) ([]domain.VesselClass, error) {
	var items []mdport.CatalogItem
	if s.Catalogs != nil {
		var err error
		items, err = s.Catalogs.List(ctx, port.CatalogVesselClass)
		var pb *problem.Problem
		switch {
		case err == nil:
		case errors.Is(err, mdport.ErrCatalogItemNotFound), errors.As(err, &pb) && pb.Status == http.StatusNotFound:
			items = nil // danh mục chưa có (pf/masterdata 000027) → mặc định
		default:
			return nil, ErrDependencyUnavailable.WithDetail("không đọc được danh mục lớp phương tiện").WithCause(err)
		}
	}
	var out []domain.VesselClass
	for _, it := range items {
		if !it.Active {
			continue
		}
		v := domain.VesselClass{Code: it.Code}
		var errs []error
		for attr, dst := range map[string]*decimal.Decimal{"tons": &v.Tons, "beam_m": &v.BeamM, "draft_m": &v.DraftM, "air_draft_m": &v.AirDraftM} {
			d, err := it.Decimal(attr)
			errs = append(errs, err)
			*dst = d
		}
		if err := errors.Join(errs...); err != nil {
			return nil, ErrParamsInvalid.WithDetail("lớp phương tiện " + it.Code + " thiếu kích thước (tons, beam_m, draft_m, air_draft_m)")
		}
		out = append(out, v)
	}
	if len(out) > 0 {
		return out, nil
	}
	out = domain.DefaultVessels()
	for i := range out {
		v := &out[i]
		for attr, dst := range map[string]*decimal.Decimal{"tons": &v.Tons, "beam_m": &v.BeamM, "draft_m": &v.DraftM, "air_draft_m": &v.AirDraftM} {
			d, err := s.decParam(ctx, port.ParamVesselPrefix+":"+v.Code+":"+attr, *dst, at)
			if err != nil {
				return nil, err
			}
			*dst = d
		}
	}
	return out, nil
}
