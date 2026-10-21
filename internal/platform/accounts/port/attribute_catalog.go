package port

import (
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// AttributeCatalogSpec is trusted application-owned data, never request identity.
// Flags are explicit: Manager does not imply ReadsAllDepartments.
type AttributeCatalogSpec struct {
	Version     string
	Provisional bool
	Levels      []MgmtLevel
	Groups      []AttributeGroup
	Duties      []CatalogItem
	Roles       []AttributeRole
}

type AttributeGroup struct {
	Code                string
	Label               string
	Manager             bool
	ReadsAllDepartments bool
	ReadOnly            bool
}

type AttributeRole struct {
	Role                  string
	GroupAndDuties        bool
	ScopedTargets         bool
	ManagementPermissions bool
}

type AttributeCapabilities struct {
	Manager             bool
	ReadsAllDepartments bool
	ForceReadOnly       bool
	ScopedTargets       bool
}

var ErrAttributeCatalog = errors.New("accounts: invalid attribute catalog")

// AttributeCodeError identifies unrecognized stored data without exposing it.
type AttributeCodeError struct{ Field string }

func (e AttributeCodeError) Error() string { return "accounts: unknown attribute " + e.Field }

// AttributeCatalog is an immutable instance snapshot. Zero/nil is invalid and
// never selects legacy data. Accessors return copies; labels never infer flags.
type AttributeCatalog struct {
	initialized     bool
	version         string
	provisional     bool
	levels          []MgmtLevel
	groups          []AttributeGroup
	duties          []CatalogItem
	roles           []AttributeRole
	managementCodes map[string]string
}

var attributeCodeRE = regexp.MustCompile(`^[a-z][a-z0-9_]{1,39}$`)

func NewAttributeCatalog(spec AttributeCatalogSpec) (*AttributeCatalog, error) {
	if strings.TrimSpace(spec.Version) == "" || len(spec.Roles) == 0 {
		return nil, ErrAttributeCatalog
	}
	c := &AttributeCatalog{version: spec.Version, provisional: spec.Provisional,
		levels: slices.Clone(spec.Levels), groups: slices.Clone(spec.Groups),
		duties: slices.Clone(spec.Duties), roles: slices.Clone(spec.Roles), managementCodes: map[string]string{}}
	check := func(code, label string, seen map[string]bool) error {
		if !attributeCodeRE.MatchString(code) || strings.TrimSpace(label) == "" || label != strings.TrimSpace(label) || seen[code] {
			return ErrAttributeCatalog
		}
		seen[code] = true
		return nil
	}
	seen := map[string]bool{}
	for _, l := range c.levels {
		if err := check(l.Code, l.Label, seen); err != nil {
			return nil, err
		}
		for _, alias := range []string{l.Code, l.Label} {
			key := tabular.NormalizeHeader(alias)
			if key == "" {
				return nil, ErrAttributeCatalog
			}
			if old, ok := c.managementCodes[key]; ok && old != l.Code {
				return nil, fmt.Errorf("%w: ambiguous management code or label", ErrAttributeCatalog)
			}
			c.managementCodes[key] = l.Code
		}
	}
	seen = map[string]bool{}
	for _, g := range c.groups {
		if err := check(g.Code, g.Label, seen); err != nil {
			return nil, err
		}
	}
	seen = map[string]bool{}
	for _, d := range c.duties {
		if err := check(d.Code, d.Label, seen); err != nil {
			return nil, err
		}
	}
	seen = map[string]bool{}
	for _, r := range c.roles {
		if !attributeCodeRE.MatchString(r.Role) || seen[r.Role] {
			return nil, ErrAttributeCatalog
		}
		seen[r.Role] = true
	}
	c.initialized = true
	return c, nil
}

func (c *AttributeCatalog) Valid() bool { return c != nil && c.initialized }
func (c *AttributeCatalog) Clone() *AttributeCatalog {
	if c == nil {
		return nil
	}
	v := *c
	v.levels = slices.Clone(c.levels)
	v.groups = slices.Clone(c.groups)
	v.duties = slices.Clone(c.duties)
	v.roles = slices.Clone(c.roles)
	v.managementCodes = maps.Clone(c.managementCodes)
	return &v
}
func (c *AttributeCatalog) Version() string {
	if !c.Valid() {
		return ""
	}
	return c.version
}
func (c *AttributeCatalog) Provisional() bool { return c.Valid() && c.provisional }
func (c *AttributeCatalog) Levels() []MgmtLevel {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.levels)
}
func (c *AttributeCatalog) Groups() []AttributeGroup {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.groups)
}
func (c *AttributeCatalog) Duties() []CatalogItem {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.duties)
}
func (c *AttributeCatalog) Roles() []AttributeRole {
	if !c.Valid() {
		return nil
	}
	return slices.Clone(c.roles)
}
func (c *AttributeCatalog) LevelOf(code string) (MgmtLevel, bool) {
	if c.Valid() {
		for _, l := range c.levels {
			if l.Code == code {
				return l, true
			}
		}
	}
	return MgmtLevel{}, false
}
func (c *AttributeCatalog) LevelLabel(code string) string { l, _ := c.LevelOf(code); return l.Label }
func (c *AttributeCatalog) GroupLabel(code string) string {
	if c.Valid() {
		for _, g := range c.groups {
			if g.Code == code {
				return g.Label
			}
		}
	}
	return ""
}
func (c *AttributeCatalog) DutyLabel(code string) string {
	if !c.Valid() {
		return ""
	}
	return CatalogLabel(c.duties, code)
}
func (c *AttributeCatalog) Role(role string) (AttributeRole, bool) {
	if c.Valid() {
		for _, r := range c.roles {
			if r.Role == role {
				return r, true
			}
		}
	}
	return AttributeRole{}, false
}

