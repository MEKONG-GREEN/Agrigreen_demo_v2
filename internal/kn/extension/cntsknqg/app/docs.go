package app

// Lập, sửa, chuyển trạng thái, ký (scan), danh sách, chi tiết biên bản/báo cáo dự án KNQG (FN-19 B1–B5, UR-TINH-20…24,
// UR-QG-01). Mỗi thao tác ghi gồm: (1) giao dịch đọc ngắn (biểu, dự án, điểm, hộ, nhật ký); (2) tra tên cán bộ, tên xã,
// nhãn chủ hộ NGOÀI giao dịch; (3) giao dịch ghi khóa theo dự án, đọc lại và kiểm lại trạng thái/phiên bản rồi ghi một
// dòng sổ (#156: không I/O ngoài trong giao dịch; #112: phát lại theo Idempotency-Key).

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/store"
	mdomain "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	mstore "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// DocView là biểu (bản hiện hành) cho API.
type DocView struct {
	store.Doc
	ProjectCode string
	ProjectName string
	ProjectKind string
	SiteName    string
	CommuneCode string
	CanEdit     bool
	Actions     []string
	Warnings    []string          // không chặn (quy tắc 11)
	Issues      []domain.FieldErr // còn thiếu trước khi trình/ký/gửi
	Blockers    []string          // Mẫu 07: điểm chưa có Mẫu 05 đã ký (quy tắc 10)
	Versions    []store.Doc       // chỉ chi tiết (không nội dung)
	Names       map[uuid.UUID]string
	Replayed    bool
}

// pctx là dự án kèm điểm, hộ và các biểu hiện hành của dự án.
type pctx struct {
	p     mstore.Project
	sites []mstore.Site
	hhs   []mstore.Household
	docs  []store.Doc // bản hiện hành, có nội dung
}

func (pc *pctx) site(ref *uuid.UUID) *mstore.Site {
	if ref == nil {
		return nil
	}
	for i := range pc.sites {
		if pc.sites[i].Ref == *ref {
			return &pc.sites[i]
		}
	}
	return nil
}

func (pc *pctx) communeOf(d store.Doc) string {
	if x := pc.site(d.SiteRef); x != nil {
		return x.CommuneCode
	}
	return ""
}

// loadProject đọc dự án, điểm, hộ, biểu của dự án (nil = không có dự án).
func loadProject(ctx context.Context, tx pgx.Tx, ref uuid.UUID) (*pctx, error) {
	p, err := mstore.LatestProject(ctx, tx, ref)
	if err != nil || p == nil {
		return nil, err
	}
	pc := &pctx{p: *p}
	if pc.sites, err = mstore.ListSites(ctx, tx, mstore.SiteFilter{Project: &ref}); err != nil {
		return nil, err
	}
	if pc.hhs, err = mstore.ListHouseholds(ctx, tx, mstore.HouseholdFilter{Project: &ref}); err != nil {
		return nil, err
	}
	pc.docs, err = store.List(ctx, tx, store.Filter{Project: &ref, Content: true})
	return pc, err
}

// diaryOf đọc thực tế nhật ký của điểm (Mẫu 05).
func diaryOf(ctx context.Context, tx pgx.Tx, form string, site *uuid.UUID) (*domain.DiaryFacts, error) {
	if form != domain.Form05 || site == nil {
		return nil, nil
	}
	d, err := store.Diary(ctx, tx, *site)
	return &d, err
}

// ext là tên tra ngoài giao dịch.
type ext struct {
	manager  string
	communes map[string]string
	labels   map[uuid.UUID]string // person_ref → nhãn chủ hộ
}

func (s *Service) ext(ctx context.Context, pc *pctx, form string) ext {
	e := ext{communes: map[string]string{}, labels: map[uuid.UUID]string{}}
	e.manager = s.names(ctx, []uuid.UUID{pc.p.ManagerRef})[pc.p.ManagerRef]
	for _, x := range pc.sites {
		if _, ok := e.communes[x.CommuneCode]; !ok {
			e.communes[x.CommuneCode] = s.unitName(ctx, x.CommuneCode)
		}
	}
	if form == domain.Form05 && s.Vault != nil {
		if v := s.Vault(); v != nil {
			for i, h := range pc.hhs {
				if i >= 200 {
					break
				}
				if d, err := v.Display(ctx, client.PurposeFieldSupport, h.PersonRef); err == nil {
					e.labels[h.PersonRef] = d.Label
				}
			}
		}
	}
	return e
}

