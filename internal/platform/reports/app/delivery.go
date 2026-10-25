package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	accountsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/dispatch"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
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
//
// P0-POOL-SHARED-2 (#143): ký URL, tra email và gửi SMTP chạy NGOÀI giao dịch inbox (txdeps.Get → s.deliver): nhận việc
// gửi ở giao dịch riêng (schedule_run.delivery_claim_*), gửi, rồi handler chạy lại trong giao dịch inbox MỚI ghi kết quả
// (MarkDelivered bỏ nhận) cùng commit với inbox (R-61). Gửi lỗi → trả nhận, giao lại gửi lại (ít nhất một lần).
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
	job, recipients := *j, slices.Clone(sch.Recipients)
	res, err := txdeps.Get(ctx, "reports.delivery:"+j.ID.String(), func(ctx context.Context) (deliveryResult, error) {
		return s.deliver(ctx, job, recipients)
	})
	if err != nil {
		return err
	}
	if res.delivered {
		return nil // worker khác đã gửi và ghi kết quả
	}
	note := ""
	if res.missing > 0 {
		note = fmt.Sprintf("%d người nhận chưa có email", res.missing)
	}
	lg.Info("đã gửi email lịch báo cáo", "sent", res.sent, "missing", res.missing, "outcome", "ok")
	status := DeliverySent
	if res.sent == 0 {
		status = DeliverySkipped
	}
	return mark(status, note, res.sent)
}

// deliveryResult là kết quả gửi email của một lần chạy lịch (ngoài giao dịch).
type deliveryResult struct {
	sent, missing int
	delivered     bool // lần chạy đã có kết quả gửi (worker khác) — không gửi
}

// deliveryLease là hạn nhận việc gửi email lịch: ngắn hơn hạn xử lý một thông điệp (inbox.DefaultHandlerTimeout 2 phút)
// để worker nhận lại (giao lại sau khi worker giữ chết) kịp chờ hết hạn rồi gửi trong hạn của chính nó.
const deliveryLease = time.Minute

// deliveryPoll là chu kỳ chờ khi worker khác đang giữ việc gửi còn hạn.
const deliveryPoll = 500 * time.Millisecond

// deliver nhận việc gửi (giao dịch riêng), ký URL, tra email, gửi SMTP — KHÔNG giữ giao dịch nào trong lúc gọi ra ngoài.
// Worker khác đang giữ còn hạn → chờ (tới khi có kết quả gửi hoặc hết hạn giữ). Gửi lỗi → trả nhận.
func (s *Service) deliver(ctx context.Context, j domain.Job, recipients []uuid.UUID) (deliveryResult, error) {
	token := ids.New()
	for {
		now := s.now()
		var c store.DeliveryClaim
		if err := s.tx(ctx, j.TenantID, func(tx pgx.Tx) error {
			var err error
			c, err = store.ClaimDelivery(ctx, tx, j.ID, token, now, now.Add(deliveryLease))
			return err
		}); err != nil {
			return deliveryResult{}, err
		}
		switch {
		case c.Delivered:
			return deliveryResult{delivered: true}, nil
		case c.Missing:
			return deliveryResult{}, fmt.Errorf("reports: không thấy lần chạy lịch của việc xuất %s", j.ID)
		case c.Claimed:
			// Hạn của các lời gọi ra ngoài: hết trước hạn giữ một biên — worker khác chỉ nhận lại sau khi lượt này đã dừng.
			sctx, cancel := context.WithTimeout(ctx, deliveryLease-dispatch.SendMargin(deliveryLease))
			res, err := s.sendDelivery(sctx, j, recipients)
			cancel()
			if err != nil {
				rctx, rcancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
				defer rcancel()
				if rerr := s.tx(rctx, j.TenantID, func(tx pgx.Tx) error { return store.ReleaseDelivery(rctx, tx, j.ID, token) }); rerr != nil {
					return deliveryResult{}, errors.Join(err, rerr)
				}
				return deliveryResult{}, err
			}
			return res, nil
		}
		select {
		case <-ctx.Done():
			return deliveryResult{}, fmt.Errorf("reports: việc gửi email lịch đang do worker khác giữ: %w", ctx.Err())
		case <-time.After(deliveryPoll):
		}
	}
}

// sendDelivery ký URL tải, tra email từng người nhận và gửi (đã nhận việc; ngoài giao dịch).
func (s *Service) sendDelivery(ctx context.Context, j domain.Job, recipients []uuid.UUID) (deliveryResult, error) {
	url, err := s.Store.PresignGet(ctx, *j.ObjectKey, objstore.MaxPresignTTL)
	if err != nil {
		return deliveryResult{}, err
	}
	msg := s.deliveryMessage(j, url)
	var res deliveryResult
	for _, r := range recipients {
		addr, err := s.Directory.EmailOf(ctx, j.TenantID, r)
		if errors.Is(err, accountsport.ErrNoEmail) || (err == nil && strings.TrimSpace(addr) == "") {
			res.missing++
			continue
		}
		if err != nil {
			return deliveryResult{}, err // lỗi tạm thời: giao lại
		}
		msg.To = addr
		if err := s.Mailer.Send(ctx, msg); err != nil {
			return deliveryResult{}, err
		}
		res.sent++
	}
	return res, nil
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
