package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// TenantLister trả danh sách tenant cần xử lý (worker, pool ag_ops_jobs).
type TenantLister func(ctx context.Context) ([]uuid.UUID, error)

// maxDigestItems: số mục tối đa trong một email.
const maxDigestItems = 200

// digestClaimMargin cộng vào hạn job để tính hạn nhận mục (claimed_until): lần nhận còn hạn thì worker khác bỏ qua.
const digestClaimMargin = time.Minute

// RunEmailDigest gửi một email tóm tắt đã đăng ký chưa gửi cho mỗi tenant tới hộp
// thư công vụ do DigestRecipients phân giải (hoặc DigestTo khi không nối resolver). Email chỉ có mã chỉ đạo, thời điểm, mã đơn vị và đường dẫn
// — không tiêu đề/nội dung, không dữ liệu cá nhân. Chưa cấu hình SMTP/người nhận → không làm gì.
//
// P0-POOL-SHARED (#126): KHÔNG gửi SMTP trong giao dịch DB. Mỗi tenant: (1) giao dịch ngắn NHẬN mục chờ
// (store.ClaimDigest — claim_id + hạn nhận), (2) phân giải người nhận + gửi email ngoài giao dịch, (3) giao dịch
// ngắn KẾT: gửi được → store.MarkDigestSent; lỗi → store.ReleaseDigestClaim, mục giữ nguyên cho lần chạy sau.
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
		ok, err := s.digestTenant(ctx, tid)
		if err != nil {
			errs = append(errs, err)
		}
		if ok {
			sent++
		}
	}
	return sent, errors.Join(errs...)
}

// digestTenant gửi tối đa một email tóm tắt cho tenant tid; true khi đã gửi và ghi nhận.
func (s *Service) digestTenant(ctx context.Context, tid uuid.UUID) (bool, error) {
	claim := ids.New()
	now := s.now()
	var items []domain.DigestItem
	if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var err error
		items, err = store.ClaimDigest(ctx, tx, claim, now, now.Add(s.digests.job.Timeout+digestClaimMargin), maxDigestItems)
		return err
	}); err != nil || len(items) == 0 {
		return false, err
	}
	// Ngoài giao dịch: dựng nội dung, phân giải người nhận, gửi SMTP.
	if err := s.sendDigest(ctx, tid, items); err != nil {
		// Context gốc có thể đã hết (job quá hạn) — trả mục bằng context riêng có hạn ngắn.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if rerr := tenant.WithTenantTx(rctx, s.DB, tid, func(tx pgx.Tx) error {
			return store.ReleaseDigestClaim(rctx, tx, claim)
		}); rerr != nil {
			return false, errors.Join(err, rerr)
		}
		return false, err
	}
	mctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	var marked int64
	if err := tenant.WithTenantTx(mctx, s.DB, tid, func(tx pgx.Tx) error {
		var err error
		marked, err = store.MarkDigestSent(mctx, tx, claim, s.now())
		return err
	}); err != nil {
		return true, fmt.Errorf("notify: email tóm tắt đã gửi nhưng chưa ghi nhận (hết hạn nhận sẽ gửi lại): %w", err)
	}
	if marked != int64(len(items)) {
		return true, fmt.Errorf("notify: %w: email tóm tắt ghi nhận %d/%d mục (hạn nhận đã hết giữa chừng)", ErrDependency, marked, len(items))
	}
	return true, nil
}

func (s *Service) sendDigest(ctx context.Context, tid uuid.UUID, items []domain.DigestItem) error {
	subject, body := s.formatDigest(items)
	if subject == "" || body == "" {
		return fmt.Errorf("notify: %w: empty digest content", ErrDependency)
	}
	to := slices.Clone(s.DigestTo)
	if s.DigestRecipients != nil {
		var err error
		if to, err = s.DigestRecipients(ctx, tid); err != nil {
			return err
		}
		if len(to) == 0 {
			return fmt.Errorf("notify: %w: digest recipients missing", ErrDependency)
		}
	}
	return s.Mailer.Send(ctx, slices.Clone(to), subject, body)
}
