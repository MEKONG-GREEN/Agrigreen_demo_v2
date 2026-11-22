package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ErrNotCNTSOwner: kỳ đã chỉ định người phụ trách biểu, người gọi không phải người đó.
var ErrNotCNTSOwner = problem.Forbidden.WithDetail("chỉ người phụ trách biểu của kỳ được xác thực hoặc yêu cầu điều chỉnh")

// CNTSReviewGuard là kiểm quyền rà soát của profile kn_cnts cho cap/forms (BF-CN-XA-DATA #175, BRD CNTS FN-09): kỳ thuộc
// lần phát hành có người phụ trách biểu thì chỉ người đó xác thực / yêu cầu điều chỉnh bài nộp của xã. Kỳ không có người
// phụ trách (phát hành trước BF-CN-PUBLISH, hoặc không chọn) giữ quy tắc của cap/forms (Cấp Tỉnh trong phạm vi, bốn mắt).
// cap/forms gọi ngoài giao dịch của mình; ở đây chỉ đọc sổ cnts_publication trong giao dịch riêng của pf/extension.
type CNTSReviewGuard struct{ Svc *Service }

// AllowReview cài đặt formsport.ReviewGuard.
func (g CNTSReviewGuard) AllowReview(ctx context.Context, periodID uuid.UUID, _ string) error {
	p, _, err := port.RequireSession(ctx)
	if err != nil {
		return err
	}
	var owner *uuid.UUID
	if err := g.Svc.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		pub, err := store.CNTSPublicationOfPeriod(ctx, tx, periodID)
		if err != nil || pub == nil {
			return err
		}
		owner = pub.OwnerRef
		return nil
	}); err != nil {
		return err
	}
	if owner != nil && *owner != p.ActorRef {
		return ErrNotCNTSOwner
	}
	return nil
}
