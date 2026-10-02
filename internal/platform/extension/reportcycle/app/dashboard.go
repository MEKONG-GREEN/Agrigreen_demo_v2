package app

import (
	"context"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/reportcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Ngưỡng cảnh báo dashboard — TẠM, chờ khách (phiên bản ghi trong phản hồi).
const (
	AlertsVersion       = "kn.dashboard.alerts/v1-tam"
	pestSurgePct        = 50 // DT nhiễm một đối tượng tăng ≥ 50 % so kỳ trước …
	pestSurgeMinHa      = 10 // … và ≥ 10 ha → "Khẩn"
	lowProgressPct      = 50 // khu vực đạt < 50 % kế hoạch xuống giống → cảnh báo
	dashboardPeriodScan = 200
)

// DashboardQuery là bộ lọc dashboard (FN-TI-02.01 / FN-TW-01.01).
type DashboardQuery struct {
	Province string // Cấp Quốc gia/Admin chọn tỉnh; Cấp Tỉnh bỏ qua (tỉnh mình)
	Region   string
	PeriodID *uuid.UUID
	BatchID  *uuid.UUID
}

// Metric là một số đã dựng sẵn kèm mốc cập nhật.
type Metric struct {
	Value decimal.Decimal
	AsOf  *time.Time
	Has   bool
}

// SeriesPoint là một điểm biểu đồ.
type SeriesPoint struct {
	Key   string
	Label string
	Value decimal.Decimal
	Plan  *decimal.Decimal
	Pct   *decimal.Decimal
}

// PestDelta là sâu bệnh so kỳ trước.
type PestDelta struct {
	Indicator string
	Area      decimal.Decimal
	Previous  decimal.Decimal
	Delta     decimal.Decimal
}

// CommuneDash là số của một xã (drill-down).
type CommuneDash struct {
	Commune, Name, Region string
	SownArea, SownPlan    decimal.Decimal
	PestArea              decimal.Decimal
	Submitted             bool
	PendingForms          []string
	Officers              []uuid.UUID
}

// Alert là một cảnh báo dashboard.
type Alert struct {
	Level   string // urgent | warning | info
	Code    string
	Message string
}

// Dashboard là dashboard trồng trọt (chỉ đọc; số = lần tổng hợp gần nhất của kỳ).
type Dashboard struct {
	Province     string
	Region       string
	Period       *store.Period
	Batch        *store.Batch
	Periods      []store.Period
	UpdatedAt    *time.Time
	SownArea     Metric
	SownPlan     Metric
	SownPct      Metric
	HarvestArea  Metric
	HarvestOut   Metric
	HarvestYield Metric
	VegSown      Metric
	VegHarvest   Metric
	VegOutput    Metric
	PestArea     Metric
	PestSevere   Metric
	FruitArea    Metric
	SownByWeek   []SeriesPoint
	Tra          []SeriesPoint
	SownByRegion []SeriesPoint
	PestsTop     []PestDelta
	Submitted    int
	Communes     int
	Unsubmitted  []CommuneDash
	ByCommune    []CommuneDash
	Alerts       []Alert
	Names        map[uuid.UUID]string
	RegionNames  map[string]string
	Versions     map[string]string
}

// KNDashboard dựng dashboard từ bảng chỉ số dựng sẵn khi "Cập nhật" (consolidation_metric) + trạng thái nộp —
// 6–8 truy vấn theo chỉ mục, không quét bài nộp (mục tiêu < 5 s, xem docs/runbooks/kn-dashboard-load.md).
func (s *Service) KNDashboard(ctx context.Context, q DashboardQuery) (Dashboard, error) {
	a, err := s.knActor(ctx, authz.KNTinh, authz.KNTW, authz.KNAdmin)
	if err != nil {
		return Dashboard{}, err
	}
	province := a.province
	if province == "" {
		province = q.Province
	}
	if province == "" {
		return Dashboard{}, problem.Validation.WithField("province", "chọn tỉnh")
	}
	dept := a.dept()
	if dept == "" {
		dept = "trong_trot"
	}
	d := Dashboard{Province: province, Region: q.Region, Versions: map[string]string{"metrics": domain.MetricsVersion, "alerts": AlertsVersion}}
	now := s.now()
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		ps, err := store.ListPeriods(ctx, tx, store.PeriodFilter{Province: province, Department: dept, BatchID: q.BatchID, Limit: dashboardPeriodScan})
		if err != nil {
			return err
		}
		var cur *store.Period
		if q.PeriodID != nil {
			for i := range ps {
				if ps[i].ID == *q.PeriodID {
					cur = &ps[i]
				}
			}
			if cur == nil {
				return ErrPeriodNotFound
			}
		} else {
			// Kỳ tuần gần nhất đã bắt đầu và có lần tổng hợp; không có → kỳ gần nhất đã bắt đầu.
			for i := range ps {
				if ps[i].From.After(now) {
					continue
				}
				l, err := store.LatestOfPeriod(ctx, tx, ps[i].ID)
				if err != nil {
					return err
				}
				if len(l) > 0 {
					cur = &ps[i]
					break
				}
				if cur == nil {
					cur = &ps[i]
				}
			}
		}
		if cur == nil {
			return nil // "Chưa có dữ liệu"
		}
		d.Period = cur
		if d.Batch, err = store.BatchByID(ctx, tx, cur.BatchID, false); err != nil {
			return err
		}
		if d.Periods, err = store.PeriodsOfBatch(ctx, tx, cur.BatchID); err != nil {
			return err
		}
		latest, err := store.LatestOfPeriod(ctx, tx, cur.ID)
		if err != nil {
			return err
		}
		runAt := map[uuid.UUID]time.Time{}
		var runIDs []uuid.UUID
		for _, l := range latest {
			runIDs = append(runIDs, l.RunID)
			runAt[l.RunID] = l.RunAt
			if d.UpdatedAt == nil || l.RunAt.After(*d.UpdatedAt) {
				t := l.RunAt
				d.UpdatedAt = &t
			}
		}
		ms, err := store.MetricsOfRuns(ctx, tx, runIDs, []string{domain.ScopeProvince, domain.ScopeRegion, domain.ScopeCommune})
		if err != nil {
			return err
		}
		fill(&d, ms, runAt, province, q.Region)
		// Sâu bệnh so kỳ trước (cùng đợt, khe trước).
		for _, p := range d.Periods {
			if p.Slot != cur.Slot-1 {
				continue
			}
			pl, err := store.LatestOfPeriod(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			var prevIDs []uuid.UUID
			for _, l := range pl {
				prevIDs = append(prevIDs, l.RunID)
			}
			pms, err := store.MetricsOfRuns(ctx, tx, prevIDs, []string{domain.ScopeProvince})
			if err != nil {
				return err
			}
			pestDeltas(&d, ms, pms)
		}
		// Tình hình nộp của các xã (thời gian thực).
		tasks, err := store.TasksOfPeriod(ctx, tx, cur.ID, nil, false)
		if err != nil {
			return err
		}
		submission(&d, tasks, *cur, now, q.Region)
		return nil
	})
	if err != nil {
		return d, err
	}
	nb, err := s.regionNames(ctx, province)
	if err != nil {
		return d, err
	}
	d.RegionNames = map[string]string{}
	for code, r := range nb.regions {
		d.RegionNames[code] = r.Name
	}
	for i := range d.ByCommune {
		d.ByCommune[i].Name = nb.commune(ctx, s, d.ByCommune[i].Commune)
	}
	for i := range d.Unsubmitted {
		d.Unsubmitted[i].Name = nb.commune(ctx, s, d.Unsubmitted[i].Commune)
	}
	for i := range d.SownByRegion {
		d.SownByRegion[i].Label = nb.region(d.SownByRegion[i].Key)
	}
	refs := []uuid.UUID{}
	for _, c := range d.Unsubmitted {
		refs = append(refs, c.Officers...)
	}
	d.Names = s.names(ctx, refs)
	alerts(&d, now, nb)
	return d, nil
}

