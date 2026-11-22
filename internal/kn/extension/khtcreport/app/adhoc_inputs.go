package app

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	form "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	fs "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/store"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	sd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"time"
)

// Bounded typed decimals must be checked before String, arithmetic or table rendering.
// A malformed port must not allocate a number with a hostile exponent.
func boundedDecimal(d *decimal.Decimal) bool {
	return d == nil || d.Exponent() >= -18 && d.Exponent() <= 18 && d.Coefficient().BitLen() <= 160
}
func boundedMetrics(metrics []domain.SourceMetric) error {
	if len(metrics) > 10000 {
		return domain.ErrInput
	}
	for _, m := range metrics {
		v := m.Value
		for _, n := range []*decimal.Decimal{v.Cumulative, v.Base, v.Increment, v.YearToDate, v.Sum, v.Min, v.Max, v.Unconfirmed} {
			if !boundedDecimal(n) {
				return domain.ErrInput
			}
		}
		if len(v.Weeks) > 60 || len(v.PriceDays) > 2 {
			return domain.ErrInput
		}
		for _, w := range v.Weeks {
			for _, n := range []*decimal.Decimal{&w.Cumulative, &w.Base, &w.Increment, &w.YearToDate, &w.Unconfirmed, &w.Sum} {
				if !boundedDecimal(n) {
					return domain.ErrInput
				}
			}
		}
		for _, day := range v.PriceDays {
			if !boundedDecimal(&day.Min) || !boundedDecimal(&day.Max) {
				return domain.ErrInput
			}
		}
	}
	return nil
}
func boundedPlan(plan port.KHTCPlan) error {
	if len(plan.Entries) > 10000 {
		return domain.ErrInput
	}
	for _, e := range plan.Entries {
		if !boundedDecimal(e.PlannedQty) {
			return domain.ErrInput
		}
	}
	return nil
}
func reportSupplements(rows []ps.AdhocRequest) []domain.Supplement {
	out := make([]domain.Supplement, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.Supplement{ID: r.ID, Department: r.Department, Instruction: r.Instruction, State: r.State, DueAt: r.DueAt, NativeSubmissionRef: r.NativeSubmissionRef, ReturnApplied: r.ReturnApplied})
	}
	return out
}
func (s *Service) supplementInputs(ctx context.Context, c caller, p ps.Period, _ []domain.Submitted) ([]domain.Supplement, []domain.SourceMetric, error) {
	var rows []ps.AdhocRequest
	if err := s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { var e error; rows, e = ps.AdhocRequests(ctx, tx, p.ID, ""); return e }); err != nil {
		return nil, nil, err
	}
	byDept := map[string][]formsport.SubmissionRef{}
	metrics := []domain.SourceMetric{}
	for _, r := range rows {
		if r.State == "failed" || r.State == "cancelled" {
			continue
		}
		subs, ok := byDept[r.Department]
		if !ok {
			var err error
			subs, err = s.Forms.Submissions(ctx, pd.ProfileKHTC, p.FormsPeriodRef, r.Department)
			if err != nil {
				return nil, nil, err
			}
			if len(subs) > 3 {
				return nil, nil, ErrDependency
			}
			byDept[r.Department] = subs
		}
		for _, sb := range subs {
			ref, err := uuid.Parse(sb.Values[form.FieldSheet])
			if err != nil {
				continue
			}
			var sealed *fs.Sheet
			if err = s.tx(ctx, c.principal.TenantID, func(tx pgx.Tx) error { var e error; sealed, e = fs.ByID(ctx, tx, ref); return e }); err != nil {
				return nil, nil, err
			}
			if sealed == nil || sealed.Kind != fs.KindSealed || sealed.PeriodID != p.ID || sealed.Department != r.Department || sealed.FormsPeriodRef != p.FormsPeriodRef || sealed.Content.AdhocRequestID != r.ID.String() {
				continue
			}
			_, hash, err := sealed.Content.Hash()
			if err != nil || hex.EncodeToString(hash) != hex.EncodeToString(sealed.ContentHash) {
				return nil, nil, ErrDependency.WithDetail("Bảng bổ sung không khớp băm")
			}
			if !(form.Result{Issues: form.ValidateAdhocAnswer(sealed.Content, r.Indicators, true)}).Valid() {
				return nil, nil, ErrDependency.WithDetail("Bảng bổ sung không hợp lệ")
			}
			digest := hex.EncodeToString(hash)
			trace := []domain.SourceTrace{{TargetKey: r.ID.String(), Module: "khtc", Calendar: "adhoc", NativeKey: "submission:" + sb.ID.String() + ":sheet:" + sealed.ID.String(), Closed: sb.Decision == "accepted", FiguresHash: &digest}}
			for _, d := range r.Indicators {
				number, valid := form.ParseNumber(sealed.Content.AdhocValues[d.Code])
				if !valid {
					return nil, nil, domain.ErrInput
				}
				key := "bo_sung:" + r.ID.String() + ":" + d.Code
				metrics = append(metrics, domain.SourceMetric{Key: key, Department: r.Department, Form: "bo_sung", Label: d.Name, Unit: d.Unit, Trace: trace, Value: sd.Value{SourceKey: key, Mode: sd.ModeSum, Sum: &number, Closed: sb.Decision == "accepted", SourceKeys: []string{r.ID.String()}}})
			}
			if sealed.Content.AdhocText != "" {
				key := "bo_sung:" + r.ID.String() + ":loi_van"
				metrics = append(metrics, domain.SourceMetric{Key: key, Department: r.Department, Form: "bo_sung", Label: "Nội dung bổ sung · " + r.Instruction, Text: sealed.Content.AdhocText, Trace: trace, Value: sd.Value{SourceKey: key, Closed: sb.Decision == "accepted", SourceKeys: []string{r.ID.String()}}})
			}
			break // a request has one immutable answer namespace
		}
	}
	return reportSupplements(rows), metrics, nil
}

