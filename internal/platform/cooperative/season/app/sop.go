package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	port "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/season/store"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Vai soạn SOP (Admin API) và vai phát hành.
var (
	sopAuthorRoles  = []string{port.RoleSOPAdmin, authz.RolePlatformAdmin}
	sopPublishRoles = []string{authz.RoleParamApprover}
	sopAdminRead    = []string{port.RoleSOPAdmin, authz.RoleParamApprover, authz.RolePlatformAdmin}
)

// SOPView là mẫu SOP kèm các phiên bản.
type SOPView struct {
	Template store.Template
	Versions []store.Version
}

// CreateSOPInput tạo mẫu SOP mới với bản nháp phiên bản 1 (HTX US-CAT-01).
type CreateSOPInput struct {
	Code           string
	Name           string
	Crop           string
	Scope          string
	CooperativeRef uuid.UUID
	Standard       string
	Description    string
	Steps          []domain.Step
	ChangeNote     string
}

func (s *Service) checkTemplateFields(ctx context.Context, in *CreateSOPInput) error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.TrimSpace(in.Name)
	in.Crop = strings.TrimSpace(in.Crop)
	in.Standard = strings.TrimSpace(in.Standard)
	if in.Crop == "" {
		in.Crop = "lua"
	}
	if in.Scope == "" {
		in.Scope = domain.ScopeSystem
	}
	switch {
	case !domain.ValidSOPCode(in.Code):
		return problem.Validation.WithField("code", "2–40 ký tự A-Z, 0-9, _ . -")
	case in.Name == "" || len([]rune(in.Name)) > 200:
		return problem.Validation.WithField("name", "bắt buộc, tối đa 200 ký tự")
	case !domain.ValidCode(in.Crop):
		return problem.Validation.WithField("crop", "dạng [a-z][a-z0-9_]")
	case in.Standard != "" && !domain.ValidCode(in.Standard):
		return problem.Validation.WithField("standard", "dạng [a-z][a-z0-9_] (vd vietgap, srp)")
	case in.Scope != domain.ScopeSystem && in.Scope != domain.ScopeHTX:
		return problem.Validation.WithField("scope", "phải là system hoặc htx")
	case in.Scope == domain.ScopeHTX && in.CooperativeRef == uuid.Nil:
		return problem.Validation.WithField("cooperative_ref", "quy trình riêng của HTX phải gắn với một HTX")
	case in.Scope == domain.ScopeSystem && in.CooperativeRef != uuid.Nil:
		return problem.Validation.WithField("cooperative_ref", "quy trình hệ thống không gắn HTX")
	}
	desc, err := domain.CleanText("description", in.Description, 2000)
	if err != nil {
		return fieldProblem(err)
	}
	in.Description = desc
	if in.Scope == domain.ScopeHTX {
		return s.checkCoop(ctx, in.CooperativeRef)
	}
	return nil
}

// CreateSOP tạo mẫu SOP (nháp) — sop_admin.
func (s *Service) CreateSOP(ctx context.Context, in CreateSOPInput) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	if err := s.checkTemplateFields(ctx, &in); err != nil {
		return SOPView{}, err
	}
	steps, err := domain.NormalizeSteps(in.Steps)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	note, err := domain.CleanText("change_note", in.ChangeNote, 1000)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	now := s.now()
	t := store.Template{
		Ref: ids.New(), Code: in.Code, Name: in.Name, NameKey: domain.NameKey(in.Name), Crop: in.Crop, Scope: in.Scope,
		CooperativeRef: in.CooperativeRef, Standard: in.Standard, Description: in.Description, Status: domain.TemplateDraft,
		LatestVersion: 1, CreatedBy: p.ActorRef,
	}
	v := store.Version{ID: ids.New(), TemplateRef: t.Ref, VersionNo: 1, Status: domain.VersionDraft, Steps: steps,
		ChangeNote: note, AuthoredBy: p.ActorRef, AuthoredAt: now}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if err := insertTemplate(ctx, tx, p.TenantID, t); err != nil {
			return err
		}
		if err := store.InsertVersion(ctx, tx, p.TenantID, v); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

