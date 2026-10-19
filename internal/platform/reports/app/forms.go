package app

// Capability mẫu lưới: nháp, thao tác, đề nghị và phát hành bốn mắt (R-56),
// phiên bản bất biến, gán loại, nhập Excel và nhật ký. Bộ seed thuộc đăng ký
// của ứng dụng; FormEngine cung cấp thao tác chung cho bên dùng.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/form"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/formimport"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi mẫu báo cáo (docs/errors.md).
var (
	ErrFormNotFound        = problem.New(http.StatusNotFound, "reports.form_not_found", "Không tìm thấy mẫu báo cáo")
	ErrFormVersionNotFound = problem.New(http.StatusNotFound, "reports.form_version_not_found", "Không tìm thấy phiên bản mẫu")
	ErrFormCodeTaken       = problem.New(http.StatusConflict, "reports.form_code_taken", "Mã mẫu đã có trong phòng ban")
	ErrFormDraftExists     = problem.New(http.StatusConflict, "reports.form_draft_exists", "Mẫu đang có phiên bản nháp hoặc chờ phát hành")
	ErrFormVersionLocked   = problem.New(http.StatusConflict, "reports.form_version_locked", "Chỉ sửa được phiên bản nháp; phiên bản đã phát hành là bất biến")
	ErrFormNotProposed     = problem.New(http.StatusConflict, "reports.form_not_proposed", "Phiên bản chưa được đề nghị phát hành")
	ErrFormNotPublished    = problem.New(http.StatusConflict, "reports.form_not_published", "Phiên bản chưa phát hành")
	ErrFormNotAssigned     = problem.New(http.StatusConflict, "reports.form_not_assigned", "Mẫu chưa gán loại báo cáo — không phát hành được")
	ErrFormEffective       = problem.New(http.StatusUnprocessableEntity, "reports.form_effective_invalid", "“Áp dụng từ kỳ” không hợp lệ")
	ErrFormRetired         = problem.New(http.StatusConflict, "reports.form_retired", "Mẫu đã ngừng dùng")
	ErrFormAssigned        = problem.New(http.StatusConflict, "reports.form_already_assigned", "Mẫu đã gán loại báo cáo này")
	ErrAssignmentNotFound  = problem.New(http.StatusNotFound, "reports.form_assignment_not_found", "Không tìm thấy gán mẫu")
	ErrImportInvalid       = problem.New(http.StatusUnprocessableEntity, "reports.form_import_invalid", "Tệp Excel không phải mẫu báo cáo hợp lệ")
)

// Vai của mẫu báo cáo KN v7.7: Admin (platform_admin) và reports_admin thiết kế, đề nghị và
// phát hành — người phát hành phải KHÁC người đề nghị/sửa cuối (R-56).
var formAdmins = []string{port.RoleReportsAdmin, authz.RolePlatformAdmin}

// formReader retains native realm roles; applications register their session
// policy separately. A resolver error or explicit denial never falls back.
func (s *Service) formReader(ctx context.Context, extra ...string) (authz.Principal, *port.SessionActor, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return authz.Principal{}, nil, problem.Unauthenticated
	}
	if s.ResolveSession != nil {
		actor, handled, err := s.ResolveSession(ctx, p, port.SessionReadForms)
		if err != nil {
			return p, nil, err
		}
		if handled {
			if !actor.Allowed || actor.AllowsDepartment == nil {
				return p, nil, problem.Forbidden
			}
			return p, &actor, nil
		}
	}
	p, err := authz.Require(ctx, append(append([]string{}, s.formReaderRoles()...), extra...)...)
	return p, nil, err
}

func formDeptAllows(actor *port.SessionActor, dept string) bool {
	return actor == nil || (actor.AllowsDepartment != nil && actor.AllowsDepartment(dept))
}

// FormSummary là một mẫu kèm phiên bản đang soạn và phiên bản phát hành mới nhất.
type FormSummary struct {
	Template    store.FormTemplate
	Open        *store.FormVersion
	Published   *store.FormVersion // phiên bản đã phát hành có áp dụng từ lớn nhất
	ReportTypes []string
}

// FormDetail là mẫu kèm mọi phiên bản và gán.
type FormDetail struct {
	Template    store.FormTemplate
	Versions    []store.FormVersion
	Assignments []store.Assignment
}

func reason(r string, required bool) (*string, error) {
	r = strings.TrimSpace(r)
	if r == "" && !required {
		return nil, nil
	}
	if n := len([]rune(r)); n < 10 || n > 500 {
		return nil, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	return &r, nil
}

func defProblem(err error) error {
	var ve *form.ValidationError
	if errors.As(err, &ve) {
		p := problem.New(http.StatusUnprocessableEntity, ve.Code(), "Định nghĩa mẫu không hợp lệ")
		for i, is := range ve.Issues {
			if i >= 50 {
				break
			}
			p = p.WithField("definition."+is.Field, is.Msg)
		}
		return p
	}
	var oe *form.OpError
	if errors.As(err, &oe) {
		return problem.New(http.StatusUnprocessableEntity, oe.Code, "Thao tác lưới không hợp lệ").WithField(fmt.Sprintf("ops[%d]", oe.Index), oe.Msg)
	}
	return err
}

func idemOr(key, suffix string) string {
	if key == "" {
		key = "gen-" + ids.New().String()
	}
	if suffix != "" {
		key += ":" + suffix
	}
	return key
}

func (s *Service) formEvent(ctx context.Context, tx pgx.Tx, tid uuid.UUID, actor uuid.UUID, tplID uuid.UUID, v *store.FormVersion, action string, changes any, why *string, idem string) error {
	b, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	if string(b) == "null" {
		b = []byte("[]")
	}
	e := store.FormEvent{ID: ids.New(), TemplateID: tplID, Action: action, Changes: b, Reason: why, EventTime: s.now(), ActorRef: actor, IdemKey: idem}
	if v != nil {
		e.VersionID, e.Version = &v.ID, &v.Version
	}
	return store.InsertFormEvent(ctx, tx, tid, &e)
}

// Change là một thay đổi trong nhật ký (field, cũ → mới).
type Change struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

// diffDef tóm tắt thay đổi định nghĩa cho nhật ký (tối đa 200 mục).
func diffDef(a, b port.FormDefinition) []Change {
	var out []Change
	add := func(f string, o, n any) {
		if len(out) < 200 {
			out = append(out, Change{f, o, n})
		}
	}
	if a.Title != b.Title {
		add("title", a.Title, b.Title)
	}
	if a.SheetName != b.SheetName {
		add("sheet_name", a.SheetName, b.SheetName)
	}
	if a.Requirements != b.Requirements {
		add("requirements", a.Requirements, b.Requirements)
	}
	if len(a.Columns) != len(b.Columns) {
		add("columns.count", len(a.Columns), len(b.Columns))
	}
	for i := 0; i < max(len(a.Columns), len(b.Columns)); i++ {
		var ca, cb *port.FormColumn
		if i < len(a.Columns) {
			ca = &a.Columns[i]
		}
		if i < len(b.Columns) {
			cb = &b.Columns[i]
		}
		switch {
		case ca == nil:
			add("columns."+cb.Letter, nil, cb.Title)
		case cb == nil:
			add("columns."+ca.Letter, ca.Title, nil)
		default:
			for _, f := range [][3]string{{"title", ca.Title, cb.Title}, {"kind", ca.Kind, cb.Kind}, {"dtype", ca.DType, cb.DType}, {"formula", ca.Formula, cb.Formula}, {"unit", ca.Unit, cb.Unit}} {
				if f[1] != f[2] {
					add("columns."+ca.Letter+"."+f[0], f[1], f[2])
				}
			}
		}
	}
	if a.Header.Rows != b.Header.Rows {
		add("header.rows", a.Header.Rows, b.Header.Rows)
	}
	if !slices.Equal(a.Header.Merges, b.Header.Merges) {
		add("header.merges", a.Header.Merges, b.Header.Merges)
	}
	if len(a.Rows.Indicators) != len(b.Rows.Indicators) {
		add("rows.indicators.count", len(a.Rows.Indicators), len(b.Rows.Indicators))
	}
	if len(a.Rules) != len(b.Rules) {
		add("rules.count", len(a.Rules), len(b.Rules))
	}
	return out
}

func validLevel(l string) bool { return l == port.LevelCommune || l == port.LevelProvince }

var deptRe = func(s string) bool {
	if len(s) < 2 || len(s) > 64 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '_' {
			return false
		}
	}
	return true
}

