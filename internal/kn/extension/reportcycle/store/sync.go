package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SyncTask là công việc kèm con trỏ đồng bộ (KN v7.7 P7, migration 000077).
type SyncTask struct {
	Task
	SyncSeq int64
}

// TasksChangedSince trả công việc của phòng ban dept (rỗng = mọi phòng ban) trên các xã communes có sync_seq > after,
// theo thứ tự tăng, tối đa limit. withSubmission = chỉ công việc đã có bài nộp hoặc đã thu hồi (entity report_submission).
// Gồm cả công việc đã thu hồi (máy nhận tombstone).
func TasksChangedSince(ctx context.Context, tx pgx.Tx, dept string, communes []string, after int64, limit int, withSubmission bool) ([]SyncTask, error) {
	rows, err := tx.Query(ctx, `SELECT `+taskCols+`, sync_seq FROM pf_extension.report_task
	  WHERE ($1 = '' OR department_code = $1) AND commune_code = ANY($2::text[]) AND sync_seq > $3
	    AND (NOT $5 OR latest_submission_id IS NOT NULL OR withdrawn)
	  ORDER BY sync_seq LIMIT $4`, dept, communes, after, limit, withSubmission)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (SyncTask, error) {
		var t SyncTask
		err := r.Scan(&t.ID, &t.BatchID, &t.PeriodID, &t.TemplateID, &t.VersionID, &t.FormCode, &t.DepartmentCode, &t.ProvinceCode, &t.CommuneCode,
			&t.RegionCode, &t.Slot, &t.PeriodBlock, &t.DueAt, &t.Status, &t.Assignees, &t.LatestVersion, &t.LatestSubmissionID, &t.SubmittedSubmissionID,
			&t.SubmittedAt, &t.SubmittedBy, &t.OnBehalfOf, &t.FirstSubmittedAt, &t.AdjustmentReason, &t.AdjustmentAt, &t.ConfirmedBy, &t.ConfirmedAt,
			&t.LastRemindedAt, &t.ReopenedUntil, &t.Withdrawn, &t.CreatedAt, &t.UpdatedAt, &t.SyncSeq)
		return t, err
	})
}

// LayoutVersion là một phiên bản mẫu của công việc trong phạm vi, kèm created_seq lớn nhất của các công việc dùng nó.
type LayoutVersion struct {
	VersionID uuid.UUID
	Seq       int64
}

// LayoutVersionsSince trả phiên bản mẫu của các công việc (chưa thu hồi) sinh sau con trỏ after, sắp theo created_seq lớn
// nhất của phiên bản: mọi phiên bản có công việc với created_seq > con trỏ cuối đều còn được trả ở trang sau.
func LayoutVersionsSince(ctx context.Context, tx pgx.Tx, dept string, communes []string, after int64, limit int) ([]LayoutVersion, error) {
	rows, err := tx.Query(ctx, `SELECT version_id, max(created_seq) FROM pf_extension.report_task
	  WHERE ($1 = '' OR department_code = $1) AND commune_code = ANY($2::text[]) AND created_seq > $3 AND NOT withdrawn
	  GROUP BY version_id ORDER BY 2 LIMIT $4`, dept, communes, after, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LayoutVersion, error) {
		var v LayoutVersion
		err := r.Scan(&v.VersionID, &v.Seq)
		return v, err
	})
}

// BatchesByIDs đọc các đợt theo id.
func BatchesByIDs(ctx context.Context, tx pgx.Tx, ids []uuid.UUID) (map[uuid.UUID]Batch, error) {
	out := map[uuid.UUID]Batch{}
	for _, id := range ids {
		if _, ok := out[id]; ok {
			continue
		}
		b, err := BatchByID(ctx, tx, id, false)
		if err != nil {
			return nil, err
		}
		if b != nil {
			out[id] = *b
		}
	}
	return out, nil
}