func scaleText(v *decimal.Decimal, unit string) string {
	if v == nil {
		return ""
	}
	return v.String() + " " + unit
}

func activeHouseholds(hhs []mstore.Household, site uuid.UUID) int {
	n := 0
	for _, h := range hhs {
		if h.SiteRef == site && h.Event != mdomain.EventWithdrawn {
			n++
		}
	}
	return n
}

// facts dựng số liệu điền sẵn/tự động.
func facts(pc *pctx, siteRef *uuid.UUID, diary *domain.DiaryFacts, e ext) domain.Facts {
	p := pc.p
	f := domain.Facts{ProjectName: p.Name, ProjectCode: p.Code, Manager: e.manager, Basis: p.Basis, Diary: diary}
	f.Duration = p.Start.Format("02/01/2006")
	if p.End != nil {
		f.Duration += " – " + p.End.Format("02/01/2006")
	}
	if p.Budget != nil {
		f.Budget = p.Budget.String()
	}
	sf := func(x mstore.Site) domain.SiteFacts {
		return domain.SiteFacts{Name: x.Name, Commune: e.communes[x.CommuneCode], Scale: scaleText(x.Scale.Value, x.Scale.Unit),
			Households: activeHouseholds(pc.hhs, x.Ref)}
	}
	for _, x := range pc.sites {
		if x.Event != mdomain.EventClosed {
			f.Sites = append(f.Sites, sf(x))
		}
	}
	if x := pc.site(siteRef); x != nil {
		v := sf(*x)
		f.Site = &v
		for _, h := range pc.hhs {
			if h.SiteRef != x.Ref || h.Event == mdomain.EventWithdrawn {
				continue
			}
			r := domain.Row{"chu_ho": e.labels[h.PersonRef], "quy_mo": scaleText(h.Scale.Value, h.Scale.Unit)}
			for k, v := range r {
				if v == "" {
					delete(r, k)
				}
			}
			f.Households = append(f.Households, r)
		}
	}
	for _, x := range pc.sites {
		for _, d := range pc.docs {
			if d.Form == domain.Form05 && d.SiteRef != nil && *d.SiteRef == x.Ref && d.Status == domain.StatusSigned {
				f.Signed05 = append(f.Signed05, domain.Summary05(d.Content))
			}
		}
	}
	return f
}

// blockers trả các điểm (chưa đóng) chưa có Mẫu 05 Đã ký — quy tắc liên biểu 10, FN-19 AC-03.
func blockers(pc *pctx, communes map[string]string) []string {
	var out []string
	signed := 0
	for _, x := range pc.sites {
		if x.Event == mdomain.EventClosed {
			continue
		}
		var d *store.Doc
		for i := range pc.docs {
			if pc.docs[i].Form == domain.Form05 && pc.docs[i].SiteRef != nil && *pc.docs[i].SiteRef == x.Ref {
				d = &pc.docs[i]
			}
		}
		name := x.Name
		if n := communes[x.CommuneCode]; n != "" {
			name += " (" + n + ")"
		}
		switch {
		case d == nil:
			out = append(out, name+": chưa lập Mẫu 05")
		case d.Status != domain.StatusSigned:
			out = append(out, name+": Mẫu 05 chưa ký (bản "+strconv.Itoa(d.Revision)+")")
		default:
			signed++
		}
	}
	if signed == 0 && len(out) == 0 {
		out = append(out, "dự án chưa có điểm trình diễn nào có Mẫu 05 đã ký")
	}
	return out
}

