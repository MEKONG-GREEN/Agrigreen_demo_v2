package app

import (
	"context"
	pa "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"slices"
	"strings"
	"time"
)

type DashboardQuery struct {
	Year, Quarter     int
	Department, Field string
}
type DashboardCounts struct {
	Exported int `json:"exported"`
	Open     int `json:"open"`
	Sent     int `json:"sent"`
	Forms    int `json:"forms"`
	Achieved int `json:"achieved"`
	Items    int `json:"items"`
	Slow     int `json:"slow"`
}
type DashboardPeriod struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	State      string     `json:"state"`
	Cutoff     string     `json:"cutoff"`
	ExportedAt *time.Time `json:"exported_at"`
}
type DashboardStatus struct {
	PeriodID   uuid.UUID  `json:"period_id"`
	PeriodName string     `json:"period_name"`
	Department string     `json:"department"`
	Name       string     `json:"name"`
	Kind       string     `json:"kind"`
	State      string     `json:"state"`
	At         *time.Time `json:"at"`
}
type Dashboard struct {
	Year           int                      `json:"year"`
	Quarter        int                      `json:"quarter"`
	Department     string                   `json:"department"`
	Field          string                   `json:"field"`
	Counts         DashboardCounts          `json:"counts"`
	Progress       domain.DashboardProgress `json:"progress"`
	ProgressPeriod *DashboardPeriod         `json:"progress_period"`
	Reports        []DashboardPeriod        `json:"reports"`
	Status         []DashboardStatus        `json:"status"`
	AsOf           time.Time                `json:"as_of"`
	CanAct         bool                     `json:"can_act"`
}

// Dashboard reads live status and captures the newest eligible Mẫu 5 again on every
// request. It never refreshes or changes a saved draft/export.
func (s *Service) Dashboard(ctx context.Context, q DashboardQuery, periods *pa.Service) (Dashboard, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return Dashboard{}, err
	}
	now := s.now().In(clock.Zone())
	if q.Year == 0 {
		q.Year = now.Year()
	}
	if q.Quarter == 0 {
		q.Quarter = (int(now.Month())-1)/3 + 1
	}
	if q.Year < 2000 || q.Year > 2100 {
		return Dashboard{}, problem.Validation.WithField("year", "2000–2100")
	}
	if q.Quarter < 1 || q.Quarter > 4 {
		return Dashboard{}, problem.Validation.WithField("quarter", "1–4")
	}
	q.Department, q.Field = strings.TrimSpace(q.Department), strings.TrimSpace(q.Field)
	if len(q.Department) > 64 || len(q.Field) > 64 {
		return Dashboard{}, problem.Validation.WithField("filter", "tối đa 64 ký tự")
	}
	out := Dashboard{Year: q.Year, Quarter: q.Quarter, Department: q.Department, Field: q.Field, AsOf: now, Reports: []DashboardPeriod{}, Status: []DashboardStatus{}, Progress: domain.DashboardProgress{Programs: []domain.DashboardProgram{}, Rows: []domain.ProgramRow{}}, CanAct: c.kn.Role == port.SessionRoleProvince && !c.kn.ReadOnly() && c.kn.ProvinceGroup != "lanh_dao_phong" && c.kn.ProvinceGroup != "ban_giam_doc"}
	// The source list API caps at 100; do not silently return incomplete KPIs.
	var all []ps.Period
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { var e error; all, e = ps.Periods(ctx, tx, q.Year, 101); return e })
	if err != nil {
		return Dashboard{}, err
	}
	if len(all) > 100 {
		return Dashboard{}, ErrInvalid.WithDetail("Năm có hơn 100 kỳ; dashboard chưa thể tổng hợp đầy đủ")
	}
	if periods == nil {
		return Dashboard{}, ErrDependency
	}
	details, err := periods.List(ctx, q.Year, 100)
	if err != nil {
		return Dashboard{}, err
	}
	end := time.Date(q.Year, time.Month(q.Quarter*3+1), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	var selected *ps.Period
	var openPeriods []pa.Detail
	for _, d := range details {
		p := d.Period
		if d.State == pd.PeriodCancelled {
			continue
		}
		if q.Department != "" && !slices.Contains(p.Departments, q.Department) {
			continue
		}
		// Reports table is annual and includes quarterly/yearly exports. Progress and
		// current-period statuses respect the selected quarter.
		if p.Type == pd.TypeQuarter || p.Type == pd.TypeYear {
			out.Reports = append(out.Reports, DashboardPeriod{ID: p.ID, Name: p.Name, Type: p.Type, State: d.State, Cutoff: p.Cutoff.Format(time.DateOnly)})
		}
		if p.Type == pd.TypeProgram && !p.Cutoff.After(end) && !p.Cutoff.After(time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)) && (selected == nil || p.Cutoff.After(selected.Cutoff)) {
			copy := p
			selected = &copy
		}
		if d.State == pd.PeriodClosed || p.From.After(end) || p.To.Before(time.Date(q.Year, time.Month((q.Quarter-1)*3+1), 1, 0, 0, 0, 0, time.UTC)) {
			continue
		}
		openPeriods = append(openPeriods, d)
		out.Counts.Open++
		for _, f := range d.Forms {
			if q.Department != "" && f.Department != q.Department {
				continue
			}
			out.Counts.Forms++
			if f.View.CountsAsSent {
				out.Counts.Sent++
			}
			out.Status = append(out.Status, DashboardStatus{PeriodID: p.ID, PeriodName: p.Name, Department: f.Department, Name: f.DepartmentName, Kind: "form", State: f.View.State, At: f.LastSubmittedAt})
		}
	}
	ids := make([]uuid.UUID, 0, len(openPeriods))
	for _, d := range openPeriods {
		ids = append(ids, d.Period.ID)
	}
	sources, err := periods.SourceStates(ctx, ids)
	if err != nil {
		return Dashboard{}, err
	}
	for _, d := range openPeriods {
		p := d.Period
		for _, r := range sources[d.Period.ID] {
			if q.Department != "" && r.Ref.Department != q.Department {
				continue
			}
			state := "unknown"
			if r.Known {
				state = "open"
				if r.Closed {
					state = "closed"
				}
			}
			out.Status = append(out.Status, DashboardStatus{PeriodID: p.ID, PeriodName: p.Name, Department: r.Ref.Department, Name: r.Ref.Label, Kind: "source", State: state, At: r.At})
		}
	}
	if selected != nil {
		data, _, e := s.capture(ctx, c, *selected)
		if e != nil {
			return Dashboard{}, e
		}
		out.Progress, e = domain.SummarizeDashboard(data, q.Department, q.Field, now)
		if e != nil {
			return Dashboard{}, e
		}
		out.ProgressPeriod = &DashboardPeriod{ID: selected.ID, Name: selected.Name, Type: selected.Type, Cutoff: selected.Cutoff.Format(time.DateOnly)}
		out.Counts.Items, out.Counts.Achieved, out.Counts.Slow = out.Progress.Items, out.Progress.Achieved, out.Progress.Slow
	}
	err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error {
		rows, e := store.DashboardExports(ctx, tx, q.Year, q.Department)
		if e != nil {
			return e
		}
		out.Counts.Exported = len(rows)
		for _, x := range rows {
			for i := range out.Reports {
				if out.Reports[i].ID == x.PeriodID && out.Reports[i].ExportedAt == nil {
					at := x.At
					out.Reports[i].ExportedAt = &at
					out.Reports[i].State = "exported"
				}
			}
		}
		return nil
	})
	if err != nil {
		return Dashboard{}, err
	}
	return out, nil
}
