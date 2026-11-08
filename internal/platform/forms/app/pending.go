package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

var _ port.PendingReader = Control{}

type pendingCursor struct {
	Query, Authority, Snapshot string
	Period                     uuid.UUID
	Unit                       string
}

// PendingSubmissions never falls back to Progress or system/explicit-tenant reads.
// Every continuation revalidates current authority before reading even its cursor.
func (c Control) PendingSubmissions(ctx context.Context, in port.PendingQuery) (port.PendingPage, error) {
	q, err := port.NormalizePendingQuery(in)
	if err != nil {
		return port.PendingPage{}, err
	}
	if c.Svc == nil || c.Svc.DB == nil {
		return port.PendingPage{}, port.ErrPendingUnavailable
	}
	if _, joined := db.JoinedTx(ctx); joined {
		return port.PendingPage{}, port.ErrPendingUnavailable
	}
	policy := c.Svc.PendingPolicies[q.Profile]
	if policy == nil {
		return port.PendingPage{}, port.ErrPendingUnavailable
	}
	p, ok := authz.FromContext(ctx)
	if !ok {
		return port.PendingPage{}, problem.Unauthenticated
	}
	if !p.MFA || p.ActorRef == uuid.Nil || p.TenantID == uuid.Nil {
		return port.PendingPage{}, problem.Forbidden
	}
	prof, a, err := c.Svc.resolve(ctx, q.Profile)
	if err != nil {
		return port.PendingPage{}, err
	}
	if !slices.Contains(prof.Kinds(), q.UnitKind) {
		return port.PendingPage{}, problem.Validation.WithField("unit_kind", "loại đơn vị không thuộc profile")
	}
	// A callback receives its own slices, not the canonical query used below.
	request, _ := port.NormalizePendingQuery(q)
	principal := p
	principal.Roles = slices.Clone(p.Roles)
	if p.SelectedSession != nil {
		selected := *p.SelectedSession
		principal.SelectedSession = &selected
	}
	grant, err := policy(ctx, principal, request)
	if err != nil {
		return port.PendingPage{}, err
	}
	if !grant.Allowed {
		return port.PendingPage{}, problem.Forbidden
	}
	grant, err = port.NormalizePendingGrant(grant)
	if err != nil {
		return port.PendingPage{}, err
	}
	// Arbitrary owner closures cannot be faithfully pushed into SQL. Explicit
	// finite selectors are evaluated outside TX; all+opaque filter is unavailable.
	scope := grant.Scope
	if a.templateFilter != nil {
		if scope.AllTemplates {
			return port.PendingPage{}, port.ErrPendingUnavailable
		}
		scope.TemplateCodes = slices.DeleteFunc(scope.TemplateCodes, func(code string) bool { return !a.allowsTemplate(code) })
	}
	if a.targetFilter != nil && (q.UnitKind == port.UnitFacility || q.UnitKind == port.UnitHousehold) {
		if scope.AllUnits {
			return port.PendingPage{}, port.ErrPendingUnavailable
		}
		permitted := make([]string, 0, len(scope.Units))
		for _, unit := range scope.Units {
			ref, e := uuid.Parse(unit)
			if e != nil {
				return port.PendingPage{}, port.ErrPendingUnavailable
			}
			if a.targetFilter(q.UnitKind, ref) {
				permitted = append(permitted, unit)
			}
		}
		scope.Units = permitted
	}
	grant.Scope = scope
	clean := q
	clean.Cursor = ""
	queryHash := pendingHash(clean)
	roles := slices.Clone(p.Roles)
	slices.Sort(roles)
	units := slices.Clone(a.scope.Units)
	slices.Sort(units)
	var stamp []byte
	if p.SelectedSession != nil {
		stamp, err = p.SelectedSession.AuthorizationStamp()
		if err != nil {
			return port.PendingPage{}, problem.Forbidden
		}
	}
	authority := pendingHash(struct {
		Actor, Tenant, Org, Node uuid.UUID
		Roles, Units             []string
		Role, Level, Department  string
		All, MFA                 bool
		Session                  []byte
		Grant                    port.PendingGrant
	}{p.ActorRef, p.TenantID, p.OrgRef, a.node, roles, units, a.role, a.level, a.dept, a.scope.All, p.MFA, stamp, grant})
	cursor, err := decodePendingCursor(q.Cursor)
	if err != nil {
		return port.PendingPage{}, err
	}
	if q.Cursor != "" && (cursor.Query != queryHash || cursor.Authority != authority) {
		return port.PendingPage{}, port.ErrPendingChanged
	}
	var result store.PendingResult
	err = c.Svc.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		var e error
		result, e = store.PendingNative(ctx, tx, store.PendingSelection{Query: q, Scope: scope, NativeAll: a.scope.All, NativeUnits: units,
			NativeDepartment: a.dept, NativeDepartmentAll: a.scope.All || (!a.leaf && a.level != ""), AfterPeriod: cursor.Period, AfterUnit: cursor.Unit})
		return e
	})
	if err != nil {
		return port.PendingPage{}, err
	}
	if err = validatePendingResult(result, q.Limit); err != nil {
		return port.PendingPage{}, err
	}
	snapshot := result.Visibility
	if q.Cursor != "" && (cursor.Snapshot != snapshot || !result.AnchorFound) {
		return port.PendingPage{}, port.ErrPendingChanged
	}
	page := port.PendingPage{Items: result.Items, Count: result.Count, CoverageComplete: true, QueryFingerprint: queryHash, AuthorityFingerprint: authority, Snapshot: snapshot}
	if len(page.Items) > q.Limit {
		page.Items = page.Items[:q.Limit]
		last := page.Items[len(page.Items)-1]
		body, e := json.Marshal(pendingCursor{queryHash, authority, snapshot, last.PeriodID, last.Unit})
		if e != nil {
			return port.PendingPage{}, e
		}
		page.NextCursor = base64.RawURLEncoding.EncodeToString(body)
	}
	return page, nil
}

