package handler_test

import (
	"context"
	"encoding/json"
	partyapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"time"
)

// provisionCooperative chỉ là setup/kiểm thử lõi hai bước, không giả thành HTTP.
// POST /v1/orgs bị khóa cho HTX; các ca HTTP công khai phải kiểm đường Neo D2.
func (c caller) provisionCooperative(body map[string]any, idem string) (partyapp.OrgResult, error) {
	var req struct {
		Code              string `json:"code"`
		Name              string `json:"name"`
		Unit              string `json:"admin_unit_code"`
		Parent            string `json:"parent_ref"`
		RepresentativeRef string `json:"representative_ref"`
		Representative    *struct {
			Name  string `json:"full_name"`
			Phone string `json:"phone"`
		} `json:"representative"`
		Cooperative *struct {
			Tax         string           `json:"tax_code"`
			Area        *decimal.Decimal `json:"area_ha"`
			Established string           `json:"established_on"`
			Address     string           `json:"address"`
			Location    *domain.Point    `json:"location"`
		} `json:"cooperative"`
	}
	raw, _ := json.Marshal(body)
	if err := json.Unmarshal(raw, &req); err != nil {
		return partyapp.OrgResult{}, err
	}
	in := partyapp.CreateOrgInput{Kind: port.OrgCooperative, Code: req.Code, Name: req.Name, AdminUnitCode: req.Unit, IdemKey: idem}
	if req.Parent != "" {
		ref, err := uuid.Parse(req.Parent)
		if err != nil {
			return partyapp.OrgResult{}, problem.Validation
		}
		in.ParentRef = ref
	}
	if req.RepresentativeRef != "" {
		ref, err := uuid.Parse(req.RepresentativeRef)
		if err != nil {
			return partyapp.OrgResult{}, problem.Validation
		}
		in.RepresentativeRef = ref
	}
	if req.Representative != nil {
		in.Representative = &partyapp.Representative{FullName: req.Representative.Name, Phone: req.Representative.Phone}
	}
	if req.Cooperative != nil {
		p := req.Cooperative
		in.Cooperative = &domain.CooperativeProfile{TaxCode: p.Tax, AreaHa: p.Area, Address: p.Address, Location: p.Location}
		if p.Established != "" {
			day, err := time.Parse(time.DateOnly, p.Established)
			if err != nil {
				return partyapp.OrgResult{}, problem.Validation
			}
			in.Cooperative.EstablishedOn = &day
		}
	}
	reg := c.f.mod.Orgs().(interface {
		CreateOrg(context.Context, partyapp.CreateOrgInput) (partyapp.OrgResult, error)
	})
	ctx := tenant.WithID(authz.WithPrincipal(context.Background(), authz.Principal{ActorRef: c.actor, TenantID: c.tid, OrgRef: c.org, Roles: c.roles, MFA: true}), c.tid)
	return reg.CreateOrg(ctx, in)
}
