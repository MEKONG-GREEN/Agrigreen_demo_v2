package app

// Chương trình, dự án (UR-TINH-19, FN-19 BR-01, AC-04): chủ nhiệm tạo, sửa dự án mình phụ trách; lãnh đạo phòng, Admin
// sửa mọi dự án trong tỉnh; dự án gắn (tùy chọn) CT/DA chủ của KHTC (D20, Q-061 giả định). Mỗi thay đổi là một phiên bản.

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsmodel/store"
	knport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	accport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ProjectView là CT/DA (bản hiện hành) cho API.
type ProjectView struct {
	store.Project
	Status         string
	SiteCount      int
	HouseholdCount int
	CanEdit        bool
	Program        *knport.KHTCProgram
	Sites          []SiteView      // chỉ chi tiết
	Versions       []store.Project // chỉ chi tiết
	Names          map[uuid.UUID]string
	Replayed       bool
}

func projectStatus(event string) string {
	if event == domain.EventClosed {
		return domain.StatusClosed
	}
	return domain.StatusActive
}

// canCreateProject: Admin; Cấp Tỉnh nhóm chủ nhiệm, lãnh đạo phòng (hoặc chưa phân nhóm).
func (c caller) canCreateProject() bool {
	if !c.writable() {
		return false
	}
	return c.admin() || c.manager() || (c.province() && (c.k.ProvinceGroup == "" || c.k.ProvinceGroup == accport.GroupChuNhiem))
}

// canEditProject: Admin; lãnh đạo phòng trong tỉnh; chủ nhiệm của dự án (FN-19 BR-01, AC-04).
func (c caller) canEditProject(p store.Project) bool {
	if !c.writable() {
		return false
	}
	return c.admin() || (c.province() && c.sees(p.Province) && (c.manager() || p.ManagerRef == c.me()))
}

// seesProject: Admin; Cấp Tỉnh/TW trong tỉnh; Cấp Xã khi dự án có điểm ở xã mình (communes = xã của các điểm).
func (c caller) seesProject(p store.Project, communes []string) bool {
	if c.admin() || (!c.commune() && c.sees(p.Province)) {
		return true
	}
	if c.commune() {
		for _, x := range communes {
			if c.sees(x) {
				return true
			}
		}
	}
	return false
}

// ProjectInput là thân tạo/sửa CT/DA.
type ProjectInput struct {
	Ref         *uuid.UUID // tạo: UUIDv7 do máy khách sinh (tùy chọn); sửa: mã dự án
	Province    string     // tạo: Admin bắt buộc; Cấp Tỉnh suy từ phạm vi
	BaseVersion *int64     // sửa
	domain.Project
	IdemKey string
}

func idemProject(k string) string { return "cproj:" + k }

func (s *Service) checkProject(ctx context.Context, c caller, p *domain.Project, cur *store.Project) error {
	if p.ManagerRef == uuid.Nil && c.province() {
		p.ManagerRef = c.me()
	}
	if es := p.Normalize(); len(es) > 0 {
		return fieldsProblem(ErrProjectInvalid, es)
	}
	if err := noPII(ErrProjectInvalid, map[string]string{"name": p.Name, "basis": p.Basis, "scale_note": p.ScaleNote,
		"training_note": p.TrainingNote, "supplies_note": p.SuppliesNote}); err != nil {
		return err
	}
	// Chủ nhiệm (không phải lãnh đạo phòng/Admin) chỉ đứng tên chính mình; đổi chủ nhiệm là việc của lãnh đạo phòng.
	if !c.admin() && !c.manager() {
		switch {
		case cur == nil && p.ManagerRef != c.me():
			return ErrProjectInvalid.WithField("manager_ref", "chủ nhiệm chỉ tạo dự án do mình phụ trách")
		case cur != nil && p.ManagerRef != cur.ManagerRef:
			return ErrProjectInvalid.WithField("manager_ref", "chỉ lãnh đạo phòng đổi chủ nhiệm")
		}
	}
	if p.ProgramRef != nil {
		if s.Programs == nil {
			return ErrDependency
		}
		pr, err := s.Programs.Program(ctx, *p.ProgramRef)
		if errors.Is(err, knport.ErrKHTCProgramNotFound) {
			return ErrProjectInvalid.WithField("program_ref", "không có trong danh sách CT/DA của KHTC")
		}
		if err != nil {
			return err
		}
		if !pr.Active && (cur == nil || cur.ProgramRef == nil || *cur.ProgramRef != pr.Ref) {
			return ErrProjectInvalid.WithField("program_ref", "CT/DA của KHTC đã ngừng")
		}
	}
	return nil
}