// unique kiểm "một bản hiệu lực" (Mẫu 01: dự án × tháng; Mẫu 07: dự án × năm; Mẫu 05: điểm).
func unique(pc *pctx, spec domain.Spec, d store.Doc) bool {
	if !spec.Unique {
		return true
	}
	for _, x := range pc.docs {
		if x.Ref == d.Ref || x.Form != d.Form {
			continue
		}
		if spec.PerSite && x.SiteRef != nil && d.SiteRef != nil && *x.SiteRef == *d.SiteRef {
			return false
		}
		if !spec.PerSite && x.Period == d.Period {
			return false
		}
	}
	return true
}

func idemDoc(k string) string { return "knqg:" + k }

// view dựng DocView (không lịch sử).
func (s *Service) view(c caller, pc *pctx, d store.Doc, communes map[string]string) DocView {
	spec, _ := domain.Lookup(d.Form)
	v := DocView{Doc: d, ProjectCode: pc.p.Code, ProjectName: pc.p.Name, ProjectKind: pc.p.Kind, CommuneCode: pc.communeOf(d)}
	if x := pc.site(d.SiteRef); x != nil {
		v.SiteName = x.Name
	}
	open := pc.p.Event != mdomain.EventClosed
	for _, a := range domain.Actions(spec.Lifecycle, d.Status) {
		if !c.may(pc.p, d.Form, a) {
			continue
		}
		if c.national() && !(d.Form == domain.Form07 && (a == domain.ActConfirm || a == domain.ActReturn)) {
			continue
		}
		if !open && a != domain.ActConfirm && a != domain.ActReturn {
			continue
		}
		v.Actions = append(v.Actions, a)
	}
	v.CanEdit = open && domain.Editable(d.Status) && c.editor(pc.p)
	if len(d.Content.Fields) > 0 || len(d.Content.Tables) > 0 {
		v.Warnings = spec.Warnings(d.Content)
		if domain.Editable(d.Status) || d.Status == domain.StatusAwaitingSign {
			v.Issues = spec.Complete(d.Content)
		}
	}
	if d.Form == domain.Form07 && domain.Editable(d.Status) {
		v.Blockers = blockers(pc, communes)
	}
	return v
}

// Specs trả đặc tả 5 mẫu (màn soạn biểu dựng theo đặc tả).
func (s *Service) Specs(ctx context.Context) ([]domain.Spec, error) {
	if _, err := s.reader(ctx); err != nil {
		return nil, err
	}
	return domain.Specs(), nil
}

// ListQuery lọc danh sách.
type ListQuery struct {
	Project *uuid.UUID
	Form    string
	Status  string
}

// List liệt kê biểu trong phạm vi người gọi (lọc theo vai/xã trước khi trả — lỗi #108).
func (s *Service) List(ctx context.Context, q ListQuery) ([]DocView, map[uuid.UUID]string, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, err
	}
	if q.Form != "" {
		if _, ok := domain.Lookup(q.Form); !ok {
			return nil, nil, problem.Validation.WithField("form", "01, 03, 04, 05 hoặc 07")
		}
	}
	var docs []store.Doc
	var ps []mstore.Project
	var sites []mstore.Site
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if docs, e = store.List(ctx, tx, store.Filter{Project: q.Project, Form: q.Form}); e != nil {
			return e
		}
		if ps, e = mstore.ListProjects(ctx, tx); e != nil {
			return e
		}
		sites, e = mstore.ListSites(ctx, tx, mstore.SiteFilter{Project: q.Project})
		return e
	}); err != nil {
		return nil, nil, err
	}
	pcs := map[uuid.UUID]*pctx{}
	for _, p := range ps {
		pcs[p.Ref] = &pctx{p: p}
	}
	for _, x := range sites {
		if pc := pcs[x.ProjectRef]; pc != nil {
			pc.sites = append(pc.sites, x)
		}
	}
	out := []DocView{}
	refs := []uuid.UUID{}
	for _, d := range docs {
		pc := pcs[d.ProjectRef]
		if pc == nil || !c.seesDoc(pc.p, d, pc.communeOf(d)) || (q.Status != "" && d.Status != q.Status) {
			continue
		}
		v := s.view(c, pc, d, nil)
		v.Blockers = nil // danh sách không tính điều kiện gửi (cần nội dung Mẫu 05)
		out = append(out, v)
		refs = append(refs, d.ActorRef)
	}
	return out, s.names(ctx, refs), nil
}

