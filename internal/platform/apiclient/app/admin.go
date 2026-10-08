package app

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var oauthClientRe = regexp.MustCompile(`^[A-Za-z0-9._-]{3,100}$`)

// ---------------------------------------------------------------------------
// Khách hàng API
// ---------------------------------------------------------------------------

// ProposeClientInput là dữ liệu đề xuất khách hàng API. Không có dữ liệu cá nhân: DisplayName là
// tên TỔ CHỨC, đầu mối liên hệ chỉ là ContactRef.
type ProposeClientInput struct {
	ClientTenantID uuid.UUID
	DisplayName    string
	Purpose        string
	ContactRef     uuid.UUID
	AuthMethods    []string
	OAuthClientID  string
	IdemKey        string
}

// ProposeClient: api_client_admin đề xuất khách hàng (trạng thái proposed).
func (s *Service) ProposeClient(ctx context.Context, in ProposeClientInput) (store.Client, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return store.Client{}, err
	}
	if in.IdemKey == "" {
		return store.Client{}, problem.IdempotencyRequired
	}
	name, purpose := strings.TrimSpace(in.DisplayName), strings.TrimSpace(in.Purpose)
	methods, okM := domain.NormAuthMethods(in.AuthMethods)
	switch {
	case in.ClientTenantID == uuid.Nil:
		return store.Client{}, problem.Validation.WithField("client_tenant_id", "bắt buộc, UUID tenant bên thứ ba")
	case in.ClientTenantID == p.TenantID:
		return store.Client{}, problem.Validation.WithField("client_tenant_id", "phải khác tenant đơn vị vận hành")
	case len([]rune(name)) < 2 || len([]rune(name)) > 200:
		return store.Client{}, problem.Validation.WithField("display_name", "tên tổ chức 2–200 ký tự")
	case !reasonOK(purpose):
		return store.Client{}, problem.Validation.WithField("purpose", "10–500 ký tự")
	case in.ContactRef == uuid.Nil:
		return store.Client{}, problem.Validation.WithField("contact_ref", "bắt buộc — mã tham chiếu đầu mối (không ghi tên, điện thoại)")
	case !okM:
		return store.Client{}, problem.Validation.WithField("auth_methods", "chỉ api_key, oauth_client_credentials")
	}
	var oauth *string
	if v := strings.TrimSpace(in.OAuthClientID); v != "" {
		if !oauthClientRe.MatchString(v) {
			return store.Client{}, problem.Validation.WithField("oauth_client_id", "3–100 ký tự [A-Za-z0-9._-]")
		}
		oauth = &v
	}
	now := s.Clock.Now()
	id := ids.New()
	e := store.ClientEvent{ID: ids.New(), TenantID: p.TenantID, ClientID: id, Action: "proposed", ClientTenantID: in.ClientTenantID,
		DisplayName: name, Purpose: purpose, ContactRef: in.ContactRef, AuthMethods: methods, OAuthClientID: oauth, ProposedBy: p.ActorRef,
		EventTime: now, ActorRef: p.ActorRef, IdemKey: "client:propose:" + in.IdemKey}
	var out store.Client
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ClientIDByIdem(ctx, tx, e.IdemKey); err != nil || prev != uuid.Nil {
			if err != nil {
				return err
			}
			c, err := store.ClientByID(ctx, tx, prev, false)
			if err == nil && c != nil {
				out = *c
			}
			return err
		}
		if err := ledger.Classify(store.InsertClientEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = store.Client{TenantID: p.TenantID, ClientID: id, ClientTenantID: in.ClientTenantID, DisplayName: name, Purpose: purpose,
			ContactRef: in.ContactRef, AuthMethods: methods, OAuthClientID: oauth, Status: string(port.ClientProposed), ProposedBy: p.ActorRef,
			ProposedAt: now.UTC(), LastEventID: e.ID, LastSeq: e.Seq, UpdatedAt: now.UTC()}
		return store.PutClient(ctx, tx, out)
	})
	return out, err
}

// clientTransition là một chuyển trạng thái khách hàng.
type clientTransition struct {
	action, from, to string
	approve          bool // R-56: người duyệt ≠ người đề xuất
}

var clientTransitions = map[string]clientTransition{
	"approve":    {action: "approved", from: string(port.ClientProposed), to: string(port.ClientActive), approve: true},
	"suspend":    {action: "suspended", from: string(port.ClientActive), to: string(port.ClientSuspended)},
	"reactivate": {action: "reactivated", from: string(port.ClientSuspended), to: string(port.ClientActive)},
	"revoke":     {action: "revoked", to: string(port.ClientRevoked)},
}