// Prior plan years remain separately identified metrics. They cannot be silently
// folded into a different year's annual plan or dropped from a cross-year range.
func (s *Service) priorYearInputs(ctx context.Context, p ps.Period, sheets []domain.Submitted) ([]domain.SourceMetric, error) {
	out := []domain.SourceMetric{}
	if s.Plans == nil {
		return out, nil
	}
	for year := p.From.Year(); year < p.Year; year++ {
		cutoff := time.Date(year, 12, 31, 0, 0, 0, 0, time.UTC)
		plan, err := s.Plans.PlanAt(ctx, year, cutoff)
		if errors.Is(err, port.ErrKHTCPlanNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err = boundedPlan(plan); err != nil {
			return nil, err
		}
		period := domain.Period{ID: p.ID, Type: pd.TypeAdhoc, Year: year, From: p.From, To: p.To, Cutoff: cutoff, Departments: p.Departments}
		data, err := domain.Compose(period, plan, sheets, nil, nil, s.now())
		if err != nil {
			return nil, err
		}
		for _, row := range data.Rows {
			if row.Kind != "item" || row.SourceKey != "" {
				continue
			}
			if len(row.InPeriod) > 48 {
				return nil, domain.ErrInput
			}
			number, parseErr := decimal.NewFromString(row.InPeriod)
			if parseErr != nil || !boundedDecimal(&number) {
				return nil, domain.ErrInput
			}
			key := fmt.Sprintf("ke_hoach_nam:%d:%s", year, row.Ref)
			traces := []domain.SourceTrace{}
			for _, sb := range data.Submissions {
				if sb.Department == row.Department && !sb.To.Before(p.From) {
					digest := sb.Hash
					traces = append(traces, domain.SourceTrace{Module: "khtc", Calendar: fmt.Sprintf("plan:%d:%s", year, plan.VersionID), NativeKey: sb.SubmissionID.String(), Closed: !row.Missing, FiguresHash: &digest})
				}
			}
			out = append(out, domain.SourceMetric{Key: key, Department: row.Department, Form: "ke_hoach_nam", Label: fmt.Sprintf("%s (kế hoạch %d)", row.Name, year), Unit: row.Unit, Trace: traces, Value: sd.Value{SourceKey: key, Mode: sd.ModeSum, Sum: &number, Closed: !row.Missing, SourceKeys: []string{plan.VersionID.String()}}})
		}
	}
	return out, nil
}

// Only own-ledger reads while holding the snapshot lock; never call a port here.
func sameSupplementSnapshot(ctx context.Context, tx pgx.Tx, id uuid.UUID, data domain.Data) error {
	if data.Template != "adhoc" {
		return nil
	}
	rows, err := ps.AdhocRequests(ctx, tx, id, "")
	if err != nil {
		return err
	}
	if len(rows) == 0 && len(data.Supplements) == 0 {
		return nil
	}
	current, err := domain.Hash(reportSupplements(rows))
	if err != nil {
		return err
	}
	saved, err := domain.Hash(data.Supplements)
	if err != nil {
		return err
	}
	if current != saved {
		return ErrChanged.WithDetail("Yêu cầu bổ sung đã đổi — cập nhật bản soạn trước khi xuất")
	}
	return nil
}
