package app

// Hộ mô hình (UR-XA-04: thông tin, tọa độ, mô hình, người ghi nhật ký; FN-10 phần hộ). Hộ là bản ghi của KN gắn một điểm
// trình diễn; chủ hộ ở kho định danh (person_ref) — họ tên, SĐT không vào sổ KN. Người sửa được dự án, hoặc Cấp Xã là
// cán bộ theo dõi điểm / có quyền "cán bộ theo dõi mô hình" bao điểm/hộ, được thêm, sửa, rút hộ. Hộ mới tự hiện ở danh
// sách hộ của xã (lịch tiêm, nhật ký — UR-TINH-19 quy tắc).

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/txdeps"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// HouseholdView là hộ (bản hiện hành) cho API.
type HouseholdView struct {
	store.Household
	SiteName    string
	ProjectCode string
	Status      string
	Point       *domain.Point
	OwnerLabel  string // nhãn che bớt từ kho định danh
	OwnerPhone  string // SĐT che một phần (BR-02)
	CanEdit     bool
	Versions    []store.Household // chỉ chi tiết
	Names       map[uuid.UUID]string
	Replayed    bool
}

func householdStatus(event string) string {
	if event == domain.EventWithdrawn {
		return domain.StatusWithdrawn
	}
	return domain.StatusActive
}

// seesHousehold: theo xã; Cấp Xã có giới hạn hộ (W11.07) chỉ thấy hộ được giao.
func (c caller) seesHousehold(h store.Household) bool {
	if !c.sees(h.CommuneCode) {
		return false
	}
	return !c.commune() || c.k.AllowsHousehold(h.Ref)
}

func (c caller) canEditHousehold(p store.Project, x store.Site, h uuid.UUID, grants []store.Grant) bool {
	if p.Event == domain.EventClosed || x.Event == domain.EventClosed {
		return false
	}
	return c.canEditProject(p) || c.follows(x, h, grants)
}

func (s *Service) householdView(c caller, p store.Project, x store.Site, h store.Household, grants []store.Grant) HouseholdView {
	return HouseholdView{Household: h, SiteName: x.Name, ProjectCode: p.Code, Status: householdStatus(h.Event),
		CanEdit: c.canEditHousehold(p, x, h.Ref, grants)}
}

// maxOwnerLabels giới hạn số lần gọi kho định danh của một danh sách.
const maxOwnerLabels = 300

// ownerLabels điền nhãn chủ hộ (che bớt) từ kho định danh; lỗi kho không chặn đọc.
func (s *Service) ownerLabels(ctx context.Context, vs []HouseholdView) {
	if s.Vault == nil {
		return
	}
	for i := range vs {
		if i >= maxOwnerLabels {
			break
		}
		d, err := s.Vault.Display(ctx, client.PurposeFieldSupport, vs[i].PersonRef)
		if err != nil && !errors.Is(err, client.ErrErased) {
			continue
		}
		vs[i].OwnerLabel, vs[i].OwnerPhone = d.Label, d.MaskedPhone
	}
}

// HouseholdInput là thân tạo/sửa hộ.
type HouseholdInput struct {
	Ref         *uuid.UUID // tạo: UUIDv7 tùy chọn; sửa: mã hộ
	SiteRef     uuid.UUID  // tạo
	BaseVersion *int64     // sửa
	PersonRef   *uuid.UUID // tạo: chủ thể có sẵn ở kho định danh, HOẶC Owner
	Owner       *domain.Owner
	Point       *domain.Point
	RemovePoint bool
	domain.Household
	IdemKey string
}

func idemHousehold(k string) string { return "chh:" + k }

