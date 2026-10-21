package app

// W11.42 (BRD CNTS v3.7 UR-XA-06…09, 13 — D4 CN2): phần dùng chung của kỳ native cho biểu kỳ Cấp Xã —
//   - phát hành MỘT LÔ kỳ (port.Publisher, gọi từ module nghiệp vụ tính lịch kỳ: kỳ ngày, tuần mới, hạn riêng);
//   - đơn vị lá là cơ sở giết mổ: kiểm cơ sở có trong danh mục đang dùng và thuộc xã (port.FacilityDirectory);
//   - "Sao chép kỳ trước": bài nộp cuối của đơn vị ở kỳ gần nhất trước đó của cùng biểu;
//   - danh sách công việc (kỳ × đơn vị trong phạm vi) cho màn "Công việc cần làm".

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Publisher cài port.Publisher trên Service (tên phương thức trùng use case REST nên tách kiểu).
type Publisher struct{ Svc *Service }

var _ port.Publisher = Publisher{}

// MaxBatchPeriods giới hạn số kỳ một lô (kỳ ngày một năm).
const MaxBatchPeriods = 366

// facilityParents kiểm đơn vị lá cơ sở giết mổ (W11.41 facility_ref) với danh mục tại ngày on: mọi cơ sở phải đang
// dùng; mã xã cha đã gửi phải khớp xã của cơ sở, vắng → điền từ danh mục. Không nối danh mục → giữ nguyên (ValidateSpec
// vẫn đòi mã xã cha).
func (s *Service) facilityParents(ctx context.Context, kind string, units, parents []string, on time.Time) ([]string, error) {
	if kind != port.UnitFacility || s.Facilities == nil {
		return parents, nil
	}
	refs := make([]uuid.UUID, 0, len(units))
	for _, u := range units {
		ref, err := uuid.Parse(u)
		if err != nil {
			return nil, port.ErrFacilityInvalid.WithField("units", "mã cơ sở (facility_ref) phải là UUID: "+u)
		}
		refs = append(refs, ref)
	}
	communes, err := s.Facilities.FacilityCommunes(ctx, refs, on)
	if err != nil {
		return nil, err
	}
	if len(parents) != 0 && len(parents) != len(units) {
		return nil, problem.Validation.WithField("unit_parents", "cần mã xã cha cho từng đơn vị")
	}
	out := make([]string, len(units))
	for i, ref := range refs {
		c, ok := communes[ref]
		if !ok {
			return nil, port.ErrFacilityInvalid.WithField("units", units[i])
		}
		if len(parents) != 0 && parents[i] != c {
			return nil, port.ErrFacilityInvalid.WithField("unit_parents", "cơ sở "+units[i]+" thuộc xã "+c+", không phải "+parents[i])
		}
		out[i] = c
	}
	return out, nil
}

// unitKindOf trả loại đơn vị lá hiệu lực của yêu cầu.
func unitKindOf(prof port.Profile, kind string) string {
	if kind == "" {
		return prof.Kinds()[0]
	}
	return kind
}

// DefineTemplate thực hiện port.Publisher.
func (pb Publisher) DefineTemplate(ctx context.Context, def port.TemplateDef, idem string) (port.TemplateRef, error) {
	var fields []domain.Field
	if err := json.Unmarshal(def.Fields, &fields); err != nil {
		return port.TemplateRef{}, port.ErrInvalidTemplate.WithField("fields", "JSON mảng trường")
	}
	t, err := pb.Svc.DefineTemplate(ctx, TemplateInput{Profile: def.Profile, Code: def.Code, Title: def.Title, Fields: fields}, idem)
	if err != nil {
		return port.TemplateRef{}, err
	}
	return refOf(t)
}

