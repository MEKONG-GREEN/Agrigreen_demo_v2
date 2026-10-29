package app

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	wfport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/workflow/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ---------------------------------------------------------------------------
// Biểu (phiên bản)
// ---------------------------------------------------------------------------

// TemplateInput là định nghĩa một phiên bản biểu.
type TemplateInput struct {
	Profile string
	Code    string
	Title   string
	Fields  []domain.Field
	Fixture bool
}

// DefineTemplate ghi phiên bản mới của biểu (profile, code) — phiên bản cũ giữ nguyên, kỳ đã phát hành giữ phiên
// bản của nó.
func (s *Service) DefineTemplate(ctx context.Context, in TemplateInput, idem string) (store.Template, error) {
	_, a, err := s.resolve(ctx, in.Profile)
	if err != nil {
		return store.Template{}, err
	}
	if !a.define {
		return store.Template{}, problem.Forbidden.WithDetail("định nghĩa biểu cần vai define_by của cấu hình")
	}
	in.Title = strings.TrimSpace(in.Title)
	if err := domain.ValidateTemplate(in.Code, in.Title, in.Fields); err != nil {
		var te domain.ErrTemplate
		if errorsAs(err, &te) {
			return store.Template{}, port.ErrInvalidTemplate.WithField(te.Field, te.Msg)
		}
		return store.Template{}, port.ErrInvalidTemplate.WithDetail(err.Error())
	}
	if !a.allowsTemplate(in.Code) {
		return store.Template{}, problem.Forbidden.WithDetail("biểu ngoài phạm vi")
	}
	t := store.Template{ID: ids.New(), Profile: in.Profile, Code: in.Code, Title: in.Title, Fields: in.Fields, Fixture: in.Fixture,
		ActorRef: a.ref, EventTime: s.Clock.Now().UTC()}
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		return store.InsertTemplate(ctx, tx, a.tenant, &t, idemOr(idem, "template:"+in.Code))
	})
	return t, classify(err)
}

// InstallFixtures ghi các biểu mẫu thử (fixture = true) cho profile — cơ chế chạy được khi chưa có biểu thật (Q-01).
func (s *Service) InstallFixtures(ctx context.Context, profile, idem string) ([]store.Template, error) {
	var out []store.Template
	for _, f := range domain.Fixtures() {
		t, err := s.DefineTemplate(ctx, TemplateInput{Profile: profile, Code: f.Code, Title: f.Title, Fields: f.Fields, Fixture: true}, idemOr(idem, f.Code))
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// Templates trả phiên bản mới nhất của mỗi biểu của profile.
func (s *Service) Templates(ctx context.Context, profile string) ([]store.Template, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	var out []store.Template
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		var e error
		out, e = store.LatestTemplates(ctx, tx, profile)
		return e
	})
	out = slices.DeleteFunc(out, func(t store.Template) bool { return !a.allowsTemplate(t.Code) })
	return out, err
}

// Template đọc một phiên bản biểu.
func (s *Service) Template(ctx context.Context, profile string, id uuid.UUID) (store.Template, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return store.Template{}, err
	}
	var t *store.Template
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		var e error
		t, e = store.TemplateByID(ctx, tx, id)
		return e
	})
	if err != nil {
		return store.Template{}, err
	}
	if t == nil || t.Profile != profile || !a.allowsTemplate(t.Code) {
		return store.Template{}, port.ErrTemplateNotFound
	}
	return *t, nil
}

// ---------------------------------------------------------------------------
// Kỳ
// ---------------------------------------------------------------------------

// PublishInput là yêu cầu phát hành kỳ.
type PublishInput struct {
	Profile    string
	TemplateID uuid.UUID
	Label      string
	DueAt      time.Time
	Units      []string // đơn vị nhận (cấp lá), thuộc phạm vi người phát hành
	// W11.05: loại đơn vị lá (rỗng = loại mặc định của profile), mã xã cha song song với Units (cơ sở/hộ),
	// giới hạn số lần gửi (nil = mặc định của profile, 0 = không giới hạn), khóa kỳ theo lịch kỳ hệ thống.
	UnitKind       string
	UnitParents    []string
	MaxSubmissions *int
	Period         *PeriodKey
}

// PeriodKey là khóa kỳ theo lịch kỳ hệ thống (W11.03 reportcycle/domain.Span: Key, From, To; thẻ quy tắc Tag()).
type PeriodKey struct {
	Kind, Key string
	From, To  *time.Time
	Rule      string
}

// UnitView là trạng thái bài nộp của một đơn vị trong kỳ.
type UnitView struct {
	Unit       string
	Parent     string // đơn vị cha dùng lọc phạm vi (cơ sở/hộ → mã xã); = Unit với xã/phòng ban
	Status     string // SubmissionStatus, hoặc trạng thái lúc chốt (Không nộp, Đã chốt – chưa điều chỉnh, Chờ mở kỳ)
	Submission *store.Submission
	Review     *domain.Review
	Held       *store.Held // bài giữ cuối ("Chờ mở kỳ"), nếu có
	Meta       *store.Meta // W11.42: thông tin phiên bản cuối (không phát sinh, ghi chú, giải trình, nộp trễ)
}

// PeriodView là kỳ kèm trạng thái chiếu từ sổ.
type PeriodView struct {
	Period    store.Period
	Spec      domain.Spec
	State     domain.PeriodState
	Events    []domain.Event
	Units     []UnitView
	Snapshots []store.Snapshot
}

