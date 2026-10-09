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
//     (tenant.Within với bằng chứng tenant.MintGrant bên cấp → bên nhận — RLS vẫn áp, không có vai
//     BYPASSRLS) để bên nhận tra cấp quyền của mình.
//   - H4: gói này (cùng party/store) là nơi duy nhất đúc tenant.Grant (archlint agtenantgate): mọi
//     ShareGrant trả ra mang Access; ValveGrantors trả bằng chứng đi từ tenant phong bì sang bên cấp.
//   - Authorize kiểm cả bảng chiếu bên nhận lẫn bảng hiện hành bên cấp; RecordRead ghi sổ đọc ở
//     tenant bên cấp cho MỖI lời gọi bề mặt đối tác.

import (
	"context"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port/grantlookup"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/shareread"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi của cấp quyền chia sẻ (docs/errors.md, mục pf/party).
var (
	ErrShareGrantNotFound = problem.New(http.StatusNotFound, "party.share_grant_not_found", "Không tìm thấy cấp quyền chia sẻ")
	ErrShareGrantState    = problem.New(http.StatusConflict, "party.share_grant_state",
		"Cấp quyền không ở trạng thái cho phép thao tác này (chỉ duyệt bản đề xuất; bản đã thu hồi không thao tác được nữa)")
	ErrPartnerNotFound = problem.New(http.StatusNotFound, "party.partner_not_found", "Bên cấp không có bản ghi này")
	// ErrShareTableNotShared (W4.1, ADR-0030 mục 2): module_table của cấp phép intra_platform phải có trong shares
	// của module.yaml chủ (danh mục modreg) — chỉ bảng đó mới mang nhánh share_scope (R-DB-08).
	ErrShareTableNotShared = problem.New(http.StatusUnprocessableEntity, "party.share_table_not_shared",
		"Bảng không được module chủ khai trong shares — không cấp phép chia sẻ được")
)

// Hạn tối đa của một cấp quyền (phải gia hạn bằng cấp quyền mới).
const maxShareValidity = 5 * 366 * 24 * time.Hour

// ShareReviewInterval là chu kỳ rà soát cấp phép định kỳ (R-SEC-04): cấp phép đang hiệu lực đến hạn rà soát khi
// lần rà soát cuối (hoặc lúc duyệt) cách hiện tại ≥ chu kỳ này. Một quý — chờ khách hàng chốt (C-10).
const ShareReviewInterval = 90 * 24 * time.Hour

var (
	reModuleTable = regexp.MustCompile(`^((?:pf|mkg)/[a-z][a-z0-9_]{0,39}):([a-z][a-z0-9_]{0,59})$`)
	rePurposeCode = regexp.MustCompile(`^[a-z][a-z0-9-]{0,39}(\.[a-z][a-z0-9-]{0,39}){0,3}$`)
)

// Sharing là use case cấp quyền chia sẻ và bề mặt đối tác danh bạ HTX.
type Sharing struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
	// Egress (G5, ADR-0024): điểm kiểm soát xuất dùng chung (pf/metering) cho kênh partner_share
	// ở RecordRead. Nil → hành vi cũ (chỉ sổ partner_read_log).
	Egress     meter.Checkpoint
	EgressMode meter.Mode
	Log        *slog.Logger
	// Approvals (tùy chọn, W6.1): cap/workflow — đề xuất/duyệt/thu hồi đề xuất ghi thêm yêu cầu và quyết định vào
	// sổ pf_workflow CÙNG giao dịch (bốn mắt kiểm lại ở DB). Nil → chỉ luồng cũ.
	Approvals port.FourEyes
	// Profiles (W3.3): vị trí hồ sơ HTX (pf/cooperative) cho danh bạ HTX của bên nhận. Nil → toạ độ null.
	Profiles port.CoopProfiles
}

var (
	_ port.PartnerAccess = (*Sharing)(nil)
	_ port.ValveGrants   = (*Sharing)(nil)
	_ port.GrantLookup   = (*Sharing)(nil)
	_ port.ContextEpochs = (*Sharing)(nil)
)

