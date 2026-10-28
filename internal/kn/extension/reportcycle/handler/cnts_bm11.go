package handler

// W11.45: Báo cáo phòng CNTS BM-11 — danh sách, tính thử, tạo, soạn, làm mới số liệu, trình, duyệt (tự gửi), trả lại, hộp
// "Báo cáo đến" của phòng nhận + "Đã nhận", Word qua pf/export.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	exportport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/export/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerCNTSBM11(g *gin.RouterGroup) {
	g.GET("/cnts/bm11/reports", h.bm11List)
	g.POST("/cnts/bm11/reports", h.bm11Create)
	g.GET("/cnts/bm11/preview", h.bm11Preview)
	g.GET("/cnts/bm11/inbox", h.bm11Inbox)
	g.GET("/cnts/bm11/reports/:id", h.bm11Get)
	g.PUT("/cnts/bm11/reports/:id/sections", h.bm11Edit)
	g.POST("/cnts/bm11/reports/:id/refresh", h.bm11Seq(func(h Handler, c *gin.Context, id uuid.UUID, r bm11SeqReq) (app.BM11View, error) {
		return h.Svc.RefreshBM11(c.Request.Context(), id, r.ExpectedSeq, idem(c))
	}))
	g.POST("/cnts/bm11/reports/:id/submit", h.bm11Seq(func(h Handler, c *gin.Context, id uuid.UUID, r bm11SeqReq) (app.BM11View, error) {
		return h.Svc.SubmitBM11(c.Request.Context(), id, r.ExpectedSeq, idem(c))
	}))
	g.POST("/cnts/bm11/reports/:id/approve", h.bm11Seq(func(h Handler, c *gin.Context, id uuid.UUID, r bm11SeqReq) (app.BM11View, error) {
		return h.Svc.ApproveBM11(c.Request.Context(), id, r.ExpectedSeq, idem(c))
	}))
	g.POST("/cnts/bm11/reports/:id/return", h.bm11Seq(func(h Handler, c *gin.Context, id uuid.UUID, r bm11SeqReq) (app.BM11View, error) {
		return h.Svc.ReturnBM11(c.Request.Context(), id, r.ExpectedSeq, r.Comment, idem(c))
	}))
	g.POST("/cnts/bm11/reports/:id/receive", h.bm11Receive)
	g.POST("/cnts/bm11/reports/:id/word", h.bm11Word)
	g.GET("/cnts/bm11/reports/:id/words", h.bm11Words)
	g.GET("/cnts/bm11/reports/:id/words/:run/download", h.bm11WordDownload)
}

func (h Handler) bm11List(c *gin.Context) {
	v, err := h.Svc.ListBM11(c.Request.Context(), c.Query("province"), c.Query("type"), c.Query("status"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, v)
}

func (h Handler) bm11Inbox(c *gin.Context) {
	v, err := h.Svc.InboxBM11(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, v)
}

func (h Handler) bm11Preview(c *gin.Context) {
	v, err := h.Svc.PreviewBM11(c.Request.Context(), c.Query("province"), c.Query("type"), c.Query("key"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, v)
}

type bm11CreateReq struct {
	Type       string   `json:"type"`
	PeriodKey  string   `json:"period_key"`
	Recipients []string `json:"recipients"`
}

func (h Handler) bm11Create(c *gin.Context) {
	var req bm11CreateReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.CreateBM11(c.Request.Context(), app.BM11CreateInput{Type: req.Type, Key: req.PeriodKey, Recipients: req.Recipients}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if v.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, v)
}

func (h Handler) bm11Get(c *gin.Context) {
	id, ok := pathID(c, app.ErrBM11NotFound)
	if !ok {
		return
	}
	v, err := h.Svc.GetBM11(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, v)
}

type bm11EditReq struct {
	ExpectedSeq *int64            `json:"expected_seq"`
	Sections    map[string]string `json:"sections"`
	Programs    map[string]string `json:"programs"`
}

func (h Handler) bm11Edit(c *gin.Context) {
	id, ok := pathID(c, app.ErrBM11NotFound)
	if !ok {
		return
	}
	var req bm11EditReq
	if !bind(c, &req) {
		return
	}
	v, err := h.Svc.EditBM11(c.Request.Context(), id, app.BM11EditInput{ExpectedSeq: req.ExpectedSeq, Sections: req.Sections, Programs: req.Programs}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

type bm11SeqReq struct {
	ExpectedSeq *int64 `json:"expected_seq"`
	Comment     string `json:"comment"`
}

func (h Handler) bm11Seq(fn func(Handler, *gin.Context, uuid.UUID, bm11SeqReq) (app.BM11View, error)) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, app.ErrBM11NotFound)
		if !ok {
			return
		}
		var req bm11SeqReq
		if !bind(c, &req) {
			return
		}
		v, err := fn(h, c, id, req)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusOK, v)
	}
}

func (h Handler) bm11Receive(c *gin.Context) {
	id, ok := pathID(c, app.ErrBM11NotFound)
	if !ok {
		return
	}
	v, err := h.Svc.ReceiveBM11(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, v)
}

type bm11WordDTO struct {
	ID              uuid.UUID `json:"id"`
	Seq             int64     `json:"seq"`
	Template        string    `json:"template"`
	TemplateVersion int       `json:"template_version"`
	DocNumber       string    `json:"doc_number"`
	DocNumberAuto   bool      `json:"doc_number_auto"`
	FileName        string    `json:"file_name"`
	BodyHash        string    `json:"body_hash"`
	Size            int64     `json:"size"`
	Actor           uuid.UUID `json:"actor_ref"`
	At              time.Time `json:"at"`
}

func bm11WordOf(r exportport.Run) bm11WordDTO {
	return bm11WordDTO{ID: r.ID, Seq: r.Seq, Template: r.Template, TemplateVersion: r.TemplateVersion, DocNumber: r.DocNumber,
		DocNumberAuto: r.DocNumberAuto, FileName: r.FileName, BodyHash: r.BodyHash, Size: r.Size, Actor: r.ActorRef, At: r.At.UTC()}
}

func (h Handler) bm11Word(c *gin.Context) {
	id, ok := pathID(c, app.ErrBM11NotFound)
	if !ok {
		return
	}
	run, err := h.Svc.ExportBM11Word(c.Request.Context(), id, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, bm11WordOf(run))
}

func (h Handler) bm11Words(c *gin.Context) {
	id, ok := pathID(c, app.ErrBM11NotFound)
	if !ok {
		return
	}
	rows, err := h.Svc.BM11WordHistory(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]bm11WordDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, bm11WordOf(r))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) bm11WordDownload(c *gin.Context) {
	id, ok := pathID(c, app.ErrBM11NotFound)
	if !ok {
		return
	}
	run, err := uuid.Parse(c.Param("run"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("run", "phải là UUID"))
		return
	}
	r, err := h.Svc.BM11WordDownload(c.Request.Context(), id, run)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+r.FileName+`"`)
	c.Header("X-Content-SHA256", r.BodyHash)
	c.Data(http.StatusOK, r.ContentType, r.Body)
}
