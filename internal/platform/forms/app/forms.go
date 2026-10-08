package app

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
	if t == nil || t.Profile != profile {
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
}

// UnitView là trạng thái bài nộp của một đơn vị trong kỳ.
type UnitView struct {
	Unit       string
	Status     string
	Submission *store.Submission
	Review     *domain.Review
}

// PeriodView là kỳ kèm trạng thái chiếu từ sổ.
type PeriodView struct {
	Period    store.Period
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
	for _, u := range in.Units {
		if !a.scope.Allows(u) {
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
		if t == nil || t.Profile != in.Profile {
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
		ev := domain.Event{ID: ids.New(), Action: "published", Mode: port.ModeManual, Actor: a.ref, At: now}
		return store.InsertEvent(ctx, tx, a.tenant, p.ID, p.Profile, &ev, idemOr(idem, "published"))
	})
	if err != nil {
		return PeriodView{}, classify(err)
	}
	return s.Period(ctx, in.Profile, id)
}

// owns: người gọi thuộc Nút phát hành của kỳ.
func (a actor) owns(p store.Period) bool {
	return a.node != uuid.Nil && p.OwnerRef != nil && *p.OwnerRef == a.node
}

// canSee: phạm vi toàn quốc, Nút chủ của kỳ, hoặc có ít nhất một đơn vị nhận trong phạm vi.
func (a actor) canSee(p store.Period) bool {
	if a.scope.All || a.owns(p) {
		return true
	}
	return slices.ContainsFunc(p.Units, a.scope.Allows)
}

// canClose: cấp close_by và (toàn quốc hoặc Nút chủ của kỳ).
func (a actor) canClose(prof string, p store.Period) bool {
	return a.level == prof && (a.scope.All || a.owns(p))
}

// load đọc kỳ + sự kiện + bài nộp cuối + rà soát + ảnh chụp trong tx.
func load(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*PeriodView, error) {
	p, err := store.PeriodByID(ctx, tx, id)
	if err != nil || p == nil {
		return nil, err
	}
	v := &PeriodView{Period: *p}
	if v.Events, err = store.Events(ctx, tx, id); err != nil {
		return nil, err
	}
	v.State = domain.StateOf(v.Events)
	subs, err := store.LatestSubmissions(ctx, tx, id)
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
	for _, u := range p.Units {
		uv := UnitView{Unit: u}
		if sb, ok := subs[u]; ok {
			sb := sb
			uv.Submission = &sb
			if r, ok := revs[sb.ID]; ok {
				r := r
				uv.Review = &r
			}
		}
		uv.Status = domain.SubmissionStatus(uv.Submission != nil, uv.Review)
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
		v, e = load(ctx, tx, id)
		return e
	})
	if err != nil {
		return PeriodView{}, err
	}
	if v == nil || v.Period.Profile != profile || !a.canSee(v.Period) {
		return PeriodView{}, port.ErrPeriodNotFound
	}
	if !a.scope.All && !a.owns(v.Period) {
		v.Units = slices.DeleteFunc(v.Units, func(u UnitView) bool { return !a.scope.Allows(u.Unit) })
	}
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
		for _, p := range ps {
			if a.canSee(p) {
				out = append(out, p)
			}
		}
		return e
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
}

// Submit nộp phiên bản mới của bài (cấp lá, đơn vị trong phạm vi): kỳ phải đang nhận bài của đơn vị, số liệu khớp
// biểu. Lập yêu cầu duyệt ở cap/workflow (rà soát bốn mắt) cùng giao dịch.
func (s *Service) Submit(ctx context.Context, profile string, periodID uuid.UUID, unit string, values map[string]string, idem string) (SubmissionView, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return SubmissionView{}, err
	}
	if err := a.need(prof.Leaf(), "nộp bài"); err != nil {
		return SubmissionView{}, err
	}
	if !domain.ValidateUnit(unit) {
		return SubmissionView{}, problem.Validation.WithField("unit", "mã đơn vị không hợp lệ")
	}
	if !a.scope.Allows(unit) {
		return SubmissionView{}, problem.Forbidden.WithDetail("đơn vị " + unit + " ngoài phạm vi")
	}
	if values == nil {
		values = map[string]string{}
	}
	now := s.Clock.Now().UTC()
	var out SubmissionView
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		if err := store.Lock(ctx, tx, periodID); err != nil {
			return err
		}
		v, err := load(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile {
			return port.ErrPeriodNotFound
		}
		if !slices.Contains(v.Period.Units, unit) {
			return port.ErrUnitNotInPeriod
		}
		if !v.State.AcceptsUnit(unit) {
			if v.State.Status == domain.PeriodReopened {
				return port.ErrUnitNotInPeriod
			}
			return port.ErrPeriodClosed
		}
		i := slices.IndexFunc(v.Units, func(u UnitView) bool { return u.Unit == unit })
		cur := v.Units[i]
		var acceptedAt *time.Time
		if cur.Review != nil && cur.Review.Decision == domain.SubAccepted {
			acceptedAt = &cur.Review.At
		}
		if !domain.CanResubmit(cur.Status, acceptedAt, v.State) {
			return port.ErrSubmissionLocked
		}
		t, err := store.TemplateByID(ctx, tx, v.Period.TemplateID)
		if err != nil {
			return err
		}
		if t == nil {
			return port.ErrTemplateNotFound
		}
		if err := domain.ValidateValues(t.Fields, values); err != nil {
			var te domain.ErrTemplate
			if errorsAs(err, &te) {
				return port.ErrInvalidValues.WithField(te.Field, te.Msg)
			}
			return port.ErrInvalidValues.WithDetail(err.Error())
		}
		sb := store.Submission{ID: ids.New(), PeriodID: periodID, Unit: unit, Values: values, Submitter: a.ref, EventTime: now}
		if err := store.InsertSubmission(ctx, tx, a.tenant, &sb, idemOr(idem, "submission")); err != nil {
			return err
		}
		if s.Approvals != nil {
			if _, err := s.Approvals.Submit(db.JoinTx(ctx, tx), wfport.SubmitInput{Kind: WorkflowKind, Subject: sb.ID, Requester: a.ref,
				Summary: "forms " + profile + " " + t.Code + " " + unit, At: now}); err != nil {
				return err
			}
		}
		out = SubmissionView{Submission: sb, Status: domain.SubSubmitted}
		return nil
	})
	return out, classify(err)
}