// blankDefinition là mẫu trống: A STT, B Khu vực, C Xã (cố định) và một cột nhập.
func blankDefinition(code, title string) port.FormDefinition {
	fixed := func(l, t, src, dt string) port.FormColumn {
		return port.FormColumn{Letter: l, Title: t, Kind: port.ColumnFixed, DType: dt, FixedSource: src}
	}
	return port.FormDefinition{
		Schema: port.FormSchema, Code: code, Title: title, Completeness: port.CompletenessComplete,
		Columns: []port.FormColumn{
			fixed("A", "STT", port.FixedRowNo, port.DTypeInteger), fixed("B", "Khu vực", port.FixedRegionName, port.DTypeText),
			fixed("C", "Xã, phường", port.FixedCommuneName, port.DTypeText),
			{Letter: "D", Title: "Chỉ tiêu 1", Kind: port.ColumnInput, DType: port.DTypeNumber},
		},
		Header: port.FormHeader{Rows: 1, Merges: []string{}, Cells: []port.HeaderCell{
			{Row: 1, Letter: "A", Text: "STT"}, {Row: 1, Letter: "B", Text: "Khu vực"}, {Row: 1, Letter: "C", Text: "Xã, phường"}, {Row: 1, Letter: "D", Text: "Chỉ tiêu 1"},
		}},
		Rows: port.FormRows{Kind: port.RowsPerCommune, Indicators: []port.Indicator{}},
		Totals: []port.TotalRowDef{
			{Kind: port.TotalRegion, Label: "Cộng {region}", LabelLetter: "B"}, {Kind: port.TotalProvince, Label: "Toàn tỉnh", LabelLetter: "B"},
		},
		PeriodBlocks: []port.PeriodBlock{}, Groups: []port.ColumnGroup{}, Rules: []port.FormRule{}, Pending: []port.PendingItem{},
		Source: &port.FormSource{Kind: "designer"},
	}
}

// CreateFormInput là yêu cầu tạo mẫu mới (trống, nhân bản hoặc kèm định nghĩa).
type CreateFormInput struct {
	Code           string
	Name           string
	DepartmentCode string
	EntryLevel     string
	Definition     *port.FormDefinition
	CloneVersionID *uuid.UUID // nhân bản từ phiên bản của mẫu khác
}

// CreateForm tạo mẫu và phiên bản nháp v1 (FN-AD-04.06 "Tạo mẫu mới").
func (s *Service) CreateForm(ctx context.Context, in CreateFormInput, idem string) (store.FormTemplate, store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormTemplate{}, store.FormVersion{}, err
	}
	in.Code, in.Name = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
	switch {
	case !form.ValidCode(in.Code):
		return store.FormTemplate{}, store.FormVersion{}, problem.Validation.WithField("code", "chữ hoa, số, . _ -; 2–32 ký tự (vd T01)")
	case len([]rune(in.Name)) < 1 || len([]rune(in.Name)) > 300:
		return store.FormTemplate{}, store.FormVersion{}, problem.Validation.WithField("name", "bắt buộc, tối đa 300 ký tự")
	case !deptRe(in.DepartmentCode):
		return store.FormTemplate{}, store.FormVersion{}, problem.Validation.WithField("department_code", "mã phòng ban chữ thường, số, gạch dưới")
	case !validLevel(in.EntryLevel):
		return store.FormTemplate{}, store.FormVersion{}, problem.Validation.WithField("entry_level", "commune | province")
	}
	now := s.now()
	t := store.FormTemplate{ID: ids.New(), Code: in.Code, Name: in.Name, DepartmentCode: in.DepartmentCode, EntryLevel: in.EntryLevel, Status: "active", CreatedBy: p.ActorRef, CreatedAt: now, UpdatedAt: now}
	v := store.FormVersion{ID: ids.New(), TemplateID: t.ID, Status: port.FormDraft, Origin: "blank", CreatedBy: p.ActorRef, CreatedAt: now, UpdatedBy: p.ActorRef, UpdatedAt: now}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		if dup, err := store.FormTemplateByCode(ctx, tx, in.DepartmentCode, in.Code); err != nil {
			return err
		} else if dup != nil {
			return ErrFormCodeTaken
		}
		switch {
		case in.CloneVersionID != nil:
			src, err := store.FormVersionByID(ctx, tx, *in.CloneVersionID, false)
			if err != nil {
				return err
			}
			if src == nil {
				return ErrFormVersionNotFound
			}
			v.Def, v.Origin, v.OriginVersionID = form.Clone(src.Def), "clone", &src.ID
		case in.Definition != nil:
			v.Def = form.Clone(*in.Definition)
		default:
			v.Def = blankDefinition(in.Code, in.Name)
		}
		v.Def.Code = in.Code
		if v.Def.Title == "" {
			v.Def.Title = in.Name
		}
		if err := form.Validate(v.Def); err != nil {
			return defProblem(err)
		}
		if err := store.InsertFormTemplate(ctx, tx, p.TenantID, t); err != nil {
			return err
		}
		if err := store.InsertFormVersion(ctx, tx, p.TenantID, &v); err != nil {
			return err
		}
		if err := s.formEvent(ctx, tx, p.TenantID, p.ActorRef, t.ID, nil, "template_created", []Change{{"code", nil, t.Code}, {"department_code", nil, t.DepartmentCode}, {"entry_level", nil, t.EntryLevel}}, nil, idemOr(idem, "t")); err != nil {
			return err
		}
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, t.ID, &v, "draft_created", []Change{{"origin", nil, v.Origin}}, nil, idemOr(idem, "v"))
	})
	return t, v, err
}