func metricAt(ms []store.Metric, name, scope, unit string, runAt map[uuid.UUID]time.Time) Metric {
	var m Metric
	for _, x := range ms {
		if x.Metric == name && x.Scope == scope && x.Unit == unit {
			m.Value, m.Has = m.Value.Add(x.Value), true
			t := runAt[x.RunID]
			m.AsOf = &t
		}
	}
	return m
}

func fill(d *Dashboard, ms []store.Metric, runAt map[uuid.UUID]time.Time, province, region string) {
	scope, unit := domain.ScopeProvince, province
	if region != "" {
		scope, unit = domain.ScopeRegion, region
	}
	get := func(name string) Metric { return metricAt(ms, name, scope, unit, runAt) }
	d.SownArea, d.SownPlan = get(domain.MetricSownArea), get(domain.MetricSownPlan)
	if d.SownArea.Has && d.SownPlan.Has {
		d.SownPct = Metric{Value: domain.Ratio(d.SownArea.Value, d.SownPlan.Value), Has: true, AsOf: d.SownArea.AsOf}
	}
	d.HarvestArea, d.HarvestOut = get(domain.MetricHarvestArea), get(domain.MetricHarvestOutput)
	if d.HarvestArea.Has && d.HarvestOut.Has {
		d.HarvestYield = Metric{Value: domain.Yield(d.HarvestOut.Value, d.HarvestArea.Value), Has: true, AsOf: d.HarvestArea.AsOf}
	}
	d.VegSown, d.VegHarvest, d.VegOutput = get(domain.MetricVegSown), get(domain.MetricVegHarvestArea), get(domain.MetricVegOutput)
	d.PestArea, d.PestSevere, d.FruitArea = get(domain.MetricPestArea), get(domain.MetricPestSevere), get(domain.MetricFruitArea)
	// Tiến độ xuống giống theo tuần (khe kỳ của PL 01) và phân trà.
	week := map[int]decimal.Decimal{}
	tra := map[int]decimal.Decimal{}
	for _, x := range ms {
		if x.Scope != scope || x.Unit != unit {
			continue
		}
		if n, ok := suffixInt(x.Metric, domain.MetricSownWeek+":"); ok {
			week[n] = week[n].Add(x.Value)
		}
		if n, ok := suffixInt(x.Metric, domain.MetricTra+":"); ok {
			tra[n] = tra[n].Add(x.Value)
		}
	}
	for _, n := range sortedKeys(week) {
		if d.Period != nil && n > d.Period.Slot {
			continue
		}
		label := "Tuần " + strconv.Itoa(n)
		for _, p := range d.Periods {
			if p.Slot == n {
				label = p.Label
			}
		}
		d.SownByWeek = append(d.SownByWeek, SeriesPoint{Key: strconv.Itoa(n), Label: label, Value: week[n]})
	}
	for _, n := range sortedKeys(tra) {
		d.Tra = append(d.Tra, SeriesPoint{Key: strconv.Itoa(n), Label: "Trà " + strconv.Itoa(n), Value: tra[n]})
	}
	// Theo khu vực (luôn cả tỉnh, để so sánh).
	byRegion := map[string]*SeriesPoint{}
	for _, x := range ms {
		if x.Scope != domain.ScopeRegion || (x.Metric != domain.MetricSownArea && x.Metric != domain.MetricSownPlan) {
			continue
		}
		sp := byRegion[x.Unit]
		if sp == nil {
			sp = &SeriesPoint{Key: x.Unit}
			byRegion[x.Unit] = sp
		}
		if x.Metric == domain.MetricSownArea {
			sp.Value = sp.Value.Add(x.Value)
		} else {
			v := x.Value
			if sp.Plan != nil {
				v = sp.Plan.Add(v)
			}
			sp.Plan = &v
		}
	}
	keys := make([]string, 0, len(byRegion))
	for k := range byRegion {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sp := *byRegion[k]
		if sp.Plan != nil {
			pct := domain.Ratio(sp.Value, *sp.Plan)
			sp.Pct = &pct
		}
		d.SownByRegion = append(d.SownByRegion, sp)
	}
	// Theo xã (drill-down).
	byCommune := map[string]*CommuneDash{}
	for _, x := range ms {
		if x.Scope != domain.ScopeCommune {
			continue
		}
		c := byCommune[x.Unit]
		if c == nil {
			c = &CommuneDash{Commune: x.Unit}
			byCommune[x.Unit] = c
		}
		switch x.Metric {
		case domain.MetricSownArea:
			c.SownArea = c.SownArea.Add(x.Value)
		case domain.MetricSownPlan:
			c.SownPlan = c.SownPlan.Add(x.Value)
		case domain.MetricPestArea:
			c.PestArea = c.PestArea.Add(x.Value)
		}
	}
	cs := make([]string, 0, len(byCommune))
	for k := range byCommune {
		cs = append(cs, k)
	}
	sort.Strings(cs)
	for _, k := range cs {
		d.ByCommune = append(d.ByCommune, *byCommune[k])
	}
}