// ChangeClient áp một chuyển trạng thái: approve (api_client_approver ≠ người đề xuất), suspend /
// revoke (api_client_admin, bắt buộc lý do), reactivate (api_client_approver).
func (s *Service) ChangeClient(ctx context.Context, clientID uuid.UUID, op, reason string) (store.Client, error) {
	tr, ok := clientTransitions[op]
	if !ok {
		return store.Client{}, problem.NotFound
	}
	role := port.RoleAPIClientAdmin
	if tr.approve || op == "reactivate" {
		role = port.RoleAPIClientApprover
	}
	p, err := s.operator(ctx, role)
	if err != nil {
		return store.Client{}, err
	}
	reason = strings.TrimSpace(reason)
	var rp *string
	if op == "suspend" || op == "revoke" {
		if !reasonOK(reason) {
			return store.Client{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
		}
		rp = &reason
	}
	now := s.Clock.Now()
	var out store.Client
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		c, err := store.ClientByID(ctx, tx, clientID, true)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrClientNotFound
		}
		if c.Status == tr.to {
			out = *c // gọi lặp
			return nil
		}
		if (tr.from != "" && c.Status != tr.from) || c.Status == string(port.ClientRevoked) {
			return ErrState
		}
		if tr.approve && c.ProposedBy == p.ActorRef {
			return port.ErrSelfApproval
		}
		e := store.ClientEvent{ID: ids.New(), TenantID: p.TenantID, ClientID: clientID, Action: tr.action, ClientTenantID: c.ClientTenantID,
			DisplayName: c.DisplayName, Purpose: c.Purpose, ContactRef: c.ContactRef, AuthMethods: c.AuthMethods, OAuthClientID: c.OAuthClientID,
			ProposedBy: c.ProposedBy, Reason: rp, EventTime: now, ActorRef: p.ActorRef, IdemKey: "client:" + op + ":" + clientID.String() + ":" + c.LastEventID.String()}
		if err := ledger.Classify(store.InsertClientEvent(ctx, tx, &e)); err != nil {
			return err
		}
		c.Status, c.LastEventID, c.LastSeq, c.StatusReason = tr.to, e.ID, e.Seq, rp
		if tr.approve {
			at := now.UTC()
			c.ApprovedBy, c.ApprovedAt = &p.ActorRef, &at
		}
		if err := store.PutClient(ctx, tx, *c); err != nil {
			return err
		}
		out = *c
		return nil
	})
	return out, err
}

// GetClient đọc một khách hàng (admin / approver).
func (s *Service) GetClient(ctx context.Context, id uuid.UUID) (store.Client, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin, port.RoleAPIClientApprover)
	if err != nil {
		return store.Client{}, err
	}
	var out store.Client
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		c, err := store.ClientByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrClientNotFound
		}
		out = *c
		return nil
	})
	return out, err
}

// ListClients phân trang khách hàng.
func (s *Service) ListClients(ctx context.Context, status string, after uuid.UUID, limit int) ([]store.Client, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin, port.RoleAPIClientApprover)
	if err != nil {
		return nil, err
	}
	var out []store.Client
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.ListClients(ctx, tx, status, after, limit)
		return err
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Đăng ký sản phẩm (scope)
// ---------------------------------------------------------------------------

