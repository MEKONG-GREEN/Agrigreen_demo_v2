package app

// Cấp quyền chia sẻ dữ liệu giữa hai tenant (ADR-0020; RB-2, Q-MKG-12).
//
// Chủ sản phẩm chốt (29–30/09/2026): MKG (ERP, TMS) là tenant riêng; chỉ các dữ liệu cần thiết
// đi từ nền tảng sang qua hệ thống nội bộ. pf/party giữ sổ cấp quyền vì cấp quyền là quan hệ giữa
// hai BÊN THAM GIA (tenant = tổ chức sở hữu dữ liệu, ADR-0009), cùng loại với đồng ý theo mục đích
// (pf_party.consent) mà module đã giữ; pf/accounts chỉ quản tài khoản Keycloak, không giữ quan hệ
// dữ liệu giữa tổ chức.
//
//   - Quản trị nền tảng của tenant BÊN CẤP (platform_admin) đề xuất; một quản trị KHÁC duyệt (R-56,
//     kiểm ở đây và bằng CHECK DB); bên cấp thu hồi được bất kỳ lúc nào.
//   - Khi duyệt / thu hồi, cùng giao dịch ghi bảng chiếu share_grant_inbound ở tenant BÊN NHẬN
//     (tenant.Within — RLS vẫn áp, không có vai BYPASSRLS) để bên nhận tra cấp quyền của mình.
//   - Authorize kiểm cả bảng chiếu bên nhận lẫn bảng hiện hành bên cấp; RecordRead ghi sổ đọc ở
//     tenant bên cấp cho MỖI lời gọi bề mặt đối tác.

import (
	"context"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	meteringport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của cấp quyền chia sẻ (docs/errors.md, mục pf/party).
var (
	ErrShareGrantNotFound = problem.New(http.StatusNotFound, "party.share_grant_not_found", "Không tìm thấy cấp quyền chia sẻ")
	ErrShareGrantState    = problem.New(http.StatusConflict, "party.share_grant_state",
		"Cấp quyền không ở trạng thái cho phép thao tác này (chỉ duyệt bản đề xuất; bản đã thu hồi không thao tác được nữa)")
	ErrPartnerNotFound = problem.New(http.StatusNotFound, "party.partner_not_found", "Bên cấp không có bản ghi này")
)

// Hạn tối đa của một cấp quyền (phải gia hạn bằng cấp quyền mới).
const maxShareValidity = 5 * 366 * 24 * time.Hour

// Sharing là use case cấp quyền chia sẻ và bề mặt đối tác danh bạ HTX.
type Sharing struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
	// Egress (G5, ADR-0024): điểm kiểm soát xuất dùng chung (pf/metering) cho kênh partner_share
	// ở RecordRead. Nil → hành vi cũ (chỉ sổ partner_read_log).
	Egress     meteringport.Checkpoint
	EgressMode meteringport.Mode
	Log        *slog.Logger
}

var (
	_ port.PartnerAccess = (*Sharing)(nil)
	_ port.ValveGrants   = (*Sharing)(nil)
	_ port.GrantLookup   = (*Sharing)(nil)
)

// ProposeShareInput là dữ liệu đề xuất cấp quyền.
type ProposeShareInput struct {
	Grantee   uuid.UUID
	Kinds     []string
	Purpose   string
	ValidFrom time.Time
	ValidTo   time.Time
	IdemKey   string
}

