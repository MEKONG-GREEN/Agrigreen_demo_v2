// Package handler: HTTP của mkg/acctbridge trên API nội bộ MKG (cmd/api-internal,
// contracts/openapi/mkg-internal.yaml, tag acctbridge). Bề mặt ứng dụng: xem bút toán, tải tệp xuất,
// yêu cầu dựng lại, đẩy lại từ DLQ (accounting_bridge_operator). Bề mặt quản trị (MFA): bảng ánh xạ tài
// khoản, duyệt bút toán (chief_accountant), dựng lại bảng chiếu.
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route bề mặt ứng dụng vào nhóm /v1 (đã xác thực + Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	h.registerReads(rg)
	rg.GET("/acctbridge/journals/:id/export", h.export)
	rg.POST("/acctbridge/journals/:id/retry-push", h.retryPush)
	rg.POST("/acctbridge/periods/:id/journal-builds", h.rebuild)
	rg.GET("/acctbridge/jobs/:id", h.job)
}

// RegisterAdmin gắn route bề mặt quản trị (MFA).
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	h.registerReads(rg)
	rg.POST("/acctbridge/mappings", h.proposeMapping)
	rg.POST("/acctbridge/mappings/:id/decision", h.decideMapping)
	rg.POST("/acctbridge/journals/:id/approve", h.approve)
	rg.POST("/acctbridge/projections/rebuild", h.rebuildProjections)
}

func (h Handler) registerReads(rg *gin.RouterGroup) {
	rg.GET("/acctbridge/journals", h.list)
	rg.GET("/acctbridge/journals/:id", h.get)
	rg.GET("/acctbridge/mappings", h.mappings)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func pathID(c *gin.Context, notFound *problem.Problem) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, notFound)
		return uuid.Nil, false
	}
	return id, true
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