// Submissions trả lịch sử phiên bản bài nộp của một đơn vị (mới trước) kèm rà soát.
func (s *Service) Submissions(ctx context.Context, profile string, periodID uuid.UUID, unit string) ([]SubmissionView, error) {
	_, a, err := s.resolve(ctx, profile)
	if err != nil {
		return nil, err
	}
	if !a.scope.Allows(unit) {
		return nil, problem.Forbidden.WithDetail("đơn vị " + unit + " ngoài phạm vi")
	}
	var out []SubmissionView
	err = s.tx(ctx, a.tenant, func(tx pgx.Tx) error {
		p, err := store.PeriodByID(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if p == nil || p.Profile != profile {
			return port.ErrPeriodNotFound
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
		v, err := load(ctx, tx, sb.PeriodID)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.scope.Allows(sb.Unit) {
			return port.ErrSubmissionMissing
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
		v, err := load(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.canSee(v.Period) {
			return port.ErrPeriodNotFound
		}
		if !a.canClose(prof.CloseBy, v.Period) {
			return problem.Forbidden.WithDetail("chốt kỳ cần cấp " + prof.CloseBy + " của Nút phát hành")
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
		us := domain.UnitSnapshot{Unit: u.Unit, Status: u.Status, Values: map[string]string{}}
		if u.Submission != nil {
			id, by := u.Submission.ID.String(), u.Submission.Submitter.String()
			us.SubmissionID, us.Version, us.Values, us.SubmittedBy = &id, u.Submission.Version, u.Submission.Values, &by
		}
		if u.Review != nil {
			by := u.Review.Reviewer.String()
			us.ReviewedBy = &by
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

// Reopen mở lại kỳ đã chốt — sự kiện mới (lý do bắt buộc), ảnh chụp đã chốt không đổi.
func (s *Service) Reopen(ctx context.Context, profile string, periodID uuid.UUID, in ReopenInput, idem string) (PeriodView, error) {
	prof, a, err := s.resolve(ctx, profile)
	if err != nil {
		return PeriodView{}, err
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
		v, err := load(ctx, tx, periodID)
		if err != nil {
			return err
		}
		if v == nil || v.Period.Profile != profile || !a.canSee(v.Period) {
			return port.ErrPeriodNotFound
		}
		if !a.canClose(prof.CloseBy, v.Period) {
			return problem.Forbidden.WithDetail("mở lại kỳ cần cấp " + prof.CloseBy + " của Nút phát hành")
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
		if (p != nil && a.canSee(*p)) || (p == nil && a.level != "" && a.level != prof.Leaf()) {
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
		if p != nil && (p.Profile != profile || !a.canSee(*p)) {
			return port.ErrPeriodNotFound
		}
		if p == nil && (a.level == "" || a.level == prof.Leaf()) {
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

// enqueueClosed ghi sự kiện pf.forms.period.closed vào outbox trong giao dịch tx.
func enqueueClosed(ctx context.Context, tx pgx.Tx, tid, periodRef uuid.UUID, profile, origin, mode string, snapID uuid.UUID, snapNo int64,
	hash []byte, actor uuid.UUID, at time.Time) error {
	return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: tid, Type: port.EventPeriodClosed, SchemaVersion: 1, OccurredAt: at,
		Producer: port.EventProducerName, Payload: port.PeriodClosedV1{PeriodRef: periodRef.String(), Profile: profile, Origin: origin, Mode: mode,
			SnapshotID: snapID.String(), SnapshotNo: snapNo, ContentHash: hexOf(hash), ActorRef: actor.String(), EventTime: at.Format(time.RFC3339Nano)}})
}
