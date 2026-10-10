package handler

// W11.02 (KN v7.7 FN-AD-04.04, UR-AD-04): nạp Excel tài khoản nhiều vai trò, tab Phiên đang hoạt động /
// Lịch sử đăng nhập (Admin API, MFA):
//
//	POST /v1/accounts/kn-users/import  (thân request = tệp CSV UTF-8/.xlsx)
//	GET  /v1/accounts/users/{id}/sessions

import (
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerKNImport gắn route nạp tệp và phiên (gọi từ RegisterKNAdmin).
func (h Handler) registerKNImport(rg *gin.RouterGroup) {
	httpx.AllowBody(rg, http.MethodPost, "/accounts/kn-users/import", app.MaxKNImportBytes+1024)
	rg.POST("/accounts/kn-users/import", h.importKNUsers)
	rg.GET("/accounts/users/:id/sessions", h.userSessions)
}

func (h Handler) importKNUsers(c *gin.Context) {
	res, err := h.Svc.ImportKNUsers(c.Request.Context(), io.LimitReader(c.Request.Body, app.MaxKNImportBytes+1), key(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]gin.H, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, gin.H{"row": r.Row, "status": r.Status, "reason": strPtr(r.Reason), "user_id": refPtr(r.UserID), "activation_email_sent": r.ActivationSent})
	}
	c.JSON(http.StatusOK, gin.H{"accounts_created": res.Accounts, "created": res.Created, "rejected": res.Rejected, "rows": rows})
}

func optTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := rfc(t)
	return &s
}

func (h Handler) userSessions(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	limit = clamp(limit)
	res, err := h.Svc.KNUserSessions(c.Request.Context(), id, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	active := make([]gin.H, 0, len(res.Active))
	for _, s := range res.Active {
		active = append(active, gin.H{"id": s.ID, "ip_address": s.IPAddress, "started_at": optTime(s.Start), "last_access_at": optTime(s.LastAccess), "clients": s.Clients})
	}
	hist := make([]gin.H, 0, len(res.History))
	for _, e := range res.History {
		hist = append(hist, gin.H{"at": optTime(e.Time), "type": e.Type, "ip_address": e.IPAddress, "client": e.Client, "error": strPtr(e.Error)})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"available": res.Available, "active": active, "history": hist})
}