func pestDeltas(d *Dashboard, cur, prev []store.Metric) {
	pv := map[string]decimal.Decimal{}
	for _, m := range prev {
		if strings.HasPrefix(m.Metric, domain.MetricPestArea+":") {
			pv[m.Metric] = pv[m.Metric].Add(m.Value)
		}
	}
	for _, m := range cur {
		if m.Scope != domain.ScopeProvince || !strings.HasPrefix(m.Metric, domain.MetricPestArea+":") {
			continue
		}
		p := pv[m.Metric]
		d.PestsTop = append(d.PestsTop, PestDelta{Indicator: strings.TrimPrefix(m.Metric, domain.MetricPestArea+":"), Area: m.Value, Previous: p, Delta: m.Value.Sub(p)})
	}
	sort.SliceStable(d.PestsTop, func(i, j int) bool { return d.PestsTop[i].Delta.GreaterThan(d.PestsTop[j].Delta) })
	if len(d.PestsTop) > 5 {
		d.PestsTop = d.PestsTop[:5]
	}
}

func submission(d *Dashboard, tasks []store.Task, p store.Period, now time.Time, region string) {
	type agg struct {
		pending  []string
		region   string
		officers []uuid.UUID
	}
	m := map[string]*agg{}
	for _, t := range tasks {
		if region != "" && t.RegionCode != region {
			continue
		}
		a := m[t.CommuneCode]
		if a == nil {
			a = &agg{region: t.RegionCode}
			m[t.CommuneCode] = a
		}
		st := domain.TaskState{Status: t.Status}
		if st.Pending() {
			a.pending = append(a.pending, t.FormCode)
		}
		for _, o := range t.Assignees {
			if !slices.Contains(a.officers, o) {
				a.officers = append(a.officers, o)
			}
		}
	}
	d.Communes = len(m)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		a := m[k]
		if len(a.pending) == 0 {
			d.Submitted++
			continue
		}
		d.Unsubmitted = append(d.Unsubmitted, CommuneDash{Commune: k, Region: a.region, PendingForms: a.pending, Officers: a.officers})
	}
	for i := range d.ByCommune {
		if a, ok := m[d.ByCommune[i].Commune]; ok {
			d.ByCommune[i].Submitted = len(a.pending) == 0
		}
	}
	_ = p
	_ = now
}