func insertTemplate(ctx context.Context, tx pgx.Tx, tid uuid.UUID, t store.Template) error {
	codeTaken, nameTaken, err := store.TemplateTaken(ctx, tx, t.Code, t.NameKey)
	if err != nil {
		return err
	}
	if nameTaken {
		return ErrSOPNameTaken
	}
	if codeTaken {
		return ErrSOPCodeTaken
	}
	err = store.InsertTemplate(ctx, tx, tid, t)
	switch {
	case isUnique(err, "sop_template_tenant_id_name_key_key"):
		return ErrSOPNameTaken
	case isUnique(err, "sop_template_tenant_id_code_key"):
		return ErrSOPCodeTaken
	}
	return err
}

func (s *Service) loadView(ctx context.Context, tx pgx.Tx, ref uuid.UUID, out *SOPView) error {
	t, err := store.TemplateByRef(ctx, tx, ref, false)
	if err != nil {
		return err
	}
	if t == nil {
		return ErrSOPNotFound
	}
	vs, err := store.VersionsOf(ctx, tx, ref)
	if err != nil {
		return err
	}
	*out = SOPView{Template: *t, Versions: vs}
	return nil
}

// NewVersionInput tạo bản nháp phiên bản mới. Steps nil → chép bước của phiên bản mới nhất.
type NewVersionInput struct {
	TemplateRef uuid.UUID
	Steps       []domain.Step
	CopySteps   bool
	ChangeNote  string
}

// NewVersion tạo bản nháp phiên bản mới của mẫu (phiên bản đã phát hành bất biến — BR-07).
func (s *Service) NewVersion(ctx context.Context, in NewVersionInput) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	var steps []domain.Step
	if !in.CopySteps {
		if steps, err = domain.NormalizeSteps(in.Steps); err != nil {
			return SOPView{}, fieldProblem(err)
		}
	}
	note, err := domain.CleanText("change_note", in.ChangeNote, 1000)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TemplateByRef(ctx, tx, in.TemplateRef, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		if t.Status == domain.TemplateRetired {
			return ErrSOPState.WithDetail("mẫu đã ngừng dùng")
		}
		vs, err := store.VersionsOf(ctx, tx, t.Ref)
		if err != nil {
			return err
		}
		latest := vs[len(vs)-1]
		if latest.Status == domain.VersionDraft {
			return ErrSOPState.WithDetail("mẫu đang có bản nháp phiên bản " + itoa(latest.VersionNo) + " — sửa bản nháp đó")
		}
		if in.CopySteps {
			steps = latest.Steps
		}
		v := store.Version{ID: ids.New(), TemplateRef: t.Ref, VersionNo: latest.VersionNo + 1, Status: domain.VersionDraft,
			Steps: steps, SourceVersionRef: latest.ID, ChangeNote: note, AuthoredBy: p.ActorRef, AuthoredAt: s.now()}
		if err := store.InsertVersion(ctx, tx, p.TenantID, v); err != nil {
			return err
		}
		t.LatestVersion = v.VersionNo
		if err := store.SetTemplateState(ctx, tx, *t); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

// UpdateDraft thay bước của bản nháp (HTX US-CAT-01 AC-1, AC-6).
func (s *Service) UpdateDraft(ctx context.Context, versionID uuid.UUID, steps []domain.Step, changeNote string) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	steps, err = domain.NormalizeSteps(steps)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	note, err := domain.CleanText("change_note", changeNote, 1000)
	if err != nil {
		return SOPView{}, fieldProblem(err)
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, err := store.VersionByID(ctx, tx, versionID, true)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrSOPNotFound
		}
		if v.Status != domain.VersionDraft {
			return ErrSOPState.WithDetail("phiên bản đã phát hành không sửa được — tạo phiên bản mới")
		}
		v.Steps, v.ChangeNote = steps, note
		if err := store.UpdateDraft(ctx, tx, *v); err != nil {
			return err
		}
		return s.loadView(ctx, tx, v.TemplateRef, &view)
	})
	return view, err
}

