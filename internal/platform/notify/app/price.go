package app

// Thông báo Báo giá nông sản (gói NOTIF — G1, D5): người nhận Cấp Tỉnh tra qua port.PriceDesk (gốc lắp ráp, pf/accounts)
// — cán bộ được phân công xã (hoặc mọi Cấp Tỉnh khi xã chưa phân công) và Quản trị nghiệp vụ báo giá; Cấp Xã là mã cán bộ
// phụ trách chính trong sự kiện (pf/price đọc từ port.Points tại ngày báo giá).

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// onPrice dựng thông báo cho một sự kiện của pf/price và ghi trong tx của inbox.
func (s *Service) onPrice(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	ms, _, err := domain.PriceMessages(env.EventType, env.Payload, env.OccurredAt)
	if err != nil {
		return err
	}
	var ns []domain.Notification
	for _, m := range ms {
		var tinh []uuid.UUID
		if m.Tinh {
			if tinh, err = s.priceTinh(ctx, env.TenantID, m); err != nil {
				return err
			}
		}
		ns = append(ns, m.Notifications(tinh)...)
	}
	if len(ns) == 0 {
		return nil
	}
	return s.deliver(ctx, tx, env, ns)
}

// priceTinh trả người nhận Cấp Tỉnh: Owner (nil → mọi Cấp Tỉnh của bên dùng) + Quản trị nghiệp vụ. Chưa nối PriceDesk →
// rỗng (thông báo chủ đề Cấp Tỉnh). Lỗi tra cứu → trả lỗi để hàng đợi giao lại (không lặng lẽ gửi thiếu người).
func (s *Service) priceTinh(ctx context.Context, tid uuid.UUID, m domain.PriceMessage) ([]uuid.UUID, error) {
	if s.PriceDesk == nil {
		return nil, nil
	}
	var out []uuid.UUID
	if m.Owner != nil && *m.Owner != uuid.Nil {
		out = append(out, *m.Owner)
	} else {
		all, err := s.PriceDesk.ProvinceOfficers(ctx, tid, m.Profile)
		if err != nil {
			return nil, err
		}
		out = append(out, all...)
	}
	st, err := s.PriceDesk.Stewards(ctx, tid, m.Profile)
	if err != nil {
		return nil, err
	}
	return domain.DedupRefs(append(out, st...)), nil
}