func alerts(d *Dashboard, now time.Time, nb *nameBook) {
	for _, pd := range d.PestsTop {
		if pd.Previous.IsPositive() && pd.Delta.GreaterThanOrEqual(decimal.NewFromInt(pestSurgeMinHa)) &&
			pd.Delta.Mul(decimal.NewFromInt(100)).GreaterThanOrEqual(pd.Previous.Mul(decimal.NewFromInt(pestSurgePct))) {
			d.Alerts = append(d.Alerts, Alert{Level: "urgent", Code: "pest_surge", Message: "Sâu bệnh tăng mạnh: " + pd.Indicator + " tăng " + pd.Delta.StringFixed(1) + " ha so kỳ trước"})
		}
	}
	if n := len(d.Unsubmitted); n > 0 && d.Period != nil {
		level := "info"
		if !now.Before(d.Period.DueAt) {
			level = "warning"
		}
		d.Alerts = append(d.Alerts, Alert{Level: level, Code: "unsubmitted", Message: strconv.Itoa(n) + " xã chưa nộp " + d.Period.Label})
	}
	for _, r := range d.SownByRegion {
		if r.Pct != nil && r.Plan != nil && r.Plan.IsPositive() && r.Pct.LessThan(decimal.NewFromInt(lowProgressPct)) {
			d.Alerts = append(d.Alerts, Alert{Level: "warning", Code: "low_progress", Message: "Tiến độ xuống giống thấp: " + nb.region(r.Key) + " đạt " + r.Pct.String() + " % kế hoạch"})
		}
	}
	if d.UpdatedAt != nil {
		d.Alerts = append(d.Alerts, Alert{Level: "info", Code: "consolidated", Message: "Đã cập nhật tổng hợp lúc " + d.UpdatedAt.In(zone()).Format("15:04 02/01/2006")})
	}
}

