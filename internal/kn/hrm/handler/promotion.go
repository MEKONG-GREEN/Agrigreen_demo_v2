package handler

// W11.25: HTTP nâng bậc lương (FN-09, FN-10) — /v1/hrm/promotion*, /v1/hrm/staff/{id}/ratings. Phản hồi có họ tên đặt
// Cache-Control: no-store. Xuất (POST) trả bản tóm tắt KHÔNG PII (phản hồi được lưu cho Idempotency-Key); tệp tải bằng
// GET …/file (dựng lại, so SHA-256).

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

func (h Handler) registerPromotion(rg *gin.RouterGroup) {
	rg.GET("/hrm/promotion", h.promoList)
	rg.GET("/hrm/promotion/cases/:key", h.promoCase)
	rg.POST("/hrm/promotion/cases/include", h.promoInclude)
	rg.POST("/hrm/promotion/cases/exclude", h.promoExclude)
	rg.GET("/hrm/promotion/nlth-pool", h.promoPool)
	rg.POST("/hrm/promotion/proposals", h.promoPropose)
	rg.POST("/hrm/promotion/proposals/withdraw", h.promoWithdraw)
	rg.GET("/hrm/promotion/exports", h.promoExports)
	rg.POST("/hrm/promotion/exports", h.promoExport)
	rg.GET("/hrm/promotion/exports/:id", h.promoExportGet)
	rg.GET("/hrm/promotion/exports/:id/file", h.promoExportFile)
	rg.GET("/hrm/promotion/decisions", h.decisionList)
	rg.POST("/hrm/promotion/decisions", h.decisionCreate)
	rg.GET("/hrm/promotion/decision-book", h.decisionBook)
	rg.GET("/hrm/promotion/decisions/:id", h.decisionGet)
	rg.POST("/hrm/promotion/decisions/:id/cancel", h.decisionCancel)
	rg.POST("/hrm/promotion/decisions/:id/file", h.decisionFile)
	rg.GET("/hrm/staff/:id/ratings", h.ratingList)
	rg.POST("/hrm/staff/:id/ratings", h.ratingSet)
}

func ymd(t time.Time) string { return t.Format(time.DateOnly) }

func optYMD(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return ymd(*t)
}

func strs(xs []string) []string {
	if xs == nil {
		return []string{}
	}
	return xs
}

func basisOut(bs []domain.Basis) []gin.H {
	out := make([]gin.H, 0, len(bs))
	for _, b := range bs {
		out = append(out, gin.H{"label": b.Label, "value": b.Value, "result": b.Result, "note": b.Note})
	}
	return out
}

func awardOut(a domain.PromoAward) gin.H {
	return gin.H{"event_id": a.EventID, "form": a.Form, "label": a.Label(), "year": a.Year, "months": a.Months, "used": a.Used}
}

func caseOut(v app.CaseView) gin.H {
	o := gin.H{"key": v.Key, "staff_id": v.StaffID, "code": v.Code, "name": v.Name, "unit": v.Unit, "unit_name": v.UnitName, "kind": v.Kind,
		"form": v.Kind.Form(), "due": ymd(v.Due), "grade": v.Grade, "grade_name": v.GradeName, "steps": v.Steps, "from_step": v.FromStep,
		"to_step": v.ToStep, "from_coeff": v.FromCoeff.StringFixed(2), "to_coeff": v.ToCoeff.StringFixed(2), "diff": v.Diff.StringFixed(2),
		"step_since": optYMD(&v.StepSince), "reserved": v.Reserved, "next_due": optYMD(&v.NextDue), "normal_due": optYMD(&v.NormalDue),
		"early_months": v.EarlyMonths, "pct_from": nil, "pct_to": nil, "pct_since": optYMD(v.PctSince), "cond": v.Cond, "why": strs(v.Why),
		"basis": basisOut(v.Basis), "planned_year": v.Planned, "blocked": strs(v.Blocked), "status": v.Status, "reason": v.Reason,
		"note": v.Note, "months_enjoyed": v.MonthsEnjoyed, "decision_id": v.DecisionID, "decision_no": v.DecisionNo, "award": nil}
	if v.Kind == domain.PromoTNVK || v.PctTo.IsPositive() {
		o["pct_to"] = v.PctTo.String()
	}
	if v.PctFrom != nil {
		o["pct_from"] = v.PctFrom.String()
	}
	if v.Award != nil {
		o["award"] = awardOut(*v.Award)
	}
	return o
}

