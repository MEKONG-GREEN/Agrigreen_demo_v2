package extension

import (
	"context"
	knreportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	reportsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type ReportBuilderDeps struct {
	Data     knreportsport.ReportData
	Orgs     partyport.Orgs
	Units    partyport.AdminUnits
	Catalogs mdport.Catalogs
	Clock    clock.Clock
}
type reportSourceBuilder struct{ deps ReportBuilderDeps }

func ReportBuilders(deps ReportBuilderDeps) []reportsport.SourceBuilderRegistration {
	b := &reportSourceBuilder{deps: deps}
	out := []reportsport.SourceBuilderRegistration{}
	for _, source := range []string{knreportsport.SourceSowing, knreportsport.SourceGrowthPest, knreportsport.SourceProgramProgress, knreportsport.SourceTasksSLA, knreportsport.SourcePeriodSummary} {
		out = append(out, reportsport.SourceBuilderRegistration{Source: source, Build: b.build})
	}
	return out
}

var reportSourceSeverityLabels = map[string]string{"nhe": "Nhẹ", "trung_binh": "Trung bình", "nang": "Nặng"}

func reportSourceText(s string) reportsport.SourceCell { return reportsport.SourceCell{Text: s} }
func reportSourceInt(n int64) reportsport.SourceCell {
	d := decimal.NewFromInt(n)
	return reportsport.SourceCell{Dec: &d}
}
func reportSourceDecimal(d decimal.Decimal) reportsport.SourceCell {
	return reportsport.SourceCell{Dec: &d}
}
func reportSourceDecimalPointer(d *decimal.Decimal) reportsport.SourceCell {
	if d == nil {
		return reportsport.SourceCell{}
	}
	n := *d
	return reportsport.SourceCell{Dec: &n}
}
func (n *reportSourceNames) now() time.Time {
	if n.d.Clock != nil {
		return n.d.Clock.Now().UTC()
	}
	return n.at
}

type reportSourceNames struct {
	d     ReportBuilderDeps
	at    time.Time
	cache map[string]string
}

func (n *reportSourceNames) unit(ctx context.Context, code string) string {
	if code == "" {
		return ""
	}
	k := "u:" + code
	if v, ok := n.cache[k]; ok {
		return v
	}
	v := code
	if n.d.Units != nil {
		if u, err := n.d.Units.Get(ctx, code, n.now()); err == nil && u.Name != "" {
			v = u.Name
		}
	}
	n.cache[k] = v
	return v
}

func (n *reportSourceNames) catalog(ctx context.Context, catalog, code string) string {
	if code == "" {
		return ""
	}
	k := "c:" + catalog + ":" + code
	if v, ok := n.cache[k]; ok {
		return v
	}
	v := code
	if n.d.Catalogs != nil {
		if it, err := n.d.Catalogs.Get(ctx, catalog, code); err == nil && it.Name != "" {
			v = it.Name
		}
	}
	n.cache[k] = v
	return v
}

func (n *reportSourceNames) coop(ctx context.Context, ref uuid.UUID) string {
	k := "o:" + ref.String()
	if v, ok := n.cache[k]; ok {
		return v
	}
	v := ref.String()
	if n.d.Orgs != nil {
		if o, err := n.d.Orgs.Get(ctx, ref); err == nil && o.Name != "" {
			v = o.Name
		}
	}
	n.cache[k] = v
	return v
}

// reportSourceGrouper cộng dồn số đo theo khóa nhóm, giữ thứ tự xuất hiện.
type reportSourceGrouper struct {
	order []string
	rows  map[string]reportsport.SourceRow
	sums  map[string]map[string]decimal.Decimal
	total map[string]decimal.Decimal
}

func newReportSourceGrouper() *reportSourceGrouper {
	return &reportSourceGrouper{rows: map[string]reportsport.SourceRow{}, sums: map[string]map[string]decimal.Decimal{}, total: map[string]decimal.Decimal{}}
}

func (g *reportSourceGrouper) add(key string, base reportsport.SourceRow, m map[string]decimal.Decimal) {
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

func (g *reportSourceGrouper) result() []reportsport.SourceRow {
	out := make([]reportsport.SourceRow, 0, len(g.order))
	for _, k := range g.order {
		r := reportsport.SourceRow{}
		for c, v := range g.rows[k] {
			r[c] = v
		}
		for c, v := range g.sums[k] {
			r[c] = reportSourceDecimal(v)
		}
		out = append(out, r)
	}
	return out
}

func (g *reportSourceGrouper) totalRow() reportsport.SourceRow {
	r := reportsport.SourceRow{}
	for c, v := range g.total {
		r[c] = reportSourceDecimal(v)
	}
	return r
}

// unitKey dựng khóa và cột đơn vị theo cấp tổng hợp.
func (n *reportSourceNames) unitKey(ctx context.Context, level, province, commune string) (string, reportsport.SourceRow) {
	switch level {
	case "national":
		return "", reportsport.SourceRow{}
	case "province":
		return province, reportsport.SourceRow{"province_code": reportSourceText(province), "province_name": reportSourceText(n.unit(ctx, province))}
	}
	return province + "|" + commune, reportsport.SourceRow{
		"province_code": reportSourceText(province), "province_name": reportSourceText(n.unit(ctx, province)),
		"commune_code": reportSourceText(commune), "commune_name": reportSourceText(n.unit(ctx, commune)),
	}
}

func reportSourceInt64(n int64) decimal.Decimal { return decimal.NewFromInt(n) }

func reportSourceCompletenessNotes(c knreportsport.Completeness) []string {
	var out []string
	if len(c.MissingCommunes) > 0 {
		out = append(out, "Còn "+strconv.Itoa(len(c.MissingCommunes))+"/"+strconv.Itoa(c.ExpectedCommunes)+" xã chưa gửi báo cáo của kỳ: "+
			strings.Join(reportSourceLimit(c.MissingCommunes, 20), ", "))
	}
	if len(c.OpenProvinces) > 0 {
		out = append(out, "Kỳ chưa chốt ở tỉnh: "+strings.Join(reportSourceLimit(c.OpenProvinces, 20), ", "))
	}
	if c.Unapproved > 0 {
		out = append(out, strconv.Itoa(c.Unapproved)+" dòng tiến độ Đề án chưa được Admin tỉnh duyệt")
	}
	return out
}

func reportSourceLimit(xs []string, n int) []string {
	if len(xs) <= n {
		return xs
	}
	return append(append([]string{}, xs[:n]...), "…")
}

func (s *reportSourceBuilder) build(ctx context.Context, request reportsport.SourceBuildQuery) (reportsport.SourceData, error) {
	if s.deps.Data == nil {
		return reportsport.SourceData{}, problem.New(http.StatusServiceUnavailable, "reports.dependency_unavailable", "Dịch vụ phụ thuộc chưa sẵn sàng").WithDetail("chưa nối cổng dữ liệu pf/extension")
	}
	q := knreportsport.ReportQuery{TenantID: request.TenantID, Scope: request.Scope, Unit: request.Unit, Periods: request.Periods, SeasonCode: request.SeasonCode, SeasonYear: request.SeasonYear, At: request.At}
	label := request.PeriodLabel
	n := &reportSourceNames{d: s.deps, at: request.At, cache: map[string]string{}}
	var b reportsport.SourceData
	if label != "" {
		b.Meta = append(b.Meta, reportsport.SourceMeta{Label: "Kỳ báo cáo", Value: label})
	}
	lv := request.Level
	g := newReportSourceGrouper()
	var comp knreportsport.Completeness
	switch request.Source {
	case knreportsport.SourceSowing:
		d, err := s.deps.Data.Sowing(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			base["season_code"] = reportSourceText(n.catalog(ctx, mdport.CatalogSeason, r.SeasonCode))
			base["variety_code"] = reportSourceText(n.catalog(ctx, mdport.CatalogRiceVariety, r.VarietyCode))
			g.add(k+reportSourceDims(request, "season_code", r.SeasonCode, "variety_code", r.VarietyCode), base, map[string]decimal.Decimal{
				"sown_area_ha": r.SownAreaHa, "cooperatives": reportSourceInt64(r.Cooperatives), "reports": reportSourceInt64(r.Reports)})
		}
		comp = d.Completeness
	case knreportsport.SourceGrowthPest:
		d, err := s.deps.Data.GrowthPest(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			base["pest_code"] = reportSourceText(n.catalog(ctx, mdport.CatalogPest, r.PestCode))
			base["severity"] = reportSourceText(reportSourceSeverityLabels[r.Severity])
			g.add(k+reportSourceDims(request, "pest_code", r.PestCode, "severity", r.Severity), base, map[string]decimal.Decimal{
				"affected_area_ha": r.AffectedAreaHa, "observations": reportSourceInt64(r.Observations), "urgent": reportSourceInt64(r.Urgent)})
		}
		comp = d.Completeness
	case knreportsport.SourceTasksSLA:
		d, err := s.deps.Data.TasksSLA(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			g.add(k, base, map[string]decimal.Decimal{"opened": reportSourceInt64(r.Opened), "resolved": reportSourceInt64(r.Resolved), "on_time": reportSourceInt64(r.OnTime),
				"escalated": reportSourceInt64(r.Escalated), "open": reportSourceInt64(r.Open), "overdue": reportSourceInt64(r.Overdue)})
		}
	case knreportsport.SourcePeriodSummary:
		d, err := s.deps.Data.PeriodSummary(ctx, q)
		if err != nil {
			return b, err
		}
		for _, r := range d.Rows {
			k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
			g.add(k, base, map[string]decimal.Decimal{"sown_area_ha": r.SownAreaHa, "affected_area_ha": r.AffectedAreaHa,
				"pest_reports": reportSourceInt64(r.PestReports), "urgent_reports": reportSourceInt64(r.UrgentReports), "registered_ha": r.RegisteredHa,
				"completed_ha": r.CompletedHa, "tasks_opened": reportSourceInt64(r.TasksOpened), "tasks_resolved": reportSourceInt64(r.TasksResolved),
				"tasks_overdue": reportSourceInt64(r.TasksOverdue)})
		}
		comp = d.Completeness
	case knreportsport.SourceProgramProgress:
		return s.buildProgram(ctx, request, q, n)
	}
	b.Rows, b.Total = g.result(), g.totalRow()
	b.Provisional = comp.Provisional
	b.Notes = reportSourceCompletenessNotes(comp)
	return b, nil
}

var reportSourceHundred = decimal.NewFromInt(100)

func reportSourceDims(def reportsport.SourceBuildQuery, kv ...string) string {
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

func (s *reportSourceBuilder) buildProgram(ctx context.Context, request reportsport.SourceBuildQuery, q knreportsport.ReportQuery, n *reportSourceNames) (reportsport.SourceData, error) {
	d, err := s.deps.Data.ProgramProgress(ctx, q)
	if err != nil {
		return reportsport.SourceData{}, err
	}
	var b reportsport.SourceData
	if q.SeasonCode != "" {
		b.Meta = append(b.Meta, reportsport.SourceMeta{Label: "Vụ", Value: reportSourceSeasonLabel(n.catalog(ctx, mdport.CatalogSeason, q.SeasonCode), q.SeasonYear)})
	}
	lv := request.Level
	g := newReportSourceGrouper()
	coops := map[string]map[uuid.UUID]bool{}
	provincesOf := map[string]map[string]bool{}
	for _, r := range d.Rows {
		k, base := n.unitKey(ctx, lv, r.ProvinceCode, r.CommuneCode)
		if lv == "cooperative" {
			k += "|" + r.CooperativeRef.String()
			base["cooperative_name"] = reportSourceText(n.coop(ctx, r.CooperativeRef))
		}
		season := reportSourceSeasonLabel(n.catalog(ctx, mdport.CatalogSeason, r.SeasonCode), r.SeasonYear)
		base["season"] = reportSourceText(season)
		k += reportSourceDims(request, "season", r.SeasonCode+"-"+strconv.Itoa(r.SeasonYear))
		unapproved := int64(0)
		if !r.Approved {
			unapproved = 1
		}
		g.add(k, base, map[string]decimal.Decimal{"registered_ha": r.RegisteredHa, "deploying_ha": r.DeployingHa,
			"completed_ha": r.CompletedHa, "unapproved": reportSourceInt64(unapproved)})
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
		rows[i]["cooperatives"] = reportSourceInt(int64(len(coops[k])))
		if lv == "national" || lv == "province" {
			t := targetOf(provincesOf[k])
			rows[i]["target_ha"] = reportSourceDecimalPointer(t)
			rows[i]["registered_pct"] = reportSourcePctOf(rows[i]["registered_ha"], t)
		}
	}
	b.Rows = rows
	b.Total = g.totalRow()
	b.Total["cooperatives"] = reportSourceInt(int64(len(allCoops)))
	if lv == "national" || lv == "province" {
		t := targetOf(allProv)
		b.Total["target_ha"] = reportSourceDecimalPointer(t)
		b.Total["registered_pct"] = reportSourcePctOf(b.Total["registered_ha"], t)
	}
	b.Provisional = d.Completeness.Provisional
	b.Notes = reportSourceCompletenessNotes(d.Completeness)
	return b, nil
}

func reportSourcePctOf(v reportsport.SourceCell, target *decimal.Decimal) reportsport.SourceCell {
	if v.Dec == nil || target == nil || !target.IsPositive() {
		return reportsport.SourceCell{}
	}
	p := v.Dec.Mul(reportSourceHundred).DivRound(*target, 4)
	return reportSourceDecimal(p)
}

func reportSourceSeasonLabel(name string, year int) string {
	if year == 0 {
		return name
	}
	return name + " " + strconv.Itoa(year)
}
