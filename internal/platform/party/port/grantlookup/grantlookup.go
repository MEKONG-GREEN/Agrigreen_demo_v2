// Package grantlookup là bộ điều hợp CHỈ ĐỌC của cổng party/port.GrantLookup (G5, ADR-0022 mục 3)
// cho gốc lắp ráp không được kéo kho định danh: cmd/api-public (archlint noVaultRoots, RB-3).
//
// Gốc module pf/party (module.go) và app/ import client kho định danh (đăng ký nông hộ, người đại
// diện) và handler Application/Admin. Gốc lắp ráp chỉ được import gốc module hoặc port/ (R-02), nên
// bộ điều hợp nằm dưới port/: chỉ phụ thuộc party/port, party/store (SQL, không vault) và shared/*.
// Ngữ nghĩa trùng app.Sharing.ActiveGrant (app/ gọi lại chính gói này): cùng truy vấn hai phía như
// Authorize, nhưng không kiểm vai OIDC — vì vậy CHỈ nhận loại sản phẩm dữ liệu dp_*.
//
// Đọc bằng pool vai party_app (R-05), tenant.WithTenantTx ở tenant bên nhận; không BYPASSRLS.
package grantlookup

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Lookup hiện thực port.GrantLookup (và danh sách bên cấp cho danh mục /v1/data-products).
type Lookup struct {
	DB    tenant.Beginner // pool vai party_app
	Clock clock.Clock
}

var _ port.GrantLookup = (*Lookup)(nil)

// New tạo bộ điều hợp trên pool vai party_app.
func New(db tenant.Beginner, clk clock.Clock) *Lookup {
	if clk == nil {
		clk = clock.System{}
	}
	return &Lookup{DB: db, Clock: clk}
}

// dataProductKind: chỉ loại dp_* hợp lệ (ShareKinds) mới đi qua cổng không vai OIDC.
func dataProductKind(kind string) bool {
	return port.ValidKind(kind) && strings.HasPrefix(kind, port.DataProductKind(""))
}

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
// bên cấp grantor (Nil = chỉ khi đúng một bên cấp) → bên nhận grantee, loại kind dp_*. Thiếu / hết
// hạn / chưa hiệu lực / thu hồi / loại lạ → party.share_grant_missing; nhiều bên cấp + grantor Nil →
// party.share_grantor_ambiguous.
func (l *Lookup) ActiveGrant(ctx context.Context, grantee, grantor uuid.UUID, kind string, at time.Time) (port.ShareGrant, error) {
	if grantee == uuid.Nil || !dataProductKind(kind) {
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

// ActiveGrantors liệt kê mọi bên cấp đang có cấp quyền loại kind (dp_*) cho grantee tại at — chỉ để
// điền danh mục /v1/data-products (apiclient/public.GrantorLister). Loại khác → rỗng.
func (l *Lookup) ActiveGrantors(ctx context.Context, grantee uuid.UUID, kind string, at time.Time) ([]uuid.UUID, error) {
	if grantee == uuid.Nil || !dataProductKind(kind) {
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
