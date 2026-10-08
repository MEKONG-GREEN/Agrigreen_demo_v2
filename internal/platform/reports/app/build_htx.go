package app

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/cropcyclesrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// HTX US-DASH-02, US-DASH-03: báo cáo sản lượng HTX — số liệu lấy qua cổng
// pf/cropcycle (cùng nguồn, cùng công thức với bảng điều hành US-DASH-01); ở đây chỉ
// ánh xạ sang cột và đổi kg → tấn (chia 1000, decimal chính xác).
// ---------------------------------------------------------------------------

// htxNote giải thích cách tính trên chân báo cáo.
const htxNote = "Dự kiến = diện tích × năng suất kỳ vọng của giống; thực tế = sản lượng đã khai; năng suất thực tế = sản lượng thực tế / diện tích đã khai sản lượng; tiến độ SOP chỉ tính nhật ký đã được duyệt. Vụ đã hủy không tính."

var kgPerTonne = decimal.NewFromInt(1000)

func (n *names) coopCode(ctx context.Context, ref uuid.UUID) string {
	k := "oc:" + ref.String()
	if v, ok := n.cache[k]; ok {
		return v
	}
	v := ""
	if n.s.Orgs != nil {
		if o, err := n.s.Orgs.Get(ctx, ref); err == nil {
			v = o.Code
		}
	}
	n.cache[k] = v
	return v
}

// htxUnits thu hẹp phạm vi đơn vị theo unit_code (tỉnh → mọi xã con) — giao với phạm vi đã giải.
func (s *Service) htxUnits(ctx context.Context, sc authz.Scope, unit string, j domain.Job) ([]string, error) {
	if s.Units == nil {
		return nil, ErrDependency.WithDetail("chưa nối danh mục đơn vị hành chính")
	}
	codes, err := s.Units.Descendants(ctx, unit, j.RequestedAt)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, c := range codes {
		if sc.Allows(c) {
			out = append(out, c)
		}
	}
	return out, nil
}

func htxFigures(f cropcyclesrc.ProductionFigures, row domain.Row) {
	row["seasons"] = domain.Int(f.Seasons)
	row["open_seasons"] = domain.Int(f.OpenSeasons)
	row["completed_seasons"] = domain.Int(f.CompletedSeasons)
	row["households"] = domain.Int(f.Households)
	row["area_ha"] = domain.D(f.AreaHa)
	row["harvested_area_ha"] = domain.D(f.HarvestedAreaHa)
	row["expected_t"] = domain.D(f.ExpectedKg.Decimal().Div(kgPerTonne))
	row["actual_t"] = domain.D(f.ActualKg.Decimal().Div(kgPerTonne))
	row["actual_yield_t_per_ha"] = domain.Dec(f.ActualYieldTPerHa)
	row["sop_progress_pct"] = domain.Dec(f.SOPProgressPct)
}

func (s *Service) buildHTX(ctx context.Context, j domain.Job, sc authz.Scope, n *names) (built, error) {
	def := j.Def
	q := cropcyclesrc.ProductionQuery{TenantID: j.TenantID, SeasonCode: j.Params.SeasonCode, SeasonYear: j.Params.SeasonYear,
		VarietyCode:   j.Params.VarietyCode,
		ByCooperative: def.HasColumn("cooperative_name") || def.HasColumn("cooperative_code"),
		BySeason:      def.HasColumn("season"), ByVariety: def.HasColumn("variety_name")}
	if j.Params.CooperativeRef != "" {
		id, err := uuid.Parse(j.Params.CooperativeRef)
		if err != nil {
			return built{}, problem.Validation.WithField("cooperative_ref", "phải là UUID")
		}
		q.CooperativeRef = id
	}
	var b built
	unit := "Toàn bộ phạm vi được phân quyền"
	switch {
	case j.Params.CoopScoped(j.ScopeAll, j.ScopeUnits):
		q.CoopOnly = true // quản lý HTX: chỉ HTX của mình (đã ghi lúc yêu cầu)
	case j.Params.UnitCode != "":
		units, err := s.htxUnits(ctx, sc, j.Params.UnitCode, j)
		if err != nil {
			return b, err
		}
		q.Scope = authz.Scope{Units: units}
		unit = n.unit(ctx, j.Params.UnitCode) + " (" + j.Params.UnitCode + ")"
	default:
		q.Scope = sc
		if sc.All {
			unit = "Toàn bộ"
		}
	}
	d, err := s.CropCycle.Production(ctx, q)
	if err != nil {
		return b, err
	}
	coop := "Tất cả HTX trong phạm vi"
	if q.CooperativeRef != uuid.Nil {
		coop = n.coop(ctx, q.CooperativeRef)
	}
	b.meta = append(b.meta, domain.Meta{Label: "HTX", Value: coop})
	if !q.CoopOnly {
		b.meta = append(b.meta, domain.Meta{Label: "Đơn vị", Value: unit})
	}
	season := "Tất cả"
	switch {
	case q.SeasonCode != "":
		season = seasonLabel(n.catalog(ctx, mdport.CatalogSeason, q.SeasonCode), q.SeasonYear)
	case q.SeasonYear != 0:
		season = "Mọi vụ năm " + strconv.Itoa(q.SeasonYear)
	}
	variety := "Tất cả"
	if q.VarietyCode != "" {
		variety = n.catalog(ctx, mdport.CatalogRiceVariety, q.VarietyCode)
	}
	b.meta = append(b.meta, domain.Meta{Label: "Vụ", Value: season}, domain.Meta{Label: "Giống lúa", Value: variety})
	for _, r := range d.Rows {
		row := domain.Row{}
		if q.ByCooperative {
			row["cooperative_code"] = domain.Txt(n.coopCode(ctx, r.CooperativeRef))
			row["cooperative_name"] = domain.Txt(n.coop(ctx, r.CooperativeRef))
		}
		if q.BySeason {
			row["season"] = domain.Txt(seasonLabel(n.catalog(ctx, mdport.CatalogSeason, r.SeasonCode), r.SeasonYear))
		}
		if q.ByVariety {
			row["variety_name"] = domain.Txt(n.catalog(ctx, mdport.CatalogRiceVariety, r.VarietyCode))
		}
		htxFigures(r.ProductionFigures, row)
		b.rows = append(b.rows, row)
	}
	b.total = domain.Row{}
	htxFigures(d.Totals, b.total)
	if d.Totals.OpenSeasons > 0 {
		// Còn lô chưa thu hoạch: sản lượng thực tế chưa đủ (US-DASH-02 AC-1 — nêu rõ, không ẩn).
		b.provisional = true
		b.notes = append(b.notes, "Còn "+strconv.FormatInt(d.Totals.OpenSeasons, 10)+" lô đang canh tác, chưa khai sản lượng — sản lượng thực tế chưa đủ.")
	}
	b.notes = append(b.notes, htxNote)
	return b, nil
}
