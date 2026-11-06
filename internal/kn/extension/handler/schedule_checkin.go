package handler

// W11.46 (D4 CN6, BRD CNTS FN-12 CV-06/CV-07) — địa điểm công tác và check-in GPS của lịch công tác.
// CNTS-V4-CHECKOUT-PHOTO (#249, BRD CNTS v4.0 FN-12 CV-07/CV-08): ảnh bắt buộc, check-out, tải ảnh.

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func placeDTO(p *domain.GeoPoint) any {
	if p == nil {
		return nil
	}
	return gin.H{"lat": p.Lat, "lon": p.Lon, "source": p.Source}
}

func pointDTO(p *domain.GeoPoint) any {
	if p == nil {
		return nil
	}
	return gin.H{"lat": p.Lat, "lon": p.Lon}
}

func photoDTO(p *store.VisitPhoto) any {
	if p == nil {
		return nil
	}
	return gin.H{"content_type": p.ContentType, "size_bytes": p.Size}
}

// checkinDTO: check-in kèm ảnh (null + legacy_no_photo = dữ liệu cũ trước CNTS v4.0) và check-out của mục (null = chưa).
func checkinDTO(c store.Checkin) gin.H {
	var out any
	if o := c.Checkout; o != nil {
		out = checkoutDTO(*o, c)
	}
	return gin.H{"id": c.ID, "item_id": c.ItemID, "day": c.Day.Format(time.DateOnly), "captured_at": c.CapturedAt.UTC().Format(time.RFC3339),
		"recorded_at": c.RecordedAt.UTC().Format(time.RFC3339), "accuracy_m": c.AccuracyM, "distance_m": c.DistanceM, "far": c.Far,
		"weak_accuracy": c.WeakAccuracy, "mock_location": c.Mock, "point_stored": c.PointStored, "location": pointDTO(c.Point),
		"photo": photoDTO(c.Photo), "legacy_no_photo": c.Photo == nil, "checkout": out}
}

// checkoutDTO: check-out (CV-08), thời gian ở địa điểm (BR-07, AC-12), "Đồng bộ trễ" (BR-08, AC-13).
func checkoutDTO(o store.Checkout, in store.Checkin) gin.H {
	return gin.H{"id": o.ID, "checkin_id": o.CheckinID, "item_id": o.ItemID, "day": o.Day.Format(time.DateOnly),
		"captured_at": o.CapturedAt.UTC().Format(time.RFC3339), "recorded_at": o.RecordedAt.UTC().Format(time.RFC3339),
		"accuracy_m": o.AccuracyM, "distance_m": o.DistanceM, "far": o.Far, "weak_accuracy": o.WeakAccuracy, "mock_location": o.Mock,
		"point_stored": o.PointStored, "location": pointDTO(o.Point), "photo": photoDTO(&o.Photo),
		"stay_minutes": domain.StayMinutes(in.CapturedAt, o.CapturedAt), "synced_at": o.EventTime.UTC().Format(time.RFC3339),
		"synced_late": domain.SyncedLate(o.Day, o.EventTime, clock.Zone())}
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
	// Photo: ảnh chụp tại chỗ đã nén (≤ 500 KB), base64 chuẩn — bắt buộc (CNTS v4.0 FN-12). JSON để hàng đợi ngoại
	// tuyến của Mobile gửi lại nguyên lệnh (một request, một Idempotency-Key).
	Photo string `json:"photo"`
}

// visitRequest đọc và kiểm thân check-in/check-out.
func visitRequest(c *gin.Context) (app.CheckinRequest, bool) {
	id, ok := pathID(c, app.ErrScheduleNotFound)
	if !ok {
		return app.CheckinRequest{}, false
	}
	item, err := uuid.Parse(c.Param("item_id"))
	if err != nil {
		httpx.Fail(c, app.ErrScheduleItemNotFound)
		return app.CheckinRequest{}, false
	}
	var req checkinReq
	if !bind(c, &req) {
		return app.CheckinRequest{}, false
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
	photo, err := base64.StdEncoding.DecodeString(req.Photo)
	switch {
	case req.Photo == "":
		v, bad = v.WithField("photo", domain.ErrVisitPhotoRequired.Error()), true
	case err != nil:
		v, bad = v.WithField("photo", "ảnh mã hóa base64 chuẩn"), true
	}
	if bad {
		httpx.Fail(c, v)
		return app.CheckinRequest{}, false
	}
	return app.CheckinRequest{ScheduleID: id, ItemID: item, Lat: *req.Lat, Lon: *req.Lon, AccuracyM: req.AccuracyM, CapturedAt: at,
		Mock: req.MockLocation, DeviceID: req.DeviceID, Photo: photo, IdemKey: idem(c)}, true
}

func warningsDTO(ws []app.ScheduleWarning) []gin.H {
	out := make([]gin.H, 0, len(ws))
	for _, w := range ws {
		out = append(out, gin.H{"code": w.Code, "message": w.Message, "item_ids": w.ItemIDs})
	}
	return out
}

// writeVisit trả kết quả check-in/check-out; cache chống trùng chỉ giữ metadata (QĐ-8: không tạo bản sao vị trí ngoài
// bảng có hạn lưu).
func writeVisit(c *gin.Context, replayed bool, full, cachedBody gin.H) {
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	cached, err := json.Marshal(cachedBody)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	idempotency.SetStoredBody(c, cached)
	c.JSON(status, full)
}

func (h Handler) checkin(c *gin.Context) {
	in, ok := visitRequest(c)
	if !ok {
		return
	}
	res, err := h.Svc.Checkin(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	warnings := warningsDTO(res.Warnings)
	metadata := res.Checkin
	metadata.Point = nil
	writeVisit(c, res.Replayed, gin.H{"checkin": checkinDTO(res.Checkin), "warnings": warnings},
		gin.H{"checkin": checkinDTO(metadata), "warnings": warnings})
}

// checkout: "Check-out" khi rời địa điểm (CNTS v4.0 CV-08, FN-12 B4b).
func (h Handler) checkout(c *gin.Context) {
	in, ok := visitRequest(c)
	if !ok {
		return
	}
	res, err := h.Svc.Checkout(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	warnings := warningsDTO(res.Warnings)
	metadata := res.Checkout
	metadata.Point = nil
	writeVisit(c, res.Replayed, gin.H{"checkout": checkoutDTO(res.Checkout, res.Checkin), "warnings": warnings},
		gin.H{"checkout": checkoutDTO(metadata, res.Checkin), "warnings": warnings})
}

// visitPhoto trả ảnh check-in / check-out của mục (người đăng ký, người duyệt lịch).
func (h Handler) visitPhoto(kind string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := pathID(c, app.ErrScheduleNotFound)
		if !ok {
			return
		}
		item, err := uuid.Parse(c.Param("item_id"))
		if err != nil {
			httpx.Fail(c, app.ErrVisitPhotoMissing)
			return
		}
		f, err := h.Svc.VisitPhoto(c.Request.Context(), id, item, kind)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		defer func() { _ = f.Body.Close() }()
		c.Header("Cache-Control", "private, no-store")
		c.Header("X-Content-Type-Options", "nosniff")
		c.DataFromReader(http.StatusOK, -1, f.ContentType, f.Body, nil)
	}
}
