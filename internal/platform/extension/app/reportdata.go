package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ReportData hiện thực port.ReportData cho pf/reports: chỉ đọc bảng hiện hành,
// lọc theo phạm vi do pf/reports truyền (đã kiểm vai lúc yêu cầu xuất).
type ReportData struct{ Svc *Service }

var _ port.ReportData = ReportData{}

// errBadQuery: truy vấn thiếu tenant hoặc kỳ không hợp lệ (lỗi lập trình của bên gọi).
var errBadQuery = errors.New("extension: truy vấn báo cáo thiếu tenant hoặc kỳ không hợp lệ")

func (d ReportData) filter(q port.ReportQuery) store.DataFilter {
	return store.DataFilter{All: q.Scope.All, Units: q.Scope.Units, Unit: q.Unit}
}

func (d ReportData) check(q port.ReportQuery, needPeriods bool) error {
	if q.TenantID == uuid.Nil {
		return errBadQuery
	}
	if needPeriods {
		if len(q.Periods) == 0 {
			return errBadQuery
		}
		for _, p := range q.Periods {
			if !domain.ValidPeriod(p) {
				return errBadQuery
			}
		}
	}
	return nil
}

func (d ReportData) tx(ctx context.Context, q port.ReportQuery, fn func(pgx.Tx) error) error {
	if q.Scope.Empty() {
		return nil // phạm vi rỗng: không thấy gì (mặc định chặn)
	}
	return tenant.WithTenantTx(ctx, d.Svc.DB, q.TenantID, fn)
}

// completeness tính xã chưa báo cáo và tỉnh chưa chốt kỳ (US-DASH-02 AC-1).
func completeness(ctx context.Context, tx pgx.Tx, f store.DataFilter, periods []string) (port.Completeness, error) {
	var c port.Completeness
	expected, err := store.ExpectedCommunes(ctx, tx, f)
	if err != nil {
		return c, err
	}
	reported, err := store.ReportedCommunes(ctx, tx, f, periods)
	if err != nil {
		return c, err
	}
	closed, err := store.FullyClosedProvinces(ctx, tx, periods)
	if err != nil {
		return c, err
	}
	rep := map[string]bool{}
	provinces := map[string]bool{}
	for _, r := range reported {
		rep[r.Commune] = true
		provinces[r.Province] = true
	}
	for _, e := range expected {
		provinces[e.Province] = true
		c.ExpectedCommunes++
		if rep[e.Commune] {
			c.ReportedCommunes++
		} else {
			c.MissingCommunes = append(c.MissingCommunes, e.Commune)
		}
	}
	for p := range provinces {
		if !closed[p] {
			c.OpenProvinces = append(c.OpenProvinces, p)
		}
	}
	sort.Strings(c.OpenProvinces)
	sort.Strings(c.MissingCommunes)
	c.Provisional = len(c.MissingCommunes) > 0 || len(c.OpenProvinces) > 0
	return c, nil
}

// Sowing: FR-01 diện tích đã sạ theo xã × vụ × giống.
func (d ReportData) Sowing(ctx context.Context, q port.ReportQuery) (port.SowingData, error) {
	var out port.SowingData
	if err := d.check(q, true); err != nil {
		return out, err
	}
	err := d.tx(ctx, q, func(tx pgx.Tx) error {
		var err error
		if out.Rows, err = store.SowingRows(ctx, tx, d.filter(q), q.Periods); err != nil {
			return err
		}
		out.Completeness, err = completeness(ctx, tx, d.filter(q), q.Periods)
		return err
	})
	return out, err
}

// GrowthPest: FR-02 sâu bệnh theo xã × loại × mức độ.
func (d ReportData) GrowthPest(ctx context.Context, q port.ReportQuery) (port.PestData, error) {
	var out port.PestData
	if err := d.check(q, true); err != nil {
		return out, err
	}
	err := d.tx(ctx, q, func(tx pgx.Tx) error {
		var err error
		if out.Rows, err = store.PestRows(ctx, tx, d.filter(q), q.Periods); err != nil {
			return err
		}
		out.Completeness, err = completeness(ctx, tx, d.filter(q), q.Periods)
		return err
	})
	return out, err
}