// Get đọc chi tiết biểu kèm lịch sử phiên bản.
func (s *Service) Get(ctx context.Context, ref uuid.UUID) (DocView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return DocView{}, err
	}
	var vs []store.Doc
	var pc *pctx
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if vs, e = store.Versions(ctx, tx, ref); e != nil || len(vs) == 0 {
			return e
		}
		pc, e = loadProject(ctx, tx, vs[0].ProjectRef)
		return e
	}); err != nil {
		return DocView{}, err
	}
	if len(vs) == 0 || pc == nil {
		return DocView{}, ErrDocNotFound
	}
	cur := vs[len(vs)-1]
	if !c.seesDoc(pc.p, cur, pc.communeOf(cur)) {
		return DocView{}, ErrDocNotFound
	}
	communes := map[string]string{}
	for _, x := range pc.sites {
		communes[x.CommuneCode] = s.unitName(ctx, x.CommuneCode)
	}
	v := s.view(c, pc, cur, communes)
	refs := []uuid.UUID{pc.p.ManagerRef}
	for i := len(vs) - 1; i >= 0; i-- {
		x := vs[i]
		x.Content = domain.Content{}
		v.Versions = append(v.Versions, x)
		refs = append(refs, x.ActorRef)
	}
	v.Names = s.names(ctx, refs)
	return v, nil
}

// CreateInput là thân lập biểu.
type CreateInput struct {
	Ref     *uuid.UUID // UUIDv7 do máy khách sinh (tùy chọn)
	Form    string
	Project uuid.UUID
	Site    *uuid.UUID
	Period  string
	IdemKey string
}

// Create lập biểu từ chi tiết dự án; số liệu điền sẵn (FN-19 B2).
func (s *Service) Create(ctx context.Context, in CreateInput) (DocView, error) {
	c, err := s.writer(ctx, in.IdemKey, false)
	if err != nil {
		return DocView{}, err
	}
	spec, ok := domain.Lookup(in.Form)
	if !ok {
		return DocView{}, ErrDocInvalid.WithField("form", "01, 03, 04, 05 hoặc 07")
	}
	period, bad := spec.NormalizePeriod(in.Period)
	if bad != "" {
		return DocView{}, ErrDocInvalid.WithField("period", bad)
	}
	if spec.PerSite != (in.Site != nil) {
		if spec.PerSite {
			return DocView{}, ErrDocInvalid.WithField("site_ref", "Mẫu 05 lập cho một điểm trình diễn")
		}
		return DocView{}, ErrDocInvalid.WithField("site_ref", "chỉ Mẫu 05 gắn điểm trình diễn")
	}
	idem := idemDoc(in.IdemKey)
	var prev *store.Doc
	var pc *pctx
	var diary *domain.DiaryFacts
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if prev, e = store.ByIdem(ctx, tx, idem); e != nil || prev != nil {
			return e
		}
		if pc, e = loadProject(ctx, tx, in.Project); e != nil || pc == nil {
			return e
		}
		diary, e = diaryOf(ctx, tx, in.Form, in.Site)
		return e
	}); err != nil {
		return DocView{}, err
	}
	if prev != nil {
		return s.replay(ctx, prev.Ref)
	}
	if err := s.checkProject(c, pc, in.Site); err != nil {
		return DocView{}, err
	}
	e := s.ext(ctx, pc, in.Form) // ngoài giao dịch
	ref := ids.New()
	if in.Ref != nil && *in.Ref != uuid.Nil {
		ref = *in.Ref
	}
	d := store.Doc{Meta: c.meta(s.now(), idem), Ref: ref, Form: in.Form, ProjectRef: in.Project, SiteRef: in.Site, Period: period,
		Revision: 1, Event: domain.EventCreated, Status: domain.StatusDraft, Content: spec.Prefill(facts(pc, in.Site, diary, e))}
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, "proj:"+in.Project.String()); err != nil {
			return err
		}
		if p, err := store.ByIdem(ctx, tx, idem); err != nil || p != nil {
			if p != nil {
				d, replayed = *p, true
			}
			return err
		}
		fresh, err := loadProject(ctx, tx, in.Project)
		if err != nil {
			return err
		}
		if err := s.checkProject(c, fresh, in.Site); err != nil {
			return err
		}
		if cur, err := store.Latest(ctx, tx, ref); err != nil {
			return err
		} else if cur != nil {
			return ErrDocInvalid.WithField("id", "mã biểu đã dùng")
		}
		if !unique(fresh, spec, d) {
			return ErrDocExists
		}
		return store.Insert(ctx, tx, c.p.TenantID, &d)
	})
	if dup(err) {
		return s.replayIdem(ctx, c, idem)
	}
	if err != nil {
		return DocView{}, err
	}
	v, err := s.Get(ctx, d.Ref)
	v.Replayed = replayed
	return v, err
}