func status(replayed bool, fresh int) int {
	if replayed {
		return http.StatusOK
	}
	return fresh
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type lineDTO struct {
	LineNo      int          `json:"line_no"`
	Account     string       `json:"account"`
	Side        string       `json:"side"`
	Amount      money.Amount `json:"amount"`
	Category    string       `json:"category"`
	Dimension   string       `json:"dimension"`
	Description string       `json:"description"`
}

type journalDTO struct {
	JournalID           string       `json:"journal_id"`
	PeriodID            string       `json:"period_id"`
	PeriodCode          string       `json:"period_code"`
	Version             int          `json:"version"`
	Status              string       `json:"status"`
	CloseRunID          string       `json:"close_run_id"`
	SourceHash          string       `json:"source_hash"`
	LinesSHA256         string       `json:"lines_sha256"`
	MappingVersion      int          `json:"mapping_version"`
	Currency            string       `json:"currency"`
	LineCount           int          `json:"line_count"`
	TotalDebit          money.Amount `json:"total_debit"`
	TotalCredit         money.Amount `json:"total_credit"`
	BuiltBy             string       `json:"built_by"`
	BuiltAt             time.Time    `json:"built_at"`
	BuildJobID          string       `json:"build_job_id"`
	SupersedesJournalID *string      `json:"supersedes_journal_id"`
	SupersededBy        *string      `json:"superseded_by"`
	ApprovedBy          *string      `json:"approved_by"`
	ApprovedAt          *time.Time   `json:"approved_at"`
	Adapter             *string      `json:"adapter"`
	ExternalRef         *string      `json:"external_ref"`
	ArtifactSHA256      *string      `json:"artifact_sha256"`
	PushedAt            *time.Time   `json:"pushed_at"`
	PushAttempts        int          `json:"push_attempts"`
	LastFailureCode     *string      `json:"last_failure_code"`
	NextRetryAt         *time.Time   `json:"next_retry_at"`
	Lines               []lineDTO    `json:"lines,omitempty"`
}

func idPtr(u *uuid.UUID) *string {
	if u == nil {
		return nil
	}
	s := u.String()
	return &s
}

func toJournal(j store.Journal, withLines bool) journalDTO {
	d := journalDTO{JournalID: j.JournalID.String(), PeriodID: j.PeriodID.String(), PeriodCode: j.PeriodCode, Version: j.Version, Status: j.Status,
		CloseRunID: j.CloseRunID.String(), SourceHash: j.SourceHash, LinesSHA256: j.LinesSHA256, MappingVersion: j.MappingVersion,
		Currency: j.Currency, LineCount: j.LineCount, TotalDebit: j.TotalDebit, TotalCredit: j.TotalCredit, BuiltBy: j.BuiltBy.String(),
		BuiltAt: j.BuiltAt, BuildJobID: j.BuildJobID.String(), SupersedesJournalID: idPtr(j.SupersedesJournalID),
		SupersededBy: idPtr(j.SupersededBy), ApprovedBy: idPtr(j.ApprovedBy), ApprovedAt: j.ApprovedAt, Adapter: j.Adapter,
		ExternalRef: j.ExternalRef, ArtifactSHA256: j.ArtifactSHA256, PushedAt: j.PushedAt, PushAttempts: j.PushAttempts,
		LastFailureCode: j.LastFailureCode, NextRetryAt: j.NextRetryAt}
	if withLines {
		for _, l := range j.Lines {
			d.Lines = append(d.Lines, lineDTO{LineNo: l.LineNo, Account: l.Account, Side: string(l.Side), Amount: l.Amount, Category: string(l.Category),
				Dimension: l.Dimension, Description: l.Description})
		}
	}
	return d
}

type attemptDTO struct {
	Attempt        int        `json:"attempt"`
	JobID          string     `json:"job_id"`
	Adapter        string     `json:"adapter"`
	Outcome        string     `json:"outcome"`
	ErrorCode      *string    `json:"error_code"`
	ExternalRef    *string    `json:"external_ref"`
	ArtifactSHA256 *string    `json:"artifact_sha256"`
	NextRetryAt    *time.Time `json:"next_retry_at"`
	StartedAt      time.Time  `json:"started_at"`
	FinishedAt     time.Time  `json:"finished_at"`
}

type jobDTO struct {
	JobID              string     `json:"job_id"`
	Kind               string     `json:"kind"`
	Status             string     `json:"status"`
	PeriodID           string     `json:"period_id"`
	PeriodCode         string     `json:"period_code"`
	CloseRunID         string     `json:"close_run_id"`
	MappingVersion     *int       `json:"mapping_version"`
	JournalID          *string    `json:"journal_id"`
	Attempt            *int       `json:"attempt"`
	FailureCode        *string    `json:"failure_code"`
	RequestedBy        string     `json:"requested_by"`
	RequestedAt        time.Time  `json:"requested_at"`
	CompletedAt        *time.Time `json:"completed_at"`
	ExpectedSourceHash string     `json:"expected_source_hash"`
}

func toJob(j store.Job) jobDTO {
	return jobDTO{JobID: j.JobID.String(), Kind: j.Kind, Status: j.Status, PeriodID: j.PeriodID.String(), PeriodCode: j.PeriodCode,
		CloseRunID: j.CloseRunID.String(), MappingVersion: j.MappingVersion, JournalID: idPtr(j.JournalID), Attempt: j.Attempt,
		FailureCode: j.FailureCode, RequestedBy: j.RequestedBy.String(), RequestedAt: j.RequestedAt, CompletedAt: j.CompletedAt,
		ExpectedSourceHash: j.ExpectedSourceHash}
}

type mappingDTO struct {
	VersionID      string        `json:"version_id"`
	VersionSeq     int           `json:"version"`
	Status         string        `json:"status"`
	Rules          []domain.Rule `json:"rules"`
	RulesSHA256    string        `json:"rules_sha256"`
	EffectiveFrom  string        `json:"effective_from"`
	Provisional    bool          `json:"provisional"`
	ProposedBy     string        `json:"proposed_by"`
	ProposalReason string        `json:"proposal_reason"`
	ProposedAt     time.Time     `json:"proposed_at"`
	DecidedBy      *string       `json:"decided_by"`
	DecidedAt      *time.Time    `json:"decided_at"`
	DecisionReason *string       `json:"decision_reason"`
}

func toMapping(m store.Mapping) mappingDTO {
	return mappingDTO{VersionID: m.VersionID.String(), VersionSeq: m.VersionSeq, Status: m.Status, Rules: m.Rules, RulesSHA256: m.RulesSHA256,
		EffectiveFrom: m.EffectiveFrom.Format("2006-01-02"), Provisional: m.Provisional, ProposedBy: m.ProposedBy.String(),
		ProposalReason: m.ProposalReason, ProposedAt: m.ProposedAt, DecidedBy: idPtr(m.DecidedBy), DecidedAt: m.DecidedAt,
		DecisionReason: m.DecisionReason}
}

// ---------------------------------------------------------------------------
// Bút toán
// ---------------------------------------------------------------------------

func (h Handler) list(c *gin.Context) {
	var f store.JournalFilter
	if s := c.Query("period_id"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("period_id", "phải là UUID"))
			return
		}
		f.PeriodID = &id
	}
	if s := c.Query("cursor"); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "con trỏ không hợp lệ"))
			return
		}
		f.Before = &id
	}
	f.Status = c.Query("status")
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	items, next, err := h.Svc.Journals(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]journalDTO, 0, len(items))
	for _, j := range items {
		out = append(out, toJournal(j, false))
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": idPtr(next)})
}

