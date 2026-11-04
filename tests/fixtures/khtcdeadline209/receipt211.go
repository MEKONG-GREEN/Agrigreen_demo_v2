package khtcdeadline209

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/gin-gonic/gin"
	"net/http"
)

// Public supplier-only composition for211; existing209 behavior is unchanged.
func (f *Native) PeriodOperations() port.PeriodOperations { return f.module.PeriodOperations() }
func (f *Native) UsePeriodOperationPolicy(profile string, policy port.PeriodOperationPolicy) error {
	return f.module.UsePeriodOperationPolicy(profile, policy)
}

func (f *Native) Entry211() port.Entry               { return f.module.Entry() }
func (f *Native) RegisterApp211(rg *gin.RouterGroup) { f.module.RegisterApp(rg) }

var _ http.Handler = (*gin.Engine)(nil)