// Publish phát hành kỳ (cấp publish_by): sổ period + sự kiện published cùng giao dịch.
func (s *Service) Publish(ctx context.Context, in PublishInput, idem string) (PeriodView, error) {
	prof, a, err := s.resolve(ctx, in.Profile)
	if err != nil {
		return PeriodView{}, err
	}
	if err := a.need(prof.PublishBy, "phát hành kỳ"); err != nil {
		return PeriodView{}, err
	}
	in.Label = strings.TrimSpace(in.Label)
	if n := len([]rune(in.Label)); n < 1 || n > 200 {
		return PeriodView{}, problem.Validation.WithField("label", "1–200 ký tự")
	}
	if in.DueAt.IsZero() {
		return PeriodView{}, problem.Validation.WithField("due_at", "bắt buộc")
	}
	if err := domain.ValidateUnits(in.Units); err != nil {
		return PeriodView{}, problem.Validation.WithField("units", err.Error())
	}
	on := s.Clock.Now().UTC()
	if in.Period != nil && in.Period.From != nil {
		on = *in.Period.From
	}
	if in.UnitParents, err = s.facilityParents(ctx, unitKindOf(prof, in.UnitKind), in.Units, in.UnitParents, on); err != nil {
		return PeriodView{}, err
	}
	spec, err := specOf(prof, in)
	if err != nil {
		return PeriodView{}, err
	}
	for _, u := range in.Units {
		if !a.allowsUnit(spec, in.Units, u) {
			return PeriodView{}, problem.Forbidden.WithDetail("đơn vị " + u + " ngoài phạm vi người phát hành")
		}
	}
	now := s.Clock.Now().UTC()
	var id uuid.UUID
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		t, err := store.TemplateByID(ctx, tx, in.TemplateID)
		if err != nil {
			return err
		}
		if t == nil || t.Profile != in.Profile || !a.allowsTemplate(t.Code) {
			return port.ErrTemplateNotFound
		}
		if t.Fixture && !prof.AllowFixtures {
			return port.ErrFixtureTemplate
		}
		owner := a.node
		p := store.Period{ID: ids.New(), TemplateID: t.ID, Label: in.Label, DueAt: in.DueAt, Units: in.Units, PublisherLevel: a.level,
			ActorRef: a.ref, EventTime: now}
		if owner != uuid.Nil {
			p.OwnerRef = &owner
		}
		if err := store.InsertPeriod(ctx, tx, a.tenant, &p, idemOr(idem, "period")); err != nil {
			return err
		}
		id = p.ID
		if err := store.InsertSpec(ctx, tx, a.tenant, p.ID, spec, a.ref, now, idemOr(idem, "spec")); err != nil {
			return err
		}
		ev := domain.Event{ID: ids.New(), Action: "published", Mode: port.ModeManual, Actor: a.ref, At: now}
		return store.InsertEvent(ctx, tx, a.tenant, p.ID, p.Profile, &ev, idemOr(idem, "published"))
	})
	if err != nil {
		return PeriodView{}, classify(err)
	}
	return s.Period(ctx, in.Profile, id)
}

// specOf dựng và kiểm đặc tả kỳ từ yêu cầu phát hành và mặc định của profile.
func specOf(prof port.Profile, in PublishInput) (domain.Spec, error) {
	spec := domain.Spec{UnitKind: in.UnitKind, UnitParents: in.UnitParents, MaxSubmissions: prof.MaxSubmissions, ReviewFlow: port.ReviewFlowSingle}
	if spec.UnitKind == "" {
		spec.UnitKind = prof.Kinds()[0]
	}
	if in.MaxSubmissions != nil {
		spec.MaxSubmissions = *in.MaxSubmissions
	}
	if k := in.Period; k != nil {
		spec.PeriodKind, spec.PeriodKey, spec.From, spec.To, spec.PeriodRule = k.Kind, strings.TrimSpace(k.Key), k.From, k.To, strings.TrimSpace(k.Rule)
	}
	if err := domain.ValidateSpec(prof.Kinds(), in.Units, spec); err != nil {
		var te domain.ErrTemplate
		if errorsAs(err, &te) {
			return spec, problem.Validation.WithField(te.Field, te.Msg)
		}
		return spec, problem.Validation.WithDetail(err.Error())
	}
	return spec, nil
}

// owns: người gọi thuộc Nút phát hành của kỳ.
func (a actor) owns(p store.Period) bool {
	return a.node != uuid.Nil && p.OwnerRef != nil && *p.OwnerRef == a.node
}

// canSee: phạm vi toàn quốc, Nút chủ của kỳ, hoặc có ít nhất một đơn vị nhận trong phạm vi (theo loại đơn vị lá).
func (a actor) canSee(p store.Period, spec domain.Spec) bool {
	if !a.allowsTemplate(p.TemplateCode) {
		return false
	}
	if (a.scope.All || a.owns(p)) && a.targetFilter == nil {
		return true
	}
	return slices.ContainsFunc(p.Units, func(u string) bool { return a.allowsUnit(spec, p.Units, u) })
}

// canClose checks the control level/node and every affected target.
// An empty selection means the whole period, as for ReopenInput.
func (a actor) canClose(prof string, p store.Period, spec domain.Spec, units []string) bool {
	if !a.allowsTemplate(p.TemplateCode) || a.level != prof || (!a.scope.All && !a.owns(p)) {
		return false
	}
	if a.targetFilter == nil {
		return true
	}
	if len(units) == 0 {
		units = p.Units
	}
	return !slices.ContainsFunc(units, func(u string) bool { return !a.allowsUnit(spec, p.Units, u) })
}

// canReadWholeSnapshot checks all bounded units before returning immutable bytes.
// Filtering a snapshot body would invalidate its stored hash.
func (a actor) canReadWholeSnapshot(p store.Period, spec domain.Spec) bool {
	if !a.canSee(p, spec) {
		return false
	}
	if a.templateFilter != nil || a.targetFilter != nil {
		return !slices.ContainsFunc(p.Units, func(u string) bool { return !a.allowsUnit(spec, p.Units, u) })
	}
	return true
}

