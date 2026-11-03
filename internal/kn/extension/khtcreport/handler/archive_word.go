package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func archiveWordID(c *gin.Context, key string) (uuid.UUID, bool) {
	raw := c.Param(key)
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil || len(raw) != 36 {
		httpx.Fail(c, app.ErrInvalid.WithDetail("Mã bản xuất hoặc tệp không hợp lệ"))
		return uuid.Nil, false
	}
	return id, true
}
func archiveWordsQuery(values url.Values) (app.ArchiveWordsQuery, error) {
	q := app.ArchiveWordsQuery{Limit: 20}
	for key, target := range map[string]*int{"offset": &q.Offset, "limit": &q.Limit} {
		if len(values[key]) > 1 {
			return q, app.ErrInvalid
		}
		raw := values.Get(key)
		if raw == "" {
			continue
		}
		if len(raw) > 6 {
			return q, app.ErrInvalid
		}
		for _, c := range raw {
			if c < '0' || c > '9' {
				return q, app.ErrInvalid
			}
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return q, app.ErrInvalid
		}
		*target = value
	}
	return q, q.Validate()
}
func (h Handler) archiveWords(c *gin.Context) {
	period, ok := archiveWordID(c, "id")
	if !ok {
		return
	}
	export, ok := archiveWordID(c, "exportID")
	if !ok {
		return
	}
	q, err := archiveWordsQuery(c.Request.URL.Query())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out, err := h.Svc.ArchiveWords(c.Request.Context(), period, export, q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, out)
}
func (h Handler) archiveWordDownload(c *gin.Context) {
	period, ok := archiveWordID(c, "id")
	if !ok {
		return
	}
	export, ok := archiveWordID(c, "exportID")
	if !ok {
		return
	}
	runID, ok := archiveWordID(c, "runID")
	if !ok {
		return
	}
	run, err := h.Svc.DownloadArchiveWord(c.Request.Context(), period, export, runID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("X-Content-SHA256", run.BodyHash)
	c.Header("Content-Disposition", `attachment; filename="`+run.FileName+`"`)
	c.Data(http.StatusOK, run.ContentType, run.Body)
}
func archiveZIPHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