func normKinds(in []string) ([]string, bool) {
	out := make([]string, 0, len(in))
	for _, k := range in {
		k = strings.TrimSpace(k)
		if !port.ValidKind(k) {
			return nil, false
		}
		if !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out, len(out) > 0
}

// Propose ghi đề xuất cấp quyền: bên cấp = tenant người gọi (platform_admin).
func (s *Sharing) Propose(ctx context.Context, in ProposeShareInput) (store.Grant, bool, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return store.Grant{}, false, err
	}
	if in.IdemKey == "" {
		return store.Grant{}, false, problem.IdempotencyRequired
	}
	kinds, ok := normKinds(in.Kinds)
	switch {
	case in.Grantee == uuid.Nil:
		return store.Grant{}, false, problem.Validation.WithField("grantee_tenant_id", "bắt buộc, phải là UUID")
	case in.Grantee == p.TenantID:
		return store.Grant{}, false, problem.Validation.WithField("grantee_tenant_id", "phải khác tenant bên cấp")
	case !ok:
		return store.Grant{}, false, problem.Validation.WithField("kinds", "danh sách loại dữ liệu hợp lệ: "+strings.Join(port.ShareKinds, ", "))
	}
	purpose := strings.TrimSpace(in.Purpose)
	if n := len([]rune(purpose)); n < 10 || n > 500 {
		return store.Grant{}, false, problem.Validation.WithField("purpose", "10–500 ký tự")
	}
	now := s.Clock.Now()
	if in.ValidFrom.IsZero() {
		in.ValidFrom = now
	}
	switch {
	case in.ValidTo.IsZero() || !in.ValidTo.After(in.ValidFrom):
		return store.Grant{}, false, problem.Validation.WithField("valid_to", "bắt buộc, sau valid_from")
	case !in.ValidTo.After(now):
		return store.Grant{}, false, problem.Validation.WithField("valid_to", "phải ở tương lai")
	case in.ValidTo.Sub(in.ValidFrom) > maxShareValidity:
		return store.Grant{}, false, problem.Validation.WithField("valid_to", "tối đa 5 năm — gia hạn bằng cấp quyền mới")
	}
	id := ids.New()
	e := store.GrantEvent{ID: ids.New(), TenantID: p.TenantID, GrantID: id, Action: "proposed", Grantee: in.Grantee, Kinds: kinds,
		Purpose: purpose, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo, ProposedBy: p.ActorRef, EventTime: now, ActorRef: p.ActorRef,
		IdemKey: "share:propose:" + in.IdemKey}
	var out store.Grant
	replayed := false
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.GrantEventByIdem(ctx, tx, e.IdemKey); err != nil || prev != nil {
			if err != nil {
				return err
			}
			g, err := store.GrantByID(ctx, tx, prev.GrantID, false)
			if err != nil {
				return err
			}
			if g == nil {
				return ErrShareGrantNotFound
			}
			out, replayed = *g, true
			return nil
		}
		if err := ledger.Classify(store.InsertGrantEvent(ctx, tx, &e)); err != nil {
			return err
		}
		out = store.Grant{TenantID: p.TenantID, GrantID: id, Grantee: in.Grantee, Kinds: kinds, Purpose: purpose, ValidFrom: in.ValidFrom.UTC(),
			ValidTo: in.ValidTo.UTC(), Status: "proposed", ProposedBy: p.ActorRef, ProposedAt: now.UTC(), LastEventID: e.ID, LastSeq: e.Seq, UpdatedAt: now.UTC()}
		return store.PutGrant(ctx, tx, out)
	})
	return out, replayed, err
}

// Approve duyệt một đề xuất: người duyệt (platform_admin) ≠ người đề xuất (R-56). Cùng giao dịch
// ghi bảng chiếu ở tenant bên nhận.
func (s *Sharing) Approve(ctx context.Context, id uuid.UUID) (store.Grant, bool, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return store.Grant{}, false, err
	}
	now := s.Clock.Now()
	var out store.Grant
	replayed := false
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		g, err := store.GrantByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if g == nil {
			return ErrShareGrantNotFound
		}
		if g.Status == "active" && g.ApprovedBy != nil && *g.ApprovedBy == p.ActorRef {
			out, replayed = *g, true // gọi lặp của chính người duyệt
			return nil
		}
		if g.Status != "proposed" {
			return ErrShareGrantState
		}
		if err := authz.EnsureDistinctApproverFor(g.ProposedBy, p, authz.RolePlatformAdmin); err != nil {
			return err
		}
		if !g.ValidTo.After(now) {
			return ErrShareGrantState.WithDetail("Cấp quyền đã quá hạn valid_to — đề xuất cấp quyền mới")
		}
		e := store.GrantEvent{ID: ids.New(), TenantID: p.TenantID, GrantID: id, Action: "approved", Grantee: g.Grantee, Kinds: g.Kinds,
			Purpose: g.Purpose, ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, ProposedBy: g.ProposedBy, EventTime: now, ActorRef: p.ActorRef,
			IdemKey: "share:approve:" + id.String()}
		if err := ledger.Classify(store.InsertGrantEvent(ctx, tx, &e)); err != nil {
			return err
		}
		at := now.UTC()
		g.Status, g.ApprovedBy, g.ApprovedAt, g.LastEventID, g.LastSeq = "active", &p.ActorRef, &at, e.ID, e.Seq
		if err := store.PutGrant(ctx, tx, *g); err != nil {
			return err
		}
		if err := tenant.Within(ctx, tx, g.Grantee, func() error {
			return store.PutInbound(ctx, tx, store.Inbound{TenantID: g.Grantee, GrantID: id, Grantor: p.TenantID, Kinds: g.Kinds,
				ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, Status: "active"})
		}); err != nil {
			return err
		}
		if err := enqueueGrantEvent(ctx, tx, e); err != nil {
			return err
		}
		out = *g
		return nil
	})
	return out, replayed, err
}

