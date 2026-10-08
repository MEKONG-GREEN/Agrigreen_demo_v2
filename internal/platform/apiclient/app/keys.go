package app

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// systemActor là actor_ref của bản ghi sổ do hệ thống ghi (khóa hết hạn).
var systemActor = uuid.MustParse("00000000-0000-7000-8000-000000000001")

// expireDue ghi sự kiện expired cho khóa đã quá hạn (hoặc quá hạn chồng lấn) của khách hàng.
func (s *Service) expireDue(ctx context.Context, tx pgx.Tx, tid uuid.UUID, keys []store.Key, now time.Time) ([]store.Key, error) {
	out := make([]store.Key, 0, len(keys))
	for _, k := range keys {
		if (k.Status == domain.KeyStatusActive || k.Status == domain.KeyStatusRotated) && !domain.KeyUsable(k.Status, k.ExpiresAt, k.GraceUntil, now) {
			e := store.KeyEvent{ID: ids.New(), TenantID: tid, KeyID: k.KeyID, ClientID: k.ClientID, Action: string(port.KeyExpired), KeyPrefix: k.KeyPrefix,
				PepperVersion: k.PepperVersion, ExpiresAt: k.ExpiresAt, EventTime: now, ActorRef: systemActor, IdemKey: "key:expire:" + k.KeyID.String()}
			if err := ledger.Classify(store.InsertKeyEvent(ctx, tx, &e)); err != nil {
				return nil, err
			}
			k.Status, k.LastEventID = domain.KeyStatusExpired, e.ID
			if err := store.PutKey(ctx, tx, k); err != nil {
				return nil, err
			}
		}
		out = append(out, k)
	}
	return out, nil
}

func usableCount(keys []store.Key, now time.Time, except uuid.UUID) int {
	n := 0
	for _, k := range keys {
		if k.KeyID != except && domain.KeyUsable(k.Status, k.ExpiresAt, k.GraceUntil, now) {
			n++
		}
	}
	return n
}

// newKey sinh khóa và dòng sổ issued (chỉ prefix + HMAC).
func (s *Service) newKey(tid, clientID, actor uuid.UUID, now time.Time, rotatedFrom *uuid.UUID, idem string) (store.KeyEvent, domain.GeneratedKey, error) {
	if s.Peppers == nil {
		return store.KeyEvent{}, domain.GeneratedKey{}, ErrDependencyUnavailable.WithDetail("Chưa cấu hình pepper khóa API (AG_APIKEY_PEPPER_V<n>)")
	}
	g, err := domain.GenerateKeyFor(s.Rand, s.Sandbox)
	if err != nil {
		return store.KeyEvent{}, domain.GeneratedKey{}, err
	}
	mac, _ := s.Peppers.MAC(s.Peppers.Current, g.Full)
	return store.KeyEvent{ID: ids.New(), TenantID: tid, KeyID: ids.New(), ClientID: clientID, Action: string(port.KeyIssued), KeyPrefix: g.Prefix,
		KeyHMAC: mac, PepperVersion: s.Peppers.Current, ExpiresAt: now.Add(port.DefaultKeyLifetime).UTC(), RotatedFrom: rotatedFrom,
		EventTime: now, ActorRef: actor, IdemKey: idem}, g, nil
}

func keyRow(e store.KeyEvent) store.Key {
	return store.Key{TenantID: e.TenantID, KeyID: e.KeyID, ClientID: e.ClientID, KeyPrefix: e.KeyPrefix, KeyHMAC: e.KeyHMAC, PepperVersion: e.PepperVersion,
		Status: domain.KeyStatusActive, IssuedAt: e.EventTime.UTC(), IssuedBy: e.ActorRef, ExpiresAt: e.ExpiresAt, LastEventID: e.ID}
}