// load reads the native period and projections in the current tenant transaction.
func load(ctx context.Context, tx pgx.Tx, id uuid.UUID, now time.Time) (*PeriodView, error) {
	p, err := store.PeriodByID(ctx, tx, id)
	if err != nil || p == nil {
		return nil, err
	}
	v := &PeriodView{Period: *p}
	if v.Spec, err = store.SpecOf(ctx, tx, id); err != nil {
		return nil, err
	}
	if v.Events, err = store.Events(ctx, tx, id); err != nil {
		return nil, err
	}
	v.State = domain.StateOf(v.Events)
	subs, err := store.LatestSubmissions(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	held, err := store.LatestHeld(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	sids := make([]uuid.UUID, 0, len(subs))
	for _, sb := range subs {
		sids = append(sids, sb.ID)
	}
	revs, err := store.ReviewsOf(ctx, tx, sids)
	if err != nil {
		return nil, err
	}
	metas, err := store.MetaOf(ctx, tx, sids)
	if err != nil {
		return nil, err
	}
	for _, u := range p.Units {
		uv := UnitView{Unit: u, Parent: v.Spec.Parent(p.Units, u)}
		if sb, ok := subs[u]; ok {
			sb := sb
			uv.Submission = &sb
			if r, ok := revs[sb.ID]; ok {
				r := r
				uv.Review = &r
			}
			if m, ok := metas[sb.ID]; ok {
				m := m
				uv.Meta = &m
			}
		}
		var heldAt *time.Time
		if h, ok := held[u]; ok {
			h := h
			uv.Held, heldAt = &h, &h.EventTime
		}
		uv.Status = domain.DisplayStatus(domain.SubmissionStatus(uv.Submission != nil, uv.Review), v.State, u, heldAt, p.DueAt, now)
		v.Units = append(v.Units, uv)
	}
	v.Snapshots, err = store.Snapshots(ctx, tx, id)
	return v, err
}

// Period đọc kỳ (người gọi phải thấy kỳ: toàn quốc, Nút chủ, hoặc có đơn vị nhận trong phạm vi — đơn vị ngoài phạm
// vi bị lược khỏi danh sách).
func (s *Service) Period(ctx context.Context, profile string, id uuid.UUID) (PeriodView, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return PeriodView{}, err
	}
	var v *PeriodView
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		var e error
		v, e = load(ctx, tx, id, s.Clock.Now().UTC())
		return e
	})
	if err != nil {
		return PeriodView{}, err
	}
	if v == nil || v.Period.Profile != profile || !a.canSee(v.Period, v.Spec) {
		return PeriodView{}, port.ErrPeriodNotFound
	}
	if !a.scope.All && !a.owns(v.Period) && a.targetFilter == nil {
		v.Units = slices.DeleteFunc(v.Units, func(u UnitView) bool { return !a.allowsUnit(v.Spec, v.Period.Units, u.Unit) })
	}
	a.filterTargets(v)
	return *v, nil
}

// Periods liệt kê kỳ của profile mà người gọi thấy (tối đa limit, mới trước).
func (s *Service) Periods(ctx context.Context, profile string, limit int) ([]store.Period, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []store.Period
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		ps, e := store.Periods(ctx, tx, profile, limit)
		if e != nil {
			return e
		}
		for _, p := range ps {
			spec := domain.DefaultSpec()
			if (!a.scope.All && !a.owns(p)) || a.targetFilter != nil {
				if spec, e = store.SpecOf(ctx, tx, p.ID); e != nil {
					return e
				}
			}
			if a.canSee(p, spec) {
				v := PeriodView{Period: p, Spec: spec}
				a.filterTargets(&v)
				out = append(out, v.Period)
			}
		}
		return nil
	})
	return out, err
}

// ---------------------------------------------------------------------------
// Nộp bài, rà soát
// ---------------------------------------------------------------------------

// SubmissionView là một phiên bản bài nộp kèm trạng thái.
type SubmissionView struct {
	Submission store.Submission
	Status     string
	Review     *domain.Review
	Meta       *store.Meta // W11.42
	Replayed   bool        // nhận lại cùng mã phiên bản (đồng bộ gửi lặp)
}

// SubmitInput là một lần nộp (W11.42): số liệu, phiên bản người nhập đã thấy (khóa lạc quan), báo không phát sinh,
// ghi chú (H08), giải trình/lý do trễ (H09), thời điểm ghi offline (bản offline — kỳ đã chốt thì GIỮ "Chờ mở kỳ").
type SubmitInput struct {
	ID     *uuid.UUID // mã phiên bản do máy sinh (đồng bộ ngoại tuyến, UUIDv7) — nil = máy chủ sinh
	Unit   string
	Values map[string]string
	// BaseVersion: số phiên bản cuối người nhập đã thấy khi mở bài (0 = chưa có bài). nil = không kiểm (API cũ).
	BaseVersion *int64
	NilReport   bool
	Note        string
	Explanation string
	CapturedAt  *time.Time
	DeviceID    string
}

// Submit nộp phiên bản mới của bài (cấp lá, đơn vị trong phạm vi): kỳ phải đang nhận bài của đơn vị, số liệu khớp
// biểu. Lập yêu cầu duyệt ở cap/workflow (rà soát bốn mắt) cùng giao dịch.
func (s *Service) Submit(ctx context.Context, profile string, periodID uuid.UUID, unit string, values map[string]string, idem string) (SubmissionView, error) {
	return s.SubmitWith(ctx, profile, periodID, SubmitInput{Unit: unit, Values: values}, idem)
}

// SubmitOffline nộp bản ghi offline (capturedAt = lúc ghi trên thiết bị): kỳ còn nhận bài của đơn vị → như Submit;
// kỳ đã chốt với đơn vị → GIỮ bài ở trạng thái "Chờ mở kỳ" (pf_forms.held_submission, CNTS III.3.0 dòng 13), không
// vào số liệu kỳ, không đổi ảnh chụp; Cấp Tỉnh mở lại kỳ rồi đơn vị nộp lại. Kỳ đã hủy → ErrPeriodCancelled.
func (s *Service) SubmitOffline(ctx context.Context, profile string, periodID uuid.UUID, unit string, values map[string]string, capturedAt time.Time,
	idem string) (SubmissionView, error) {
	if capturedAt.IsZero() {
		return SubmissionView{}, problem.Validation.WithField("captured_at", "bắt buộc với bản offline")
	}
	return s.SubmitWith(ctx, profile, periodID, SubmitInput{Unit: unit, Values: values, CapturedAt: &capturedAt}, idem)
}

