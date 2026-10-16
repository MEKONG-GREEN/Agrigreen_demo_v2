package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"

	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port/mechanizationsrc"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var levelLabels = map[string]string{
	domain.LevelNational: "Toàn quốc", domain.LevelProvince: "Tỉnh", domain.LevelCommune: "Xã", domain.LevelCooperative: "HTX",
}

var areaSourceLabels = map[string]string{"app_htx": "App HTX", "manual": "Nhập tay"}

// names tra tên hiển thị (đơn vị hành chính, danh mục, HTX) có bộ nhớ đệm; lỗi
// tra cứu → hiện mã (không chặn báo cáo).
type names struct {
	s     *Service
	cache map[string]string
}

func (n *names) unit(ctx context.Context, code string) string {
	if code == "" {
		return ""
	}
	k := "u:" + code
	if v, ok := n.cache[k]; ok {
		return v
	}
	v := code
	if n.s.Units != nil {
		if u, err := n.s.Units.Get(ctx, code, n.s.now()); err == nil && u.Name != "" {
			v = u.Name
		}
	}
	n.cache[k] = v
	return v
}

func (n *names) catalog(ctx context.Context, catalog, code string) string {
	if code == "" {
		return ""
	}
	k := "c:" + catalog + ":" + code
	if v, ok := n.cache[k]; ok {
		return v
	}
	v := code
	if n.s.Catalogs != nil {
		if it, err := n.s.Catalogs.Get(ctx, catalog, code); err == nil && it.Name != "" {
			v = it.Name
		}
	}
	n.cache[k] = v
	return v
}

func (n *names) coop(ctx context.Context, ref uuid.UUID) string {
	k := "o:" + ref.String()
	if v, ok := n.cache[k]; ok {
		return v
	}
	v := ref.String()
	if n.s.Orgs != nil {
		if o, err := n.s.Orgs.Get(ctx, ref); err == nil && o.Name != "" {
			v = o.Name
		}
	}
	n.cache[k] = v
	return v
}

// built là tập dữ liệu kèm cờ tạm tính và ghi chú của nguồn.
type built struct {
	rows        []domain.Row
	total       domain.Row
	meta        []domain.Meta
	provisional bool
	notes       []string
}

// buildDataset dựng tập dữ liệu của việc xuất từ cổng đọc của module nguồn, với
// phạm vi và bộ lọc ĐÃ GIẢI lúc yêu cầu. Chạy ở worker (không có token).
func (s *Service) buildDataset(ctx context.Context, j domain.Job) (domain.Dataset, error) {
	ctx = tenant.WithID(ctx, j.TenantID)
	sc := authz.Scope{All: j.ScopeAll, Units: j.ScopeUnits}
	n := &names{s: s, cache: map[string]string{}}
	src, registered := s.sourceSpecs()[j.Def.Source]
	if !registered {
		return domain.Dataset{}, ErrDependency.WithDetail("nguồn báo cáo chưa được đăng ký")
	}
	meta := []domain.Meta{{Label: "Ngày xuất", Value: domain.FormatDateTimeVN(j.RequestedAt)}}
	var b built
	var err error
	if builder, ok := s.Builders.lookup(j.Def.Source); ok {
		b, err = s.buildRegisteredSource(ctx, j, sc, builder)
	} else {
		switch src.Module {
		case "mechanization":
			if s.Mechanization == nil {
				return domain.Dataset{}, ErrDependency.WithDetail("chưa nối cổng dữ liệu pf/mechanization")
			}
			b, err = s.buildMechanization(ctx, j, sc, n)
		case "cropcycle":
			if s.CropCycle == nil {
				return domain.Dataset{}, ErrDependency.WithDetail("chưa nối cổng dữ liệu pf/cropcycle")
			}
			b, err = s.buildHTX(ctx, j, sc, n)
			if err != nil {
				return domain.Dataset{}, err
			}
			ds := domain.BuildWithSources(j.Def, j.Params.Level, b.rows, b.total, s.sourceSpecs())
			ds.Meta = append(meta, b.meta...)
			ds.Provisional = b.provisional
			ds.Notes = append(b.notes, ds.Notes...)
			return ds, nil
		default:
			return domain.Dataset{}, ErrDependency.WithDetail("chưa nối bộ dựng nguồn báo cáo")
		}
	}
	if err != nil {
		return domain.Dataset{}, err
	}
	meta = append(meta, b.meta...)
	unit := "Toàn bộ phạm vi được phân quyền"
	if j.Params.UnitCode != "" {
		unit = n.unit(ctx, j.Params.UnitCode) + " (" + j.Params.UnitCode + ")"
	} else if sc.All {
		unit = "Toàn quốc"
	}
	meta = append(meta, domain.Meta{Label: "Đơn vị", Value: unit}, domain.Meta{Label: "Cấp tổng hợp", Value: levelLabels[j.Params.Level]})
	if j.Params.MachineType != "" {
		meta = append(meta, domain.Meta{Label: "Chủng loại", Value: n.catalog(ctx, mdport.CatalogMachineType, j.Params.MachineType)})
	}
	ds := domain.BuildWithSources(j.Def, j.Params.Level, b.rows, b.total, s.sourceSpecs())
	ds.Meta = meta
	ds.Provisional = b.provisional
	ds.Notes = append(b.notes, ds.Notes...)
	return ds, nil
}

