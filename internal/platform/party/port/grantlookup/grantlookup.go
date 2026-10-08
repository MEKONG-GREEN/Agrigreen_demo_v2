// Package grantlookup là bộ điều hợp CHỈ ĐỌC của cổng party/port.GrantLookup (G5, ADR-0022 mục 3)
// cho gốc lắp ráp không được kéo kho định danh: cmd/api-public (archlint noVaultRoots, RB-3).
//
// Gốc module pf/party (module.go) và app/ import client kho định danh (đăng ký nông hộ, người đại
// diện) và handler Application/Admin. Gốc lắp ráp chỉ được import gốc module hoặc port/ (R-02), nên
// bộ điều hợp nằm dưới port/: chỉ phụ thuộc party/port, party/store (SQL, không vault) và shared/*.
// Ngữ nghĩa trùng app.Sharing.ActiveGrant (app/ gọi lại chính gói này): cùng truy vấn hai phía như
// Authorize, nhưng không kiểm vai OIDC — vì vậy CHỈ nhận loại sản phẩm dữ liệu dp_* và loại feed lớp partner
// của cổng tích hợp (port.FeedKinds — cooperative_directory, plot_directory; W5.5).
//
// Đọc bằng pool vai party_app (R-05), tenant.WithTenantTx ở tenant bên nhận; không BYPASSRLS.
package grantlookup

import (
	"context"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Lookup hiện thực port.GrantLookup (và danh sách bên cấp cho danh mục /v1/data-products).
type Lookup struct {
	DB    tenant.Beginner // pool đã bọc r_party (New)
	Clock clock.Clock
	// Locations (W5.5/W3.3): vị trí hồ sơ HTX cho feed cooperative_directory (pf/cooperative); nil → tọa độ null.
	Locations CoopLocations
}

var (
	_ port.GrantLookup = (*Lookup)(nil)
	_ port.APIReadLog  = (*Lookup)(nil)
)

// dbModule: bộ điều hợp đọc/ghi bảng của pf/party — giao dịch chạy SET LOCAL ROLE r_party (W2.6). Không import
// gốc pf/party (R-02), nên khai lại cùng module (ModuleID so sánh theo giá trị).
var dbModule = shareddb.Module("pf/party")

// New tạo bộ điều hợp trên pool vai đăng nhập của đơn vị (app_public) hoặc vai cũ party_app.
func New(db tenant.Beginner, clk clock.Clock) *Lookup {
	if clk == nil {
		clk = clock.System{}
	}
	return &Lookup{DB: shareddb.ForModule(db, dbModule), Clock: clk}
}

// gatewayKind: chỉ loại dp_* hợp lệ và loại feed lớp partner (port.FeedKinds, W5.5) mới đi qua cổng không vai OIDC.
func gatewayKind(kind string) bool { return port.GatewayKind(kind) }

func (l *Lookup) active(ctx context.Context, grantee uuid.UUID, kind string, grantor uuid.UUID, at time.Time) ([]store.Inbound, error) {
	var out []store.Inbound
	err := tenant.WithTenantTx(ctx, l.DB, grantee, func(tx pgx.Tx) error {
		var err error
		out, err = store.VerifiedActive(ctx, tx, grantee, kind, grantor, at)
		return err
	})
	return out, err
}

// ActiveGrant cài đặt port.GrantLookup: cấp quyền active, trong hạn tại at (zero = giờ đồng hồ),
// bên cấp grantor (Nil = chỉ khi đúng một bên cấp) → bên nhận grantee, loại kind dp_* hoặc port.FeedKinds. Thiếu / hết
// hạn / chưa hiệu lực / thu hồi / loại lạ → party.share_grant_missing; nhiều bên cấp + grantor Nil →
// party.share_grantor_ambiguous.
func (l *Lookup) ActiveGrant(ctx context.Context, grantee, grantor uuid.UUID, kind string, at time.Time) (port.ShareGrant, error) {
	if grantee == uuid.Nil || !gatewayKind(kind) {
		return port.ShareGrant{}, port.ErrShareGrantMissing
	}
	if at.IsZero() {
		at = l.Clock.Now()
	}
	rows, err := l.active(ctx, grantee, kind, grantor, at)
	if err != nil {
		return port.ShareGrant{}, err
	}
	if len(rows) == 0 {
		return port.ShareGrant{}, port.ErrShareGrantMissing
	}
	for _, in := range rows[1:] {
		if in.Grantor != rows[0].Grantor {
			return port.ShareGrant{}, port.ErrShareGrantorAmbiguous
		}
	}
	in := rows[0]
	return port.ShareGrant{ID: in.GrantID, Grantor: in.Grantor, Grantee: in.TenantID, Kinds: in.Kinds, ValidFrom: in.ValidFrom, ValidTo: in.ValidTo}, nil
}

// ActiveGrantors liệt kê mọi bên cấp đang có cấp quyền loại kind (dp_* hoặc port.FeedKinds) cho grantee tại at — chỉ để
// điền danh mục /v1/data-products (apiclient/public.GrantorLister). Loại khác → rỗng.
func (l *Lookup) ActiveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error) {
	if grantee == uuid.Nil || !gatewayKind(kind) {
		return nil, nil
	}
	if at.IsZero() {
		at = l.Clock.Now()
	}
	rows, err := l.active(ctx, grantee, kind, uuid.Nil, at)
	if err != nil {
		return nil, err
	}
	out := make([]uuid.UUID, 0, len(rows))
	for _, in := range rows {
		if !slices.Contains(out, in.Grantor) {
			out = append(out, in.Grantor)
		}
	}
	return out, nil
}

