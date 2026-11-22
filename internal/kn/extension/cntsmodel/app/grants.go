package app

// Cấp quyền cán bộ xã, nông hộ trong module CNTS (FN-14, UR-TINH-09): vai trong module, địa bàn (xã, cơ sở giết mổ, điểm,
// hộ), biểu mẫu được giao, hiệu lực; thu hồi có lý do và người nhận việc chưa nộp. Mọi thay đổi là một dòng sổ cnts_grant
// (BR-03 "mọi thay đổi quyền có log"). Người được cấp phải là tài khoản Cấp Xã CNTS có sẵn (Admin tạo, FN-04) — trừ vai
// nông hộ (GĐ2, tài khoản App nông hộ).
//
// Quyền KN này điều khiển các thao tác của module KN (vd cán bộ theo dõi mô hình sửa hộ — UR-XA-04) và, từ #71, phạm vi
// phiên chủ KN của Cấp Xã CNTS (session_grants.go → biểu kỳ cap/forms) — xem docs/kn/gia-dinh/W11.47.md.

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
)

// GrantView là quyền (bản hiện hành) cho API.
type GrantView struct {
	store.Grant
	Status   string
	CanEdit  bool
	Versions []store.Grant // chỉ chi tiết
	Names    map[uuid.UUID]string
	Replayed bool
}

// granter: Admin; Cấp Tỉnh CNTS nhóm tổng hợp, chủ nhiệm, lãnh đạo phòng (hoặc chưa phân nhóm).
func (c caller) granter() bool {
	if !c.writable() {
		return false
	}
	return c.admin() || c.manager() || (c.province() && slices.Contains([]string{"", accport.GroupTongHop, accport.GroupChuNhiem}, c.k.ProvinceGroup))
}

// projectOnly: chủ nhiệm (không phải lãnh đạo phòng/Admin) chỉ cấp quyền trong chương trình mình (FN-14 BR-03).
func (c caller) projectOnly() bool {
	return !c.admin() && !c.manager() && c.k.ProvinceGroup == accport.GroupChuNhiem
}

func (s *Service) grantView(c caller, g store.Grant) GrantView {
	st := domain.GrantStatus(g.Event, g.ValidFrom, g.ValidTo, s.today())
	return GrantView{Grant: g, Status: st, CanEdit: c.granter() && c.sees(g.Province) && st != domain.StatusRevoked &&
		(!c.projectOnly() || g.ActorRef == c.me())}
}

// GrantInput là thân cấp/sửa quyền.
type GrantInput struct {
	Ref         *uuid.UUID // sửa
	BaseVersion *int64     // sửa
	GranteeRef  uuid.UUID  // cấp
	Province    string     // cấp: Admin bắt buộc
	domain.Grant
	IdemKey string
}

func idemGrant(k string) string { return "cgrant:" + k }

