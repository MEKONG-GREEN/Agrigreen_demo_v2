package handler

// HTTP mặt hàng theo thời kỳ (W11.73, pf 000141; BRD Báo giá v1.3 FN-AD-04.03, FN-XA-03.01) — Application API
// /v1/price/items*, groups*, assignments*, point-items, item-sets*, proposals*. Hợp đồng ở
// contracts/openapi/application.yaml, mục "# --- pf/price ---" (W11.73).

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ItemHandler gói ItemService cho HTTP.
type ItemHandler struct{ Svc *app.ItemService }

// RegisterApp gắn route Application API của mặt hàng.
func (h ItemHandler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/items", h.listItems)
	rg.POST("/price/items", h.createItem)
	rg.GET("/price/items/:code", h.getItem)
	rg.POST("/price/items/:code/revise", h.reviseItem)
	rg.POST("/price/items/:code/retire", h.retireItem)
	rg.POST("/price/items/:code/move", h.moveItem)
	rg.GET("/price/groups", h.listGroups)
	rg.POST("/price/groups", h.createGroup)
	rg.POST("/price/groups/:code/revise", h.reviseGroup)
	rg.POST("/price/groups/:code/retire", h.retireGroup)
	rg.GET("/price/assignments", h.listAssignments)
	rg.POST("/price/assignments", h.assign)
	rg.POST("/price/assignments/remove", h.unassign)
	rg.GET("/price/point-items", h.pointItems)
	rg.GET("/price/item-sets", h.listSets)
	rg.POST("/price/item-sets", h.createSet)
	rg.POST("/price/item-sets/:code/revise", h.reviseSet)
	rg.GET("/price/proposals", h.listProposals)
	rg.POST("/price/proposals", h.createProposal)
	rg.POST("/price/proposals/:id/approve", h.approveProposal)
	rg.POST("/price/proposals/:id/reject", h.rejectProposal)
}

func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return false
	}
	return true
}

// day đọc ngày YYYY-MM-DD (rỗng → zero).
func day(s, field string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, problem.Validation.WithDetail(field + " phải là ngày YYYY-MM-DD")
	}
	return d, nil
}

func queryDay(c *gin.Context) (time.Time, bool) {
	d, err := day(c.Query("at"), "at")
	if err != nil {
		httpx.Fail(c, err)
		return d, false
	}
	return d, true
}

