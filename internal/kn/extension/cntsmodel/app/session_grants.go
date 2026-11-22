package app

// Phạm vi quyền FN-14 cho phiên chủ KN (#71 KN-GRANT-OWNER). Chỉ ĐỌC sổ cnts_grant (và hộ mô hình để mở điểm thành hộ)
// trong giao dịch tenant của người gọi; không ghi sổ, không đổi người thao tác lịch sử, không chuyển nháp/chủ sở hữu.
// Chuyển việc khi thu hồi (AC-02) là chuyển ĐIỀU KIỆN: RevokeGrant cấp người nhận quyền cùng phạm vi, công việc biểu
// (kỳ × đơn vị) được tính lại theo phạm vi mới ở request kế tiếp; dấu quyền của phiên đổi nên idempotency không trả
// phản hồi cũ và con trỏ đồng bộ reset (docs/kn/gia-dinh/W11.47.md).

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// SessionGrants trả phạm vi quyền FN-14 hiện hành của grantee trong tenant, giới hạn trong các xã mà phiên được phép
// (inScope — Scope của lần gán đã chọn).
//
//   - nil: grantee CHƯA TỪNG có quyền CNTS nào (mọi phiên bản, mọi xã, vai khác nông hộ) trong tenant → giữ W11.07;
//   - khác nil: tường minh — chỉ quyền đang hiệu lực hôm nay (giờ Việt Nam, tính cả ngày đầu/cuối; thu hồi, chờ hiệu
//     lực, hết hạn bị bỏ) có xã thuộc phiên. Không còn quyền nào → phạm vi rỗng (chặn), không quay về W11.07.
//
// Tường minh xét trên MỌI phiên bản sổ, không chỉ bản hiện hành, và không theo xã: sửa thu hẹp xã (X+Y → X), đổi mã xã
// do sáp nhập hay lần gán ở xã khác không bao giờ mở lại W11.07 cho người đã được quản lý bằng FN-14 (review #71).
//
// Vai nông hộ thuộc phiên nông hộ (GĐ2) nên không chiếu vào phiên Cấp Xã.
func (s *Service) SessionGrants(ctx context.Context, tenantID, grantee uuid.UUID, inScope func(string) bool) (*knport.CNTSGrantScope, error) {
	if tenantID == uuid.Nil || grantee == uuid.Nil || inScope == nil {
		return nil, nil
	}
	today := s.today()
	var out *knport.CNTSGrantScope
	err := tenant.WithTenantTx(ctx, s.DB, tenantID, func(tx pgx.Tx) error {
		out = nil
		gs, err := store.ListGrants(ctx, tx, &grantee)
		if err != nil {
			return err
		}
		scope := &knport.CNTSGrantScope{Grants: []knport.CNTSGrant{}, Households: []uuid.UUID{}}
		var hhRefs, siteRefs []uuid.UUID
		for _, g := range gs {
			if g.GranteeRef != grantee || g.Role == domain.RoleFarmer {
				continue
			}
			communes := slices.DeleteFunc(slices.Clone(g.Communes), func(c string) bool { return !inScope(c) })
			if len(communes) == 0 {
				continue
			}
			if domain.GrantStatus(g.Event, g.ValidFrom, g.ValidTo, today) != domain.StatusActive {
				continue
			}
			slices.Sort(communes)
			e := knport.CNTSGrant{Ref: g.Ref, Seq: g.Seq, Role: g.Role, Communes: communes, Facilities: sortedRefs(g.Facilities),
				Sites: sortedRefs(g.Sites), Households: sortedRefs(g.Households), Forms: slices.Sorted(slices.Values(g.Forms)),
				ValidFrom: g.ValidFrom.Format(time.DateOnly)}
			if g.ValidTo != nil {
				e.ValidTo = g.ValidTo.Format(time.DateOnly)
			}
			scope.Grants = append(scope.Grants, e)
			hhRefs = append(hhRefs, e.Households...)
			siteRefs = append(siteRefs, e.Sites...)
		}
		if len(gs) == 0 {
			return nil
		}
		ever, err := store.GranteeEverGranted(ctx, tx, grantee)
		if err != nil || !ever {
			return err
		}
		slices.SortFunc(scope.Grants, func(a, b knport.CNTSGrant) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
		if len(hhRefs) > 0 || len(siteRefs) > 0 {
			hs, err := store.CoveredHouseholds(ctx, tx, uniq(hhRefs), uniq(siteRefs))
			if err != nil {
				return err
			}
			for _, h := range hs {
				if h.Event != domain.EventWithdrawn && inScope(h.CommuneCode) && covered(scope.Grants, h) {
					scope.Households = append(scope.Households, h.Ref)
				}
			}
			scope.Households = sortedRefs(scope.Households)
		}
		out = scope
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// covered: hộ thuộc một quyền đang hiệu lực (giao trực tiếp hoặc qua điểm) trong xã của chính quyền đó.
func covered(gs []knport.CNTSGrant, h store.Household) bool {
	for _, g := range gs {
		if slices.Contains(g.Communes, h.CommuneCode) && (slices.Contains(g.Households, h.Ref) || slices.Contains(g.Sites, h.SiteRef)) {
			return true
		}
	}
	return false
}

func sortedRefs(in []uuid.UUID) []uuid.UUID {
	out := slices.Clone(in)
	if out == nil {
		out = []uuid.UUID{}
	}
	slices.SortFunc(out, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(out)
}