// maxQuery giới hạn tham số lọc lưu ở sổ đọc (như app.Sharing.RecordRead).
const maxQuery = 500

// RecordRead cài đặt port.APIReadLog (K-05, KN US-MRV-05): MỘT dòng partner_read_log ở tenant BÊN CẤP
// cho một lần đọc của khách hàng Public API. Người đọc lấy từ authz.APIClient trong context (không
// bao giờ từ request): tenant bên nhận phải = g.Grantee, actor_ref = mã khách hàng API. Chỉ loại dp_* (hoặc port.FeedKinds)
// mà cấp quyền có. idem_key theo mã yêu cầu (AG-Request-Id) nên ghi lại trong cùng yêu cầu không thêm
// dòng. Không qua kênh partner_share: egress public_api đã ghi cùng sổ sử dụng (ADR-0023). Lỗi →
// port.ErrPartnerAuditUnavailable (module gọi không trả dữ liệu).
func (l *Lookup) RecordRead(ctx context.Context, g port.ShareGrant, r port.PartnerRead) error {
	c, ok := authz.APIClientFrom(ctx)
	if !ok || c.ClientID == uuid.Nil || c.TenantID == uuid.Nil || c.TenantID != g.Grantee ||
		g.Grantor == uuid.Nil || g.ID == uuid.Nil || !gatewayKind(r.Kind) || !g.Covers(r.Kind) {
		return port.ErrPartnerAuditUnavailable
	}
	var q *string
	if r.Query != "" {
		v := r.Query
		if len(v) > maxQuery {
			v = v[:maxQuery]
		}
		q = &v
	}
	req := c.RequestID
	if req == uuid.Nil {
		req = ids.New()
	}
	at := c.ReceivedAt
	if at.IsZero() {
		at = l.Clock.Now()
	}
	row := store.ReadLog{ID: ids.New(), TenantID: g.Grantor, GrantID: g.ID, Grantee: g.Grantee, Kind: r.Kind, Endpoint: r.Endpoint, Query: q,
		Items: r.Items, EventTime: at, ActorRef: c.ClientID, IdemKey: "read:api:" + req.String()}
	if err := tenant.WithTenantTx(ctx, l.DB, g.Grantor, func(tx pgx.Tx) error { return store.InsertReadLog(ctx, tx, &row) }); err != nil {
		return port.ErrPartnerAuditUnavailable.WithCause(err)
	}
	return nil
}