// checkProject: dự án thấy được, loại KNQG, người gọi sửa được, chưa kết thúc; Mẫu 05 — điểm thuộc dự án, chưa đóng.
func (s *Service) checkProject(c caller, pc *pctx, site *uuid.UUID) error {
	if pc == nil || !c.seesProject(pc.p) {
		return ErrDocNotFound.WithDetail("không có dự án trong phạm vi")
	}
	if pc.p.Kind != mdomain.KindNational {
		return ErrProjectKind
	}
	if !c.editor(pc.p) {
		return problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin lập, sửa mẫu biểu dự án (FN-19 BR-01)")
	}
	if pc.p.Event == mdomain.EventClosed {
		return ErrProjectClose
	}
	if site != nil {
		x := pc.site(site)
		if x == nil {
			return ErrDocInvalid.WithField("site_ref", "không phải điểm trình diễn của dự án")
		}
		if x.Event == mdomain.EventClosed {
			return ErrDocInvalid.WithField("site_ref", "điểm trình diễn đã đóng")
		}
	}
	return nil
}

func (s *Service) replay(ctx context.Context, ref uuid.UUID) (DocView, error) {
	v, err := s.Get(ctx, ref)
	v.Replayed = true
	return v, err
}

func (s *Service) replayIdem(ctx context.Context, c caller, idem string) (DocView, error) {
	var prev *store.Doc
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		prev, e = store.ByIdem(ctx, tx, idem)
		return e
	}); err != nil {
		return DocView{}, err
	}
	if prev == nil {
		return DocView{}, ErrDocConflict
	}
	return s.replay(ctx, prev.Ref)
}

// loadDoc đọc biểu hiện hành, dự án và thực tế nhật ký (giao dịch đọc).
func (s *Service) loadDoc(ctx context.Context, c caller, ref uuid.UUID, idem string) (*store.Doc, *store.Doc, *pctx, *domain.DiaryFacts, error) {
	var prev, cur *store.Doc
	var pc *pctx
	var diary *domain.DiaryFacts
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if prev, e = store.ByIdem(ctx, tx, idem); e != nil || prev != nil {
			return e
		}
		if cur, e = store.Latest(ctx, tx, ref); e != nil || cur == nil {
			return e
		}
		if pc, e = loadProject(ctx, tx, cur.ProjectRef); e != nil || pc == nil {
			return e
		}
		diary, e = diaryOf(ctx, tx, cur.Form, cur.SiteRef)
		return e
	})
	if err != nil || prev != nil {
		return prev, nil, nil, nil, err
	}
	if cur == nil || pc == nil || !c.seesDoc(pc.p, *cur, pc.communeOf(*cur)) {
		return nil, nil, nil, nil, ErrDocNotFound
	}
	return nil, cur, pc, diary, nil
}

// UpdateInput là thân sửa nội dung.
type UpdateInput struct {
	Ref         uuid.UUID
	BaseVersion *int64
	Content     domain.Content
	IdemKey     string
}

