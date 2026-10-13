package handler

// HTTP phiếu báo giá + xác minh (W11.75, pf 000148; BRD Báo giá v1.3 FN-XA-03.01…03, 03.05, FN-TI-02.02…04, 02.09) —
// Application API /v1/price/tickets*, /v1/price/alerts*. Hợp đồng ở contracts/openapi/application.yaml, mục
// "# --- pf/price ---" (W11.75). Giá, % là chuỗi decimal (schema Decimal) — không float (R-CODE-01).

import (
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// TicketHandler gói TicketService cho HTTP.
type TicketHandler struct{ Svc *app.TicketService }

// RegisterApp gắn route Application API của phiếu báo giá.
func (h TicketHandler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/tickets", h.day)
	rg.GET("/price/tickets/sheet", h.sheet)
	rg.POST("/price/tickets/sheet", h.saveEntry)
	rg.POST("/price/tickets/proxy", h.saveProxy)
	rg.GET("/price/tickets/history", h.history)
	rg.GET("/price/tickets/export", h.export)
	rg.GET("/price/tickets/print", h.print)
	rg.GET("/price/tickets/:id", h.ticket)
	rg.GET("/price/tickets/:id/log", h.log)
	rg.POST("/price/tickets/:id/correct", h.correct)
	rg.POST("/price/tickets/:id/redate", h.redate)
	rg.POST("/price/tickets/:id/delete", h.remove)
	rg.GET("/price/alerts", h.alerts)
	rg.POST("/price/alerts/request", h.request)
	rg.POST("/price/alerts/respond", h.respond)
	rg.POST("/price/alerts/decide", h.decide)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func tsPtr(t *time.Time) *string {
	if t == nil || t.IsZero() {
		return nil
	}
	s := ts(*t)
	return &s
}

func decP(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func uuidPtr(u uuid.UUID) *string { return refPtr(u) }

func queryDate(c *gin.Context, name string) (time.Time, bool) {
	d, err := day(c.Query(name), name)
	if err != nil {
		httpx.Fail(c, err)
		return d, false
	}
	return d, true
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("id", "mã phiếu không hợp lệ"))
		return id, false
	}
	return id, true
}

// ---- DTO ----

type ticketPointDTO struct {
	Code        string  `json:"code"`
	Name        string  `json:"name"`
	RegionCode  string  `json:"region_code"`
	RegionName  string  `json:"region_name"`
	Active      bool    `json:"active"`
	OfficerRef  *string `json:"officer_ref"`
	OfficerName *string `json:"officer_name"`
	OwnerRef    *string `json:"owner_ref"`
	OwnerName   *string `json:"owner_name"`
}

type ticketHeadDTO struct {
	ID          string  `json:"id"`
	Version     int64   `json:"version"`
	Day         string  `json:"day"`
	PointCode   string  `json:"point_code"`
	Status      string  `json:"status"`
	Source      string  `json:"source"`
	Op          string  `json:"op"`
	Reason      *string `json:"reason"`
	SubmittedAt *string `json:"submitted_at"`
	OfficerRef  *string `json:"officer_ref"`
	ActorRef    string  `json:"actor_ref"`
	ActorName   *string `json:"actor_name"`
	UpdatedAt   string  `json:"updated_at"`
}

func headDTO(t *store.Ticket, actorName string) *ticketHeadDTO {
	if t == nil {
		return nil
	}
	return &ticketHeadDTO{ID: t.ID.String(), Version: t.Seq, Day: t.Day.Format(time.DateOnly), PointCode: t.PointCode, Status: t.Status,
		Source: t.Source, Op: t.Op, Reason: strPtr(t.Reason), SubmittedAt: tsPtr(t.SubmittedAt), OfficerRef: uuidPtr(t.OfficerRef),
		ActorRef: t.ActorRef.String(), ActorName: strPtr(actorName), UpdatedAt: ts(t.EventTime)}
}

type alertVersionDTO struct {
	Version     int64   `json:"version"`
	Status      string  `json:"status"`
	Direction   string  `json:"direction"`
	RefPrice    string  `json:"ref_price"`
	Price       string  `json:"price"`
	ChangePct   string  `json:"change_pct"`
	Threshold   string  `json:"threshold_pct"`
	Message     *string `json:"message"`
	RespondBy   *string `json:"respond_by"`
	Response    *string `json:"response"`
	ProposedMin *string `json:"proposed_min"`
	ProposedMax *string `json:"proposed_max"`
	Explanation *string `json:"explanation"`
	ActorRef    string  `json:"actor_ref"`
	At          string  `json:"at"`
}

func alertVer(a store.Alert) alertVersionDTO {
	return alertVersionDTO{Version: a.Seq, Status: a.Status, Direction: a.Direction, RefPrice: a.Ref.String(), Price: a.Price.String(),
		ChangePct: a.ChangePct.StringFixed(2), Threshold: a.Threshold.StringFixed(2), Message: strPtr(a.Message), RespondBy: tsPtr(a.RespondBy),
		Response: strPtr(a.Response), ProposedMin: decP(a.ProposedMin), ProposedMax: decP(a.ProposedMax), Explanation: strPtr(a.Explanation),
		ActorRef: a.ActorRef.String(), At: ts(a.EventTime)}
}

type alertDTO struct {
	TicketID string `json:"ticket_id"`
	ItemCode string `json:"item_code"`
	View     string `json:"view"`
	alertVersionDTO
	Request *alertVersionDTO  `json:"request"`
	Reply   *alertVersionDTO  `json:"reply"`
	History []alertVersionDTO `json:"history"`
}

func toAlertDTO(a *app.AlertInfo) *alertDTO {
	if a == nil {
		return nil
	}
	d := &alertDTO{TicketID: a.TicketID.String(), ItemCode: a.ItemCode, View: a.View, alertVersionDTO: alertVer(a.Alert),
		History: make([]alertVersionDTO, 0, len(a.History))}
	for _, h := range a.History {
		d.History = append(d.History, alertVer(h))
	}
	if a.Request != nil {
		v := alertVer(*a.Request)
		d.Request = &v
	}
	if a.Response != nil {
		v := alertVer(*a.Response)
		d.Reply = &v
	}
	return d
}

type refDTO struct {
	Min string `json:"min"`
	Max string `json:"max"`
	Mid string `json:"mid"`
	Day string `json:"day"`
}

type evalDTO struct {
	Direction string `json:"direction"`
	ChangePct string `json:"change_pct"`
}

type sheetLineDTO struct {
	ItemCode  string    `json:"item_code"`
	Name      string    `json:"name"`
	Variety   string    `json:"variety"`
	GroupCode string    `json:"group_code"`
	GroupName string    `json:"group_name"`
	UnitCode  string    `json:"unit_code"`
	UnitName  string    `json:"unit_name"`
	Kind      string    `json:"kind"`
	Ref       *refDTO   `json:"ref"`
	Threshold string    `json:"threshold_pct"`
	Min       *string   `json:"min"`
	Max       *string   `json:"max"`
	NoGoods   bool      `json:"no_goods"`
	Note      *string   `json:"note"`
	Entered   bool      `json:"entered"`
	AdjMin    *string   `json:"adj_min"`
	AdjMax    *string   `json:"adj_max"`
	AdjReason *string   `json:"adj_reason"`
	Eval      *evalDTO  `json:"eval"`
	Alert     *alertDTO `json:"alert"`
}

type sheetCanDTO struct {
	Enter   bool `json:"enter"`
	Proxy   bool `json:"proxy"`
	Correct bool `json:"correct"`
	Redate  bool `json:"redate"`
	Delete  bool `json:"delete"`
	Review  bool `json:"review"`
	Respond bool `json:"respond"`
}

type sheetDTO struct {
	Point     ticketPointDTO `json:"point"`
	Day       string         `json:"day"`
	Quote     bool           `json:"quote"`
	LockAt    *string        `json:"lock_at"`
	RespondBy *string        `json:"respond_by"`
	Locked    bool           `json:"locked"`
	Board     string         `json:"board"`
	Now       string         `json:"now"`
	View      string         `json:"view"`
	Ticket    *ticketHeadDTO `json:"ticket"`
	Can       sheetCanDTO    `json:"can"`
	Lines     []sheetLineDTO `json:"lines"`
}

func planTimes(p domain.DayPlan) (*string, *string) {
	if !p.Quote {
		return nil, nil
	}
	return tsPtr(&p.LockAt), tsPtr(&p.RespondBy)
}

func toSheetDTO(sh app.Sheet) sheetDTO {
	p := sh.Point
	lock, resp := planTimes(sh.Plan)
	out := sheetDTO{Point: ticketPointDTO{Code: p.Code, Name: p.Name, RegionCode: p.RegionCode, RegionName: sh.RegionName, Active: sh.PointActive,
		OfficerRef: uuidPtr(p.Officer), OfficerName: strPtr(sh.OfficerName), OwnerRef: uuidPtr(p.Owner), OwnerName: strPtr(sh.OwnerName)},
		Day: sh.Day.Format(time.DateOnly), Quote: sh.Plan.Quote, LockAt: lock, RespondBy: resp, Locked: sh.Locked, Board: sh.Board,
		Now: ts(sh.Now), View: sh.View, Ticket: headDTO(sh.Ticket, sh.ActorName), Can: sheetCanDTO(sh.Can),
		Lines: make([]sheetLineDTO, 0, len(sh.Lines))}
	for _, l := range sh.Lines {
		d := sheetLineDTO{ItemCode: l.Item.Code, Name: l.Item.Name, Variety: l.Item.Variety, GroupCode: l.Item.GroupCode, GroupName: l.GroupName,
			UnitCode: l.Item.UnitCode, UnitName: l.UnitName, Kind: l.Item.Kind, Threshold: l.Threshold.StringFixed(2), Min: decP(l.Entry.Min),
			Max: decP(l.Entry.Max), NoGoods: l.Entry.NoGoods, Note: strPtr(l.Entry.Note), Entered: l.Entered, AdjMin: decP(l.AdjMin),
			AdjMax: decP(l.AdjMax), AdjReason: strPtr(l.AdjReason), Alert: toAlertDTO(l.Alert)}
		if l.Ref != nil {
			d.Ref = &refDTO{Min: l.Ref.Min.String(), Max: l.Ref.Max.String(), Mid: l.Ref.Mid.String(), Day: l.Ref.Day.Format(time.DateOnly)}
		}
		if l.Eval.HasRef {
			d.Eval = &evalDTO{Direction: l.Eval.Direction, ChangePct: l.Eval.ChangePct.StringFixed(2)}
		}
		out.Lines = append(out.Lines, d)
	}
	return out
}

func (h TicketHandler) replySheet(c *gin.Context, status int, sh app.Sheet, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status, toSheetDTO(sh))
}

