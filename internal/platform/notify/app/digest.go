package app

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// TenantLister trả danh sách tenant cần xử lý (worker, pool ag_ops_jobs).
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// maxDigestItems: số mục tối đa trong một email.
const maxDigestItems = 200

// RunEmailDigest gửi một email tóm tắt đã đăng ký chưa gửi cho mỗi tenant tới hộp
// thư công vụ do DigestRecipients phân giải (hoặc DigestTo khi không nối resolver). Email chỉ có mã chỉ đạo, thời điểm, mã đơn vị và đường dẫn
// — không tiêu đề/nội dung, không dữ liệu cá nhân. Gửi lỗi → giao dịch hủy, mục giữ
// nguyên cho lần chạy sau. Chưa cấu hình SMTP/người nhận → không làm gì.
func (s *Service) RunEmailDigest(ctx context.Context, tenants TenantLister) (int, error) {
	if s.digests == nil || !s.digests.configured {
		return 0, nil
	}
	if s.Mailer == nil || (s.DigestRecipients == nil && len(s.DigestTo) == 0) {
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
			to := slices.Clone(s.DigestTo)
			if s.DigestRecipients != nil {
				to, err = s.DigestRecipients(ctx, tid)
				if err != nil {
					return err
				}
				if len(to) == 0 {
					return fmt.Errorf("notify: %w: digest recipients missing", ErrDependency)
				}
			}
			if err := s.Mailer.Send(ctx, slices.Clone(to), subject, body); err != nil {
				return err
			}
			refs := make([]uuid.UUID, len(items))
			for i, it := range items {
				refs[i] = it.Reference
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