// IssueKey: api_client_admin phát hành khóa cho khách hàng active (tối đa port.MaxActiveKeys khóa
// hiệu lực). Bí mật chỉ có trong kết quả trả về — gọi lặp cùng Idempotency-Key trả khóa đã phát
// hành KHÔNG kèm bí mật (Secret rỗng).
func (s *Service) IssueKey(ctx context.Context, clientID uuid.UUID, idem string) (port.IssuedKey, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return port.IssuedKey{}, err
	}
	if idem == "" {
		return port.IssuedKey{}, problem.IdempotencyRequired
	}
	now := s.Clock.Now()
	key := "key:issue:" + idem
	var out port.IssuedKey
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := replayKey(ctx, tx, key); err != nil || prev != nil {
			if prev != nil {
				out = *prev
			}
			return err
		}
		c, err := store.ClientByID(ctx, tx, clientID, true)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrClientNotFound
		}
		if c.Status != string(port.ClientActive) {
			return ErrState.WithDetail("Chỉ phát hành khóa cho khách hàng đã duyệt (active)")
		}
		keys, err := store.KeysOf(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if keys, err = s.expireDue(ctx, tx, p.TenantID, keys, now); err != nil {
			return err
		}
		if usableCount(keys, now, uuid.Nil) >= port.MaxActiveKeys {
			return port.ErrTooManyKeys
		}
		e, g, err := s.newKey(p.TenantID, clientID, p.ActorRef, now, nil, key)
		if err != nil {
			return err
		}
		if err := ledger.Classify(store.InsertKeyEvent(ctx, tx, &e)); err != nil {
			return err
		}
		if err := store.PutKey(ctx, tx, keyRow(e)); err != nil {
			return err
		}
		out = port.IssuedKey{KeyID: e.KeyID, ClientID: clientID, Prefix: g.Prefix, Secret: g.Full, ExpiresAt: e.ExpiresAt}
		return nil
	})
	if err == nil {
		s.log().InfoContext(ctx, "phát hành khóa API", "event", "apiclient.key_issued", "client_id", clientID, "key_prefix", out.Prefix, "outcome", "ok")
	}
	return out, err
}

func replayKey(ctx context.Context, tx pgx.Tx, idem string) (*port.IssuedKey, error) {
	id, err := store.KeyIDByIdem(ctx, tx, idem)
	if err != nil || id == uuid.Nil {
		return nil, err
	}
	k, err := store.KeyByID(ctx, tx, id)
	if err != nil || k == nil {
		return nil, err
	}
	return &port.IssuedKey{KeyID: k.KeyID, ClientID: k.ClientID, Prefix: k.KeyPrefix, ExpiresAt: k.ExpiresAt}, nil
}

// RotateKey: khóa mới + khóa cũ chuyển rotated, còn hiệu lực tới grace_until (mặc định 7 ngày).
func (s *Service) RotateKey(ctx context.Context, clientID, keyID uuid.UUID, idem string) (port.IssuedKey, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return port.IssuedKey{}, err
	}
	if idem == "" {
		return port.IssuedKey{}, problem.IdempotencyRequired
	}
	now := s.Clock.Now()
	key := "key:rotate:" + idem
	var out port.IssuedKey
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := replayKey(ctx, tx, key); err != nil || prev != nil {
			if prev != nil {
				out = *prev
			}
			return err
		}
		c, err := store.ClientByID(ctx, tx, clientID, true)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrClientNotFound
		}
		if c.Status != string(port.ClientActive) {
			return ErrState.WithDetail("Chỉ xoay khóa của khách hàng active")
		}
		old, err := store.KeyByID(ctx, tx, keyID)
		if err != nil {
			return err
		}
		if old == nil || old.ClientID != clientID {
			return ErrKeyNotFound
		}
		keys, err := store.KeysOf(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if keys, err = s.expireDue(ctx, tx, p.TenantID, keys, now); err != nil {
			return err
		}
		for _, k := range keys {
			if k.KeyID == keyID {
				*old = k
			}
		}
		if old.Status != domain.KeyStatusActive {
			return ErrState.WithDetail("Chỉ xoay khóa đang active")
		}
		// Sau khi xoay: khóa cũ (chồng lấn) + khóa mới — không được vượt MaxActiveKeys.
		if usableCount(keys, now, keyID)+2 > port.MaxActiveKeys {
			return port.ErrTooManyKeys
		}
		e, g, err := s.newKey(p.TenantID, clientID, p.ActorRef, now, &keyID, key)
		if err != nil {
			return err
		}
		grace := domain.RotateGrace(now, old.ExpiresAt).UTC()
		re := store.KeyEvent{ID: ids.New(), TenantID: p.TenantID, KeyID: keyID, ClientID: clientID, Action: string(port.KeyRotated), KeyPrefix: old.KeyPrefix,
			PepperVersion: old.PepperVersion, ExpiresAt: old.ExpiresAt, GraceUntil: &grace, ReplacedBy: &e.KeyID, EventTime: now, ActorRef: p.ActorRef,
			IdemKey: "key:rotated:" + keyID.String()}
		if err := ledger.Classify(store.InsertKeyEvent(ctx, tx, &re)); err != nil {
			return err
		}
		old.Status, old.GraceUntil, old.LastEventID = domain.KeyStatusRotated, &grace, re.ID
		if err := store.PutKey(ctx, tx, *old); err != nil {
			return err
		}
		if err := ledger.Classify(store.InsertKeyEvent(ctx, tx, &e)); err != nil {
			return err
		}
		if err := store.PutKey(ctx, tx, keyRow(e)); err != nil {
			return err
		}
		out = port.IssuedKey{KeyID: e.KeyID, ClientID: clientID, Prefix: g.Prefix, Secret: g.Full, ExpiresAt: e.ExpiresAt, GraceUntil: &grace}
		return nil
	})
	if err == nil {
		s.log().InfoContext(ctx, "xoay khóa API", "event", "apiclient.key_rotated", "client_id", clientID, "key_prefix", out.Prefix, "outcome", "ok")
	}
	return out, err
}