// CreateProject tạo CT/DA.
func (s *Service) CreateProject(ctx context.Context, in ProjectInput) (ProjectView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return ProjectView{}, err
	}
	if !c.canCreateProject() {
		return ProjectView{}, problem.Forbidden.WithDetail("chỉ chủ nhiệm, lãnh đạo phòng Cấp Tỉnh (hoặc Admin) tạo chương trình, dự án")
	}
	province, err := s.callerProvince(ctx, c, in.Province)
	if err != nil {
		return ProjectView{}, err
	}
	if err := s.checkProject(ctx, c, &in.Project, nil); err != nil {
		return ProjectView{}, err
	}
	ref := ids.New()
	if in.Ref != nil {
		ref = *in.Ref
	}
	p := store.Project{Meta: c.meta(s.now(), idemProject(in.IdemKey)), Ref: ref, Event: domain.EventCreated, Province: province,
		Project: in.Project}
	replayed := false
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ProjectByIdem(ctx, tx, p.IdemKey); err != nil || prev != nil {
			if prev != nil {
				p, replayed = *prev, true
			}
			return err
		}
		if err := store.Lock(ctx, tx, "pcode:"+p.Code); err != nil {
			return err
		}
		if cur, err := store.LatestProject(ctx, tx, ref); err != nil {
			return err
		} else if cur != nil {
			return ErrProjectInvalid.WithField("id", "mã dự án đã dùng")
		}
		if taken, err := store.ProjectCodeTaken(ctx, tx, p.Code, ref); err != nil {
			return err
		} else if taken {
			return ErrProjectCodeTaken.WithField("code", "đã có dự án mã "+p.Code)
		}
		return store.InsertProject(ctx, tx, c.p.TenantID, &p)
	})
	if dup(err) {
		return s.CreateProject(ctx, in)
	}
	if err != nil {
		return ProjectView{}, err
	}
	v, err := s.GetProject(ctx, p.Ref)
	v.Replayed = replayed
	return v, err
}

// UpdateProject ghi phiên bản mới của CT/DA (khóa lạc quan base_version).
func (s *Service) UpdateProject(ctx context.Context, in ProjectInput) (ProjectView, error) {
	c, err := s.writer(ctx, in.IdemKey)
	if err != nil {
		return ProjectView{}, err
	}
	if in.Ref == nil {
		return ProjectView{}, ErrProjectNotFound
	}
	if in.BaseVersion == nil {
		return ProjectView{}, errBaseVersion
	}
	replayed := false
	var out store.Project
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ProjectByIdem(ctx, tx, idemProject(in.IdemKey)); err != nil || prev != nil {
			if prev != nil {
				out, replayed = *prev, true
			}
			return err
		}
		if err := store.Lock(ctx, tx, "proj:"+in.Ref.String()); err != nil {
			return err
		}
		cur, err := store.LatestProject(ctx, tx, *in.Ref)
		if err != nil {
			return err
		}
		if cur == nil || !c.seesProject(*cur, nil) {
			return ErrProjectNotFound
		}
		if !c.canEditProject(*cur) {
			return problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin sửa dự án (FN-19 BR-01)")
		}
		if cur.Event == domain.EventClosed {
			return ErrProjectClosed
		}
		if !versionOK(in.BaseVersion, cur.Seq) {
			return ErrProjectConflict
		}
		if err := s.checkProject(ctx, c, &in.Project, cur); err != nil {
			return err
		}
		if err := store.Lock(ctx, tx, "pcode:"+in.Code); err != nil {
			return err
		}
		if taken, err := store.ProjectCodeTaken(ctx, tx, in.Code, cur.Ref); err != nil {
			return err
		} else if taken {
			return ErrProjectCodeTaken.WithField("code", "đã có dự án mã "+in.Code)
		}
		out = store.Project{Meta: c.meta(s.now(), idemProject(in.IdemKey)), Ref: cur.Ref, Event: domain.EventUpdated,
			Province: cur.Province, Project: in.Project}
		return store.InsertProject(ctx, tx, c.p.TenantID, &out)
	})
	if dup(err) {
		return s.UpdateProject(ctx, in)
	}
	if err != nil {
		return ProjectView{}, err
	}
	v, err := s.GetProject(ctx, out.Ref)
	v.Replayed = replayed
	return v, err
}

