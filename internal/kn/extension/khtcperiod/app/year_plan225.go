package app

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/jackc/pgx/v5"
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

// PlanYearReferences is the public owned-calendar seam. It never reads Forms SQL.
func (s *Service) PlanYearReferences(ctx context.Context, year int) (ext.KHTCPlanYearReferences, error) {
	var out ext.KHTCPlanYearReferences
	p, k, err := ext.RequireSession(ctx)
	if err != nil {
		return out, err
	}
	if (k.Role == ext.SessionRoleAdmin || k.Support != nil) && !p.MFA {
		return out, problem.MFARequired
	}
	if k.Role != ext.SessionRoleAdmin && (k.Role != ext.SessionRoleProvince || k.Department != "ke_hoach_tai_chinh") {
		return out, problem.Forbidden
	}
	if year < 2000 || year > 2100 {
		return out, problem.Validation.WithField("year", "năm 2000–2100")
	}
	if s.DB == nil {
		return out, ErrDependency
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.PlanYearReferences225(ctx, tx, p.TenantID, year)
		return e
	})
	if err != nil {
		return out, ErrDependency.WithDetail("Không xác minh được tham chiếu năm kế hoạch").WithCause(err)
	}
	return out, nil
}
