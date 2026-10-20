package app

// Điểm trình diễn/điểm nuôi (UR-TINH-19: xã, hộ, quy mô, tọa độ, cán bộ theo dõi, mẫu nhật ký). Người sửa được dự án
// lập, sửa, đóng điểm; Cấp Xã xem điểm của xã mình (FN-19 "Cấp Xã: Dữ liệu › Điểm trình diễn").

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SiteView là điểm (bản hiện hành) cho API.
type SiteView struct {
	store.Site
	ProjectCode      string
	ProjectName      string
	Status           string
	Point            *domain.Point
	HouseholdCount   int
	CanEdit          bool            // sửa/đóng điểm
	CanAddHouseholds bool            // thêm, sửa hộ của điểm
	Households       []HouseholdView // chỉ chi tiết
	Versions         []store.Site    // chỉ chi tiết
	Names            map[uuid.UUID]string
	ModelTypeName    string
	Replayed         bool
}

func siteStatus(event string) string {
	if event == domain.EventClosed {
		return domain.StatusClosed
	}
	return domain.StatusActive
}

// seesSite: Admin; Cấp Tỉnh/TW theo phạm vi; Cấp Xã theo xã phụ trách.
func (c caller) seesSite(x store.Site) bool { return c.sees(x.CommuneCode) }

// follows: Cấp Xã là cán bộ theo dõi của điểm, hoặc có quyền "cán bộ theo dõi mô hình" bao điểm/hộ.
func (c caller) follows(x store.Site, household uuid.UUID, grants []store.Grant) bool {
	if !c.commune() || !c.writable() || !c.sees(x.CommuneCode) {
		return false
	}
	if x.OfficerRef != nil && *x.OfficerRef == c.me() {
		return true
	}
	for _, g := range grants {
		if g.Covers(x.CommuneCode, x.Ref, household) {
			return true
		}
	}
	return false
}

func (s *Service) siteView(c caller, p store.Project, x store.Site, grants []store.Grant) SiteView {
	open := p.Event != domain.EventClosed && x.Event != domain.EventClosed
	edit := c.canEditProject(p)
	return SiteView{Site: x, ProjectCode: p.Code, ProjectName: p.Name, Status: siteStatus(x.Event), CanEdit: edit && p.Event != domain.EventClosed,
		CanAddHouseholds: open && (edit || c.follows(x, uuid.Nil, grants))}
}

// SiteInput là thân tạo/sửa điểm.
type SiteInput struct {
	Ref         *uuid.UUID // tạo: UUIDv7 tùy chọn; sửa: mã điểm
	ProjectRef  uuid.UUID  // tạo
	BaseVersion *int64     // sửa
	Point       *domain.Point
	RemovePoint bool
	domain.Site
	IdemKey string
}

func idemSite(k string) string { return "csite:" + k }

func (s *Service) checkSite(ctx context.Context, c caller, p store.Project, in *SiteInput, cur *store.Site) error {
	if s.Catalogs == nil {
		return ErrDependency
	}
	if es := in.Normalize(); len(es) > 0 {
		return fieldsProblem(ErrSiteInvalid, es)
	}
	if in.Point != nil {
		if es := in.Point.Validate("location"); len(es) > 0 {
			return fieldsProblem(ErrSiteInvalid, es)
		}
	}
	if err := noPII(ErrSiteInvalid, map[string]string{"name": in.Name, "note": in.Note}); err != nil {
		return err
	}
	if err := s.communeIn(ctx, c, ErrSiteInvalid, "commune_code", in.CommuneCode, p.Province); err != nil {
		return err
	}
	it, err := s.Catalogs.ItemAt(ctx, knport.CatalogCNTSModelType, in.ModelType, s.today())
	switch {
	case errors.Is(err, mdport.ErrCatalogItemNotFound), errors.Is(err, mdport.ErrNotEffective):
		return ErrSiteInvalid.WithField("model_type_code", "không có trong danh mục loại mô hình CNTS")
	case err != nil:
		return err
	case !it.Active && (cur == nil || cur.ModelType != in.ModelType):
		return ErrSiteInvalid.WithField("model_type_code", "loại mô hình đã ngừng dùng")
	}
	if in.OfficerRef != nil && (cur == nil || cur.OfficerRef == nil || *cur.OfficerRef != *in.OfficerRef || cur.CommuneCode != in.CommuneCode) {
		if err := s.checkOfficer(ctx, c, ErrSiteInvalid, "officer_ref", *in.OfficerRef, in.CommuneCode); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) checkCoop(ctx context.Context, tx pgx.Tx, in *SiteInput, cur *store.Site) error {
	if in.CooperativeRef == nil || (cur != nil && cur.CooperativeRef != nil && *cur.CooperativeRef == *in.CooperativeRef) {
		return nil
	}
	ok, err := store.CooperativeExists(ctx, tx, *in.CooperativeRef)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSiteInvalid.WithField("cooperative_ref", "không có HTX này trong dữ liệu công bố của HTX")
	}
	return nil
}