// LatestTemplate thực hiện port.Publisher.
func (pb Publisher) LatestTemplate(ctx context.Context, profile, code string) (port.TemplateRef, error) {
	ts, err := pb.Svc.Templates(ctx, profile)
	if err != nil {
		return port.TemplateRef{}, err
	}
	for _, t := range ts {
		if t.Code == code {
			return refOf(t)
		}
	}
	return port.TemplateRef{}, port.ErrTemplateNotFound
}

func refOf(t store.Template) (port.TemplateRef, error) {
	raw, err := json.Marshal(t.Fields)
	return port.TemplateRef{ID: t.ID, Code: t.Code, Version: t.Version, Title: t.Title, Fields: raw}, err
}

// PublishBatch thực hiện port.Publisher: mọi kỳ của lô cùng biểu, cùng đơn vị nhận, trong MỘT giao dịch.
func (pb Publisher) PublishBatch(ctx context.Context, in port.BatchInput, idem string) ([]port.PublishedPeriod, error) {
	s := pb.Svc
	prof, a, err := s.resolve(ctx, in.Profile)
	if err != nil {
		return nil, err
	}
	if err := a.need(prof.PublishBy, "phát hành kỳ"); err != nil {
		return nil, err
	}
	if len(in.Periods) == 0 || len(in.Periods) > MaxBatchPeriods {
		return nil, problem.Validation.WithField("periods", "1–366 kỳ mỗi lô")
	}
	if err := domain.ValidateUnits(in.Units); err != nil {
		return nil, problem.Validation.WithField("units", err.Error())
	}
	kind := unitKindOf(prof, in.UnitKind)
	parents, err := s.facilityParents(ctx, kind, in.Units, in.UnitParents, in.Periods[0].From)
	if err != nil {
		return nil, err
	}
	specs := make([]domain.Spec, len(in.Periods))
	for i, pd := range in.Periods {
		from, to := pd.From, pd.To
		pi := PublishInput{Profile: in.Profile, Label: pd.Label, DueAt: pd.DueAt, Units: in.Units, UnitKind: kind, UnitParents: parents,
			Period: &PeriodKey{Kind: pd.Kind, Key: pd.Key, From: &from, To: &to, Rule: pd.Rule}, MaxSubmissions: in.MaxSubmissions}
		if n := len([]rune(strings.TrimSpace(pd.Label))); n < 1 || n > 200 || pd.DueAt.IsZero() {
			return nil, problem.Validation.WithField("periods", "nhãn 1–200 ký tự và hạn nộp bắt buộc")
		}
		if specs[i], err = specOf(prof, pi); err != nil {
			return nil, err
		}
	}
	for _, u := range in.Units {
		if !a.allowsUnit(specs[0], in.Units, u) {
			return nil, problem.Forbidden.WithDetail("đơn vị " + u + " ngoài phạm vi người phát hành")
		}
	}
	now := s.Clock.Now().UTC()
	var out []port.PublishedPeriod
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
		for i, pd := range in.Periods {
			key := pd.Key
			if key == "" {
				key = pd.From.Format(time.DateOnly)
			}
			p := store.Period{ID: ids.New(), TemplateID: t.ID, Label: strings.TrimSpace(pd.Label), DueAt: pd.DueAt, Units: in.Units,
				PublisherLevel: a.level, ActorRef: a.ref, EventTime: now}
			if a.node != uuid.Nil {
				owner := a.node
				p.OwnerRef = &owner
			}
			if err := store.InsertPeriod(ctx, tx, a.tenant, &p, idemOr(idem, "period:"+key)); err != nil {
				return err
			}
			if err := store.InsertSpec(ctx, tx, a.tenant, p.ID, specs[i], a.ref, now, idemOr(idem, "spec:"+key)); err != nil {
				return err
			}
			ev := domain.Event{ID: ids.New(), Action: "published", Mode: port.ModeManual, Actor: a.ref, At: now}
			if err := store.InsertEvent(ctx, tx, a.tenant, p.ID, p.Profile, &ev, idemOr(idem, "published:"+key)); err != nil {
				return err
			}
			out = append(out, port.PublishedPeriod{ID: p.ID, Key: pd.Key, Label: p.Label, DueAt: p.DueAt})
		}
		return nil
	})
	return out, classify(err)
}

