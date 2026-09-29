package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

// registerG3App: kiểm kê, chuyển kho, đo lường lô, hao hụt (bề mặt ứng dụng).
func (h Handler) registerG3App(rg *gin.RouterGroup) {
	rg.POST("/hub/facilities/:id/stocktakes", h.submitStocktake)
	rg.GET("/hub/facilities/:id/stocktakes", h.listStocktakes)
	rg.GET("/hub/stocktakes/:id", h.getStocktake)
	rg.POST("/hub/stocktakes/:id/decision", h.decideStocktake)
	rg.POST("/hub/facilities/:id/transfers", h.startTransfer)
	rg.POST("/hub/facilities/:id/transfers/:transfer/receipt", h.receiveTransfer)
	rg.GET("/hub/transfers/:id", h.getTransfer)
	rg.GET("/hub/lots/:id/measurements", h.measurements)
	rg.GET("/hub/facilities/:id/losses", h.losses)
}

// registerG3Admin: tham số có phiên bản hub.risk.v2 / hub.weigh.v1 (bề mặt quản trị, MFA).
func (h Handler) registerG3Admin(rg *gin.RouterGroup) {
	rg.GET("/hub/params/:set", h.params)
	rg.POST("/hub/params/:set/versions", h.proposeParams)
	rg.POST("/hub/params/:set/versions/:version/decision", h.decideParams)
}

func idStr(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}
	s := id.String()
	return &s
}

// ---------------------------------------------------------------------------
// Tham số
// ---------------------------------------------------------------------------

func toParamVersion(v store.ParamVersion) gin.H {
	state := "pending"
	if v.Decision != nil {
		state = *v.Decision
	}
	return gin.H{
		"id": v.ID.String(), "param_set": v.Set, "version": v.Seq, "rule_version": v.Rule, "params": v.Params, "reason": v.Reason,
		"effective_from": ts(v.EffectiveFrom), "proposed_by": v.ProposedBy.String(), "state": state, "decided_by": idStr(v.DecidedBy),
		"decided_at": tsp(v.DecidedAt), "event_time": ts(v.EventTime),
	}
}