// SubmitWith là Submit đầy đủ (W11.42): khóa lạc quan theo BaseVersion (409 forms.submission_conflict, kèm thời điểm
// cập nhật của phiên bản mới hơn), báo không phát sinh (trường số = 0), quy tắc nghiệp vụ của bên dùng (Validator),
// lý do trễ bắt buộc khi profile đòi (RequireLateReason) và máy chủ nhận bài sau hạn (D8 — không theo captured_at của
// thiết bị). Thông tin phiên bản ghi sổ phụ submission_meta cùng giao dịch (DB kiểm lại base_version = seq − 1, tính
// late theo giờ máy chủ). in.ID đã ghi (bài nộp hoặc bài
// giữ) → trả lại bản đã ghi, Replayed (đồng bộ gửi lặp).
func (s *Service) SubmitWith(ctx context.Context, profile string, periodID uuid.UUID, in SubmitInput, idem string) (SubmissionView, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return SubmissionView{}, err
	}
	if err := a.need(prof.Leaf(), "nộp bài"); err != nil {
		return SubmissionView{}, err
	}
	unit := in.Unit
	if !domain.ValidateUnit(unit) {
		return SubmissionView{}, problem.Validation.WithField("unit", "mã đơn vị không hợp lệ")
	}
	in.Note, in.Explanation = strings.TrimSpace(in.Note), strings.TrimSpace(in.Explanation)
	if len([]rune(in.Note)) > 1000 {
		return SubmissionView{}, problem.Validation.WithField("note", "tối đa 1000 ký tự")
	}
	if len([]rune(in.Explanation)) > 1000 {
		return SubmissionView{}, problem.Validation.WithField("explanation", "tối đa 1000 ký tự")
	}
	if in.BaseVersion != nil && *in.BaseVersion < 0 {
		return SubmissionView{}, problem.Validation.WithField("base_version", "≥ 0")
	}
	values := in.Values
	if values == nil {
		values = map[string]string{}
	}
	now := s.Clock.Now().UTC()
	var out SubmissionView
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, periodID); err != nil {
			return err
		}
		v, err := load(ctx, tx, periodID, now)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.allowsTemplate(v.Period.TemplateCode) {
			return port.ErrPeriodNotFound
		}
		if !a.allowsUnit(v.Spec, v.Period.Units, unit) {
			return problem.Forbidden.WithDetail("đơn vị " + unit + " ngoài phạm vi")
		}
		if !slices.Contains(v.Period.Units, unit) {
			return port.ErrUnitNotInPeriod
		}
		if in.ID != nil {
			if done, err := s.replayed(ctx, tx, *in.ID, a.ref, periodID, unit, &out); err != nil || done {
				return err
			}
		}
		if v.State.Status == domain.PeriodCancelled {
			return port.ErrPeriodCancelled
		}
		var t *store.Template
		check := func() error {
			var err error
			if in.NilReport { // điền 0 cho trường số trước khi kiểm bắt buộc
				tp, err := store.TemplateByID(ctx, tx, v.Period.TemplateID)
				if err != nil {
					return err
				}
				if tp != nil {
					if values, err = domain.NilValues(tp.Fields, values); err != nil {
						var te domain.ErrTemplate
						errorsAs(err, &te)
						return port.ErrInvalidValues.WithField(te.Field, te.Msg)
					}
				}
			}
			if t, err = checkValues(ctx, tx, v.Period.TemplateID, values); err != nil {
				return err
			}
			if vd := s.Validators[profile]; vd != nil {
				if issues := vd.Validate(ctx, t.Code, values); len(issues) > 0 {
					p := port.ErrInvalidValues
					for _, is := range issues {
						p = p.WithField(is.Field, is.Reason)
					}
					return p
				}
			}
			return nil
		}
		if in.CapturedAt != nil && v.State.ClosedFor(unit) {
			if err := check(); err != nil {
				return err
			}
			h := store.Held{ID: ids.New(), PeriodID: periodID, Unit: unit, Values: values, Submitter: a.ref, CapturedAt: *in.CapturedAt, EventTime: now}
			if in.ID != nil {
				h.ID = *in.ID
			}
			if err := store.InsertHeld(ctx, tx, a.tenant, &h, idemOr(idem, "held")); err != nil {
				return err
			}
			out = SubmissionView{Submission: store.Submission{ID: h.ID, PeriodID: periodID, Unit: unit, Values: values, Submitter: a.ref, EventTime: now},
				Status: domain.SubAwaitingReopen}
			return nil
		}
		if !v.State.AcceptsUnit(unit) {
			if v.State.Status == domain.PeriodReopened {
				return port.ErrUnitNotInPeriod
			}
			return port.ErrPeriodClosed
		}
		i := slices.IndexFunc(v.Units, func(u UnitView) bool { return u.Unit == unit })
		cur := v.Units[i]
		var curVersion int64
		if cur.Submission != nil {
			curVersion = cur.Submission.Version
		}
		if in.BaseVersion != nil && *in.BaseVersion != curVersion {
			return port.ErrVersionConflict.WithDetail(conflictDetail(cur.Submission)).WithField("base_version",
				fmt.Sprintf("phiên bản hiện tại %d", curVersion))
		}
		var acceptedAt *time.Time
		if cur.Review != nil && cur.Review.Decision == domain.SubAccepted {
			acceptedAt = &cur.Review.At
		}
		if !domain.CanResubmit(cur.Status, acceptedAt, v.State) {
			return port.ErrSubmissionLocked
		}
		if cur.Submission != nil && v.Spec.LimitReached(cur.Submission.Version) {
			return port.ErrSubmissionLimit.WithDetail(fmt.Sprintf("tối đa %d lần gửi", v.Spec.MaxSubmissions))
		}
		if err := check(); err != nil {
			return err
		}
		// D8: trễ hạn theo giờ MÁY CHỦ nhận bài (now), kể cả bản offline — captured_at (giờ thiết bị) chỉ lưu để kiểm
		// toán (submission_meta.captured_at); DB tính late cùng mốc (pf 20261013091503). Bản offline ghi trước hạn nhưng tới sau
		// hạn cũng phải có lý do trễ: máy nhận 422 forms.late_reason_required, người nhập ghi lý do rồi gửi lại.
		if prof.RequireLateReason && now.After(v.Period.DueAt) && in.Explanation == "" {
			return port.ErrLateReason.WithField("explanation", "bắt buộc khi gửi sau hạn "+v.Period.DueAt.In(vnZone).Format("15:04 02/01/2006"))
		}
		sb := store.Submission{ID: ids.New(), PeriodID: periodID, Unit: unit, Values: values, Submitter: a.ref, EventTime: now}
		if in.ID != nil {
			sb.ID = *in.ID
		}
		if err := store.InsertSubmission(ctx, tx, a.tenant, &sb, idemOr(idem, "submission")); err != nil {
			return err
		}
		m := store.Meta{SubmissionID: sb.ID, BaseVersion: curVersion, NilReport: in.NilReport, Note: in.Note, Explanation: in.Explanation,
			CapturedAt: in.CapturedAt}
		if err := store.InsertMeta(ctx, tx, a.tenant, &m, a.ref, now, in.DeviceID, idemOr(idem, "meta")); err != nil {
			return err
		}
		if s.Approvals != nil {
			if _, err := s.Approvals.Submit(db.JoinTx(ctx, tx), wfport.SubmitInput{Kind: WorkflowKind, Subject: sb.ID, Requester: a.ref,
				Summary: "forms " + profile + " " + t.Code + " " + unit, At: now}); err != nil {
				return err
			}
		}
		out = SubmissionView{Submission: sb, Status: domain.SubSubmitted, Meta: &m}
		// W11.43: quy tắc liên biểu + cảnh báo của bên dùng chạy sau commit (consumer inbox đọc qua port.SubmissionFeed).
		return enqueueRecorded(ctx, tx, a.tenant, profile, t.Code, sb, m)
	})
	return out, classify(err)
}