// Update ghi phiên bản mới của nội dung (Đang lập / Trả lại).
func (s *Service) Update(ctx context.Context, in UpdateInput) (DocView, error) {
	c, err := s.writer(ctx, in.IdemKey, false)
	if err != nil {
		return DocView{}, err
	}
	if in.BaseVersion == nil {
		return DocView{}, errBaseVersion
	}
	idem := idemDoc(in.IdemKey)
	prev, cur, pc, _, err := s.loadDoc(ctx, c, in.Ref, idem)
	if err != nil {
		return DocView{}, err
	}
	if prev != nil {
		return s.replay(ctx, prev.Ref)
	}
	spec, _ := domain.Lookup(cur.Form)
	if !c.editor(pc.p) {
		return DocView{}, problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin sửa biểu (FN-19 BR-01)")
	}
	if pc.p.Event == mdomain.EventClosed {
		return DocView{}, ErrProjectClose
	}
	if !domain.Editable(cur.Status) {
		return DocView{}, ErrDocLocked
	}
	if !versionOK(in.BaseVersion, cur.Seq) {
		return DocView{}, ErrDocConflict
	}
	content := in.Content
	if es := spec.Normalize(&content); len(es) > 0 {
		return DocView{}, fieldsProblem(ErrDocInvalid, es)
	}
	if err := noPII(content.Texts()); err != nil {
		return DocView{}, err
	}
	e := s.ext(ctx, pc, cur.Form)
	return s.write(ctx, c, *cur, in.BaseVersion, idem, func(fresh *pctx, diary *domain.DiaryFacts, d *store.Doc) error {
		spec.Fill(&content, facts(fresh, d.SiteRef, diary, e))
		d.Event, d.Content = domain.EventUpdated, content
		return nil
	})
}

// write là pha ghi chung: khóa dự án, phát lại, đọc lại biểu + dự án + nhật ký, kiểm phiên bản, cho mutate sửa bản sao của
// bản hiện hành rồi ghi một dòng sổ.
func (s *Service) write(ctx context.Context, c caller, cur store.Doc, base *int64, idem string,
	mutate func(fresh *pctx, diary *domain.DiaryFacts, d *store.Doc) error) (DocView, error) {
	var d store.Doc
	replayed := false
	err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, "proj:"+cur.ProjectRef.String()); err != nil {
			return err
		}
		if p, err := store.ByIdem(ctx, tx, idem); err != nil || p != nil {
			if p != nil {
				d, replayed = *p, true
			}
			return err
		}
		latest, err := store.Latest(ctx, tx, cur.Ref)
		if err != nil {
			return err
		}
		if latest == nil || latest.Seq != cur.Seq || !versionOK(base, latest.Seq) {
			return ErrDocConflict
		}
		fresh, err := loadProject(ctx, tx, cur.ProjectRef)
		if err != nil {
			return err
		}
		if fresh == nil {
			return ErrDocNotFound
		}
		diary, err := diaryOf(ctx, tx, cur.Form, cur.SiteRef)
		if err != nil {
			return err
		}
		d = *latest
		d.Meta = c.meta(s.now(), idem)
		d.StatusReason = ""
		if err := mutate(fresh, diary, &d); err != nil {
			return err
		}
		return store.Insert(ctx, tx, c.p.TenantID, &d)
	})
	if dup(err) {
		return s.replayIdem(ctx, c, idem)
	}
	if err != nil {
		return DocView{}, err
	}
	v, err := s.Get(ctx, d.Ref)
	v.Replayed = replayed
	return v, err
}

// ActInput là thân chuyển trạng thái.
type ActInput struct {
	Ref         uuid.UUID
	Action      string
	BaseVersion *int64
	Reason      string
	IdemKey     string
}

// Act chuyển trạng thái: trình/duyệt/trả lại (Mẫu 01), trình ký/sửa lại (03, 04, 05), gửi (07), xác nhận/trả lại (TW — Mẫu
// 07), bản điều chỉnh (01 đã duyệt; 03, 04, 05 đã ký). Ký là SignScan (có tệp).
func (s *Service) Act(ctx context.Context, in ActInput) (DocView, error) {
	if in.Action == domain.ActSign {
		return DocView{}, problem.Validation.WithField("action", "ký bằng cách tải bản scan đã ký (POST …/signed)")
	}
	return s.transition(ctx, in, nil)
}