// ProposeSubscription: api_client_admin đề xuất đăng ký sản phẩm + scope cho khách hàng.
func (s *Service) ProposeSubscription(ctx context.Context, clientID uuid.UUID, product string, scopes []string, idem string) (store.Subscription, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return store.Subscription{}, err
	}
	if idem == "" {
		return store.Subscription{}, problem.IdempotencyRequired
	}
	name := strings.TrimSpace(product)
	var (
		sc []string
		ok bool
	)
	if prod, err := dataproduct.Get(name); err == nil {
		name = prod.Name
		if sc, ok = domain.NormScopes(prod, scopes); !ok {
			return store.Subscription{}, problem.Validation.WithField("scopes", "chỉ scope của hợp đồng sản phẩm ("+strings.Join(prod.Scopes, ", ")+") và usage:read")
		}
	} else if domain.IngestContract(name) { // W5.4: hợp đồng nạp contracts/integration/ingest — scope ingest:<tên>:write
		if sc, ok = domain.NormIngestScopes(name, scopes); !ok {
			return store.Subscription{}, problem.Validation.WithField("scopes", "chỉ "+port.IngestScope(name)+" và usage:read")
		}
	} else if fc, ferr := FeedContract(name, 0); ferr == nil { // W5.3: feed contracts/integration/feeds — scope feed:<tên>:read
		name = fc.Name
		if sc, ok = domain.NormFeedScopes(name, scopes); !ok {
			return store.Subscription{}, problem.Validation.WithField("scopes", "chỉ "+port.FeedScope(name)+" và usage:read")
		}
	} else {
		return store.Subscription{}, port.ErrUnknownProduct
	}
	now := s.Clock.Now()
	sid := ids.New()
	e := store.SubscriptionEvent{ID: ids.New(), TenantID: p.TenantID, SubscriptionID: sid, ClientID: clientID, Action: "proposed", Product: name,
		Scopes: sc, ProposedBy: p.ActorRef, EventTime: now, ActorRef: p.ActorRef, IdemKey: "subscription:propose:" + idem}
	var out store.Subscription
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.SubscriptionIDByIdem(ctx, tx, e.IdemKey); err != nil || prev != uuid.Nil {
			if err != nil {
				return err
			}
			sub, err := store.SubscriptionByID(ctx, tx, prev)
			if err == nil && sub != nil {
				out = *sub
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
		if c.Status == string(port.ClientRevoked) {
			return ErrState
		}
		subs, err := store.SubscriptionsOf(ctx, tx, clientID)
		if err != nil {
			return err
		}
		for _, x := range subs {
			if x.Product == name && x.Status != string(port.SubscriptionRevoked) {
				return ErrState.WithDetail("Khách hàng đã có đăng ký chưa thu hồi cho sản phẩm này")
			}
		}
		if err := ledger.Classify(store.InsertSubscriptionEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = store.Subscription{TenantID: p.TenantID, SubscriptionID: sid, ClientID: clientID, Product: name, Scopes: sc,
			Status: string(port.SubscriptionProposed), ProposedBy: p.ActorRef, ProposedAt: now.UTC(), LastEventID: e.ID, UpdatedAt: now.UTC()}
		return store.PutSubscription(ctx, tx, out)
	})
	return out, err
}

func itoa(n int) string { return strconv.Itoa(n) }

// ChangeSubscription: approve (api_client_approver ≠ người đề xuất) hoặc revoke (api_client_admin,
// lý do). Thu hồi có hiệu lực ngay ở lời gọi kế tiếp (scope đọc lúc xác thực).
func (s *Service) ChangeSubscription(ctx context.Context, clientID, subID uuid.UUID, op, reason string) (store.Subscription, error) {
	role := port.RoleAPIClientAdmin
	if op == "approve" {
		role = port.RoleAPIClientApprover
	} else if op != "revoke" {
		return store.Subscription{}, problem.NotFound
	}
	p, err := s.operator(ctx, role)
	if err != nil {
		return store.Subscription{}, err
	}
	reason = strings.TrimSpace(reason)
	var rp *string
	if op == "revoke" {
		if !reasonOK(reason) {
			return store.Subscription{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
		}
		rp = &reason
	}
	now := s.Clock.Now()
	var out store.Subscription
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		sub, err := store.SubscriptionByID(ctx, tx, subID)
		if err != nil {
			return err
		}
		if sub == nil || sub.ClientID != clientID {
			return ErrSubscriptionNotFound
		}
		target, action := string(port.SubscriptionActive), "approved"
		if op == "revoke" {
			target, action = string(port.SubscriptionRevoked), "revoked"
		}
		if sub.Status == target {
			out = *sub
			return nil
		}
		if sub.Status == string(port.SubscriptionRevoked) || (op == "approve" && sub.Status != string(port.SubscriptionProposed)) {
			return ErrState
		}
		if op == "approve" {
			if sub.ProposedBy == p.ActorRef {
				return port.ErrSelfApproval
			}
			c, err := store.ClientByID(ctx, tx, clientID, false)
			if err != nil {
				return err
			}
			if c == nil || c.Status == string(port.ClientRevoked) {
				return ErrState.WithDetail("Khách hàng đã thu hồi")
			}
		}
		e := store.SubscriptionEvent{ID: ids.New(), TenantID: p.TenantID, SubscriptionID: subID, ClientID: clientID, Action: action, Product: sub.Product,
			Scopes: sub.Scopes, ProposedBy: sub.ProposedBy, Reason: rp, EventTime: now, ActorRef: p.ActorRef,
			IdemKey: "subscription:" + op + ":" + subID.String()}
		if err := ledger.Classify(store.InsertSubscriptionEvent(ctx, tx, &e)); err != nil {
			return err
		}
		at := now.UTC()
		sub.Status, sub.LastEventID = target, e.ID
		if op == "approve" {
			sub.ApprovedBy, sub.ApprovedAt = &p.ActorRef, &at
		} else {
			sub.RevokedAt, sub.RevokeReason = &at, rp
		}
		if err := store.PutSubscription(ctx, tx, *sub); err != nil {
			return err
		}
		out = *sub
		return nil
	})
	return out, err
}