func (s *Service) checkHousehold(ctx context.Context, c caller, x store.Site, in *HouseholdInput, cur *store.Household) error {
	if es := in.Normalize(); len(es) > 0 {
		return fieldsProblem(ErrHouseholdInvalid, es)
	}
	if in.Point != nil {
		if es := in.Point.Validate("location"); len(es) > 0 {
			return fieldsProblem(ErrHouseholdInvalid, es)
		}
	}
	if err := noPII(ErrHouseholdInvalid, map[string]string{"note": in.Note}); err != nil {
		return err
	}
	if in.KeeperRef != nil && (x.OfficerRef == nil || *x.OfficerRef != *in.KeeperRef) &&
		(cur == nil || cur.KeeperRef == nil || *cur.KeeperRef != *in.KeeperRef) {
		if err := s.checkOfficer(ctx, c, ErrHouseholdInvalid, "keeper_ref", *in.KeeperRef, x.CommuneCode); err != nil {
			return err
		}
	}
	return nil
}

// person trả chủ thể kho định danh của chủ hộ: ref có sẵn, hoặc tạo mới từ họ tên/SĐT (trùng SĐT → dùng chủ thể cũ).
func (s *Service) person(ctx context.Context, in HouseholdInput, commune string) (uuid.UUID, error) {
	switch {
	case in.PersonRef != nil && in.Owner != nil:
		return uuid.Nil, ErrHouseholdInvalid.WithField("person_ref", "chọn chủ thể có sẵn HOẶC nhập họ tên chủ hộ")
	case in.PersonRef != nil:
		if s.Vault == nil {
			return uuid.Nil, ErrDependency
		}
		if _, err := s.Vault.Display(ctx, client.PurposeFieldSupport, *in.PersonRef); err != nil {
			if errors.Is(err, client.ErrNotFound) || errors.Is(err, client.ErrErased) {
				return uuid.Nil, ErrHouseholdInvalid.WithField("person_ref", "không có chủ thể trong kho định danh")
			}
			return uuid.Nil, err
		}
		return *in.PersonRef, nil
	case in.Owner != nil:
		if es := in.Owner.Validate(); len(es) > 0 {
			return uuid.Nil, fieldsProblem(ErrHouseholdInvalid, es)
		}
		if s.Vault == nil {
			return uuid.Nil, ErrDependency
		}
		cr, err := s.Vault.Create(ctx, client.PurposeContact, client.Identity{Kind: client.SubjectHousehold,
			FullName: in.Owner.FullName, Phone: in.Owner.Phone, AreaCode: commune})
		if errors.Is(err, client.ErrDuplicate) && cr.Ref != uuid.Nil {
			return cr.Ref, nil
		}
		if err != nil {
			return uuid.Nil, err
		}
		return cr.Ref, nil
	}
	return uuid.Nil, ErrHouseholdInvalid.WithField("owner_name", "nhập họ tên chủ hộ (lưu ở kho định danh)")
}

