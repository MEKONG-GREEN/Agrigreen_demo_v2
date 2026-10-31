// Package knadhoc is a test-only composition root. Owned-module probes receive
// capability ports and the real HTTP registrar, without importing a foreign
// module implementation or adding a production testing hook.
package knadhoc

import (
	native "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/gin-gonic/gin"
)

type Deps struct {
	Profiles   []port.Profile
	Validators map[string]port.Validator
}
type Fixture struct {
	publisher port.Publisher
	control   port.PeriodControl
	entry     port.Entry
	register  func(*gin.RouterGroup)
}

func New(b tenant.Beginner, clk clock.Clock, d Deps) (*Fixture, error) {
	module, e := native.New(b, clk, native.Deps{Profiles: d.Profiles, Validators: d.Validators})
	if e != nil {
		return nil, e
	}
	return &Fixture{publisher: module.Publisher(), control: module.Control(), entry: module.Entry(), register: module.RegisterApp}, nil
}
func (f *Fixture) Publisher() port.Publisher      { return f.publisher }
func (f *Fixture) Control() port.PeriodControl    { return f.control }
func (f *Fixture) Entry() port.Entry              { return f.entry }
func (f *Fixture) RegisterApp(g *gin.RouterGroup) { f.register(g) }
