package handler

import (
	"net/http"
	"strconv"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
)

func (h Handler) registerCNTSDesign(g *gin.RouterGroup) {
	g.GET("/cnts/form-designs", h.cntsDesign)
	g.POST("/cnts/form-designs", h.cntsDesignCreate)
	httpx.AllowBody(g, http.MethodPost, "/cnts/form-designs/import-preview", app.MaxDesignImportBytes+1024)
	g.POST("/cnts/form-designs/import-preview", h.cntsDesignImport)
	g.GET("/cnts/form-designs/:code/versions", h.cntsDesignHistory)
	g.POST("/cnts/form-designs/:code/versions", h.cntsDesignSave)
}
func (h Handler) cntsDesign(c *gin.Context) {
	items, err := h.Svc.CNTSDesign(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
func (h Handler) cntsDesignImport(c *gin.Context) {
	result, err := h.Svc.PreviewCNTSDesignImport(c.Request.Context(), c.Request.Body)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, result)
}
func (h Handler) cntsDesignHistory(c *gin.Context) {
	var before int64
	if v := c.Query("before_version"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation.WithField("before_version", "số nguyên không âm"))
			return
		}
		before = n
	}
	n := limit(c)
	if n == 0 {
		n = 50
	}
	items, err := h.Svc.CNTSDesignHistory(c.Request.Context(), c.Param("code"), before, n)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type cntsDesignSaveReq struct {
	BaseVersion *int64             `json:"base_version"`
	Title       string             `json:"title"`
	Fields      []domain.CNTSField `json:"fields"`
}

func (h Handler) cntsDesignSave(c *gin.Context) {
	var r cntsDesignSaveReq
	if !bind(c, &r) {
		return
	}
	if r.BaseVersion == nil {
		httpx.Fail(c, problem.Validation.WithField("base_version", "bắt buộc — phiên bản đã đọc (0 khi biểu chưa có phiên bản)"))
		return
	}
	v, err := h.Svc.SaveCNTSDesign(c.Request.Context(), c.Param("code"), app.CNTSDesignSave{BaseVersion: *r.BaseVersion, Title: r.Title,
		Fields: r.Fields}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": c.Param("code"), "version": v})
}

type cntsDesignCreateReq struct {
	CodeSuffix    string             `json:"code_suffix"`
	Title         string             `json:"title"`
	SourceCode    string             `json:"source_code"`
	SourceVersion int64              `json:"source_version"`
	Fields        []domain.CNTSField `json:"fields"`
}

func (h Handler) cntsDesignCreate(c *gin.Context) {
	var r cntsDesignCreateReq
	if !bind(c, &r) {
		return
	}
	code, v, err := h.Svc.CreateCNTSDesign(c.Request.Context(), app.CNTSDesignCreate{CodeSuffix: r.CodeSuffix, Title: r.Title,
		SourceCode: r.SourceCode, SourceVersion: r.SourceVersion, Fields: r.Fields}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"code": code, "version": v})
}
