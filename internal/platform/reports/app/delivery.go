package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// Trạng thái gửi email của một lần chạy lịch.
const (
	DeliverySent    = "sent"
	DeliverySkipped = "skipped"
)

// HandleExportCompleted là inbox.Handler cho pf.reports.export.completed
// (consumer port.DeliveryConsumer): việc xuất theo lịch xong → gửi email tới
// người nhận với liên kết tải ngắn hạn (không đính kèm tệp). Email tra lúc gửi
// qua MailDirectory — thiếu cổng này (pf/accounts chưa nối) hoặc thiếu SMTP →
// bỏ gửi kèm cảnh báo, ghi trạng thái "skipped". Giao lặp: lần chạy đã có kết
// quả gửi → không gửi lại.
func (s *Service) HandleExportCompleted(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var pl port.ExportCompletedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("reports: payload %s hỏng: %w", env.EventType, err)
	}
	if pl.ScheduleID == nil {
		return nil // xuất theo yêu cầu: người yêu cầu tự tải qua API
	}
	id, err := uuid.Parse(pl.JobID)
	if err != nil {
		return fmt.Errorf("reports: job_id không hợp lệ: %w", err)
	}
	j, err := store.JobByID(ctx, tx, id, false)
	if err != nil || j == nil || j.Status != domain.JobCompleted || j.ScheduleID == nil {
		return err
	}
	run, err := store.RunByJob(ctx, tx, id)
	if err != nil || run == nil || run.DeliveryStatus != nil {
		return err
	}
	sch, err := store.ScheduleByID(ctx, tx, *j.ScheduleID, false)
	if err != nil || sch == nil {
		return err
	}
	lg := s.log().With("event", "reports.schedule.deliver", "tenant_id", j.TenantID, "schedule_id", sch.ID, "job_id", j.ID, "slot", run.SlotKey)
	mark := func(status, note string, sent int) error {
		return store.MarkDelivered(ctx, tx, sch.ID, run.SlotKey, status, note, sent, s.now())
	}
	// A source enabled after an old scheduled job completed must not deliver
	// its pending artifact without the unsupported per-run owner authorization.
	if _, enabled := s.Builders.jobPolicy(j.Def.Source); enabled || j.SourceArgs != nil {
		return mark(DeliverySkipped, "nguồn báo cáo chưa hỗ trợ lịch gửi", 0)
	}
	switch {
	case s.Directory == nil:
		lg.Warn("bỏ gửi email: chưa nối danh bạ email (pf/accounts)", "outcome", "skipped")
		return mark(DeliverySkipped, "chưa nối danh bạ email của tài khoản (pf/accounts)", 0)
	case s.Mailer == nil:
		lg.Warn("bỏ gửi email: chưa cấu hình SMTP (AG_SMTP_*)", "outcome", "skipped")
		return mark(DeliverySkipped, "chưa cấu hình máy chủ thư (AG_SMTP_*)", 0)
	case s.Store == nil || j.ObjectKey == nil:
		lg.Warn("bỏ gửi email: chưa cấu hình kho đối tượng", "outcome", "skipped")
		return mark(DeliverySkipped, "chưa cấu hình kho đối tượng", 0)
	}
	url, err := s.Store.PresignGet(ctx, *j.ObjectKey, objstore.MaxPresignTTL)
	if err != nil {
		return err
	}
	msg := s.deliveryMessage(*j, url)
	sent, missing := 0, 0
	for _, r := range sch.Recipients {
		addr, err := s.Directory.EmailOf(ctx, j.TenantID, r)
		if errors.Is(err, accountsport.ErrNoEmail) || (err == nil && strings.TrimSpace(addr) == "") {
			missing++
			continue
		}
		if err != nil {
			return err // lỗi tạm thời: giao lại
		}
		msg.To = addr
		if err := s.Mailer.Send(ctx, msg); err != nil {
			return err
		}
		sent++
	}
	note := ""
	if missing > 0 {
		note = fmt.Sprintf("%d người nhận chưa có email", missing)
	}
	lg.Info("đã gửi email lịch báo cáo", "sent", sent, "missing", missing, "outcome", "ok")
	status := DeliverySent
	if sent == 0 {
		status = DeliverySkipped
	}
	return mark(status, note, sent)
}

// deliveryMessage soạn email: chỉ tiêu đề mẫu, kỳ và liên kết — không số liệu, không đính kèm.
func (s *Service) deliveryMessage(j domain.Job, url string) port.Message {
	period := ""
	if j.Params.Period != "" {
		if p, err := domain.ParsePeriod(j.Params.Period); err == nil {
			period = p.Label
		}
	} else if j.Params.SeasonCode != "" {
		period = fmt.Sprintf("Vụ %s-%d", j.Params.SeasonCode, j.Params.SeasonYear)
	}
	subject := "[AgriGreen] " + j.Def.Title
	if period != "" {
		subject += " — " + period
	}
	var b strings.Builder
	b.WriteString("Báo cáo định kỳ đã sẵn sàng.\n\n")
	b.WriteString("Mẫu: " + j.Def.Title + "\n")
	if period != "" {
		b.WriteString("Kỳ: " + period + "\n")
	}
	b.WriteString("Ngày xuất: " + domain.FormatDateTimeVN(j.RequestedAt) + "\n")
	if j.Provisional != nil && *j.Provisional {
		b.WriteString(domain.ProvisionalLabel + ": số liệu chưa đầy đủ tại thời điểm xuất.\n")
	}
	b.WriteString("\nTải tệp (" + strings.ToUpper(j.Format) + ") — liên kết chỉ dùng được trong 15 phút:\n" + url + "\n")
	if s.LinkBase != "" {
		b.WriteString("\nSau thời hạn trên, đăng nhập AgriGreen để tải lại: " + strings.TrimRight(s.LinkBase, "/") + "/reports/exports/" + j.ID.String() + "\n")
	}
	b.WriteString("\nEmail không đính kèm tệp để bảo vệ dữ liệu. Vui lòng không chuyển tiếp liên kết.\n")
	return port.Message{Subject: subject, Text: b.String()}
}