// RevokeKey: thu hồi (bắt buộc lý do), hiệu lực ngay ở lời gọi kế tiếp.
func (s *Service) RevokeKey(ctx context.Context, clientID, keyID uuid.UUID, reason string) (store.Key, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return store.Key{}, err
	}
	reason = strings.TrimSpace(reason)
	if !reasonOK(reason) {
		return store.Key{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	now := s.Clock.Now()
	var out store.Key
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		k, err := store.KeyByID(ctx, tx, keyID)
		if err != nil {
			return err
		}
		if k == nil || k.ClientID != clientID {
			return ErrKeyNotFound
		}
		if k.Status == domain.KeyStatusRevoked {
			out = *k
			return nil
		}
		e := store.KeyEvent{ID: ids.New(), TenantID: p.TenantID, KeyID: keyID, ClientID: clientID, Action: string(port.KeyRevoked), KeyPrefix: k.KeyPrefix,
			PepperVersion: k.PepperVersion, ExpiresAt: k.ExpiresAt, Reason: &reason, EventTime: now, ActorRef: p.ActorRef,
			IdemKey: "key:revoke:" + keyID.String()}
		if err := ledger.Classify(store.InsertKeyEvent(ctx, tx, &e)); err != nil {
			return err
		}
		at := now.UTC()
		k.Status, k.RevokedAt, k.RevokeReason, k.LastEventID = domain.KeyStatusRevoked, &at, &reason, e.ID
		if err := store.PutKey(ctx, tx, *k); err != nil {
			return err
		}
		out = *k
		return nil
	})
	return out, err
}

// ListKeys trả khóa của khách hàng (không bí mật, không HMAC ra ngoài — handler chỉ đưa prefix).
func (s *Service) ListKeys(ctx context.Context, clientID uuid.UUID) ([]store.Key, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin, port.RoleAPIClientApprover)
	if err != nil {
		return nil, err
	}
	var out []store.Key
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.KeysOf(ctx, tx, clientID)
		return err
	})
	return out, err
}

// ExpireDue ghi sự kiện expired cho mọi khóa đã quá hạn của khách hàng (job / thao tác quản trị).
func (s *Service) ExpireDue(ctx context.Context, operatorTenant, clientID uuid.UUID, now time.Time) error {
	return tenant.WithTenantTx(ctx, s.DB, operatorTenant, func(tx pgx.Tx) error {
		keys, err := store.KeysOf(ctx, tx, clientID)
		if err != nil {
			return err
		}
		_, err = s.expireDue(ctx, tx, operatorTenant, keys, now)
		return err
	})
}