// checkGrant kiểm địa bàn, cơ sở, điểm, hộ của quyền thuộc tỉnh province.
func (s *Service) checkGrant(ctx context.Context, tx pgx.Tx, c caller, province string, grantee uuid.UUID, g *domain.Grant) error {
	if es := g.Normalize(); len(es) > 0 {
		return fieldsProblem(ErrGrantInvalid, es)
	}
	if c.projectOnly() && g.Role != domain.RoleFollower && g.Role != domain.RoleFarmer {
		return problem.Forbidden.WithDetail("chủ nhiệm chỉ cấp vai cán bộ theo dõi mô hình, nông hộ trong dự án mình (FN-14 BR-03)")
	}
	for _, code := range g.Communes {
		if err := s.communeIn(ctx, c, ErrGrantInvalid, "commune_codes", code, province); err != nil {
			return err
		}
	}
	if len(g.Facilities) > 0 {
		if s.Catalogs == nil {
			return ErrDependency
		}
		day := s.today()
		fs, err := txdeps.Get(ctx, "facilities:"+strings.Join(g.Communes, ",")+"@"+day.Format(time.DateOnly),
			func(ctx context.Context) ([]knport.SlaughterFacility, error) {
				return s.Catalogs.FacilitiesAt(ctx, day, g.Communes)
			})
		if err != nil {
			return err
		}
		for _, ref := range g.Facilities {
			ok := false
			for _, f := range fs {
				if f.Ref == ref && f.Active && slices.Contains(g.Communes, f.CommuneCode) {
					ok = true
				}
			}
			if !ok {
				return ErrGrantInvalid.WithField("facility_refs", "cơ sở giết mổ không hoạt động hoặc không thuộc xã được giao: "+ref.String())
			}
		}
	}
	mine := func(projectRef uuid.UUID) (bool, error) {
		if !c.projectOnly() {
			return true, nil
		}
		p, err := store.LatestProject(ctx, tx, projectRef)
		return err == nil && p != nil && p.ManagerRef == c.me(), err
	}
	for _, ref := range g.Sites {
		x, err := store.LatestSite(ctx, tx, ref)
		if err != nil {
			return err
		}
		if x == nil || x.Event == domain.EventClosed || !slices.Contains(g.Communes, x.CommuneCode) {
			return ErrGrantInvalid.WithField("site_refs", "điểm không hoạt động hoặc không thuộc xã được giao: "+ref.String())
		}
		if ok, err := mine(x.ProjectRef); err != nil {
			return err
		} else if !ok {
			return ErrOutOfScope.WithField("site_refs", "điểm không thuộc dự án do bạn làm chủ nhiệm (FN-14 BR-03)")
		}
	}
	for _, ref := range g.Households {
		h, err := store.LatestHousehold(ctx, tx, ref)
		if err != nil {
			return err
		}
		if h == nil || h.Event == domain.EventWithdrawn || !slices.Contains(g.Communes, h.CommuneCode) {
			return ErrGrantInvalid.WithField("household_refs", "hộ không còn tham gia hoặc không thuộc xã được giao: "+ref.String())
		}
		if ok, err := mine(h.ProjectRef); err != nil {
			return err
		} else if !ok {
			return ErrOutOfScope.WithField("household_refs", "hộ không thuộc dự án do bạn làm chủ nhiệm (FN-14 BR-03)")
		}
	}
	if c.projectOnly() && len(g.Sites) == 0 && len(g.Households) == 0 {
		return ErrOutOfScope.WithField("site_refs", "chủ nhiệm chỉ giao điểm/hộ của dự án mình (FN-14 BR-03)")
	}
	if g.Role != domain.RoleFarmer {
		idx, err := s.officers(ctx, c)
		if err != nil {
			return err
		}
		if _, ok := idx[grantee]; !ok {
			return ErrGrantInvalid.WithField("grantee_ref", "không phải tài khoản Cấp Xã phòng Chăn nuôi – Thủy sản còn hiệu lực (Admin tạo tài khoản)")
		}
	}
	return nil
}

// activeSameRole tìm quyền còn hiệu lực/chờ hiệu lực khác của cùng người, cùng vai, cùng tỉnh.
func activeSameRole(gs []store.Grant, grantee uuid.UUID, role, province string, except uuid.UUID, today time.Time) bool {
	for _, g := range gs {
		if g.Ref != except && g.GranteeRef == grantee && g.Role == role && g.Province == province &&
			domain.GrantStatus(g.Event, g.ValidFrom, g.ValidTo, today) != domain.StatusRevoked &&
			domain.GrantStatus(g.Event, g.ValidFrom, g.ValidTo, today) != domain.StatusExpired {
			return true
		}
	}
	return false
}