// replayed: mã phiên bản id đã ghi (bài nộp hoặc bài giữ) → out = bản đã ghi, done.
func (s *Service) replayed(ctx context.Context, tx pgx.Tx, id, actorRef, periodRef uuid.UUID, unit string, out *SubmissionView) (bool, error) {
	sb, err := store.SubmissionByID(ctx, tx, id)
	if err != nil {
		return false, err
	}
	if sb != nil && (sb.Submitter != actorRef || sb.PeriodID != periodRef || sb.Unit != unit) {
		return false, problem.Conflict.WithDetail("mã phiên bản bài nộp đã dùng")
	}
	if sb != nil {
		*out = SubmissionView{Submission: *sb, Status: domain.SubSubmitted, Replayed: true}
		return true, nil
	}
	h, err := store.HeldByID(ctx, tx, id)
	if err != nil || h == nil {
		return false, err
	}
	if h.Submitter != actorRef || h.PeriodID != periodRef || h.Unit != unit {
		return false, problem.Conflict.WithDetail("mã phiên bản bài nộp đã dùng")
	}
	*out = SubmissionView{Submission: store.Submission{ID: h.ID, PeriodID: h.PeriodID, Unit: h.Unit, Values: h.Values, Submitter: h.Submitter,
		EventTime: h.EventTime}, Status: domain.SubAwaitingReopen, Replayed: true}
	return true, nil
}

// vnZone là giờ Việt Nam (hiển thị hạn nộp, thời điểm cập nhật trong thông báo lỗi).
var vnZone = time.FixedZone("ICT", 7*3600)

// conflictDetail: "Đã có người cập nhật lúc …" (CNTS III.3.0).
func conflictDetail(cur *store.Submission) string {
	if cur == nil {
		return "Bài đã thay đổi — tải bản mới"
	}
	return fmt.Sprintf("Đã có người cập nhật lúc %s (phiên bản %d) — tải bản mới rồi sửa lại", cur.EventTime.In(vnZone).Format("15:04 02/01/2006"),
		cur.Version)
}

// checkValues đọc phiên bản biểu của kỳ và kiểm số liệu theo nó.
func checkValues(ctx context.Context, tx pgx.Tx, templateID uuid.UUID, values map[string]string) (*store.Template, error) {
	t, err := store.TemplateByID(ctx, tx, templateID)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, port.ErrTemplateNotFound
	}
	if err := domain.ValidateValues(t.Fields, values); err != nil {
		var te domain.ErrTemplate
		if errorsAs(err, &te) {
			return nil, port.ErrInvalidValues.WithField(te.Field, te.Msg)
		}
		return nil, port.ErrInvalidValues.WithDetail(err.Error())
	}
	return t, nil
}

// Submissions trả lịch sử phiên bản bài nộp của một đơn vị (mới trước) kèm rà soát.
func (s *Service) Submissions(ctx context.Context, profile string, periodID uuid.UUID, unit string) ([]SubmissionView, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	var out []SubmissionView
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		p, err := store.PeriodByID(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if p == nil || p.Profile != profile || !a.allowsTemplate(p.TemplateCode) {
			return port.ErrPeriodNotFound
		}
		spec, err := store.SpecOf(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if !a.allowsUnit(spec, p.Units, unit) {
			return problem.Forbidden.WithDetail("đơn vị " + unit + " ngoài phạm vi")
		}
		subs, err := store.SubmissionHistory(ctx, tx, periodID, unit)
		if err != nil {
			return err
		}
		sids := make([]uuid.UUID, 0, len(subs))
		for _, sb := range subs {
			sids = append(sids, sb.ID)
		}
		revs, err := store.ReviewsOf(ctx, tx, sids)
		if err != nil {
			return err
		}
		for _, sb := range subs {
			sv := SubmissionView{Submission: sb}
			if r, ok := revs[sb.ID]; ok {
				r := r
				sv.Review = &r
			}
			sv.Status = domain.SubmissionStatus(true, sv.Review)
			out = append(out, sv)
		}
		return nil
	})
	return out, err
}

