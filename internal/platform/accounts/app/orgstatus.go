package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
)

// Lý do ghi sổ khi khóa/mở theo trạng thái tổ chức (≥ 20 ký tự, không PII).
const (
	reasonOrgDeactivated = "Tổ chức bị vô hiệu hóa (pf.party.org.deactivated)"
	reasonOrgReactivated = "Tổ chức được kích hoạt lại (pf.party.org.reactivated)"
)

// OrgStatusResult đếm số tài khoản đã xử lý (cho log của worker).
type OrgStatusResult struct {
	Locked, Unlocked, Skipped int
}

// HandleOrgStatus xử lý pf.party.org.deactivated/reactivated trong giao dịch của
// inbox.Process (idempotent theo event_id, R-61):
//
//   - deactivated: khóa MỌI tài khoản có org_ref là tổ chức đó, thu hồi phiên, ghi
//     lock_cause = org_deactivated (HTX US-HTXSTATUS-01 AC-2). Tài khoản đang khóa
//     vì lý do khác giữ nguyên nguyên nhân cũ.
//   - reactivated: mở CHỈ các tài khoản có lock_cause = org_deactivated (US-HTXSTATUS-02 AC-2).
//
// Gọi Keycloak trước khi commit: nếu giao dịch hỏng, sự kiện được giao lại và chạy
// lại an toàn (tài khoản đã tắt ở Keycloak mà pf còn ghi "đang hoạt động" được coi
// là do lần chạy trước).
func (s *Service) HandleOrgStatus(ctx context.Context, tx pgx.Tx, env outbox.Envelope) (OrgStatusResult, error) {
	if s.Dir == nil {
		return OrgStatusResult{}, errNoDirectoryYet
	}
	switch env.EventType {
	case partyport.EventOrgDeactivated:
		var p partyport.OrgDeactivatedV1
		if err := json.Unmarshal(env.Payload, &p); err != nil || p.OrgRef == uuid.Nil {
			return OrgStatusResult{}, fmt.Errorf("accounts: payload %s không hợp lệ", env.EventType)
		}
		return s.lockOrg(ctx, tx, env, p.OrgRef, actorOr(p.ActorRef, p.OrgRef))
	case partyport.EventOrgReactivated:
		var p partyport.OrgReactivatedV1
		if err := json.Unmarshal(env.Payload, &p); err != nil || p.OrgRef == uuid.Nil {
			return OrgStatusResult{}, fmt.Errorf("accounts: payload %s không hợp lệ", env.EventType)
		}
		return s.unlockOrg(ctx, tx, env, p.OrgRef, actorOr(p.ActorRef, p.OrgRef))
	}
	return OrgStatusResult{}, nil
}

func actorOr(a, fallback uuid.UUID) uuid.UUID {
	if a != uuid.Nil {
		return a
	}
	return fallback
}

// orgUsers đọc mọi người dùng của tổ chức trong tenant từ Keycloak (đủ các trang).
func (s *Service) orgUsers(ctx context.Context, tid, org uuid.UUID) ([]port.User, error) {
	attrs := map[string]string{port.AttrTenantID: tid.String(), port.AttrOrgRef: org.String()}
	var out []port.User
	for off := 0; ; off += listBatch {
		batch, err := s.Dir.FindUsers(ctx, port.Query{Attributes: attrs, First: off, Max: listBatch})
		if err != nil {
			return nil, err
		}
		for _, u := range batch {
			if u.TenantID == tid && u.OrgRef == org {
				out = append(out, u)
			}
		}
		if len(batch) < listBatch {
			return out, nil
		}
	}
}

func (s *Service) lockOrg(ctx context.Context, tx pgx.Tx, env outbox.Envelope, org, actorRef uuid.UUID) (OrgStatusResult, error) {
	var res OrgStatusResult
	users, err := s.orgUsers(ctx, env.TenantID, org)
	if err != nil {
		return res, err
	}
	for _, u := range users {
		acc, err := store.Account(ctx, tx, u.ID, true)
		if err != nil {
			return res, err
		}
		// Keycloak đã tắt: chỉ ghi nếu pf còn ghi "đang hoạt động" (lần chạy trước dừng giữa chừng).
		if !u.Enabled && (acc == nil || !acc.Enabled) {
			res.Skipped++ // khóa vì lý do khác (hoặc tắt ngoài nền tảng) — giữ nguyên
			continue
		}
		if u.Enabled {
			if err := s.Dir.SetEnabled(ctx, u.ID, false); err != nil {
				if errors.Is(err, port.ErrUserNotFound) {
					res.Skipped++
					continue
				}
				return res, err
			}
		}
		revoked := s.Dir.Logout(ctx, u.ID) == nil
		state := domain.Account{UserRef: u.ID, OrgRef: org}
		if acc != nil {
			state.Roles = acc.Roles
		} else {
			roles, err := s.Dir.RealmRoles(ctx, u.ID)
			if err != nil {
				return res, err
			}
			state.Roles = domain.ManagedOf(roles)
		}
		state.Enabled, state.LockCause = false, port.LockOrgDeactivated
		ev := newEvent(env.TenantID, actorRef, state, domain.ActionLocked, s.now(), "org_deactivated:"+env.EventID.String()+":"+u.ID.String(), reasonOrgDeactivated)
		ev.Details = map[string]any{"source_event_id": env.EventID.String(), "sessions_revoked": revoked}
		if err := record(ctx, tx, &ev); err != nil {
			return res, err
		}
		res.Locked++
	}
	return res, nil
}

func (s *Service) unlockOrg(ctx context.Context, tx pgx.Tx, env outbox.Envelope, org, actorRef uuid.UUID) (OrgStatusResult, error) {
	var res OrgStatusResult
	accs, err := store.LockedByOrg(ctx, tx, org, port.LockOrgDeactivated)
	if err != nil {
		return res, err
	}
	for _, acc := range accs {
		if err := s.Dir.SetEnabled(ctx, acc.UserRef, true); err != nil {
			if errors.Is(err, port.ErrUserNotFound) {
				res.Skipped++
				continue
			}
			return res, err
		}
		state := acc
		state.Enabled, state.LockCause = true, ""
		ev := newEvent(env.TenantID, actorRef, state, domain.ActionUnlocked, s.now(), "org_reactivated:"+env.EventID.String()+":"+acc.UserRef.String(), reasonOrgReactivated)
		ev.Details = map[string]any{"source_event_id": env.EventID.String(), "prev_cause": string(port.LockOrgDeactivated)}
		if err := record(ctx, tx, &ev); err != nil {
			return res, err
		}
		res.Unlocked++
	}
	return res, nil
}