// ProgramProgress: FR-03 tiến độ Đề án theo HTX × vụ kèm mục tiêu tỉnh.
// Tạm tính khi còn dòng chưa được Admin tỉnh duyệt.
func (d ReportData) ProgramProgress(ctx context.Context, q port.ReportQuery) (port.ProgramData, error) {
	out := port.ProgramData{Targets: map[string]*decimal.Decimal{}}
	if err := d.check(q, false); err != nil {
		return out, err
	}
	err := d.tx(ctx, q, func(tx pgx.Tx) error {
		var err error
		out.Rows, err = store.ProgramRows(ctx, tx, d.filter(q), q.SeasonCode, q.SeasonYear)
		return err
	})
	if err != nil {
		return out, err
	}
	var provinces []string
	for _, r := range out.Rows {
		provinces = append(provinces, r.ProvinceCode)
		if !r.Approved {
			out.Completeness.Unapproved++
		}
	}
	if q.Unit != "" && len(out.Rows) == 0 {
		if prov, _, perr := d.Svc.provinceOf(ctx, q.Unit, "unit"); perr == nil && prov == q.Unit {
			provinces = append(provinces, prov)
		}
	}
	at := q.At
	if at.IsZero() {
		at = d.Svc.now()
	}
	if out.Targets, err = d.Svc.programTargets(ctx, provinces, at); err != nil {
		return out, err
	}
	out.Completeness.Provisional = out.Completeness.Unapproved > 0
	return out, nil
}

// periodRange trả [đầu kỳ sớm nhất, cuối kỳ muộn nhất) theo giờ Việt Nam.
func periodRange(periods []string) (time.Time, time.Time, error) {
	ps := append([]string(nil), periods...)
	sort.Strings(ps)
	from, _, err := periodBounds(ps[0])
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	_, to, err := periodBounds(ps[len(ps)-1])
	return from, to, err
}

// TasksSLA: nhiệm vụ hỗ trợ trong kỳ — tạo mới, đóng, đúng hạn SLA, leo thang, đang mở/quá hạn tại At.
func (d ReportData) TasksSLA(ctx context.Context, q port.ReportQuery) (port.TaskData, error) {
	var out port.TaskData
	if err := d.check(q, true); err != nil {
		return out, err
	}
	from, to, err := periodRange(q.Periods)
	if err != nil {
		return out, errBadQuery
	}
	at := q.At
	if at.IsZero() {
		at = d.Svc.now()
	}
	err = d.tx(ctx, q, func(tx pgx.Tx) error {
		var err error
		out.Rows, err = store.TaskRows(ctx, tx, d.filter(q), from, to, at)
		return err
	})
	return out, err
}

// PeriodSummary: FR-05 gom FR-01, FR-02, FR-03 và nhiệm vụ theo xã cho kỳ tháng/quý/năm.
func (d ReportData) PeriodSummary(ctx context.Context, q port.ReportQuery) (port.SummaryData, error) {
	var out port.SummaryData
	sow, err := d.Sowing(ctx, q)
	if err != nil {
		return out, err
	}
	pest, err := d.GrowthPest(ctx, q)
	if err != nil {
		return out, err
	}
	pq := q
	pq.SeasonCode, pq.SeasonYear = "", 0
	prog, err := d.ProgramProgress(ctx, pq)
	if err != nil {
		return out, err
	}
	tasks, err := d.TasksSLA(ctx, q)
	if err != nil {
		return out, err
	}
	rows := map[store.UnitPair]*port.SummaryRow{}
	row := func(p, c string) *port.SummaryRow {
		k := store.UnitPair{Province: p, Commune: c}
		if rows[k] == nil {
			rows[k] = &port.SummaryRow{ProvinceCode: p, CommuneCode: c, SownAreaHa: decimal.Zero, AffectedAreaHa: decimal.Zero,
				RegisteredHa: decimal.Zero, CompletedHa: decimal.Zero}
		}
		return rows[k]
	}
	for _, r := range sow.Rows {
		x := row(r.ProvinceCode, r.CommuneCode)
		x.SownAreaHa = x.SownAreaHa.Add(r.SownAreaHa)
	}
	for _, r := range pest.Rows {
		x := row(r.ProvinceCode, r.CommuneCode)
		x.AffectedAreaHa = x.AffectedAreaHa.Add(r.AffectedAreaHa)
		x.PestReports += r.Observations
		x.UrgentReports += r.Urgent
	}
	for _, r := range prog.Rows {
		x := row(r.ProvinceCode, r.CommuneCode)
		x.RegisteredHa = x.RegisteredHa.Add(r.RegisteredHa)
		x.CompletedHa = x.CompletedHa.Add(r.CompletedHa)
	}
	for _, r := range tasks.Rows {
		x := row(r.ProvinceCode, r.CommuneCode)
		x.TasksOpened += r.Opened
		x.TasksResolved += r.Resolved
		x.TasksOverdue += r.Overdue
	}
	keys := make([]store.UnitPair, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Province != keys[j].Province {
			return keys[i].Province < keys[j].Province
		}
		return keys[i].Commune < keys[j].Commune
	})
	for _, k := range keys {
		out.Rows = append(out.Rows, *rows[k])
	}
	out.Completeness = sow.Completeness
	out.Completeness.Unapproved = prog.Completeness.Unapproved
	out.Completeness.Provisional = sow.Completeness.Provisional || prog.Completeness.Provisional
	return out, nil
}
