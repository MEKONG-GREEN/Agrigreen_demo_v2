package app

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// AvailableForms trả mẫu đã gán cho loại báo cáo của phòng ban (cấp xã) với phiên bản áp dụng tại from (Bước 1).
func (s *Service) AvailableForms(ctx context.Context, reportType, from string) ([]repport.FormVersion, error) {
	a, err := s.knActor(ctx, port.SessionRoleProvince, port.SessionRoleNational, port.SessionRoleAdmin)
	if err != nil {
		return nil, err
	}
	if s.Forms == nil {
		return nil, ErrDependency.WithDetail("chưa nối engine mẫu báo cáo (pf/reports)")
	}
	if _, ok := domain.ReportPeriodType[reportType]; !ok {
		return nil, problem.Validation.WithField("report_type", "weekly | monthly | seasonal | annual | adhoc")
	}
	at := s.now()
	if from != "" {
		if at, err = domain.ParseDate(from); err != nil {
			return nil, problem.Validation.WithField("from_date", err.Error())
		}
	}
	dept := a.k.Department
	if dept == "" {
		dept = "trong_trot"
	}
	return s.Forms.AssignedForms(ctx, a.p.TenantID, dept, repport.LevelCommune, reportType, at)
}

// PeriodQuery lọc kỳ cho bộ lọc dashboard/báo cáo.
type PeriodQuery struct {
	Province   string
	ReportType string
	BatchID    *uuid.UUID
	Limit      int
}

// ListPeriods trả kỳ của đợt đã phát hành trong phạm vi (mới nhất trước).
func (s *Service) ListPeriods(ctx context.Context, q PeriodQuery) ([]store.Period, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return nil, err
	}
	f := store.PeriodFilter{Department: a.dept(), ReportType: q.ReportType, BatchID: q.BatchID, Limit: clampLimit(q.Limit)}
	switch {
	case a.province != "":
		f.Province = a.province
	case q.Province != "":
		f.Province = q.Province
	default:
		f.AllProvinces = true
	}
	var out []store.Period
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.ListPeriods(ctx, tx, f)
		return err
	})
	return out, err
}

// Khóa số đếm menu (khớp party/domain Badge* — menu kn.menu/v2 của P2).
const (
	BadgeReportTasksTodo    = "extension.report_tasks.todo"
	BadgeSubmissionStatus   = "extension.report_status.not_submitted"
	BadgeReviewPending      = "extension.submissions.pending_review"
	BadgeAdHocReportsOpen   = "extension.report_tasks.ad_hoc_todo"
	BadgeAdHocReportsStatus = "extension.report_status.ad_hoc_pending"
)

// Badges trả số đếm menu "việc cần xử lý" của ngữ cảnh (ẩn khi 0 ở client): Cấp Xã — công việc còn phải gửi; Cấp
// Tỉnh — xã chưa nộp đã quá hạn, bài nộp chờ rà soát; tách báo cáo đột xuất.
func (s *Service) Badges(ctx context.Context) (map[string]int, error) {
	a, err := s.knActor(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int{BadgeReportTasksTodo: 0, BadgeSubmissionStatus: 0, BadgeReviewPending: 0, BadgeAdHocReportsOpen: 0, BadgeAdHocReportsStatus: 0}
	var communes []string
	if a.k.Role == port.SessionRoleCommune {
		communes = append([]string{}, a.k.Communes...)
	}
	now := s.now()
	err = s.tx(ctx, a.p.TenantID, func(tx pgx.Tx) error {
		cs, err := store.CountTasks(ctx, tx, a.dept(), a.province, communes, now)
		if err != nil {
			return err
		}
		for _, c := range cs {
			if c.Closed {
				continue
			}
			pending := domain.TaskState{Status: c.Status}.Pending()
			switch a.k.Role {
			case port.SessionRoleCommune:
				if pending && c.Started {
					if c.Adhoc {
						out[BadgeAdHocReportsOpen] += c.N
					} else {
						out[BadgeReportTasksTodo] += c.N
					}
				}
			default:
				if pending && c.Overdue {
					if c.Adhoc {
						out[BadgeAdHocReportsStatus] += c.N
					} else {
						out[BadgeSubmissionStatus] += c.N
					}
				}
				if c.Status == domain.TaskSubmitted {
					out[BadgeReviewPending] += c.N
				}
			}
		}
		return nil
	})
	return out, err
}

// Now trả thời điểm hiện tại của dịch vụ (handler dùng để suy trạng thái hiển thị).
func (s *Service) Now() time.Time { return s.now() }