// ProposeShareInput là dữ liệu đề xuất cấp quyền.
type ProposeShareInput struct {
	Grantee   uuid.UUID
	Kinds     []string
	Purpose   string
	ValidFrom time.Time
	ValidTo   time.Time
	IdemKey   string
	// Điều khoản v2 (W4.1, ADR-0030 mục 3). GrantClass rỗng = partner (hành vi cũ).
	GrantClass   string
	ModuleTable  string    // '<module>:<share>', bắt buộc với intra_platform
	ScopeNodeRef uuid.UUID // Nút của đơn vị chủ, bắt buộc với intra_platform
	PurposeCode  string    // bắt buộc với intra_platform
	BasisRef     string    // mã thỏa thuận, bắt buộc với intra_platform
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// shareTerms kiểm điều khoản v2 (không đọc DB) và trả điều khoản + danh sách loại đã chuẩn hóa.
//   - partner (mặc định): ≥ 1 loại có tên (ADR-0020), không bảng/Nút; purpose_code, basis_ref tùy chọn.
//   - intra_platform: KHÔNG loại; module_table có trong shares của module chủ (modreg), Nút, mục đích, căn cứ bắt buộc.
func shareTerms(in ProposeShareInput) (store.Terms, []string, error) {
	t := store.Terms{Class: strings.TrimSpace(in.GrantClass)}
	if t.Class == "" {
		t.Class = store.ClassPartner
	}
	purpose, basis := strings.TrimSpace(in.PurposeCode), strings.TrimSpace(in.BasisRef)
	if purpose != "" && !rePurposeCode.MatchString(purpose) {
		return t, nil, problem.Validation.WithField("purpose_code", "mã mục đích dạng khuyen-nong hoặc mrv.evidence (chữ thường, số, gạch nối)")
	}
	if n := len([]rune(basis)); basis != "" && (n < 3 || n > 200) {
		return t, nil, problem.Validation.WithField("basis_ref", "3–200 ký tự (mã thỏa thuận chia sẻ)")
	}
	t.PurposeCode, t.BasisRef = optStr(purpose), optStr(basis)
	switch t.Class {
	case store.ClassPartner:
		if strings.TrimSpace(in.ModuleTable) != "" || in.ScopeNodeRef != uuid.Nil {
			return t, nil, problem.Validation.WithField("module_table", "chỉ dùng với grant_class = intra_platform")
		}
		kinds, ok := normKinds(in.Kinds)
		if !ok {
			return t, nil, problem.Validation.WithField("kinds", "danh sách loại dữ liệu hợp lệ: "+strings.Join(port.ShareKinds, ", "))
		}
		return t, kinds, nil
	case store.ClassIntraPlatform:
		if len(in.Kinds) > 0 {
			return t, nil, problem.Validation.WithField("kinds", "cấp phép intra_platform không dùng kinds — phạm vi là module_table × Nút")
		}
		mt := strings.TrimSpace(in.ModuleTable)
		m := reModuleTable.FindStringSubmatch(mt)
		switch {
		case m == nil:
			return t, nil, problem.Validation.WithField("module_table", "bắt buộc, dạng <module>:<share>, vd pf/cooperative:household")
		case !modreg.Shared(m[1], m[2]):
			return t, nil, ErrShareTableNotShared.WithField("module_table", mt+" không có trong shares của "+m[1])
		case in.ScopeNodeRef == uuid.Nil:
			return t, nil, problem.Validation.WithField("scope_node_ref", "bắt buộc — Nút phạm vi của đơn vị chủ")
		case purpose == "":
			return t, nil, problem.Validation.WithField("purpose_code", "bắt buộc với intra_platform")
		case basis == "":
			return t, nil, problem.Validation.WithField("basis_ref", "bắt buộc với intra_platform — căn cứ thỏa thuận chia sẻ (R-SEC-04)")
		}
		node := in.ScopeNodeRef
		t.ModuleTable, t.ScopeNodeRef = &mt, &node
		return t, []string{}, nil
	default:
		return t, nil, problem.Validation.WithField("grant_class", "intra_platform | partner")
	}
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

// Propose ghi đề xuất cấp quyền: bên cấp = tenant người gọi. Chỉ quản trị viên của ĐƠN VỊ CHỦ dữ liệu
// (platform_admin của tenant bên cấp — R-SEC-04) đề xuất; Nút phạm vi của cấp phép intra_platform phải là Nút
// đang hoạt động của chính đơn vị đó.
func (s *Sharing) Propose(ctx context.Context, in ProposeShareInput) (store.Grant, bool, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return store.Grant{}, false, err
	}
	if in.IdemKey == "" {
		return store.Grant{}, false, problem.IdempotencyRequired
	}
	switch in.Grantee {
	case uuid.Nil:
		return store.Grant{}, false, problem.Validation.WithField("grantee_tenant_id", "bắt buộc, phải là UUID")
	case p.TenantID:
		return store.Grant{}, false, problem.Validation.WithField("grantee_tenant_id", "phải khác tenant bên cấp")
	}
	terms, kinds, err := shareTerms(in)
	if err != nil {
		return store.Grant{}, false, err
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
		if terms.ScopeNodeRef != nil {
			n, err := store.OrgByRef(ctx, tx, *terms.ScopeNodeRef, false)
			if err != nil {
				return err
			}
			if n == nil || !n.Active {
				return problem.Validation.WithField("scope_node_ref", "không phải Nút đang hoạt động của đơn vị chủ (tenant bên cấp)")
			}
		}
		if err := ledger.Classify(store.InsertGrantEvent(ctx, tx, &e)); err != nil {
			return err
		}
		if err := ledger.Classify(store.InsertGrantTerms(ctx, tx, p.TenantID, id, p.ActorRef, terms, now, "share:terms:"+in.IdemKey)); err != nil {
			return err
		}
		out = store.Grant{TenantID: p.TenantID, GrantID: id, Grantee: in.Grantee, Kinds: kinds, Purpose: purpose, ValidFrom: in.ValidFrom.UTC(),
			ValidTo: in.ValidTo.UTC(), Status: "proposed", ProposedBy: p.ActorRef, ProposedAt: now.UTC(), LastEventID: e.ID, LastSeq: e.Seq, UpdatedAt: now.UTC(),
			Terms: &terms}
		if err := store.PutGrant(ctx, tx, out); err != nil {
			return err
		}
		if s.Approvals != nil {
			return s.Approvals.Submit(db.JoinTx(ctx, tx), port.ShareGrantApprovalKind, id, p.ActorRef, now)
		}
		return nil
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
		// W6.1: quyết định vào sổ cap/workflow (đề xuất trước W6.1 chưa có yêu cầu — workflow lập bù).
		if s.Approvals != nil {
			if err := s.Approvals.Decide(db.JoinTx(ctx, tx), port.ShareGrantApprovalKind, id, g.ProposedBy, p.ActorRef, port.FourEyesApproved, "", now); err != nil {
				return err
			}
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
		// Bảng chiếu bên nhận là đường của partner (/v1/partner/*, Public API, van); intra_platform đọc tại chỗ qua
		// k_ref.share_scope (W4.2), không qua bảng chiếu này.
		if g.Class() == store.ClassPartner {
			if err := tenant.Within(ctx, tx, tenant.MintGrant(p.TenantID, g.Grantee), func() error {
				return store.PutInbound(ctx, tx, store.Inbound{TenantID: g.Grantee, GrantID: id, Grantor: p.TenantID, Kinds: g.Kinds,
					ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, Status: "active"})
			}); err != nil {
				return err
			}
		}
		if err := enqueueGrantEvent(ctx, tx, e, g.Terms); err != nil {
			return err
		}
		out = *g
		return nil
	})
	return out, replayed, err
}

// enqueueGrantEvent phát pf.party.share_grant.approved / revoked (W2.5: worker pf.kref dựng
// k_ref.share_grant_active) cùng giao dịch với sổ. Điều khoản v2 (W4.1) là trường tùy chọn của payload.
func enqueueGrantEvent(ctx context.Context, tx pgx.Tx, e store.GrantEvent, t *store.Terms) error {
	ev := outbox.Event{
		ID: ids.New(), TenantID: e.TenantID, SchemaVersion: 1, OccurredAt: e.EventTime,
		TraceID: telemetry.TraceID(ctx), Producer: "pf/party",
	}
	var class, table, purpose, basis string
	var node *uuid.UUID
	if t != nil {
		class, table, purpose, basis, node = t.Class, deref(t.ModuleTable), deref(t.PurposeCode), deref(t.BasisRef), t.ScopeNodeRef
	}
	switch e.Action {
	case "approved":
		ev.Type = port.EventShareGrantApproved
		ev.Payload = port.ShareGrantApprovedV1{GrantID: e.GrantID, GranteeTenantID: e.Grantee, Kinds: e.Kinds,
			ValidFrom: e.ValidFrom.UTC().Format(time.RFC3339Nano), ValidTo: e.ValidTo.UTC().Format(time.RFC3339Nano),
			ActorRef: e.ActorRef, EventTime: e.EventTime.UTC().Format(time.RFC3339), Seq: e.Seq,
			GrantClass: class, ModuleTable: table, ScopeNodeRef: node, PurposeCode: purpose, BasisRef: basis}
	case "revoked":
		ev.Type = port.EventShareGrantRevoked
		ev.Payload = port.ShareGrantRevokedV1{GrantID: e.GrantID, GranteeTenantID: e.Grantee, Kinds: e.Kinds,
			ValidFrom: e.ValidFrom.UTC().Format(time.RFC3339Nano), ValidTo: e.ValidTo.UTC().Format(time.RFC3339Nano),
			ActorRef: e.ActorRef, EventTime: e.EventTime.UTC().Format(time.RFC3339), Seq: e.Seq,
			GrantClass: class, ModuleTable: table, ScopeNodeRef: node, PurposeCode: purpose, BasisRef: basis}
	default:
		return nil
	}
	return outbox.Enqueue(ctx, tx, ev)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
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
		if err := s.revokeTx(ctx, tx, p.TenantID, p.ActorRef, g, reason, now); err != nil {
			return err
		}
		out = *g
		return nil
	})
	return out, replayed, err
}