// ManagementCode uses precisely the CSV importer's normalization. Construction
// rejects ambiguous labels/codes so import cannot silently select a leader.
func (c *AttributeCatalog) ManagementCode(cell string) (string, bool) {
	if !c.Valid() {
		return "", false
	}
	if strings.TrimSpace(cell) == "" {
		return "", true
	}
	code, ok := c.managementCodes[tabular.NormalizeHeader(cell)]
	return code, ok
}
func (c *AttributeCatalog) Capabilities(role, level, group string) (AttributeCapabilities, error) {
	if !c.Valid() {
		return AttributeCapabilities{}, ErrAttributeCatalog
	}
	r, ok := c.Role(role)
	if !ok {
		return AttributeCapabilities{}, AttributeCodeError{Field: "role"}
	}
	var l MgmtLevel
	if level != "" {
		l, ok = c.LevelOf(level)
		if !ok {
			return AttributeCapabilities{}, AttributeCodeError{Field: "mgmt_level"}
		}
	}
	var g AttributeGroup
	if group != "" {
		found := false
		for _, candidate := range c.groups {
			if candidate.Code == group {
				g, found = candidate, true
				break
			}
		}
		if !found {
			return AttributeCapabilities{}, AttributeCodeError{Field: "province_group"}
		}
	}
	return AttributeCapabilities{Manager: r.ManagementPermissions && (l.Manager || g.Manager),
		ReadsAllDepartments: r.ManagementPermissions && (l.ReadsAllDepartments || g.ReadsAllDepartments),
		ForceReadOnly:       r.ManagementPermissions && g.ReadOnly, ScopedTargets: r.ScopedTargets}, nil
}

// LegacyAttributeCatalog is the temporary bridge until the owner adapter is
// injected. It snapshots old data; callers never mutate the published instance.
func LegacyAttributeCatalog() *AttributeCatalog {
	groups := make([]AttributeGroup, 0, len(ProvinceGroups))
	for _, g := range ProvinceGroups {
		groups = append(groups, AttributeGroup{Code: g.Code, Label: g.Label,
			Manager: g.Code == GroupLanhDaoPhong, ReadsAllDepartments: g.Code == GroupBanGiamDoc, ReadOnly: g.Code == GroupBanGiamDoc})
	}
	roles := make([]AttributeRole, 0, len(KNRolesAll))
	for _, role := range KNRolesAll {
		roles = append(roles, AttributeRole{Role: role, GroupAndDuties: role == KNTinh, ScopedTargets: role == KNXa, ManagementPermissions: role == KNTinh})
	}
	c, err := NewAttributeCatalog(AttributeCatalogSpec{Version: MgmtCatalogVersion, Provisional: true, Levels: MgmtLevels, Groups: groups, Duties: Duties, Roles: roles})
	if err != nil {
		panic(err)
	}
	return c
}