// enqueueGrantEvent phát pf.party.share_grant.approved / revoked (W2.5: worker pf.kref dựng
// k_ref.share_grant_active) cùng giao dịch với sổ.
func enqueueGrantEvent(ctx context.Context, tx pgx.Tx, e store.GrantEvent) error {
	ev := outbox.Event{
		ID: ids.New(), TenantID: e.TenantID, SchemaVersion: 1, OccurredAt: e.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: "pf/party",
	}
	switch e.Action {
	case "approved":
		ev.Type = port.EventShareGrantApproved
		ev.Payload = port.ShareGrantApprovedV1{GrantID: e.GrantID, GranteeTenantID: e.Grantee, Kinds: e.Kinds,
			ValidFrom: e.ValidFrom.UTC().Format(time.RFC3339Nano), ValidTo: e.ValidTo.UTC().Format(time.RFC3339Nano),
			ActorRef: e.ActorRef, EventTime: e.EventTime.UTC().Format(time.RFC3339), Seq: e.Seq}
	case "revoked":
		ev.Type = port.EventShareGrantRevoked
		ev.Payload = port.ShareGrantRevokedV1{GrantID: e.GrantID, GranteeTenantID: e.Grantee, Kinds: e.Kinds,
			ValidFrom: e.ValidFrom.UTC().Format(time.RFC3339Nano), ValidTo: e.ValidTo.UTC().Format(time.RFC3339Nano),
			ActorRef: e.ActorRef, EventTime: e.EventTime.UTC().Format(time.RFC3339), Seq: e.Seq}
	default:
		return nil
	}
	return outbox.Enqueue(ctx, tx, ev)
}

// Revoke thu hồi (đề xuất hoặc cấp quyền đang hiệu lực). Có lý do; đọc sau thu hồi → 403.
func (s *Sharing) Revoke(ctx context.Context, id uuid.UUID, reason string) (store.Grant, bool, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return store.Grant{}, false, err
	}
	reason = strings.TrimSpace(reason)
	if n := len([]rune(reason)); n < 10 || n > 500 {
		return store.Grant{}, false, problem.Validation.WithField("reason", "10–500 ký tự")
	}
	now := s.Clock.Now()
	var out store.Grant
	replayed := false
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		g, err := store.GrantByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if g == nil {
			return ErrShareGrantNotFound
		}
		if g.Status == "revoked" {
			if g.RevokedBy != nil && *g.RevokedBy == p.ActorRef {
				out, replayed = *g, true
				return nil
			}
			return ErrShareGrantState
		}
		wasActive := g.Status == "active"
		e := store.GrantEvent{ID: ids.New(), TenantID: p.TenantID, GrantID: id, Action: "revoked", Grantee: g.Grantee, Kinds: g.Kinds,
			Purpose: g.Purpose, ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, ProposedBy: g.ProposedBy, Reason: &reason, EventTime: now,
			ActorRef: p.ActorRef, IdemKey: "share:revoke:" + id.String()}
		if err := ledger.Classify(store.InsertGrantEvent(ctx, tx, &e)); err != nil {
			return err
		}
		at := now.UTC()
		g.Status, g.RevokedBy, g.RevokedAt, g.RevokeReason, g.LastEventID, g.LastSeq = "revoked", &p.ActorRef, &at, &reason, e.ID, e.Seq
		if err := store.PutGrant(ctx, tx, *g); err != nil {
			return err
		}
		if wasActive {
			if err := tenant.Within(ctx, tx, g.Grantee, func() error {
				return store.PutInbound(ctx, tx, store.Inbound{TenantID: g.Grantee, GrantID: id, Grantor: p.TenantID, Kinds: g.Kinds,
					ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, Status: "revoked"})
			}); err != nil {
				return err
			}
		}
		if err := enqueueGrantEvent(ctx, tx, e); err != nil {
			return err
		}
		out = *g
		return nil
	})
	return out, replayed, err
}