// ListForms trả mẫu theo phòng ban/cấp/trạng thái (Admin).
func (s *Service) ListForms(ctx context.Context, f store.FormTemplateFilter, reportType string) ([]FormSummary, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return nil, err
	}
	var out []FormSummary
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		ts, err := store.ListFormTemplates(ctx, tx, f)
		if err != nil {
			return err
		}
		asg, err := store.ListAssignments(ctx, tx, store.AssignmentFilter{Department: f.Department, Level: f.Level, ActiveOnly: true})
		if err != nil {
			return err
		}
		for _, t := range ts {
			sum := FormSummary{Template: t, ReportTypes: []string{}}
			for _, a := range asg {
				if a.TemplateID == t.ID {
					sum.ReportTypes = append(sum.ReportTypes, a.ReportType)
				}
			}
			if reportType != "" && !slices.Contains(sum.ReportTypes, reportType) {
				continue
			}
			vs, err := store.FormVersions(ctx, tx, t.ID)
			if err != nil {
				return err
			}
			for i := range vs {
				v := vs[i]
				if (v.Status == port.FormDraft || v.Status == port.FormProposed) && sum.Open == nil {
					sum.Open = &v
				}
				if v.Status == port.FormPublished && (sum.Published == nil || v.EffectiveFrom.After(*sum.Published.EffectiveFrom)) {
					sum.Published = &v
				}
			}
			out = append(out, sum)
		}
		return nil
	})
	return out, err
}

// GetForm trả mẫu kèm mọi phiên bản (lịch sử phiên bản) và gán.
func (s *Service) GetForm(ctx context.Context, id uuid.UUID) (FormDetail, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return FormDetail{}, err
	}
	var d FormDetail
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.FormTemplateByID(ctx, tx, id, false)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrFormNotFound
		}
		d.Template = *t
		if d.Versions, err = store.FormVersions(ctx, tx, id); err != nil {
			return err
		}
		d.Assignments, err = store.ListAssignments(ctx, tx, store.AssignmentFilter{TemplateID: &id})
		return err
	})
	return d, err
}

// loadVersion đọc (mẫu, phiên bản số n) trong giao dịch.
func loadVersion(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, n int, lock bool) (*store.FormTemplate, *store.FormVersion, error) {
	t, err := store.FormTemplateByID(ctx, tx, templateID, lock)
	if err != nil {
		return nil, nil, err
	}
	if t == nil {
		return nil, nil, ErrFormNotFound
	}
	v, err := store.FormVersionByNumber(ctx, tx, templateID, n, lock)
	if err != nil {
		return nil, nil, err
	}
	if v == nil {
		return nil, nil, ErrFormVersionNotFound
	}
	return t, v, nil
}

// GetFormVersion trả một phiên bản kèm định nghĩa (Admin: mọi trạng thái).
func (s *Service) GetFormVersion(ctx context.Context, templateID uuid.UUID, n int) (store.FormTemplate, store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormTemplate{}, store.FormVersion{}, err
	}
	var t *store.FormTemplate
	var v *store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		t, v, e = loadVersion(ctx, tx, templateID, n, false)
		return e
	})
	if err != nil {
		return store.FormTemplate{}, store.FormVersion{}, err
	}
	return *t, *v, nil
}

// NewFormVersion tạo nháp mới từ một phiên bản (mặc định: phiên bản mới nhất). restore =
// "khôi phục phiên bản cũ thành nháp"; phiên bản nguồn giữ nguyên.
func (s *Service) NewFormVersion(ctx context.Context, templateID uuid.UUID, from *int, restore bool, idem string) (store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormVersion{}, err
	}
	now := s.now()
	v := store.FormVersion{ID: ids.New(), TemplateID: templateID, Status: port.FormDraft, Origin: "clone", CreatedBy: p.ActorRef, CreatedAt: now, UpdatedBy: p.ActorRef, UpdatedAt: now}
	if restore {
		v.Origin = "restore"
	}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.FormTemplateByID(ctx, tx, templateID, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrFormNotFound
		}
		if t.Status != "active" {
			return ErrFormRetired
		}
		if open, err := store.OpenFormVersion(ctx, tx, templateID); err != nil {
			return err
		} else if open != nil {
			return ErrFormDraftExists.WithDetail(fmt.Sprintf("phiên bản v%d (%s)", open.Version, open.Status))
		}
		vs, err := store.FormVersions(ctx, tx, templateID)
		if err != nil {
			return err
		}
		var src *store.FormVersion
		for i := range vs {
			if from == nil && i == 0 || from != nil && vs[i].Version == *from {
				src = &vs[i]
				break
			}
		}
		if src == nil {
			return ErrFormVersionNotFound
		}
		v.Def, v.OriginVersionID = form.Clone(src.Def), &src.ID
		v.Def.Source = &port.FormSource{Kind: "designer"}
		if err := store.InsertFormVersion(ctx, tx, p.TenantID, &v); err != nil {
			return err
		}
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, templateID, &v, "draft_created", []Change{{"origin", nil, v.Origin}, {"from_version", nil, src.Version}}, nil, idemOr(idem, ""))
	})
	return v, err
}

// DraftInput là nội dung lưu nháp (nil = giữ).
type DraftInput struct {
	Definition     *port.FormDefinition
	EffectiveFrom  *time.Time
	EffectiveLabel *string
}

// SaveFormDraft lưu định nghĩa/áp dụng từ kỳ của nháp ("Lưu nháp").
func (s *Service) SaveFormDraft(ctx context.Context, templateID uuid.UUID, n int, in DraftInput, idem string) (store.FormVersion, error) {
	return s.mutateDraft(ctx, templateID, n, idem, "draft_saved", func(v *store.FormVersion) (any, error) {
		old := v.Def
		var ch []Change
		if in.Definition != nil {
			d := form.Clone(*in.Definition)
			d.Code = old.Code
			if err := form.Validate(d); err != nil {
				return nil, defProblem(err)
			}
			v.Def = d
			ch = diffDef(old, d)
		}
		if in.EffectiveFrom != nil {
			ch = append(ch, Change{"effective_from", datePtrString(v.EffectiveFrom), in.EffectiveFrom.Format("2006-01-02")})
			v.EffectiveFrom = in.EffectiveFrom
		}
		if in.EffectiveLabel != nil {
			l := strings.TrimSpace(*in.EffectiveLabel)
			if n := len([]rune(l)); n < 1 || n > 60 {
				return nil, problem.Validation.WithField("effective_label", "1–60 ký tự, vd “Tuần 37/2026”")
			}
			v.EffectiveLabel = &l
		}
		return ch, nil
	})
}

