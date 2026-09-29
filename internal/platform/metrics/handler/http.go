// Package handler: HTTP của pf/metrics (contracts/openapi/application.yaml, mục
// "# --- pf/metrics ---"). Chỉ Application API, chỉ đọc.
package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route Application API.
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/metrics/definitions", h.definitions)
	rg.GET("/metrics/:key/values", h.values)
	rg.GET("/gis/events", h.events)
	rg.GET("/gis/snapshots/:date", h.snapshots)
	rg.GET("/gis/replay", h.replay)
}

// RegisterAdmin: không có route quản trị (backfill chạy bằng lệnh worker).
func (h Handler) RegisterAdmin(*gin.RouterGroup) {}

func dateParam(c *gin.Context, field, raw string) (*clock.Date, bool) {
	if raw == "" {
		return nil, true
	}
	d, err := domain.ParseDate(raw)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(field, "định dạng YYYY-MM-DD"))
		return nil, false
	}
	return &d, true
}

func layersQuery(c *gin.Context) []string {
	raw := strings.TrimSpace(c.Query("layers"))
	if raw == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func wmJSON(w port.Watermark) any {
	if w.LastEventID == uuid.Nil {
		return nil
	}
	return gin.H{"last_event_id": w.LastEventID, "recorded_at": w.RecordedAt.UTC().Format(time.RFC3339Nano)}
}

func (h Handler) definitions(c *gin.Context) {
	defs, err := h.Svc.ListDefinitions(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(defs))
	for _, d := range defs {
		items = append(items, gin.H{
			"key": d.Key, "version": d.Version, "title": d.Title, "description": d.Description, "unit": d.Unit,
			"grain": d.Grain, "aggregation": d.Aggregation, "source_module": d.SourceModule, "source": d.Source,
			"rule_version": d.RuleVersion, "effective_from": d.EffectiveFrom.String(), "proposed_by": d.ProposedBy,
			"approved_by": d.ApprovedBy, "file": d.File,
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

func (h Handler) values(c *gin.Context) {
	from, ok := dateParam(c, "from", c.Query("from"))
	if !ok {
		return
	}
	to, ok := dateParam(c, "to", c.Query("to"))
	if !ok {
		return
	}
	res, err := h.Svc.Values(c.Request.Context(), app.ValuesQuery{
		Key: port.Key(c.Param("key")), From: from, To: to, Unit: strings.TrimSpace(c.Query("unit")),
		Dimension: strings.TrimSpace(c.Query("dimension")), History: c.Query("history") == "true",
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(res.Items))
	for _, v := range res.Items {
		items = append(items, valueJSON(v))
	}
	c.JSON(http.StatusOK, gin.H{
		"key": res.Def.Key, "title": res.Def.Title, "unit": res.Def.Unit, "grain": res.Def.Grain,
		"scope_unit": res.Unit, "dimension": res.Dimension, "from": res.From.String(), "to": res.To.String(),
		"items": items, "dimensions": res.Dimensions,
	})
}

func valueJSON(v store.ValueRow) gin.H {
	return gin.H{
		"id": v.ID, "business_date": v.BusinessDate.String(), "value": v.Value.String(), "definition_version": v.DefVersion,
		"cutoff": v.Cutoff.UTC().Format(time.RFC3339), "watermark": wmJSON(v.Watermark),
		"computed_at": v.RecordedAt.UTC().Format(time.RFC3339Nano), "supersedes_id": v.SupersedesID,
		"correction_reason": v.CorrectionReason, "current": v.Current,
	}
}

func encodeCursor(w *port.Watermark) any {
	if w == nil {
		return nil
	}
	b, _ := json.Marshal([]string{w.RecordedAt.UTC().Format(time.RFC3339Nano), w.LastEventID.String()})
	return base64.RawURLEncoding.EncodeToString(b)
}

func decodeCursor(s string) (*port.Watermark, bool) {
	if s == "" {
		return nil, true
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	var parts []string
	if err != nil || json.Unmarshal(b, &parts) != nil || len(parts) != 2 {
		return nil, false
	}
	t, err1 := time.Parse(time.RFC3339Nano, parts[0])
	id, err2 := uuid.Parse(parts[1])
	if err1 != nil || err2 != nil {
		return nil, false
	}
	return &port.Watermark{LastEventID: id, RecordedAt: t}, true
}

func (h Handler) events(c *gin.Context) {
	from, ok := dateParam(c, "from", c.Query("from"))
	if !ok {
		return
	}
	to, ok := dateParam(c, "to", c.Query("to"))
	if !ok {
		return
	}
	after, ok := decodeCursor(c.Query("cursor"))
	if !ok {
		httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
		return
	}
	var feature uuid.UUID
	if s := c.Query("feature_ref"); s != "" {
		var err error
		if feature, err = uuid.Parse(s); err != nil {
			httpx.Fail(c, problem.Validation.WithField("feature_ref", "phải là UUID"))
			return
		}
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	page, err := h.Svc.ListEvents(c.Request.Context(), app.EventsQuery{
		Layer: strings.TrimSpace(c.Query("layer")), FeatureRef: feature, From: from, To: to, After: after, Limit: limit,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(page.Items))
	for _, r := range page.Items {
		items = append(items, gin.H{
			"id": r.ID, "layer": r.Layer, "feature_ref": r.FeatureRef, "op": r.Op, "before": raw(r.Before), "after": raw(r.After),
			"source_event_id": r.SourceEventID, "source_type": r.SourceType, "source_module": r.SourceModule,
			"event_time": r.EventTime.UTC().Format(time.RFC3339Nano), "recorded_at": r.RecordedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": encodeCursor(page.NextAfter), "from": page.From.String(), "to": page.To.String()})
}

func raw(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

func layerJSON(v app.LayerView) gin.H {
	feats := make([]gin.H, 0, len(v.Features))
	for _, f := range v.Features {
		x := gin.H{"feature_ref": f.FeatureRef, "attributes": raw(f.Attributes), "retired": f.Retired}
		if len(f.Geometry) > 0 {
			x["geometry"] = f.Geometry
		}
		feats = append(feats, x)
	}
	var base any
	if v.BaseDate != nil {
		base = v.BaseDate.String()
	}
	out := gin.H{"layer": v.Layer, "business_date": v.BusinessDate.String(), "watermark": wmJSON(v.Watermark), "basis": v.Basis,
		"base_snapshot_date": base, "feature_count": len(feats), "features": feats}
	if v.RecordedAt != nil {
		out["recorded_at"] = v.RecordedAt.UTC().Format(time.RFC3339Nano)
	}
	return out
}

func (h Handler) snapshots(c *gin.Context) {
	d, err := domain.ParseDate(c.Param("date"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("date", "định dạng YYYY-MM-DD"))
		return
	}
	views, err := h.Svc.SnapshotsOn(c.Request.Context(), d, layersQuery(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(views))
	for _, v := range views {
		items = append(items, layerJSON(v))
	}
	c.JSON(http.StatusOK, gin.H{"business_date": d.String(), "items": items})
}

func (h Handler) replay(c *gin.Context) {
	d, err := domain.ParseDate(c.Query("date"))
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField("date", "bắt buộc, định dạng YYYY-MM-DD"))
		return
	}
	views, err := h.Svc.Replay(c.Request.Context(), d, layersQuery(c), c.Query("geometry") == "true")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]gin.H, 0, len(views))
	for _, v := range views {
		items = append(items, layerJSON(v))
	}
	c.JSON(http.StatusOK, gin.H{"business_date": d.String(), "cutoff": port.Cutoff(d).UTC().Format(time.RFC3339), "items": items})
}
