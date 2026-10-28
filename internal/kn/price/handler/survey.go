package handler

// HTTP điều tra giá thành sản xuất theo vụ (W11.78, pf 20261024091116; BRD Báo giá v1.3 FN-TI-02.10…02.12, FN-TW-01.04) —
// Application API /v1/price/surveys/*, /v1/price/survey-report*. Hợp đồng ở contracts/openapi/modules/price/application.yaml
// mục W11.78. Tiền, % là chuỗi decimal. Họ tên, SĐT chỉ có ở phản hồi cho Cấp Tỉnh, Admin.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// SurveyHandler gói SurveyService cho HTTP.
type SurveyHandler struct{ Svc *app.SurveyService }

// maxSurveyUpload: tệp 5 MB + phần ánh xạ.
const maxSurveyUpload = 5<<20 + 256<<10

// RegisterApp gắn route Application API của điều tra giá thành.
func (h SurveyHandler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/surveys", h.list)
	rg.POST("/price/surveys", h.create)
	rg.GET("/price/surveys/:id", h.get)
	rg.PUT("/price/surveys/:id", h.update)
	rg.POST("/price/surveys/:id/actions", h.act)
	rg.POST("/price/surveys/:id/clone", h.clone)
	httpx.AllowBody(rg, http.MethodPost, "/price/surveys/:id/imports/preview", maxSurveyUpload)
	httpx.AllowBody(rg, http.MethodPost, "/price/surveys/:id/imports", maxSurveyUpload)
	rg.POST("/price/surveys/:id/imports/preview", h.preview)
	rg.POST("/price/surveys/:id/imports", h.commit)
	rg.GET("/price/surveys/:id/imports", h.imports)
	rg.GET("/price/surveys/:id/imports/:imp/issues", h.issues)
	rg.GET("/price/surveys/:id/forms", h.forms)
	rg.GET("/price/surveys/:id/forms/export", h.formsExport)
	rg.POST("/price/surveys/:id/forms/approve-unflagged", h.approveUnflagged)
	rg.GET("/price/surveys/:id/forms/:form", h.form)
	rg.POST("/price/surveys/:id/forms/:form/actions", h.formAct)
	rg.GET("/price/survey-report", h.report)
	rg.GET("/price/survey-report/export", h.reportExport)
}

func uuidParam(c *gin.Context, name, label string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, label+" không hợp lệ"))
		return uuid.Nil, false
	}
	return id, true
}

func optUUID(c *gin.Context, name string) (uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return uuid.Nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "mã không hợp lệ"))
		return uuid.Nil, false
	}
	return id, true
}

func money0(d decimal.Decimal) string { return d.Round(0).String() }

func moneyP(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.Round(0).String()
	return &s
}

// ---- Đợt điều tra (FN-TI-02.10) ----

type roundDTO struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Season   string   `json:"season"`
	Year     int      `json:"year"`
	From     *string  `json:"collect_from"`
	To       *string  `json:"collect_to"`
	Crops    []string `json:"crops"`
	Status   string   `json:"status"`
	Communes int      `json:"communes"`
	Forms    int      `json:"forms"`
	Target   int      `json:"target"`
	Pending  int      `json:"pending"`
	Seq      int64    `json:"seq"`
}

func roundOut(r app.RoundSummary) roundDTO {
	crops := r.Crops
	if crops == nil {
		crops = []string{}
	}
	return roundDTO{ID: r.ID.String(), Name: r.Name, Season: r.Season, Year: r.Year, From: dateStr(r.From), To: dateStr(r.To), Crops: crops,
		Status: r.Status, Communes: r.Communes, Forms: r.Forms, Target: r.Target, Pending: r.Pending, Seq: r.Seq}
}