func (h Handler) promoList(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	win, _ := strconv.Atoi(c.Query("window"))
	l, err := h.Svc.Promotion(c.Request.Context(), d, win)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(l.Items))
	for _, v := range l.Items {
		items = append(items, caseOut(v))
	}
	status := gin.H{}
	for _, s := range domain.PromoStatuses {
		status[string(s)] = l.Summary.ByStatus[s]
	}
	var scan any
	if l.LastScan != nil {
		scan = gin.H{"day": ymd(l.LastScan.Day), "counts": l.LastScan.Counts, "recorded_at": l.LastScan.RecordedAt.UTC().Format(time.RFC3339)}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"as_of": ymd(l.Day), "window": l.Window, "items": items, "missing": briefsOut(l.Missing), "last_scan": scan,
		"summary": gin.H{"total": l.Summary.Total, "nltx": l.Summary.NLTX, "tnvk": l.Summary.TNVK, "nlth": l.Summary.NLTH, "by_status": status,
			"by_month": l.Summary.ByMonth, "quota": gin.H{"year": l.Summary.QuotaYear, "total": l.Summary.Quota, "used": l.Summary.QuotaUsed,
				"pending": l.Summary.QuotaPending}},
		"rules": gin.H{"min_rating": l.Rules.MinRating, "rating_years": l.Rules.RatingYears, "achievement_years": l.Rules.AchievementYears,
			"tnvk_start_pct": l.Rules.TNVKStartPct.String(), "tnvk_step_pct": l.Rules.TNVKStepPct.String(), "tnvk_cycle_months": l.Rules.TNVKCycleMonths,
			"tnvk_first_months": l.Rules.TNVKFirstMonths, "nlth_quota_pct": l.Rules.NLTHQuotaPct.String(), "remind_months": l.Rules.RemindMonths}})
}

func ratingOut(r store.RatingRow) gin.H {
	return gin.H{"id": r.ID, "year": r.Year, "rating": r.Rating, "source": r.Source, "void": r.Void, "supersedes_id": r.Supersedes,
		"reason": r.Reason, "actor": r.Actor, "recorded_at": r.RecordedAt.UTC().Format(time.RFC3339)}
}

func promoEventOut(e store.PromoEvent) gin.H {
	return gin.H{"id": e.ID, "action": e.Action, "due_on": optYMD(e.DueOn), "export_id": e.ExportID, "decision_id": e.DecisionID,
		"reason": e.Reason, "actor": e.Actor, "recorded_at": e.RecordedAt.UTC().Format(time.RFC3339)}
}

func (h Handler) promoCase(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	v, err := h.Svc.PromotionCase(c.Request.Context(), c.Param("key"), d)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	o := caseOut(v.CaseView)
	hist := make([]gin.H, 0, len(v.History))
	for _, e := range v.History {
		hist = append(hist, promoEventOut(e))
	}
	rs := make([]gin.H, 0, len(v.Ratings))
	for _, r := range v.Ratings {
		rs = append(rs, ratingOut(r))
	}
	as := make([]gin.H, 0, len(v.Awards))
	for _, a := range v.Awards {
		as = append(as, awardOut(a))
	}
	o["history"], o["ratings"], o["awards"] = hist, rs, as
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, o)
}

type keysIn struct {
	Keys   []string `json:"keys"`
	Reason string   `json:"reason"`
}

func (h Handler) promoInclude(c *gin.Context) {
	var in keysIn
	if !decode(c, &in) {
		return
	}
	n, err := h.Svc.IncludeCases(c.Request.Context(), in.Keys, in.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"changed": n})
}