func datePtrString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format("2006-01-02")
}

// ApplyFormOps áp lô thao tác lưới của trình thiết kế lên nháp.
func (s *Service) ApplyFormOps(ctx context.Context, templateID uuid.UUID, n int, ops []form.Op, idem string) (store.FormVersion, error) {
	if len(ops) == 0 || len(ops) > 500 {
		return store.FormVersion{}, problem.Validation.WithField("ops", "1–500 thao tác")
	}
	return s.mutateDraft(ctx, templateID, n, idem, "draft_ops_applied", func(v *store.FormVersion) (any, error) {
		d, err := form.Apply(v.Def, ops)
		if err != nil {
			return nil, defProblem(err)
		}
		ch := append(diffDef(v.Def, d), Change{"ops", nil, ops})
		v.Def = d
		return ch, nil
	})
}

func (s *Service) mutateDraft(ctx context.Context, templateID uuid.UUID, n int, idem, action string, fn func(*store.FormVersion) (any, error)) (store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormVersion{}, err
	}
	var out store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, v, err := loadVersion(ctx, tx, templateID, n, true)
		if err != nil {
			return err
		}
		if t.Status != "active" {
			return ErrFormRetired
		}
		if v.Status != port.FormDraft {
			return ErrFormVersionLocked.WithDetail("phiên bản v" + fmt.Sprint(v.Version) + " đang ở trạng thái " + v.Status)
		}
		ch, err := fn(v)
		if err != nil {
			return err
		}
		v.UpdatedBy, v.UpdatedAt = p.ActorRef, s.now()
		if err := store.SaveFormDraft(ctx, tx, v); err != nil {
			return err
		}
		out = *v
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, templateID, v, action, ch, nil, idemOr(idem, ""))
	})
	return out, err
}

// ProposeInput là "Phát hành phiên bản" bước 1: đề nghị kèm áp dụng từ kỳ.
type ProposeInput struct {
	EffectiveFrom  *time.Time
	EffectiveLabel *string
}

// ProposeFormVersion đề nghị phát hành nháp: khóa nội dung, chờ người khác duyệt (R-56).
// Mẫu chưa gán loại báo cáo → không phát hành được.
func (s *Service) ProposeFormVersion(ctx context.Context, templateID uuid.UUID, n int, in ProposeInput, idem string) (store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormVersion{}, err
	}
	var out store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, v, err := loadVersion(ctx, tx, templateID, n, true)
		if err != nil {
			return err
		}
		if t.Status != "active" {
			return ErrFormRetired
		}
		if v.Status != port.FormDraft {
			return ErrFormVersionLocked
		}
		if err := form.Validate(v.Def); err != nil {
			return defProblem(err)
		}
		if in.EffectiveFrom != nil {
			v.EffectiveFrom = in.EffectiveFrom
		}
		if in.EffectiveLabel != nil {
			l := strings.TrimSpace(*in.EffectiveLabel)
			v.EffectiveLabel = &l
		}
		if err := s.checkPublishable(ctx, tx, t, v); err != nil {
			return err
		}
		v.UpdatedBy, v.UpdatedAt = p.ActorRef, s.now()
		if err := store.SaveFormDraft(ctx, tx, v); err != nil {
			return err
		}
		now := s.now()
		v.Status, v.ProposedBy, v.ProposedAt = port.FormProposed, &p.ActorRef, &now
		if err := store.SetFormVersionStatus(ctx, tx, *v); err != nil {
			return err
		}
		out = *v
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, templateID, v, "proposed",
			[]Change{{"status", port.FormDraft, port.FormProposed}, {"effective_from", nil, datePtrString(v.EffectiveFrom)}}, nil, idemOr(idem, ""))
	})
	return out, err
}

// checkPublishable: đã gán loại báo cáo; áp dụng từ kỳ có và không sớm hơn phiên bản đã phát hành.
func (s *Service) checkPublishable(ctx context.Context, tx pgx.Tx, t *store.FormTemplate, v *store.FormVersion) error {
	asg, err := store.ListAssignments(ctx, tx, store.AssignmentFilter{TemplateID: &t.ID, ActiveOnly: true})
	if err != nil {
		return err
	}
	if len(asg) == 0 {
		return ErrFormNotAssigned
	}
	if v.EffectiveFrom == nil {
		return ErrFormEffective.WithField("effective_from", "bắt buộc: ngày bắt đầu kỳ áp dụng (vd 2026-09-07 cho Tuần 37/2026)")
	}
	last, err := store.LatestPublishedEffective(ctx, tx, t.ID)
	if err != nil {
		return err
	}
	if last != nil && v.EffectiveFrom.Before(last.UTC().Truncate(24*time.Hour)) {
		return ErrFormEffective.WithDetail("kỳ đã có phiên bản " + last.Format("2006-01-02") + " — phiên bản mới chỉ áp dụng từ kỳ sau")
	}
	return nil
}

// WithdrawFormProposal rút đề nghị: phiên bản về nháp để sửa tiếp.
func (s *Service) WithdrawFormProposal(ctx context.Context, templateID uuid.UUID, n int, why string, idem string) (store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormVersion{}, err
	}
	r, err := reason(why, false)
	if err != nil {
		return store.FormVersion{}, err
	}
	var out store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		_, v, err := loadVersion(ctx, tx, templateID, n, true)
		if err != nil {
			return err
		}
		if v.Status != port.FormProposed {
			return ErrFormNotProposed
		}
		prev := v.ProposedBy
		v.Status, v.ProposedBy, v.ProposedAt = port.FormDraft, nil, nil
		if err := store.SetFormVersionStatus(ctx, tx, *v); err != nil {
			return err
		}
		out = *v
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, templateID, v, "proposal_withdrawn", []Change{{"status", port.FormProposed, port.FormDraft}, {"proposed_by", prev, nil}}, r, idemOr(idem, ""))
	})
	return out, err
}