func suffixInt(s, prefix string) (int, bool) {
	if !strings.HasPrefix(s, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, prefix))
	return n, err == nil
}

func sortedKeys(m map[int]decimal.Decimal) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// ---------------------------------------------------------------------------
// Báo cáo của xã (FN-XA-03.07)
// ---------------------------------------------------------------------------

// CommuneStats là khung "Thống kê của xã" theo vụ (đợt).
type CommuneStats struct {
	Commune    string
	Batch      *store.Batch
	SownArea   Metric
	SownPlan   Metric
	SownPct    Metric
	PestArea   Metric
	DueTasks   int // công việc đã đến hạn
	OnTime     int // nộp đúng hạn (lần gửi đầu ≤ hạn)
	Submitted  int
	TotalTasks int
}

// CommuneStatsOf tính thống kê của xã từ CHÍNH các bản đã gửi của xã (AC: khớp báo cáo đã gửi).
func (s *Service) CommuneStatsOf(ctx context.Context, commune string, batchID *uuid.UUID) (CommuneStats, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return CommuneStats{}, err
	}
	if a.k.Role == authz.KNXa && !slices.Contains(a.k.Communes, commune) {
		return CommuneStats{}, ErrOutOfScope
	}
	out := CommuneStats{Commune: commune}
	now := s.now()
	fc := s.forms(a.p.TenantID)
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		f := store.TaskFilter{Department: a.dept(), Province: a.province, Communes: []string{commune}, BatchID: batchID, OrderByDue: true}
		tasks, err := store.ListTasks(ctx, tx, f)
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			return nil
		}
		// Mặc định: đợt có công việc gần nhất của xã.
		bid := tasks[len(tasks)-1].BatchID
		if batchID != nil {
			bid = *batchID
		}
		if out.Batch, err = store.BatchByID(ctx, tx, bid, false); err != nil {
			return err
		}
		latestBy := map[string]store.Task{} // mã mẫu → công việc có bản gửi ở khe lớn nhất
		for _, t := range tasks {
			if t.BatchID != bid {
				continue
			}
			out.TotalTasks++
			if t.SubmittedAt != nil {
				out.Submitted++
			}
			if !now.Before(t.DueAt) {
				out.DueTasks++
				if t.FirstSubmittedAt != nil && !t.FirstSubmittedAt.After(t.DueAt) {
					out.OnTime++
				}
			}
			if t.SubmittedSubmissionID != nil {
				if cur, ok := latestBy[t.FormCode]; !ok || t.Slot > cur.Slot {
					latestBy[t.FormCode] = t
				}
			}
		}
		for code, t := range latestBy {
			sub, err := store.SubmissionByID(ctx, tx, *t.SubmittedSubmissionID)
			if err != nil || sub == nil {
				return err
			}
			fi, err := fc.get(ctx, t.VersionID)
			if err != nil {
				return err
			}
			rows := make([]domain.Row, 0, len(sub.Rows))
			for _, r := range sub.Rows {
				rows = append(rows, domain.Row{Key: r.Key, Commune: commune, Indicator: r.Indicator, Cells: r.Cells})
			}
			at := sub.EventTime
			for _, m := range domain.ExtractMetrics(code, t.ProvinceCode, fi.def, rows, nil) {
				if m.Scope != domain.ScopeCommune {
					continue
				}
				mv := Metric{Value: m.Value, Has: true, AsOf: &at}
				switch m.Metric {
				case domain.MetricSownArea:
					out.SownArea = mv
				case domain.MetricSownPlan:
					out.SownPlan = mv
				case domain.MetricPestArea:
					out.PestArea = mv
				}
			}
		}
		if out.SownArea.Has && out.SownPlan.Has {
			out.SownPct = Metric{Value: domain.Ratio(out.SownArea.Value, out.SownPlan.Value), Has: true, AsOf: out.SownArea.AsOf}
		}
		return nil
	})
	return out, err
}