// ---- màn nhập / chi tiết ----

func (h TicketHandler) sheet(c *gin.Context) {
	d, ok := queryDate(c, "day")
	if !ok {
		return
	}
	sh, err := h.Svc.GetSheet(c.Request.Context(), c.Query("profile"), c.Query("point"), d)
	h.replySheet(c, http.StatusOK, sh, err)
}

func (h TicketHandler) ticket(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	sh, err := h.Svc.GetTicket(c.Request.Context(), c.Query("profile"), id)
	h.replySheet(c, http.StatusOK, sh, err)
}

type lineReq struct {
	ItemCode string  `json:"item_code"`
	Min      *string `json:"min"`
	Max      *string `json:"max"`
	NoGoods  bool    `json:"no_goods"`
	Note     *string `json:"note"`
}

type entryReq struct {
	Point   string    `json:"point"`
	Day     string    `json:"day"`
	Version int64     `json:"version"`
	Submit  bool      `json:"submit"`
	Reason  string    `json:"reason"`
	Lines   []lineReq `json:"lines"`
}

func parseDec(s *string, field string) (*decimal.Decimal, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	d, err := decimal.NewFromString(*s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "phải là số thập phân dạng chuỗi")
	}
	return &d, nil
}

func (r entryReq) input() (app.EntryInput, error) {
	in := app.EntryInput{Point: r.Point, Version: r.Version, Submit: r.Submit, Reason: r.Reason}
	var err error
	if in.Day, err = day(r.Day, "day"); err != nil {
		return in, err
	}
	if len(r.Lines) > 500 {
		return in, problem.Validation.WithField("lines", "tối đa 500 dòng")
	}
	for _, l := range r.Lines {
		x := domain.Line{ItemCode: l.ItemCode, NoGoods: l.NoGoods}
		if l.Note != nil {
			x.Note = *l.Note
		}
		if x.Min, err = parseDec(l.Min, "min"); err != nil {
			return in, err
		}
		if x.Max, err = parseDec(l.Max, "max"); err != nil {
			return in, err
		}
		in.Lines = append(in.Lines, x)
	}
	return in, nil
}