func (h Handler) promoExclude(c *gin.Context) {
	var in keysIn
	if !decode(c, &in) {
		return
	}
	n, err := h.Svc.ExcludeCases(c.Request.Context(), in.Keys, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"changed": n})
}

func (h Handler) promoPool(c *gin.Context) {
	win, _ := strconv.Atoi(c.Query("window"))
	p, err := h.Svc.PromotionPool(c.Request.Context(), win)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]gin.H, 0, len(p.Rows))
	for _, r := range p.Rows {
		opts := make([]gin.H, 0, len(r.Options))
		for _, o := range r.Options {
			x := o.Candidate
			opts = append(opts, gin.H{"award": awardOut(o.Award), "key": x.Key, "due": ymd(x.Due), "early_months": x.EarlyMonths, "cond": x.Cond,
				"why": strs(x.Why), "planned_year": x.Planned})
		}
		rows = append(rows, gin.H{"staff_id": r.StaffID, "code": r.Code, "name": r.Name, "unit_name": r.UnitName, "step": r.Step, "steps": r.Steps,
			"normal_due": optYMD(r.NormalDue), "block": r.Block, "options": opts})
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"as_of": ymd(p.Day), "excluded": p.Excluded, "rows": rows,
		"quota": gin.H{"year": p.QuotaYear, "total": p.Quota, "used": p.QuotaUsed, "pending": p.QuotaPending}})
}

type proposeIn struct {
	Items []struct {
		StaffID      uuid.UUID `json:"staff_id"`
		AwardEventID uuid.UUID `json:"award_event_id"`
	} `json:"items"`
	Reason string `json:"reason"`
}

func (h Handler) promoPropose(c *gin.Context) {
	var in proposeIn
	if !decode(c, &in) {
		return
	}
	ps := make([]app.Proposal, 0, len(in.Items))
	for _, x := range in.Items {
		ps = append(ps, app.Proposal{StaffID: x.StaffID, AwardEventID: x.AwardEventID})
	}
	keys, err := h.Svc.ProposeNLTH(c.Request.Context(), ps, in.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"keys": strs(keys)})
}

func (h Handler) promoWithdraw(c *gin.Context) {
	var in keysIn
	if !decode(c, &in) {
		return
	}
	n, err := h.Svc.WithdrawNLTH(c.Request.Context(), in.Keys, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"changed": n})
}

type promoExportIn struct {
	Keys          []string          `json:"keys"`
	ReviewMonth   string            `json:"review_month"`
	PreparerID    *uuid.UUID        `json:"preparer_id"`
	SignerTitle   string            `json:"signer_title"`
	TNVKPct       map[string]string `json:"tnvk_pct"`
	Months        map[string]string `json:"months"`
	Notes         map[string]string `json:"notes"`
	ReservedAfter map[string]string `json:"reserved_after"`
}

func (h Handler) promoExport(c *gin.Context) {
	var in promoExportIn
	if !decode(c, &in) {
		return
	}
	m, err := time.Parse("2006-01", in.ReviewMonth)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("review_month", "YYYY-MM"))
		return
	}
	xs, err := h.Svc.ExportPromotion(c.Request.Context(), app.PromoExportInput{Keys: in.Keys, ReviewMonth: m, Preparer: in.PreparerID,
		SignerTitle: in.SignerTitle, TNVKPct: in.TNVKPct, Months: in.Months, Notes: in.Notes, ReservedAfter: in.ReservedAfter}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, gin.H{"id": x.ID, "form": x.Form, "file_name": x.FileName, "sha256": x.SHA256, "byte_size": x.Size, "count": x.Count,
			"download": "/v1/hrm/promotion/exports/" + x.ID.String() + "/file"})
	}
	// Không PII: phản hồi này được lưu cho Idempotency-Key.
	c.JSON(http.StatusCreated, gin.H{"items": out})
}

