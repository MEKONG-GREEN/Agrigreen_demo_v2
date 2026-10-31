// Package hrmappraisal composes the real export capability for integration tests.
package hrmappraisal

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

func NewExporter(pool tenant.Beginner, clk clock.Clock, templates exportport.TemplateRegistration) (exportport.Exporter, error) {
	module, err := export.New(pool, clk, export.Deps{Access: exportport.AccessFunc(func(context.Context) bool { return false }), Templates: []exportport.TemplateRegistration{templates}})
	if err != nil {
		return nil, err
	}
	return module.Exporter(), nil
}