func (h TicketHandler) entry(c *gin.Context, proxy bool) {
	var req entryReq
	if !bind(c, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var sh app.Sheet
	if proxy {
		sh, err = h.Svc.SaveProxy(c.Request.Context(), c.Query("profile"), in, idem(c))
	} else {
		sh, err = h.Svc.SaveEntry(c.Request.Context(), c.Query("profile"), in, idem(c))
	}
	h.replySheet(c, http.StatusCreated, sh, err)
}

func (h TicketHandler) saveEntry(c *gin.Context) { h.entry(c, false) }
func (h TicketHandler) saveProxy(c *gin.Context) { h.entry(c, true) }

type adjReq struct {
	ItemCode string `json:"item_code"`
	Min      string `json:"min"`
	Max      string `json:"max"`
}

type correctReq struct {
	Version int64    `json:"version"`
	Reason  string   `json:"reason"`
	Lines   []adjReq `json:"lines"`
}

func (h TicketHandler) correct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req correctReq
	if !bind(c, &req) {
		return
	}
	adj := make([]app.Adjustment, 0, len(req.Lines))
	for _, l := range req.Lines {
		lo, err := parseDec(&l.Min, "min")
		if err != nil || lo == nil {
			httpx.Fail(c, problem.Validation.WithField("min", "bắt buộc, số thập phân dạng chuỗi"))
			return
		}
		hi, err := parseDec(&l.Max, "max")
		if err != nil || hi == nil {
			httpx.Fail(c, problem.Validation.WithField("max", "bắt buộc, số thập phân dạng chuỗi"))
			return
		}
		adj = append(adj, app.Adjustment{ItemCode: l.ItemCode, Min: *lo, Max: *hi})
	}
	sh, err := h.Svc.Correct(c.Request.Context(), c.Query("profile"), id, req.Version, req.Reason, adj, idem(c))
	h.replySheet(c, http.StatusCreated, sh, err)
}

