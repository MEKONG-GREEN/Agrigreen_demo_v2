package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// StaffSupportQuery là tham số liệt kê yêu cầu hỗ trợ cho cán bộ.
type StaffSupportQuery struct {
	TaskStatus  string
	Category    string
	Urgency     string
	CommuneCode string
	Cooperative uuid.UUID
	GPSMissing  *bool
	Before      uuid.UUID
	Limit       int
}

// StaffSupportItem là một yêu cầu kèm nhiệm vụ tự sinh hiện hành.
type StaffSupportItem struct {
	Request domain.SupportRequest
	Task    domain.Task
}

// StaffSupportPage là một trang yêu cầu hỗ trợ cho cán bộ.
type StaffSupportPage struct {
	Items []StaffSupportItem
	Next  *uuid.UUID
}

// ListSupportForStaff liệt kê yêu cầu hỗ trợ của nông dân cho cán bộ (HTX US-SUPPORT-01:
// quản lý HTX biết để hỗ trợ kịp thời; KN US-TASK-01/US-TASK-03: cán bộ khuyến nông giám
// sát yêu cầu và nhiệm vụ tự sinh). Phạm vi như danh sách nhiệm vụ: quản lý HTX chỉ HTX
// của mình (Principal.OrgRef) trong phạm vi; cán bộ KN theo xã/tỉnh phụ trách hoặc
// nhiệm vụ được giao cho mình; platform_admin mọi nơi. Mới nhất trước, cursor = id.
func (s *Service) ListSupportForStaff(ctx context.Context, q StaffSupportQuery) (StaffSupportPage, error) {
	p, err := authz.Require(ctx, taskReaders...)
	if err != nil {
		return StaffSupportPage{}, err
	}
	if q.Limit <= 0 || q.Limit > 200 {
		q.Limit = 50
	}
	switch {
	case q.TaskStatus != "" && !validStatus(q.TaskStatus):
		return StaffSupportPage{}, problem.Validation.WithField("task_status", "không hợp lệ")
	case q.Category != "" && !domain.ValidSupportCategory(q.Category):
		return StaffSupportPage{}, problem.Validation.WithField("category", "không hợp lệ")
	case q.Urgency != "" && !domain.ValidSupportUrgency(q.Urgency):
		return StaffSupportPage{}, problem.Validation.WithField("urgency", "phải là binh_thuong hoặc khan")
	}
	sc, err := s.scope(ctx, p)
	if err != nil {
		return StaffSupportPage{}, err
	}
	f := store.SupportFilter{
		All: sc.All, Units: sc.Units, Actor: p.ActorRef, Cooperative: q.Cooperative, TaskStatus: q.TaskStatus,
		Category: q.Category, Urgency: q.Urgency, CommuneCode: q.CommuneCode, GPSMissing: q.GPSMissing, Before: q.Before,
		Limit: q.Limit + 1,
	}
	empty := StaffSupportPage{Items: []StaffSupportItem{}}
	if onlyCoopManager(p) {
		if q.Cooperative != uuid.Nil && q.Cooperative != p.OrgRef {
			return StaffSupportPage{}, ErrOutOfScope.WithDetail("quản lý HTX chỉ xem yêu cầu hỗ trợ của HTX mình")
		}
		if p.OrgRef == uuid.Nil || sc.Empty() {
			return empty, nil
		}
		// Quản lý HTX: chỉ HTX mình, không mở rộng theo người được giao.
		f.Cooperative, f.Actor = p.OrgRef, uuid.Nil
	}
	var rows []domain.SupportRequest
	tasks := map[uuid.UUID]domain.Task{}
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		if rows, e = store.SupportForStaff(ctx, tx, f); e != nil {
			return e
		}
		taskIDs := make([]uuid.UUID, 0, len(rows))
		for _, r := range rows {
			taskIDs = append(taskIDs, r.TaskID)
		}
		tasks, e = store.TasksByIDs(ctx, tx, taskIDs)
		return e
	})
	if err != nil {
		return StaffSupportPage{}, err
	}
	pg := empty
	for i, r := range rows {
		if i == q.Limit {
			last := rows[q.Limit-1].ID
			pg.Next = &last
			break
		}
		pg.Items = append(pg.Items, StaffSupportItem{Request: r, Task: tasks[r.TaskID]})
	}
	return pg, nil
}

// SupportTasks trả các nhiệm vụ của một trang (để tra tên hiển thị).
func (pg StaffSupportPage) SupportTasks() []domain.Task {
	out := make([]domain.Task, 0, len(pg.Items))
	for _, it := range pg.Items {
		out = append(out, it.Task)
	}
	return out
}
