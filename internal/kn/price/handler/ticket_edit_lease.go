package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

func editHandle(raw string) uuid.UUID { id, _ := uuid.Parse(raw); return id }

type editLeaseDTO struct {
	Now        string  `json:"now"`
	Held       bool    `json:"held"`
	Mine       bool    `json:"mine"`
	HolderName *string `json:"holder_name"`
	ExpiresAt  *string `json:"expires_at"`
	Handle     *string `json:"handle_id"`
}

func leaseDTO(l app.EditLease) editLeaseDTO {
	return editLeaseDTO{Now: ts(l.Now), Held: l.Held, Mine: l.Mine, HolderName: strPtr(l.HolderName), ExpiresAt: tsPtr(l.Expires), Handle: uuidPtr(l.Handle)}
}
func (h TicketHandler) registerEditLease(rg *gin.RouterGroup) {
	rg.GET("/price/tickets/:id/edit-lease", func(c *gin.Context) { h.editLease(c, "read") })
	for _, action := range []string{"acquire", "renew", "release"} {
		rg.POST("/price/tickets/:id/edit-lease/"+action, func(c *gin.Context) { h.editLease(c, action) })
	}
}
func (h TicketHandler) editLease(c *gin.Context, action string) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req struct {
		Handle string `json:"handle_id"`
	}
	if action != "read" && !bind(c, &req) {
		return
	}
	value, err := h.Svc.EditTicketLease(c.Request.Context(), c.Query("profile"), id, editHandle(req.Handle), action)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, leaseDTO(value))
}
