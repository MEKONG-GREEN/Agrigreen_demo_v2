package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/seeddata"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// InternalUnitDefinition selects compatibility only when no owner is configured.
// Callers obtain and validate the complete definition before opening a transaction.
func (s *Service) InternalUnitDefinition() (port.InternalUnitDefinition, error) {
	if s.UnitDelegations != nil {
		d := s.UnitDelegations.InternalUnitDefinition()
		if err := port.ValidateInternalUnitDefinition(d); err != nil {
			return port.InternalUnitDefinition{}, fmt.Errorf("%w: %w", ErrDependency, err)
		}
		return d, nil
	}
	var f unitSeedFile
	dec := json.NewDecoder(bytes.NewReader(seeddata.InternalUnits))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return port.InternalUnitDefinition{}, fmt.Errorf("accounts: dữ liệu nhúng internal-units lỗi: %w", err)
	}
	if f.Format != "agrigreen.internal-units/v1" {
		return port.InternalUnitDefinition{}, fmt.Errorf("accounts: định dạng internal-units lỗi: %s", f.Format)
	}
	d := port.InternalUnitDefinition{Kinds: slices.Clone(port.InternalUnitKinds), DepartmentKinds: []string{port.UnitKindPhong},
		OrgKinds: []string{string(partyport.OrgTTKNNational), string(partyport.OrgTTKNProvince)}, SeedStatus: f.Status}
	for _, u := range f.Units {
		d.Seed = append(d.Seed, port.InternalUnitSeedItem{Code: u.Code, Kind: u.Kind, Name: u.Name,
			DepartmentCode: u.DepartmentCode, SortOrder: u.SortOrder})
	}
	return d, nil
}

// DelegationDefinition is the same source for validation, grantee eligibility
// and API labels. Configured invalid data never selects compatibility values.
func (s *Service) DelegationDefinition() (port.DelegationDefinition, error) {
	if s.UnitDelegations != nil {
		d := s.UnitDelegations.DelegationDefinition()
		if err := port.ValidateDelegationDefinition(d); err != nil {
			return port.DelegationDefinition{}, fmt.Errorf("%w: %w", ErrDependency, err)
		}
		return d, nil
	}
	return port.DelegationDefinition{Powers: slices.Clone(port.Powers), MaxDuration: domain.DelegationMaxDays * 24 * time.Hour,
		GranteeRoles: []string{port.KNTinh}}, nil
}

func (s *Service) authorizeDelegationGrant(ctx context.Context, department string) (authz.Principal, port.DelegationGrantDecision, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return p, port.DelegationGrantDecision{}, problem.Unauthenticated
	}
	if s.UnitDelegations != nil {
		decision, handled, err := s.UnitDelegations.AuthorizeDelegationGrant(ctx, department)
		if err != nil {
			return p, port.DelegationGrantDecision{}, err
		}
		if handled {
			if p.SelectedSession == nil {
				return p, port.DelegationGrantDecision{}, problem.Forbidden
			}
			if !decision.Allowed || decision.UnitRef == uuid.Nil || decision.Department == "" {
				return p, port.DelegationGrantDecision{}, ErrDelegationForbidden
			}
			return p, decision, nil
		}
		if p.SelectedSession != nil || p.KN != nil {
			return p, port.DelegationGrantDecision{}, problem.Forbidden
		}
	}
	_, k, err := authz.RequireKN(ctx, port.KNTinh)
	if err != nil {
		return p, port.DelegationGrantDecision{}, err
	}
	if department == "" {
		department = k.Department
	}
	if !k.Manager || k.ReadOnly() || k.Support != nil || department != k.Department {
		return p, port.DelegationGrantDecision{}, ErrDelegationForbidden
	}
	return p, port.DelegationGrantDecision{Allowed: true, UnitRef: k.UnitRef, Department: department}, nil
}

func (s *Service) authorizeDelegationRevoke(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return p, problem.Unauthenticated
	}
	if s.UnitDelegations != nil {
		handled, err := s.UnitDelegations.AuthorizeDelegationRevoke(ctx)
		if err != nil {
			return p, err
		}
		if handled {
			if p.SelectedSession == nil {
				return p, problem.Forbidden
			}
			return p, nil
		}
		if p.SelectedSession != nil || p.KN != nil {
			return p, problem.Forbidden
		}
	}
	_, _, err := authz.RequireKN(ctx, port.KNTinh)
	return p, err
}

func (s *Service) peopleScope(ctx context.Context) (authz.Principal, port.PeopleScopeDecision, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return p, port.PeopleScopeDecision{}, problem.Unauthenticated
	}
	if s.UnitDelegations != nil {
		decision, handled, err := s.UnitDelegations.PeopleScope(ctx)
		if errors.Is(err, port.ErrOwnerAttributesRevoked) {
			err = ErrContextRevoked.WithDetail("thuộc tính vai không còn trong danh mục")
		} else if errors.Is(err, port.ErrAttributeCatalog) {
			err = ErrDependency
		}
		if err != nil {
			return p, decision, err
		}
		if handled {
			if p.SelectedSession == nil {
				return p, port.PeopleScopeDecision{}, problem.Forbidden
			}
			return p, decision, nil
		}
		if p.SelectedSession != nil || p.KN != nil {
			return p, port.PeopleScopeDecision{}, problem.Forbidden
		}
	}
	_, k, err := authz.RequireKN(ctx)
	if err != nil {
		return p, port.PeopleScopeDecision{}, err
	}
	caps, err := s.attributeCapabilities(port.Assignment{Role: k.Role, MgmtLevel: k.MgmtLevel}, port.AssignmentAttrs{ProvinceGroup: k.ProvinceGroup, Duties: k.Duties})
	if err != nil {
		return p, port.PeopleScopeDecision{}, err
	}
	decision := port.PeopleScopeDecision{Mode: port.PeopleScopeSelf, InternalUnit: k.InternalUnit}
	switch {
	case k.Role == port.KNAdmin, k.Role == port.KNTinh && k.Department == partyport.DeptHanhChinhTongHop && !k.ReadOnly() && !caps.ForceReadOnly,
		caps.ReadsAllDepartments:
		decision.Mode = port.PeopleScopeAll
	case k.Role == port.KNTinh && caps.Manager && k.InternalUnit != "":
		decision.Mode, decision.UnitRef = port.PeopleScopeInternalUnit, k.UnitRef
		decision.MemberRole, decision.OfficerRole = port.KNTinh, port.KNXa
	}
	return p, decision, nil
}