type redateReq struct {
	Version int64  `json:"version"`
	Day     string `json:"day"`
	Reason  string `json:"reason"`
}

func (h TicketHandler) redate(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req redateReq
	if !bind(c, &req) {
		return
	}
	d, err := day(req.Day, "day")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	sh, err := h.Svc.Redate(c.Request.Context(), c.Query("profile"), id, req.Version, d, req.Reason, idem(c))
	h.replySheet(c, http.StatusCreated, sh, err)
}

type deleteReq struct {
	Version int64  `json:"version"`
	Reason  string `json:"reason"`
}

func (h TicketHandler) remove(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var req deleteReq
	if !bind(c, &req) {
		return
	}
	sh, err := h.Svc.Delete(c.Request.Context(), c.Query("profile"), id, req.Version, req.Reason, idem(c))
	h.replySheet(c, http.StatusCreated, sh, err)
}

// ---- danh sách ----

type ticketRowDTO struct {
	Point      ticketPointDTO `json:"point"`
	Mine       bool           `json:"mine"`
	View       string         `json:"view"`
	Ticket     *ticketHeadDTO `json:"ticket"`
	Items      int            `json:"items"`
	Priced     int            `json:"priced"`
	NoGoods    int            `json:"no_goods"`
	Adjusted   int            `json:"adjusted"`
	Excluded   int            `json:"excluded"`
	High       int            `json:"high"`
	Low        int            `json:"low"`
	OpenAlerts int            `json:"open_alerts"`
}

func rowDTOOf(r app.TicketRow, active bool) ticketRowDTO {
	p := r.Point
	return ticketRowDTO{Point: ticketPointDTO{Code: p.Code, Name: p.Name, RegionCode: p.RegionCode, RegionName: r.RegionName, Active: active,
		OfficerRef: uuidPtr(p.Officer), OfficerName: strPtr(r.OfficerName), OwnerRef: uuidPtr(p.Owner), OwnerName: strPtr(r.OwnerName)},
		Mine: r.Mine, View: r.View, Ticket: headDTO(r.Ticket, ""), Items: r.Items, Priced: r.Priced, NoGoods: r.NoGoods, Adjusted: r.Adjusted,
		Excluded: r.Excluded, High: r.High, Low: r.Low, OpenAlerts: r.OpenAlerts}
}