func (s *Service) writePoint(ctx context.Context, tx pgx.Tx, c caller, kind string, ref uuid.UUID, p *domain.Point, remove bool) error {
	switch {
	case p != nil:
		return store.SetPoint(ctx, tx, c.p.TenantID, kind, ref, *p, c.me())
	case remove:
		return store.ClearPoint(ctx, tx, c.p.TenantID, kind, ref)
	}
	return nil
}

// CreateSite tạo điểm trình diễn trong dự án.
func (s *Service) CreateSite(ctx context.Context, in SiteInput) (SiteView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return SiteView{}, err
	}
	ref := ids.New()
	if in.Ref != nil {
		ref = *in.Ref
	}
	var out store.Site
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.SiteByIdem(ctx, tx, idemSite(in.IdemKey)); err != nil || prev != nil {
			if prev != nil {
				out, replayed = *prev, true
			}
			return err
		}
		if err := store.Lock(ctx, tx, "proj:"+in.ProjectRef.String()); err != nil {
			return err
		}
		p, err := store.LatestProject(ctx, tx, in.ProjectRef)
		if err != nil {
			return err
		}
		if p == nil || !c.seesProject(*p, nil) {
			return ErrProjectNotFound.WithField("project_ref", "không có dự án")
		}
		if !c.canEditProject(*p) {
			return problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin thêm điểm")
		}
		if p.Event == domain.EventClosed {
			return ErrProjectClosed
		}
		if err := s.checkSite(ctx, c, *p, &in, nil); err != nil {
			return err
		}
		if err := s.checkCoop(ctx, tx, &in, nil); err != nil {
			return err
		}
		if cur, err := store.LatestSite(ctx, tx, ref); err != nil {
			return err
		} else if cur != nil {
			return ErrSiteInvalid.WithField("id", "mã điểm đã dùng")
		}
		out = store.Site{Meta: c.meta(s.now(), idemSite(in.IdemKey)), Ref: ref, Event: domain.EventCreated, ProjectRef: p.Ref, Site: in.Site}
		if err := store.InsertSite(ctx, tx, c.p.TenantID, &out); err != nil {
			return err
		}
		return s.writePoint(ctx, tx, c, store.PointSite, ref, in.Point, false)
	})
	if dup(err) {
		return s.CreateSite(ctx, in)
	}
	if err != nil {
		return SiteView{}, err
	}
	v, err := s.GetSite(ctx, out.Ref)
	v.Replayed = replayed
	return v, err
}

// UpdateSite ghi phiên bản mới của điểm (khóa lạc quan).
func (s *Service) UpdateSite(ctx context.Context, in SiteInput) (SiteView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return SiteView{}, err
	}
	if in.Ref == nil {
		return SiteView{}, ErrSiteNotFound
	}
	if in.BaseVersion == nil {
		return SiteView{}, errBaseVersion
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.SiteByIdem(ctx, tx, idemSite(in.IdemKey)); err != nil || prev != nil {
			return err
		}
		cur, p, err := s.lockSite(ctx, tx, c, *in.Ref)
		if err != nil {
			return err
		}
		if !c.canEditProject(*p) {
			return problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin sửa điểm")
		}
		if p.Event == domain.EventClosed {
			return ErrProjectClosed
		}
		if cur.Event == domain.EventClosed {
			return ErrSiteClosed
		}
		if !versionOK(in.BaseVersion, cur.Seq) {
			return ErrSiteConflict
		}
		if err := s.checkSite(ctx, c, *p, &in, cur); err != nil {
			return err
		}
		if err := s.checkCoop(ctx, tx, &in, cur); err != nil {
			return err
		}
		if in.CommuneCode != cur.CommuneCode {
			hhs, err := store.ListHouseholds(ctx, tx, store.HouseholdFilter{Site: &cur.Ref})
			if err != nil {
				return err
			}
			for _, h := range hhs {
				if h.Event != domain.EventWithdrawn {
					return ErrSiteInvalid.WithField("commune_code", "điểm đã có hộ — rút hộ trước khi đổi xã")
				}
			}
		}
		n := store.Site{Meta: c.meta(s.now(), idemSite(in.IdemKey)), Ref: cur.Ref, Event: domain.EventUpdated, ProjectRef: cur.ProjectRef, Site: in.Site}
		if err := store.InsertSite(ctx, tx, c.p.TenantID, &n); err != nil {
			return err
		}
		return s.writePoint(ctx, tx, c, store.PointSite, cur.Ref, in.Point, in.RemovePoint)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return SiteView{}, err
	}
	return s.GetSite(ctx, *in.Ref)
}

