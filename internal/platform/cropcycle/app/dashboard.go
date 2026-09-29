package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// DashboardInput là bộ lọc bảng điều hành (HTX US-DASH-01 AC-1: vụ, năm, HTX, giống).
type DashboardInput struct {
	SeasonCode     string
	SeasonYear     int
	CooperativeRef uuid.UUID
	VarietyCode    string
}

// DashGroup là một nhóm của bảng điều hành với chỉ số dẫn xuất.
type DashGroup struct {
	store.DashGroup
	ActualYieldTPerHa *decimal.Decimal // Σ thực tế / Σ diện tích đã thu hoạch
	SOPProgressPct    *decimal.Decimal // bước SOP có nhật ký ĐÃ DUYỆT / tổng bước (HTX US-LOG-05 AC-2)
}

// Dashboard là số liệu dự kiến – thực tế và thẻ chỉ số.
type Dashboard struct {
	Totals        DashGroup
	ByVariety     []DashGroup
	BySeason      []DashGroup
	ByCooperative []DashGroup
}

var hundred = decimal.NewFromInt(100)

func derive(g store.DashGroup) DashGroup {
	d := DashGroup{DashGroup: g}
	if g.HarvestedAreaHa.IsPositive() {
		y := domain.YieldTPerHaDec(g.ActualKg, g.HarvestedAreaHa)
		d.ActualYieldTPerHa = &y
	}
	if g.SOPSteps > 0 {
		p := decimal.NewFromInt(g.SOPStepsDone).Mul(hundred).Div(decimal.NewFromInt(g.SOPSteps)).Round(1)
		d.SOPProgressPct = &p
	}
	return d
}

// Dashboard tổng hợp theo phạm vi của người gọi: quản lý HTX chỉ HTX mình (AC-3),
// quản trị xem toàn bộ hoặc lọc theo HTX (AC-4), cán bộ KN theo xã.
func (s *Service) Dashboard(ctx context.Context, in DashboardInput) (Dashboard, error) {
	a, err := s.access(ctx, readRoles...)
	if err != nil {
		return Dashboard{}, err
	}
	if a.kind == accCoop && in.CooperativeRef != uuid.Nil && in.CooperativeRef != a.p.OrgRef {
		return Dashboard{}, ErrOutOfScope
	}
	if in.SeasonCode != "" && !domain.ValidSeasonCode(in.SeasonCode) {
		return Dashboard{}, problem.Validation.WithField("season_code", "mã vụ không hợp lệ")
	}
	f := store.DashFilter{Scope: a.filter(), CooperativeRef: in.CooperativeRef, SeasonCode: in.SeasonCode, SeasonYear: in.SeasonYear, VarietyCode: in.VarietyCode}
	var d Dashboard
	err = s.inTx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		tot, err := store.Dashboard(ctx, tx, f, "")
		if err != nil {
			return err
		}
		if len(tot) > 0 {
			d.Totals = derive(tot[0])
		} else {
			d.Totals = derive(store.DashGroup{})
		}
		for group, dst := range map[string]*[]DashGroup{"variety": &d.ByVariety, "season": &d.BySeason, "cooperative": &d.ByCooperative} {
			rows, err := store.Dashboard(ctx, tx, f, group)
			if err != nil {
				return err
			}
			*dst = make([]DashGroup, 0, len(rows))
			for _, r := range rows {
				*dst = append(*dst, derive(r))
			}
		}
		return nil
	})
	return d, err
}