func dayFilter(c *gin.Context) (app.DayFilter, bool) {
	d, ok := queryDate(c, "day")
	return app.DayFilter{Day: d, Region: c.Query("region"), Mine: c.Query("mine") == "true"}, ok
}

func (h TicketHandler) day(c *gin.Context) {
	f, ok := dayFilter(c)
	if !ok {
		return
	}
	v, err := h.Svc.DayTickets(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	lock, resp := planTimes(v.Plan)
	rows := make([]ticketRowDTO, 0, len(v.Rows))
	for _, r := range v.Rows {
		rows = append(rows, rowDTOOf(r, true))
	}
	c.JSON(http.StatusOK, gin.H{"day": v.Day.Format(time.DateOnly), "quote": v.Plan.Quote, "lock_at": lock, "respond_by": resp,
		"locked": v.Locked, "board": v.Board, "now": ts(v.Now), "rows": rows})
}

func (h TicketHandler) history(c *gin.Context) {
	from, ok := queryDate(c, "from")
	if !ok {
		return
	}
	to, ok := queryDate(c, "to")
	if !ok {
		return
	}
	rs, err := h.Svc.History(c.Request.Context(), c.Query("profile"), c.Query("point"), from, to)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(rs))
	for _, r := range rs {
		d := rowDTOOf(r.TicketRow, true)
		out = append(out, gin.H{"day": r.Day.Format(time.DateOnly), "view": r.View, "ticket": headDTO(r.Ticket, ""), "point": d.Point,
			"priced": d.Priced, "no_goods": d.NoGoods, "adjusted": d.Adjusted, "excluded": d.Excluded, "high": d.High, "low": d.Low})
	}
	c.JSON(http.StatusOK, gin.H{"rows": out})
}

func (h TicketHandler) log(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	lg, err := h.Svc.Log(c.Request.Context(), c.Query("profile"), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	name := func(u uuid.UUID) *string { return strPtr(lg.Names[u]) }
	heads := make([]gin.H, 0, len(lg.Ticket))
	for i := range lg.Ticket {
		t := lg.Ticket[i]
		d := headDTO(&t, "")
		heads = append(heads, gin.H{"version": d.Version, "day": d.Day, "status": d.Status, "source": d.Source, "op": d.Op, "reason": d.Reason,
			"submitted_at": d.SubmittedAt, "actor_ref": d.ActorRef, "actor_name": name(t.ActorRef), "at": d.UpdatedAt})
	}
	lines := make([]gin.H, 0, len(lg.Lines))
	for _, l := range lg.Lines {
		lines = append(lines, gin.H{"item_code": l.Line.ItemCode, "version": l.Seq, "min": decP(l.Line.Min), "max": decP(l.Line.Max),
			"no_goods": l.Line.NoGoods, "note": strPtr(l.Line.Note), "adj_min": decP(l.AdjMin), "adj_max": decP(l.AdjMax),
			"adj_reason": strPtr(l.AdjReason), "actor_ref": l.ActorRef.String(), "actor_name": name(l.ActorRef), "at": ts(l.EventTime)})
	}
	alerts := make([]gin.H, 0, len(lg.Alerts))
	for _, a := range lg.Alerts {
		alerts = append(alerts, gin.H{"item_code": a.ItemCode, "entry": alertVer(a), "actor_name": name(a.ActorRef)})
	}
	c.JSON(http.StatusOK, gin.H{"ticket": heads, "lines": lines, "alerts": alerts})
}

func sendFile(c *gin.Context, f app.Export, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(f.FileName))
	c.Data(http.StatusOK, f.ContentType, f.Body)
}

func (h TicketHandler) export(c *gin.Context) {
	f, ok := dayFilter(c)
	if !ok {
		return
	}
	x, err := h.Svc.ExportDay(c.Request.Context(), c.Query("profile"), f)
	sendFile(c, x, err)
}

func (h TicketHandler) print(c *gin.Context) {
	var idList []uuid.UUID
	for _, s := range c.QueryArray("ticket") {
		id, err := uuid.Parse(s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("ticket", "mã phiếu không hợp lệ"))
			return
		}
		idList = append(idList, id)
	}
	x, err := h.Svc.PrintTickets(c.Request.Context(), c.Query("profile"), idList, c.Query("format"))
	sendFile(c, x, err)
}

// ---- cảnh báo, xác minh ----