func exportOut(x app.PromoExportView, withItems bool) gin.H {
	o := gin.H{"id": x.ID, "form": x.Form, "review_month": x.ReviewMonth.Format("2006-01"), "as_of": ymd(x.AsOf), "count": len(x.Items),
		"file_name": x.FileName, "sha256": x.SHA256, "byte_size": x.ByteSize, "actor": x.Actor, "recorded_at": x.RecordedAt.UTC().Format(time.RFC3339),
		"signer_title": x.Params["signer_title"], "preparer_id": x.Params["preparer"], "decision_no": x.DecisionNo}
	if withItems {
		items := make([]gin.H, 0, len(x.Items))
		for _, it := range x.Items {
			items = append(items, gin.H{"case_key": it.CaseKey, "staff_id": it.StaffID, "code": it.Code, "name": x.Names[it.StaffID],
				"kind": it.Kind, "due": it.Due, "unit_name": it.UnitName, "grade": it.Grade, "grade_name": it.GradeName, "from_step": it.FromStep,
				"to_step": it.ToStep, "from_coeff": it.FromCoeff, "to_coeff": it.ToCoeff, "diff": it.Diff, "pct_from": it.PctFrom, "pct_to": it.PctTo,
				"next_due": it.NextDue, "months_enjoyed": it.MonthsEnjoyed, "early_months": it.EarlyMonths, "award_label": it.AwardLabel,
				"note": it.Note, "reason": it.Reason, "status": x.Status[it.CaseKey]})
		}
		o["items"] = items
	}
	return o
}

func (h Handler) promoExports(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	xs, err := h.Svc.PromotionExports(c.Request.Context(), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, exportOut(x, false))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) promoExportGet(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrPromoExportNotFound)
	if !ok {
		return
	}
	x, err := h.Svc.PromotionExport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, exportOut(x, true))
}

func (h Handler) promoExportFile(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrPromoExportNotFound)
	if !ok {
		return
	}
	f, err := h.Svc.DownloadPromotionExport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+f.FileName+`"`)
	c.Header("X-Export-Drift", strconv.FormatBool(f.Drift))
	c.Header("X-Export-Sha256", f.SHA256)
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, f.Data)
}

func decisionRowOut(r store.DecisionRow) gin.H {
	return gin.H{"id": r.ID, "action": r.Action, "doc_id": r.DocID, "reason": r.Reason, "actor": r.Actor,
		"recorded_at": r.RecordedAt.UTC().Format(time.RFC3339)}
}

func decisionOut(v app.DecisionView, full bool) gin.H {
	is := v.Issue
	status := "applied"
	if v.Cancelled != nil {
		status = "cancelled"
	}
	o := gin.H{"id": is.DecisionID, "decision_no": is.No, "decided_on": ymd(is.DecidedOn), "issuer": is.Issuer, "kind": is.Kind,
		"export_id": is.ExportID, "source": v.Source, "count": len(is.StaffIDs), "doc_id": v.DocID, "status": status, "cancel_reason": nil,
		"recorded_at": is.RecordedAt.UTC().Format(time.RFC3339), "actor": is.Actor}
	if v.Cancelled != nil {
		o["cancel_reason"] = v.Cancelled.Reason
	}
	people := make([]gin.H, 0, len(is.People))
	for _, p := range is.People {
		people = append(people, gin.H{"staff_id": p.StaffID, "name": v.Names[p.StaffID], "case_key": p.CaseKey, "effective_on": p.EffectiveOn,
			"grade": p.Grade, "from_step": p.FromStep, "to_step": p.ToStep, "from_coeff": p.FromCoeff, "to_coeff": p.ToCoeff,
			"pct_from": p.PctFrom, "pct_to": p.PctTo})
	}
	o["people"] = people
	if full {
		rows := make([]gin.H, 0, len(v.Rows))
		for _, r := range v.Rows {
			rows = append(rows, decisionRowOut(r))
		}
		o["history"] = rows
	}
	return o
}

func decisionFilter(c *gin.Context) (app.DecisionFilter, bool) {
	f := app.DecisionFilter{Kind: c.Query("kind"), Issuer: c.Query("issuer"), Q: c.Query("q")}
	if y := c.Query("year"); y != "" {
		n, err := strconv.Atoi(y)
		if err != nil || n < 1975 || n > 2100 {
			httpx.Fail(c, problem.Validation.WithField("year", "năm 1975–2100"))
			return f, false
		}
		f.Year = n
	}
	return f, true
}