// PublishFormVersion phát hành phiên bản đã đề nghị. Người phát hành ≠ người đề nghị và ≠
// người sửa cuối (R-56: app/ + CHECK DB). Từ đây phiên bản bất biến.
func (s *Service) PublishFormVersion(ctx context.Context, templateID uuid.UUID, n int, idem string) (store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormVersion{}, err
	}
	var out store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, v, err := loadVersion(ctx, tx, templateID, n, true)
		if err != nil {
			return err
		}
		if v.Status != port.FormProposed {
			return ErrFormNotProposed
		}
		if *v.ProposedBy == p.ActorRef || v.UpdatedBy == p.ActorRef {
			return problem.SelfApproval.WithCause(authz.ErrSelfApproval).WithDetail("người phát hành phải khác người đề nghị và người sửa cuối (R-56)")
		}
		if err := s.checkPublishable(ctx, tx, t, v); err != nil {
			return err
		}
		now := s.now()
		v.Status, v.PublishedBy, v.PublishedAt = port.FormPublished, &p.ActorRef, &now
		if err := store.SetFormVersionStatus(ctx, tx, *v); err != nil {
			return err
		}
		out = *v
		if err := s.formEvent(ctx, tx, p.TenantID, p.ActorRef, templateID, v, "published", []Change{{"status", port.FormProposed, port.FormPublished}}, nil, idemOr(idem, "")); err != nil {
			return err
		}
		return outbox.Enqueue(ctx, tx, outbox.Event{
			ID: ids.New(), TenantID: p.TenantID, Type: port.EventFormVersionPublished, SchemaVersion: 1, OccurredAt: now,
			TraceID: telemetry.TraceID(ctx), Producer: port.Producer,
			Payload: port.FormVersionPublishedV1{
				TemplateID: t.ID.String(), VersionID: v.ID.String(), Code: t.Code, Version: v.Version, DepartmentCode: t.DepartmentCode,
				EntryLevel: t.EntryLevel, EffectiveFrom: v.EffectiveFrom.Format("2006-01-02"), EffectiveLabel: v.EffectiveLabel,
				ProposedBy: v.ProposedBy.String(), PublishedBy: p.ActorRef.String(), PublishedAt: ts(now),
			},
		})
	})
	return out, err
}

// RetireFormVersion ngừng dùng một phiên bản đã phát hành (kèm lý do). Công việc đã ghim
// phiên bản này giữ nguyên (P4).
func (s *Service) RetireFormVersion(ctx context.Context, templateID uuid.UUID, n int, why string, idem string) (store.FormVersion, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormVersion{}, err
	}
	r, err := reason(why, true)
	if err != nil {
		return store.FormVersion{}, err
	}
	var out store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		_, v, err := loadVersion(ctx, tx, templateID, n, true)
		if err != nil {
			return err
		}
		if v.Status != port.FormPublished {
			return ErrFormNotPublished
		}
		now := s.now()
		v.Status, v.RetiredBy, v.RetiredAt, v.RetireReason = port.FormRetired, &p.ActorRef, &now, r
		if err := store.SetFormVersionStatus(ctx, tx, *v); err != nil {
			return err
		}
		out = *v
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, templateID, v, "version_retired", []Change{{"status", port.FormPublished, port.FormRetired}}, r, idemOr(idem, ""))
	})
	return out, err
}

// RetireForm ngừng dùng cả mẫu (không xóa — RB-1): không tạo nháp, không phát hành tiếp.
func (s *Service) RetireForm(ctx context.Context, templateID uuid.UUID, why string, idem string) (store.FormTemplate, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.FormTemplate{}, err
	}
	r, err := reason(why, true)
	if err != nil {
		return store.FormTemplate{}, err
	}
	var out store.FormTemplate
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.FormTemplateByID(ctx, tx, templateID, true)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrFormNotFound
		}
		if t.Status != "active" {
			return ErrFormRetired
		}
		t.Status, t.RetiredReason, t.UpdatedAt = "retired", r, s.now()
		if err := store.UpdateFormTemplate(ctx, tx, *t); err != nil {
			return err
		}
		out = *t
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, templateID, nil, "template_retired", []Change{{"status", "active", "retired"}}, r, idemOr(idem, ""))
	})
	return out, err
}

// AssignForm gán mẫu vào loại báo cáo của phòng ban × cấp của mẫu (FN-AD-04.06 B6).
func (s *Service) AssignForm(ctx context.Context, templateID uuid.UUID, reportType, idem string) (store.Assignment, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.Assignment{}, err
	}
	if !slices.Contains(port.ReportTypes, reportType) {
		return store.Assignment{}, problem.Validation.WithField("report_type", strings.Join(port.ReportTypes, " | "))
	}
	var out store.Assignment
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = s.assign(ctx, tx, p, templateID, reportType, idemOr(idem, ""))
		return e
	})
	return out, err
}

func (s *Service) assign(ctx context.Context, tx pgx.Tx, p authz.Principal, templateID uuid.UUID, reportType, idem string) (store.Assignment, error) {
	t, err := store.FormTemplateByID(ctx, tx, templateID, true)
	if err != nil {
		return store.Assignment{}, err
	}
	if t == nil {
		return store.Assignment{}, ErrFormNotFound
	}
	if t.Status != "active" {
		return store.Assignment{}, ErrFormRetired
	}
	cur, err := store.ListAssignments(ctx, tx, store.AssignmentFilter{TemplateID: &templateID, ReportType: reportType, ActiveOnly: true})
	if err != nil {
		return store.Assignment{}, err
	}
	if len(cur) > 0 {
		return store.Assignment{}, ErrFormAssigned
	}
	now := s.now()
	a := store.Assignment{ID: ids.New(), TemplateID: t.ID, DepartmentCode: t.DepartmentCode, EntryLevel: t.EntryLevel, ReportType: reportType,
		Status: "active", AssignedBy: p.ActorRef, AssignedAt: now, LastEventID: ids.New()}
	if err := store.InsertAssignmentEvent(ctx, tx, p.TenantID, store.AssignmentEvent{ID: a.LastEventID, AssignmentID: a.ID, Action: "assigned",
		TemplateID: t.ID, DepartmentCode: t.DepartmentCode, EntryLevel: t.EntryLevel, ReportType: reportType, EventTime: now, ActorRef: p.ActorRef, IdemKey: idem}); err != nil {
		return store.Assignment{}, err
	}
	if err := store.PutAssignment(ctx, tx, p.TenantID, a); err != nil {
		return store.Assignment{}, err
	}
	return a, s.formEvent(ctx, tx, p.TenantID, p.ActorRef, t.ID, nil, "assigned", []Change{{"report_type", nil, reportType}}, nil, idem+":log")
}

