package handler

// W11.46 (D4 CN6, BRD CNTS FN-12 CV-06/CV-07) — địa điểm công tác và check-in GPS của lịch công tác.

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func placeDTO(p *domain.GeoPoint) any {
	if p == nil {
		return nil
	}
	return gin.H{"lat": p.Lat, "lon": p.Lon, "source": p.Source}
}

func checkinDTO(c store.Checkin) gin.H {
	var loc any
	if c.Point != nil {
		loc = gin.H{"lat": c.Point.Lat, "lon": c.Point.Lon}
	}
	return gin.H{"id": c.ID, "item_id": c.ItemID, "day": c.Day.Format(time.DateOnly), "captured_at": c.CapturedAt.UTC().Format(time.RFC3339),
		"recorded_at": c.RecordedAt.UTC().Format(time.RFC3339), "accuracy_m": c.AccuracyM, "distance_m": c.DistanceM, "far": c.Far,
		"weak_accuracy": c.WeakAccuracy, "mock_location": c.Mock, "point_stored": c.PointStored, "location": loc}
}

// addCheckins thêm check-in (khóa theo mục) và số đếm vào đối tượng lịch.
func addCheckins(m gin.H, d app.ScheduleDetail) {
	m["checkin_enabled"] = d.CheckinEnabled
	list := make([]gin.H, 0, len(d.Checkins))
	for _, it := range d.Items {
		if c, ok := d.Checkins[it.ItemID]; ok {
			list = append(list, checkinDTO(c))
		}
	}
	missed := 0
	for _, w := range d.Warnings {
		if w.Code == "checkin_missed" {
			missed = len(w.ItemIDs)
		}
	}
	m["checkins"], m["checkin_count"], m["missed_checkin_count"] = list, len(list), missed
}

type checkinReq struct {
	Lat          *float64 `json:"lat"`
	Lon          *float64 `json:"lon"`
	AccuracyM    *float64 `json:"accuracy_m"`
	CapturedAt   string   `json:"captured_at"`
	MockLocation bool     `json:"mock_location"`
	DeviceID     *string  `json:"device_id"`
}

func (h Handler) checkin(c *gin.Context) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return
	}
	item, err := uuid.Parse(c.Param("item_id"))
	if err != nil {
		httpx.Fail(c, app.ErrScheduleItemNotFound)
		return
	}
	var req checkinReq
	if !bind(c, &req) {
		return
	}
	v, bad := problem.Validation, false
	if req.Lat == nil {
		v, bad = v.WithField("lat", "bắt buộc"), true
	}
	if req.Lon == nil {
		v, bad = v.WithField("lon", "bắt buộc"), true
	}
	at, err := time.Parse(time.RFC3339, req.CapturedAt)
	if err != nil {
		v, bad = v.WithField("captured_at", "thời điểm RFC 3339"), true
	}
	if bad {
		httpx.Fail(c, v)
		return
	}
	res, err := h.Svc.Checkin(c.Request.Context(), app.CheckinRequest{ScheduleID: id, ItemID: item, Lat: *req.Lat, Lon: *req.Lon,
		AccuracyM: req.AccuracyM, CapturedAt: at, Mock: req.MockLocation, DeviceID: req.DeviceID, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	warnings := make([]gin.H, 0, len(res.Warnings))
	for _, w := range res.Warnings {
		warnings = append(warnings, gin.H{"code": w.Code, "message": w.Message, "item_ids": w.ItemIDs})
	}
	status := http.StatusCreated
	if res.Replayed {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"checkin": checkinDTO(res.Checkin), "warnings": warnings})
}