func limit(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), "…")
}

func seasonLabel(name string, year int) string {
	if year == 0 {
		return name
	}
	return name + " " + strconv.Itoa(year)
}

// ---------------------------------------------------------------------------
// CGH FN-10: số liệu lấy NGUYÊN từ kết quả đã lưu qua cổng mechanization — ở đây
// chỉ ánh xạ sang cột, không cộng, không chia, không làm tròn (BR-04).
// ---------------------------------------------------------------------------

func (s *Service) mechQuery(ctx context.Context, j domain.Job, sc authz.Scope) (mechanizationsrc.ReportQuery, error) {
	q := mechanizationsrc.ReportQuery{TenantID: j.TenantID, Scope: sc, SeasonCode: j.Params.SeasonCode, SeasonYear: j.Params.SeasonYear,
		MachineType: j.Params.MachineType, Level: j.Params.Level}
	if q.Level == domain.LevelCooperative {
		q.Level = domain.LevelProvince
	}
	if j.Params.RunID != "" {
		id, err := uuid.Parse(j.Params.RunID)
		if err != nil {
			return q, err
		}
		q.RunID = id
	}
	if j.Params.UnitCode != "" {
		u, err := s.unitLevel(ctx, j.Params.UnitCode)
		if err != nil {
			return q, err
		}
		if u.Level == partyport.LevelProvince {
			q.ProvinceCode = u.Code
		} else {
			q.CommuneCode = u.Code
		}
	}
	return q, nil
}

func runMeta(label string, r mechanizationsrc.RunInfo) domain.Meta {
	return domain.Meta{Label: label, Value: r.SeasonCode + "-" + strconv.Itoa(r.SeasonYear) + " (kết quả cân đối lưu lúc " +
		domain.FormatDateTimeVN(r.ComputedAt) + ", quy tắc " + r.RuleVersion + ")"}
}

func runNotes(r mechanizationsrc.RunInfo) (bool, []string) {
	var notes []string
	if len(r.MissingNorms) > 0 {
		notes = append(notes, "Chưa cấu hình định mức cho: "+strings.Join(limit(r.MissingNorms, 10), ", "))
	}
	if !r.ThresholdsComplete {
		notes = append(notes, "Chưa cấu hình ngưỡng cảnh báo — mức đáp ứng không được phân loại")
	}
	return len(notes) > 0, notes
}

const savedNote = "Toàn bộ số liệu lấy từ kết quả cân đối đã lưu theo vụ (FN-09), không tính lại tại thời điểm xuất (FN-10 BR-04)."