// RevokeAssignment bỏ gán (kèm lý do); công việc đã phát hành giữ nguyên.
func (s *Service) RevokeAssignment(ctx context.Context, id uuid.UUID, why, idem string) (store.Assignment, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return store.Assignment{}, err
	}
	r, err := reason(why, true)
	if err != nil {
		return store.Assignment{}, err
	}
	var out store.Assignment
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		a, err := store.AssignmentByID(ctx, tx, id, true)
		if err != nil {
			return err
		}
		if a == nil || a.Status != "active" {
			return ErrAssignmentNotFound
		}
		now := s.now()
		ev := store.AssignmentEvent{ID: ids.New(), AssignmentID: a.ID, Action: "revoked", TemplateID: a.TemplateID, DepartmentCode: a.DepartmentCode,
			EntryLevel: a.EntryLevel, ReportType: a.ReportType, Reason: r, EventTime: now, ActorRef: p.ActorRef, IdemKey: idemOr(idem, "")}
		if err := store.InsertAssignmentEvent(ctx, tx, p.TenantID, ev); err != nil {
			return err
		}
		a.Status, a.RevokedBy, a.RevokedAt, a.RevokeReason, a.LastEventID = "revoked", &p.ActorRef, &now, r, ev.ID
		if err := store.PutAssignment(ctx, tx, p.TenantID, *a); err != nil {
			return err
		}
		out = *a
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, a.TemplateID, nil, "assignment_revoked", []Change{{"report_type", a.ReportType, nil}}, r, ev.IdemKey+":log")
	})
	return out, err
}

// ListAssignments trả gán theo bộ lọc (Admin).
func (s *Service) ListFormAssignments(ctx context.Context, f store.AssignmentFilter) ([]store.Assignment, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return nil, err
	}
	var out []store.Assignment
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = store.ListAssignments(ctx, tx, f)
		return e
	})
	return out, err
}

// FormLog trả nhật ký thiết kế của mẫu (mới nhất trước, con trỏ theo id).
func (s *Service) FormLog(ctx context.Context, templateID uuid.UUID, before *uuid.UUID, limit int) ([]store.FormEvent, *uuid.UUID, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return nil, nil, err
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	var out []store.FormEvent
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		t, err := store.FormTemplateByID(ctx, tx, templateID, false)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrFormNotFound
		}
		out, err = store.FormEvents(ctx, tx, templateID, before, limit+1)
		return err
	})
	if err != nil {
		return nil, nil, err
	}
	var next *uuid.UUID
	if len(out) > limit {
		out = out[:limit]
		id := out[limit-1].ID
		next = &id
	}
	return out, next, nil
}

// ImportInput là tham số nạp Excel. TemplateID có → nháp mới của mẫu đó; không → tạo mẫu mới.
type ImportInput struct {
	TemplateID     *uuid.UUID
	Code           string
	Name           string
	DepartmentCode string
	EntryLevel     string
	FileName       string
	Options        formimport.Options
}

// ImportResult là kết quả nạp.
type ImportResult struct {
	ImportID uuid.UUID
	Template store.FormTemplate
	Version  store.FormVersion
	Report   formimport.Report
}

// ImportForm nạp một sheet Excel thành phiên bản nháp (mẫu mới hoặc nháp mới của mẫu có sẵn).
// Tệp hỏng → reports.form_import_invalid (lần nạp lỗi vẫn ghi lại).
func (s *Service) ImportForm(ctx context.Context, data []byte, in ImportInput, idem string) (ImportResult, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return ImportResult{}, err
	}
	if len(data) == 0 || len(data) > formimport.MaxBytes {
		return ImportResult{}, ErrImportInvalid.WithDetail(fmt.Sprintf("tệp rỗng hoặc vượt %d MB", formimport.MaxBytes>>20))
	}
	sum := sha256.Sum256(data)
	fileName := strings.TrimSpace(in.FileName)
	if fileName == "" {
		fileName = "mau.xlsx"
	}
	if r := []rune(fileName); len(r) > 255 {
		fileName = string(r[:255])
	}
	res := ImportResult{ImportID: ids.New()}
	now := s.now()
	var failure error
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var tpl *store.FormTemplate
		if in.TemplateID != nil {
			t, err := store.FormTemplateByID(ctx, tx, *in.TemplateID, true)
			if err != nil {
				return err
			}
			if t == nil {
				return ErrFormNotFound
			}
			if t.Status != "active" {
				return ErrFormRetired
			}
			if open, err := store.OpenFormVersion(ctx, tx, t.ID); err != nil {
				return err
			} else if open != nil {
				return ErrFormDraftExists.WithDetail(fmt.Sprintf("phiên bản v%d (%s)", open.Version, open.Status))
			}
			tpl = t
			in.Code, in.Name = t.Code, t.Name
		} else {
			in.Code, in.Name = strings.TrimSpace(in.Code), strings.TrimSpace(in.Name)
			switch {
			case !form.ValidCode(in.Code):
				return problem.Validation.WithField("code", "chữ hoa, số, . _ -; 2–32 ký tự")
			case in.Name == "" || len([]rune(in.Name)) > 300:
				return problem.Validation.WithField("name", "bắt buộc, tối đa 300 ký tự")
			case !deptRe(in.DepartmentCode):
				return problem.Validation.WithField("department_code", "mã phòng ban chữ thường, số, gạch dưới")
			case !validLevel(in.EntryLevel):
				return problem.Validation.WithField("entry_level", "commune | province")
			}
			if dup, err := store.FormTemplateByCode(ctx, tx, in.DepartmentCode, in.Code); err != nil {
				return err
			} else if dup != nil {
				return ErrFormCodeTaken
			}
		}
		opts := in.Options
		opts.Code, opts.Title, opts.FileName = in.Code, in.Name, fileName
		def, rep, perr := formimport.Import(bytes.NewReader(data), opts)
		res.Report = rep
		repJSON, _ := json.Marshal(rep)
		imp := store.FormImport{ID: res.ImportID, FileName: fileName, FileSHA256: hex.EncodeToString(sum[:]), SizeBytes: int64(len(data)),
			SheetName: rep.Sheet, Report: repJSON, CreatedBy: p.ActorRef, CreatedAt: now}
		if imp.SheetName == "" {
			imp.SheetName = opts.Sheet
		}
		if perr != nil {
			code := ErrImportInvalid.Code
			var ve *form.ValidationError
			if errors.As(perr, &ve) {
				code = ve.Code()
				failure = defProblem(perr)
			} else {
				failure = ErrImportInvalid.WithDetail(strings.TrimPrefix(perr.Error(), formimport.ErrInvalid.Error()+": "))
			}
			imp.Status, imp.ErrorCode = "failed", &code
			return store.InsertFormImport(ctx, tx, p.TenantID, imp) // lần nạp lỗi vẫn ghi
		}
		def.Source.SHA256 = imp.FileSHA256
		if tpl == nil {
			tpl = &store.FormTemplate{ID: ids.New(), Code: in.Code, Name: in.Name, DepartmentCode: in.DepartmentCode, EntryLevel: in.EntryLevel, Status: "active", CreatedBy: p.ActorRef, CreatedAt: now, UpdatedAt: now}
			if err := store.InsertFormTemplate(ctx, tx, p.TenantID, *tpl); err != nil {
				return err
			}
			if err := s.formEvent(ctx, tx, p.TenantID, p.ActorRef, tpl.ID, nil, "template_created", []Change{{"code", nil, tpl.Code}, {"origin", nil, "import"}}, nil, idemOr(idem, "t")); err != nil {
				return err
			}
		}
		if s.Store != nil {
			key := fmt.Sprintf("reports/%s/form-imports/%s.xlsx", p.TenantID, res.ImportID)
			if _, err := s.Store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), imp.FileSHA256, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"); err != nil {
				s.log().Warn("không lưu được tệp nạp mẫu", "event", "reports.form_import.store_failed", "import_id", res.ImportID)
			} else {
				imp.ObjectKey = &key
			}
		}
		v := store.FormVersion{ID: ids.New(), TemplateID: tpl.ID, Status: port.FormDraft, Def: def, Origin: "import", ImportID: &res.ImportID,
			CreatedBy: p.ActorRef, CreatedAt: now, UpdatedBy: p.ActorRef, UpdatedAt: now}
		if err := store.InsertFormVersion(ctx, tx, p.TenantID, &v); err != nil {
			return err
		}
		imp.Status, imp.TemplateID, imp.VersionID = "parsed", &tpl.ID, &v.ID
		if err := store.InsertFormImport(ctx, tx, p.TenantID, imp); err != nil {
			return err
		}
		res.Template, res.Version = *tpl, v
		return s.formEvent(ctx, tx, p.TenantID, p.ActorRef, tpl.ID, &v, "imported",
			[]Change{{"file_sha256", nil, imp.FileSHA256}, {"sheet", nil, rep.Sheet}, {"columns", nil, rep.Columns}, {"warnings", nil, len(rep.Warnings)}}, nil, idemOr(idem, "v"))
	})
	if err != nil {
		return ImportResult{}, err
	}
	if failure != nil {
		return res, failure
	}
	return res, nil
}

