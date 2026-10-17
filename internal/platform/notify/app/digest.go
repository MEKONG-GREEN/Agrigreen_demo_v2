package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// TenantLister trả danh sách tenant cần xử lý (worker, pool ag_ops_jobs).
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// maxDigestItems: số chỉ đạo tối đa trong một email.
const maxDigestItems = 200

// RunEmailDigest gửi một email tóm tắt chỉ đạo khẩn chưa gửi cho mỗi tenant tới hộp
// thư công vụ DigestTo. Email chỉ có mã chỉ đạo, thời điểm, mã đơn vị và đường dẫn
// — không tiêu đề/nội dung, không dữ liệu cá nhân. Gửi lỗi → giao dịch hủy, mục giữ
// nguyên cho lần chạy sau. Chưa cấu hình SMTP/người nhận → không làm gì.
func (s *Service) RunEmailDigest(ctx context.Context, tenants TenantLister) (int, error) {
	if s.Mailer == nil || len(s.DigestTo) == 0 {
		return 0, nil
	}
	ts, err := tenants(ctx)
	if err != nil {
		return 0, err
	}
	sent := 0
	var errs []error
	for _, tid := range ts {
		err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			items, err := store.PendingDigest(ctx, tx, maxDigestItems)
			if err != nil || len(items) == 0 {
				return err
			}
			subject, body := s.formatDigest(items)
			if subject == "" || body == "" {
				return fmt.Errorf("notify: %w: empty digest content", ErrDependency)
			}
			if err := s.Mailer.Send(ctx, s.DigestTo, subject, body); err != nil {
				return err
			}
			refs := make([]uuid.UUID, len(items))
			for i, it := range items {
				refs[i] = it.DirectiveID
			}
			sent++
			return store.MarkDigestSent(ctx, tx, refs, ids.New(), s.now())
		})
		if err != nil {
			errs = append(errs, err)
		}
	}
	return sent, errors.Join(errs...)
}

// DigestMessage dựng tiêu đề và nội dung email tóm tắt (văn bản thuần).
func DigestMessage(items []domain.DigestItem) (subject, body string) {
	subject = fmt.Sprintf("[AgriGreen] %d chỉ đạo khẩn mới", len(items))
	var b strings.Builder
	b.WriteString("Tóm tắt chỉ đạo khẩn đã ban hành. Nội dung đầy đủ xem trong ứng dụng AgriGreen (đăng nhập).\n\n")
	for _, it := range items {
		fmt.Fprintf(&b, "- %s | vùng: %s | mở: /extension/directives/%s\n",
			it.IssuedAt.In(clock.Zone()).Format("02/01/2006 15:04"), strings.Join(it.AudienceUnits, ", "), it.DirectiveID)
	}
	b.WriteString("\nEmail tự động từ hệ thống, không trả lời thư này.\n")
	return subject, b.String()
}
