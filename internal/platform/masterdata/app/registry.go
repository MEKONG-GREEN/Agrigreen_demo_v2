package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

var _ partyport.AdminUnitTree = UnitsPort{}

// Children thực hiện port.AdminUnitTree: đơn vị con trực tiếp có hiệu lực tại at
// (parent rỗng → các tỉnh), đọc hết mọi trang.
func (p UnitsPort) Children(ctx context.Context, parent string, at time.Time) ([]partyport.AdminUnit, error) {
	var out []partyport.AdminUnit
	err := tenant.FromCtxTx(ctx, p.Svc.DB, func(tx pgx.Tx) error {
		after := ""
		for {
			us, err := store.Children(ctx, tx, parent, domain.DayOf(at), after, 500)
			if err != nil {
				return err
			}
			for _, u := range us {
				out = append(out, ToPortUnit(u))
			}
			if len(us) < 500 {
				return nil
			}
			after = us[len(us)-1].Code
		}
	})
	return out, err
}

// ---------------------------------------------------------------------------
// G-06: danh sách danh mục (Admin API)
// ---------------------------------------------------------------------------

// CatalogSummary là một danh mục kèm số mục.
type CatalogSummary struct {
	Catalog       string
	Name          string
	ItemCount     int
	InactiveCount int
	AttrsSchema   []string
}

// ListCatalogs trả mọi danh mục module quản lý kèm tên tiếng Việt, số mục và số
// mục ngừng dùng (cột trái /quan-tri/danh-muc). Vai masterdata_admin.
func (s *Service) ListCatalogs(ctx context.Context) ([]CatalogSummary, error) {
	p, err := authz.Require(ctx, port.RoleMasterdataAdmin)
	if err != nil {
		return nil, err
	}
	var counts map[string][2]int
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		counts, e = store.CatalogCounts(ctx, tx)
		return e
	})
	if err != nil {
		return nil, err
	}
	out := make([]CatalogSummary, 0, len(domain.Catalogs))
	for _, c := range domain.Catalogs {
		n := counts[c]
		out = append(out, CatalogSummary{
			Catalog: c, Name: domain.CatalogName(c), ItemCount: n[0], InactiveCount: n[1], AttrsSchema: domain.CatalogAttrs(c),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// G-07: sổ đăng ký tham số (Admin API)
// ---------------------------------------------------------------------------

// ParamCurrent là phiên bản đã duyệt đang hiệu lực của một khóa.
type ParamCurrent struct {
	Version       int
	Value         *string
	Text          *string
	EffectiveFrom time.Time
}

// ParamEntry là một khóa tham số trong sổ đăng ký.
type ParamEntry struct {
	domain.ParamSpec
	Key            string
	Current        *ParamCurrent
	SubmittedCount int
}

// ParamsQuery là tham số liệt kê sổ đăng ký.
type ParamsQuery struct {
	Prefix string
	After  string
	Limit  int
}

// ListParams trả sổ đăng ký tham số: mọi khóa đã biết (kể cả khóa chưa có phiên
// bản nào) và mọi khóa đã có phiên bản, kèm tên, đơn vị mặc định, kiểu, khoảng
// hợp lệ, phiên bản đang hiệu lực và số phiên bản chờ duyệt. Sắp theo khóa,
// phân trang theo khóa (R-53).
func (s *Service) ListParams(ctx context.Context, q ParamsQuery) (Page[ParamEntry], error) {
	p, err := authz.Require(ctx, paramViewerRoles...)
	if err != nil {
		return Page[ParamEntry]{}, err
	}
	limit := clampLimit(q.Limit)
	now := s.Clock.Now()
	var stats map[string]store.KeyStat
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		stats, e = store.ParamKeyStats(ctx, tx, q.Prefix)
		return e
	})
	if err != nil {
		return Page[ParamEntry]{}, err
	}
	keys := map[string]bool{}
	for k := range stats {
		keys[k] = true
	}
	for _, sp := range s.parameterSpecs() {
		if !sp.Dimensional && strings.HasPrefix(sp.Key, q.Prefix) {
			keys[sp.Key] = true
		}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		if k > q.After {
			sorted = append(sorted, k)
		}
	}
	sort.Strings(sorted)
	var pg Page[ParamEntry]
	if len(sorted) > limit {
		sorted = sorted[:limit]
		last := sorted[limit-1]
		pg.Next = &last
	}
	pg.Items = make([]ParamEntry, 0, len(sorted))
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		for _, k := range sorted {
			e := ParamEntry{ParamSpec: s.parameterSpec(k), Key: k, SubmittedCount: stats[k].Submitted}
			if e.Name == k && stats[k].HasText {
				e.Kind = domain.ParamKindText
			}
			a, err := store.ApprovedAt(ctx, tx, k, now)
			if err != nil {
				return err
			}
			if a != nil {
				c := &ParamCurrent{Version: a.Version, Text: a.Text, EffectiveFrom: a.EffectiveFrom}
				if a.Value != nil {
					v := a.Value.String()
					c.Value = &v
				}
				e.Current = c
			}
			pg.Items = append(pg.Items, e)
		}
		return nil
	})
	return pg, err
}

// ActorNames tra tên hiển thị của cán bộ (người đặt/duyệt tham số) — G-07. Có
// thể nil: tên trả rỗng, web hiện mã.
func (s *Service) ActorNames(ctx context.Context, refs []uuid.UUID) map[uuid.UUID]string {
	if s.Staff == nil || len(refs) == 0 {
		return map[uuid.UUID]string{}
	}
	m, err := s.Staff.DisplayNames(ctx, refs)
	if err != nil || m == nil {
		return map[uuid.UUID]string{}
	}
	return m
}

// staffDirectory là port danh bạ cán bộ (pf/extension) dùng cho tên người đặt/duyệt.
type staffDirectory = port.StaffNames
