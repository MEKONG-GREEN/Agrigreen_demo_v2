package contact

import (
	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/contact/handler"
)

// SupportConfig holds trusted deployment routing and the public key provisioned by an authenticated Admin.
type SupportConfig = app.SupportConfig

// SupportConfigFromEnv validates all intake settings before startup.
func SupportConfigFromEnv(get func(string) string) (SupportConfig, error) {
	return app.SupportConfigFromEnv(get)
}

// UseSupport installs a copied immutable deployment snapshot, never values from HTTP.
func (m *Module) UseSupport(cfg SupportConfig) {
	cfg.PublicKey = append([]byte(nil), cfg.PublicKey...)
	m.svc.Support = cfg
}

// RegisterPublic exposes only anonymous create; queue/detail/update stay behind real authentication.
func (m *Module) RegisterPublic(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterPublic(rg) }