func (h SurveyHandler) list(c *gin.Context) {
	rs, err := h.Svc.ListRounds(c.Request.Context(), c.Query("profile"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]roundDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, roundOut(r))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

type officerIn struct {
	ID       string `json:"id"`
	Commune  string `json:"commune"`
	FullName string `json:"full_name"`
	Phone    string `json:"phone"`
	Unit     string `json:"unit"`
}

type communeIn struct {
	Code    string         `json:"code"`
	Targets map[string]int `json:"targets"`
}

type roundIn struct {
	Name     string      `json:"name"`
	Season   string      `json:"season"`
	Year     int         `json:"year"`
	From     string      `json:"collect_from"`
	To       string      `json:"collect_to"`
	Crops    []string    `json:"crops"`
	Note     string      `json:"note"`
	Communes []communeIn `json:"communes"`
	Officers []officerIn `json:"officers"`
	Confirm  bool        `json:"confirm"`
	Open     bool        `json:"open"`
	Seq      int64       `json:"seq"`
}

func (in roundIn) input() (app.RoundInput, error) {
	from, err := day(in.From, "collect_from")
	if err != nil {
		return app.RoundInput{}, err
	}
	to, err := day(in.To, "collect_to")
	if err != nil {
		return app.RoundInput{}, err
	}
	out := app.RoundInput{Name: in.Name, Season: in.Season, Year: in.Year, From: from, To: to, Crops: in.Crops, Note: in.Note,
		Confirm: in.Confirm, Open: in.Open, Seq: in.Seq}
	for _, cm := range in.Communes {
		out.Communes = append(out.Communes, app.CommuneInput{Code: cm.Code, Targets: cm.Targets})
	}
	for _, o := range in.Officers {
		oi := app.OfficerInput{Commune: o.Commune, FullName: o.FullName, Phone: o.Phone, Unit: o.Unit}
		if o.ID != "" {
			if oi.ID, err = uuid.Parse(o.ID); err != nil {
				return app.RoundInput{}, problem.Validation.WithField("officers", "mã cán bộ điều tra không hợp lệ")
			}
		}
		out.Officers = append(out.Officers, oi)
	}
	return out, nil
}

func (h SurveyHandler) create(c *gin.Context) {
	var in roundIn
	if !bind(c, &in) {
		return
	}
	ri, err := in.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	id, err := h.Svc.CreateRound(c.Request.Context(), c.Query("profile"), ri, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.detail(c, id, http.StatusCreated)
}

func (h SurveyHandler) update(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	var in roundIn
	if !bind(c, &in) {
		return
	}
	ri, err := in.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.Svc.UpdateRound(c.Request.Context(), c.Query("profile"), id, ri, idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	h.detail(c, id, http.StatusOK)
}

func (h SurveyHandler) get(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	h.detail(c, id, http.StatusOK)
}

func cropMap(m map[string]int) map[string]int {
	out := map[string]int{}
	for _, cr := range domain.Crops {
		out[cr] = m[cr]
	}
	return out
}

func (h SurveyHandler) detail(c *gin.Context, id uuid.UUID, status int) {
	d, err := h.Svc.GetRound(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	communes := make([]gin.H, 0, len(d.CommuneSet))
	for _, cm := range d.CommuneSet {
		offs := make([]gin.H, 0, len(cm.Officers))
		for _, o := range cm.Officers {
			offs = append(offs, gin.H{"id": o.ID.String(), "commune": o.Commune, "full_name": o.FullName, "phone": app.MaskOrPhone(o.Phone),
				"unit": o.Unit, "forms": o.Forms})
		}
		communes = append(communes, gin.H{"code": cm.Code, "name": cm.Name, "targets": cropMap(cm.Targets), "imported": cropMap(cm.Imported),
			"officers": offs})
	}
	logs := make([]gin.H, 0, len(d.Log))
	for _, l := range d.Log {
		logs = append(logs, gin.H{"seq": l.Seq, "op": l.Op, "status": l.Status, "reason": l.Reason, "at": l.At.UTC().Format(time.RFC3339),
			"actor_ref": l.Actor.String()})
	}
	warnings := d.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	c.JSON(status, gin.H{"round": roundOut(d.RoundSummary), "note": d.Note, "communes": communes, "log": logs, "warnings": warnings,
		"can_edit": d.CanEdit, "can_reopen": d.CanReopen})
}

type roundActIn struct {
	Op     string `json:"op"`
	Reason string `json:"reason"`
	To     string `json:"collect_to"`
	Seq    int64  `json:"seq"`
}

func (h SurveyHandler) act(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	var in roundActIn
	if !bind(c, &in) {
		return
	}
	to, err := day(in.To, "collect_to")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.Svc.ActRound(c.Request.Context(), c.Query("profile"), id, app.RoundAction{Op: in.Op, Reason: in.Reason, To: to, Seq: in.Seq},
		idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	h.detail(c, id, http.StatusOK)
}

func (h SurveyHandler) clone(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	nid, err := h.Svc.CloneRound(c.Request.Context(), c.Query("profile"), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.detail(c, nid, http.StatusCreated)
}

// ---- Nhập từ tệp (FN-TI-02.11) ----

// importInput đọc multipart: file, mapping (JSON {trường: chỉ số cột}), names (JSON {item|commune: {giá trị: mã}}), sha256,
// save_synonyms.
func importInput(c *gin.Context) (app.ImportInput, bool) {
	if err := c.Request.ParseMultipartForm(maxSurveyUpload); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Fail(c, problem.Validation.WithField("file", "tệp tối đa 5 MB"))
			return app.ImportInput{}, false
		}
		httpx.Fail(c, problem.Validation.WithDetail("thân phải là multipart/form-data có phần file"))
		return app.ImportInput{}, false
	}
	defer func() { _ = c.Request.MultipartForm.RemoveAll() }()
	fh, err := c.FormFile("file")
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "bắt buộc"))
		return app.ImportInput{}, false
	}
	f, err := fh.Open()
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "không đọc được"))
		return app.ImportInput{}, false
	}
	defer func() { _ = f.Close() }()
	body, err := io.ReadAll(io.LimitReader(f, 5<<20+1))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("file", "không đọc được"))
		return app.ImportInput{}, false
	}
	in := app.ImportInput{File: body, FileName: fh.Filename, SHA256: c.Request.FormValue("sha256"),
		SaveSynonyms: c.Request.FormValue("save_synonyms") == "true"}
	if s := c.Request.FormValue("mapping"); s != "" {
		if err := json.Unmarshal([]byte(s), &in.Mapping); err != nil {
			httpx.Fail(c, problem.Validation.WithField("mapping", "JSON {trường: chỉ số cột}"))
			return app.ImportInput{}, false
		}
	}
	if s := c.Request.FormValue("names"); s != "" {
		if err := json.Unmarshal([]byte(s), &in.Names); err != nil {
			httpx.Fail(c, problem.Validation.WithField("names", "JSON {item|commune: {giá trị: mã}}"))
			return app.ImportInput{}, false
		}
	}
	return in, true
}