// SeedResult là kết quả khởi tạo bộ mẫu đã đăng ký.
type SeedResult struct {
	Created []string
	Skipped []string
}

// SeedForms creates draft v1 and assignments from trusted application registration.
// Existing codes are preserved; publication still requires a second person (R-56).
func (s *Service) SeedForms(ctx context.Context, dept, idem string) (SeedResult, error) {
	p, err := authz.Require(ctx, formAdmins...)
	if err != nil {
		return SeedResult{}, err
	}
	set, err := s.formSeedSet()
	if err != nil {
		return SeedResult{}, err
	}
	if dept == "" {
		dept = set.DefaultDepartment
	}
	if !deptRe(dept) {
		return SeedResult{}, problem.Validation.WithField("department_code", "mã phòng ban chữ thường, số, gạch dưới")
	}
	res := SeedResult{Created: []string{}, Skipped: []string{}}
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		for _, sd := range set.Templates {
			if dup, err := store.FormTemplateByCode(ctx, tx, dept, sd.Code); err != nil {
				return err
			} else if dup != nil {
				res.Skipped = append(res.Skipped, sd.Code)
				continue
			}
			now := s.now()
			t := store.FormTemplate{ID: ids.New(), Code: sd.Code, Name: sd.Name, DepartmentCode: dept, EntryLevel: sd.EntryLevel, Status: "active", CreatedBy: p.ActorRef, CreatedAt: now, UpdatedAt: now}
			if err := store.InsertFormTemplate(ctx, tx, p.TenantID, t); err != nil {
				return err
			}
			v := store.FormVersion{ID: ids.New(), TemplateID: t.ID, Status: port.FormDraft, Def: sd.Def, Origin: "seed", CreatedBy: p.ActorRef, CreatedAt: now, UpdatedBy: p.ActorRef, UpdatedAt: now}
			if err := store.InsertFormVersion(ctx, tx, p.TenantID, &v); err != nil {
				return err
			}
			if err := s.formEvent(ctx, tx, p.TenantID, p.ActorRef, t.ID, &v, "seeded",
				[]Change{{"seed", nil, set.Version}, {"completeness", nil, sd.Def.Completeness}, {"pending", nil, len(sd.Def.Pending)}}, nil, idemOr(idem, sd.Code)); err != nil {
				return err
			}
			if _, err := s.assign(ctx, tx, p, t.ID, sd.ReportType, idemOr(idem, sd.Code+":asg")); err != nil {
				return err
			}
			res.Created = append(res.Created, sd.Code)
		}
		return nil
	})
	return res, err
}

// ---------------------------------------------------------------------------
// Application API: chọn mẫu, layout, tính thử
// ---------------------------------------------------------------------------

// AvailableForm là một mẫu đã gán kèm phiên bản áp dụng.
type AvailableForm struct {
	Template   store.FormTemplate
	ReportType string
	Version    store.FormVersion
}

// AvailableForms trả mẫu đã gán cho (phòng ban, cấp, loại báo cáo) có phiên bản đã phát hành
// áp dụng tại ngày at (Cấp Tỉnh chọn khi phát hành biểu — FN-TI-02.02).
func (s *Service) AvailableForms(ctx context.Context, dept, level, reportType string, at time.Time) ([]AvailableForm, error) {
	p, viaKN, err := s.formReader(ctx)
	if err != nil {
		return nil, err
	}
	if !deptRe(dept) {
		return nil, problem.Validation.WithField("department", "bắt buộc")
	}
	if !formDeptAllows(viaKN, dept) {
		return []AvailableForm{}, nil
	}
	var out []AvailableForm
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		out, e = availableForms(ctx, tx, dept, level, reportType, at)
		return e
	})
	return out, err
}

func availableForms(ctx context.Context, tx pgx.Tx, dept, level, reportType string, at time.Time) ([]AvailableForm, error) {
	asg, err := store.ListAssignments(ctx, tx, store.AssignmentFilter{Department: dept, Level: level, ReportType: reportType, ActiveOnly: true})
	if err != nil {
		return nil, err
	}
	out := []AvailableForm{}
	for _, a := range asg {
		t, err := store.FormTemplateByID(ctx, tx, a.TemplateID, false)
		if err != nil {
			return nil, err
		}
		if t == nil || t.Status != "active" {
			continue
		}
		v, err := store.ApplicableFormVersion(ctx, tx, a.TemplateID, at)
		if err != nil {
			return nil, err
		}
		if v == nil {
			continue
		}
		out = append(out, AvailableForm{Template: *t, ReportType: a.ReportType, Version: *v})
	}
	slices.SortFunc(out, func(a, b AvailableForm) int { return strings.Compare(a.Template.Code, b.Template.Code) })
	return out, nil
}