// lockSite khóa và đọc điểm cùng dự án (kiểm phạm vi đọc).
func (s *Service) lockSite(ctx context.Context, tx pgx.Tx, c caller, ref uuid.UUID) (*store.Site, *store.Project, error) {
	cur, err := store.LatestSite(ctx, tx, ref)
	if err != nil {
		return nil, nil, err
	}
	if cur == nil || !c.seesSite(*cur) {
		return nil, nil, ErrSiteNotFound
	}
	if err := store.Lock(ctx, tx, "proj:"+cur.ProjectRef.String()); err != nil {
		return nil, nil, err
	}
	if cur, err = store.LatestSite(ctx, tx, ref); err != nil {
		return nil, nil, err
	}
	p, err := store.LatestProject(ctx, tx, cur.ProjectRef)
	if err != nil {
		return nil, nil, err
	}
	if p == nil {
		return nil, nil, ErrSiteNotFound
	}
	return cur, p, nil
}

// SetSiteStatus đóng/mở lại điểm — lý do bắt buộc.
func (s *Service) SetSiteStatus(ctx context.Context, ref uuid.UUID, closeIt bool, base *int64, reason, idem string) (SiteView, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return SiteView{}, err
	}
	if base == nil {
		return SiteView{}, errBaseVersion
	}
	if !domain.ValidReason(reason) {
		return SiteView{}, problem.Validation.WithField("reason", "5–1000 ký tự")
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.SiteByIdem(ctx, tx, idemSite(idem)); err != nil || prev != nil {
			return err
		}
		cur, p, err := s.lockSite(ctx, tx, c, ref)
		if err != nil {
			return err
		}
		if !c.canEditProject(*p) {
			return problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin")
		}
		if p.Event == domain.EventClosed {
			return ErrProjectClosed
		}
		if !versionOK(base, cur.Seq) {
			return ErrSiteConflict
		}
		if (cur.Event == domain.EventClosed) == closeIt {
			return ErrSiteConflict.WithDetail("điểm đã ở trạng thái này")
		}
		ev := domain.EventReopened
		if closeIt {
			ev = domain.EventClosed
		}
		n := *cur
		n.Meta, n.Event, n.StatusReason = c.meta(s.now(), idemSite(idem)), ev, strings.TrimSpace(reason)
		return store.InsertSite(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return SiteView{}, err
	}
	return s.GetSite(ctx, ref)
}

// SiteQuery lọc danh sách điểm.
type SiteQuery struct {
	Project *uuid.UUID
	Commune string
	Status  string
}

