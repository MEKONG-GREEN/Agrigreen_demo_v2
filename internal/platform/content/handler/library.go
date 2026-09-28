package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/content/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// registerLibrary gắn route danh mục cá nhân (KN US-LIB-05).
func (h Handler) registerLibrary(rg *gin.RouterGroup) {
	rg.GET("/library", h.library)
	rg.POST("/documents/:ref/save", h.saveDoc)
	rg.POST("/documents/:ref/unsave", h.unsaveDoc)
}

type libraryItemDTO struct {
	DocumentRef string  `json:"document_ref"`
	SavedAt     string  `json:"saved_at"`
	Note        *string `json:"note"`
	Available   bool    `json:"available"`
	Kind        *string `json:"kind"`
	Title       *string `json:"title"`
	Summary     *string `json:"summary"`
	PublishedAt *string `json:"published_at"`
}

func toLibraryDTO(e app.LibraryEntry) libraryItemDTO {
	d := libraryItemDTO{DocumentRef: e.Item.DocumentRef.String(), SavedAt: e.Item.EventTime.UTC().Format(time.RFC3339Nano), Note: e.Item.Note}
	if e.Doc != nil {
		k, t, s := string(e.Doc.Kind), e.Doc.Title, e.Doc.Summary
		d.Available, d.Kind, d.Title, d.Summary = true, &k, &t, &s
		if e.Doc.PublishedAt != nil {
			p := e.Doc.PublishedAt.UTC().Format(time.RFC3339Nano)
			d.PublishedAt = &p
		}
	}
	return d
}

func docRef(c *gin.Context) (uuid.UUID, bool) {
	ref, err := uuid.Parse(c.Param("ref"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return ref, true
}

func (h Handler) library(c *gin.Context) {
	var before int64
	if cur := c.Query("cursor"); cur != "" {
		n, err := strconv.ParseInt(cur, 10, 64)
		if err != nil || n <= 0 {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		before = n
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	pg, err := h.Svc.Library(c.Request.Context(), before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]libraryItemDTO, 0, len(pg.Items))
	for _, e := range pg.Items {
		items = append(items, toLibraryDTO(e))
	}
	var next any
	if pg.Next > 0 {
		next = strconv.FormatInt(pg.Next, 10)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}

func (h Handler) saveDoc(c *gin.Context) {
	ref, ok := docRef(c)
	if !ok {
		return
	}
	var req struct {
		Note *string `json:"note"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
			return
		}
	}
	e, err := h.Svc.SaveDocument(c.Request.Context(), ref, req.Note)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toLibraryDTO(e))
}

func (h Handler) unsaveDoc(c *gin.Context) {
	ref, ok := docRef(c)
	if !ok {
		return
	}
	it, err := h.Svc.UnsaveDocument(c.Request.Context(), ref)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"document_ref": it.DocumentRef.String(), "saved": false, "removed_at": it.EventTime.UTC().Format(time.RFC3339Nano)})
}
