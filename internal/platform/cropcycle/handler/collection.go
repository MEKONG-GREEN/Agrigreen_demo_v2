package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cropcycle/app"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerCollection gắn route thu gom MKG phía HTX (G3-2).
func (h Handler) registerCollection(rg *gin.RouterGroup) {
	rg.GET("/cropcycle/htx-collection/schedules", h.listSchedules)
	rg.GET("/cropcycle/htx-collection/statements", h.listStatements)
	rg.GET("/cropcycle/htx-collection/statements/:ref", h.getStatement)
	rg.POST("/cropcycle/htx-collection/statements/:ref/responses", h.respondStatement)
	// Luồng phản hồi cho MKG (ADR-0020): đường chuẩn ở bề mặt đối tác; đường cũ giữ làm bí danh
	// (cùng kiểm cấp quyền htx_collection_responses, cùng sổ đọc).
	rg.GET("/partner/htx-collection/responses", h.responseFeed)
	rg.GET("/cropcycle/htx-collection/responses", h.responseFeed)
}

func collectionList(c *gin.Context) (app.CollectionListInput, bool) {
	after, limit, ok := cursor(c)
	if !ok {
		return app.CollectionListInput{}, false
	}
	f := newFields()
	in := app.CollectionListInput{CooperativeRef: qRef(c, f, "cooperative_ref"), Status: c.Query("status"), After: after, Limit: limit}
	if f.bad {
		httpx.Fail(c, f.p)
		return in, false
	}
	return in, true
}

func (h Handler) listSchedules(c *gin.Context) {
	in, ok := collectionList(c)
	if !ok {
		return
	}
	pg, err := h.Svc.ListCollectionSchedules(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]app.ScheduleData, 0, len(pg.Items))
	for _, v := range pg.Items {
		items = append(items, app.ScheduleOut(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}

func (h Handler) listStatements(c *gin.Context) {
	in, ok := collectionList(c)
	if !ok {
		return
	}
	pg, err := h.Svc.ListCollectionStatements(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]app.StatementData, 0, len(pg.Items))
	for _, v := range pg.Items {
		items = append(items, app.StatementOut(v.StatementView, v.PlotCode))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}

func (h Handler) getStatement(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrStatementNotFound)
	if !ok {
		return
	}
	d, err := h.Svc.GetCollectionStatement(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	hist := make([]app.ResponseData, 0, len(d.Responses))
	for _, r := range d.Responses {
		hist = append(hist, app.ResponseOut(r))
	}
	c.JSON(http.StatusOK, gin.H{"statement": app.StatementOut(d.Statement.StatementView, d.Statement.PlotCode), "responses": hist})
}

func (h Handler) respondStatement(c *gin.Context) {
	ref, ok := pathID(c, "ref", app.ErrStatementNotFound)
	if !ok {
		return
	}
	var req app.CollectionResponsePayload
	if !bindJSON(c, &req) {
		return
	}
	r, err := h.Svc.RespondToStatement(c.Request.Context(), app.RespondInput{StatementRef: ref, Revision: req.Revision,
		Outcome: req.Outcome, HTXRolls: req.HTXRolls, Reason: req.Reason, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, r.Replayed, gin.H{"response": app.ResponseOut(r.Response), "statement": app.StatementOut(r.Statement, "")})
}

// feedItem là một phản hồi trong luồng cho MKG — chỉ trường MKG cần để ghi xác nhận.
type feedItem struct {
	ResponseID        string  `json:"response_id"`
	Cursor            int64   `json:"cursor"`
	StatementRef      string  `json:"statement_ref"`
	StatementRevision int     `json:"statement_revision"`
	HTXRef            string  `json:"htx_ref"`
	PlotRef           string  `json:"plot_ref"`
	MKGRolls          int     `json:"mkg_rolls"`
	Outcome           string  `json:"outcome"`
	HTXRolls          int     `json:"htx_rolls"`
	Reason            *string `json:"reason"`
	RespondedAt       string  `json:"responded_at"`
	Supersedes        *string `json:"supersedes_response_id"`
}

func (h Handler) responseFeed(c *gin.Context) {
	var after int64
	if s := c.Query("after"); s != "" {
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("after", "phải là số nguyên ≥ 0"))
			return
		}
		after = n
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	grantor, err := partyport.ParseGrantor(c.Query("grantor"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	pg, err := h.Svc.CollectionResponseFeed(c.Request.Context(), grantor, after, limit, "GET "+c.FullPath(), c.Request.URL.RawQuery)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]feedItem, 0, len(pg.Items))
	for _, r := range pg.Items {
		d := app.ResponseOut(r)
		items = append(items, feedItem{ResponseID: d.ID, Cursor: r.FeedSeq, StatementRef: r.StatementRef.String(),
			StatementRevision: r.StatementRevision, HTXRef: r.CooperativeRef.String(), PlotRef: r.PlotRef.String(), MKGRolls: r.MKGRolls,
			Outcome: r.Outcome, HTXRolls: r.HTXRolls, Reason: r.Reason, RespondedAt: d.RespondedAt, Supersedes: d.SupersedesID})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": pg.Next})
}
