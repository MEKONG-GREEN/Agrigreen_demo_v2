package app

import (
	"bytes"
	"context"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/store"
	pd "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SubmissionHistory contains native identities, not a reconstruction from the
// current form or a list of sealed rows (which may include failed submissions).
type SubmissionHistory struct {
	Period     ps.Period
	Department string
	Items      []forms.SubmissionRef
}

type HistoryRow struct {
	EntryRef uuid.UUID
	Name     string
	Unit     string
}

// SubmissionSnapshot exposes only the selected submitted payload. Calculated
// cells, current source values and current validation are deliberately absent:
// the sealed sheet does not record their historical inputs/results.
type SubmissionSnapshot struct {
	Period     ps.Period
	Department string
	Submission forms.SubmissionRef
	Plan       *ext.KHTCPlan
	Sheet      store.Sheet
	Rows       []HistoryRow
	Sections   []domain.Section
}

func (s *Service) historyContext(ctx context.Context, id uuid.UUID, dept string) (caller, ps.Period, []forms.SubmissionRef, error) {
	c, err := s.caller(ctx, dept)
	if err != nil {
		return caller{}, ps.Period{}, nil, err
	}
	// National users read exported reports, never department submission payloads.
	if c.k.Role == ext.SessionRoleNational {
		return c, ps.Period{}, nil, errNotDept
	}
	p, events, err := s.periodCtx(ctx, c, id, dept)
	if err != nil {
		return c, ps.Period{}, nil, err
	}
	if terminalOf(events) == pd.PeriodCancelled && c.member {
		return c, ps.Period{}, nil, ErrFormNotFound
	}
	if p.Type == pd.TypeAdhoc {
		if c.k.Role == ext.SessionRoleAdmin && !c.p.MFA {
			return c, ps.Period{}, nil, problem.MFARequired
		}
		if c.k.ReadOnly() {
			return c, ps.Period{}, nil, errNotDept
		}
	}
	// A port call must finish outside the extension transaction, including with
	// a one-connection pool. The native port rechecks its own profile/unit scope.
	subs, err := s.Entry.Submissions(ctx, pd.ProfileKHTC, p.FormsPeriodRef, dept)
	if err != nil {
		return c, ps.Period{}, nil, err
	}
	ids, versions := map[uuid.UUID]bool{}, map[int64]bool{}
	for _, sub := range subs {
		if sub.ID == uuid.Nil || sub.Unit != dept || sub.Version < 1 || sub.Submitter == uuid.Nil ||
			sub.SubmittedAt.IsZero() || ids[sub.ID] || versions[sub.Version] ||
			sub.Decision != "" && sub.Decision != DecisionAccepted && sub.Decision != DecisionReturned ||
			sub.Decision == "" && (sub.Reviewer != nil || sub.ReviewedAt != nil || sub.ReviewNote != "") ||
			sub.Decision != "" && (sub.Reviewer == nil || *sub.Reviewer == uuid.Nil || sub.ReviewedAt == nil || sub.ReviewedAt.IsZero()) {
			return c, ps.Period{}, nil, ErrDependency.WithDetail("Chưa đối chiếu được định danh lịch sử gửi")
		}
		ids[sub.ID], versions[sub.Version] = true, true
	}
	return c, p, subs, nil
}

func (s *Service) History(ctx context.Context, id uuid.UUID, dept string) (SubmissionHistory, error) {
	_, p, subs, err := s.historyContext(ctx, id, dept)
	if err != nil {
		return SubmissionHistory{}, err
	}
	return SubmissionHistory{Period: p, Department: dept, Items: subs}, nil
}

func (s *Service) HistoricalSubmission(ctx context.Context, id uuid.UUID, dept string, submissionID uuid.UUID) (SubmissionSnapshot, error) {
	c, p, subs, err := s.historyContext(ctx, id, dept)
	if err != nil {
		return SubmissionSnapshot{}, err
	}
	for _, sub := range subs {
		if sub.ID != submissionID {
			continue
		}
		ref := sheetRef(sub.Values)
		if ref == nil || *ref == uuid.Nil {
			return SubmissionSnapshot{}, ErrFormNotFound
		}
		var sealed *store.Sheet
		err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
			var e error
			sealed, e = store.ByID(ctx, tx, *ref)
			return e
		})
		if err != nil {
			return SubmissionSnapshot{}, err
		}
		if sealed == nil || sealed.Kind != store.KindSealed || sealed.PeriodID != id ||
			sealed.FormsPeriodRef != p.FormsPeriodRef || sealed.Department != dept || sealed.Year != p.Year ||
			int64(sealed.BaseAttempt) != sub.Version-1 || sealed.ActorRef != sub.Submitter {
			return SubmissionSnapshot{}, ErrFormNotFound
		}
		_, hash, err := sealed.Content.Hash()
		if err != nil || !bytes.Equal(hash, sealed.ContentHash) {
			return SubmissionSnapshot{}, ErrDependency.WithDetail("Không xác minh được nội dung đã niêm phong")
		}
		template := domain.TemplateFor(dept)
		if template.Code != sealed.TemplateCode || template.Version != sealed.TemplateVersion {
			return SubmissionSnapshot{}, ErrDependency.WithDetail("Chưa có định nghĩa đúng phiên bản mẫu đã gửi")
		}
		out := SubmissionSnapshot{Period: p, Department: dept, Submission: sub, Sheet: *sealed,
			Rows: []HistoryRow{}, Sections: template.Sections}
		if sealed.PlanVersionRef != nil {
			versions, ok := s.Plans.(ext.KHTCPlanVersions)
			if !ok || *sealed.PlanVersionRef == uuid.Nil {
				return SubmissionSnapshot{}, ErrDependency.WithDetail("Chưa có cổng đọc đúng phiên bản kế hoạch đã gửi")
			}
			// Resolve the recorded identity, never PlanAt (which can now point
			// at another version). This port call is also outside any DB TX.
			plan, e := versions.PlanVersion(ctx, sealed.Year, *sealed.PlanVersionRef, p.Cutoff)
			if e != nil {
				return SubmissionSnapshot{}, ErrDependency.WithCause(e)
			}
			if plan.VersionID != *sealed.PlanVersionRef || plan.Year != sealed.Year {
				return SubmissionSnapshot{}, ErrDependency.WithDetail("Không đối chiếu được phiên bản kế hoạch đã gửi")
			}
			out.Plan = &plan
			for _, row := range plan.Entries {
				if row.Kind == "item" && row.Department == dept {
					out.Rows = append(out.Rows, HistoryRow{EntryRef: row.Ref, Name: row.Name, Unit: row.UnitCode})
				}
			}
		}
		return out, nil
	}
	return SubmissionSnapshot{}, ErrFormNotFound
}
