package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/render"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Render dựng tệp của một định dạng từ tập dữ liệu (xuất khẩu để kiểm thử dùng lại).
func Render(ds domain.Dataset, format string, j domain.Job) ([]byte, error) {
	if format == domain.FormatPDF {
		return render.PDF(ds, j.RequestedAt, render.PDFOptions{})
	}
	return render.XLSX(ds, j.RequestedAt)
}

// HandleExportRequested là inbox.Handler của worker cho pf.reports.export.requested
// (consumer port.ExportConsumer, pool vai reports_app). Chạy trong giao dịch do
// inbox.Process mở nên kết quả + inbox + outbox commit cùng nhau (R-61). Việc xuất
// đã xong/hỏng → không làm gì (giao lặp an toàn, một tệp cho mỗi việc xuất).
func (s *Service) HandleExportRequested(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var pl port.ExportRequestedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("reports: payload %s hỏng: %w", env.EventType, err)
	}
	id, err := uuid.Parse(pl.JobID)
	if err != nil {
		return fmt.Errorf("reports: job_id không hợp lệ: %w", err)
	}
	j, err := store.JobByID(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if j == nil || j.Status != domain.JobQueued {
		return nil
	}
	if _, _, policyErr := s.sourceJobPolicyState(*j); policyErr != nil {
		return store.FailJob(ctx, tx, j.ID, sourceJobInvalidCode, s.now())
	}
	if policy, ok := s.Builders.jobPolicy(j.Def.Source); ok || j.SourceArgs != nil {
		at, parseErr := time.Parse(time.RFC3339Nano, pl.RequestedAt)
		if policy.Version < 1 || parseErr != nil || env.TenantID != j.TenantID || pl.RequestedBy != j.RequestedBy.String() ||
			pl.TemplateKey != j.TemplateKey || pl.Format != j.Format || !at.Equal(j.RequestedAt) {
			return store.FailJob(ctx, tx, j.ID, sourceJobInvalidCode, s.now())
		}
	}
	if s.Store == nil {
		return ErrDependency.WithDetail("chưa cấu hình kho đối tượng")
	}
	lg := s.log().With("event", "reports.export.render", "tenant_id", j.TenantID, "job_id", j.ID, "template_key", j.TemplateKey, "format", j.Format)
	ds, err := s.buildDataset(ctx, *j)
	var p *problem.Problem
	if errors.As(err, &p) && p.Status < 500 {
		// Lỗi nghiệp vụ vĩnh viễn (vd chưa có kết quả cân đối của vụ): đánh dấu hỏng, không giao lại.
		lg.Warn("việc xuất hỏng", "error_code", p.Code, "outcome", "rejected")
		return store.FailJob(ctx, tx, j.ID, p.Code, s.now())
	}
	if err != nil {
		return err
	}
	body, err := Render(ds, j.Format, *j)
	if err != nil {
		return err
	}
	sha := objstore.SHA256Hex(body)
	key := domain.ObjectKey(j.TenantID, j.ID, j.Format)
	if _, err := s.Store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), sha, domain.ContentType(j.Format)); err != nil {
		if errors.Is(err, objstore.ErrExists) {
			// Đã có tệp khác nội dung dưới cùng khóa (dữ liệu đổi giữa hai lần giao) — không ghi đè.
			return store.FailJob(ctx, tx, j.ID, "reports.object_conflict", s.now())
		}
		return err
	}
	now := s.now()
	if err := store.CompleteJob(ctx, tx, j.ID, key, sha, int64(len(body)), len(ds.Rows), ds.Provisional, now); err != nil {
		return err
	}
	lg.Info("đã dựng tệp báo cáo", "rows", len(ds.Rows), "size_bytes", len(body), "provisional", ds.Provisional, "outcome", "ok")
	var sched *string
	if j.ScheduleID != nil {
		v := j.ScheduleID.String()
		sched = &v
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: j.TenantID, Type: port.EventExportCompleted, SchemaVersion: 1, OccurredAt: now,
		TraceID: env.TraceID, Producer: port.Producer,
		Payload: port.ExportCompletedV1{
			JobID: j.ID.String(), TemplateKey: j.TemplateKey, Format: j.Format, ObjectKey: key, SHA256: sha, SizeBytes: int64(len(body)),
			RowCount: len(ds.Rows), Provisional: ds.Provisional, ScheduleID: sched, CompletedAt: ts(now),
		},
	})
}
