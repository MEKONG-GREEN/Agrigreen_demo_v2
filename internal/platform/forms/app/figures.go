package app

// W11.44: port.FiguresReader — số liệu kỳ native theo lô cho tổng hợp của module nghiệp vụ (KN CNTS BM-02, BM-18).
// Quyền như GET /v1/forms/periods/{id}: người gọi phải thấy kỳ (toàn quốc, Nút chủ, hoặc có đơn vị trong phạm vi);
// ngoài Nút chủ/toàn quốc thì đơn vị ngoài phạm vi bị lược.

import (
	"context"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Figures cài port.FiguresReader trên Service.
type Figures struct{ Svc *Service }

var _ port.FiguresReader = Figures{}

// Figures đọc kỳ có lịch của một mã biểu giao với [From, To] kèm bài nộp cuối và ảnh chụp mới nhất.
func (f Figures) Figures(ctx context.Context, q port.FiguresQuery) ([]port.PeriodFigures, error) {
	_, a, err := f.Svc.resolve(ctx, q.Profile)
	if err != nil {
		return nil, err
	}
	if q.TemplateCode == "" || q.To.Before(q.From) {
		return nil, problem.Validation.WithDetail("cần mã biểu và khoảng ngày hợp lệ")
	}
	var out []port.PeriodFigures
	err = f.Svc.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		ps, err := store.FigurePeriods(ctx, tx, q.Profile, q.TemplateCode, q.From, q.To, port.MaxFiguresPeriods+1)
		if err != nil {
			return err
		}
		if len(ps) > port.MaxFiguresPeriods {
			return problem.Validation.WithDetail("khoảng ngày có quá nhiều kỳ — thu hẹp khoảng")
		}
		ids := make([]uuid.UUID, 0, len(ps))
		byID := make(map[uuid.UUID]int, len(ps))
		for _, p := range ps {
			if !a.canSee(p.Period, p.Spec) {
				continue
			}
			byID[p.ID] = len(out)
			ids = append(ids, p.ID)
			out = append(out, periodFigures(p))
		}
		subs, err := store.LatestFigureSubmissions(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, s := range subs {
			pf := &out[byID[s.PeriodID]]
			pf.Submitted[s.Unit] = port.UnitFigures{Values: s.Values, NilReport: s.NilReport, Version: s.Version, Decision: s.Decision}
		}
		snaps, err := store.LatestFigureSnapshots(ctx, tx, ids)
		if err != nil {
			return err
		}
		for _, sn := range snaps {
			pf := &out[byID[sn.PeriodID]]
			at := sn.At
			pf.ClosedNo, pf.ClosedAt, pf.Closed = sn.No, &at, map[string]port.UnitFigures{}
			for _, u := range sn.Content.Units {
				if u.SubmissionID == nil {
					continue
				}
				pf.Closed[u.Unit] = port.UnitFigures{Values: u.Values, NilReport: u.NilReport, Version: u.Version}
			}
		}
		// Lược đơn vị ngoài phạm vi (người gọi không phải Nút chủ/toàn quốc).
		for _, p := range ps {
			j, ok := byID[p.ID]
			if !ok || (a.targetFilter == nil && (a.scope.All || a.owns(p.Period))) {
				continue
			}
			pf := &out[j]
			pf.Units = slices.DeleteFunc(pf.Units, func(u string) bool { return !a.allowsUnit(p.Spec, p.Units, u) })
			if a.targetFilter != nil {
				for u := range pf.Parents {
					if !a.allowsUnit(p.Spec, p.Units, u) {
						delete(pf.Parents, u)
					}
				}
			}
			for _, m := range []map[string]port.UnitFigures{pf.Submitted, pf.Closed} {
				for u := range m {
					if !a.allowsUnit(p.Spec, p.Units, u) {
						delete(m, u)
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, classify(err)
	}
	return out, nil
}

func periodFigures(p store.FigurePeriod) port.PeriodFigures {
	out := port.PeriodFigures{ID: p.ID, TemplateCode: p.TemplateCode, Label: p.Label, OwnerRef: p.OwnerRef, UnitKind: p.Spec.UnitKind,
		Units: slices.Clone(p.Units), Kind: p.Spec.PeriodKind, Key: p.Spec.PeriodKey, Rule: p.Spec.PeriodRule,
		Submitted: map[string]port.UnitFigures{}}
	if p.Spec.From != nil {
		out.From = *p.Spec.From
	}
	if p.Spec.To != nil {
		out.To = *p.Spec.To
	}
	if len(p.Spec.UnitParents) == len(p.Units) {
		out.Parents = make(map[string]string, len(p.Units))
		for i, u := range p.Units {
			out.Parents[u] = p.Spec.UnitParents[i]
		}
	}
	switch p.LastAction {
	case "closed":
		out.Status = domain.PeriodClosed
	case "reopened":
		out.Status = domain.PeriodReopened
	default:
		out.Status = domain.PeriodOpen
	}
	return out
}
