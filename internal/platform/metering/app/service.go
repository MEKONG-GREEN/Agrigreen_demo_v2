// Package app: use case của pf/metering — ghi sổ sử dụng + bộ đếm hạn mức (Ledger, Quotas, Usage),
// điểm kiểm soát xuất (Checkpoint), bảng kê theo kỳ (mẫu §12, R-56) và dựng lại bảng chiếu (R-22).
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/params"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi thêm của module (docs/errors.md, mục pf/metering). Mã đóng băng ở port/.
var (
	ErrNotFound              = problem.New(http.StatusNotFound, "metering.not_found", "Không tìm thấy bảng kê / yêu cầu lập bảng kê")
	ErrDependencyUnavailable = problem.New(http.StatusServiceUnavailable, "metering.dependency_unavailable", "Chưa nối cơ sở dữ liệu hoặc phụ thuộc của pf/metering")
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai metering_app
	Clock clock.Clock
	// Plans: gói hạn mức có phiên bản của pf/apiclient (pool apiclient_app). nil → mặc định "tạm, chờ khách".
	Plans apiclientport.Plans
	// Params: tham số đã duyệt của pf/masterdata (hạn mức/ngưỡng kênh DLP, giá). nil → mặc định.
	Params params.Params
	// Mode: AG_EGRESS_MODE của Checkpoint.Pass (Public API qua Ledger.Record luôn enforce).
	Mode    meter.Mode
	Metrics *Metrics
	Log     *slog.Logger
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	if s.DB == nil {
		return ErrDependencyUnavailable
	}
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}

func (s *Service) metrics() *Metrics {
	if s.Metrics == nil {
		s.Metrics = NewMetrics(nil)
	}
	return s.Metrics
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

func (s *Service) mode() meter.Mode {
	if s.Mode == "" {
		return meter.ModeObserve
	}
	return s.Mode
}

// param đọc tham số đã duyệt của tenant tid tại at; chưa có → ok = false (dùng mặc định).
func (s *Service) param(ctx context.Context, tid uuid.UUID, key string, at time.Time) (params.ParamValue, bool, error) {
	if s.Params == nil {
		return params.ParamValue{}, false, nil
	}
	v, err := s.Params.At(tenant.WithID(ctx, tid), key, at)
	if errors.Is(err, params.ErrParamNotFound) {
		return params.ParamValue{}, false, nil
	}
	if err != nil {
		return params.ParamValue{}, false, fmt.Errorf("metering: đọc tham số %s: %w", key, err)
	}
	return v, true, nil
}

// EgressParams trả tham số DLP hiệu lực của kênh ch ở tenant chủ dữ liệu tại at: hạn mức ngày, hệ số
// và ngưỡng tuyệt đối bất thường. Tham số có phiên bản, setter ≠ approver ở pf/masterdata; chưa duyệt
// → mặc định g5-conventions §1.2 ("tạm, chờ khách").
func (s *Service) EgressParams(ctx context.Context, dataTenant uuid.UUID, ch meter.Channel, at time.Time) (domain.EgressParams, error) {
	p := domain.DefaultEgressParams(ch)
	for _, it := range []struct {
		format string
		apply  func(decimal.Decimal) error
	}{
		{port.ParamEgressDailyQuotaFmt, func(d decimal.Decimal) error {
			if d.IsNegative() || !d.IsInteger() {
				return errors.New("hạn mức phải là số nguyên ≥ 0")
			}
			p.DailyQuota = d.IntPart()
			return nil
		}},
		{port.ParamEgressAnomalyFactorFmt, func(d decimal.Decimal) error {
			if !d.IsPositive() {
				return errors.New("hệ số phải > 0")
			}
			p.AnomalyFactor = d
			return nil
		}},
		{port.ParamEgressAnomalyRecordsFmt, func(d decimal.Decimal) error {
			if d.IsNegative() || !d.IsInteger() {
				return errors.New("ngưỡng phải là số nguyên ≥ 0")
			}
			p.AnomalyRecords = d.IntPart()
			return nil
		}},
	} {
		key := port.EgressParam(it.format, ch)
		v, ok, err := s.param(ctx, dataTenant, key, at)
		if err != nil {
			return p, err
		}
		if !ok {
			p.Source[key] = "default"
			continue
		}
		if err := it.apply(v.Value); err != nil {
			return p, fmt.Errorf("metering: tham số %s v%d không hợp lệ: %w", key, v.Version, err)
		}
		p.Source[key] = fmt.Sprintf("v%d", v.Version)
	}
	return p, nil
}

// plan đọc gói hạn mức hiệu lực (mặc định khi chưa nối pf/apiclient).
func (s *Service) plan(ctx context.Context, op, client uuid.UUID, at time.Time) (apiclientport.Plan, error) {
	if s.Plans == nil {
		return apiclientport.Plan{ClientID: client, RequestsPerMinute: apiclientport.DefaultRequestsPerMinute,
			DailyUnits: apiclientport.DefaultDailyUnits, MonthlyUnits: apiclientport.DefaultMonthlyUnits,
			MaxExportRows: apiclientport.DefaultMaxExportRows}, nil
	}
	return s.Plans.PlanFor(ctx, op, client, at)
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// Page là một trang kết quả phân trang cursor (R-53).
type Page[T any] struct {
	Items []T
	Next  *uuid.UUID
}

func page[T any](items []T, limit int, id func(T) uuid.UUID) Page[T] {
	var pg Page[T]
	if len(items) > limit {
		items = items[:limit]
		last := id(items[limit-1])
		pg.Next = &last
	}
	pg.Items = items
	return pg
}