// CreateGrant cấp quyền.
func (s *Service) CreateGrant(ctx context.Context, in GrantInput) (GrantView, error) {
	ctx = txdeps.With(ctx) // P0-POOL-KN-2: port module khác chỉ đọc ngoài giao dịch (service.go tx)
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return GrantView{}, err
	}
	if !c.granter() {
		return GrantView{}, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh (tổng hợp, chủ nhiệm, lãnh đạo phòng) hoặc Admin cấp quyền")
	}
	if in.GranteeRef == uuid.Nil {
		return GrantView{}, ErrGrantInvalid.WithField("grantee_ref", "bắt buộc")
	}
	if in.GranteeRef == c.me() {
		return GrantView{}, ErrGrantInvalid.WithField("grantee_ref", "không tự cấp quyền cho mình")
	}
	if in.ValidFrom.IsZero() {
		in.ValidFrom = s.today()
	}
	province, err := s.callerProvince(ctx, c, in.Province)
	if err != nil {
		return GrantView{}, err
	}
	var out store.Grant
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.GrantByIdem(ctx, tx, idemGrant(in.IdemKey)); err != nil || prev != nil {
			if prev != nil {
				out, replayed = *prev, true
			}
			return err
		}
		if err := store.Lock(ctx, tx, "grantee:"+in.GranteeRef.String()); err != nil {
			return err
		}
		if err := s.checkGrant(ctx, tx, c, province, in.GranteeRef, &in.Grant); err != nil {
			return err
		}
		gs, err := store.ListGrants(ctx, tx, &in.GranteeRef)
		if err != nil {
			return err
		}
		if activeSameRole(gs, in.GranteeRef, in.Role, province, uuid.Nil, s.today()) {
			return ErrGrantExists
		}
		out = store.Grant{Meta: c.meta(s.now(), idemGrant(in.IdemKey)), Ref: ids.New(), Event: domain.EventGranted,
			GranteeRef: in.GranteeRef, Province: province, Grant: in.Grant}
		return store.InsertGrant(ctx, tx, c.p.TenantID, &out)
	})
	if dup(err) {
		return s.CreateGrant(ctx, in)
	}
	if err != nil {
		return GrantView{}, err
	}
	v, err := s.GetGrant(ctx, out.Ref)
	v.Replayed = replayed
	return v, err
}

func (s *Service) lockGrant(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID, base *int64) (*store.Grant, error) {
	g, err := store.LatestGrant(ctx, tx, ref)
	if err != nil {
		return nil, err
	}
	if g == nil || !c.sees(g.Province) {
		return nil, ErrGrantNotFound
	}
	if err := store.Lock(ctx, tx, "grantee:"+g.GranteeRef.String()); err != nil {
		return nil, err
	}
	if g, err = store.LatestGrant(ctx, tx, ref); err != nil {
		return nil, err
	}
	v := s.grantView(c, *g)
	switch {
	case g.Event == domain.EventRevoked:
		return nil, ErrGrantRevoked
	case !v.CanEdit:
		return nil, problem.Forbidden.WithDetail("chỉ người cấp (chủ nhiệm) hoặc Cấp Tỉnh tổng hợp, lãnh đạo phòng, Admin sửa quyền này")
	case !versionOK(base, g.Seq):
		return nil, ErrGrantConflict
	}
	return g, nil
}