// CreateHousehold thêm hộ vào điểm trình diễn.
func (s *Service) CreateHousehold(ctx context.Context, in HouseholdInput) (HouseholdView, error) {
	ctx = txdeps.With(ctx) // P0-POOL-KN-2: port module khác chỉ đọc ngoài giao dịch (service.go tx)
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return HouseholdView{}, err
	}
	ref := ids.New()
	if in.Ref != nil {
		ref = *in.Ref
	}
	// Kiểm quyền, điểm trước khi gọi kho định danh (không tạo chủ thể cho yêu cầu bị từ chối).
	var x *store.Site
	var p *store.Project
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if x, e = store.LatestSite(ctx, tx, in.SiteRef); e != nil || x == nil {
			return e
		}
		if p, e = store.LatestProject(ctx, tx, x.ProjectRef); e != nil {
			return e
		}
		grants, e := s.myFollowerGrants(ctx, tx, c)
		if e != nil {
			return e
		}
		if p != nil && c.seesSite(*x) && !c.canEditHousehold(*p, *x, uuid.Nil, grants) {
			if p.Event == domain.EventClosed || x.Event == domain.EventClosed {
				return ErrSiteClosed
			}
			return problem.Forbidden.WithDetail("chỉ người quản lý dự án hoặc cán bộ theo dõi điểm thêm hộ mô hình")
		}
		return nil
	}); err != nil {
		return HouseholdView{}, err
	}
	if x == nil || p == nil || !c.seesSite(*x) {
		return HouseholdView{}, ErrSiteNotFound.WithField("site_ref", "không có điểm trình diễn")
	}
	if err := s.checkHousehold(ctx, c, *x, &in, nil); err != nil {
		return HouseholdView{}, err
	}
	person, err := s.person(ctx, in, x.CommuneCode)
	if err != nil {
		return HouseholdView{}, err
	}
	var out store.Household
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.HouseholdByIdem(ctx, tx, idemHousehold(in.IdemKey)); err != nil || prev != nil {
			if prev != nil {
				out, replayed = *prev, true
			}
			return err
		}
		cur, p, err := s.lockSite(ctx, tx, c, in.SiteRef)
		if err != nil {
			return err
		}
		if p.Event == domain.EventClosed || cur.Event == domain.EventClosed {
			return ErrSiteClosed
		}
		if old, err := store.LatestHousehold(ctx, tx, ref); err != nil {
			return err
		} else if old != nil {
			return ErrHouseholdInvalid.WithField("id", "mã hộ đã dùng")
		}
		hhs, err := store.ListHouseholds(ctx, tx, store.HouseholdFilter{Site: &cur.Ref})
		if err != nil {
			return err
		}
		for _, h := range hhs {
			if h.PersonRef == person && h.Event != domain.EventWithdrawn {
				return ErrHouseholdExists
			}
		}
		out = store.Household{Meta: c.meta(s.now(), idemHousehold(in.IdemKey)), Ref: ref, Event: domain.EventCreated, SiteRef: cur.Ref,
			ProjectRef: cur.ProjectRef, CommuneCode: cur.CommuneCode, PersonRef: person, Household: in.Household}
		if err := store.InsertHousehold(ctx, tx, c.p.TenantID, &out); err != nil {
			return err
		}
		return s.writePoint(ctx, tx, c, store.PointHousehold, ref, in.Point, false)
	})
	if dup(err) {
		return s.CreateHousehold(ctx, in)
	}
	if err != nil {
		return HouseholdView{}, err
	}
	v, err := s.GetHousehold(ctx, out.Ref)
	v.Replayed = replayed
	return v, err
}

// lockHousehold đọc hộ, khóa dự án, kiểm quyền sửa.
func (s *Service) lockHousehold(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID, base *int64) (*store.Household, *store.Site, *store.Project, error) {
	h, err := store.LatestHousehold(ctx, tx, ref)
	if err != nil {
		return nil, nil, nil, err
	}
	if h == nil || !c.seesHousehold(*h) {
		return nil, nil, nil, ErrHouseholdNF
	}
	x, p, err := s.lockSite(ctx, tx, c, h.SiteRef)
	if err != nil {
		return nil, nil, nil, err
	}
	if h, err = store.LatestHousehold(ctx, tx, ref); err != nil {
		return nil, nil, nil, err
	}
	grants, err := s.myFollowerGrants(ctx, tx, c)
	if err != nil {
		return nil, nil, nil, err
	}
	if !c.canEditHousehold(*p, *x, h.Ref, grants) {
		if p.Event == domain.EventClosed || x.Event == domain.EventClosed {
			return nil, nil, nil, ErrSiteClosed
		}
		return nil, nil, nil, problem.Forbidden.WithDetail("chỉ người quản lý dự án hoặc cán bộ theo dõi điểm sửa hộ mô hình")
	}
	if !versionOK(base, h.Seq) {
		return nil, nil, nil, ErrHouseholdConfl
	}
	return h, x, p, nil
}