// PreviousView là bài nộp cuối của đơn vị ở kỳ gần nhất trước đó của cùng biểu ("Sao chép kỳ trước", UR-XA-13).
type PreviousView struct {
	PeriodID   uuid.UUID
	Label      string
	Submission store.Submission
	Meta       *store.Meta
}

// Previous trả bài để sao chép (nil nếu đơn vị chưa nộp kỳ nào trước). Người gọi phải thấy đơn vị trong kỳ hiện tại.
func (s *Service) Previous(ctx context.Context, profile string, periodID uuid.UUID, unit string) (*PreviousView, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	var out *PreviousView
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		p, err := store.PeriodByID(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if p == nil || p.Profile != profile || !a.allowsTemplate(p.TemplateCode) || !slices.Contains(p.Units, unit) {
			return port.ErrPeriodNotFound
		}
		spec, err := store.SpecOf(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if !a.allowsUnit(spec, p.Units, unit) {
			return problem.Forbidden.WithDetail("đơn vị " + unit + " ngoài phạm vi")
		}
		c, err := store.PreviousSubmission(ctx, tx, periodID, unit)
		if err != nil || c == nil {
			return err
		}
		sb, err := store.SubmissionByID(ctx, tx, c.SubmissionID)
		if err != nil || sb == nil {
			return err
		}
		metas, err := store.MetaOf(ctx, tx, []uuid.UUID{sb.ID})
		if err != nil {
			return err
		}
		out = &PreviousView{PeriodID: c.PeriodID, Label: c.Label, Submission: *sb}
		if m, ok := metas[sb.ID]; ok {
			out.Meta = &m
		}
		return nil
	})
	return out, err
}

// TaskFilter lọc danh sách công việc.
type TaskFilter struct {
	Code     string     // mã biểu (rỗng = mọi biểu)
	From, To *time.Time // khoảng ngày của kỳ (đặc tả)
	Limit    int        // số kỳ tối đa (mặc định 60, trần 200)
}

// Task là một công việc: một đơn vị trong phạm vi của một kỳ.
type Task struct {
	Period   store.Period
	Spec     domain.Spec
	State    domain.PeriodState
	Unit     UnitView
	Template *store.Template // phiên bản biểu của kỳ (Tasks nạp; đồng bộ nạp riêng)
}

// Tasks liệt kê công việc (kỳ × đơn vị trong phạm vi người gọi) của profile, kỳ mới trước — màn "Công việc cần làm"
// (CNTS FN-08) và theo dõi nộp của Cấp Tỉnh. Kỳ đã hủy bỏ qua.
func (s *Service) Tasks(ctx context.Context, profile string, f TaskFilter) ([]Task, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 60
	}
	now := s.Clock.Now().UTC()
	var out []Task
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		ps, err := store.PeriodsFiltered(ctx, tx, profile, f.Code, f.From, f.To, f.Limit)
		if err != nil {
			return err
		}
		for _, p := range ps {
			v, err := load(ctx, tx, p.ID, now)
			if err != nil {
				return err
			}
			if v == nil || !a.allowsTemplate(v.Period.TemplateCode) || v.State.Status == domain.PeriodCancelled {
				continue
			}
			a.filterTargets(v)
			full := a.scope.All || a.owns(v.Period)
			var tpl *store.Template
			for _, u := range v.Units {
				if (!full || a.targetFilter != nil) && !a.allowsUnit(v.Spec, v.Period.Units, u.Unit) {
					continue
				}
				if tpl == nil {
					if tpl, err = store.TemplateByID(ctx, tx, v.Period.TemplateID); err != nil {
						return err
					}
				}
				out = append(out, Task{Period: v.Period, Spec: v.Spec, State: v.State, Unit: u, Template: tpl})
			}
		}
		return nil
	})
	return out, err
}