// UpdateGrant sửa phạm vi, biểu mẫu, hiệu lực của quyền (người được cấp, vai không đổi).
func (s *Service) UpdateGrant(ctx context.Context, in GrantInput) (GrantView, error) {
	ctx = txdeps.With(ctx) // P0-POOL-KN-2: port module khác chỉ đọc ngoài giao dịch (service.go tx)
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return GrantView{}, err
	}
	if in.Ref == nil {
		return GrantView{}, ErrGrantNotFound
	}
	if in.BaseVersion == nil {
		return GrantView{}, errBaseVersion
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.GrantByIdem(ctx, tx, idemGrant(in.IdemKey)); err != nil || prev != nil {
			return err
		}
		g, err := s.lockGrant(ctx, tx, c, *in.Ref, in.BaseVersion)
		if err != nil {
			return err
		}
		if in.Role != "" && in.Role != g.Role {
			return ErrGrantInvalid.WithField("role", "không đổi vai — thu hồi và cấp quyền mới")
		}
		in.Role = g.Role
		if in.ValidFrom.IsZero() {
			in.ValidFrom = g.ValidFrom
		}
		if err := s.checkGrant(ctx, tx, c, g.Province, g.GranteeRef, &in.Grant); err != nil {
			return err
		}
		n := store.Grant{Meta: c.meta(s.now(), idemGrant(in.IdemKey)), Ref: g.Ref, Event: domain.EventUpdated, GranteeRef: g.GranteeRef,
			Province: g.Province, Grant: in.Grant}
		return store.InsertGrant(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return GrantView{}, err
	}
	return s.GetGrant(ctx, *in.Ref)
}

// RevokeInput là thân thu hồi quyền.
type RevokeInput struct {
	Ref          uuid.UUID
	BaseVersion  *int64
	Reason       string
	SuccessorRef *uuid.UUID // người nhận việc chưa nộp: được cấp quyền mới cùng phạm vi (FN-14 AC-02)
	IdemKey      string
}

// RevokeGrant thu hồi quyền; có người nhận → cấp quyền cùng phạm vi cho người nhận trong cùng giao dịch.
func (s *Service) RevokeGrant(ctx context.Context, in RevokeInput) (GrantView, error) {
	ctx = txdeps.With(ctx) // P0-POOL-KN-2: port module khác chỉ đọc ngoài giao dịch (service.go tx)
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return GrantView{}, err
	}
	if in.BaseVersion == nil {
		return GrantView{}, errBaseVersion
	}
	if !domain.ValidReason(in.Reason) {
		return GrantView{}, problem.Validation.WithField("reason", "5–1000 ký tự")
	}
	in.Reason = strings.TrimSpace(in.Reason)
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.GrantByIdem(ctx, tx, idemGrant(in.IdemKey)); err != nil || prev != nil {
			return err
		}
		g, err := s.lockGrant(ctx, tx, c, in.Ref, in.BaseVersion)
		if err != nil {
			return err
		}
		n := *g
		n.Meta, n.Event, n.Reason = c.meta(s.now(), idemGrant(in.IdemKey)), domain.EventRevoked, in.Reason
		if in.SuccessorRef != nil {
			succ := *in.SuccessorRef
			if succ == g.GranteeRef || succ == uuid.Nil {
				return ErrGrantInvalid.WithField("successor_ref", "người nhận việc phải khác người bị thu hồi")
			}
			if err := store.Lock(ctx, tx, "grantee:"+succ.String()); err != nil {
				return err
			}
			scope := g.Grant
			if scope.ValidFrom.Before(s.today()) {
				scope.ValidFrom = s.today()
			}
			if scope.ValidTo != nil && scope.ValidTo.Before(scope.ValidFrom) {
				scope.ValidTo = nil
			}
			if err := s.checkGrant(ctx, tx, c, g.Province, succ, &scope); err != nil {
				return err
			}
			gs, err := store.ListGrants(ctx, tx, &succ)
			if err != nil {
				return err
			}
			if activeSameRole(gs, succ, g.Role, g.Province, uuid.Nil, s.today()) {
				return ErrGrantExists.WithField("successor_ref", "người nhận đã có quyền cùng vai — sửa quyền đó")
			}
			ng := store.Grant{Meta: c.meta(s.now(), idemGrant(in.IdemKey)+":successor"), Ref: ids.New(), Event: domain.EventGranted,
				GranteeRef: succ, Province: g.Province, Reason: "Nhận việc khi thu hồi quyền " + g.Ref.String(), Grant: scope}
			if err := store.InsertGrant(ctx, tx, c.p.TenantID, &ng); err != nil {
				return err
			}
			n.SuccessorRef, n.SuccessorGrantRef = &succ, &ng.Ref
		}
		return store.InsertGrant(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return GrantView{}, err
	}
	return s.GetGrant(ctx, in.Ref)
}