// revokeTx thu hồi g (đã khóa, chưa thu hồi) trong tx của tenant bên cấp: sổ + bảng chiếu + bảng chiếu bên nhận
// (partner) + epoch ngữ cảnh của đơn vị xem (cấp phép đang hiệu lực) + sự kiện. g được cập nhật tại chỗ.
func (s *Sharing) revokeTx(ctx context.Context, tx pgx.Tx, grantor, actor uuid.UUID, g *store.Grant, reason string, now time.Time) error {
	id := g.GrantID
	wasActive := g.Status == "active"
	// W6.1: thu hồi đề xuất chưa duyệt = hủy yêu cầu duyệt trong cap/workflow (không cần bốn mắt).
	if !wasActive && s.Approvals != nil {
		if err := s.Approvals.Decide(db.JoinTx(ctx, tx), port.ShareGrantApprovalKind, id, g.ProposedBy, actor, port.FourEyesCancelled, reason, now); err != nil {
			return err
		}
	}
	e := store.GrantEvent{ID: ids.New(), TenantID: grantor, GrantID: id, Action: "revoked", Grantee: g.Grantee, Kinds: g.Kinds,
		Purpose: g.Purpose, ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, ProposedBy: g.ProposedBy, Reason: &reason, EventTime: now,
		ActorRef: actor, IdemKey: "share:revoke:" + id.String()}
	if err := ledger.Classify(store.InsertGrantEvent(ctx, tx, &e)); err != nil {
		return err
	}
	at := now.UTC()
	g.Status, g.RevokedBy, g.RevokedAt, g.RevokeReason, g.LastEventID, g.LastSeq = "revoked", &actor, &at, &reason, e.ID, e.Seq
	if err := store.PutGrant(ctx, tx, *g); err != nil {
		return err
	}
	if wasActive {
		// Bằng chứng bên cấp → bên nhận (H4): ghi ở tenant BÊN NHẬN dưới RLS, không BYPASSRLS.
		if err := tenant.Within(ctx, tx, tenant.MintGrant(grantor, g.Grantee), func() error {
			if g.Class() == store.ClassPartner {
				if err := store.PutInbound(ctx, tx, store.Inbound{TenantID: g.Grantee, GrantID: id, Grantor: grantor, Kinds: g.Kinds,
					ValidFrom: g.ValidFrom, ValidTo: g.ValidTo, Status: "revoked"}); err != nil {
					return err
				}
			}
			return bumpViewerEpoch(ctx, tx, g.Grantee, grantor, id, actor, now)
		}); err != nil {
			return err
		}
	}
	return enqueueGrantEvent(ctx, tx, e, g.Terms)
}