// SetProjectStatus đóng (kết thúc/đã nghiệm thu) hoặc mở lại CT/DA — lý do bắt buộc.
func (s *Service) SetProjectStatus(ctx context.Context, ref uuid.UUID, closeIt bool, base *int64, reason, idem string) (ProjectView, error) {
	c, err := s.writer(ctx, idem)
	if err != nil {
		return ProjectView{}, err
	}
	if base == nil {
		return ProjectView{}, errBaseVersion
	}
	if !domain.ValidReason(reason) {
		return ProjectView{}, problem.Validation.WithField("reason", "5–1000 ký tự")
	}
	err = s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		if prev, err := store.ProjectByIdem(ctx, tx, idemProject(idem)); err != nil || prev != nil {
			return err
		}
		if err := store.Lock(ctx, tx, "proj:"+ref.String()); err != nil {
			return err
		}
		cur, err := store.LatestProject(ctx, tx, ref)
		if err != nil {
			return err
		}
		if cur == nil || !c.seesProject(*cur, nil) {
			return ErrProjectNotFound
		}
		if !c.canEditProject(*cur) {
			return problem.Forbidden.WithDetail("chỉ chủ nhiệm của dự án, lãnh đạo phòng hoặc Admin")
		}
		if !versionOK(base, cur.Seq) {
			return ErrProjectConflict
		}
		ev := domain.EventReopened
		if closeIt {
			ev = domain.EventClosed
		}
		if (cur.Event == domain.EventClosed) == closeIt {
			return ErrProjectConflict.WithDetail("dự án đã ở trạng thái này")
		}
		n := *cur
		n.Meta, n.Event, n.StatusReason = c.meta(s.now(), idemProject(idem)), ev, strings.TrimSpace(reason)
		return store.InsertProject(ctx, tx, c.p.TenantID, &n)
	})
	if dup(err) {
		err = nil
	}
	if err != nil {
		return ProjectView{}, err
	}
	return s.GetProject(ctx, ref)
}

// ProjectQuery lọc danh sách.
type ProjectQuery struct {
	Status string // active | closed | ""
	Kind   string
	Q      string // tìm theo mã/tên
}

func (s *Service) projectView(c caller, p store.Project) ProjectView {
	return ProjectView{Project: p, Status: projectStatus(p.Event), CanEdit: c.canEditProject(p) && p.Event != domain.EventClosed}
}

// ListProjects liệt kê CT/DA trong phạm vi.
func (s *Service) ListProjects(ctx context.Context, q ProjectQuery) ([]ProjectView, map[uuid.UUID]string, bool, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, nil, false, err
	}
	switch q.Status {
	case "", domain.StatusActive, domain.StatusClosed:
	default:
		return nil, nil, false, problem.Validation.WithField("status", "active hoặc closed")
	}
	var ps []store.Project
	var sites []store.Site
	var hhs []store.Household
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if ps, e = store.ListProjects(ctx, tx); e != nil {
			return e
		}
		if sites, e = store.ListSites(ctx, tx, store.SiteFilter{}); e != nil {
			return e
		}
		hhs, e = store.ListHouseholds(ctx, tx, store.HouseholdFilter{})
		return e
	}); err != nil {
		return nil, nil, false, err
	}
	communes, siteCount, hhCount := map[uuid.UUID][]string{}, map[uuid.UUID]int{}, map[uuid.UUID]int{}
	for _, x := range sites {
		communes[x.ProjectRef] = append(communes[x.ProjectRef], x.CommuneCode)
		if x.Event != domain.EventClosed {
			siteCount[x.ProjectRef]++
		}
	}
	for _, h := range hhs {
		if h.Event != domain.EventWithdrawn {
			hhCount[h.ProjectRef]++
		}
	}
	needle := strings.ToLower(strings.TrimSpace(q.Q))
	out := []ProjectView{}
	refs := []uuid.UUID{}
	for _, p := range ps {
		if !c.seesProject(p, communes[p.Ref]) {
			continue
		}
		v := s.projectView(c, p)
		if (q.Status != "" && v.Status != q.Status) || (q.Kind != "" && p.Kind != q.Kind) ||
			(needle != "" && !strings.Contains(strings.ToLower(p.Code+" "+p.Name), needle)) {
			continue
		}
		v.SiteCount, v.HouseholdCount = siteCount[p.Ref], hhCount[p.Ref]
		out = append(out, v)
		refs = append(refs, p.ManagerRef)
	}
	return out, s.names(ctx, refs), c.canCreateProject(), nil
}