// GrantQuery lọc danh sách quyền.
type GrantQuery struct {
	Mine    bool // quyền của chính người gọi (mọi vai)
	Grantee *uuid.UUID
	Role    string
	Status  string
}

// ListGrants liệt kê quyền (Cấp Tỉnh/Admin: theo tỉnh; mine: của mình).
func (s *Service) ListGrants(ctx context.Context, q GrantQuery) ([]GrantView, map[uuid.UUID]string, bool, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	who := q.Grantee
	if q.Mine {
		who = &c.p.ActorRef
	} else if !c.admin() && !c.province() {
		return nil, nil, false, problem.Forbidden.WithDetail("Cấp Xã chỉ xem quyền của mình (mine=true)")
	}
	var gs []store.Grant
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		gs, e = store.ListGrants(ctx, tx, who)
		return e
	}); err != nil {
		return nil, nil, false, err
	}
	out := []GrantView{}
	refs := []uuid.UUID{}
	for _, g := range gs {
		if !q.Mine && !c.sees(g.Province) {
			continue
		}
		v := s.grantView(c, g)
		if (q.Role != "" && g.Role != q.Role) || (q.Status != "" && v.Status != q.Status) {
			continue
		}
		out = append(out, v)
		refs = append(refs, g.GranteeRef, g.ActorRef)
	}
	return out, s.names(ctx, refs), c.granter(), nil
}

// GetGrant đọc quyền kèm nhật ký thay đổi.
func (s *Service) GetGrant(ctx context.Context, ref uuid.UUID) (GrantView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return GrantView{}, err
	}
	var vs []store.Grant
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		vs, e = store.GrantVersions(ctx, tx, ref)
		return e
	}); err != nil {
		return GrantView{}, err
	}
	if len(vs) == 0 {
		return GrantView{}, ErrGrantNotFound
	}
	cur := vs[len(vs)-1]
	if cur.GranteeRef != c.me() && ((!c.admin() && !c.province()) || !c.sees(cur.Province)) {
		return GrantView{}, ErrGrantNotFound
	}
	v := s.grantView(c, cur)
	v.Versions = vs
	refs := []uuid.UUID{cur.GranteeRef}
	if cur.SuccessorRef != nil {
		refs = append(refs, *cur.SuccessorRef)
	}
	for _, x := range vs {
		refs = append(refs, x.ActorRef)
	}
	v.Names = s.names(ctx, refs)
	return v, nil
}

// Candidate là tài khoản Cấp Xã CNTS có thể được cấp quyền / chọn làm cán bộ theo dõi.
type Candidate struct {
	Ref      uuid.UUID
	Communes []string
}

// Candidates liệt kê cán bộ Cấp Xã CNTS phụ trách ít nhất một xã trong phạm vi người gọi.
func (s *Service) Candidates(ctx context.Context) ([]Candidate, map[uuid.UUID]string, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, err
	}
	if !c.admin() && !c.province() {
		return nil, nil, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh, Admin")
	}
	idx, err := s.officers(ctx, c)
	if err != nil {
		return nil, nil, err
	}
	out := []Candidate{}
	refs := []uuid.UUID{}
	for _, x := range idx {
		var cs []string
		for _, code := range x.Communes {
			if c.sees(code) {
				cs = append(cs, code)
			}
		}
		if len(cs) == 0 && !c.admin() {
			continue
		}
		if cs == nil {
			cs = []string{}
		}
		out = append(out, Candidate{Ref: x.ActorRef, Communes: cs})
		refs = append(refs, x.ActorRef)
	}
	slices.SortFunc(out, func(a, b Candidate) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return out, s.names(ctx, refs), nil
}
