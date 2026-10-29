// Package handler: HTTP của pf/forms (`cap/forms`) — Application API /v1/forms/*. Hợp đồng ở
// contracts/openapi/application.yaml, mục "# --- pf/forms ---". Bên dùng chọn bằng tham số query `profile`
// (POST tạo biểu/kỳ: trường `profile` của thân). Route v1 cũ của KN (/v1/extension/report-*) chạy song song.
package handler

import (
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/forms/profiles", h.profiles)
	rg.GET("/forms/templates", h.templates)
	rg.POST("/forms/templates", h.define)
	rg.POST("/forms/templates/fixtures", h.fixtures)
	rg.GET("/forms/templates/:id", h.template)
	rg.GET("/forms/periods", h.periods)
	rg.POST("/forms/periods", h.publish)
	rg.GET("/forms/periods/:id", h.period)
	rg.POST("/forms/periods/:id/submissions", h.submit)
	rg.GET("/forms/periods/:id/submissions", h.submissions)
	rg.POST("/forms/periods/:id/close", h.close)
	rg.POST("/forms/periods/:id/reopen", h.reopen)
	rg.POST("/forms/periods/:id/cancel", h.cancel)
	rg.GET("/forms/periods/:id/snapshots", h.snapshots)
	rg.POST("/forms/submissions/:id/review", h.review)
	rg.GET("/forms/snapshots/:id", h.snapshot)
	// W11.42: công việc (kỳ × đơn vị trong phạm vi), sao chép kỳ trước.
	rg.GET("/forms/tasks", h.tasks)
	rg.GET("/forms/periods/:id/previous", h.previous)
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type profileDTO struct {
	Name                       string            `json:"name"`
	Hierarchy                  []string          `json:"hierarchy"`
	Roles                      map[string]string `json:"roles"`
	PublishBy                  string            `json:"publish_by"`
	CloseBy                    string            `json:"close_by"`
	ReviewBy                   string            `json:"review_by"`
	AllowFixtures              bool              `json:"allow_fixtures"`
	RequireReviewedBeforeClose bool              `json:"require_reviewed_before_close"`
	UnitKinds                  []string          `json:"unit_kinds"`
	Departments                []string          `json:"departments"`
	MaxSubmissions             int               `json:"max_submissions"`
}

type templateDTO struct {
	ID        string         `json:"id"`
	Profile   string         `json:"profile"`
	Code      string         `json:"code"`
	Version   int64          `json:"version"`
	Title     string         `json:"title"`
	Fields    []domain.Field `json:"fields"`
	Fixture   bool           `json:"fixture"`
	CreatedBy string         `json:"created_by"`
	CreatedAt string         `json:"created_at"`
}

func templateOf(t store.Template) templateDTO {
	return templateDTO{ID: t.ID.String(), Profile: t.Profile, Code: t.Code, Version: t.Version, Title: t.Title, Fields: t.Fields,
		Fixture: t.Fixture, CreatedBy: t.ActorRef.String(), CreatedAt: ts(t.EventTime)}
}

type periodDTO struct {
	ID              string   `json:"id"`
	Profile         string   `json:"profile"`
	TemplateID      string   `json:"template_id"`
	TemplateCode    string   `json:"template_code"`
	TemplateVersion int64    `json:"template_version"`
	Label           string   `json:"label"`
	DueAt           string   `json:"due_at"`
	Units           []string `json:"units"`
	OwnerRef        *string  `json:"owner_ref"`
	PublisherLevel  string   `json:"publisher_level"`
	PublishedBy     string   `json:"published_by"`
	PublishedAt     string   `json:"published_at"`
}

func periodOf(p store.Period) periodDTO {
	d := periodDTO{ID: p.ID.String(), Profile: p.Profile, TemplateID: p.TemplateID.String(), TemplateCode: p.TemplateCode,
		TemplateVersion: p.TemplateVersion, Label: p.Label, DueAt: ts(p.DueAt), Units: p.Units, PublisherLevel: p.PublisherLevel,
		PublishedBy: p.ActorRef.String(), PublishedAt: ts(p.EventTime)}
	if p.OwnerRef != nil {
		s := p.OwnerRef.String()
		d.OwnerRef = &s
	}
	return d
}

type eventDTO struct {
	ID     string   `json:"id"`
	Seq    int64    `json:"seq"`
	Action string   `json:"action"`
	Mode   string   `json:"mode"`
	Units  []string `json:"units"`
	Reason *string  `json:"reason"`
	Actor  string   `json:"actor_ref"`
	At     string   `json:"event_time"`
}

type reviewDTO struct {
	ID       string  `json:"id"`
	Decision string  `json:"decision"`
	Reviewer string  `json:"reviewer_ref"`
	Note     *string `json:"note"`
	At       string  `json:"event_time"`
}

func reviewOf(r *domain.Review) *reviewDTO {
	if r == nil {
		return nil
	}
	d := &reviewDTO{ID: r.ID.String(), Decision: r.Decision, Reviewer: r.Reviewer.String(), At: ts(r.At)}
	if r.Note != "" {
		d.Note = &r.Note
	}
	return d
}

type submissionDTO struct {
	ID        string            `json:"id"`
	PeriodID  string            `json:"period_id"`
	Unit      string            `json:"unit"`
	Version   int64             `json:"version"`
	Values    map[string]string `json:"values"`
	Submitter string            `json:"submitter_ref"`
	At        string            `json:"event_time"`
	Status    string            `json:"status"`
	Review    *reviewDTO        `json:"review"`
	// W11.42 (sổ phụ submission_meta; phiên bản trước pf 000149 → mặc định).
	NilReport   bool    `json:"nil_report"`
	Note        *string `json:"note"`
	Explanation *string `json:"explanation"`
	Late        bool    `json:"late"`
	CapturedAt  *string `json:"captured_at"`
}

func submissionOf(s store.Submission, status string, r *domain.Review, m ...*store.Meta) submissionDTO {
	d := submissionDTO{ID: s.ID.String(), PeriodID: s.PeriodID.String(), Unit: s.Unit, Version: s.Version, Values: s.Values,
		Submitter: s.Submitter.String(), At: ts(s.EventTime), Status: status, Review: reviewOf(r)}
	if len(m) > 0 && m[0] != nil {
		x := m[0]
		d.NilReport, d.Late = x.NilReport, x.Late
		if x.Note != "" {
			d.Note = &x.Note
		}
		if x.Explanation != "" {
			d.Explanation = &x.Explanation
		}
		if x.CapturedAt != nil {
			c := ts(*x.CapturedAt)
			d.CapturedAt = &c
		}
	}
	return d
}

type unitDTO struct {
	Unit       string         `json:"unit"`
	Parent     string         `json:"parent"`
	Status     string         `json:"status"`
	Submission *submissionDTO `json:"submission"`
	Held       *heldDTO       `json:"held"`
}

type heldDTO struct {
	ID         string            `json:"id"`
	Version    int64             `json:"version"`
	Values     map[string]string `json:"values"`
	Submitter  string            `json:"submitter_ref"`
	CapturedAt string            `json:"captured_at"`
	At         string            `json:"event_time"`
}

type specDTO struct {
	UnitKind       string   `json:"unit_kind"`
	UnitParents    []string `json:"unit_parents"`
	MaxSubmissions int      `json:"max_submissions"`
	ReviewFlow     string   `json:"review_flow"`
	PeriodKind     *string  `json:"period_kind"`
	PeriodKey      *string  `json:"period_key"`
	PeriodFrom     *string  `json:"period_from"`
	PeriodTo       *string  `json:"period_to"`
	PeriodRule     *string  `json:"period_rule"`
}

func specOf(s domain.Spec) specDTO {
	opt := func(v string) *string {
		if v == "" {
			return nil
		}
		return &v
	}
	day := func(t *time.Time) *string {
		if t == nil {
			return nil
		}
		v := t.UTC().Format(time.DateOnly)
		return &v
	}
	return specDTO{UnitKind: s.UnitKind, UnitParents: nonNil(s.UnitParents), MaxSubmissions: s.MaxSubmissions, ReviewFlow: s.ReviewFlow,
		PeriodKind: opt(s.PeriodKind), PeriodKey: opt(s.PeriodKey), PeriodFrom: day(s.From), PeriodTo: day(s.To), PeriodRule: opt(s.PeriodRule)}
}

type snapshotDTO struct {
	ID            string          `json:"id"`
	PeriodRef     string          `json:"period_ref"`
	Profile       string          `json:"profile"`
	SnapshotNo    int64           `json:"snapshot_no"`
	PeriodEventID string          `json:"period_event_id"`
	ContentHash   string          `json:"content_hash"`
	HashValid     bool            `json:"hash_valid"`
	RuleVersion   string          `json:"rule_version"`
	ActorRef      string          `json:"actor_ref"`
	At            string          `json:"event_time"`
	Content       json.RawMessage `json:"content,omitempty"`
}

func snapshotOf(s store.Snapshot) snapshotDTO {
	return snapshotDTO{ID: s.ID.String(), PeriodRef: s.PeriodRef.String(), Profile: s.Profile, SnapshotNo: s.No,
		PeriodEventID: s.PeriodEventID.String(), ContentHash: hex.EncodeToString(s.Hash), HashValid: s.HashValid,
		RuleVersion: s.RuleVersion, ActorRef: s.ActorRef.String(), At: ts(s.EventTime), Content: s.Content}
}

type periodViewDTO struct {
	Period      periodDTO     `json:"period"`
	Spec        specDTO       `json:"spec"`
	Status      string        `json:"status"`
	ReopenUnits []string      `json:"reopen_units"`
	Events      []eventDTO    `json:"events"`
	Units       []unitDTO     `json:"units"`
	Snapshots   []snapshotDTO `json:"snapshots"`
}

func viewOf(v app.PeriodView) periodViewDTO {
	d := periodViewDTO{Period: periodOf(v.Period), Spec: specOf(v.Spec), Status: v.State.Status, ReopenUnits: nonNil(v.State.ReopenUnits), Events: []eventDTO{},
		Units: []unitDTO{}, Snapshots: []snapshotDTO{}}
	for _, e := range v.Events {
		ed := eventDTO{ID: e.ID.String(), Seq: e.Seq, Action: e.Action, Mode: e.Mode, Units: nonNil(e.Units), Actor: e.Actor.String(), At: ts(e.At)}
		if e.Reason != "" {
			r := e.Reason
			ed.Reason = &r
		}
		d.Events = append(d.Events, ed)
	}
	for _, u := range v.Units {
		ud := unitDTO{Unit: u.Unit, Parent: u.Parent, Status: u.Status}
		if u.Submission != nil {
			sd := submissionOf(*u.Submission, domain.SubmissionStatus(true, u.Review), u.Review, u.Meta)
			ud.Submission = &sd
		}
		if h := u.Held; h != nil {
			ud.Held = &heldDTO{ID: h.ID.String(), Version: h.Seq, Values: h.Values, Submitter: h.Submitter.String(), CapturedAt: ts(h.CapturedAt),
				At: ts(h.EventTime)}
		}
		d.Units = append(d.Units, ud)
	}
	for _, s := range v.Snapshots {
		d.Snapshots = append(d.Snapshots, snapshotOf(s))
	}
	return d
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

func (h Handler) profiles(c *gin.Context) {
	if _, ok := authz.FromContext(c.Request.Context()); !ok {
		httpx.Fail(c, problem.Unauthenticated)
		return
	}
	out := []profileDTO{}
	for _, p := range h.Svc.Profiles() {
		out = append(out, profileDTO{Name: p.Name, Hierarchy: p.Hierarchy, Roles: p.Roles, PublishBy: p.PublishBy, CloseBy: p.CloseBy,
			ReviewBy: p.Reviewer(), AllowFixtures: p.AllowFixtures, RequireReviewedBeforeClose: p.RequireReviewedBeforeClose,
			UnitKinds: p.Kinds(), Departments: nonNil(p.Departments), MaxSubmissions: p.MaxSubmissions})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) templates(c *gin.Context) {
	ts, err := h.Svc.Templates(c.Request.Context(), c.Query("profile"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]templateDTO, 0, len(ts))
	for _, t := range ts {
		out = append(out, templateOf(t))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type defineReq struct {
	Profile string         `json:"profile"`
	Code    string         `json:"code"`
	Title   string         `json:"title"`
	Fields  []domain.Field `json:"fields"`
	Fixture bool           `json:"fixture"`
}

func (h Handler) define(c *gin.Context) {
	var req defineReq
	if !bindJSON(c, &req) {
		return
	}
	t, err := h.Svc.DefineTemplate(c.Request.Context(), app.TemplateInput{Profile: req.Profile, Code: req.Code, Title: req.Title,
		Fields: req.Fields, Fixture: req.Fixture}, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, templateOf(t))
}

func (h Handler) fixtures(c *gin.Context) {
	ts, err := h.Svc.InstallFixtures(c.Request.Context(), c.Query("profile"), idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]templateDTO, 0, len(ts))
	for _, t := range ts {
		out = append(out, templateOf(t))
	}
	c.JSON(http.StatusCreated, gin.H{"items": out})
}

func (h Handler) template(c *gin.Context) {
	id, ok := pathID(c, port.ErrTemplateNotFound)
	if !ok {
		return
	}
	t, err := h.Svc.Template(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, templateOf(t))
}

func (h Handler) periods(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	ps, err := h.Svc.Periods(c.Request.Context(), c.Query("profile"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]periodDTO, 0, len(ps))
	for _, p := range ps {
		out = append(out, periodOf(p))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type publishReq struct {
	Profile        string   `json:"profile"`
	TemplateID     string   `json:"template_id"`
	Label          string   `json:"label"`
	DueAt          string   `json:"due_at"`
	Units          []string `json:"units"`
	UnitKind       string   `json:"unit_kind"`
	UnitParents    []string `json:"unit_parents"`
	MaxSubmissions *int     `json:"max_submissions"`
	Period         *struct {
		Kind string `json:"kind"`
		Key  string `json:"key"`
		From string `json:"from"`
		To   string `json:"to"`
		Rule string `json:"rule"`
	} `json:"period"`
}

func (h Handler) publish(c *gin.Context) {
	var req publishReq
	if !bindJSON(c, &req) {
		return
	}
	tid, err := uuid.Parse(req.TemplateID)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("template_id", "UUID"))
		return
	}
	due, err := time.Parse(time.RFC3339, req.DueAt)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("due_at", "RFC 3339"))
		return
	}
	in := app.PublishInput{Profile: req.Profile, TemplateID: tid, Label: req.Label, DueAt: due, Units: req.Units, UnitKind: req.UnitKind,
		UnitParents: req.UnitParents, MaxSubmissions: req.MaxSubmissions}
	if pk := req.Period; pk != nil {
		in.Period = &app.PeriodKey{Kind: pk.Kind, Key: pk.Key, Rule: pk.Rule}
		for _, x := range []struct {
			field, raw string
			dst        **time.Time
		}{{"period.from", pk.From, &in.Period.From}, {"period.to", pk.To, &in.Period.To}} {
			if x.raw == "" {
				continue
			}
			d, err := time.Parse(time.DateOnly, x.raw)
			if err != nil {
				httpx.Fail(c, problem.Validation.WithField(x.field, "YYYY-MM-DD"))
				return
			}
			*x.dst = &d
		}
	}
	v, err := h.Svc.Publish(c.Request.Context(), in, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, viewOf(v))
}

func (h Handler) period(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Period(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, viewOf(v))
}

type submitReq struct {
	Unit   string            `json:"unit"`
	Values map[string]string `json:"values"`
	// CapturedAt: bản offline (lúc ghi trên thiết bị, RFC 3339) — kỳ đã chốt thì bài được giữ "Chờ mở kỳ" (202).
	CapturedAt string `json:"captured_at"`
	// W11.42: khóa lạc quan (phiên bản đã thấy, 0 = chưa có bài), báo không phát sinh, ghi chú (H08), giải trình (H09).
	BaseVersion *int64 `json:"base_version"`
	NilReport   bool   `json:"nil_report"`
	Note        string `json:"note"`
	Explanation string `json:"explanation"`
}

func (h Handler) submit(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	var req submitReq
	if !bindJSON(c, &req) {
		return
	}
	in := app.SubmitInput{Unit: req.Unit, Values: req.Values, BaseVersion: req.BaseVersion, NilReport: req.NilReport, Note: req.Note,
		Explanation: req.Explanation}
	if req.CapturedAt != "" {
		at, perr := time.Parse(time.RFC3339, req.CapturedAt)
		if perr != nil {
			httpx.Fail(c, problem.Validation.WithField("captured_at", "RFC 3339"))
			return
		}
		in.CapturedAt = &at
	}
	s, err := h.Svc.SubmitWith(c.Request.Context(), c.Query("profile"), id, in, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	code := http.StatusCreated
	if s.Status == domain.SubAwaitingReopen {
		code = http.StatusAccepted
	}
	c.JSON(code, submissionOf(s.Submission, s.Status, s.Review, s.Meta))
}

func (h Handler) submissions(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	ss, err := h.Svc.Submissions(c.Request.Context(), c.Query("profile"), id, c.Query("unit"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]submissionDTO, 0, len(ss))
	for _, s := range ss {
		out = append(out, submissionOf(s.Submission, s.Status, s.Review))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type reviewReq struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (h Handler) review(c *gin.Context) {
	id, ok := pathID(c, port.ErrSubmissionMissing)
	if !ok {
		return
	}
	var req reviewReq
	if !bindJSON(c, &req) {
		return
	}
	s, err := h.Svc.Review(c.Request.Context(), c.Query("profile"), id, req.Decision, req.Note, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, submissionOf(s.Submission, s.Status, s.Review))
}

func (h Handler) close(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Close(c.Request.Context(), c.Query("profile"), id, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, viewOf(v))
}

type reopenReq struct {
	Reason string   `json:"reason"`
	Units  []string `json:"units"`
}

func (h Handler) reopen(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	var req reopenReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.Svc.Reopen(c.Request.Context(), c.Query("profile"), id, app.ReopenInput{Units: req.Units, Reason: req.Reason}, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, viewOf(v))
}

type cancelReq struct {
	Reason string `json:"reason"`
}

func (h Handler) cancel(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	var req cancelReq
	if !bindJSON(c, &req) {
		return
	}
	v, err := h.Svc.Cancel(c.Request.Context(), c.Query("profile"), id, req.Reason, idemKey(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, viewOf(v))
}

func (h Handler) snapshots(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	ss, err := h.Svc.Snapshots(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]snapshotDTO, 0, len(ss))
	for _, s := range ss {
		out = append(out, snapshotOf(s))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) snapshot(c *gin.Context) {
	id, ok := pathID(c, port.ErrSnapshotNotFound)
	if !ok {
		return
	}
	s, err := h.Svc.Snapshot(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, snapshotOf(s))
}

func (h Handler) tasks(c *gin.Context) {
	f := app.TaskFilter{Code: c.Query("code"), Cursor: c.Query("cursor")}
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	for _, x := range []struct {
		field string
		dst   **time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		raw := c.Query(x.field)
		if raw == "" {
			continue
		}
		d, err := time.Parse(time.DateOnly, raw)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField(x.field, "YYYY-MM-DD"))
			return
		}
		*x.dst = &d
	}
	pg, err := h.Svc.TasksPage(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]app.TaskData, 0, len(pg.Items))
	for _, t := range pg.Items {
		out = append(out, app.TaskDataOf(t, t.Template))
	}
	var next *string
	if pg.NextCursor != "" {
		next = &pg.NextCursor
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": next})
}

type previousDTO struct {
	PeriodID   string        `json:"period_id"`
	Label      string        `json:"label"`
	Submission submissionDTO `json:"submission"`
}

func (h Handler) previous(c *gin.Context) {
	id, ok := pathID(c, port.ErrPeriodNotFound)
	if !ok {
		return
	}
	p, err := h.Svc.Previous(c.Request.Context(), c.Query("profile"), id, c.Query("unit"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if p == nil {
		c.JSON(http.StatusOK, gin.H{"previous": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"previous": previousDTO{PeriodID: p.PeriodID.String(), Label: p.Label,
		Submission: submissionOf(p.Submission, domain.SubSubmitted, nil, p.Meta)}})
}

// ---------------------------------------------------------------------------
// Tiện ích
// ---------------------------------------------------------------------------

func pathID(c *gin.Context, notFound *problem.Problem) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, notFound)
		return id, false
	}
	return id, true
}

func bindJSON(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func idemKey(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func nonNil(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}