// PublishSOP phát hành bản nháp: param_approver, khác người soạn (R-56); phải có
// bước thu hoạch. Phát pf.cropcycle.sop.published trong cùng giao dịch.
func (s *Service) PublishSOP(ctx context.Context, versionID uuid.UUID) (SOPView, error) {
	p, err := authz.Require(ctx, sopPublishRoles...)
	if err != nil {
		return SOPView{}, err
	}
	now := s.now()
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		v, err := store.VersionByID(ctx, tx, versionID, true)
		if err != nil {
			return err
		}
		if v == nil {
			return ErrSOPNotFound
		}
		if err := authz.EnsureDistinctApprover(v.AuthoredBy, p); err != nil {
			return err
		}
		t, err := store.TemplateByRef(ctx, tx, v.TemplateRef, true)
		if err != nil {
			return err
		}
		if v.Status != domain.VersionDraft || t.Status == domain.TemplateRetired {
			return ErrSOPState.WithDetail("chỉ phát hành được bản nháp của mẫu chưa ngừng dùng")
		}
		if _, err := domain.NormalizeSteps(v.Steps); err != nil {
			return fieldProblem(err)
		}
		h, ok := domain.HarvestStep(v.Steps)
		if !ok {
			return ErrSOPNoHarvestStep
		}
		if err := store.PublishVersion(ctx, tx, v.ID, p.ActorRef, now); err != nil {
			return err
		}
		no := v.VersionNo
		t.Status, t.PublishedVersion = domain.TemplatePublished, &no
		if err := store.SetTemplateState(ctx, tx, *t); err != nil {
			return err
		}
		if err := enqueue(ctx, tx, p.TenantID, port.EventSOPPublished, now, port.SOPPublishedV1{
			TemplateRef: t.Ref, Code: t.Code, Name: t.Name, Scope: t.Scope, CooperativeRef: uuidPtr(t.CooperativeRef),
			VersionID: v.ID, VersionNo: v.VersionNo, StepCount: len(v.Steps), HarvestDayOffset: h.DayOffset,
			HarvestWindowDays: h.WindowDays, AuthoredBy: v.AuthoredBy, PublishedBy: p.ActorRef, PublishedAt: now.Format(time.RFC3339),
		}); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

// CloneInput nhân bản mẫu SOP (HTX US-CAT-03).
type CloneInput struct {
	SourceRef      uuid.UUID
	Code           string
	Name           string
	Scope          string
	CooperativeRef uuid.UUID
}

// CloneSOP tạo mẫu mới giữ nguyên toàn bộ bước và thứ tự của phiên bản nguồn
// (bản đã phát hành mới nhất, không có thì bản mới nhất); bản sao là nháp, tên
// phải khác mọi mẫu (AC-3); sửa bản sao không đổi nguồn (AC-2 — dòng khác).
func (s *Service) CloneSOP(ctx context.Context, in CloneInput) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		src, err := store.TemplateByRef(ctx, tx, in.SourceRef, false)
		if err != nil {
			return err
		}
		if src == nil {
			return ErrSOPNotFound
		}
		ver, err := store.LatestPublished(ctx, tx, src.Ref)
		if err != nil {
			return err
		}
		if ver == nil {
			vs, err := store.VersionsOf(ctx, tx, src.Ref)
			if err != nil {
				return err
			}
			ver = &vs[len(vs)-1]
		}
		ci := CreateSOPInput{Code: in.Code, Name: in.Name, Crop: src.Crop, Scope: in.Scope, CooperativeRef: in.CooperativeRef,
			Standard: src.Standard, Description: src.Description}
		if ci.Scope == "" {
			ci.Scope, ci.CooperativeRef = src.Scope, src.CooperativeRef
		}
		ref := ids.New()
		if strings.TrimSpace(ci.Code) == "" {
			ci.Code = "SOP-" + strings.ToUpper(ref.String()[28:36])
		}
		if err := s.checkTemplateFields(ctx, &ci); err != nil {
			return err
		}
		t := store.Template{Ref: ref, Code: ci.Code, Name: ci.Name, NameKey: domain.NameKey(ci.Name), Crop: ci.Crop, Scope: ci.Scope,
			CooperativeRef: ci.CooperativeRef, Standard: ci.Standard, Description: ci.Description, Status: domain.TemplateDraft,
			SourceTemplateRef: src.Ref, LatestVersion: 1, CreatedBy: p.ActorRef}
		if err := insertTemplate(ctx, tx, p.TenantID, t); err != nil {
			return err
		}
		v := store.Version{ID: ids.New(), TemplateRef: t.Ref, VersionNo: 1, Status: domain.VersionDraft, Steps: ver.Steps,
			SourceVersionRef: ver.ID, ChangeNote: "Nhân bản từ " + src.Code + " v" + itoa(ver.VersionNo), AuthoredBy: p.ActorRef, AuthoredAt: s.now()}
		if err := store.InsertVersion(ctx, tx, p.TenantID, v); err != nil {
			return err
		}
		return s.loadView(ctx, tx, t.Ref, &view)
	})
	return view, err
}