// GetProject đọc chi tiết CT/DA kèm điểm và lịch sử phiên bản.
func (s *Service) GetProject(ctx context.Context, ref uuid.UUID) (ProjectView, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return ProjectView{}, err
	}
	var vs []store.Project
	var sites []store.Site
	var hhs []store.Household
	var points map[uuid.UUID]domain.Point
	var follow []store.Grant
	if err := s.tx(ctx, c.p.TenantID, func(tx pgx.Tx) error {
		var e error
		if vs, e = store.ProjectVersions(ctx, tx, ref); e != nil || len(vs) == 0 {
			return e
		}
		if sites, e = store.ListSites(ctx, tx, store.SiteFilter{Project: &ref}); e != nil {
			return e
		}
		if hhs, e = store.ListHouseholds(ctx, tx, store.HouseholdFilter{Project: &ref}); e != nil {
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
		return ProjectView{}, err
	}
	if len(vs) == 0 {
		return ProjectView{}, ErrProjectNotFound
	}
	cur := vs[len(vs)-1]
	communes := make([]string, 0, len(sites))
	for _, x := range sites {
		communes = append(communes, x.CommuneCode)
	}
	if !c.seesProject(cur, communes) {
		return ProjectView{}, ErrProjectNotFound
	}
	v := s.projectView(c, cur)
	v.Versions = vs
	hhBySite := map[uuid.UUID]int{}
	for _, h := range hhs {
		if h.Event != domain.EventWithdrawn {
			hhBySite[h.SiteRef]++
			v.HouseholdCount++
		}
	}
	refs := []uuid.UUID{cur.ManagerRef}
	for _, x := range sites {
		if !c.seesSite(x) {
			continue
		}
		sv := s.siteView(c, cur, x, follow)
		if p, ok := points[x.Ref]; ok {
			sv.Point = &p
		}
		sv.HouseholdCount = hhBySite[x.Ref]
		v.Sites = append(v.Sites, sv)
		if x.Event != domain.EventClosed {
			v.SiteCount++
		}
		if x.OfficerRef != nil {
			refs = append(refs, *x.OfficerRef)
		}
	}
	for _, x := range vs {
		refs = append(refs, x.ActorRef)
	}
	if cur.ProgramRef != nil && s.Programs != nil {
		if pr, err := s.Programs.Program(ctx, *cur.ProgramRef); err == nil {
			v.Program = &pr
		}
	}
	v.Names = s.names(ctx, refs)
	return v, nil
}

// Programs liệt kê CT/DA chủ của KHTC còn dùng (để gắn dự án).
func (s *Service) KHTCPrograms(ctx context.Context) ([]knport.KHTCProgram, error) {
	c, err := s.reader(ctx)
	if err != nil {
		return nil, err
	}
	if !c.admin() && !c.province() {
		return nil, problem.Forbidden.WithDetail("chỉ Cấp Tỉnh, Admin")
	}
	if s.Programs == nil {
		return []knport.KHTCProgram{}, nil
	}
	list, err := s.Programs.Programs(ctx, knport.KHTCProgramFilter{})
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []knport.KHTCProgram{}
	}
	return list, nil
}