// ExpireAllDue (job port.JobKeyExpiry) ghi sự kiện expired cho mọi khóa đã quá hạn của MỌI khách hàng
// của tenant đơn vị vận hành tại now, một giao dịch. Idempotent: khóa đã expired không ghi lại (lọc
// trạng thái + idem_key key:expire:<key_id>). Trả số khóa vừa chuyển expired.
func (s *Service) ExpireAllDue(ctx context.Context, operatorTenant uuid.UUID, now time.Time) (int, error) {
	if operatorTenant == uuid.Nil {
		return 0, tenant.ErrMissing
	}
	n := 0
	err := tenant.WithTenantTx(ctx, s.DB, operatorTenant, func(tx pgx.Tx) error {
		clients, err := store.ClientsWithDueKeys(ctx, tx, now)
		if err != nil {
			return err
		}
		for _, cid := range clients {
			keys, err := store.KeysOf(ctx, tx, cid)
			if err != nil {
				return err
			}
			before := map[uuid.UUID]string{}
			for _, k := range keys {
				before[k.KeyID] = k.Status
			}
			after, err := s.expireDue(ctx, tx, operatorTenant, keys, now)
			if err != nil {
				return err
			}
			for _, k := range after {
				if k.Status == domain.KeyStatusExpired && before[k.KeyID] != domain.KeyStatusExpired {
					n++
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Xác thực (port.Authenticator)
// ---------------------------------------------------------------------------

// Authenticator hiện thực port.Authenticator trên tenant đơn vị vận hành cố định.
type Authenticator struct{ Svc *Service }

var _ port.Authenticator = Authenticator{}

// Authenticate: prefix → bảng chiếu khóa → HMAC thời gian hằng theo pepper_version → khóa dùng được
// tại receivedAt → khách hàng active → scope từ đăng ký active. Mọi thất bại → ErrUnauthenticated.
func (a Authenticator) Authenticate(ctx context.Context, presented string, receivedAt time.Time) (port.Caller, error) {
	s := a.Svc
	prefix, ok := port.ParseKey(presented)
	if !ok {
		return port.Caller{}, port.ErrUnauthenticated
	}
	// Môi trường thử (W5.8, R-INT-08): khóa thử chỉ ở tiến trình thử, khóa thật không bao giờ ở tiến trình thử.
	if port.IsSandboxKey(presented) != s.Sandbox {
		s.log().WarnContext(ctx, "từ chối khóa API sai môi trường (thử ↔ thật)", "event", "apiclient.sandbox_key_refused",
			"key_prefix", prefix, "sandbox", s.Sandbox, "outcome", "rejected")
		return port.Caller{}, port.ErrUnauthenticated
	}
	if s.Peppers == nil || s.OperatorTenant == uuid.Nil {
		return port.Caller{}, ErrDependencyUnavailable
	}
	var out port.Caller
	found := false
	err := tenant.WithTenantTx(ctx, s.DB, s.OperatorTenant, func(tx pgx.Tx) error {
		k, err := store.KeyByPrefix(ctx, tx, prefix)
		if err != nil {
			return err
		}
		if k == nil {
			s.Peppers.Verify(s.Peppers.Current, presented, nil) // cân thời gian
			return nil
		}
		if !s.Peppers.Verify(k.PepperVersion, presented, k.KeyHMAC) || !domain.KeyUsable(k.Status, k.ExpiresAt, k.GraceUntil, receivedAt) {
			return nil
		}
		c, err := store.ClientByID(ctx, tx, k.ClientID, false)
		if err != nil || c == nil || c.Status != string(port.ClientActive) {
			return err
		}
		scopes, err := store.ActiveScopes(ctx, tx, c.ClientID)
		if err != nil {
			return err
		}
		methods := make([]port.AuthMethod, 0, len(c.AuthMethods))
		for _, m := range c.AuthMethods {
			methods = append(methods, port.AuthMethod(m))
		}
		out = port.Caller{
			Client: port.Client{ID: c.ClientID, OperatorTenant: c.TenantID, ClientTenantID: c.ClientTenantID, DisplayName: c.DisplayName,
				Status: port.ClientStatus(c.Status), AuthMethods: methods, ContactRef: c.ContactRef, Scopes: scopes},
			KeyID: k.KeyID, KeyPrefix: k.KeyPrefix, AuthMethod: port.AuthAPIKey, RequestID: ids.New(), ReceivedAt: receivedAt,
		}
		found = true
		return nil
	})
	if err != nil {
		return port.Caller{}, err
	}
	if !found {
		return port.Caller{}, port.ErrUnauthenticated
	}
	return out, nil
}
