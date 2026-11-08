package app

import (
	"context"
	"slices"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	ext "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PendingReviewRow struct {
	PeriodID       uuid.UUID `json:"period_id"`
	PeriodName     string    `json:"period_name"`
	Department     string    `json:"department"`
	DepartmentName string    `json:"department_name"`
	SubmissionID   uuid.UUID `json:"submission_id"`
	Attempt        int64     `json:"attempt"`
	SubmittedAt    time.Time `json:"submitted_at"`
}

type PendingReviewPage struct {
	Rows       []PendingReviewRow `json:"rows"`
	NextCursor *uuid.UUID         `json:"next_cursor"`
}

type PendingReviewQuery struct {
	Before, PeriodID uuid.UUID
	Department       string
	ReportType       string // regular (default) | adhoc
}

// PendingReview preserves the regular inbox's default contract.
func (s *Service) PendingReview(ctx context.Context, before uuid.UUID) (PendingReviewPage, error) {
	return s.PendingReviewFiltered(ctx, PendingReviewQuery{Before: before})
}

// PendingReviewFiltered reads a bounded group of nonterminal KHTC periods.
// It is independent of draft/sealed sheets, plans and mutation-intent state.
// Native Progress keeps the actual caller and runs after the KN transaction.
// This is a page, never a global count or proof that a review will be authorized.
func (s *Service) PendingReviewFiltered(ctx context.Context, q PendingReviewQuery) (PendingReviewPage, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return PendingReviewPage{}, err
	}
	if c.k.Role == ext.SessionRoleNational {
		return PendingReviewPage{}, problem.Forbidden
	}
	if q.ReportType == "" {
		q.ReportType = "regular"
	}
	if q.ReportType != "regular" && q.ReportType != domain.TypeAdhoc {
		return PendingReviewPage{}, problem.BadRequest.WithField("report_type", "regular hoặc adhoc")
	}
	if q.Department != "" && (len(q.Department) > 40 || !deptRe.MatchString(q.Department)) {
		return PendingReviewPage{}, problem.BadRequest.WithField("department", "mã phòng không hợp lệ")
	}
	adhoc := q.ReportType == domain.TypeAdhoc
	if !c.k.ExpiresAt.IsZero() && !c.k.ExpiresAt.After(s.now()) {
		return PendingReviewPage{}, problem.Forbidden.WithDetail("Ngữ cảnh phiên đã hết hạn")
	}
	if adhoc && !adhocReader(c) {
		if c.k.Role == ext.SessionRoleAdmin && !c.p.MFA {
			return PendingReviewPage{}, problem.MFARequired
		}
		return PendingReviewPage{}, problem.Forbidden
	}
	var periods []store.Period
	var requests map[uuid.UUID]map[string]*store.AdhocRequest
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		periods, e = store.PendingReviewPeriods(ctx, tx, q.Before, q.PeriodID, q.Department, adhoc)
		if e != nil || !adhoc {
			return e
		}
		ids := make([]uuid.UUID, 0, min(len(periods), store.PendingReviewPeriodPage))
		for _, p := range periods[:min(len(periods), store.PendingReviewPeriodPage)] {
			ids = append(ids, p.ID)
		}
		requests, e = store.ActiveAdhocRequestsForPeriods(ctx, tx, ids)
		return e
	})
	if err != nil {
		return PendingReviewPage{}, err
	}
	out := PendingReviewPage{Rows: []PendingReviewRow{}}
	if len(periods) > store.PendingReviewPeriodPage {
		periods = periods[:store.PendingReviewPeriodPage]
		id := periods[len(periods)-1].ID
		out.NextCursor = &id
	}
	names, _, _ := s.unitDepartments(ctx, c.k)
	for _, p := range periods {
		progress, e := s.Control.Progress(ctx, domain.ProfileKHTC, p.FormsPeriodRef)
		if e != nil {
			return PendingReviewPage{}, e
		}
		if progress.ID != p.FormsPeriodRef || progress.Profile != domain.ProfileKHTC {
			return PendingReviewPage{}, ErrDependency.WithDetail("Không đối chiếu được kỳ native của hộp phiếu")
		}
		switch progress.Status {
		case "closed", "cancelled":
			continue
		case "open", "reopened":
		default:
			return PendingReviewPage{}, ErrDependency.WithDetail("Không đọc được trạng thái kỳ native của hộp phiếu")
		}
		seen := map[string]bool{}
		for _, unit := range progress.Units {
			if !slices.Contains(p.Departments, unit.Unit) {
				continue
			}
			if seen[unit.Unit] || unit.Versions < 0 || (unit.Decision != "" && unit.Decision != "accepted" && unit.Decision != "returned") {
				return PendingReviewPage{}, ErrDependency.WithDetail("Tiến độ phiếu native không nhất quán")
			}
			seen[unit.Unit] = true
			if q.Department != "" && unit.Unit != q.Department {
				continue
			}
			if adhoc {
				switch unit.Status {
				case "not_submitted", "overdue", "submitted", "accepted", "returned", "missed", "closed_unadjusted", "awaiting_reopen":
				default:
					return PendingReviewPage{}, ErrDependency.WithDetail("Không đọc được trạng thái phiếu native")
				}
				if (unit.Versions == 0) != (unit.LastSubmissionID == nil) || (unit.Versions == 0) != (unit.LastSubmittedAt == nil) {
					return PendingReviewPage{}, ErrDependency.WithDetail("Số lần gửi và định danh native không nhất quán")
				}
			}
			if unit.Versions == 0 || unit.Decision != "" || unit.Status != "submitted" {
				continue
			}
			if unit.LastSubmissionID == nil || *unit.LastSubmissionID == uuid.Nil || unit.LastSubmittedAt == nil || unit.LastSubmittedAt.IsZero() {
				return PendingReviewPage{}, ErrDependency.WithDetail("Thiếu định danh lần gửi native của hộp phiếu")
			}
			if request := requests[p.ID][unit.Unit]; request != nil {
				// An in-flight request is never evidence that the latest native
				// submission answers it. Do not turn uncertain delivery into an
				// empty page or fall back to an older submission.
				if request.State != "fulfilled" || request.NativeSubmissionRef == nil || *request.NativeSubmissionRef != *unit.LastSubmissionID || unit.Versions <= request.BaseAttempt {
					return PendingReviewPage{}, ErrDependency.WithDetail("Chưa đối chiếu được lần gửi native với yêu cầu bổ sung")
				}
			}
			out.Rows = append(out.Rows, PendingReviewRow{PeriodID: p.ID, PeriodName: p.Name, Department: unit.Unit,
				DepartmentName: names[unit.Unit], SubmissionID: *unit.LastSubmissionID, Attempt: unit.Versions, SubmittedAt: *unit.LastSubmittedAt})
		}
	}
	return out, nil
}