func (h Handler) get(c *gin.Context) {
	id, ok := pathID(c, app.ErrJournalNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Journal(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	atts := make([]attemptDTO, 0, len(v.Attempts))
	for _, a := range v.Attempts {
		atts = append(atts, attemptDTO{Attempt: a.Attempt, JobID: a.JobID.String(), Adapter: a.Adapter, Outcome: a.Outcome, ErrorCode: a.ErrorCode,
			ExternalRef: a.ExternalRef, ArtifactSHA256: a.ArtifactSHA256, NextRetryAt: a.NextRetryAt, StartedAt: a.StartedAt, FinishedAt: a.FinishedAt})
	}
	c.JSON(http.StatusOK, gin.H{"journal": toJournal(v.Journal, true), "push_attempts": atts})
}

func (h Handler) export(c *gin.Context) {
	id, ok := pathID(c, app.ErrJournalNotFound)
	if !ok {
		return
	}
	format := c.DefaultQuery("format", domain.FormatCSV)
	x, err := h.Svc.Export(c.Request.Context(), id, format)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+x.Filename+`"`)
	c.Header("X-Content-SHA256", x.SHA256)
	c.Data(http.StatusOK, x.ContentType, x.Body)
}

func (h Handler) approve(c *gin.Context) {
	id, ok := pathID(c, app.ErrJournalNotFound)
	if !ok {
		return
	}
	r, replayed, err := h.Svc.Approve(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/acctbridge/jobs/"+r.Job.JobID.String())
	c.JSON(status(replayed, http.StatusAccepted), gin.H{"journal": toJournal(r.Journal, false), "job": toJob(r.Job)})
}

func (h Handler) retryPush(c *gin.Context) {
	id, ok := pathID(c, app.ErrJournalNotFound)
	if !ok {
		return
	}
	r, replayed, err := h.Svc.RetryPush(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/acctbridge/jobs/"+r.Job.JobID.String())
	c.JSON(status(replayed, http.StatusAccepted), gin.H{"journal": toJournal(r.Journal, false), "job": toJob(r.Job)})
}

func (h Handler) rebuild(c *gin.Context) {
	id, ok := pathID(c, app.ErrPeriodUnknown)
	if !ok {
		return
	}
	j, replayed, err := h.Svc.RequestRebuild(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/acctbridge/jobs/"+j.JobID.String())
	c.JSON(status(replayed, http.StatusAccepted), gin.H{"job": toJob(j)})
}

func (h Handler) job(c *gin.Context) {
	id, ok := pathID(c, app.ErrJobNotFound)
	if !ok {
		return
	}
	j, err := h.Svc.Job(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"job": toJob(j)})
}

func (h Handler) rebuildProjections(c *gin.Context) {
	r, err := h.Svc.RebuildProjections(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"mapping_events": r.MappingEvents, "journal_events": r.JournalEvents, "journals": r.Journals})
}

// ---------------------------------------------------------------------------
// Ánh xạ tài khoản
// ---------------------------------------------------------------------------

func (h Handler) mappings(c *gin.Context) {
	v, err := h.Svc.Mappings(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]mappingDTO, 0, len(v.Versions))
	for _, m := range v.Versions {
		out = append(out, toMapping(m))
	}
	var eff *mappingDTO
	if v.Effective != nil {
		d := toMapping(*v.Effective)
		eff = &d
	}
	c.JSON(http.StatusOK, gin.H{"versions": out, "effective": eff, "default_rules": v.Default, "default_provisional": true})
}

type proposeReq struct {
	Rules         []domain.Rule `json:"rules"`
	EffectiveFrom string        `json:"effective_from"`
	Provisional   *bool         `json:"provisional"`
	Reason        string        `json:"reason"`
}

func (h Handler) proposeMapping(c *gin.Context) {
	var r proposeReq
	if !bind(c, &r) {
		return
	}
	res, replayed, err := h.Svc.ProposeMapping(c.Request.Context(), app.MappingInput{Rules: r.Rules, EffectiveFrom: r.EffectiveFrom,
		Provisional: r.Provisional, Reason: r.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(replayed, http.StatusCreated), gin.H{"mapping": toMapping(res.Mapping)})
}

type decideReq struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func (h Handler) decideMapping(c *gin.Context) {
	id, ok := pathID(c, app.ErrMappingNotFound)
	if !ok {
		return
	}
	var r decideReq
	if !bind(c, &r) {
		return
	}
	if r.Decision != "approved" && r.Decision != "rejected" {
		httpx.Fail(c, problem.Validation.WithField("decision", "approved | rejected"))
		return
	}
	res, replayed, err := h.Svc.DecideMapping(c.Request.Context(), id, r.Decision == "approved", r.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status(replayed, http.StatusCreated), gin.H{"mapping": toMapping(res.Mapping)})
}
