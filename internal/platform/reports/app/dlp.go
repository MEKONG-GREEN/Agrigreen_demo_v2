package app

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Loại đường xuất qua điểm kiểm soát.
const (
	EgressExport    = "export"    // yêu cầu xuất qua API
	EgressScheduled = "scheduled" // xuất theo lịch gửi
	EgressDownload  = "download"  // cấp URL tải tệp
)

// Egress là một lần dữ liệu rời hệ thống qua đường xuất báo cáo.
type Egress struct {
	Kind        string
	TenantID    uuid.UUID
	ActorRef    uuid.UUID
	TemplateKey string
	Format      string
	JobID       uuid.UUID
}

// egress là ĐIỂM KIỂM SOÁT DUY NHẤT của mọi đường xuất báo cáo (mục 18 "Phòng
// chống thất thoát dữ liệu"): yêu cầu xuất, xuất theo lịch, cấp URL tải đều gọi
// hàm này trong cùng giao dịch với thao tác.
//
//   - Nhật ký: một dòng pf_reports.export_audit + một dòng log có cấu trúc
//     (event=reports.egress) — chỉ mã tham chiếu, không dữ liệu cá nhân (R-14).
//   - Hạn mức: số lần xuất (API + lịch) của một người trong ngày (giờ Việt Nam)
//     không vượt reports.daily_export_quota (mặc định 50) → 429.
//   - Cảnh báo: lần xuất vượt reports.export_anomaly_threshold (mặc định 30) được
//     đánh dấu; lần đầu vượt trong ngày phát pf.reports.export.anomaly v1.
//
// Đếm được tuần tự hóa bằng khóa tư vấn theo (tenant, người, ngày).
//
// G5 (ADR-0024 mục 3): sau khi điểm kiểm soát cục bộ cho qua (hạn mức người/ngày), lần xuất đi tiếp
// qua điểm kiểm soát DÙNG CHUNG của pf/metering (kênh report_export: hạn mức theo tenant/kênh/bên
// nhận, nhật ký thống nhất) TRƯỚC khi ghi export_audit — enforce + từ chối → không có dòng nhật ký
// cục bộ, thao tác không xảy ra. Mã reports.export_quota_exceeded và sự kiện pf.reports.export.anomaly
// giữ nguyên; checkpoint nil → hành vi cũ.
func (s *Service) egress(ctx context.Context, tx pgx.Tx, e Egress) error {
	now := s.now()
	day := clock.BusinessDate(now).String()
	lg := s.log().With("event", "reports.egress", "kind", e.Kind, "tenant_id", e.TenantID, "actor_ref", e.ActorRef,
		"template_key", e.TemplateKey, "format", e.Format, "job_id", e.JobID, "business_date", day, "trace_id", telemetry.TraceID(ctx))
	if err := store.LockActorDay(ctx, tx, e.TenantID, e.ActorRef, day); err != nil {
		return err
	}
	count, err := store.CountExports(ctx, tx, e.ActorRef, day)
	if err != nil {
		return err
	}
	row := store.AuditRow{ID: ids.New(), Kind: e.Kind, ActorRef: e.ActorRef, TemplateKey: e.TemplateKey, Format: e.Format,
		JobID: e.JobID, Day: day, DailyCount: count, EventTime: now}
	var threshold, quota int
	if e.Kind != EgressDownload {
		if quota, err = s.paramInt(ctx, port.ParamDailyExportQuota, now, DefaultDailyQuota); err != nil {
			return err
		}
		if threshold, err = s.paramInt(ctx, port.ParamAnomalyThreshold, now, DefaultAnomalyThreshold); err != nil {
			return err
		}
		if count >= quota {
			lg.Warn("từ chối xuất: vượt hạn mức ngày", "daily_count", count, "quota", quota, "outcome", "rejected")
			return ErrQuotaExceeded.WithDetail("Hạn mức " + strconv.Itoa(quota) + " lần xuất/ngày; thử lại vào ngày mai hoặc liên hệ quản trị")
		}
		row.DailyCount = count + 1
		row.Anomaly = row.DailyCount > threshold
	}
	if err := meteringport.Guard(ctx, s.Egress, s.EgressMode, meteringport.Egress{
		SourceID: row.ID, Channel: meteringport.ChannelReportExport, DataTenantID: e.TenantID,
		CounterpartyKind: meteringport.CounterpartyActor, CounterpartyRef: e.ActorRef, Product: e.TemplateKey,
		Records: 1, OccurredAt: now, ActorRef: e.ActorRef, TraceID: telemetry.TraceID(ctx),
	}, s.log()); err != nil {
		lg.Warn("điểm kiểm soát xuất dùng chung từ chối", "outcome", "egress_denied")
		return err
	}
	if err := store.InsertAudit(ctx, tx, e.TenantID, row); err != nil {
		return err
	}
	if row.Anomaly {
		lg.Warn("khối lượng xuất bất thường", "daily_count", row.DailyCount, "threshold", threshold, "outcome", "anomaly")
		if row.DailyCount == threshold+1 { // lần đầu vượt ngưỡng trong ngày: một cảnh báo
			if err := outbox.Enqueue(ctx, tx, outbox.Event{
				ID: ids.New(), TenantID: e.TenantID, Type: port.EventExportAnomaly, SchemaVersion: 1, OccurredAt: now,
				Producer: port.Producer,
				Payload: port.ExportAnomalyV1{
					ActorRef: e.ActorRef.String(), BusinessDate: day, DailyCount: row.DailyCount, Threshold: threshold, Quota: quota,
					LastJobID: e.JobID.String(), TemplateKey: e.TemplateKey, Kind: e.Kind, DetectedAt: ts(now),
				},
			}); err != nil {
				return err
			}
		}
		return nil
	}
	lg.Info("xuất dữ liệu báo cáo", "daily_count", row.DailyCount, "outcome", "ok")
	return nil
}
