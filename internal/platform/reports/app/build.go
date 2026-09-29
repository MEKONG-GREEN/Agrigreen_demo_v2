package app

import (
	"context"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	mechport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var levelLabels = map[string]string{
	domain.LevelNational: "Toàn quốc", domain.LevelProvince: "Tỉnh", domain.LevelCommune: "Xã", domain.LevelCooperative: "HTX",
}

var severityLabels = map[string]string{"nhe": "Nhẹ", "trung_binh": "Trung bình", "nang": "Nặng"}

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

// grouper cộng dồn số đo theo khóa nhóm, giữ thứ tự xuất hiện.
type grouper struct {
	order []string
	rows  map[string]domain.Row
	sums  map[string]map[string]decimal.Decimal
	total map[string]decimal.Decimal
}

func newGrouper() *grouper {
	return &grouper{rows: map[string]domain.Row{}, sums: map[string]map[string]decimal.Decimal{}, total: map[string]decimal.Decimal{}}
}

func (g *grouper) add(key string, base domain.Row, m map[string]decimal.Decimal) {
	if _, ok := g.rows[key]; !ok {
		g.order = append(g.order, key)
		g.rows[key] = base
		g.sums[key] = map[string]decimal.Decimal{}
	}
	for k, v := range m {
		g.sums[key][k] = g.sums[key][k].Add(v)
		g.total[k] = g.total[k].Add(v)
	}
}

func (g *grouper) result() []domain.Row {
	out := make([]domain.Row, 0, len(g.order))
	for _, k := range g.order {
		r := domain.Row{}
		for c, v := range g.rows[k] {
			r[c] = v
		}
		for c, v := range g.sums[k] {
			r[c] = domain.D(v)
		}
		out = append(out, r)
	}
	return out
}

func (g *grouper) totalRow() domain.Row {
	r := domain.Row{}
	for c, v := range g.total {
		r[c] = domain.D(v)
	}
	return r
}

// unitKey dựng khóa và cột đơn vị theo cấp tổng hợp.
func (n *names) unitKey(ctx context.Context, level, province, commune string) (string, domain.Row) {
	switch level {
	case domain.LevelNational:
		return "", domain.Row{}
	case domain.LevelProvince:
		return province, domain.Row{"province_code": domain.Txt(province), "province_name": domain.Txt(n.unit(ctx, province))}
	}
	return province + "|" + commune, domain.Row{
		"province_code": domain.Txt(province), "province_name": domain.Txt(n.unit(ctx, province)),
		"commune_code": domain.Txt(commune), "commune_name": domain.Txt(n.unit(ctx, commune)),
	}
}

func i64(n int64) decimal.Decimal { return decimal.NewFromInt(n) }

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
	src := domain.Sources[j.Def.Source]
	meta := []domain.Meta{{Label: "Ngày xuất", Value: domain.FormatDateTimeVN(j.RequestedAt)}}
	var b built
	var err error
	switch src.Module {
	case "extension":
		if s.Extension == nil {
			return domain.Dataset{}, ErrDependency.WithDetail("chưa nối cổng dữ liệu pf/extension")
		}
		b, err = s.buildExtension(ctx, j, sc, n)
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
		ds := domain.Build(j.Def, j.Params.Level, b.rows, b.total)
		ds.Meta = append(meta, b.meta...)
		ds.Provisional = b.provisional
		ds.Notes = append(b.notes, ds.Notes...)
		return ds, nil
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
	ds := domain.Build(j.Def, j.Params.Level, b.rows, b.total)
	ds.Meta = meta
	ds.Provisional = b.provisional
	ds.Notes = append(b.notes, ds.Notes...)
	return ds, nil
}

func (s *Service) extQuery(ctx context.Context, j domain.Job, sc authz.Scope) (extport.ReportQuery, string, error) {
	q := extport.ReportQuery{TenantID: j.TenantID, Scope: sc, Unit: j.Params.UnitCode, SeasonCode: j.Params.SeasonCode,
		SeasonYear: j.Params.SeasonYear, At: j.RequestedAt}
	label := ""
	if j.Params.Period != "" {
		per, err := domain.ParsePeriod(j.Params.Period)
		if err != nil {
			return q, "", err
		}
		q.Periods, label = per.Months, per.Label
	}
	return q, label, nil
}

func completenessNotes(c extport.Completeness) []string {
	var out []string
	if len(c.MissingCommunes) > 0 {
		out = append(out, "Còn "+strconv.Itoa(len(c.MissingCommunes))+"/"+strconv.Itoa(c.ExpectedCommunes)+" xã chưa gửi báo cáo của kỳ: "+
			strings.Join(limit(c.MissingCommunes, 20), ", "))
	}
	if len(c.OpenProvinces) > 0 {
		out = append(out, "Kỳ chưa chốt ở tỉnh: "+strings.Join(limit(c.OpenProvinces, 20), ", "))
	}
	if c.Unapproved > 0 {
		out = append(out, strconv.Itoa(c.Unapproved)+" dòng tiến độ Đề án chưa được Admin tỉnh duyệt")
	}
	return out
}

func limit(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), "…")
}