// FormLayout trả phiên bản kèm định nghĩa đã đặt nhãn kỳ/vụ cho màn nhập/xem. Người đọc
// (không phải Admin) chỉ thấy phiên bản đã phát hành hoặc đã ngừng dùng.
func (s *Service) FormLayout(ctx context.Context, templateID uuid.UUID, n int, o form.LayoutOptions) (store.FormTemplate, store.FormVersion, error) {
	p, viaKN, err := s.formReader(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return store.FormTemplate{}, store.FormVersion{}, err
	}
	if len(o.PeriodLabels) > 200 {
		return store.FormTemplate{}, store.FormVersion{}, problem.Validation.WithField("period_labels", "tối đa 200 kỳ")
	}
	var t *store.FormTemplate
	var v *store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		t, v, e = loadVersion(ctx, tx, templateID, n, false)
		return e
	})
	if err != nil {
		return store.FormTemplate{}, store.FormVersion{}, err
	}
	if !hasAny(p, formAdmins) && v.Status != port.FormPublished && v.Status != port.FormRetired {
		return store.FormTemplate{}, store.FormVersion{}, ErrFormVersionNotFound
	}
	if !formDeptAllows(viaKN, t.DepartmentCode) {
		return store.FormTemplate{}, store.FormVersion{}, ErrFormNotFound
	}
	v.Def = form.Layout(v.Def, o)
	return *t, *v, nil
}

// EvaluateFormVersion kiểm và tính thử một lưới theo phiên bản (xem trước của trình thiết kế,
// tính tạm trên web/app — server là nguồn đúng).
func (s *Service) EvaluateFormVersion(ctx context.Context, versionID uuid.UUID, in port.EvalInput) (port.EvalResult, []port.TotalRowResult, error) {
	p, _, err := s.formReader(ctx, authz.RolePlatformAdmin)
	if err != nil {
		return port.EvalResult{}, nil, err
	}
	if len(in.Rows) > 5000 {
		return port.EvalResult{}, nil, problem.Validation.WithField("rows", "tối đa 5000 dòng")
	}
	if in.Mode != port.EvalDraft && in.Mode != port.EvalSubmit {
		return port.EvalResult{}, nil, problem.Validation.WithField("mode", "draft | submit")
	}
	var v *store.FormVersion
	err = s.tx(ctx, p.TenantID, func(tx pgx.Tx) error {
		var e error
		v, e = store.FormVersionByID(ctx, tx, versionID, false)
		return e
	})
	if err != nil {
		return port.EvalResult{}, nil, err
	}
	if v == nil || !hasAny(p, formAdmins) && v.Status != port.FormPublished && v.Status != port.FormRetired {
		return port.EvalResult{}, nil, ErrFormVersionNotFound
	}
	c, err := form.Compile(v.Def)
	if err != nil {
		return port.EvalResult{}, nil, defProblem(err)
	}
	r := c.Evaluate(in)
	return r, c.Totals(r.Rows), nil
}

// ---------------------------------------------------------------------------
// Cổng FormEngine cho P4 (tenant tường minh — job chạy ở worker)
// ---------------------------------------------------------------------------

// Forms là hiện thực port.FormEngine trên pool reports_app.
type Forms struct{ Svc *Service }

var _ port.FormEngine = Forms{}

func toPortVersion(t store.FormTemplate, v store.FormVersion, types []string) port.FormVersion {
	if types == nil {
		types = []string{}
	}
	return port.FormVersion{TemplateID: t.ID, VersionID: v.ID, Code: t.Code, Name: t.Name, DepartmentCode: t.DepartmentCode, EntryLevel: t.EntryLevel,
		Version: v.Version, Status: v.Status, EffectiveFrom: v.EffectiveFrom, EffectiveLabel: v.EffectiveLabel, ReportTypes: types, Definition: v.Def}
}

func reportTypesOf(ctx context.Context, tx pgx.Tx, templateID uuid.UUID) ([]string, error) {
	asg, err := store.ListAssignments(ctx, tx, store.AssignmentFilter{TemplateID: &templateID, ActiveOnly: true})
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, a := range asg {
		out = append(out, a.ReportType)
	}
	return out, nil
}

// FormVersion đọc một phiên bản theo id (mọi trạng thái).
func (f Forms) FormVersion(ctx context.Context, tenantID, versionID uuid.UUID) (port.FormVersion, error) {
	var out port.FormVersion
	err := tenant.WithTenantTx(ctx, f.Svc.DB, tenantID, func(tx pgx.Tx) error {
		v, err := store.FormVersionByID(ctx, tx, versionID, false)
		if err != nil {
			return err
		}
		if v == nil {
			return port.ErrFormNotFound
		}
		t, err := store.FormTemplateByID(ctx, tx, v.TemplateID, false)
		if err != nil {
			return err
		}
		types, err := reportTypesOf(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		out = toPortVersion(*t, *v, types)
		return nil
	})
	return out, err
}

// ApplicableVersion trả phiên bản đã phát hành mới nhất áp dụng tại periodStart.
func (f Forms) ApplicableVersion(ctx context.Context, tenantID, templateID uuid.UUID, periodStart time.Time) (port.FormVersion, error) {
	var out port.FormVersion
	err := tenant.WithTenantTx(ctx, f.Svc.DB, tenantID, func(tx pgx.Tx) error {
		t, err := store.FormTemplateByID(ctx, tx, templateID, false)
		if err != nil {
			return err
		}
		if t == nil {
			return port.ErrFormNotFound
		}
		v, err := store.ApplicableFormVersion(ctx, tx, templateID, periodStart)
		if err != nil {
			return err
		}
		if v == nil {
			return port.ErrFormNotFound
		}
		types, err := reportTypesOf(ctx, tx, t.ID)
		if err != nil {
			return err
		}
		out = toPortVersion(*t, *v, types)
		return nil
	})
	return out, err
}

// AssignedForms trả mẫu đang gán cho (phòng ban, cấp, loại báo cáo) kèm phiên bản áp dụng.
func (f Forms) AssignedForms(ctx context.Context, tenantID uuid.UUID, dept, level, reportType string, periodStart time.Time) ([]port.FormVersion, error) {
	var out []port.FormVersion
	err := tenant.WithTenantTx(ctx, f.Svc.DB, tenantID, func(tx pgx.Tx) error {
		av, err := availableForms(ctx, tx, dept, level, reportType, periodStart)
		if err != nil {
			return err
		}
		out = make([]port.FormVersion, 0, len(av))
		for _, a := range av {
			out = append(out, toPortVersion(a.Template, a.Version, []string{a.ReportType}))
		}
		return nil
	})
	return out, err
}

// Evaluate kiểm và tính một lưới (thuần).
func (Forms) Evaluate(def port.FormDefinition, in port.EvalInput) port.EvalResult {
	return form.Evaluate(def, in)
}

// Totals dựng dòng tổng (thuần).
func (Forms) Totals(def port.FormDefinition, rows []port.EvalRowResult) []port.TotalRowResult {
	return form.Totals(def, rows)
}