func figs(prefix string, f *mechanizationsrc.Figures, row domain.Row) {
	if f == nil {
		return
	}
	row["required"+prefix] = domain.Dec(f.Required)
	row["capacity"+prefix] = domain.Int(int64(f.Capacity))
	row["coverage"+prefix] = domain.Dec(f.CoveragePct)
}

func (s *Service) buildMechanization(ctx context.Context, j domain.Job, sc authz.Scope, n *names) (built, error) {
	q, err := s.mechQuery(ctx, j, sc)
	if err != nil {
		return built{}, err
	}
	var b built
	stage := func(code string) string { return n.catalog(ctx, mdport.CatalogFarmStage, code) }
	mtype := func(code string) string { return n.catalog(ctx, mdport.CatalogMachineType, code) }
	switch j.Def.Source {
	case port.SourceCGHSummary:
		d, err := s.Mechanization.Coverage(ctx, q)
		if err != nil {
			return b, err
		}
		b.meta = append(b.meta, runMeta("Vụ", d.Run))
		b.provisional, b.notes = runNotes(d.Run)
		for _, r := range d.Rows {
			b.rows = append(b.rows, domain.Row{
				"province_name": domain.Txt(n.unit(ctx, r.ProvinceCode)), "commune_name": domain.Txt(n.unit(ctx, r.CommuneCode)),
				"farm_stage": domain.Txt(stage(r.FarmStage)), "machine_type": domain.Txt(mtype(r.MachineType)),
				"required": domain.Dec(r.Required), "capacity": domain.Int(int64(r.Capacity)), "coverage_pct": domain.Dec(r.CoveragePct),
				"status": domain.Txt(r.StatusLabel),
			})
		}
	case port.SourceCGHShortage:
		d, err := s.Mechanization.Shortage(ctx, q)
		if err != nil {
			return b, err
		}
		b.meta = append(b.meta, runMeta("Vụ", d.Run))
		b.provisional, b.notes = runNotes(d.Run)
		for _, r := range d.Rows {
			b.rows = append(b.rows, domain.Row{
				"cooperative_code": domain.Txt(r.CooperativeCode), "cooperative_name": domain.Txt(r.CooperativeName),
				"province_name": domain.Txt(n.unit(ctx, r.ProvinceCode)), "commune_name": domain.Txt(n.unit(ctx, r.CommuneCode)),
				"farm_stage": domain.Txt(stage(r.FarmStage)), "machine_type": domain.Txt(mtype(r.MachineType)),
				"capacity": domain.Int(int64(r.Capacity)), "required_whole": domain.IntP(r.RequiredWhole),
				"shortage": domain.Int(int64(r.Shortage)), "coverage_pct": domain.Dec(r.CoveragePct),
				"area_source": domain.Txt(areaSourceLabels[r.AreaSource]),
			})
		}
	case port.SourceCGHCompare:
		d, err := s.Mechanization.SeasonCompare(ctx, mechanizationsrc.CompareQuery{ReportQuery: q, SeasonCodeB: j.Params.SeasonCodeB, SeasonYearB: j.Params.SeasonYearB})
		if err != nil {
			return b, err
		}
		b.meta = append(b.meta, runMeta("Vụ A", d.RunA), runMeta("Vụ B", d.RunB))
		pa, na := runNotes(d.RunA)
		pb, nb := runNotes(d.RunB)
		b.provisional, b.notes = pa || pb, append(na, nb...)
		for _, r := range d.Rows {
			unit := n.unit(ctx, r.ProvinceCode)
			if r.CommuneCode != "" {
				unit = n.unit(ctx, r.CommuneCode)
			}
			row := domain.Row{"unit_name": domain.Txt(unit), "farm_stage": domain.Txt(stage(r.FarmStage)), "machine_type": domain.Txt(mtype(r.MachineType))}
			figs("_a", r.A, row)
			figs("_b", r.B, row)
			row["capacity_diff"] = domain.IntP(r.CapacityDiff)
			row["coverage_diff"] = domain.Dec(r.PctDiff)
			b.rows = append(b.rows, row)
		}
	}
	b.notes = append(b.notes, savedNote)
	return b, nil
}
