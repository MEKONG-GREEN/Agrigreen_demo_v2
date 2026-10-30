package handler

import (
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
)

func (h Handler) openRecoveries(c *gin.Context) {
	xs, e := h.Svc.AdhocOpenRecoveries(c.Request.Context())
	if e != nil {
		httpx.Fail(c, e)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": xs})
}
func (h Handler) reconcileOpen(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var payload struct {
		ExpectedGeneration *int64 `json:"expected_generation"`
		Reason             string `json:"reason"`
	}
	d := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	d.DisallowUnknownFields()
	if e := d.Decode(&payload); e != nil {
		httpx.Fail(c, problem.BadRequest)
		return
	}
	if e := d.Decode(new(any)); e != io.EOF {
		httpx.Fail(c, problem.BadRequest)
		return
	}
	if payload.ExpectedGeneration == nil {
		httpx.Fail(c, problem.Validation.WithField("expected_generation", "bắt buộc"))
		return
	}
	out, e := h.Svc.ReconcileAdhocOpen(c.Request.Context(), id, app.AdhocOpenReconcileInput{ExpectedGeneration: *payload.ExpectedGeneration, Reason: payload.Reason})
	if e != nil {
		httpx.Fail(c, e)
		return
	}
	c.JSON(http.StatusOK, periodOf(out))
}