// ListShareGrants liệt kê cấp quyền của tenant bên cấp (platform_admin).
func (s *Sharing) ListShareGrants(ctx context.Context, status string, after uuid.UUID, limit int) ([]store.Grant, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return nil, err
	}
	if status != "" && status != "proposed" && status != "active" && status != "revoked" {
		return nil, problem.Validation.WithField("status", "proposed | active | revoked")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.Grant
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListGrants(ctx, tx, status, after, limit)
		return e
	})
	return out, err
}

// GetShareGrant đọc một cấp quyền của tenant bên cấp.
func (s *Sharing) GetShareGrant(ctx context.Context, id uuid.UUID) (store.Grant, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return store.Grant{}, err
	}
	var out *store.Grant
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.GrantByID(ctx, tx, id, false)
		return e
	})
	if err != nil {
		return store.Grant{}, err
	}
	if out == nil {
		return store.Grant{}, ErrShareGrantNotFound
	}
	return *out, nil
}

// ShareGrantReads trả sổ đọc của bên nhận với một cấp quyền (platform_admin bên cấp).
func (s *Sharing) ShareGrantReads(ctx context.Context, id uuid.UUID, after int64, limit int) ([]store.ReadLog, error) {
	if _, err := s.GetShareGrant(ctx, id); err != nil {
		return nil, err
	}
	p, _ := authz.FromContext(ctx)
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out []store.ReadLog
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ReadLogs(ctx, tx, id, after, limit)
		return e
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Phía bên nhận
// ---------------------------------------------------------------------------

func toShareGrant(in store.Inbound) port.ShareGrant {
	return port.ShareGrant{ID: in.GrantID, Grantor: in.Grantor, Grantee: in.TenantID, Kinds: in.Kinds, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo}
}

// activeFor trả cấp quyền đang hiệu lực (đã kiểm hai phía — store.VerifiedActive) của bên nhận
// grantee cho kind.
func (s *Sharing) activeFor(ctx context.Context, grantee uuid.UUID, kind string, grantor uuid.UUID, at time.Time) ([]port.ShareGrant, error) {
	var out []port.ShareGrant
	err := tenant.WithTenantTx(ctx, s.DB, grantee, func(tx pgx.Tx) error {
		rows, err := store.VerifiedActive(ctx, tx, grantee, kind, grantor, at)
		for _, in := range rows {
			out = append(out, toShareGrant(in))
		}
		return err
	})
	return out, err
}

// PartnerGrants liệt kê cấp quyền đang hiệu lực mà tenant người gọi (bên nhận, vai mkg_sync) nhận.
func (s *Sharing) PartnerGrants(ctx context.Context) ([]port.ShareGrant, error) {
	p, err := authz.Require(ctx, port.RolePartnerSync)
	if err != nil {
		return nil, err
	}
	return s.activeFor(ctx, p.TenantID, "", uuid.Nil, s.Clock.Now())
}

// Authorize cài đặt port.PartnerAccess.
func (s *Sharing) Authorize(ctx context.Context, kind string, grantor uuid.UUID) (port.ShareGrant, error) {
	p, err := authz.Require(ctx, port.RolePartnerSync)
	if err != nil {
		return port.ShareGrant{}, err
	}
	if !port.ValidKind(kind) {
		return port.ShareGrant{}, port.ErrShareGrantMissing
	}
	gs, err := s.activeFor(ctx, p.TenantID, kind, grantor, s.Clock.Now())
	if err != nil {
		return port.ShareGrant{}, err
	}
	if len(gs) == 0 {
		return port.ShareGrant{}, port.ErrShareGrantMissing
	}
	for _, g := range gs[1:] {
		if g.Grantor != gs[0].Grantor {
			return port.ShareGrant{}, port.ErrShareGrantorAmbiguous
		}
	}
	return gs[0], nil
}

// ActiveGrant cài đặt port.GrantLookup (G5, ADR-0022 mục 3) cho cmd/api-public: không kiểm vai OIDC
// (bên gọi là khách hàng API đã xác thực bằng khóa) nên CHỈ nhận loại sản phẩm dữ liệu dp_* — các
// loại khác vẫn phải qua Authorize (vai mkg_sync). Cùng truy vấn hai phía như Authorize: bảng chiếu
// bên nhận + bảng hiện hành bên cấp, trạng thái active (chưa thu hồi), at ∈ [valid_from, valid_to).
func (s *Sharing) ActiveGrant(ctx context.Context, grantee, grantor uuid.UUID, kind string, at time.Time) (port.ShareGrant, error) {
	return grantlookup.New(s.DB, s.Clock).ActiveGrant(ctx, grantee, grantor, kind, at)
}

// ActiveGrantors (apiclient/public.GrantorLister): mọi bên cấp đang có cấp quyền dp_* loại kind cho
// grantee — danh mục /v1/data-products.
func (s *Sharing) ActiveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error) {
	return grantlookup.New(s.DB, s.Clock).ActiveGrantors(ctx, grantee, kind, at)
}