// Review rà soát phiên bản bài nộp cuối của đơn vị (cấp review_by, đơn vị trong phạm vi): chấp nhận hoặc trả lại
// (bắt buộc ghi chú). Người rà soát ≠ người nộp: kiểm ở đây, ở cap/workflow (CHECK decision_four_eyes) và ở
// pf_forms.review (CHECK review_four_eyes). Quyết định của cap/workflow và dòng review cùng commit.
func (s *Service) Review(ctx context.Context, profile string, submissionID uuid.UUID, decision, note, idem string) (SubmissionView, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return SubmissionView{}, err
	}
	if err := a.need(prof.Reviewer(), "rà soát bài nộp"); err != nil {
		return SubmissionView{}, err
	}
	note = strings.TrimSpace(note)
	switch decision {
	case domain.SubAccepted:
	case domain.SubReturned:
		if note == "" {
			return SubmissionView{}, problem.Validation.WithField("note", "bắt buộc khi trả lại")
		}
	default:
		return SubmissionView{}, problem.Validation.WithField("decision", "accepted | returned")
	}
	if len([]rune(note)) > 1000 {
		return SubmissionView{}, problem.Validation.WithField("note", "tối đa 1000 ký tự")
	}
	now := s.Clock.Now().UTC()
	if g := s.ReviewGuards[profile]; g != nil {
		// BF-CN-XA-DATA (#175): quy tắc rà soát của bên dùng (CNTS: chỉ người phụ trách biểu). Đọc kỳ của bài nộp và kiểm
		// phạm vi như dưới (ngoài phạm vi → 404 như cũ, không lộ qua lỗi của guard), rồi gọi guard NGOÀI giao dịch.
		var period uuid.UUID
		var code string
		if err := s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
			sb, err := store.SubmissionByID(ctx, tx, submissionID)
			if err != nil {
				return err
			}
			if sb == nil {
				return port.ErrSubmissionMissing
			}
			v, err := load(ctx, tx, sb.PeriodID, now)
			if err != nil {
				return err
			}
			if v == nil || v.Period.Profile != profile || !a.allowsTemplate(v.Period.TemplateCode) || !a.allowsUnit(v.Spec, v.Period.Units, sb.Unit) {
				return port.ErrSubmissionMissing
			}
			period, code = sb.PeriodID, v.Period.TemplateCode
			return nil
		}); err != nil {
			return SubmissionView{}, classify(err)
		}
		if err := g.AllowReview(ctx, period, code); err != nil {
			return SubmissionView{}, err
		}
	}
	var out SubmissionView
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		sb, err := store.SubmissionByID(ctx, tx, submissionID)
		if err != nil {
			return err
		}
		if sb == nil {
			return port.ErrSubmissionMissing
		}
		if err := store.Lock(ctx, tx, sb.PeriodID); err != nil {
			return err
		}
		v, err := load(ctx, tx, sb.PeriodID, now)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.allowsTemplate(v.Period.TemplateCode) || !a.allowsUnit(v.Spec, v.Period.Units, sb.Unit) {
			return port.ErrSubmissionMissing
		}
		if v.State.Status == domain.PeriodCancelled {
			return port.ErrPeriodCancelled
		}
		if v.State.Status == domain.PeriodClosed {
			return port.ErrPeriodClosed
		}
		i := slices.IndexFunc(v.Units, func(u UnitView) bool { return u.Unit == sb.Unit })
		cur := v.Units[i]
		if cur.Submission == nil || cur.Submission.ID != sb.ID {
			return port.ErrSuperseded
		}
		if cur.Review != nil {
			return port.ErrAlreadyReviewed
		}
		if decision == domain.SubReturned && v.Spec.LimitReached(sb.Version) {
			// KHTC FN-TI-02.04: lần gửi cuối cho phép thì phiếu tự khóa — không yêu cầu bổ sung được nữa.
			return port.ErrSubmissionLimit.WithDetail("đã gửi đủ số lần tối đa — chỉ xác nhận được")
		}
		if sb.Submitter == a.ref {
			return problem.SelfApproval.WithDetail("người rà soát trùng người nộp")
		}
		var reqID *uuid.UUID
		if s.Approvals != nil {
			outcome, wnote := wfport.OutcomeApproved, ""
			if decision == domain.SubReturned {
				outcome, wnote = wfport.OutcomeRejected, note
			}
			d, err := s.Approvals.Decide(db.JoinTx(ctx, tx), wfport.DecideInput{Kind: WorkflowKind, Subject: sb.ID, Requester: sb.Submitter,
				Decider: a.ref, Outcome: outcome, Note: wnote, At: now})
			if err != nil {
				return err
			}
			reqID = &d.RequestID
		}
		r := domain.Review{ID: ids.New(), Decision: decision, Reviewer: a.ref, Note: note, At: now}
		if err := store.InsertReview(ctx, tx, a.tenant, sb.ID, &r, reqID, idemOr(idem, "review")); err != nil {
			return err
		}
		out = SubmissionView{Submission: *sb, Status: decision, Review: &r}
		return nil
	})
	return out, classify(err)
}

// ---------------------------------------------------------------------------
// Chốt, mở lại
// ---------------------------------------------------------------------------