// bumpViewerEpoch tăng epoch ngữ cảnh của đơn vị xem (tx đang ở ngữ cảnh tenant viewer) và phát
// pf.party.context_epoch.bumped (tenant phong bì = đơn vị xem) — kiến trúc v4.1 §17.1, ADR-0030 mục 3.
func bumpViewerEpoch(ctx context.Context, tx pgx.Tx, viewer, grantor, grantID, actor uuid.UUID, now time.Time) error {
	const cause = "share_grant_revoked"
	epoch, err := store.BumpContextEpoch(ctx, tx, viewer, cause, grantID, grantor)
	if err != nil {
		return err
	}
	return outbox.Enqueue(ctx, tx, outbox.Event{
		ID: ids.New(), TenantID: viewer, Type: port.EventContextEpochBumped, SchemaVersion: 1, OccurredAt: now,
		TraceID: telemetry.TraceID(ctx), Producer: "pf/party",
		Payload: port.ContextEpochBumpedV1{Epoch: epoch, Cause: cause, GrantID: grantID, GrantorTenant: grantor, ActorRef: actor,
			EventTime: now.UTC().Format(time.RFC3339)},
	})
}

// RecordShareRead là shareread.Sink của pf/party (W4.2, ADR-0030 mục 4): ghi sự kiện gộp
// pf.party.share_read.recorded ở tenant ĐƠN VỊ XEM (outbox, cùng giao dịch) cho một yêu cầu đã đọc tại chỗ dữ liệu
// của đơn vị chủ qua cấp phép. Lỗi → middleware trả 503, không trả dữ liệu (đóng khi lỗi).
func (s *Sharing) RecordShareRead(ctx context.Context, e shareread.Event) error {
	if e.ViewerTenant == uuid.Nil || len(e.Reads) == 0 {
		return problem.Internal.WithDetail("nhật ký đọc chia sẻ thiếu đơn vị xem hoặc bản ghi")
	}
	reads := make([]port.ShareReadV1, 0, len(e.Reads))
	for _, r := range e.Reads {
		reads = append(reads, port.ShareReadV1{ModuleTable: r.ModuleTable, OwnerTenant: r.OwnerTenant, Rows: r.Rows})
	}
	now := s.Clock.Now()
	return tenant.WithTenantTx(ctx, s.DB, e.ViewerTenant, func(tx pgx.Tx) error {
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: e.ViewerTenant, Type: port.EventShareReadRecorded, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: "pf/party",
			Payload: port.ShareReadRecordedV1{ActorRef: e.ActorRef, PurposeCode: e.Purpose, Method: e.Method, Route: e.Route, Reads: reads,
				EventTime: now.UTC().Format(time.RFC3339)},
		})
	})
}

