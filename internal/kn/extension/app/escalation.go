package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// escalationBatch giới hạn số nhiệm vụ xử lý trong một giao dịch.
const escalationBatch = 200

// EscalateOverdue leo thang mọi nhiệm vụ của tenant quá SLA mà chưa hoàn thành và
// chưa leo thang (US-TASK-02 AC-2) lên TTKN tỉnh: ghi sổ "escalated", đánh dấu bảng
// hiện tại và phát pf.extension.task.escalated — trong CÙNG giao dịch.
//
// Idempotent: nhiệm vụ đã leo thang có escalated_at nên lần chạy sau bỏ qua; khóa
// idem của sổ là "sla:<task>:<due_at>" nên kể cả chạy chồng cũng không ghi hai lần.
// Mở lại nhiệm vụ đặt due_at mới → có thể leo thang lại một lần cho hạn mới.
func (s *Service) EscalateOverdue(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	total := 0
	for {
		n, err := s.escalateBatch(ctx, tid, now.UTC())
		total += n
		if err != nil || n < escalationBatch || ctx.Err() != nil {
			return total, err
		}
	}
}

func (s *Service) escalateBatch(ctx context.Context, tid uuid.UUID, now time.Time) (int, error) {
	n := 0
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		tasks, err := store.OverdueUnescalated(ctx, tx, now, escalationBatch)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			at := now
			t.EscalatedAt = &at
			ev := domain.Event{
				ID: ids.New(), TenantID: tid, TaskID: t.ID, Action: domain.ActionEscalated, StatusAfter: t.Status,
				AssigneeRef: t.AssigneeRef, DueAt: t.DueAt, EscalatedUnit: t.EscalatedUnit, EscalationOrgRef: t.EscalationOrgRef,
				Note:      fmt.Sprintf("Quá SLA %s giờ chưa hoàn thành — tự động leo thang lên TTKN tỉnh", t.SLAHours.String()),
				EventTime: now, ActorRef: SystemActor,
				IdemKey: fmt.Sprintf("sla:%s:%d", t.ID, t.DueAt.Unix()),
			}
			if err := store.InsertEvent(ctx, tx, &ev); err != nil {
				return err
			}
			t.LastSeq = ev.Seq
			if err := store.UpdateTask(ctx, tx, t, ev); err != nil {
				return err
			}
			if err := enqueueEscalated(ctx, tx, t, ev, port.EscalationReasonSLA); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
		// Bản chạy khác vừa leo thang cùng nhiệm vụ: coi như đã xong.
		return 0, nil
	}
	return n, err
}

// TenantLister trả các tenant có nhiệm vụ quá hạn chưa leo thang tại thời điểm at.
type TenantLister func(ctx context.Context, at time.Time) ([]uuid.UUID, error)

// OverdueTenants dùng pool vai ag_ops_jobs (BYPASSRLS, chỉ được SELECT bốn
// cột tenant_id, status, escalated_at, due_at của task_current — migration 000006)
// để tìm tenant cần chạy; việc leo thang chạy bằng vai extension_app theo từng tenant.
func OverdueTenants(pool *pgxpool.Pool) TenantLister {
	return func(ctx context.Context, at time.Time) ([]uuid.UUID, error) {
		rows, err := pool.Query(ctx, `SELECT DISTINCT tenant_id FROM pf_extension.task_current
		  WHERE status <> 'resolved' AND escalated_at IS NULL AND due_at < $1`, at.UTC())
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if err := rows.Scan(&id); err != nil {
				return nil, err
			}
			out = append(out, id)
		}
		return out, rows.Err()
	}
}

// RunEscalation là thân job "extension.escalation": mọi tenant có nhiệm vụ quá hạn.
func (s *Service) RunEscalation(ctx context.Context, tenants TenantLister) (int, error) {
	now := s.now()
	tids, err := tenants(ctx, now)
	if err != nil {
		return 0, err
	}
	total := 0
	var errs []error
	for _, tid := range tids {
		n, err := s.EscalateOverdue(ctx, tid, now)
		total += n
		if err != nil {
			errs = append(errs, fmt.Errorf("tenant %s: %w", tid, err))
		}
	}
	return total, errors.Join(errs...)
}
