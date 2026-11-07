package handler

import (
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

// Read the menu through its existing server owner: custom department codes and
// generic profile aliases have the same permissions as their enabled catalog.
// Native CNTS routes stay outside this group; use cases retain role/data scope.
func (h Handler) trongTrotOnly(c *gin.Context) {
	_, _, err := port.RequireSession(c.Request.Context())
	if err == nil {
		if h.Menu == nil {
			err = app.ErrDependency
		} else {
			var menu partyport.MenuView
			menu, err = h.Menu(c.Request.Context())
			if err == nil && !hasTTMenu(menu) {
				err = problem.Forbidden
			}
		}
	}
	if err != nil {
		httpx.Fail(c, err)
		c.Abort()
		return
	}
	c.Next()
}

func hasTTMenu(menu partyport.MenuView) bool {
	var visit func([]partyport.MenuItemView) bool
	visit = func(items []partyport.MenuItemView) bool {
		for _, item := range items {
			if !item.Enabled {
				continue
			}
			if len(item.Children) > 0 {
				if visit(item.Children) {
					return true
				}
				continue
			}
			if item.Key == "admin.pb.theo_doi_nop" || item.Key == "tinh.dashboard" || item.Key == "xa.nhiem_vu.cong_viec" || strings.HasPrefix(item.Key, "tw.bao_cao.") {
				return true
			}
		}
		return false
	}
	for _, group := range menu.Groups {
		if visit(group.Items) {
			return true
		}
	}
	return false
}
