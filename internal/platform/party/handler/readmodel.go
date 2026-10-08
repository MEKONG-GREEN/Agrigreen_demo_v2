package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// coopItemDTO là HTX kèm dữ liệu đọc bổ sung (G-03): tên địa bàn, số hộ, số thửa số hoá.
type coopItemDTO struct {
	coopDTO
	ProvinceCode     *string `json:"province_code"`
	CommuneName      *string `json:"commune_name"`
	ProvinceName     *string `json:"province_name"`
	MemberHouseholds int64   `json:"member_households"`
	DigitizedPlots   *int64  `json:"digitized_plots"`
}

func (h Registry) coopItems(ctx context.Context, items []app.Cooperative) ([]coopItemDTO, error) {
	ex, err := h.Reg.CoopExtras(ctx, items)
	if err != nil {
		return nil, err
	}
	out := make([]coopItemDTO, 0, len(items))
	for _, it := range items {
		e := ex[it.Ref]
		out = append(out, coopItemDTO{
			coopDTO: toCoopDTO(it), ProvinceCode: strPtr(e.ProvinceCode), CommuneName: strPtr(e.CommuneName),
			ProvinceName: strPtr(e.ProvinceName), MemberHouseholds: e.MemberHouseholds, DigitizedPlots: e.DigitizedPlots,
		})
	}
	return out, nil
}

// adminListOrgs: GET /v1/orgs trên Admin API (G-08) — cây tổ chức cho quản trị.
func (h Registry) adminListOrgs(c *gin.Context) {
	after, ok := parseCursor(c)
	if !ok {
		return
	}
	q := app.AdminListQuery{Kind: c.Query("kind"), IncludeInactive: c.Query("include_inactive") != "false", After: after, Limit: queryLimit(c)}
	if s := c.Query("parent_ref"); s != "" {
		ref, err := ids.ParseRef(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("parent_ref", "phải là UUID"))
			return
		}
		q.ParentRef = &ref
	}
	page, err := h.Reg.AdminListOrgs(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]orgDTO, 0, len(page.Items))
	for _, o := range page.Items {
		items = append(items, toOrgDTO(o))
	}
	withRegions(c.Request.Context(), h.KN, items, page.Items)
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(page.Next)})
}
