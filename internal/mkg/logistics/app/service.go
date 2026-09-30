// Package app: use case của mkg/logistics. Giao dịch mở ở đây bằng tenant.WithTenantTx; kiểm vai ở
// đây (R-55), kể cả chặn tự phê duyệt tham số (R-56). Định tuyến (gọi mạng) chạy NGOÀI giao dịch,
// kết quả đã dùng lưu nguyên văn vào route_snapshot trong giao dịch ghi chặng.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	costport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	fieldport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Vai.
const (
	RoleDispatcher      = port.RoleDispatcher
	RoleDriver          = port.RoleDriver
	RoleChiefAccountant = costport.RoleChiefAccountant // duyệt tham số cước (g4-conventions §1.2)
)

// Producer là tên nguồn phát trên phong bì sự kiện.
const Producer = "mkg/logistics"

// Mã lỗi của module (docs/errors.md, mục mkg/logistics).
var (
	ErrTripNotFound     = problem.New(404, "logistics.trip_not_found", "Không tìm thấy chuyến")
	ErrVesselNotFound   = problem.New(404, "logistics.vessel_not_found", "Không tìm thấy phương tiện trong danh mục")
	ErrParamNotFound    = problem.New(404, "logistics.param_not_found", "Không có tham số / phiên bản tham số này")
	ErrSnapshotNotFound = problem.New(404, "logistics.route_snapshot_not_found", "Chặng không có ảnh chụp định tuyến")
	ErrTransition       = problem.New(409, "logistics.trip_transition", "Chuyển trạng thái chuyến bị cấm — chỉ tiến planned → departed → arrived → completed, hủy từ planned/departed")
	ErrTripClosed       = problem.New(409, "logistics.trip_closed", "Chuyến đã hoàn tất hoặc đã hủy")
	ErrVesselCodeTaken  = problem.New(409, "logistics.vessel_code_taken", "Số hiệu phương tiện đã có trong danh mục (proto GH-01)")
	ErrVesselStale      = problem.New(409, "logistics.vessel_version_stale", "Danh mục phương tiện đã có phiên bản mới hơn — sửa theo phiên bản hiện hành")
	ErrParamDecided     = problem.New(409, "logistics.param_decided", "Phiên bản tham số đã được duyệt / từ chối")
	ErrManualReason     = problem.New(422, "logistics.manual_km_reason_required", "Km nhập tay phải có lý do (10–500 ký tự)")
	ErrCorrectionReason = problem.New(422, "logistics.correction_reason_required", "Sửa chuyến đã hoàn tất cần correction_reason (10–500 ký tự)")
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai logistics_app (cụm mkg)
	Clock clock.Clock
	// Waterway: định tuyến đường thủy qua bề mặt đối tác (ADR-0020); nil → adapter.Unconfigured (dự phòng).
	Waterway port.WaterwayRoutes
	// Road: định tuyến đường bộ; nil → Haversine × tham số logistics.route.road_haversine_factor (#10).
	Road port.RoadRoutes
}

var _ port.Trips = (*Service)(nil)

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var fe domain.FieldError
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &fe):
		return problem.Validation.WithField(fe.Field, fe.Msg)
	case errors.Is(err, domain.ErrTerminal):
		return ErrTripClosed
	case errors.Is(err, domain.ErrTransition):
		return ErrTransition.WithDetail(err.Error())
	case errors.Is(err, port.ErrTripNotFound):
		return ErrTripNotFound
	case errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "trip_forward_only":
		return ErrTransition.WithDetail(pg.Message)
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "vessel_current_tenant_id_code_key":
		return ErrVesselCodeTaken
	}
	return err
}

// write chạy fn trong giao dịch tenant; replay thử trước (gọi lặp) và sau ledger.ErrDuplicate.
func (s *Service) write(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error, replay func(pgx.Tx) (bool, error)) (replayed bool, err error) {
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ok, err := replay(tx)
		if err != nil || ok {
			replayed = ok
			return err
		}
		return fn(tx)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		rerr := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			ok, e := replay(tx)
			if e == nil && !ok {
				return err
			}
			return e
		})
		if rerr == nil {
			return true, nil
		}
	}
	return replayed, mapErr(err)
}

func (s *Service) read(ctx context.Context, fn func(pgx.Tx) error, roles ...string) error {
	if len(roles) == 0 {
		roles = []string{RoleDispatcher, RoleDriver}
	}
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return err
	}
	return mapErr(tenant.WithTenantTx(ctx, s.DB, p.TenantID, fn))
}

func requireIdem(key string) error {
	if key == "" {
		return problem.IdempotencyRequired
	}
	return nil
}

func insert(err error) error {
	if err != nil {
		return ledger.Classify(err)
	}
	return nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// Tham số theo thời điểm sự việc
// ---------------------------------------------------------------------------

// ParamValue là giá trị tham số đã ghim.
type ParamValue struct {
	Value       decimal.Decimal
	Version     string // "<key>:v<n>" | "<key>:default"
	Provisional bool
}

func day(t time.Time) time.Time {
	d := clock.BusinessDate(t)
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
}

// param đọc phiên bản đã duyệt hiệu lực vào ngày của at; không có → mặc định tạm (chờ khách).
func param(ctx context.Context, tx pgx.Tx, key string, at time.Time) (ParamValue, error) {
	spec, ok := domain.Spec(key)
	if !ok {
		return ParamValue{}, fmt.Errorf("logistics: khóa tham số lạ %s", key)
	}
	p, err := store.EffectiveParam(ctx, tx, key, day(at))
	if err != nil {
		return ParamValue{}, err
	}
	if p == nil {
		return ParamValue{Value: spec.Default, Version: key + ":default", Provisional: true}, nil
	}
	return ParamValue{Value: p.Value, Version: fmt.Sprintf("%s:v%d", key, p.VersionSeq), Provisional: p.Provisional}, nil
}

// rateKey trả khóa đơn giá theo phương thức.
func rateKey(m port.Mode) string {
	if m == port.ModeWaterway {
		return port.ParamWaterwayRatePerTonKm
	}
	return port.ParamRoadRatePerTonKm
}

// KgPerRollSource là nguồn kg/cuộn cho tấn ước FM-05 (fieldops/port, cùng tiến trình).
type KgPerRollSource = fieldport.KgPerRollEstimates