func lineOut(l app.ImportLine) gin.H {
	msgs := l.Messages
	if msgs == nil {
		msgs = []string{}
	}
	var off *string
	if l.OfficerID != nil {
		s := l.OfficerID.String()
		off = &s
	}
	return gin.H{"row": l.Row, "level": l.Level, "messages": msgs, "skip": l.Skip, "commune": l.Commune, "commune_name": l.CommuneName,
		"crop": l.Crop, "item": l.Item, "item_name": l.ItemName, "officer_id": off, "cost_per_kg": moneyP(l.CostPerKg), "flag": l.Flag}
}

func (h SurveyHandler) preview(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	in, ok := importInput(c)
	if !ok {
		return
	}
	p, err := h.Svc.PreviewImport(c.Request.Context(), c.Query("profile"), id, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	lines := make([]gin.H, 0, len(p.Lines))
	for _, l := range p.Lines {
		lines = append(lines, lineOut(l))
	}
	fields := make([]gin.H, 0, len(app.SurveyFields))
	for _, f := range app.SurveyFields {
		fields = append(fields, gin.H{"key": f.Key, "label": f.Label, "required": f.Required})
	}
	missing := p.Missing
	if missing == nil {
		missing = []string{}
	}
	var already *string
	if p.AlreadyImported != nil {
		s := p.AlreadyImported.UTC().Format(time.RFC3339)
		already = &s
	}
	c.JSON(http.StatusOK, gin.H{"sha256": p.SHA256, "file_name": p.FileName, "columns": p.Columns, "mapping": p.Mapping, "fields": fields,
		"missing": missing, "unknown": p.Unknown, "lines": lines, "ok": p.OK, "warnings": p.Warn, "errors": p.Err, "skipped": p.Skip,
		"already_imported_at": already})
}

func (h SurveyHandler) commit(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	in, ok := importInput(c)
	if !ok {
		return
	}
	r, err := h.Svc.CommitImport(c.Request.Context(), c.Query("profile"), id, in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	lines := make([]gin.H, 0, len(r.Lines))
	for _, l := range r.Lines {
		if l.Level != "ok" {
			lines = append(lines, lineOut(l))
		}
	}
	c.JSON(http.StatusCreated, gin.H{"import_id": r.ImportID.String(), "rows": r.Rows, "created": r.Created, "skipped": r.Skipped,
		"warnings": r.Warnings, "errors": r.Errors, "issues": lines})
}

func importOut(im store.SurveyImport) gin.H {
	return gin.H{"id": im.ImportID.String(), "file_name": im.FileName, "sha256": im.SHA256, "rows": im.Rows, "created": im.Created,
		"skipped": im.Skipped, "warnings": im.Warnings, "errors": im.Errors, "at": im.EventTime.UTC().Format(time.RFC3339),
		"actor_ref": im.Actor.String()}
}

func (h SurveyHandler) imports(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	ims, err := h.Svc.ListImports(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(ims))
	for _, im := range ims {
		out = append(out, importOut(im))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h SurveyHandler) issues(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	imp, ok := uuidParam(c, "imp", "mã lần nhập")
	if !ok {
		return
	}
	x, err := h.Svc.ExportImportIssues(c.Request.Context(), c.Query("profile"), id, imp)
	sendFile(c, x, err)
}

// ---- Rà soát phiếu (FN-TI-02.12) ----

func formRowOut(r app.FormRow) gin.H {
	var off *string
	if r.OfficerID != nil {
		s := r.OfficerID.String()
		off = &s
	}
	var pct *string
	if r.Pct != nil {
		s := r.Pct.Round(2).StringFixed(2)
		pct = &s
	}
	return gin.H{"id": r.ID.String(), "seq": r.Seq, "row_no": r.RowNo, "commune": r.Commune, "commune_name": r.CommuneName, "crop": r.Crop,
		"item": r.Item, "item_name": r.ItemName, "officer_id": off, "officer_name": r.OfficerName, "farmer_name": r.FarmerName,
		"price": money0(r.Price), "orig_price": money0(r.OrigPrice), "corrected": r.Corrected, "median": moneyP(r.Median), "pct": pct,
		"flag": r.Flag, "cost_per_kg": money0(r.CostPerKg), "status": r.Status}
}

func refsOut(rs []app.Ref) []gin.H {
	out := make([]gin.H, 0, len(rs))
	for _, r := range rs {
		out = append(out, gin.H{"code": r.Code, "name": r.Name})
	}
	return out
}

func (h SurveyHandler) forms(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))
	v, err := h.Svc.ListForms(c.Request.Context(), c.Query("profile"), id, app.FormFilter{Crop: c.Query("crop"), Item: c.Query("item"),
		Commune: c.Query("commune"), Flag: c.Query("flag"), Status: c.Query("status"), Limit: limit, Offset: offset})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]gin.H, 0, len(v.Rows))
	for _, r := range v.Rows {
		rows = append(rows, formRowOut(r))
	}
	dist := make([]gin.H, 0, len(v.Dist))
	for _, d := range v.Dist {
		dist = append(dist, gin.H{"commune": d.Commune, "commune_name": d.CommuneName, "n": d.N, "min": money0(d.Min), "max": money0(d.Max),
			"median": money0(d.Median)})
	}
	k := v.KPIs
	c.JSON(http.StatusOK, gin.H{"round": roundOut(v.Round), "kpis": gin.H{"sent": k.Sent, "flagged": k.Flagged, "verify": k.Verify,
		"approved": k.Approved, "rejected": k.Rejected, "pending": k.Pending}, "total": v.Total, "rows": rows, "distribution": dist,
		"items": refsOut(v.Items), "communes": refsOut(v.Communes), "threshold_pct": v.ThresholdPct.StringFixed(2), "can_review": v.CanReview,
		"can_close": v.CanClose})
}

