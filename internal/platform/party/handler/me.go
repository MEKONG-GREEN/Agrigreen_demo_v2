package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// Me gói app.Registry.Me cho HTTP: GET /v1/me.
type Me struct{ Reg *app.Registry }

// Register gắn route vào nhóm /v1 đã có xác thực.
func (h Me) Register(rg *gin.RouterGroup) { rg.GET("/me", h.get) }

type meOrgDTO struct {
	Ref           string  `json:"ref"`
	Kind          string  `json:"kind"`
	Name          string  `json:"name"`
	AdminUnitCode *string `json:"admin_unit_code"`
	Active        bool    `json:"active"`
}

type meScopeDTO struct {
	All       bool              `json:"all"`
	Level     string            `json:"level"`
	Units     []string          `json:"units"`
	UnitNames map[string]string `json:"unit_names"`
}

type meDTO struct {
	ActorRef string     `json:"actor_ref"`
	TenantID string     `json:"tenant_id"`
	Roles    []string   `json:"roles"`
	MFA      bool       `json:"mfa"`
	Org      *meOrgDTO  `json:"org"`
	Scope    meScopeDTO `json:"scope"`
}

func (h Me) get(c *gin.Context) {
	m, err := h.Reg.Me(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := meDTO{
		ActorRef: m.ActorRef.String(), TenantID: m.TenantID.String(), Roles: m.Roles, MFA: m.MFA,
		Scope: meScopeDTO{All: m.Scope.All, Level: m.Level, Units: m.Scope.Units, UnitNames: m.UnitNames},
	}
	if m.Org != nil {
		out.Org = &meOrgDTO{Ref: m.Org.Ref.String(), Kind: string(m.Org.Kind), Name: m.Org.Name, AdminUnitCode: strPtr(m.Org.AdminUnitCode), Active: m.Org.Active}
	}
	// Phản hồi theo người gọi — không cho proxy/trình duyệt dùng chung.
	c.Header("Cache-Control", "private, no-store")
	c.JSON(http.StatusOK, out)
}