type alertRowDTO struct {
	alertDTO
	Day        string         `json:"day"`
	Point      ticketPointDTO `json:"point"`
	ItemName   string         `json:"item_name"`
	Variety    string         `json:"variety"`
	GroupCode  string         `json:"group_code"`
	GroupName  string         `json:"group_name"`
	UnitName   string         `json:"unit_name"`
	Min        *string        `json:"min"`
	Max        *string        `json:"max"`
	Note       *string        `json:"note"`
	Adjusted   *string        `json:"adjusted"`
	TicketVer  int64          `json:"ticket_version"`
	TicketView string         `json:"ticket_status"`
	Mine       bool           `json:"mine"`
	CanAct     bool           `json:"can_act"`
}

func (h TicketHandler) alerts(c *gin.Context) {
	from, ok := queryDate(c, "from")
	if !ok {
		return
	}
	to, ok := queryDate(c, "to")
	if !ok {
		return
	}
	rs, err := h.Svc.ListAlerts(c.Request.Context(), c.Query("profile"), app.AlertFilter{From: from, To: to, Point: c.Query("point"),
		Mine: c.Query("mine") == "true", View: c.Query("view")})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]alertRowDTO, 0, len(rs))
	for i := range rs {
		r := rs[i]
		p := r.Point
		out = append(out, alertRowDTO{alertDTO: *toAlertDTO(&r.AlertInfo), Day: r.Day.Format(time.DateOnly),
			Point: ticketPointDTO{Code: p.Code, Name: p.Name, RegionCode: p.RegionCode, RegionName: r.RegionName, Active: true,
				OfficerRef: uuidPtr(p.Officer), OwnerRef: uuidPtr(p.Owner)},
			ItemName: r.Item.Name, Variety: r.Item.Variety, GroupCode: r.Item.GroupCode, GroupName: r.GroupName, UnitName: r.UnitName,
			Min: decP(r.Entry.Min), Max: decP(r.Entry.Max), Note: strPtr(r.Entry.Note), Adjusted: strPtr(r.AdjText), TicketVer: r.Ticket.Seq,
			TicketView: r.Ticket.Status, Mine: r.Mine, CanAct: r.CanAct})
	}
	c.JSON(http.StatusOK, gin.H{"alerts": out})
}

type alertRefReq struct {
	TicketID string `json:"ticket_id"`
	ItemCode string `json:"item_code"`
}

func (r alertRefReq) ref() (app.AlertRef, error) {
	id, err := uuid.Parse(r.TicketID)
	if err != nil {
		return app.AlertRef{}, problem.Validation.WithField("ticket_id", "mã phiếu không hợp lệ")
	}
	return app.AlertRef{TicketID: id, ItemCode: r.ItemCode}, nil
}

type requestReq struct {
	Alerts  []alertRefReq `json:"alerts"`
	Message string        `json:"message"`
}

func (h TicketHandler) request(c *gin.Context) {
	var req requestReq
	if !bind(c, &req) {
		return
	}
	refs := make([]app.AlertRef, 0, len(req.Alerts))
	for _, a := range req.Alerts {
		r, err := a.ref()
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		refs = append(refs, r)
	}
	n, err := h.Svc.RequestVerification(c.Request.Context(), c.Query("profile"), refs, req.Message, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"written": n})
}

type respondReq struct {
	alertRefReq
	Choice      string  `json:"choice"`
	Min         *string `json:"min"`
	Max         *string `json:"max"`
	Explanation string  `json:"explanation"`
}

func (h TicketHandler) respond(c *gin.Context) {
	var req respondReq
	if !bind(c, &req) {
		return
	}
	r, err := req.ref()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	in := app.ResponseInput{AlertRef: r, Choice: req.Choice, Explanation: req.Explanation}
	if in.Min, err = parseDec(req.Min, "min"); err != nil {
		httpx.Fail(c, err)
		return
	}
	if in.Max, err = parseDec(req.Max, "max"); err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.Svc.Respond(c.Request.Context(), c.Query("profile"), in, idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"written": 1})
}

type decideReq struct {
	alertRefReq
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

func (h TicketHandler) decide(c *gin.Context) {
	var req decideReq
	if !bind(c, &req) {
		return
	}
	r, err := req.ref()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.Svc.Decide(c.Request.Context(), c.Query("profile"), r, req.Decision, req.Note, idem(c)); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"written": 1})
}
