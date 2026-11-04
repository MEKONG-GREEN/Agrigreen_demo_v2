package app

import (
	"context"
	"errors"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"net/http"
	"time"
)

var ErrPlanRequired225 = problem.New(http.StatusConflict, "extension.khtc_plan_required", "Năm chưa có kế hoạch đã áp dụng tại ngày chốt")

// Presence is the successful typed PlanAt-backed result, never len(departments).
// The call is outside Calendar transactions and precedes every fresh native dispatch.
func (s *Service) publishedYearDepartments225(ctx context.Context, year int, cutoff time.Time) ([]string, error) {
	if s.Plan == nil {
		return nil, ErrDependency.WithDetail("Chưa nối kế hoạch năm đã áp dụng")
	}
	depts, err := s.Plan.PlanDepartments(ctx, year, cutoff)
	if errors.Is(err, ext.ErrKHTCPlanNotFound) {
		return nil, ErrPlanRequired225
	}
	if err != nil {
		return nil, ErrDependency.WithDetail("Không xác minh được kế hoạch năm đã áp dụng").WithCause(err)
	}
	return depts, nil
}