// readerOf trả (bên nhận, người đọc) của một lần đọc: người dùng OIDC của tenant bên nhận (bề mặt
// /v1/partner/*) hoặc khách hàng Public API (apiclientport.Caller — tenant bên thứ ba lấy từ bản ghi
// khóa, người đọc = mã khách hàng API). viaAPI = true với khách hàng Public API.
func readerOf(ctx context.Context) (tid, actor uuid.UUID, viaAPI, ok bool) {
	if p, ok := authz.FromContext(ctx); ok {
		return p.TenantID, p.ActorRef, false, true
	}
	if c, ok := apiclientport.CallerFrom(ctx); ok && c.Client.ID != uuid.Nil {
		return c.Client.ClientTenantID, c.Client.ID, true, true
	}
	return uuid.Nil, uuid.Nil, false, false
}

// RecordRead cài đặt port.PartnerAccess: một dòng partner_read_log ở tenant bên cấp. Với bề mặt
// /v1/partner/* (người gọi OIDC), lần đọc cũng đi qua điểm kiểm soát xuất kênh partner_share
// (ADR-0024 mục 3, P2) TRƯỚC khi ghi sổ đọc; enforce + từ chối → không ghi, không trả dữ liệu. Lần
// đọc của khách hàng Public API không qua kênh partner_share: api-public đã ghi egress public_api
// cùng giao dịch với sổ sử dụng (ADR-0023) — không đếm hai lần.
func (s *Sharing) RecordRead(ctx context.Context, g port.ShareGrant, r port.PartnerRead) error {
	tid, actor, viaAPI, ok := readerOf(ctx)
	if !ok || tid != g.Grantee || g.Grantor == uuid.Nil {
		return port.ErrPartnerAuditUnavailable
	}
	if viaAPI {
		// Khách hàng Public API (K-05): cùng một ngữ nghĩa với cmd/api-public (bộ điều hợp không vault).
		return grantlookup.New(s.DB, s.Clock).RecordRead(ctx, g, r)
	}
	var q *string
	if r.Query != "" {
		v := r.Query
		if len(v) > 500 {
			v = v[:500]
		}
		q = &v
	}
	row := store.ReadLog{ID: ids.New(), TenantID: g.Grantor, GrantID: g.ID, Grantee: g.Grantee, Kind: r.Kind, Endpoint: r.Endpoint, Query: q,
		Items: r.Items, EventTime: s.Clock.Now(), ActorRef: actor, IdemKey: "read:" + ids.New().String()}
	if err := meteringport.Guard(ctx, s.Egress, s.EgressMode, meteringport.Egress{
		SourceID: row.ID, Channel: meteringport.ChannelPartnerShare, DataTenantID: g.Grantor,
		CounterpartyKind: meteringport.CounterpartyTenant, CounterpartyRef: g.Grantee, Product: r.Kind,
		Records: int64(r.Items), OccurredAt: row.EventTime, ActorRef: actor, TraceID: telemetry.TraceID(ctx),
	}, s.Log); err != nil {
		return err
	}
	if err := tenant.WithTenantTx(ctx, s.DB, g.Grantor, func(tx pgx.Tx) error { return store.InsertReadLog(ctx, tx, &row) }); err != nil {
		return port.ErrPartnerAuditUnavailable.WithCause(err)
	}
	return nil
}