// ContextEpoch cài đặt port.ContextEpochs: epoch ngữ cảnh hiện tại của tenant (0 = chưa từng tăng).
func (s *Sharing) ContextEpoch(ctx context.Context, tid uuid.UUID) (int64, error) {
	var epoch int64
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		var e error
		epoch, e = store.ContextEpoch(ctx, tx)
		return e
	})
	return epoch, err
}

// ---------------------------------------------------------------------------
// Rà soát cấp phép định kỳ (R-SEC-04)
// ---------------------------------------------------------------------------

// ReviewInput là một quyết định rà soát: retain (giữ) | revoke (thu hồi ngay, note là lý do).
type ReviewInput struct {
	Decision string
	Note     string
	IdemKey  string
}

// ReviewShareGrant ghi một quyết định rà soát định kỳ cho cấp phép đang hiệu lực của đơn vị chủ (platform_admin
// bên cấp). Sổ share_grant_review chỉ ghi thêm; retain đặt lại hạn rà soát (last_reviewed_at), revoke thu hồi
// trong CÙNG giao dịch (tăng epoch ngữ cảnh của đơn vị xem như Revoke).
func (s *Sharing) ReviewShareGrant(ctx context.Context, id uuid.UUID, in ReviewInput) (store.Review, store.Grant, bool, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return store.Review{}, store.Grant{}, false, err
	}
	if in.IdemKey == "" {
		return store.Review{}, store.Grant{}, false, problem.IdempotencyRequired
	}
	note := strings.TrimSpace(in.Note)
	switch n := len([]rune(note)); {
	case in.Decision != "retain" && in.Decision != "revoke":
		return store.Review{}, store.Grant{}, false, problem.Validation.WithField("decision", "retain | revoke")
	case in.Decision == "revoke" && note == "":
		return store.Review{}, store.Grant{}, false, problem.Validation.WithField("note", "bắt buộc khi thu hồi (lý do, 10–500 ký tự)")
	case note != "" && (n < 10 || n > 500):
		return store.Review{}, store.Grant{}, false, problem.Validation.WithField("note", "10–500 ký tự")
	}
	now := s.Clock.Now()
	r := store.Review{ID: ids.New(), GrantID: id, Decision: in.Decision, Note: optStr(note), EventTime: now, ActorRef: p.ActorRef,
		IdemKey: "share:review:" + in.IdemKey}
	var g *store.Grant
	replayed := false
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ReviewByIdem(ctx, tx, r.IdemKey); err != nil || prev != nil {
			if err != nil {
				return err
			}
			if prev.GrantID != id {
				return problem.Validation.WithDetail("Idempotency-Key đã dùng cho cấp phép khác")
			}
			r, replayed = *prev, true
			g, err = store.GrantByID(ctx, tx, id, false)
			return err
		}
		var err error
		if g, err = store.GrantByID(ctx, tx, id, true); err != nil {
			return err
		}
		if g == nil {
			return ErrShareGrantNotFound
		}
		if g.Status != "active" {
			return ErrShareGrantState.WithDetail("Chỉ rà soát cấp phép đang hiệu lực")
		}
		if err := ledger.Classify(store.InsertReview(ctx, tx, p.TenantID, &r)); err != nil {
			return err
		}
		at := now.UTC()
		g.LastReviewedAt, g.LastReviewedBy = &at, &p.ActorRef
		if in.Decision == "revoke" {
			return s.revokeTx(ctx, tx, p.TenantID, p.ActorRef, g, note, now)
		}
		return store.PutGrant(ctx, tx, *g)
	})
	if err != nil {
		return store.Review{}, store.Grant{}, false, err
	}
	if g == nil {
		return store.Review{}, store.Grant{}, false, ErrShareGrantNotFound
	}
	return r, *g, replayed, nil
}

