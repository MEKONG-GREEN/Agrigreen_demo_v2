package app

import (
	"context"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func validateCatalogConstraints(d port.CatalogDefinition) {
	c := d.Constraints
	if c == nil {
		return
	}
	attribute := func(a string) {
		if _, ok := d.Attributes[a]; !ok {
			panic("masterdata: undeclared constraint attribute " + a)
		}
	}
	issue := func(i port.CatalogConstraintIssue) {
		field, ok := strings.CutPrefix(i.Field, "attrs.")
		if i.Status < 400 || i.Status > 499 || i.Code == "" || i.Title == "" || !ok || !catalogAttributePattern.MatchString(field) {
			panic("masterdata: invalid catalog constraint issue " + d.Catalog)
		}
	}
	if c.StableUUIDAttribute != "" {
		attribute(c.StableUUIDAttribute)
	}
	if u := c.AdminUnit; u != nil {
		attribute(u.Attribute)
		if !slices.Contains([]string{"province", "district", "commune"}, u.Level) {
			panic("masterdata: invalid administrative level " + u.Level)
		}
		issue(u.Issue)
	}
	if u := c.UniqueTokens; u != nil {
		if len(u.Attributes) == 0 || strings.TrimSpace(u.Separator) == "" {
			panic("masterdata: empty token constraint " + d.Catalog)
		}
		seen := map[string]bool{}
		for _, a := range u.Attributes {
			attribute(a)
			if seen[a] {
				panic("masterdata: duplicate token attribute " + a)
			}
			seen[a] = true
		}
		issue(u.Issue)
	}
}

func constraintProblem(i port.CatalogConstraintIssue, reason string) error {
	return problem.New(i.Status, i.Code, i.Title).WithField(i.Field, reason)
}

// prepareCatalogIdentity ignores a caller's UUID before normalization, on creates and updates.
func (s *Service) prepareCatalogIdentity(in domain.Item) domain.Item {
	if d, ok := s.CatalogRegistry.definition(in.Catalog); ok && d.Constraints != nil {
		if a := d.Constraints.StableUUIDAttribute; a != "" {
			in.Attrs = withAttr(in.Attrs, a, ids.New().String())
		}
		return in
	}
	return prepareFacilityRef(in) // temporary compatibility path, removed after owner proof
}

func (s *Service) keepCatalogIdentity(upd, cur domain.Item) domain.Item {
	if d, ok := s.CatalogRegistry.definition(upd.Catalog); ok && d.Constraints != nil {
		if a := d.Constraints.StableUUIDAttribute; a != "" && strings.TrimSpace(cur.Attrs[a]) != "" {
			upd.Attrs = withAttr(upd.Attrs, a, cur.Attrs[a])
		}
		return upd
	}
	return keepFacilityRef(upd, cur) // temporary compatibility path
}

func catalogReferences(c port.CatalogConstraints, attrs map[string]string) ([]port.CatalogReference, error) {
	if c.References == nil {
		return nil, nil
	}
	return c.References(maps.Clone(attrs))
}

func (s *Service) checkCatalogConstraints(ctx context.Context, tx pgx.Tx, it domain.Item, on time.Time, c port.CatalogConstraints) error {
	refs, err := catalogReferences(c, it.Attrs)
	if err != nil {
		return asProblem(err)
	}
	for _, r := range refs {
		if !s.catalogKnown(r.Catalog) {
			return ErrCatalogRef.WithField(r.Field, r.Code)
		}
		if err := activeRef(ctx, tx, r.Catalog, r.Code, r.Field); err != nil {
			return err
		}
	}
	if u := c.AdminUnit; u != nil {
		code := it.Attrs[u.Attribute]
		unit, err := store.UnitAt(ctx, tx, code, on)
		if err != nil {
			return err
		}
		if unit == nil || string(unit.Level) != u.Level {
			return constraintProblem(u.Issue, code)
		}
	}
	if u := c.UniqueTokens; u != nil {
		var tokens []string
		for _, a := range u.Attributes {
			if value := it.Attrs[a]; strings.TrimSpace(value) != "" {
				tokens = append(tokens, strings.Split(value, u.Separator)...)
			}
		}
		owners, err := store.AttributeTokenOwners(ctx, tx, it.Catalog, it.Code, u.Attributes, u.Separator, tokens)
		if err != nil {
			return err
		}
		for _, token := range tokens {
			if owner, taken := owners[token]; taken {
				return constraintProblem(u.Issue, token+" → "+owner)
			}
		}
	}
	return nil
}