// UpdateHousehold ghi phiên bản mới của hộ (khóa lạc quan). Chủ hộ không đổi (rút hộ, thêm hộ mới).
func (s *Service) UpdateHousehold(ctx context.Context, in HouseholdInput) (HouseholdView, error) {
	ctx = txdeps.With(ctx) // P0-POOL-KN-2: port module khác chỉ đọc ngoài giao dịch (service.go tx)
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return HouseholdView{}, err
	}
	if in.Ref == nil {
		return HouseholdView{}, ErrHouseholdNF
	}
	if in.BaseVersion == nil {
		return HouseholdView{}, errBaseVersion
	}
	if in.PersonRef != nil || in.Owner != nil {
		return HouseholdView{}, ErrHouseholdInvalid.WithField("owner_name", "không đổi chủ hộ — rút hộ và thêm hộ mới")
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.HouseholdByIdem(ctx, tx, idemHousehold(in.IdemKey)); err != nil || prev != nil {
			return err
		}
		h, x, _, err := s.lockHousehold(ctx, tx, c, *in.Ref, in.BaseVersion)
		if err != nil {
			return err
		}
		if h.Event == domain.EventWithdrawn {
			return ErrHouseholdOut
		}
		if err := s.checkHousehold(ctx, c, *x, &in, h); err != nil {
			return err
		}
		n := *h
		n.Meta, n.Event, n.StatusReason, n.Household = c.meta(s.now(), idemHousehold(in.IdemKey)), domain.EventUpdated, "", in.Household
		if err := store.InsertHousehold(ctx, tx, c.p.TenantID, &n); err != nil {
			return err
		}
		return s.writePoint(ctx, tx, c, store.PointHousehold, h.Ref, in.Point, in.RemovePoint)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return HouseholdView{}, err
	}
	return s.GetHousehold(ctx, *in.Ref)
}

// SetHouseholdStatus rút hộ khỏi mô hình (xóa tọa độ) hoặc đưa lại — lý do bắt buộc.
func (s *Service) SetHouseholdStatus(ctx context.Context, ref uuid.UUID, withdraw bool, base *int64, reason, idem string) (HouseholdView, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return HouseholdView{}, err
	}
	if base == nil {
		return HouseholdView{}, errBaseVersion
	}
	if !domain.ValidReason(reason) {
		return HouseholdView{}, problem.Validation.WithField("reason", "5–1000 ký tự")
	}
	if err := noPII(problem.Validation, map[string]string{"reason": reason}); err != nil {
		return HouseholdView{}, err
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.HouseholdByIdem(ctx, tx, idemHousehold(idem)); err != nil || prev != nil {
			return err
		}
		h, _, _, err := s.lockHousehold(ctx, tx, c, ref, base)
		if err != nil {
			return err
		}
		if (h.Event == domain.EventWithdrawn) == withdraw {
			return ErrHouseholdConfl.WithDetail("hộ đã ở trạng thái này")
		}
		if !withdraw {
			hhs, err := store.ListHouseholds(ctx, tx, store.HouseholdFilter{Site: &h.SiteRef})
			if err != nil {
				return err
			}
			for _, o := range hhs {
				if o.Ref != h.Ref && o.PersonRef == h.PersonRef && o.Event != domain.EventWithdrawn {
					return ErrHouseholdExists
				}
			}
		}
		ev := domain.EventReinstated
		if withdraw {
			ev = domain.EventWithdrawn
		}
		n := *h
		n.Meta, n.Event, n.StatusReason = c.meta(s.now(), idemHousehold(idem)), ev, strings.TrimSpace(reason)
		if err := store.InsertHousehold(ctx, tx, c.p.TenantID, &n); err != nil {
			return err
		}
		if withdraw {
			return store.ClearPoint(ctx, tx, c.p.TenantID, store.PointHousehold, h.Ref)
		}
		return nil
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return HouseholdView{}, err
	}
	return s.GetHousehold(ctx, ref)
}

// HouseholdQuery lọc danh sách hộ.
type HouseholdQuery struct {
	Site    *uuid.UUID
	Project *uuid.UUID
	Commune string
	Status  string
}