// ValveGrantors cài đặt port.ValveGrants (consumer van — không có người gọi).
func (s *Sharing) ValveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error) {
	gs, err := s.activeFor(ctx, grantee, kind, uuid.Nil, at)
	if err != nil {
		return nil, err
	}
	var out []uuid.UUID
	for _, g := range gs {
		if !slices.Contains(out, g.Grantor) {
			out = append(out, g.Grantor)
		}
	}
	return out, nil
}

// PartnerCooperativePage là một trang danh bạ HTX cho bên nhận.
type PartnerCooperativePage struct {
	Items []store.CooperativeEntry
	Next  *uuid.UUID
}

// PartnerCooperatives trả danh bạ HTX của bên cấp (loại cooperative_directory), ghi sổ đọc.
func (s *Sharing) PartnerCooperatives(ctx context.Context, grantor, after uuid.UUID, limit int, query string) (PartnerCooperativePage, error) {
	g, err := s.Authorize(ctx, port.KindCooperativeDirectory, grantor)
	if err != nil {
		return PartnerCooperativePage{}, err
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var pg PartnerCooperativePage
	err = tenant.WithTenantTx(ctx, s.DB, g.Grantor, func(tx pgx.Tx) error {
		var e error
		pg.Items, e = store.CooperativeEntries(ctx, tx, after, limit+1)
		return e
	})
	if err != nil {
		return PartnerCooperativePage{}, err
	}
	if len(pg.Items) > limit {
		pg.Items = pg.Items[:limit]
		n := pg.Items[limit-1].Ref
		pg.Next = &n
	}
	if err := s.RecordRead(ctx, g, port.PartnerRead{Kind: port.KindCooperativeDirectory, Endpoint: "GET /v1/partner/cooperatives",
		Query: query, Items: len(pg.Items)}); err != nil {
		return PartnerCooperativePage{}, err
	}
	return pg, nil
}

// PartnerCooperative trả một HTX của bên cấp; không có → 404 (vẫn ghi sổ đọc với 0 dòng).
func (s *Sharing) PartnerCooperative(ctx context.Context, grantor, ref uuid.UUID) (store.CooperativeEntry, error) {
	g, err := s.Authorize(ctx, port.KindCooperativeDirectory, grantor)
	if err != nil {
		return store.CooperativeEntry{}, err
	}
	var e *store.CooperativeEntry
	if err := tenant.WithTenantTx(ctx, s.DB, g.Grantor, func(tx pgx.Tx) error {
		var er error
		e, er = store.CooperativeEntryByRef(ctx, tx, ref)
		return er
	}); err != nil {
		return store.CooperativeEntry{}, err
	}
	n := 0
	if e != nil {
		n = 1
	}
	if err := s.RecordRead(ctx, g, port.PartnerRead{Kind: port.KindCooperativeDirectory, Endpoint: "GET /v1/partner/cooperatives/{ref}",
		Query: "ref=" + ref.String(), Items: n}); err != nil {
		return store.CooperativeEntry{}, err
	}
	if e == nil {
		return store.CooperativeEntry{}, ErrPartnerNotFound
	}
	return *e, nil
}
