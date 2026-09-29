// Package app: use case của pf/simulation. Giao dịch mở bằng tenant.WithTenantTx;
// vai kiểm ở đây (R-55). Không tính mô phỏng — engine ở optimizer/ (Python).
package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/simulation/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Service là use case của module.
type Service struct {
	DB     tenant.Beginner
	Clock  clock.Clock
	Params port.ParamSet     // nil → mọi tham số lấy giá trị danh mục, không "Chính thức" được
	Supply port.SupplySource // chỉ worker cần; nil → việc mô phỏng hỏng với simulation.dependency_unavailable
	Router port.Router       // nil → Haversine × hệ số #10 (FN-04 BR-03)
	Log    *slog.Logger
}

// Mã lỗi của module (docs/errors.md, mục pf/simulation).
var (
	ErrHubNotFound          = problem.New(http.StatusNotFound, "simulation.hub_not_found", "Không tìm thấy Hub ứng viên")
	ErrHubRetired           = problem.New(http.StatusConflict, "simulation.hub_retired", "Hub ứng viên đã ngừng dùng")
	ErrScenarioNotFound     = problem.New(http.StatusNotFound, "simulation.scenario_not_found", "Không tìm thấy kịch bản")
	ErrHubNotAttached       = problem.New(http.StatusNotFound, "simulation.hub_not_attached", "Hub chưa được gán vào kịch bản")
	ErrScenarioWithoutHubs  = problem.New(http.StatusUnprocessableEntity, "simulation.scenario_without_hubs", "Kịch bản chưa có Hub nào — hãy gán ít nhất một Hub ứng viên")
	ErrBaselineScenario     = problem.New(http.StatusUnprocessableEntity, "simulation.baseline_scenario_invalid", "Kịch bản baseline không hợp lệ")
	ErrSimulationNotFound   = problem.New(http.StatusNotFound, "simulation.simulation_not_found", "Không tìm thấy việc mô phỏng")
	ErrResultNotReady       = problem.New(http.StatusConflict, "simulation.result_not_ready", "Mô phỏng chưa có kết quả")
	ErrParamsUnapproved     = problem.New(http.StatusConflict, "simulation.params_unapproved", "Còn tham số chưa phê duyệt — không thể đánh dấu Chính thức")
	ErrNotAccepting         = problem.New(http.StatusConflict, "simulation.not_accepting_result", "Việc mô phỏng không ở trạng thái chờ kết quả")
	ErrSnapshotMismatch     = problem.New(http.StatusConflict, "simulation.snapshot_mismatch", "Kết quả không tính trên ảnh chụp đã ghim")
	ErrResultInvalid        = problem.New(http.StatusUnprocessableEntity, "simulation.result_invalid", "Phong bì kết quả không đúng hợp đồng contracts/optimizer")
	ErrSnapshotIntegrity    = problem.New(http.StatusInternalServerError, "simulation.snapshot_integrity", "Ảnh chụp không khớp băm đã lưu")
	ErrDependency           = problem.New(http.StatusServiceUnavailable, "simulation.dependency_unavailable", "Thiếu nguồn dữ liệu để dựng ảnh chụp")
	ErrSupplyInvalid        = problem.New(http.StatusUnprocessableEntity, "simulation.supply_invalid", "Nguồn cung trả dữ liệu không hợp lệ")
	ErrNoSupply             = problem.New(http.StatusUnprocessableEntity, "simulation.no_supply", "Không có HTX nào có toạ độ trong bán kính phục vụ của các Hub")
	ErrInternalUnauthorized = problem.New(http.StatusForbidden, "simulation.internal_forbidden", "Internal API chỉ nhận dịch vụ được phép qua mTLS")
)

var analysts = []string{port.RoleSimulationAnalyst}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505"
}

func require(ctx context.Context) (authz.Principal, error) { return authz.Require(ctx, analysts...) }

// resolveParams tra 57 tham số (và các chiều của #10, #26) tại at.
func (s *Service) resolveParams(ctx context.Context, tid uuid.UUID, at time.Time) (domain.SnapParams, error) {
	lookup := map[string]domain.ParamLookup{}
	if s.Params != nil {
		for _, def := range domain.Catalog {
			if def.ManagedElsewhere {
				continue
			}
			keys := []string{domain.ParamKey(def.Code, "")}
			for _, d := range def.Dims {
				keys = append(keys, domain.ParamKey(def.Code, d.Key))
			}
			for _, k := range keys {
				p, found, err := s.Params.Approved(ctx, tid, k, at)
				if err != nil {
					return domain.SnapParams{}, err
				}
				if found {
					lookup[k] = domain.ParamLookup{Found: true, Value: p.Value, Ver: p.Version}
				}
			}
		}
	}
	return domain.ResolveParams(lookup), nil
}

func paramOr(p domain.SnapParams, code string, def int64) decimal.Decimal {
	if v, ok := p.ParamValue(code); ok {
		return v
	}
	return decimal.NewFromInt(def)
}
