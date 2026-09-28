package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Page là một trang hộp thư (mới nhất trước).
type Page struct {
	Items []domain.Notification
	Next  *uuid.UUID
}

// List trả hộp thư của người gọi. before = id mục cuối trang trước.
func (s *Service) List(ctx context.Context, before uuid.UUID, unreadOnly bool, limit int) (Page, error) {
	p, v, err := s.viewer(ctx)
	if err != nil {
		return Page{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	since := s.now().Add(-TopicWindow)
	var items []domain.Notification
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListForViewer(ctx, tx, v, since, before, unreadOnly, limit+1)
		return e
	})
	if err != nil {
		return Page{}, err
	}
	pg := Page{Items: items}
	if len(items) > limit {
		pg.Items = items[:limit]
		last := pg.Items[limit-1].ID
		pg.Next = &last
	}
	if pg.Items == nil {
		pg.Items = []domain.Notification{}
	}
	return pg, nil
}

// UnreadCount đếm thông báo chưa đọc của người gọi.
func (s *Service) UnreadCount(ctx context.Context) (int64, error) {
	p, v, err := s.viewer(ctx)
	if err != nil {
		return 0, err
	}
	var n int64
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		n, e = store.UnreadCount(ctx, tx, v, s.now().Add(-TopicWindow))
		return e
	})
	return n, err
}

// MarkRead đánh dấu đã đọc một thông báo người gọi thấy được. Đọc lại → giữ read_at
// lần đầu. Không thấy (của người khác, ngoài phạm vi, tenant khác) → 404.
func (s *Service) MarkRead(ctx context.Context, id uuid.UUID) (domain.Notification, error) {
	p, v, err := s.viewer(ctx)
	if err != nil {
		return domain.Notification{}, err
	}
	now, since := s.now(), s.now().Add(-TopicWindow)
	var out *domain.Notification
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		n, err := store.GetForViewer(ctx, tx, v, since, id)
		if err != nil {
			return err
		}
		if n == nil {
			return ErrNotificationNotFound
		}
		if n.ReadAt == nil {
			if n.Topic() {
				err = store.InsertReceipt(ctx, tx, p.TenantID, id, p.ActorRef, now)
			} else {
				err = store.MarkPersonalRead(ctx, tx, id, p.ActorRef, now)
			}
			if err != nil {
				return err
			}
			if n, err = store.GetForViewer(ctx, tx, v, since, id); err != nil {
				return err
			}
		}
		out = n
		return nil
	})
	if err != nil {
		return domain.Notification{}, err
	}
	return *out, nil
}

// ReadAll đánh dấu đã đọc mọi thông báo người gọi thấy được; trả số đã đánh dấu.
func (s *Service) ReadAll(ctx context.Context) (int64, error) {
	p, v, err := s.viewer(ctx)
	if err != nil {
		return 0, err
	}
	var n int64
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		n, e = store.ReadAll(ctx, tx, v, s.now().Add(-TopicWindow), s.now())
		return e
	})
	return n, err
}
