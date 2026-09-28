package app

import (
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	syncport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// NudgeInput: đúng một trong DeviceID hoặc Unit.
type NudgeInput struct {
	DeviceID *uuid.UUID
	Unit     string
}

// NudgeResult là kết quả yêu cầu đồng bộ ngay.
type NudgeResult struct {
	ID      uuid.UUID
	Target  string
	Kind    string
	Devices int
}

// RateLimited mang số giây phải chờ (header Retry-After).
type RateLimited struct{ RetryAfter time.Duration }

// Nudge gửi thông điệp dữ liệu {kind: "sync_now"} tới thiết bị trong phạm vi
// (US-SYNC-02). Giới hạn: một đích mỗi NudgeCooldown; mỗi người NudgeHourly lần/giờ.
//   - unit: đơn vị phải trong phạm vi người gọi; nhận là thiết bị có phạm vi chứa đơn vị.
//   - device_id: thiết bị phải có token đang dùng và (trừ vai device_admin/platform_admin)
//     thuộc phạm vi người gọi; không thấy → 404 (không lộ sự tồn tại).
func (s *Service) Nudge(ctx context.Context, in NudgeInput) (NudgeResult, *RateLimited, error) {
	p, err := authz.Require(ctx, port.NudgeRoles...)
	if err != nil {
		return NudgeResult{}, nil, err
	}
	in.Unit = strings.TrimSpace(in.Unit)
	if (in.DeviceID == nil) == (in.Unit == "") {
		return NudgeResult{}, nil, problem.Validation.WithField("device_id", "cần đúng một trong device_id hoặc unit")
	}
	sc, err := s.scope(ctx)
	if err != nil {
		return NudgeResult{}, nil, err
	}
	res := NudgeResult{ID: ids.New(), Kind: domain.NudgeUnit, Target: in.Unit}
	if in.DeviceID != nil {
		res.Kind, res.Target = domain.NudgeDevice, in.DeviceID.String()
	} else if !sc.Allows(in.Unit) {
		return NudgeResult{}, nil, ErrOutOfScope
	}
	tenantWide := p.Has(syncport.RoleDeviceAdmin) || p.Has(authz.RolePlatformAdmin)
	now := s.now()
	var limited *RateLimited
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var tokens []domain.PushToken
		if in.DeviceID != nil {
			t, err := store.TokenByDevice(ctx, tx, *in.DeviceID)
			if err != nil {
				return err
			}
			if t == nil || t.DisabledAt != nil || !(tenantWide || tokenInScope(*t, sc)) {
				return ErrNudgeTargetNotFound
			}
			tokens = []domain.PushToken{*t}
		}
		if err := store.LockNudgeTarget(ctx, tx, p.TenantID, res.Kind, res.Target); err != nil {
			return err
		}
		last, err := store.LastNudgeAt(ctx, tx, res.Kind, res.Target)
		if err != nil {
			return err
		}
		if last != nil && now.Sub(*last) < s.NudgeCooldown {
			limited = &RateLimited{RetryAfter: s.NudgeCooldown - now.Sub(*last)}
			return nil
		}
		n, err := store.CountNudgesSince(ctx, tx, p.ActorRef, now.Add(-time.Hour))
		if err != nil {
			return err
		}
		if n >= s.NudgeHourly {
			limited = &RateLimited{RetryAfter: time.Hour}
			return nil
		}
		if in.DeviceID == nil {
			if tokens, err = store.ActiveTokensInUnit(ctx, tx, in.Unit); err != nil {
				return err
			}
		}
		res.Devices = len(tokens)
		if err := store.InsertNudge(ctx, tx, p.TenantID, res.ID, p.ActorRef, res.Kind, res.Target, res.Devices, now); err != nil {
			return err
		}
		// Lô gửi dùng chính mã nudge làm batch_id.
		return s.queueDeliveries(ctx, tx, p.TenantID, res.ID, port.KindSyncNow, now, sendSpec{tokens: tokens, urgent: true})
	})
	if err != nil {
		return NudgeResult{}, nil, err
	}
	if limited != nil {
		return NudgeResult{}, limited, ErrNudgeRateLimited.With("retry_after_s", int(math.Ceil(limited.RetryAfter.Seconds())))
	}
	return res, nil, nil
}

// tokenInScope: phạm vi chụp của thiết bị giao với phạm vi người gọi.
func tokenInScope(t domain.PushToken, sc authz.Scope) bool {
	if sc.All {
		return true
	}
	if t.ScopeAll {
		return false
	}
	return slices.ContainsFunc(t.ScopeUnits, sc.Allows)
}
