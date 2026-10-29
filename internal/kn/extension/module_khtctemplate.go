package extension

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	reportdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	templateapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/app"
	templatedomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/domain"
	templatehandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtctemplate/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type khtcTemplateService = templateapp.Service

func (m *Module) khtcTemplateSvc() *templateapp.Service {
	if m.khtcTemplates == nil {
		m.khtcTemplates = &templateapp.Service{DB: m.svc.DB, Clock: m.svc.Clock, Contexts: khtcTemplateContexts{m: m}, Names: func() partyport.ActorNames { return m.rc.Names }}
		m.khtcTemplates.LegacyTemplates = khtcLegacyTemplates()
	}
	return m.khtcTemplates
}
func (m *Module) registerKHTCTemplates(rg *gin.RouterGroup) {
	templatehandler.Handler{Svc: m.khtcTemplateSvc()}.RegisterApp(rg)
}
func (m *Module) KHTCFixedTemplates() port.KHTCFixedTemplates { return m.khtcTemplateSvc() }

func khtcLegacyTemplates() map[string]port.KHTCTemplateSnapshot {
	out := map[string]port.KHTCTemplateSnapshot{}
	for _, key := range templatedomain.Keys {
		sections, err := json.Marshal(reportdomain.Sections(key))
		if err != nil {
			panic(fmt.Errorf("builtin KHTC sections: %w", err))
		}
		pinned, err := templatedomain.LegacySnapshot(key, reportdomain.RuleVersion, sections, reportapp.WordTemplate(key))
		if err != nil {
			panic(fmt.Errorf("builtin KHTC template: %w", err))
		}
		out[key] = pinned
	}
	return out
}

type khtcTemplateContexts struct{ m *Module }

func (a khtcTemplateContexts) TemplateContext(ctx context.Context, key string, id uuid.UUID) (port.KHTCTemplateContext, error) {
	p, _, err := port.RequireSession(ctx, port.SessionRoleAdmin)
	if err != nil {
		return port.KHTCTemplateContext{}, err
	}
	if id == uuid.Nil {
		kind := map[string]string{"mau_1": "week", "mau_2": "month", "mau_3": "quarter", "mau_4": "year", "mau_5": "program"}[key]
		err = tenant.WithTenantTx(ctx, a.m.svc.DB, p.TenantID, func(tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT id FROM pf_extension.khtc_period WHERE report_type=$1 ORDER BY period_from DESC,id DESC LIMIT 1`, kind).Scan(&id)
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return port.KHTCTemplateContext{}, templateapp.ErrNotFound.WithDetail("Chưa có kỳ báo cáo phù hợp để xem trước")
		}
		if err != nil {
			return port.KHTCTemplateContext{}, err
		}
	}
	detail, err := a.m.khtcReportSvc().Get(ctx, id)
	if err != nil {
		return port.KHTCTemplateContext{}, err
	}
	if detail.Content.Data.Template != key {
		return port.KHTCTemplateContext{}, templateapp.ErrInvalid.WithField("period_id", "Kỳ không thuộc mẫu đã chọn")
	}
	return templateapp.ContextFromDraft(detail.Content, detail.Terminal), nil
}
