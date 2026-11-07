package departments

// Menu theo cấu hình (UR-KT-02), cây module của phòng ban và nạp phòng ban mặc định (KNSPLIT-2 #222). Luật đối tượng,
// đơn vị, trạng thái phòng ban và bảng menu ở pf/party; ở đây chỉ đọc request, gọi partyport.DepartmentAdmin và ghi
// thân JSON (KNMenu = partyport.MenuDocument, cùng thân với ca vàng web kn-menu.json).

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

func menuQuery(c *gin.Context) (partyport.MenuQuery, bool) {
	unit, ok := optRefQuery(c, "org_ref")
	if !ok {
		return partyport.MenuQuery{}, false
	}
	// "role" là tên cũ của "audience" (hợp đồng tạm P6 trước KN-fix) — nhận thêm một đợt, "audience" thắng.
	aud := c.Query("audience")
	if aud == "" {
		aud = c.Query("role")
	}
	return partyport.MenuQuery{Audience: aud, Department: c.Query("department"), UnitRef: unit}, true
}

func writeMenu(c *gin.Context, v partyport.MenuView, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	// Menu đổi ngay khi Admin đổi trạng thái phòng ban — không lưu đệm phía trung gian.
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, partyport.NewMenuDocument(v))
}

func (h Handler) menu(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	q, ok := menuQuery(c)
	if !ok {
		return
	}
	v, err := svc.Menu(c.Request.Context(), q)
	writeMenu(c, v, err)
}

func (h Handler) modules(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	q, ok := menuQuery(c)
	if !ok {
		return
	}
	v, err := svc.DepartmentModules(c.Request.Context(), c.Param("code"), q)
	writeMenu(c, v, err)
}

func (h Handler) seed(c *gin.Context) {
	svc, ok := h.svc(c)
	if !ok {
		return
	}
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, partyport.ErrOrgMissing) // :ref không phải UUID → 404 party.org_not_found như khi binding còn ở pf/party
		return
	}
	res, err := svc.SeedDepartments(c.Request.Context(), ref, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]departmentDTO, 0, len(res.Created))
	for _, d := range res.Created {
		items = append(items, toDepartmentDTO(d, nil))
	}
	c.JSON(http.StatusOK, gin.H{"created": items, "existing": res.Existing, "status": res.Status,
		"legacy_retired": res.LegacyRetired, "legacy_kept": res.LegacyKept})
}
