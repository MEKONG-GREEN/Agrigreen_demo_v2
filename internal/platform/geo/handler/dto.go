// Package handler: HTTP của pf/geo (contracts/openapi/application.yaml và admin.yaml,
// mục "# --- pf/geo ---").
package handler

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func refOrNil(u uuid.UUID) any {
	if u == uuid.Nil {
		return nil
	}
	return u.String()
}

func strOrNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// plotFeature là thửa dạng GeoJSON Feature (RFC 7946); id = ref.
func plotFeature(p store.Plot) gin.H {
	return gin.H{
		"type":     "Feature",
		"id":       p.Ref.String(),
		"geometry": json.RawMessage(p.GeoJSON),
		"properties": gin.H{
			"ref": p.Ref.String(), "code": p.Code, "name": strOrNil(p.Name), "commune_code": p.CommuneCode,
			"cooperative_ref": refOrNil(p.CooperativeRef), "household_ref": refOrNil(p.HouseholdRef), "status": p.Status,
			"status_reason": strOrNil(p.StatusReason), "version": p.CurrentVersion, "boundary_id": p.CurrentBoundaryID.String(),
			"area_ha": p.AreaHa.StringFixed(4), "source": p.Source, "needs_review": p.NeedsReview, "revision": p.Revision,
			"created_at": ts(p.CreatedAt), "updated_at": ts(p.UpdatedAt),
		},
	}
}

func featureCollection(items []store.Plot) []gin.H {
	out := make([]gin.H, 0, len(items))
	for _, p := range items {
		out = append(out, plotFeature(p))
	}
	return out
}

func boundaryDTO(b store.Boundary) gin.H {
	m := gin.H{
		"boundary_id": b.ID.String(), "plot_ref": b.PlotRef.String(), "version": b.Version, "area_ha": b.AreaHa.StringFixed(4),
		"point_count": b.PointCount, "gps_accuracy_m": nil, "source": b.Source, "commune_code": b.CommuneCode,
		"cooperative_ref": refOrNil(b.CooperativeRef), "became_current": b.BecameCurrent, "event_time": ts(b.EventTime),
		"recorded_at": ts(b.RecordedAt), "supersedes_id": nil, "correction_reason": b.CorrectionReason,
		"drawn_by": b.ActorRef.String(), "device_id": b.DeviceID,
	}
	if b.GPSAccuracyM != nil {
		m["gps_accuracy_m"] = b.GPSAccuracyM.String()
	}
	if b.SupersedesID != nil {
		m["supersedes_id"] = b.SupersedesID.String()
	}
	if b.GeoJSON != "" {
		m["geometry"] = json.RawMessage(b.GeoJSON)
	}
	return m
}

func conflictDTO(c store.Conflict) gin.H {
	others := make([]string, 0, len(c.OtherPlotRefs))
	for _, o := range c.OtherPlotRefs {
		others = append(others, o.String())
	}
	m := gin.H{
		"id": c.ID.String(), "kind": c.Kind, "plot_ref": c.PlotRef.String(), "boundary_id": c.BoundaryID.String(),
		"other_plot_refs": others, "max_overlap_pct": nil, "commune_code": c.CommuneCode, "owner_ref": refOrNil(c.OwnerRef),
		"due_at": ts(c.DueAt), "status": c.Status, "detected_at": ts(c.DetectedAt), "resolved_by": refOrNil(c.ResolvedBy),
		"resolved_at": nil, "resolution_note": strOrNil(c.ResolutionNote),
	}
	if c.MaxOverlapPct != nil {
		m["max_overlap_pct"] = c.MaxOverlapPct.StringFixed(4)
	}
	if c.ResolvedAt != nil {
		m["resolved_at"] = ts(*c.ResolvedAt)
	}
	return m
}

func conflictList(cs []store.Conflict) []gin.H {
	out := make([]gin.H, 0, len(cs))
	for _, c := range cs {
		out = append(out, conflictDTO(c))
	}
	return out
}

func overlapDTOs(os []domain.Overlap) []gin.H {
	out := make([]gin.H, 0, len(os))
	for _, o := range os {
		out = append(out, gin.H{
			"plot_ref": o.PlotRef.String(), "code": o.Code, "cooperative_ref": refOrNil(o.CooperativeRef),
			"intersect_ha": o.IntersectHa.StringFixed(4), "overlap_pct": o.Pct.StringFixed(4),
		})
	}
	return out
}

func decOrNil(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return d.StringFixed(4)
}

func encodeCursor(u *uuid.UUID) any {
	if u == nil {
		return nil
	}
	return base64.RawURLEncoding.EncodeToString(u[:])
}

func decodeCursor(c *gin.Context) (uuid.UUID, error) {
	var after uuid.UUID
	cur := c.Query("cursor")
	if cur == "" {
		return after, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(cur)
	if err != nil || len(b) != 16 {
		return after, problem.Validation.WithField("cursor", "không hợp lệ")
	}
	copy(after[:], b)
	return after, nil
}