func (s *Service) buildExtension(ctx context.Context, j domain.Job, sc authz.Scope, n *names) (built, error) {
	q, label, err := s.extQuery(ctx, j, sc)
	if err != nil {
		return built{}, err
	}
	var b built
	if label != "" {
		b.meta = append(b.meta, domain.Meta{Label: "Kỳ báo cáo", Value: label})
	}
	lv := j.Params.Level
	g := newGrouper()
	var comp extport.Completeness
	switch j.Def.Source {
	case port.SourceSowing:
		d, err := s.Extension.Sowing(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			base["season_code"] = domain.Txt(n.catalog(ctx, mdport.CatalogSeason, r.SeasonCode))
			base["variety_code"] = domain.Txt(n.catalog(ctx, mdport.CatalogRiceVariety, r.VarietyCode))
			g.add(k+dims(j.Def, "season_code", r.SeasonCode, "variety_code", r.VarietyCode), base, map[string]decimal.Decimal{
				"sown_area_ha": r.SownAreaHa, "cooperatives": i64(r.Cooperatives), "reports": i64(r.Reports)})
		}
		comp = d.Completeness
	case port.SourceGrowthPest:
		d, err := s.Extension.GrowthPest(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			base["pest_code"] = domain.Txt(n.catalog(ctx, mdport.CatalogPest, r.PestCode))
			base["severity"] = domain.Txt(severityLabels[r.Severity])
			g.add(k+dims(j.Def, "pest_code", r.PestCode, "severity", r.Severity), base, map[string]decimal.Decimal{
				"affected_area_ha": r.AffectedAreaHa, "observations": i64(r.Observations), "urgent": i64(r.Urgent)})
		}
		comp = d.Completeness
	case port.SourceTasksSLA:
		d, err := s.Extension.TasksSLA(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			g.add(k, base, map[string]decimal.Decimal{"opened": i64(r.Opened), "resolved": i64(r.Resolved), "on_time": i64(r.OnTime),
				"escalated": i64(r.Escalated), "open": i64(r.Open), "overdue": i64(r.Overdue)})
		}
	case port.SourcePeriodSummary:
		d, err := s.Extension.PeriodSummary(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			g.add(k, base, map[string]decimal.Decimal{"sown_area_ha": r.SownAreaHa, "affected_area_ha": r.AffectedAreaHa,
				"pest_reports": i64(r.PestReports), "urgent_reports": i64(r.UrgentReports), "registered_ha": r.RegisteredHa,
				"completed_ha": r.CompletedHa, "tasks_opened": i64(r.TasksOpened), "tasks_resolved": i64(r.TasksResolved),
				"tasks_overdue": i64(r.TasksOverdue)})
		}
		comp = d.Completeness
	case port.SourceProgramProgress:
		return s.buildProgram(ctx, j, q, n)
	}
	b.rows, b.total = g.result(), g.totalRow()
	b.provisional = comp.Provisional
	b.notes = completenessNotes(comp)
	return b, nil
}

var hundred = decimal.NewFromInt(100)

// dims nối giá trị các chiều phân nhóm (cặp khóa cột, giá trị) — chỉ chiều có cột
// trong mẫu mới tách dòng; chiều không hiển thị được cộng dồn.
func dims(def domain.Definition, kv ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		for _, c := range def.Columns {
			if c.Key == kv[i] {
				b.WriteString("|" + kv[i+1])
				break
			}
		}
	}
	return b.String()
}