// ListSites liệt kê điểm trong phạm vi (Cấp Xã: điểm của xã mình).
func (s *Service) ListSites(ctx context.Context, q SiteQuery) ([]SiteView, map[uuid.UUID]string, map[string]string, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	switch q.Status {
	case "", domain.StatusActive, domain.StatusClosed:
	default:
		return nil, nil, nil, problem.Validation.WithField("status", "active hoặc closed")
	}
	f := store.SiteFilter{Project: q.Project}
	if c := strings.TrimSpace(q.Commune); c != "" {
		f.Communes = []string{c}
	}
	var sites []store.Site
	var hhs []store.Household
	var points map[uuid.UUID]domain.Point
	var follow []store.Grant
	projects := map[uuid.UUID]store.Project{}
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if sites, e = store.ListSites(ctx, tx, f); e != nil {
			return e
		}
		ps, e := store.ListProjects(ctx, tx)
		if e != nil {
			return e
		}
		for _, p := range ps {
			projects[p.Ref] = p
		}
		if hhs, e = store.ListHouseholds(ctx, tx, store.HouseholdFilter{Project: q.Project}); e != nil {
			return e
		}
		refs := make([]uuid.UUID, 0, len(sites))
		for _, x := range sites {
			refs = append(refs, x.Ref)
		}
		if points, e = store.Points(ctx, tx, store.PointSite, refs); e != nil {
			return e
		}
		follow, e = s.myFollowerGrants(ctx, tx, c)
		return e
	}); err != nil {
		return nil, nil, nil, err
	}
	count := map[uuid.UUID]int{}
	for _, h := range hhs {
		if h.Event != domain.EventWithdrawn {
			count[h.SiteRef]++
		}
	}
	out := []SiteView{}
	refs := []uuid.UUID{}
	for _, x := range sites {
		p, ok := projects[x.ProjectRef]
		if !ok || !c.seesSite(x) {
			continue
		}
		v := s.siteView(c, p, x, follow)
		if q.Status != "" && v.Status != q.Status {
			continue
		}
		if pt, ok := points[x.Ref]; ok {
			v.Point = &pt
		}
		v.HouseholdCount = count[x.Ref]
		out = append(out, v)
		if x.OfficerRef != nil {
			refs = append(refs, *x.OfficerRef)
		}
	}
	return out, s.names(ctx, refs), s.catalogNames(ctx, knport.CatalogCNTSModelType), nil
}

// GetSite đọc chi tiết điểm kèm hộ và lịch sử.
func (s *Service) GetSite(ctx context.Context, ref uuid.UUID) (SiteView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return SiteView{}, err
	}
	var vs []store.Site
	var p *store.Project
	var hhs []store.Household
	var follow []store.Grant
	var sitePts, hhPts map[uuid.UUID]domain.Point
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if vs, e = store.SiteVersions(ctx, tx, ref); e != nil || len(vs) == 0 {
			return e
		}
		if p, e = store.LatestProject(ctx, tx, vs[0].ProjectRef); e != nil {
			return e
		}
		if hhs, e = store.ListHouseholds(ctx, tx, store.HouseholdFilter{Site: &ref}); e != nil {
			return e
		}
		if sitePts, e = store.Points(ctx, tx, store.PointSite, []uuid.UUID{ref}); e != nil {
			return e
		}
		hrefs := make([]uuid.UUID, 0, len(hhs))
		for _, h := range hhs {
			hrefs = append(hrefs, h.Ref)
		}
		if hhPts, e = store.Points(ctx, tx, store.PointHousehold, hrefs); e != nil {
			return e
		}
		follow, e = s.myFollowerGrants(ctx, tx, c)
		return e
	}); err != nil {
		return SiteView{}, err
	}
	if len(vs) == 0 || p == nil || !c.seesSite(vs[len(vs)-1]) {
		return SiteView{}, ErrSiteNotFound
	}
	cur := vs[len(vs)-1]
	v := s.siteView(c, *p, cur, follow)
	v.Versions = vs
	if pt, ok := sitePts[ref]; ok {
		v.Point = &pt
	}
	refs := []uuid.UUID{}
	if cur.OfficerRef != nil {
		refs = append(refs, *cur.OfficerRef)
	}
	for _, h := range hhs {
		if !c.seesHousehold(h) {
			continue
		}
		hv := s.householdView(c, *p, cur, h, follow)
		if pt, ok := hhPts[h.Ref]; ok {
			hv.Point = &pt
		}
		v.Households = append(v.Households, hv)
		if h.Event != domain.EventWithdrawn {
			v.HouseholdCount++
		}
		if h.KeeperRef != nil {
			refs = append(refs, *h.KeeperRef)
		}
	}
	s.ownerLabels(ctx, v.Households)
	for _, x := range vs {
		refs = append(refs, x.ActorRef)
	}
	v.Names = s.names(ctx, refs)
	v.ModelTypeName = s.catalogNames(ctx, knport.CatalogCNTSModelType)[cur.ModelType]
	return v, nil
}