// ShareGrantsDue liệt kê cấp phép đang hiệu lực của đơn vị chủ đến hạn rà soát định kỳ trong within tới (0 = đã
// đến hạn): lần rà soát cuối (hoặc lúc duyệt) + ShareReviewInterval ≤ now + within.
func (s *Sharing) ShareGrantsDue(ctx context.Context, within time.Duration, after uuid.UUID, limit int) ([]store.Grant, error) {
	p, err := authz.Require(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return nil, err
	}
	if within < 0 || within > 366*24*time.Hour {
		return nil, problem.Validation.WithField("within_days", "0–366")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	before := s.Clock.Now().Add(within - ShareReviewInterval)
	var out []store.Grant
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.GrantsDueForReview(ctx, tx, before, after, limit)
		return e
	})
	return out, err
}

// ShareGrantReviews trả sổ rà soát của một cấp phép (platform_admin bên cấp).
func (s *Sharing) ShareGrantReviews(ctx context.Context, id uuid.UUID) ([]store.Review, error) {
	if _, err := s.GetShareGrant(ctx, id); err != nil {
		return nil, err
	}
	p, _ := authz.FromContext(ctx)
	var out []store.Review
	err := tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.Reviews(ctx, tx, id)
		return e
	})
	return out, err
}

// ReviewDue trả hạn rà soát kế tiếp của một cấp phép đang hiệu lực (nil với cấp phép chưa duyệt / đã thu hồi).
func ReviewDue(g store.Grant) *time.Time {
	if g.Status != "active" {
		return nil
	}
	base := g.ApprovedAt
	if g.LastReviewedAt != nil {
		base = g.LastReviewedAt
	}
	if base == nil {
		return nil
	}
	t := base.Add(ShareReviewInterval)
	return &t
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

// toShareGrant chỉ gọi với dòng đã kiểm hai phía (store.VerifiedActive): đúc bằng chứng H4 bên nhận → bên cấp.
func toShareGrant(in store.Inbound) port.ShareGrant {
	return port.ShareGrant{ID: in.GrantID, Grantor: in.Grantor, Grantee: in.TenantID, Kinds: in.Kinds, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo,
		Access: tenant.MintGrant(in.TenantID, in.Grantor)}
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
// grantlookup đã kiểm hai phía nhưng không đúc bằng chứng H4 (chờ chuyển ra adapter — review M9): đúc ở đây.
func (s *Sharing) ActiveGrant(ctx context.Context, grantee, grantor uuid.UUID, kind string, at time.Time) (port.ShareGrant, error) {
	g, err := grantlookup.New(s.DB, s.Clock).ActiveGrant(ctx, grantee, grantor, kind, at)
	if err != nil {
		return g, err
	}
	g.Access = tenant.MintGrant(g.Grantee, g.Grantor)
	return g, nil
}

// ActiveGrantors (apiclient/public.GrantorLister): mọi bên cấp đang có cấp quyền dp_* loại kind cho
// grantee — danh mục /v1/data-products.
func (s *Sharing) ActiveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error) {
	return grantlookup.New(s.DB, s.Clock).ActiveGrantors(ctx, grantee, kind, at)
}

// readerOf trả (bên nhận, người đọc) của một lần đọc: người dùng OIDC của tenant bên nhận (bề mặt
// /v1/partner/*) hoặc khách hàng Public API (authz.APIClient — tenant bên thứ ba lấy từ bản ghi
// khóa, người đọc = mã khách hàng API). viaAPI = true với khách hàng Public API.
func readerOf(ctx context.Context) (tid, actor uuid.UUID, viaAPI, ok bool) {
	if p, ok := authz.FromContext(ctx); ok {
		return p.TenantID, p.ActorRef, false, true
	}
	if c, ok := authz.APIClientFrom(ctx); ok && c.ClientID != uuid.Nil {
		return c.TenantID, c.ClientID, true, true
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
	if err := meter.Guard(ctx, s.Egress, s.EgressMode, meter.Egress{
		SourceID: row.ID, Channel: meter.ChannelPartnerShare, DataTenantID: g.Grantor,
		CounterpartyKind: meter.CounterpartyTenant, CounterpartyRef: g.Grantee, Product: r.Kind,
		Records: int64(r.Items), OccurredAt: row.EventTime, ActorRef: actor, TraceID: telemetry.TraceID(ctx),
	}, s.Log); err != nil {
		return err
	}
	if err := tenant.WithTenantTx(ctx, s.DB, g.Grantor, func(tx pgx.Tx) error { return store.InsertReadLog(ctx, tx, &row) }); err != nil {
		return port.ErrPartnerAuditUnavailable.WithCause(err)
	}
	return nil
}

// ValveGrantors cài đặt port.ValveGrants (consumer van — không có người gọi): mỗi bên cấp một bằng
// chứng đi từ grantee (tenant phong bì) sang bên cấp.
func (s *Sharing) ValveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]tenant.Grant, error) {
	gs, err := s.activeFor(ctx, grantee, kind, uuid.Nil, at)
	if err != nil {
		return nil, err
	}
	var out []tenant.Grant
	for _, g := range gs {
		if !slices.ContainsFunc(out, func(x tenant.Grant) bool { return x.Tenant() == g.Grantor }) {
			out = append(out, g.Access)
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
	if err := s.withLocations(ctx, g, pg.Items); err != nil {
		return PartnerCooperativePage{}, err
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
		one := []store.CooperativeEntry{*e}
		if err := s.withLocations(ctx, g, one); err != nil {
			return store.CooperativeEntry{}, err
		}
		*e = one[0]
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

// withLocations ghép toạ độ hồ sơ HTX (pf/cooperative, W3.3) vào dòng danh bạ — độ, 7 chữ số thập phân như
// round(ST_X(location)::numeric, 7)::text trước W3.3. Tenant là bên cấp — đi qua bằng chứng của cấp quyền (H4).
func (s *Sharing) withLocations(ctx context.Context, g port.ShareGrant, items []store.CooperativeEntry) error {
	if s.Profiles == nil || len(items) == 0 {
		return nil
	}
	refs := make([]uuid.UUID, 0, len(items))
	for _, it := range items {
		refs = append(refs, it.Ref)
	}
	gctx, err := tenant.WithGrant(ctx, g.Access)
	if err != nil {
		return err
	}
	profs, err := s.Profiles.Profiles(gctx, refs)
	if err != nil {
		return ErrDependency.WithDetail("không đọc được hồ sơ HTX").WithCause(err)
	}
	for i := range items {
		p, ok := profs[items[i].Ref]
		if !ok || p.Lat == nil || p.Lon == nil {
			continue
		}
		lon, lat := p.Lon.StringFixed(7), p.Lat.StringFixed(7)
		items[i].Lon, items[i].Lat = &lon, &lat
	}
	return nil
}