// RetireSOP ngừng dùng mẫu (không xóa — HTX US-CAT-01 AC-2): vụ đã ghim vẫn giữ phiên bản.
func (s *Service) RetireSOP(ctx context.Context, ref uuid.UUID, reason string) (SOPView, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return SOPView{}, err
	}
	if !domain.ValidReason(reason) {
		return SOPView{}, problem.Validation.WithField("reason", "bắt buộc, ít nhất 10 ký tự")
	}
	var view SOPView
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TemplateByRef(ctx, tx, ref, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		if t.Status == domain.TemplateRetired {
			return ErrSOPState.WithDetail("mẫu đã ngừng dùng")
		}
		t.Status, t.StatusReason = domain.TemplateRetired, strings.TrimSpace(reason)
		if err := store.SetTemplateState(ctx, tx, *t); err != nil {
			return err
		}
		return s.loadView(ctx, tx, ref, &view)
	})
	return view, err
}

// BindVariety gắn giống lúa với mẫu SOP mặc định (HTX US-SEASON-01 AC-1).
func (s *Service) BindVariety(ctx context.Context, variety string, template uuid.UUID, note string) (store.Binding, error) {
	p, err := authz.Require(ctx, sopAuthorRoles...)
	if err != nil {
		return store.Binding{}, err
	}
	variety = strings.TrimSpace(variety)
	if _, err := s.catalogItem(ctx, mdport.CatalogRiceVariety, "variety_code", variety); err != nil {
		return store.Binding{}, err
	}
	note, err = domain.CleanText("note", note, 500)
	if err != nil {
		return store.Binding{}, fieldProblem(err)
	}
	b := store.Binding{ID: ids.New(), VarietyCode: variety, TemplateRef: template, Note: note, BoundBy: p.ActorRef, BoundAt: s.now()}
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.TemplateByRef(ctx, tx, template, false)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrSOPNotFound
		}
		if t.Scope != domain.ScopeSystem || t.Status != domain.TemplatePublished {
			return ErrBindingTemplateBad
		}
		old, err := store.CurrentBinding(ctx, tx, variety, true)
		if err != nil {
			return err
		}
		if old != nil && old.TemplateRef == template {
			b = *old
			return nil
		}
		return store.ReplaceBinding(ctx, tx, p.TenantID, old, b)
	})
	return b, err
}

// Bindings liệt kê gắn giống ↔ SOP hiện hành.
func (s *Service) Bindings(ctx context.Context) ([]store.Binding, error) {
	p, err := authz.Require(ctx, append(append([]string{}, readRoles...), sopAdminRead...)...)
	if err != nil {
		return nil, err
	}
	var out []store.Binding
	err = s.inTx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListBindings(ctx, tx)
		return e
	})
	return out, err
}

// SOPListInput lọc danh sách mẫu.
type SOPListInput struct {
	Status string
	Admin  bool // Admin API: mọi mẫu, kể cả nháp
	After  uuid.UUID
	Limit  int
}

