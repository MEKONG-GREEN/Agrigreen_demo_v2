package domain

import (
	"fmt"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// LiveM5Window is a read/calculation window, not an opened reporting period.
// ThroughDate is inclusive in Vietnam time; quantities include only full KHTC
// weeks ending on/before it. Price/confirmation and effective-plan reads use the
// requested cutoff, independently of the last quantity week.
type LiveM5Window struct {
	Year                             int       `json:"year"`
	Quarter                          int       `json:"quarter"`
	ThroughDate                      string    `json:"through_date"`
	EffectiveCutoff                  string    `json:"effective_cutoff"`
	FromDate                         string    `json:"from_date"`
	QuantityTo                       string    `json:"quantity_to"`
	CalendarID                       string    `json:"calendar_id"`
	CalendarVersion                  int       `json:"calendar_version"`
	PlanVersionID                    *string   `json:"plan_version_id"`
	From, To, Cutoff, ConfirmationAt time.Time `json:"-"`
}

func NewLiveM5Window(year, quarter int, raw string, now time.Time) (*LiveM5Window, error) {
	if year < 2000 || year > 2100 || quarter < 1 || quarter > 4 {
		return nil, ErrInput
	}
	start := rc.Date(year, time.Month(quarter*3-2), 1)
	end := start.AddDate(0, 3, -1)
	local := now.In(clock.Zone())
	today := rc.Date(local.Year(), local.Month(), local.Day())
	through := end
	if through.After(today) {
		through = today
	}
	if raw != "" {
		// Bound and verify canonical ASCII syntax before invoking a date parser.
		if len(raw) != 10 || raw[4] != '-' || raw[7] != '-' {
			return nil, ErrInput
		}
		for i := range raw {
			if i != 4 && i != 7 && (raw[i] < '0' || raw[i] > '9') {
				return nil, ErrInput
			}
		}
		var err error
		through, err = time.Parse(time.DateOnly, raw)
		if err != nil || through.Format(time.DateOnly) != raw {
			return nil, ErrInput
		}
	}
	if raw == "" && today.Before(start) {
		return nil, nil
	}
	if through.Before(start) || through.After(end) {
		return nil, ErrInput
	}
	requested := through
	if through.After(today) {
		through = today
	}
	if through.Before(start) {
		return nil, nil
	}
	confirmation := time.Date(through.Year(), through.Month(), through.Day(), 0, 0, 0, 0, clock.Zone()).AddDate(0, 0, 1).Add(-time.Nanosecond)
	if confirmation.After(now) {
		confirmation = now
	}
	first := rc.KHTCRule.WeekOf(rc.Date(year, 1, 1))
	last := rc.KHTCRule.WeekOf(through)
	to := last.To
	if to.After(through) {
		to = last.From.AddDate(0, 0, -1)
	}
	if to.Before(first.From) {
		return nil, nil
	}
	return &LiveM5Window{Year: year, Quarter: quarter, ThroughDate: requested.Format(time.DateOnly), EffectiveCutoff: confirmation.UTC().Format(time.RFC3339Nano), FromDate: first.From.Format(time.DateOnly), QuantityTo: to.Format(time.DateOnly),
		CalendarID: rc.KHTCRule.CalendarID, CalendarVersion: rc.KHTCRule.CalendarVersion, From: first.From, To: to, Cutoff: through, ConfirmationAt: confirmation}, nil
}

// ComposeLiveM5 shares the frozen-report arithmetic and numeric guards without
// constructing an entity, UUID, snapshot, report link or lifecycle state.
func ComposeLiveM5(window LiveM5Window, plan port.KHTCPlan, submitted []Submitted, metrics []SourceMetric, at time.Time) (Data, error) {
	if window.Year != plan.Year || window.Cutoff.IsZero() {
		return Data{}, fmt.Errorf("%w: live window", ErrInput)
	}
	data := Data{Template: "mau_5", Plan: plan, Metrics: metrics, Rows: []ProgramRow{}, Submissions: []Submitted{}, FormTexts: map[string]map[string]string{}, Provisional: []string{}, CapturedAt: at}
	return composeCalculation(data, calculationWindow{year: window.Year, from: window.From, to: window.To, kind: "program", live: true}, submitted)
}

// SummarizeLiveM5 is a pure bridge to the unchanged dashboard summary. The sole
// Period field populated is the calculation year used by its slow threshold;
// this envelope is never serialized, captured, persisted or passed to a port.
func SummarizeLiveM5(window LiveM5Window, data Data, department, field string, now time.Time) (DashboardProgress, error) {
	data.Period.Year = window.Year
	return SummarizeDashboard(data, department, field, now)
}