func (h Handler) decisionList(c *gin.Context) {
	f, ok := decisionFilter(c)
	if !ok {
		return
	}
	xs, err := h.Svc.Decisions(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, decisionOut(x, false))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) decisionGet(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrDecisionNotFound)
	if !ok {
		return
	}
	v, err := h.Svc.Decision(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, decisionOut(v, true))
}

type decisionIn struct {
	ExportID    *uuid.UUID `json:"export_id"`
	Kind        string     `json:"kind"`
	DecisionNo  string     `json:"decision_no"`
	DecidedOn   string     `json:"decided_on"`
	Issuer      string     `json:"issuer"`
	Acknowledge bool       `json:"acknowledge_warnings"`
	People      []struct {
		CaseKey     string    `json:"case_key"`
		StaffID     uuid.UUID `json:"staff_id"`
		EffectiveOn string    `json:"effective_on"`
		ToStep      int       `json:"to_step"`
		PctTo       string    `json:"pct_to"`
	} `json:"people"`
}

func (h Handler) decisionCreate(c *gin.Context) {
	var in decisionIn
	if !decode(c, &in) {
		return
	}
	d, err := dateOf(in.DecidedOn, "decided_on")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	ai := app.DecisionInput{ExportID: in.ExportID, Kind: in.Kind, No: in.DecisionNo, DecidedOn: d, Issuer: in.Issuer, Acknowledge: in.Acknowledge}
	for _, p := range in.People {
		x := app.DecisionPersonInput{CaseKey: p.CaseKey, StaffID: p.StaffID, ToStep: p.ToStep, PctTo: p.PctTo}
		if p.EffectiveOn != "" {
			e, err := dateOf(p.EffectiveOn, "effective_on")
			if err != nil {
				httpx.Fail(c, err)
				return
			}
			x.EffectiveOn = &e
		}
		ai.People = append(ai.People, x)
	}
	r, err := h.Svc.IssueDecision(c.Request.Context(), ai, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": r.ID, "updated": r.Updated, "returned": r.Returned, "warnings": strs(r.Warnings)})
}

func (h Handler) decisionCancel(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrDecisionNotFound)
	if !ok {
		return
	}
	var in reasonIn
	if !decode(c, &in) {
		return
	}
	if err := h.Svc.CancelDecision(c.Request.Context(), id, in.Reason, idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "status": "cancelled"})
}

// decisionFile: multipart — phần "file" (PDF, Word, ảnh scan).
func (h Handler) decisionFile(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrDecisionNotFound)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxDocUpload)
	if err := c.Request.ParseMultipartForm(maxDocUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, port.ErrDocTooLarge.WithDetail("tối đa 15 MB mỗi tệp"))
			return
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân phải là multipart/form-data có phần file"))
		return
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "bắt buộc"))
		return
	}
	fr, err := fh.Open()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var buf bytes.Buffer
	_, err = io.Copy(&buf, io.LimitReader(fr, domain.MaxDocBytes+1))
	_ = fr.Close()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	doc, err := h.Svc.AttachDecisionFile(c.Request.Context(), id, fh.Filename, buf.Bytes(), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"doc_id": doc})
}

func (h Handler) decisionBook(c *gin.Context) {
	f, ok := decisionFilter(c)
	if !ok {
		return
	}
	data, name, err := h.Svc.DecisionBook(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+name+`"`)
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, data)
}

func (h Handler) ratingList(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	xs, err := h.Svc.Ratings(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, ratingOut(x))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type ratingIn struct {
	Year   int    `json:"year"`
	Rating string `json:"rating"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}

func (h Handler) ratingSet(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	var in ratingIn
	if !decode(c, &in) {
		return
	}
	r, err := h.Svc.SetRating(c.Request.Context(), id, app.RatingInput{Year: in.Year, Rating: in.Rating, Source: in.Source, Reason: in.Reason}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, ratingOut(r))
}