// ListSOPs liệt kê mẫu. Application API: chỉ mẫu đã ban hành, mẫu hệ thống +
// mẫu riêng của HTX người gọi; Admin API: mọi mẫu.
func (s *Service) ListSOPs(ctx context.Context, in SOPListInput) (Page[store.Template], error) {
	f := store.TemplateFilter{Status: in.Status, After: in.After, Limit: clampLimit(in.Limit) + 1}
	var tid uuid.UUID
	if in.Admin {
		p, err := authz.Require(ctx, sopAdminRead...)
		if err != nil {
			return Page[store.Template]{}, err
		}
		tid = p.TenantID
	} else {
		a, err := s.access(ctx, readRoles...)
		if err != nil {
			return Page[store.Template]{}, err
		}
		tid = a.p.TenantID
		all, coop := a.sopVisibility()
		f.OnlyVisibleTo, f.CooperativeRef, f.Status = !all, coop, domain.TemplatePublished
	}
	var items []store.Template
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		items, e = store.ListTemplates(ctx, tx, f)
		return e
	})
	if err != nil {
		return Page[store.Template]{}, err
	}
	return pageOf(items, clampLimit(in.Limit), func(t store.Template) uuid.UUID { return t.Ref }), nil
}

func pageOf[T any](items []T, limit int, key func(T) uuid.UUID) Page[T] {
	var pg Page[T]
	if len(items) > limit {
		items = items[:limit]
		k := key(items[limit-1])
		pg.Next = &k
	}
	pg.Items = items
	if pg.Items == nil {
		pg.Items = []T{}
	}
	return pg
}

// GetSOP đọc mẫu kèm phiên bản. Application API chỉ thấy phiên bản đã phát hành
// của mẫu nhìn thấy được.
func (s *Service) GetSOP(ctx context.Context, ref uuid.UUID, admin bool) (SOPView, error) {
	var tid uuid.UUID
	visible := func(store.Template) bool { return true }
	if admin {
		p, err := authz.Require(ctx, sopAdminRead...)
		if err != nil {
			return SOPView{}, err
		}
		tid = p.TenantID
	} else {
		a, err := s.access(ctx, readRoles...)
		if err != nil {
			return SOPView{}, err
		}
		tid = a.p.TenantID
		all, coop := a.sopVisibility()
		visible = func(t store.Template) bool {
			return t.PublishedVersion != nil && (all || t.Scope == domain.ScopeSystem || t.CooperativeRef == coop)
		}
	}
	var view SOPView
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error { return s.loadView(ctx, tx, ref, &view) })
	if err != nil {
		return SOPView{}, err
	}
	if !visible(view.Template) {
		return SOPView{}, ErrSOPNotFound
	}
	if !admin {
		pub := view.Versions[:0:0]
		for _, v := range view.Versions {
			if v.Status == domain.VersionPublished {
				pub = append(pub, v)
			}
		}
		view.Versions = pub
	}
	return view, nil
}

// GetSOPVersion đọc một phiên bản (Application API: chỉ bản đã phát hành).
func (s *Service) GetSOPVersion(ctx context.Context, id uuid.UUID, admin bool) (store.Version, store.Template, error) {
	var tid uuid.UUID
	all, coop := true, uuid.Nil
	if admin {
		p, err := authz.Require(ctx, sopAdminRead...)
		if err != nil {
			return store.Version{}, store.Template{}, err
		}
		tid = p.TenantID
	} else {
		a, err := s.access(ctx, readRoles...)
		if err != nil {
			return store.Version{}, store.Template{}, err
		}
		tid = a.p.TenantID
		all, coop = a.sopVisibility()
	}
	var v *store.Version
	var t *store.Template
	err := s.inTx(ctx, tid, func(tx pgx.Tx) error {
		var e error
		if v, e = store.VersionByID(ctx, tx, id, false); e != nil || v == nil {
			return e
		}
		t, e = store.TemplateByRef(ctx, tx, v.TemplateRef, false)
		return e
	})
	if err != nil {
		return store.Version{}, store.Template{}, err
	}
	if v == nil || t == nil || (!admin && (v.Status != domain.VersionPublished || (!all && t.Scope != domain.ScopeSystem && t.CooperativeRef != coop))) {
		return store.Version{}, store.Template{}, ErrSOPNotFound
	}
	return *v, *t, nil
}

func itoa(n int) string { return strconv.Itoa(n) }