// transition là phần chung của Act và SignScan.
func (s *Service) transition(ctx context.Context, in ActInput, scan *store.Scan) (DocView, error) {
	c, err := s.writer(ctx, in.IdemKey, in.Action == domain.ActConfirm || in.Action == domain.ActReturn)
	if err != nil {
		return DocView{}, err
	}
	if in.BaseVersion == nil {
		return DocView{}, errBaseVersion
	}
	idem := idemDoc(in.IdemKey)
	prev, cur, pc, _, err := s.loadDoc(ctx, c, in.Ref, idem)
	if err != nil {
		return DocView{}, err
	}
	if prev != nil {
		return s.replay(ctx, prev.Ref)
	}
	spec, _ := domain.Lookup(cur.Form)
	if c.national() && !(cur.Form == domain.Form07 && (in.Action == domain.ActConfirm || in.Action == domain.ActReturn)) {
		return DocView{}, ErrReadOnly.WithDetail("Cấp TW chỉ xác nhận hoặc trả lại Mẫu 07 đã gửi")
	}
	step, ok := domain.Transition(spec.Lifecycle, cur.Status, in.Action)
	if !ok {
		return DocView{}, ErrDocState.WithDetail("thao tác \"" + in.Action + "\" không áp dụng cho biểu ở trạng thái " + cur.Status)
	}
	if !c.may(pc.p, cur.Form, in.Action) {
		return DocView{}, problem.Forbidden.WithDetail(denied(in.Action))
	}
	if pc.p.Event == mdomain.EventClosed && in.Action != domain.ActConfirm && in.Action != domain.ActReturn {
		return DocView{}, ErrProjectClose
	}
	reason := strings.TrimSpace(in.Reason)
	if step.Reason && !domain.ValidReason(reason) {
		return DocView{}, problem.Validation.WithField("reason", "ý kiến/lý do 5–1000 ký tự (FN-19: trả lại không ghi ý kiến bị chặn)")
	}
	if !step.Reason && utf8Len(reason) > domain.MaxReason {
		return DocView{}, problem.Validation.WithField("reason", "tối đa 1000 ký tự")
	}
	if !versionOK(in.BaseVersion, cur.Seq) {
		return DocView{}, ErrDocConflict
	}
	e := s.ext(ctx, pc, cur.Form)
	return s.write(ctx, c, *cur, in.BaseVersion, idem, func(fresh *pctx, diary *domain.DiaryFacts, d *store.Doc) error {
		if step.NewRev {
			d.Revision++
			d.Scan = nil
		}
		if domain.Editable(step.Status) || step.Complete {
			spec.Fill(&d.Content, facts(fresh, d.SiteRef, diary, e))
		}
		if step.Complete {
			if es := spec.Complete(d.Content); len(es) > 0 {
				return fieldsProblem(ErrDocIncomplete, es)
			}
		}
		if in.Action == domain.ActSend {
			if b := blockers(fresh, e.communes); len(b) > 0 {
				return ErrNeedSigned05.WithDetail(strings.Join(b, "; "))
			}
		}
		if scan != nil {
			d.Scan = scan
		}
		d.Event, d.Status = step.Event, step.Status
		if reason != "" {
			d.StatusReason = reason
		}
		return nil
	})
}

func utf8Len(s string) int { return len([]rune(s)) }

func denied(act string) string {
	switch act {
	case domain.ActApprove:
		return "chỉ lãnh đạo phòng duyệt Mẫu 01"
	case domain.ActReturn:
		return "chỉ lãnh đạo phòng (Mẫu 01) hoặc Cấp TW (Mẫu 07) trả lại"
	case domain.ActConfirm:
		return "chỉ Cấp TW (TTKN Quốc gia) xác nhận Mẫu 07"
	}
	return "chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin (FN-19 BR-01)"
}

// SignInput là bản scan đã ký.
type SignInput struct {
	Ref         uuid.UUID
	BaseVersion *int64
	FileName    string
	Data        []byte
	IdemKey     string
}

