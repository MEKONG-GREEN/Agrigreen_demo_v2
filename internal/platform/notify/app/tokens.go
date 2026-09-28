package app

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// RegisterPushToken gắn token đẩy cho thiết bị của CHÍNH người gọi. Thiết bị phải
// đã đăng ký ở pf/sync bởi người gọi và chưa bị thu hồi. Gắn lại cập nhật token và
// ảnh chụp phạm vi/vai (app gọi lại mỗi lần khởi động hoặc khi FCM đổi token).
func (s *Service) RegisterPushToken(ctx context.Context, deviceID uuid.UUID, provider, token string) (domain.PushToken, error) {
	p, err := principal(ctx)
	if err != nil {
		return domain.PushToken{}, err
	}
	token, provider = strings.TrimSpace(token), strings.TrimSpace(provider)
	if provider == "" {
		provider = domain.ProviderFCM
	}
	if provider != domain.ProviderFCM {
		return domain.PushToken{}, problem.Validation.WithField("provider", "chỉ hỗ trợ fcm")
	}
	if !domain.ValidToken(provider, token) {
		return domain.PushToken{}, problem.Validation.WithField("token", "20–4096 ký tự: chữ, số, _ : . -")
	}
	if s.Devices == nil {
		return domain.PushToken{}, ErrDependency
	}
	dev, err := s.Devices.Device(ctx, p.TenantID, deviceID)
	switch {
	case errors.Is(err, syncport.ErrDeviceNotFound):
		return domain.PushToken{}, ErrDeviceUnregistered
	case err != nil:
		return domain.PushToken{}, err
	case dev.ActorRef != p.ActorRef:
		return domain.PushToken{}, ErrDeviceUnregistered
	case dev.Revoked:
		return domain.PushToken{}, ErrDeviceRevoked
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return domain.PushToken{}, err
	}
	now := s.now()
	t := domain.PushToken{DeviceID: deviceID, ActorRef: p.ActorRef, OrgRef: orgPtr(p.OrgRef), Provider: provider, Token: token,
		Platform: dev.Platform, ScopeAll: sc.All, ScopeUnits: sc.Units, Roles: p.Roles, RegisteredAt: now, UpdatedAt: now}
	var out *domain.PushToken
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if err := store.UpsertToken(ctx, tx, p.TenantID, t); err != nil {
			return err
		}
		if err := store.DisableTokenValue(ctx, tx, token, deviceID, now); err != nil {
			return err
		}
		var e error
		out, e = store.TokenByDevice(ctx, tx, deviceID)
		return e
	})
	if err != nil {
		return domain.PushToken{}, err
	}
	return *out, nil
}