// Close chốt kỳ (cấp close_by, Nút chủ hoặc toàn quốc): sự kiện closed + ẢNH CHỤP ĐÓNG BĂNG (bài nộp cuối của từng
// đơn vị, trạng thái rà soát, số liệu) — băm SHA-256 do DB tính; sự kiện pf.forms.period.closed. Chốt lại sau mở lại
// sinh ảnh chụp mới, ảnh cũ không đổi.
func (s *Service) Close(ctx context.Context, profile string, periodID uuid.UUID, idem string) (PeriodView, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return PeriodView{}, err
	}
	now := s.Clock.Now().UTC()
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, periodID); err != nil {
			return err
		}
		v, err := load(ctx, tx, periodID, now)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.canSee(v.Period, v.Spec) {
			return port.ErrPeriodNotFound
		}
		if !a.canClose(prof.CloseBy, v.Period, v.Spec, nil) {
			return problem.Forbidden.WithDetail("chốt kỳ cần cấp " + prof.CloseBy + " của Nút phát hành")
		}
		if v.State.Status == domain.PeriodCancelled {
			return port.ErrPeriodCancelled
		}
		if v.State.Status == domain.PeriodClosed {
			return port.ErrPeriodClosed
		}
		content := snapshotOf(v, now, port.ModeManual)
		if pending := domain.PendingReview(content.Units); prof.RequireReviewedBeforeClose && len(pending) > 0 {
			return port.ErrPendingReview.WithDetail("đơn vị chờ rà soát: " + strings.Join(pending, ", "))
		}
		ev := domain.Event{ID: ids.New(), Action: "closed", Mode: port.ModeManual, Actor: a.ref, At: now}
		if err := store.InsertEvent(ctx, tx, a.tenant, periodID, profile, &ev, idemOr(idem, "closed")); err != nil {
			return err
		}
		raw, err := json.Marshal(content)
		if err != nil {
			return err
		}
		snap := store.Snapshot{ID: ids.New(), PeriodRef: periodID, PeriodEventID: ev.ID, Content: raw, ActorRef: a.ref, EventTime: now}
		if err := store.InsertSnapshot(ctx, tx, a.tenant, &snap); err != nil {
			return err
		}
		return enqueueClosed(ctx, tx, a.tenant, periodID, profile, "native", port.ModeManual, snap.ID, snap.No, snap.Hash, a.ref, now)
	})
	if err != nil {
		return PeriodView{}, classify(err)
	}
	return s.Period(ctx, profile, periodID)
}

// snapshotOf dựng nội dung ảnh chụp từ trạng thái kỳ.
func snapshotOf(v *PeriodView, now time.Time, mode string) domain.Snapshot {
	snap := domain.Snapshot{Schema: domain.SnapshotSchema, RuleVersion: domain.RuleVersion, Profile: v.Period.Profile,
		PeriodID: v.Period.ID.String(), Label: v.Period.Label, TemplateID: v.Period.TemplateID.String(), TemplateCode: v.Period.TemplateCode,
		TemplateVersion: v.Period.TemplateVersion, SnapshotNo: int64(v.State.Closes + 1), ClosedAt: now.Format(time.RFC3339Nano), Mode: mode,
		Units: []domain.UnitSnapshot{}}
	for _, u := range v.Units {
		us := domain.UnitSnapshot{Unit: u.Unit, Status: domain.SubmissionStatus(u.Submission != nil, u.Review), ClosedStatus: domain.ClosedStatus(u.Status),
			Values: map[string]string{}}
		if u.Submission != nil {
			id, by := u.Submission.ID.String(), u.Submission.Submitter.String()
			us.SubmissionID, us.Version, us.Values, us.SubmittedBy = &id, u.Submission.Version, u.Submission.Values, &by
		}
		if u.Review != nil {
			by := u.Review.Reviewer.String()
			us.ReviewedBy = &by
		}
		if u.Meta != nil {
			us.NilReport, us.Late = u.Meta.NilReport, u.Meta.Late
		}
		snap.Units = append(snap.Units, us)
	}
	return snap
}

// ReopenInput là yêu cầu mở lại kỳ đã chốt.
type ReopenInput struct {
	Units  []string // rỗng = cả kỳ
	Reason string   // 10–500 ký tự
}

// Reopen mở lại kỳ đã chốt — sự kiện mới (lý do bắt buộc), ảnh chụp đã chốt không đổi. Profile NoReopen → 403.
func (s *Service) Reopen(ctx context.Context, profile string, periodID uuid.UUID, in ReopenInput, idem string) (PeriodView, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return PeriodView{}, err
	}
	if prof.NoReopen {
		return PeriodView{}, problem.Forbidden.WithDetail("biểu " + profile + " không mở lại kỳ đã chốt — thay đổi sau chốt qua điều chỉnh của nghiệp vụ")
	}
	why, ok := domain.Reason(in.Reason)
	if !ok {
		return PeriodView{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	now := s.Clock.Now().UTC()
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, periodID); err != nil {
			return err
		}
		v, err := load(ctx, tx, periodID, now)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.canSee(v.Period, v.Spec) {
			return port.ErrPeriodNotFound
		}
		if !a.canClose(prof.CloseBy, v.Period, v.Spec, in.Units) {
			return problem.Forbidden.WithDetail("mở lại kỳ cần cấp " + prof.CloseBy + " của Nút phát hành")
		}
		if v.State.Status == domain.PeriodCancelled {
			return port.ErrPeriodCancelled
		}
		if v.State.Status != domain.PeriodClosed {
			return port.ErrPeriodNotClosed
		}
		for i, u := range in.Units {
			if !slices.Contains(v.Period.Units, u) || slices.Contains(in.Units[:i], u) {
				return port.ErrUnitNotInPeriod.WithDetail("đơn vị " + u)
			}
		}
		ev := domain.Event{ID: ids.New(), Action: "reopened", Mode: port.ModeManual, Units: in.Units, Reason: why, Actor: a.ref, At: now}
		return store.InsertEvent(ctx, tx, a.tenant, periodID, profile, &ev, idemOr(idem, "reopened"))
	})
	if err != nil {
		return PeriodView{}, classify(err)
	}
	return s.Period(ctx, profile, periodID)
}