// SignScan ghi Đã ký kèm bản scan (Mẫu 03, 04, 05 — FN-19 B4: "Xuất Word, ký, tải bản đã ký"). Tệp ghi vào kho đối tượng
// TRƯỚC giao dịch (khóa theo băm — ghi lại cùng nội dung là bản cũ).
func (s *Service) SignScan(ctx context.Context, in SignInput) (DocView, error) {
	c, err := s.writer(ctx, in.IdemKey, false)
	if err != nil {
		return DocView{}, err
	}
	st := s.objects()
	if st == nil {
		return DocView{}, ErrDependency.WithDetail("chưa nối kho tệp")
	}
	if len(in.Data) == 0 || len(in.Data) > domain.MaxScanBytes {
		return DocView{}, ErrScanInvalid
	}
	ct, name, ok := domain.ScanType(in.FileName, in.Data)
	if !ok {
		return DocView{}, ErrScanInvalid
	}
	// Kiểm quyền, trạng thái trước khi ghi tệp (không ghi tệp rác cho người không có quyền).
	prev, cur, pc, _, err := s.loadDoc(ctx, c, in.Ref, idemDoc(in.IdemKey))
	if err != nil {
		return DocView{}, err
	}
	if prev != nil {
		return s.replay(ctx, prev.Ref)
	}
	spec, _ := domain.Lookup(cur.Form)
	if _, ok := domain.Transition(spec.Lifecycle, cur.Status, domain.ActSign); !ok {
		return DocView{}, ErrDocState.WithDetail("chỉ ký biểu đang Chờ ký")
	}
	if !c.editor(pc.p) {
		return DocView{}, problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin tải bản đã ký")
	}
	sum := objstore.SHA256Hex(in.Data)
	key := "cnts-knqg/" + c.p.TenantID.String() + "/" + in.Ref.String() + "/" + sum
	if _, err := st.Put(ctx, key, bytes.NewReader(in.Data), int64(len(in.Data)), sum, ct); err != nil {
		return DocView{}, err
	}
	return s.transition(ctx, ActInput{Ref: in.Ref, Action: domain.ActSign, BaseVersion: in.BaseVersion, IdemKey: in.IdemKey},
		&store.Scan{Key: key, Name: name, Type: ct, SHA256: sum, Size: int64(len(in.Data))})
}

// ScanFile là bản scan để tải.
type ScanFile struct {
	store.Scan
	Body io.ReadCloser
}

// Scan trả bản scan đã ký của biểu: seq = phiên bản cụ thể (0 = bản ký mới nhất).
func (s *Service) Scan(ctx context.Context, ref uuid.UUID, seq int64) (ScanFile, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ScanFile{}, err
	}
	var vs []store.Doc
	var pc *pctx
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if vs, e = store.Versions(ctx, tx, ref); e != nil || len(vs) == 0 {
			return e
		}
		pc, e = loadProject(ctx, tx, vs[0].ProjectRef)
		return e
	}); err != nil {
		return ScanFile{}, err
	}
	if len(vs) == 0 || pc == nil || !c.seesDoc(pc.p, vs[len(vs)-1], pc.communeOf(vs[len(vs)-1])) {
		return ScanFile{}, ErrDocNotFound
	}
	var hit *store.Scan
	for i := len(vs) - 1; i >= 0; i-- {
		if vs[i].Scan != nil && (seq == 0 || vs[i].Seq == seq) {
			hit = vs[i].Scan
			break
		}
	}
	if hit == nil {
		return ScanFile{}, ErrScanNotFound
	}
	st := s.objects()
	if st == nil {
		return ScanFile{}, ErrDependency.WithDetail("chưa nối kho tệp")
	}
	rc, _, err := st.Get(ctx, hit.Key)
	if errors.Is(err, objstore.ErrNotFound) {
		return ScanFile{}, ErrScanNotFound
	}
	if err != nil {
		return ScanFile{}, err
	}
	return ScanFile{Scan: *hit, Body: rc}, nil
}