func (h SurveyHandler) formsExport(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	x, err := h.Svc.ExportForms(c.Request.Context(), c.Query("profile"), id)
	sendFile(c, x, err)
}

func (h SurveyHandler) approveUnflagged(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	n, err := h.Svc.ApproveUnflagged(c.Request.Context(), c.Query("profile"), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"approved": n})
}

func (h SurveyHandler) form(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	fid, ok := uuidParam(c, "form", "mã phiếu")
	if !ok {
		return
	}
	d, err := h.Svc.GetForm(c.Request.Context(), c.Query("profile"), id, fid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.formOut(c, d)
}

func (h SurveyHandler) formOut(c *gin.Context, d app.FormDetail) {
	out := formRowOut(d.FormRow)
	costs := gin.H{}
	for k, v := range d.Costs {
		costs[k] = money0(v)
	}
	var margin *string
	if d.Calc.MarginPct != nil {
		s := d.Calc.MarginPct.Round(2).StringFixed(2)
		margin = &s
	}
	logs := make([]gin.H, 0, len(d.Log))
	for _, l := range d.Log {
		logs = append(logs, gin.H{"seq": l.Seq, "op": l.Op, "status": l.Status, "note": l.Note, "price": moneyP(l.Price),
			"at": l.At.UTC().Format(time.RFC3339), "actor_ref": l.Actor.String()})
	}
	out["area_ha"] = d.AreaHa.String()
	out["output_kg"] = d.OutputKg.String()
	out["other_income"] = money0(d.OtherIncome)
	out["costs"] = costs
	out["calc"] = gin.H{"total_cost": money0(d.Calc.TotalCost), "cost_per_kg": money0(d.Calc.CostPerKg), "yield_ha": d.Calc.YieldPerHa.Round(2).String(),
		"revenue": money0(d.Calc.Revenue), "profit": money0(d.Calc.Profit), "profit_per_ha": money0(d.Calc.ProfitPerHa), "margin_pct": margin}
	out["note"] = d.Note
	out["unverifiable"] = d.Unverifiable
	out["farmer_phone"] = d.FarmerPhone
	out["officer_phone"] = d.OfficerPhone
	out["officer_unit"] = d.OfficerUnit
	out["import_id"] = d.ImportID.String()
	out["log"] = logs
	c.JSON(http.StatusOK, out)
}

type formActIn struct {
	Op           string  `json:"op"`
	Note         string  `json:"note"`
	Price        *string `json:"price"`
	Unverifiable bool    `json:"unverifiable"`
	Seq          int64   `json:"seq"`
}

func (h SurveyHandler) formAct(c *gin.Context) {
	id, ok := uuidParam(c, "id", "mã đợt")
	if !ok {
		return
	}
	fid, ok := uuidParam(c, "form", "mã phiếu")
	if !ok {
		return
	}
	var in formActIn
	if !bind(c, &in) {
		return
	}
	act := app.FormAction{Op: in.Op, Note: in.Note, Unverifiable: in.Unverifiable, Seq: in.Seq}
	if in.Price != nil && *in.Price != "" {
		d, ok := domain.ParseNumber(*in.Price)
		if !ok {
			httpx.Fail(c, problem.Validation.WithField("price", "giá bán đúng phải là số"))
			return
		}
		act.Price = &d
	}
	if err := h.Svc.ReviewForm(c.Request.Context(), c.Query("profile"), id, fid, act, idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	d, err := h.Svc.GetForm(c.Request.Context(), c.Query("profile"), id, fid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	h.formOut(c, d)
}

// ---- Báo cáo giá thành (FN-TW-01.04) ----

func costFilter(c *gin.Context) (app.CostFilter, bool) {
	r, ok := optUUID(c, "round")
	if !ok {
		return app.CostFilter{}, false
	}
	cmp, ok := optUUID(c, "compare")
	if !ok {
		return app.CostFilter{}, false
	}
	return app.CostFilter{Round: r, Compare: cmp, Crop: c.Query("crop"), Item: c.Query("item"), Region: c.Query("region"),
		Commune: c.Query("commune")}, true
}

func blockOut(b *app.CostBlock) gin.H {
	if b == nil {
		return nil
	}
	rows := make([]gin.H, 0, len(b.Rows))
	for _, r := range b.Rows {
		costs := gin.H{}
		for k, v := range r.AvgCosts {
			costs[k] = money0(v)
		}
		var margin *string
		if r.AvgMarginPct != nil {
			s := r.AvgMarginPct.StringFixed(2)
			margin = &s
		}
		rows = append(rows, gin.H{"item": r.Item, "item_name": r.ItemName, "crop": r.Crop, "n": r.N, "small_sample": r.SmallSample,
			"avg_costs": costs, "avg_total_cost": money0(r.AvgTotalCost), "avg_yield_ha": r.AvgYieldHa.StringFixed(2),
			"avg_price": money0(r.AvgPrice), "median_price": money0(r.MedianPrice), "avg_cost_per_kg": money0(r.AvgCostPerKg),
			"median_cost_per_kg": money0(r.MedianCostPerKg), "avg_profit": money0(r.AvgProfit), "avg_profit_per_ha": money0(r.AvgProfitPerHa),
			"avg_margin_pct": margin})
	}
	regions := make([]gin.H, 0, len(b.Regions))
	for _, r := range b.Regions {
		regions = append(regions, gin.H{"item": r.Item, "item_name": r.ItemName, "region": r.Region, "region_name": r.RegionName, "n": r.N,
			"avg_cost_per_kg": money0(r.AvgCostPerKg), "avg_price": money0(r.AvgPrice)})
	}
	communes := make([]gin.H, 0, len(b.Communes))
	for _, r := range b.Communes {
		communes = append(communes, gin.H{"commune": r.Commune, "commune_name": r.CommuneName, "n": r.N, "min_price": money0(r.MinPrice),
			"max_price": money0(r.MaxPrice), "median_price": money0(r.MedianPrice), "avg_cost_per_kg": money0(r.AvgCost),
			"median_cost_per_kg": money0(r.MedianCostPerKg)})
	}
	k := b.KPIs
	return gin.H{"round": roundOut(b.Round), "kpis": gin.H{"forms": k.Forms, "avg_cost_per_kg": moneyP(k.AvgCostPerKg),
		"avg_price": moneyP(k.AvgPrice), "avg_profit_per_ha": moneyP(k.AvgProfitPerHa)}, "rows": rows, "regions": regions, "communes": communes}
}

func (h SurveyHandler) report(c *gin.Context) {
	f, ok := costFilter(c)
	if !ok {
		return
	}
	v, err := h.Svc.Cost(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rounds := make([]roundDTO, 0, len(v.Rounds))
	for _, r := range v.Rounds {
		rounds = append(rounds, roundOut(r))
	}
	c.JSON(http.StatusOK, gin.H{"rounds": rounds, "main": blockOut(v.Main), "compare": blockOut(v.Compare), "regions": refsOut(v.Regions),
		"can_export": v.CanExport})
}

func (h SurveyHandler) reportExport(c *gin.Context) {
	f, ok := costFilter(c)
	if !ok {
		return
	}
	x, err := h.Svc.ExportCost(c.Request.Context(), c.Query("profile"), f)
	sendFile(c, x, err)
}
