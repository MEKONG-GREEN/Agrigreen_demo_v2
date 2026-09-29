package app

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Mã lỗi G2 của pf/geo (docs/errors.md).
var (
	ErrCoopBoundaryNotFound = problem.New(http.StatusNotFound, "geo.cooperative_boundary_not_found", "HTX chưa có ranh giới hoặc nằm ngoài phạm vi")
	ErrCoopBoundaryExists   = problem.New(http.StatusConflict, "geo.cooperative_boundary_exists", "HTX đã có ranh giới — gửi phiên bản mới (kèm supersedes_id và lý do) để sửa")
	ErrFacilityNotFound     = problem.New(http.StatusNotFound, "geo.facility_not_found", "Không tìm thấy kho/Hub")
	ErrFacilityCodeExists   = problem.New(http.StatusConflict, "geo.facility_code_exists", "Mã kho/Hub đã được dùng")
	ErrHubNotFound          = problem.New(http.StatusNotFound, "geo.hub_not_found", "Chưa nhận hub MKG này qua van công bố")
	ErrRoadNotFound         = problem.New(http.StatusNotFound, "geo.road_not_found", "Không tìm thấy tuyến đường bộ")
	ErrFeatureState         = problem.New(http.StatusConflict, "geo.feature_state_conflict", "Trạng thái đối tượng không cho phép thao tác này")
	ErrNothingToRelease     = problem.New(http.StatusConflict, "geo.capacity_nothing_to_release", "Không có số sức chứa nhập tay đang áp dụng để nhả")
	ErrGpsLogReadOnly       = problem.New(http.StatusMethodNotAllowed, "geo.gps_log_read_only", domain.MsgGpsReadOnly)
)

// Vai ghi ranh giới HTX (Application API): cán bộ KN trong phạm vi, quản lý HTX
// cho chính HTX mình, quản trị GIS.
var coopWriteRoles = []string{port.RolePlotEditor, port.RoleCoopManager, port.RoleGeoAdmin, authz.RolePlatformAdmin}

// Vai đọc lớp nền G2 (bản đồ GIS, App HTX).
var layerReadRoles = []string{port.RolePlotViewer, port.RolePlotEditor, port.RoleCoopManager, port.RoleGeoAdmin, authz.RolePlatformAdmin}

// validBBox kiểm khung nhìn WGS84.
func validBBox(b store.BBox) error {
	if !(b.MinLon < b.MaxLon && b.MinLat < b.MaxLat) || b.MinLon < -180 || b.MaxLon > 180 || b.MinLat < -90 || b.MaxLat > 90 {
		return problem.Validation.WithField("bbox", "phải là minLon,minLat,maxLon,maxLat theo WGS84, min < max")
	}
	return nil
}

// viewportLimit chuẩn hóa limit của khung nhìn (GIS BR-25: tối đa 1.000 đối tượng).
func viewportLimit(n int) (int, error) {
	if n == 0 {
		return MaxViewportPlots, nil
	}
	if n < 1 || n > MaxViewportPlots {
		return 0, problem.Validation.WithField("limit", "từ 1 đến 1000")
	}
	return n, nil
}

// reason chuẩn hóa lý do sửa/ngừng dùng (≥ 10 ký tự, RB-1).
func reason(field, s string) (string, error) {
	if !domain.ValidReason(s) {
		return "", problem.Validation.WithField(field, "bắt buộc, ít nhất 10 ký tự")
	}
	return strings.TrimSpace(s), nil
}

// writeOnce chạy một lần ghi có chống lặp theo idem_key: đã ghi → đọc lại
// (replayed = true); hai request cùng khóa chạy song song: bên thua đọc lại.
func writeOnce[T any](ctx context.Context, s *Service, tid uuid.UUID, replay func(pgx.Tx) (*T, error), fn func(pgx.Tx) (T, error)) (T, bool, error) {
	var res T
	replayed := false
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		prev, err := replay(tx)
		if err != nil {
			return err
		}
		if prev != nil {
			res, replayed = *prev, true
			return nil
		}
		res, err = fn(tx)
		return err
	})
	err = ledger.Classify(err)
	if errors.Is(err, ledger.ErrDuplicate) {
		var prev *T
		if rerr := s.inTx(ctx, tid, func(tx pgx.Tx) error {
			var e error
			prev, e = replay(tx)
			return e
		}); rerr == nil && prev != nil {
			return *prev, true, nil
		}
	}
	return res, replayed, err
}

// unitAt kiểm mã đơn vị hành chính còn hiệu lực, đúng cấp (level rỗng = mọi cấp).
func (s *Service) unitAt(ctx context.Context, field, code string, level mdport.Level) error {
	if code == "" {
		return problem.Validation.WithField(field, "bắt buộc")
	}
	if s.Units == nil {
		return ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	u, err := s.Units.Get(ctx, code, s.now())
	if errors.Is(err, mdport.ErrUnitNotFound) {
		return problem.Validation.WithField(field, "không có đơn vị hành chính còn hiệu lực với mã này")
	}
	if err != nil {
		return ErrDependency.WithDetail("không tra được đơn vị hành chính").WithCause(err)
	}
	if level != "" && u.Level != level {
		return problem.Validation.WithField(field, "phải là mã cấp "+string(level))
	}
	return nil
}

// paramOr đọc tham số số học có hiệu lực tại at; chưa có phiên bản duyệt → def.
func (s *Service) paramOr(ctx context.Context, key string, at time.Time, def decimal.Decimal) (decimal.Decimal, error) {
	if s.Params == nil {
		return def, nil
	}
	v, err := s.Params.At(ctx, key, at)
	if errors.Is(err, mdport.ErrParamNotFound) {
		return def, nil
	}
	if err != nil {
		return decimal.Zero, ErrDependency.WithDetail("không đọc được tham số " + key).WithCause(err)
	}
	return v.Value, nil
}

// hubThresholds đọc ngưỡng % của widget Hub (BR-50 → FN-03). Cặp không hợp lệ
// (không tăng dần trong 0–100) → mặc định 60/85.
func (s *Service) hubThresholds(ctx context.Context, at time.Time) (warn, full decimal.Decimal, err error) {
	if warn, err = s.paramOr(ctx, port.ParamHubCapacityWarnPct, at, domain.DefaultHubWarnPct); err != nil {
		return
	}
	if full, err = s.paramOr(ctx, port.ParamHubCapacityFullPct, at, domain.DefaultHubFullPct); err != nil {
		return
	}
	hundred := decimal.NewFromInt(100)
	if warn.IsNegative() || !warn.LessThan(full) || full.GreaterThan(hundred) {
		warn, full = domain.DefaultHubWarnPct, domain.DefaultHubFullPct
	}
	return
}

// plotMinZoom đọc mức zoom tối thiểu của lớp thửa (GIS BR-24); mặc định 13.
func (s *Service) plotMinZoom(ctx context.Context) (int, error) {
	v, err := s.paramOr(ctx, port.ParamPlotLayerMinZoom, s.now(), decimal.NewFromInt(DefaultPlotLayerMinZoom))
	if err != nil {
		return 0, err
	}
	z := int(v.IntPart())
	if z < 0 || z > 22 {
		z = DefaultPlotLayerMinZoom
	}
	return z, nil
}

// DefaultPlotLayerMinZoom: cận cảnh 1:10.000 → 1:500 (nguyên mẫu ZOOM_BANDS can_canh).
const DefaultPlotLayerMinZoom = 13

// positive kiểm số dương (sức chứa, tải trọng).
func positive(d *decimal.Decimal) bool { return d != nil && d.IsPositive() }