// ListHouseholds liệt kê hộ trong phạm vi.
func (s *Service) ListHouseholds(ctx context.Context, q HouseholdQuery) ([]HouseholdView, map[uuid.UUID]string, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, err
	}
	switch q.Status {
	case "", domain.StatusActive, domain.StatusWithdrawn:
	default:
		return nil, nil, problem.Validation.WithField("status", "active hoặc withdrawn")
	}
	f := store.HouseholdFilter{Site: q.Site, Project: q.Project}
	if x := strings.TrimSpace(q.Commune); x != "" {
		f.Communes = []string{x}
	}
	var hhs []store.Household
	var follow []store.Grant
	var points map[uuid.UUID]domain.Point
	sites, projects := map[uuid.UUID]store.Site{}, map[uuid.UUID]store.Project{}
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if hhs, e = store.ListHouseholds(ctx, tx, f); e != nil {
			return e
		}
		xs, e := store.ListSites(ctx, tx, store.SiteFilter{Project: q.Project})
		if e != nil {
			return e
		}
		for _, x := range xs {
			sites[x.Ref] = x
		}
		ps, e := store.ListProjects(ctx, tx)
		if e != nil {
			return e
		}
		for _, p := range ps {
			projects[p.Ref] = p
		}
		refs := make([]uuid.UUID, 0, len(hhs))
		for _, h := range hhs {
			refs = append(refs, h.Ref)
		}
		if points, e = store.Points(ctx, tx, store.PointHousehold, refs); e != nil {
			return e
		}
		follow, e = s.myFollowerGrants(ctx, tx, c)
		return e
	}); err != nil {
		return nil, nil, err
	}
	out := []HouseholdView{}
	refs := []uuid.UUID{}
	for _, h := range hhs {
		x, okS := sites[h.SiteRef]
		p, okP := projects[h.ProjectRef]
		if !okS || !okP || !c.seesHousehold(h) {
			continue
		}
		v := s.householdView(c, p, x, h, follow)
		if q.Status != "" && v.Status != q.Status {
			continue
		}
		if pt, ok := points[h.Ref]; ok {
			v.Point = &pt
		}
		out = append(out, v)
		if h.KeeperRef != nil {
			refs = append(refs, *h.KeeperRef)
		}
	}
	s.ownerLabels(ctx, out)
	return out, s.names(ctx, refs), nil
}

// GetHousehold đọc chi tiết hộ kèm lịch sử.
func (s *Service) GetHousehold(ctx context.Context, ref uuid.UUID) (HouseholdView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return HouseholdView{}, err
	}
	var vs []store.Household
	var x *store.Site
	var p *store.Project
	var follow []store.Grant
	var points map[uuid.UUID]domain.Point
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if vs, e = store.HouseholdVersions(ctx, tx, ref); e != nil || len(vs) == 0 {
			return e
		}
		cur := vs[len(vs)-1]
		if x, e = store.LatestSite(ctx, tx, cur.SiteRef); e != nil {
			return e
		}
		if p, e = store.LatestProject(ctx, tx, cur.ProjectRef); e != nil {
			return e
		}
		if points, e = store.Points(ctx, tx, store.PointHousehold, []uuid.UUID{ref}); e != nil {
			return e
		}
		follow, e = s.myFollowerGrants(ctx, tx, c)
		return e
	}); err != nil {
		return HouseholdView{}, err
	}
	if len(vs) == 0 || x == nil || p == nil || !c.seesHousehold(vs[len(vs)-1]) {
		return HouseholdView{}, ErrHouseholdNF
	}
	cur := vs[len(vs)-1]
	v := s.householdView(c, *p, *x, cur, follow)
	v.Versions = vs
	if pt, ok := points[ref]; ok {
		v.Point = &pt
	}
	one := []HouseholdView{v}
	s.ownerLabels(ctx, one)
	v = one[0]
	refs := []uuid.UUID{}
	if cur.KeeperRef != nil {
		refs = append(refs, *cur.KeeperRef)
	}
	for _, h := range vs {
		refs = append(refs, h.ActorRef)
	}
	v.Names = s.names(ctx, refs)
	return v, nil
}
