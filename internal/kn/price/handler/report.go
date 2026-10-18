package handler

// HTTP báo cáo giá (W11.77, pf 20261013164235; BRD Báo giá v1.3 FN-TI-02.01, FN-TI-02.08, FN-TW-01.01…03) — Application
// API /v1/price/reports/*. Hợp đồng ở contracts/openapi/application.yaml, mục pf/price W11.77. Giá, % là chuỗi decimal.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ReportHandler gói báo cáo giá của TicketService cho HTTP.
type ReportHandler struct{ Svc *app.TicketService }

// RegisterApp gắn route Application API của báo cáo giá.
func (h ReportHandler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/reports/progress", h.progress)
	rg.GET("/price/reports/progress/export", h.progressExport)
	rg.GET("/price/reports/summary", h.summary)
	rg.GET("/price/reports/summary/export", h.summaryExport)
	rg.GET("/price/reports/movement", h.movement)
	rg.GET("/price/reports/movement/export", h.movementExport)
	rg.GET("/price/reports/dashboard", h.dashboard)
	rg.GET("/price/reports/dashboard/export", h.dashboardExport)
	rg.GET("/price/reports/stats", h.stats)
	rg.GET("/price/reports/stats/export", h.statsExport)
	rg.GET("/price/reports/stats/detail", h.statsDetail)
	rg.POST("/price/reports/stats/close", h.statsClose)
	rg.POST("/price/reports/stats/reopen", h.statsReopen)
}

func refsDTO(rs []app.Ref) []gin.H {
	out := make([]gin.H, 0, len(rs))
	for _, r := range rs {
		out = append(out, gin.H{"code": r.Code, "name": r.Name})
	}
	return out
}

func decS(d decimal.Decimal) string { return d.String() }

func pctP(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.Round(2).StringFixed(2)
	return &s
}

func dayP(t *time.Time) *string {
	if t == nil {
		return nil
	}
	return dateStr(*t)
}

type itemInfoDTO struct {
	ItemCode  string `json:"item_code"`
	ItemName  string `json:"item_name"`
	GroupCode string `json:"group_code"`
	GroupName string `json:"group_name"`
	UnitCode  string `json:"unit_code"`
	UnitName  string `json:"unit_name"`
	Basic     bool   `json:"basic"`
}

func infoDTO(i app.ItemInfo) itemInfoDTO {
	return itemInfoDTO{ItemCode: i.ItemCode, ItemName: i.ItemName, GroupCode: i.GroupCode, GroupName: i.GroupName, UnitCode: i.UnitCode,
		UnitName: i.UnitName, Basic: i.Basic}
}

type moveDTO struct {
	Prev      *string `json:"prev"`
	ChangePct *string `json:"change_pct"`
	Trend     string  `json:"trend"`
}

func toMoveDTO(m app.Move) moveDTO {
	return moveDTO{Prev: decP(m.Prev), ChangePct: pctP(m.ChangePct), Trend: m.Trend}
}

type pointPriceDTO struct {
	itemInfoDTO
	PointCode  string `json:"point_code"`
	PointName  string `json:"point_name"`
	RegionCode string `json:"region_code"`
	RegionName string `json:"region_name"`
	Min        string `json:"min"`
	Max        string `json:"max"`
	Mid        string `json:"mid"`
	Late       bool   `json:"late"`
}

func pointPrices(ps []app.PointPrice) []pointPriceDTO {
	out := make([]pointPriceDTO, 0, len(ps))
	for _, p := range ps {
		out = append(out, pointPriceDTO{itemInfoDTO: infoDTO(p.ItemInfo), PointCode: p.PointCode, PointName: p.PointName,
			RegionCode: p.RegionCode, RegionName: p.RegionName, Min: decS(p.Min), Max: decS(p.Max), Mid: decS(p.Mid), Late: p.Late})
	}
	return out
}

// ---- Tiến độ nhập giá (FN-TI-02.01) ----

func progressFilter(c *gin.Context) (app.ProgressFilter, bool) {
	d, ok := queryDate(c, "day")
	return app.ProgressFilter{Day: d, Region: c.Query("region"), Status: c.Query("status"), Point: c.Query("point"),
		Mine: c.Query("mine") == "true"}, ok
}

type progressRowDTO struct {
	ticketRowDTO
	UpdatedAt *string  `json:"updated_at"`
	Reminders []string `json:"reminders"`
}