// buildProgram: FR-03 — tỷ lệ đăng ký/mục tiêu tính trên tổng của nhóm (tỉnh; toàn
// quốc = tổng mục tiêu các tỉnh, chỉ khi mọi tỉnh đã đặt mục tiêu).
func (s *Service) buildProgram(ctx context.Context, j domain.Job, q extport.ReportQuery, n *names) (built, error) {
	d, err := s.Extension.ProgramProgress(ctx, q)
	if err != nil {
		return built{}, err
	}
	var b built
	if q.SeasonCode != "" {
		b.meta = append(b.meta, domain.Meta{Label: "Vụ", Value: seasonLabel(n.catalog(ctx, mdport.CatalogSeason, q.SeasonCode), q.SeasonYear)})
	}
	lv := j.Params.Level
	g := newGrouper()
	coops := map[string]map[uuid.UUID]bool{}
	provincesOf := map[string]map[string]bool{}
	for _, r := range d.Rows {
		k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
		if lv == domain.LevelCooperative {
			k += "|" + r.CooperativeRef.String()
			base["cooperative_name"] = domain.Txt(n.coop(ctx, r.CooperativeRef))
		}
		season := seasonLabel(n.catalog(ctx, mdport.CatalogSeason, r.SeasonCode), r.SeasonYear)
		base["season"] = domain.Txt(season)
		k += dims(j.Def, "season", r.SeasonCode+"-"+strconv.Itoa(r.SeasonYear))
		unapproved := int64(0)
		if !r.Approved {
			unapproved = 1
		}
		g.add(k, base, map[string]decimal.Decimal{"registered_ha": r.RegisteredHa, "deploying_ha": r.DeployingHa,
			"completed_ha": r.CompletedHa, "unapproved": i64(unapproved)})
		if coops[k] == nil {
			coops[k], provincesOf[k] = map[uuid.UUID]bool{}, map[string]bool{}
		}
		coops[k][r.CooperativeRef] = true
		provincesOf[k][r.ProvinceCode] = true
	}
	allCoops := map[uuid.UUID]bool{}
	allProv := map[string]bool{}
	for _, k := range g.order {
		for c := range coops[k] {
			allCoops[c] = true
		}
		for p := range provincesOf[k] {
			allProv[p] = true
		}
	}
	targetOf := func(provs map[string]bool) *decimal.Decimal {
		sum := decimal.Zero
		for p := range provs {
			t := d.Targets[p]
			if t == nil {
				return nil
			}
			sum = sum.Add(*t)
		}
		if len(provs) == 0 {
			return nil
		}
		return &sum
	}
	rows := g.result()
	for i, k := range g.order {
		rows[i]["cooperatives"] = domain.Int(int64(len(coops[k])))
		if lv == domain.LevelNational || lv == domain.LevelProvince {
			t := targetOf(provincesOf[k])
			rows[i]["target_ha"] = domain.Dec(t)
			rows[i]["registered_pct"] = pctOf(rows[i]["registered_ha"], t)
		}
	}
	b.rows = rows
	b.total = g.totalRow()
	b.total["cooperatives"] = domain.Int(int64(len(allCoops)))
	if lv == domain.LevelNational || lv == domain.LevelProvince {
		t := targetOf(allProv)
		b.total["target_ha"] = domain.Dec(t)
		b.total["registered_pct"] = pctOf(b.total["registered_ha"], t)
	}
	b.provisional = d.Completeness.Provisional
	b.notes = completenessNotes(d.Completeness)
	return b, nil
}

func pctOf(v domain.Value, target *decimal.Decimal) domain.Value {
	if v.Dec == nil || target == nil || !target.IsPositive() {
		return domain.Value{}
	}
	p := v.Dec.Mul(hundred).DivRound(*target, 4)
	return domain.D(p)
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

func (s *Service) mechQuery(ctx context.Context, j domain.Job, sc authz.Scope) (mechport.ReportQuery, error) {
	q := mechport.ReportQuery{TenantID: j.TenantID, Scope: sc, SeasonCode: j.Params.SeasonCode, SeasonYear: j.Params.SeasonYear,
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
		if u.Level == mdport.LevelProvince {
			q.ProvinceCode = u.Code
		} else {
			q.CommuneCode = u.Code
		}
	}
	return q, nil
}

func runMeta(label string, r mechport.RunInfo) domain.Meta {
	return domain.Meta{Label: label, Value: r.SeasonCode + "-" + strconv.Itoa(r.SeasonYear) + " (kết quả cân đối lưu lúc " +
		domain.FormatDateTimeVN(r.ComputedAt) + ", quy tắc " + r.RuleVersion + ")"}
}

func runNotes(r mechport.RunInfo) (bool, []string) {
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

func figs(prefix string, f *mechport.Figures, row domain.Row) {
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
		d, err := s.Mechanization.SeasonCompare(ctx, mechport.CompareQuery{ReportQuery: q, SeasonCodeB: j.Params.SeasonCodeB, SeasonYearB: j.Params.SeasonYearB})
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