func pendingHash(v any) string {
	raw, _ := json.Marshal(v) // Only concrete, bounded structs with no custom marshalers.
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func decodePendingCursor(raw string) (pendingCursor, error) {
	if raw == "" {
		return pendingCursor{}, nil
	}
	body, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return pendingCursor{}, problem.Validation.WithField("cursor", "mốc trang không hợp lệ")
	}
	var out pendingCursor
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&out); err != nil {
		return pendingCursor{}, problem.Validation.WithField("cursor", "mốc trang không hợp lệ")
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF || len(out.Query) != 64 || len(out.Authority) != 64 || len(out.Snapshot) != 64 || out.Period == uuid.Nil || !domain.ValidateUnit(out.Unit) {
		return pendingCursor{}, problem.Validation.WithField("cursor", "mốc trang không hợp lệ")
	}
	return out, nil
}

func validatePendingResult(r store.PendingResult, limit int) error {
	if !r.CoverageComplete || r.Invalid || r.Count < 0 || r.Candidates < r.Count || len(r.Visibility) != 64 || len(r.Items) > limit+1 || int64(len(r.Items)) > r.Count {
		return port.ErrPendingUnavailable
	}
	identities := map[uuid.UUID]bool{}
	keys := map[string]bool{}
	for _, item := range r.Items {
		key := item.PeriodID.String() + ":" + item.Unit
		if item.PeriodID == uuid.Nil || item.SubmissionID == uuid.Nil || item.Attempt < 1 || item.SubmittedAt.IsZero() ||
			item.Status != domain.SubSubmitted || item.Decision != "" || !domain.ValidateUnit(item.Unit) || !domain.ValidTemplateCode(item.TemplateCode) || identities[item.SubmissionID] || keys[key] {
			return port.ErrPendingUnavailable
		}
		identities[item.SubmissionID] = true
		keys[key] = true
	}
	return nil
}