func (h ReportHandler) progress(c *gin.Context) {
	f, ok := progressFilter(c)
	if !ok {
		return
	}
	v, err := h.Svc.Progress(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	lock, _ := planTimes(v.Plan)
	rows := make([]progressRowDTO, 0, len(v.Rows))
	for _, r := range v.Rows {
		rem := make([]string, 0, len(r.Reminders))
		for _, t := range r.Reminders {
			rem = append(rem, ts(t))
		}
		rows = append(rows, progressRowDTO{ticketRowDTO: rowDTOOf(r.TicketRow, true), UpdatedAt: tsPtr(r.UpdatedAt), Reminders: rem})
	}
	k := v.Cards
	c.JSON(http.StatusOK, gin.H{"requested": v.Requested.Format(time.DateOnly), "day": v.Day.Format(time.DateOnly), "no_quote": v.NoQuote,
		"quote": v.Plan.Quote, "lock_at": lock, "locked": v.Locked, "board": v.Board, "now": ts(v.Now),
		"cards": gin.H{"points": k.Points, "submitted": k.Submitted, "draft": k.Draft, "not_entered": k.NotEntered, "missed": k.Missed,
			"late": k.Late, "high": k.High, "low": k.Low, "unassigned": k.Unassigned, "no_officer": k.NoOfficer},
		"rows": rows, "regions": refsDTO(v.Regions)})
}

func (h ReportHandler) progressExport(c *gin.Context) {
	f, ok := progressFilter(c)
	if !ok {
		return
	}
	x, err := h.Svc.ExportProgress(c.Request.Context(), c.Query("profile"), f)
	sendFile(c, x, err)
}

// ---- Bảng giá tổng hợp (FN-TW-01.01) ----

func summaryFilter(c *gin.Context) (app.SummaryFilter, bool) {
	d, ok := queryDate(c, "day")
	view := c.DefaultQuery("view", "item")
	if ok && view != "item" && view != "point" {
		httpx.Fail(c, problem.Validation.WithField("view", "item | point"))
		return app.SummaryFilter{}, false
	}
	return app.SummaryFilter{Day: d, Region: c.Query("region"), Group: c.Query("group"), ByPoint: view == "point", Item: c.Query("item")}, ok
}

type summaryLineDTO struct {
	itemInfoDTO
	moveDTO
	Min    string `json:"min"`
	Max    string `json:"max"`
	Avg    string `json:"avg"`
	Points int    `json:"points"`
}

func (h ReportHandler) summary(c *gin.Context) {
	f, ok := summaryFilter(c)
	if !ok {
		return
	}
	v, err := h.Svc.Summary(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	lines := make([]summaryLineDTO, 0, len(v.Lines))
	for _, l := range v.Lines {
		lines = append(lines, summaryLineDTO{itemInfoDTO: infoDTO(l.ItemInfo), moveDTO: toMoveDTO(l.Move), Min: decS(l.Min), Max: decS(l.Max),
			Avg: decS(l.Avg), Points: l.Points})
	}
	var approved *string
	if !v.Day.IsZero() {
		approved = tsPtr(&v.ApprovedAt)
	}
	c.JSON(http.StatusOK, gin.H{"requested": v.Requested.Format(time.DateOnly), "day": dateStr(v.Day), "version": v.Version,
		"approved_at": approved, "not_today": v.NotToday, "updating": v.Updating, "prev_day": dayP(v.PrevDay),
		"flat_pct": decS(v.FlatPct), "region": v.Region, "read_only": v.ReadOnly, "lines": lines, "points": pointPrices(v.Points),
		"regions": refsDTO(v.Regions), "groups": refsDTO(v.Groups)})
}

func (h ReportHandler) summaryExport(c *gin.Context) {
	f, ok := summaryFilter(c)
	if !ok {
		return
	}
	x, err := h.Svc.ExportSummary(c.Request.Context(), c.Query("profile"), f)
	sendFile(c, x, err)
}

// ---- Biến động giá (FN-TW-01.02) ----

func movementFilter(c *gin.Context) (app.MovementFilter, bool) {
	d, ok := queryDate(c, "day")
	return app.MovementFilter{Period: c.Query("period"), Day: d, Region: c.Query("region"), Point: c.Query("point"), Group: c.Query("group"),
		Item: c.Query("item")}, ok
}

func periodDTO(p app.Period) gin.H {
	return gin.H{"from": dateStr(p.From), "to": dateStr(p.To), "label": p.Label, "days": p.Days}
}

type movementLineDTO struct {
	itemInfoDTO
	moveDTO
	Cur *string `json:"cur"`
}

func (h ReportHandler) movement(c *gin.Context) {
	f, ok := movementFilter(c)
	if !ok {
		return
	}
	v, err := h.Svc.Movement(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	lines := make([]movementLineDTO, 0, len(v.Lines))
	for _, l := range v.Lines {
		lines = append(lines, movementLineDTO{itemInfoDTO: infoDTO(l.ItemInfo), moveDTO: toMoveDTO(l.Move), Cur: decP(l.Cur)})
	}
	trend := make([]gin.H, 0, len(v.Trend))
	for _, t := range v.Trend {
		trend = append(trend, gin.H{"period": periodDTO(t.Period), "avg": decP(t.Avg)})
	}
	points := make([]gin.H, 0, len(v.Points))
	for _, p := range v.Points {
		m := toMoveDTO(p.Move)
		points = append(points, gin.H{"point_code": p.PointCode, "point_name": p.PointName, "region_name": p.RegionName, "cur": decP(p.Cur),
			"prev": m.Prev, "change_pct": m.ChangePct, "trend": m.Trend})
	}
	c.JSON(http.StatusOK, gin.H{"period": v.Period, "cur": periodDTO(v.Cur), "prev": periodDTO(v.Prev), "insufficient": v.Insufficient,
		"flat_pct": decS(v.FlatPct), "cards": gin.H{"up": v.Cards.Up, "down": v.Cards.Down, "flat": v.Cards.Flat, "exceeded": v.Cards.Exceeded},
		"lines": lines, "item": v.Item, "trend": trend, "points": points, "regions": refsDTO(v.Regions), "groups": refsDTO(v.Groups),
		"can_export": v.CanExport})
}

func (h ReportHandler) movementExport(c *gin.Context) {
	f, ok := movementFilter(c)
	if !ok {
		return
	}
	format := c.DefaultQuery("format", "xlsx")
	if format != "xlsx" && format != "pdf" {
		httpx.Fail(c, problem.Validation.WithField("format", "xlsx | pdf"))
		return
	}
	x, err := h.Svc.ExportMovement(c.Request.Context(), c.Query("profile"), f, format)
	sendFile(c, x, err)
}

// ---- Dashboard theo vùng (FN-TW-01.03) ----

func dashboardFilter(c *gin.Context) (app.DashboardFilter, bool) {
	d, ok := queryDate(c, "day")
	return app.DashboardFilter{Day: d, Group: c.Query("group"), Item: c.Query("item"), Region: c.Query("region"),
		All: c.Query("all") == "true"}, ok
}

func regionAvgsDTO(rs []app.RegionAvg) []gin.H {
	out := make([]gin.H, 0, len(rs))
	for _, r := range rs {
		out = append(out, gin.H{"region": r.Region, "avg": decP(r.Avg), "points": r.Points})
	}
	return out
}

func (h ReportHandler) dashboard(c *gin.Context) {
	f, ok := dashboardFilter(c)
	if !ok {
		return
	}
	v, err := h.Svc.Dashboard(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	regions := make([]gin.H, 0, len(v.Regions))
	for _, r := range v.Regions {
		regions = append(regions, gin.H{"code": r.Code, "name": r.Name, "points": r.Points})
	}
	lines := make([]gin.H, 0, len(v.Lines))
	for _, l := range v.Lines {
		m := toMoveDTO(l.Move)
		lines = append(lines, gin.H{"item": infoDTO(l.ItemInfo), "avg": decS(l.Avg), "prev": m.Prev, "change_pct": m.ChangePct, "trend": m.Trend,
			"regions": regionAvgsDTO(l.Regions)})
	}
	groups := make([]gin.H, 0, len(v.Groups))
	for _, g := range v.Groups {
		groups = append(groups, gin.H{"code": g.Group.Code, "name": g.Group.Name, "up": g.Up, "down": g.Down, "flat": g.Flat})
	}
	trend := make([]gin.H, 0, len(v.Trend))
	for _, t := range v.Trend {
		trend = append(trend, gin.H{"day": t.Day.Format(time.DateOnly), "avg": decP(t.Avg), "regions": regionAvgsDTO(t.Regions)})
	}
	var approved *string
	if !v.Day.IsZero() {
		approved = tsPtr(&v.ApprovedAt)
	}
	c.JSON(http.StatusOK, gin.H{"requested": v.Requested.Format(time.DateOnly), "day": dateStr(v.Day), "version": v.Version,
		"approved_at": approved, "not_today": v.NotToday, "updating": v.Updating, "prev_day": dayP(v.PrevDay),
		"cards":   gin.H{"up": v.Cards.Up, "down": v.Cards.Down, "flat": v.Cards.Flat, "points": v.Cards.Points},
		"regions": regions, "lines": lines, "groups": groups, "item": v.Item, "trend": trend, "region": v.Region,
		"drill": pointPrices(v.Drill)})
}

func (h ReportHandler) dashboardExport(c *gin.Context) {
	f, ok := dashboardFilter(c)
	if !ok {
		return
	}
	x, err := h.Svc.ExportDashboard(c.Request.Context(), c.Query("profile"), f)
	sendFile(c, x, err)
}

// ---- Thống kê phiếu (FN-TI-02.08) ----

func monthParam(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	m, err := time.Parse("2006-01", s)
	if err != nil {
		return time.Time{}, problem.Validation.WithField(field, "tháng YYYY-MM")
	}
	return m, nil
}

func statsFilter(c *gin.Context) (app.StatsFilter, bool) {
	m, err := monthParam(c.Query("month"), "month")
	if err != nil {
		httpx.Fail(c, err)
		return app.StatsFilter{}, false
	}
	from, ok := queryDate(c, "from")
	if !ok {
		return app.StatsFilter{}, false
	}
	to, ok := queryDate(c, "to")
	return app.StatsFilter{Month: m, From: from, To: to, Region: c.Query("region"), Point: c.Query("point")}, ok
}

func statsDTO(v app.StatsView) gin.H {
	rows := make([]gin.H, 0, len(v.Rows))
	for _, r := range v.Rows {
		rows = append(rows, gin.H{"officer_ref": r.Officer.String(), "officer_name": strPtr(r.OfficerName), "point_code": r.Point,
			"point_name": r.PointName, "region_code": r.Region, "region_name": r.RegionName, "required": r.Required, "on_time": r.OnTime,
			"late": r.Late, "missed": r.Missed, "deleted": r.Deleted, "adjusted_lines": r.AdjustedLines})
	}
	var month gin.H
	if v.Month != nil {
		status := v.Month.Status
		if v.Month.Seq == 0 {
			status = "none"
		}
		month = gin.H{"seq": v.Month.Seq, "status": status, "version": v.Month.Version, "reason": strPtr(v.Month.Reason)}
		if v.Month.Seq > 0 {
			month["at"] = ts(v.Month.EventTime)
		}
	}
	k := v.Cards
	return gin.H{"from": v.From.Format(time.DateOnly), "to": v.To.Format(time.DateOnly), "monthly": v.Monthly, "month": month,
		"snapshot": v.Snapshot, "pending": v.Pending,
		"cards": gin.H{"quote_days": k.QuoteDays, "required": k.Required, "on_time": k.OnTime, "late": k.Late, "missed": k.Missed},
		"rows":  rows, "can_close": v.CanClose, "can_reopen": v.CanReopen, "regions": refsDTO(v.Regions)}
}

func (h ReportHandler) stats(c *gin.Context) {
	f, ok := statsFilter(c)
	if !ok {
		return
	}
	v, err := h.Svc.Stats(c.Request.Context(), c.Query("profile"), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, statsDTO(v))
}

func (h ReportHandler) statsExport(c *gin.Context) {
	f, ok := statsFilter(c)
	if !ok {
		return
	}
	x, err := h.Svc.ExportStats(c.Request.Context(), c.Query("profile"), f)
	sendFile(c, x, err)
}

var statResultView = map[string]string{domain.StatOnTime: "on_time", domain.StatLate: "late", domain.StatMissed: "missed"}

func (h ReportHandler) statsDetail(c *gin.Context) {
	f, ok := statsFilter(c)
	if !ok {
		return
	}
	officer, err := uuid.Parse(c.Query("officer"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("officer", "mã cán bộ không hợp lệ"))
		return
	}
	days, err := h.Svc.StatDetail(c.Request.Context(), c.Query("profile"), f, officer)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(days))
	for _, d := range days {
		out = append(out, gin.H{"day": d.Day.Format(time.DateOnly), "point_code": d.Point, "result": statResultView[d.Result], "view": d.View,
			"ticket_id": uuidPtr(d.Ticket), "deleted": d.Deleted, "adjusted_lines": d.Adjusted})
	}
	c.JSON(http.StatusOK, gin.H{"days": out})
}

type statMonthReq struct {
	Month  string `json:"month"`
	Seq    int64  `json:"seq"`
	Reason string `json:"reason"`
}

func (h ReportHandler) statMonthOp(c *gin.Context, closing bool) {
	var req statMonthReq
	if !bind(c, &req) {
		return
	}
	m, err := monthParam(req.Month, "month")
	if err == nil && m.IsZero() {
		err = problem.Validation.WithField("month", "bắt buộc")
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	in := app.StatMonthInput{Month: m, Seq: req.Seq, Reason: req.Reason}
	var v app.StatsView
	if closing {
		v, err = h.Svc.CloseStatMonth(c.Request.Context(), c.Query("profile"), in, idem(c))
	} else {
		v, err = h.Svc.ReopenStatMonth(c.Request.Context(), c.Query("profile"), in, idem(c))
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, statsDTO(v))
}

func (h ReportHandler) statsClose(c *gin.Context)  { h.statMonthOp(c, true) }
func (h ReportHandler) statsReopen(c *gin.Context) { h.statMonthOp(c, false) }