func (h Handler) params(c *gin.Context) {
	pv, err := h.Svc.Params(c.Request.Context(), c.Param("set"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(pv.Versions))
	for _, v := range pv.Versions {
		items = append(items, toParamVersion(v))
	}
	c.JSON(http.StatusOK, gin.H{"param_set": pv.Set, "active_version": pv.Active, "active": pv.Current, "items": items})
}

type paramsReq struct {
	Params        json.RawMessage `json:"params"`
	Reason        string          `json:"reason"`
	EffectiveFrom string          `json:"effective_from"`
	DeviceID      *string         `json:"device_id"`
}

func (h Handler) proposeParams(c *gin.Context) {
	var req paramsReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	eff := x.time("effective_from", req.EffectiveFrom, false)
	if x.fail(c) {
		return
	}
	v, replayed, err := h.Svc.ProposeParams(c.Request.Context(), app.ProposeParamsInput{
		Set: c.Param("set"), Params: req.Params, Reason: req.Reason, EffectiveFrom: eff, DeviceID: req.DeviceID, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/hub/params/"+v.Set, toParamVersion(v))
}

func (h Handler) decideParams(c *gin.Context) {
	seq, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || seq < 1 {
		httpx.Fail(c, app.ErrParamVersionMissing)
		return
	}
	var req decideReq
	if !bind(c, &req) {
		return
	}
	v, replayed, err := h.Svc.DecideParams(c.Request.Context(), app.DecideParamsInput{
		Set: c.Param("set"), VersionSeq: seq, Decision: domain.Decision(req.Decision), Reason: req.Reason, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toParamVersion(v))
}

// ---------------------------------------------------------------------------
// Kiểm kê
// ---------------------------------------------------------------------------

func toStocktake(r app.StocktakeResult) gin.H {
	st := r.Stocktake
	adj := make([]gin.H, 0, len(r.Adjustments))
	for _, a := range r.Adjustments {
		adj = append(adj, gin.H{"lot_id": a.LotID.String(), "movement_id": a.MovementID.String(), "qty_kg": a.QtyKg.String()})
	}
	return gin.H{
		"id": st.ID.String(), "facility_id": st.FacilityID.String(), "status": st.Status, "lines": st.Lines,
		"book_kg": st.Book.String(), "counted_kg": st.Counted.String(), "variance_kg": st.Variance.String(), "note": st.Note,
		"supersedes_id": idStr(st.SupersedesID), "counted_by": st.ActorRef.String(), "counted_at": ts(st.EventTime),
		"decided_by": idStr(st.DecidedBy), "decided_at": tsp(st.DecidedAt), "adjustments": adj,
	}
}

type stocktakeReq struct {
	Lines []struct {
		Product   string `json:"product"`
		CountedKg string `json:"counted_kg"`
	} `json:"lines"`
	Note         string  `json:"note"`
	SupersedesID *string `json:"supersedes_id"`
	Reason       string  `json:"reason"`
	EventTime    string  `json:"event_time"`
	DeviceID     *string `json:"device_id"`
}

func (h Handler) submitStocktake(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var req stocktakeReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.StocktakeInput{FacilityID: fid, Note: req.Note, Reason: req.Reason, DeviceID: req.DeviceID, IdemKey: idem(c)}
	for i, l := range req.Lines {
		in.Lines = append(in.Lines, domain.CountLine{Product: domain.Product(l.Product), Counted: x.kg("lines["+strconv.Itoa(i)+"].counted_kg", l.CountedKg)})
	}
	in.Supersedes = x.uuidPtr("supersedes_id", req.SupersedesID)
	in.EventTime = x.time("event_time", req.EventTime, false)
	if x.fail(c) {
		return
	}
	st, replayed, err := h.Svc.SubmitStocktake(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/hub/stocktakes/"+st.ID.String(), toStocktake(app.StocktakeResult{Stocktake: st}))
}

func (h Handler) listStocktakes(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	ss, err := h.Svc.Stocktakes(c.Request.Context(), fid)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(ss))
	for _, s := range ss {
		items = append(items, toStocktake(app.StocktakeResult{Stocktake: s}))
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) getStocktake(c *gin.Context) {
	id, ok := pathID(c, app.ErrStocktakeNotFound)
	if !ok {
		return
	}
	r, err := h.Svc.Stocktake(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toStocktake(r))
}

func (h Handler) decideStocktake(c *gin.Context) {
	id, ok := pathID(c, app.ErrStocktakeNotFound)
	if !ok {
		return
	}
	var req decideReq
	if !bind(c, &req) {
		return
	}
	r, replayed, err := h.Svc.DecideStocktake(c.Request.Context(), app.DecideStocktakeInput{
		StocktakeID: id, Decision: domain.Decision(req.Decision), Reason: req.Reason, IdemKey: idem(c),
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "", toStocktake(r))
}

// ---------------------------------------------------------------------------
// Chuyển kho
// ---------------------------------------------------------------------------

func transferLots(ls []store.TransferLot) []gin.H {
	out := make([]gin.H, 0, len(ls))
	for _, l := range ls {
		out = append(out, gin.H{"source_lot_id": idStr(l.SourceLotID), "lot_id": l.LotID.String(), "mass_kg": l.MassKg.String()})
	}
	return out
}

func toTransfer(t store.Transfer) gin.H {
	return gin.H{
		"id": t.ID.String(), "from_facility_id": t.FromFacility.String(), "to_facility_id": t.ToFacility.String(),
		"dispatch_id": t.DispatchID.String(), "status": t.Status, "dispatched_kg": t.Dispatched.String(),
		"received_kg": kgp(t.Received), "loss_kg": kgp(t.Loss), "source_lots": transferLots(t.SourceLots),
		"received_lots": transferLots(t.ReceivedLots), "dispatched_at": ts(t.DispatchedAt), "received_at": tsp(t.ReceivedAt),
	}
}

type transferReq struct {
	ToFacilityID string `json:"to_facility_id"`
	MassKg       string `json:"mass_kg"`
	Allocations  []struct {
		LotID  string `json:"lot_id"`
		MassKg string `json:"mass_kg"`
	} `json:"allocations"`
	EventTime string  `json:"event_time"`
	DeviceID  *string `json:"device_id"`
}

func (h Handler) startTransfer(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	var req transferReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.TransferInput{From: fid, DeviceID: req.DeviceID, IdemKey: idem(c)}
	in.To = x.uuid("to_facility_id", req.ToFacilityID)
	in.Mass = x.kg("mass_kg", req.MassKg)
	for i, a := range req.Allocations {
		f := "allocations[" + strconv.Itoa(i) + "]"
		in.Allocations = append(in.Allocations, domain.Allocation{LotID: x.uuid(f+".lot_id", a.LotID), MassKg: x.kg(f+".mass_kg", a.MassKg)})
	}
	in.EventTime = x.time("event_time", req.EventTime, false)
	if x.fail(c) {
		return
	}
	t, replayed, err := h.Svc.StartTransfer(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/hub/transfers/"+t.ID.String(), toTransfer(t))
}

type transferReceiptReq struct {
	MassKg          string   `json:"mass_kg"`
	WeighTicketRefs []string `json:"weigh_ticket_refs"`
	EventTime       string   `json:"event_time"`
	DeviceID        *string  `json:"device_id"`
}

func (h Handler) receiveTransfer(c *gin.Context) {
	fid, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	tid, err := uuid.Parse(c.Param("transfer"))
	if err != nil {
		httpx.Fail(c, app.ErrTransferNotFound)
		return
	}
	var req transferReceiptReq
	if !bind(c, &req) {
		return
	}
	x := newV()
	in := app.ReceiveTransferInput{TransferID: tid, FacilityID: fid, DeviceID: req.DeviceID, IdemKey: idem(c)}
	if req.MassKg != "" {
		in.Mass = x.kg("mass_kg", req.MassKg)
	}
	for i, r := range req.WeighTicketRefs {
		in.WeighTicketRefs = append(in.WeighTicketRefs, x.uuid("weigh_ticket_refs["+strconv.Itoa(i)+"]", r))
	}
	in.EventTime = x.time("event_time", req.EventTime, false)
	if x.fail(c) {
		return
	}
	t, replayed, err := h.Svc.ReceiveTransfer(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replayed, "/v1/hub/transfers/"+t.ID.String(), toTransfer(t))
}

func (h Handler) getTransfer(c *gin.Context) {
	id, ok := pathID(c, app.ErrTransferNotFound)
	if !ok {
		return
	}
	t, err := h.Svc.Transfer(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toTransfer(t))
}

// ---------------------------------------------------------------------------
// Đo lường lô, hao hụt
// ---------------------------------------------------------------------------

func toLotWeigh(w *store.LotWeigh) any {
	if w == nil {
		return nil
	}
	return gin.H{
		"ticket_id": idStr(w.TicketID), "measured_kg": kgp(w.Measured), "moisture_pct": w.MoisturePct, "status": w.Status,
		"basis": w.Basis, "book_kg": kgp(w.Book), "dispatch_id": idStr(w.DispatchID),
	}
}

func (h Handler) measurements(c *gin.Context) {
	id, ok := pathID(c, app.ErrLotNotFound)
	if !ok {
		return
	}
	m, err := h.Svc.LotMeasurements(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	links := make([]gin.H, 0, len(m.Links))
	for _, l := range m.Links {
		links = append(links, gin.H{
			"id": l.ID.String(), "point": l.Point, "ticket_id": idStr(l.TicketID), "ticket_source": l.TicketSource,
			"supersedes_ticket_id": idStr(l.SupersedesTicketID), "measured_kg": kgp(l.Measured), "book_kg": kgp(l.Book),
			"diff_kg": kgp(l.Diff), "tolerance_kg": kgp(l.Tolerance), "outcome": l.Outcome, "dispatch_id": idStr(l.DispatchID),
			"movement_id": idStr(l.MovementID), "reason": l.Reason, "param_version": l.ParamVersion, "event_time": ts(l.EventTime),
			"recorded_at": ts(l.RecordedAt), "seq": l.Seq,
		})
	}
	var loss any
	if m.Loss != nil {
		loss = gin.H{
			"reconciliation_id": m.Loss.ReconciliationID.String(), "route": m.Loss.Route, "hub_in_kg": kgp(m.Loss.HubIn),
			"hub_out_kg": kgp(m.Loss.HubOut), "expected_loss_kg": kgp(m.Loss.ExpectedLoss), "abnormal_loss_kg": kgp(m.Loss.AbnormalLoss),
			"outcome": m.Loss.Outcome, "event_time": ts(m.Loss.EventTime),
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"lot_ref": m.LotRef.String(), "hub_in": toLotWeigh(m.HubIn), "hub_out": toLotWeigh(m.HubOut), "links": links,
		"expected_rolls": m.Expected, "expected_kg": kgp(m.ExpectedKg), "weigh_param_version": m.WeighVersion, "loss": loss,
	})
}

func (h Handler) losses(c *gin.Context) {
	id, ok := pathID(c, app.ErrFacilityNotFound)
	if !ok {
		return
	}
	s, err := h.Svc.FacilityLoss(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"facility_id": id.String(), "lots": s.Lots, "loss_alerts": s.LossAlerts, "hub_in_kg": s.HubIn.String(),
		"hub_out_kg": s.HubOut.String(), "expected_loss_kg": s.ExpectedLoss.String(), "abnormal_loss_kg": s.AbnormalLoss.String(),
	})
}