// ListSubscriptions trả đăng ký của một khách hàng.
func (s *Service) ListSubscriptions(ctx context.Context, clientID uuid.UUID) ([]store.Subscription, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin, port.RoleAPIClientApprover)
	if err != nil {
		return nil, err
	}
	var out []store.Subscription
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.SubscriptionsOf(ctx, tx, clientID)
		return err
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Gói hạn mức có phiên bản
// ---------------------------------------------------------------------------

// ProposePlan: api_client_admin đề xuất phiên bản gói hạn mức mới (số phiên bản tăng dần).
func (s *Service) ProposePlan(ctx context.Context, clientID uuid.UUID, in port.Plan, idem string) (store.PlanRow, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return store.PlanRow{}, err
	}
	if idem == "" {
		return store.PlanRow{}, problem.IdempotencyRequired
	}
	if f, r := domain.PlanProblem(in); f != "" {
		return store.PlanRow{}, problem.Validation.WithField(f, r)
	}
	now := s.Clock.Now()
	if in.EffectiveFrom.IsZero() {
		in.EffectiveFrom = now
	}
	key := "plan:propose:" + idem
	var out store.PlanRow
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if v, err := store.PlanVersionByIdem(ctx, tx, key); err != nil || v != 0 {
			if err != nil {
				return err
			}
			row, err := store.PlanVersion(ctx, tx, clientID, v)
			if err == nil && row != nil {
				out = *row
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
		v, err := store.NextPlanVersion(ctx, tx, clientID)
		if err != nil {
			return err
		}
		out = store.PlanRow{TenantID: p.TenantID, ClientID: clientID, Version: v, RequestsPerMinute: in.RequestsPerMinute, DailyUnits: in.DailyUnits,
			MonthlyUnits: in.MonthlyUnits, MaxExportRows: in.MaxExportRows, EffectiveFrom: in.EffectiveFrom.UTC(), Status: "proposed",
			ProposedBy: p.ActorRef, ProposedAt: now.UTC()}
		if err := ledger.Classify(store.InsertPlanEvent(ctx, tx, ids.New(), out, "proposed", p.ActorRef, now, key)); err != nil {
			return err
		}
		return store.PutPlan(ctx, tx, out)
	})
	return out, err
}

// ApprovePlan: api_client_approver ≠ người đề xuất duyệt một phiên bản gói.
func (s *Service) ApprovePlan(ctx context.Context, clientID uuid.UUID, version int) (store.PlanRow, error) {
	p, err := s.operator(ctx, port.RoleAPIClientApprover)
	if err != nil {
		return store.PlanRow{}, err
	}
	now := s.Clock.Now()
	var out store.PlanRow
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		row, err := store.PlanVersion(ctx, tx, clientID, version)
		if err != nil {
			return err
		}
		if row == nil {
			return ErrPlanNotFound
		}
		if row.Status == "approved" {
			out = *row
			return nil
		}
		if row.ProposedBy == p.ActorRef {
			return port.ErrSelfApproval
		}
		if err := ledger.Classify(store.InsertPlanEvent(ctx, tx, ids.New(), *row, "approved", p.ActorRef, now,
			"plan:approve:"+clientID.String()+":"+itoa(version))); err != nil {
			return err
		}
		at := now.UTC()
		row.Status, row.ApprovedBy, row.ApprovedAt = "approved", &p.ActorRef, &at
		if err := store.PutPlan(ctx, tx, *row); err != nil {
			return err
		}
		out = *row
		return nil
	})
	return out, err
}

// ListPlans trả mọi phiên bản gói của một khách hàng.
func (s *Service) ListPlans(ctx context.Context, clientID uuid.UUID) ([]store.PlanRow, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin, port.RoleAPIClientApprover)
	if err != nil {
		return nil, err
	}
	var out []store.PlanRow
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var err error
		out, err = store.PlansOf(ctx, tx, clientID)
		return err
	})
	return out, err
}

// PlanFor hiện thực port.Plans: phiên bản đã duyệt có hiệu lực tại at, không có → gói mặc định
// (Version 0, "tạm, chờ khách").
func (s *Service) PlanFor(ctx context.Context, operatorTenant, clientID uuid.UUID, at time.Time) (port.Plan, error) {
	out := domain.DefaultPlan(clientID)
	err := tenant.WithTenantTx(ctx, s.DB, operatorTenant, func(tx pgx.Tx) error {
		row, err := store.PlanAt(ctx, tx, clientID, at)
		if err != nil || row == nil {
			return err
		}
		out = port.Plan{ClientID: clientID, Version: row.Version, RequestsPerMinute: row.RequestsPerMinute, DailyUnits: row.DailyUnits,
			MonthlyUnits: row.MonthlyUnits, MaxExportRows: row.MaxExportRows, EffectiveFrom: row.EffectiveFrom}
		return nil
	})
	return out, err
}

var _ port.Plans = (*Service)(nil)