func dateStr(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.Format(time.DateOnly)
	return &s
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

type writeDTO struct {
	Code      string `json:"code"`
	Version   int64  `json:"version"`
	ValidFrom string `json:"valid_from"`
	Written   int    `json:"written"`
}

func (h ItemHandler) reply(c *gin.Context, status int, r app.WriteResult, err error) {
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(status, writeDTO{Code: r.Code, Version: r.Version, ValidFrom: r.ValidFrom.Format(time.DateOnly), Written: r.Written})
}

// ---- mặt hàng ----

type itemDTO struct {
	Code         string  `json:"code"`
	Name         string  `json:"name"`
	Variety      string  `json:"variety"`
	GroupCode    string  `json:"group_code"`
	UnitCode     string  `json:"unit_code"`
	Spec         *string `json:"spec"`
	SalePlace    *string `json:"sale_place"`
	Kind         string  `json:"kind"`
	SortOrder    int     `json:"sort_order"`
	Version      int64   `json:"version"`
	ValidFrom    string  `json:"valid_from"`
	ValidTo      *string `json:"valid_to"`
	Since        *string `json:"since"`
	State        string  `json:"state"`
	ReplacesCode *string `json:"replaces_code"`
	Note         *string `json:"note"`
	PointCount   *int    `json:"point_count"`
	HasPrice     bool    `json:"has_price"`
	NewPrice     bool    `json:"new_price"`
	Gated        bool    `json:"gated"`
	ProposalID   *string `json:"proposal_id"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func stateDTO(st domain.ItemState) itemDTO {
	return itemDTO{Code: st.Code, Name: st.Name, Variety: st.Variety, GroupCode: st.GroupCode, UnitCode: st.UnitCode, Spec: strPtr(st.Spec),
		SalePlace: strPtr(st.SalePlace), Kind: st.Kind, SortOrder: st.SortOrder, Version: st.Version, ValidFrom: st.ValidFrom.Format(time.DateOnly),
		ValidTo: dateStr(st.ValidTo), Since: dateStr(st.Since), State: st.State, ReplacesCode: strPtr(st.ReplacesCode), Note: strPtr(st.Note)}
}

func viewDTO(v app.ItemView) itemDTO {
	d := stateDTO(v.ItemState)
	if v.PointCount >= 0 {
		n := v.PointCount
		d.PointCount = &n
	}
	d.HasPrice, d.NewPrice, d.Gated = v.HasPrice, v.NewPrice, v.Gated
	if v.ProposalID != nil {
		s := v.ProposalID.String()
		d.ProposalID = &s
	}
	return d
}

type itemListDTO struct {
	Day              string    `json:"day"`
	Items            []itemDTO `json:"items"`
	PendingProposals int       `json:"pending_proposals"`
	CanEdit          bool      `json:"can_edit"`
	CanSteward       bool      `json:"can_steward"`
}

func (h ItemHandler) listItems(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	l, err := h.Svc.ListItems(c.Request.Context(), c.Query("profile"), app.ItemFilter{At: at, Group: c.Query("group"), Kind: c.Query("kind"),
		State: c.Query("state"), Q: c.Query("q")})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := itemListDTO{Day: l.Day.Format(time.DateOnly), Items: make([]itemDTO, 0, len(l.Items)), PendingProposals: l.PendingProposals,
		CanEdit: l.CanEdit, CanSteward: l.CanSteward}
	for _, v := range l.Items {
		out.Items = append(out.Items, viewDTO(v))
	}
	c.JSON(http.StatusOK, out)
}

type versionDTO struct {
	Version    int64   `json:"version"`
	Status     string  `json:"status"`
	ValidFrom  string  `json:"valid_from"`
	Name       string  `json:"name"`
	Variety    string  `json:"variety"`
	GroupCode  string  `json:"group_code"`
	UnitCode   string  `json:"unit_code"`
	Spec       *string `json:"spec"`
	SalePlace  *string `json:"sale_place"`
	Kind       string  `json:"kind"`
	SortOrder  int     `json:"sort_order"`
	Note       *string `json:"note"`
	EventTime  string  `json:"event_time"`
	ActorRef   string  `json:"actor_ref"`
	ProposalID *string `json:"proposal_id"`
}

type itemDetailDTO struct {
	itemDTO
	History    []versionDTO `json:"history"`
	ReplacedBy []string     `json:"replaced_by"`
}

func versionOf(v store.ItemVersion) versionDTO {
	d := versionDTO{Version: v.Seq, Status: v.Status, ValidFrom: v.ValidFrom.Format(time.DateOnly), Name: v.V.Name, Variety: v.V.Variety,
		GroupCode: v.V.GroupCode, UnitCode: v.V.UnitCode, Spec: strPtr(v.V.Spec), SalePlace: strPtr(v.V.SalePlace), Kind: v.V.Kind,
		SortOrder: v.V.SortOrder, Note: strPtr(v.V.Note), EventTime: v.EventTime.UTC().Format(time.RFC3339), ActorRef: v.ActorRef.String()}
	if v.ProposalID != nil {
		s := v.ProposalID.String()
		d.ProposalID = &s
	}
	return d
}

func (h ItemHandler) getItem(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	d, err := h.Svc.GetItem(c.Request.Context(), c.Query("profile"), c.Param("code"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := itemDetailDTO{itemDTO: viewDTO(d.ItemView), History: make([]versionDTO, 0, len(d.History)), ReplacedBy: d.ReplacedBy}
	if out.ReplacedBy == nil {
		out.ReplacedBy = []string{}
	}
	for _, v := range d.History {
		out.History = append(out.History, versionOf(v))
	}
	c.JSON(http.StatusOK, out)
}

type itemReq struct {
	Name         string   `json:"name"`
	Variety      string   `json:"variety"`
	GroupCode    string   `json:"group_code"`
	UnitCode     string   `json:"unit_code"`
	Spec         string   `json:"spec"`
	SalePlace    string   `json:"sale_place"`
	Kind         string   `json:"kind"`
	ValidFrom    string   `json:"valid_from"`
	ValidTo      string   `json:"valid_to"`
	ReplacesCode string   `json:"replaces_code"`
	Points       []string `json:"points"`
}

func (r itemReq) input() (app.ItemInput, error) {
	from, err := day(r.ValidFrom, "valid_from")
	if err != nil {
		return app.ItemInput{}, err
	}
	to, err := day(r.ValidTo, "valid_to")
	if err != nil {
		return app.ItemInput{}, err
	}
	return app.ItemInput{Name: r.Name, Variety: r.Variety, GroupCode: r.GroupCode, UnitCode: r.UnitCode, Spec: r.Spec, SalePlace: r.SalePlace,
		Kind: r.Kind, ValidFrom: from, ValidTo: to, ReplacesCode: r.ReplacesCode, Points: r.Points}, nil
}

func (h ItemHandler) itemInput(c *gin.Context) (app.ItemInput, bool) {
	var req itemReq
	if !bind(c, &req) {
		return app.ItemInput{}, false
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return in, false
	}
	return in, true
}

func (h ItemHandler) createItem(c *gin.Context) {
	in, ok := h.itemInput(c)
	if !ok {
		return
	}
	r, err := h.Svc.CreateItem(c.Request.Context(), c.Query("profile"), in, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) reviseItem(c *gin.Context) {
	in, ok := h.itemInput(c)
	if !ok {
		return
	}
	if !in.ValidTo.IsZero() || in.ReplacesCode != "" || len(in.Points) > 0 {
		httpx.Fail(c, problem.Validation.WithDetail("sửa mặt hàng không nhận valid_to, replaces_code, points — dùng ngưng / thêm mặt hàng thay thế / gán xã"))
		return
	}
	r, err := h.Svc.ReviseItem(c.Request.Context(), c.Query("profile"), c.Param("code"), in, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) retireItem(c *gin.Context) {
	var req struct {
		ValidTo string `json:"valid_to"`
		Reason  string `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	to, err := day(req.ValidTo, "valid_to")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	r, err := h.Svc.RetireItem(c.Request.Context(), c.Query("profile"), c.Param("code"), to, req.Reason, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) moveItem(c *gin.Context) {
	var req struct {
		Direction string `json:"direction"`
	}
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.MoveItem(c.Request.Context(), c.Query("profile"), c.Param("code"), req.Direction, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

// ---- nhóm hàng ----

type groupDTO struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
	Version   int64  `json:"version"`
	ValidFrom string `json:"valid_from"`
	Items     int    `json:"items"`
}

func (h ItemHandler) listGroups(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	gs, err := h.Svc.ListGroups(c.Request.Context(), c.Query("profile"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]groupDTO, 0, len(gs))
	for _, g := range gs {
		out = append(out, groupDTO{Code: g.Code, Name: g.Name, SortOrder: g.SortOrder, Version: g.Version, ValidFrom: g.ValidFrom.Format(time.DateOnly),
			Items: g.Items})
	}
	c.JSON(http.StatusOK, gin.H{"groups": out})
}

type groupReq struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}

func (h ItemHandler) createGroup(c *gin.Context) {
	var req groupReq
	if !bind(c, &req) {
		return
	}
	if req.Code != "" && !domain.ValidGroupCode(req.Code) {
		httpx.Fail(c, problem.Validation.WithDetail("mã nhóm hàng: chữ thường, số, gạch dưới (≤ 40)"))
		return
	}
	r, err := h.Svc.CreateGroup(c.Request.Context(), c.Query("profile"), app.GroupInput{Code: req.Code, Name: req.Name, SortOrder: req.SortOrder}, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) reviseGroup(c *gin.Context) {
	var req groupReq
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.ReviseGroup(c.Request.Context(), c.Query("profile"), c.Param("code"), app.GroupInput{Name: req.Name, SortOrder: req.SortOrder}, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) retireGroup(c *gin.Context) {
	r, err := h.Svc.RetireGroup(c.Request.Context(), c.Query("profile"), c.Param("code"), idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

// ---- gán xã ----

type assignmentDTO struct {
	PointCode string `json:"point_code"`
	ItemCode  string `json:"item_code"`
	Version   int64  `json:"version"`
	ValidFrom string `json:"valid_from"`
}

func (h ItemHandler) listAssignments(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	as, err := h.Svc.ListAssignments(c.Request.Context(), c.Query("profile"), c.Query("point"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]assignmentDTO, 0, len(as))
	for _, a := range as {
		out = append(out, assignmentDTO{PointCode: a.PointCode, ItemCode: a.ItemCode, Version: a.Version, ValidFrom: a.ValidFrom.Format(time.DateOnly)})
	}
	c.JSON(http.StatusOK, gin.H{"assignments": out})
}

type assignReq struct {
	PointCode string   `json:"point_code"`
	ItemCodes []string `json:"item_codes"`
	ValidFrom string   `json:"valid_from"`
}

func (h ItemHandler) assignment(c *gin.Context, remove bool) {
	var req assignReq
	if !bind(c, &req) {
		return
	}
	from, err := day(req.ValidFrom, "valid_from")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	fn := h.Svc.Assign
	if remove {
		fn = h.Svc.Unassign
	}
	r, err := fn(c.Request.Context(), c.Query("profile"), req.PointCode, req.ItemCodes, from, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) assign(c *gin.Context)   { h.assignment(c, false) }
func (h ItemHandler) unassign(c *gin.Context) { h.assignment(c, true) }

func (h ItemHandler) pointItems(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	l, err := h.Svc.ListPointItems(c.Request.Context(), c.Query("profile"), c.Query("point"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]itemDTO, 0, len(l.Items))
	for _, st := range l.Items {
		items = append(items, stateDTO(st))
	}
	c.JSON(http.StatusOK, gin.H{"day": l.Day.Format(time.DateOnly), "point_code": l.Point, "items": items})
}

// ---- bộ theo vụ ----

type setDTO struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Season    string   `json:"season"`
	ItemCodes []string `json:"item_codes"`
	Enabled   bool     `json:"enabled"`
	Version   int64    `json:"version"`
	ValidFrom string   `json:"valid_from"`
	State     string   `json:"state"`
}

func (h ItemHandler) listSets(c *gin.Context) {
	at, ok := queryDay(c)
	if !ok {
		return
	}
	ss, err := h.Svc.ListSets(c.Request.Context(), c.Query("profile"), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]setDTO, 0, len(ss))
	for _, s := range ss {
		out = append(out, setDTO{Code: s.Code, Name: s.Name, Season: s.Season, ItemCodes: s.ItemCodes, Enabled: s.Enabled, Version: s.Version,
			ValidFrom: s.ValidFrom.Format(time.DateOnly), State: s.State})
	}
	c.JSON(http.StatusOK, gin.H{"sets": out})
}

type setReq struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Season    string   `json:"season"`
	ItemCodes []string `json:"item_codes"`
	Enabled   bool     `json:"enabled"`
	ValidFrom string   `json:"valid_from"`
	Retire    bool     `json:"retire"`
}

func (h ItemHandler) setInput(c *gin.Context, code string) (app.SetInput, bool) {
	var req setReq
	if !bind(c, &req) {
		return app.SetInput{}, false
	}
	from, err := day(req.ValidFrom, "valid_from")
	if err != nil {
		httpx.Fail(c, err)
		return app.SetInput{}, false
	}
	if code == "" {
		code = req.Code
	}
	return app.SetInput{Code: code, Name: req.Name, Season: req.Season, ItemCodes: req.ItemCodes, Enabled: req.Enabled, ValidFrom: from,
		Retire: req.Retire}, true
}

func (h ItemHandler) createSet(c *gin.Context) {
	in, ok := h.setInput(c, "")
	if !ok {
		return
	}
	if in.Retire {
		httpx.Fail(c, problem.Validation.WithDetail("retire chỉ dùng khi sửa bộ"))
		return
	}
	r, err := h.Svc.CreateSet(c.Request.Context(), c.Query("profile"), in, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) reviseSet(c *gin.Context) {
	in, ok := h.setInput(c, c.Param("code"))
	if !ok {
		return
	}
	r, err := h.Svc.ReviseSet(c.Request.Context(), c.Query("profile"), in, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

// ---- đề xuất ----

type proposalDTO struct {
	ID           string  `json:"id"`
	Version      int64   `json:"version"`
	PointCode    string  `json:"point_code"`
	Name         string  `json:"name"`
	Variety      string  `json:"variety"`
	GroupCode    string  `json:"group_code"`
	UnitCode     string  `json:"unit_code"`
	Spec         *string `json:"spec"`
	Reason       string  `json:"reason"`
	Status       string  `json:"status"`
	ItemCode     *string `json:"item_code"`
	DecisionNote *string `json:"decision_note"`
	ProposerRef  string  `json:"proposer_ref"`
	CreatedAt    string  `json:"created_at"`
	DecidedAt    *string `json:"decided_at"`
}

func (h ItemHandler) listProposals(c *gin.Context) {
	ps, err := h.Svc.ListProposals(c.Request.Context(), c.Query("profile"), c.Query("status"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]proposalDTO, 0, len(ps))
	for _, p := range ps {
		d := proposalDTO{ID: p.ProposalID.String(), Version: p.Seq, PointCode: p.PointCode, Name: p.Name, Variety: p.Variety, GroupCode: p.GroupCode,
			UnitCode: p.UnitCode, Spec: strPtr(p.Spec), Reason: p.Reason, Status: p.Status, ItemCode: strPtr(p.ItemCode),
			DecisionNote: strPtr(p.DecisionNote), ProposerRef: p.ProposerRef.String(), CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339)}
		if p.DecidedAt != nil {
			s := p.DecidedAt.UTC().Format(time.RFC3339)
			d.DecidedAt = &s
		}
		out = append(out, d)
	}
	c.JSON(http.StatusOK, gin.H{"proposals": out})
}

func (h ItemHandler) createProposal(c *gin.Context) {
	var req struct {
		PointCode string `json:"point_code"`
		Name      string `json:"name"`
		Variety   string `json:"variety"`
		GroupCode string `json:"group_code"`
		UnitCode  string `json:"unit_code"`
		Spec      string `json:"spec"`
		Reason    string `json:"reason"`
	}
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.CreateProposal(c.Request.Context(), c.Query("profile"), app.ProposalInput{PointCode: req.PointCode, Name: req.Name,
		Variety: req.Variety, GroupCode: req.GroupCode, UnitCode: req.UnitCode, Spec: req.Spec, Reason: req.Reason}, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func proposalID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, port.ErrProposalNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func (h ItemHandler) approveProposal(c *gin.Context) {
	id, ok := proposalID(c)
	if !ok {
		return
	}
	in, ok := h.itemInput(c)
	if !ok {
		return
	}
	r, err := h.Svc.ApproveProposal(c.Request.Context(), c.Query("profile"), id, in, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}

func (h ItemHandler) rejectProposal(c *gin.Context) {
	id, ok := proposalID(c)
	if !ok {
		return
	}
	var req struct {
		Note string `json:"note"`
	}
	if !bind(c, &req) {
		return
	}
	r, err := h.Svc.RejectProposal(c.Request.Context(), c.Query("profile"), id, req.Note, idem(c))
	h.reply(c, http.StatusCreated, r, err)
}