// Cancel hủy kỳ đang mở (KHTC BR-DL-18, W11.05): cấp close_by của Nút phát hành, lý do bắt buộc. Kỳ đã hủy là trạng
// thái cuối — không nộp, rà soát, chốt hay mở lại; số liệu đã gửi giữ trong sổ nhưng không vào ảnh chụp (kỳ hủy không
// có ảnh chụp mới). Kỳ đã chốt không hủy được (mở lại trước — khớp trg_transition).
func (s *Service) Cancel(ctx context.Context, profile string, periodID uuid.UUID, reason, idem string) (PeriodView, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return PeriodView{}, err
	}
	why, ok := domain.Reason(reason)
	if !ok {
		return PeriodView{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
	}
	now := s.Clock.Now().UTC()
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, periodID); err != nil {
			return err
		}
		v, err := load(ctx, tx, periodID, now)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.canSee(v.Period, v.Spec) {
			return port.ErrPeriodNotFound
		}
		if !a.canClose(prof.CloseBy, v.Period, v.Spec, nil) {
			return problem.Forbidden.WithDetail("hủy kỳ cần cấp " + prof.CloseBy + " của Nút phát hành")
		}
		switch v.State.Status {
		case domain.PeriodCancelled:
			return port.ErrPeriodCancelled
		case domain.PeriodClosed:
			return port.ErrPeriodClosed
		}
		ev := domain.Event{ID: ids.New(), Action: port.ActionCancelled, Mode: port.ModeManual, Reason: why, Actor: a.ref, At: now}
		return store.InsertEvent(ctx, tx, a.tenant, periodID, profile, &ev, idemOr(idem, "cancelled"))
	})
	if err != nil {
		return PeriodView{}, classify(err)
	}
	return s.Period(ctx, profile, periodID)
}

// Snapshot đọc một ảnh chụp kèm nội dung và kết quả kiểm băm. Kỳ native: người gọi phải thấy kỳ; kỳ ghi song song
// (mirror): cấp trên cấp lá của profile.
func (s *Service) Snapshot(ctx context.Context, profile string, id uuid.UUID) (store.Snapshot, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return store.Snapshot{}, err
	}
	var out *store.Snapshot
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		snap, err := store.SnapshotByID(ctx, tx, id)
		if err != nil || snap == nil || snap.Profile != profile {
			return err
		}
		p, err := store.PeriodByID(ctx, tx, snap.PeriodRef)
		if err != nil {
			return err
		}
		if p != nil {
			spec, err := store.SpecOf(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			if p.Profile == profile && a.canReadWholeSnapshot(*p, spec) {
				out = snap
			}
			return nil
		}
		if a.templateFilter == nil && a.targetFilter == nil && a.level != "" && a.level != prof.Leaf() {
			out = snap
		}
		return nil
	})
	if err != nil {
		return store.Snapshot{}, err
	}
	if out == nil {
		return store.Snapshot{}, port.ErrSnapshotNotFound
	}
	return *out, nil
}

// Snapshots liệt kê ảnh chụp của một kỳ (không kèm nội dung): kỳ native — người gọi phải thấy kỳ; kỳ ghi song song
// (mirror) — cấp trên cấp lá của profile.
func (s *Service) Snapshots(ctx context.Context, profile string, periodRef uuid.UUID) ([]store.Snapshot, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	var out []store.Snapshot
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		p, err := store.PeriodByID(ctx, tx, periodRef)
		if err != nil {
			return err
		}
		if p != nil {
			spec, err := store.SpecOf(ctx, tx, p.ID)
			if err != nil {
				return err
			}
			if p.Profile != profile || !a.canSee(*p, spec) {
				return port.ErrPeriodNotFound
			}
		}
		if p == nil && (a.templateFilter != nil || a.targetFilter != nil || a.level == "" || a.level == prof.Leaf()) {
			return problem.Forbidden.WithDetail("ảnh chụp kỳ ghi song song cần cấp trên cấp lá")
		}
		ss, err := store.Snapshots(ctx, tx, periodRef)
		for _, x := range ss {
			if x.Profile == profile {
				out = append(out, x)
			}
		}
		return err
	})
	return out, err
}

// enqueueRecorded ghi sự kiện pf.forms.submission.recorded vào outbox trong giao dịch nộp bài (W11.43).
func enqueueRecorded(ctx context.Context, tx pgx.Tx, tid uuid.UUID, profile, code string, sb store.Submission, m store.Meta) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventSubmissionRecorded, SchemaVersion: 1,
		OccurredAt: sb.EventTime, Producer: port.EventProducerName, Payload: port.SubmissionRecordedV1{SubmissionID: sb.ID.String(),
			PeriodRef: sb.PeriodID.String(), Profile: profile, TemplateCode: code, Unit: sb.Unit, Version: sb.Version, NilReport: m.NilReport,
			Late: m.Late, ActorRef: sb.Submitter.String(), EventTime: sb.EventTime.Format(time.RFC3339Nano)}})
}

// enqueueClosed ghi sự kiện pf.forms.period.closed vào outbox trong giao dịch tx.
func enqueueClosed(ctx context.Context, tx pgx.Tx, tid, periodRef uuid.UUID, profile, origin, mode string, snapID uuid.UUID, snapNo int64,
	hash []byte, actor uuid.UUID, at time.Time) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventPeriodClosed, SchemaVersion: 1, OccurredAt: at,
		Producer: port.EventProducerName, Payload: port.PeriodClosedV1{PeriodRef: periodRef.String(), Profile: profile, Origin: origin, Mode: mode,
			SnapshotID: snapID.String(), SnapshotNo: snapNo, ContentHash: hexOf(hash), ActorRef: actor.String(), EventTime: at.Format(time.RFC3339Nano)}})
}
