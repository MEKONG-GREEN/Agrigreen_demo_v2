package handler

import (
	"fmt"
	"math"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/waterway/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Bề mặt đối tác (ADR-0020, G4-0): định tuyến đường thủy cho mkg/logistics — loại waterway_routes.
// Chỉ hạ tầng (hình học, km, phiên bản mạng, đoạn, ràng buộc tải trọng); không dữ liệu cá nhân.

type partnerLegDTO struct {
	From        location     `json:"from"`
	To          location     `json:"to"`
	Found       bool         `json:"found"`
	DistanceKm  string       `json:"distance_km"`
	AccessKm    string       `json:"access_km"`
	Source      string       `json:"source"`
	Geometry    *lineGeo     `json:"geometry"`
	SegmentRefs []string     `json:"segment_refs"`
	Limiting    *limitingDTO `json:"limiting"`
	Reason      *string      `json:"reason"`
}

type partnerRouteDTO struct {
	GrantorTenantID string          `json:"grantor_tenant_id"`
	NetworkVersion  *int            `json:"network_version"`
	Found           bool            `json:"found"`
	DistanceKm      string          `json:"distance_km"`
	Source          string          `json:"source"`
	Limiting        *limitingDTO    `json:"limiting"`
	Legs            []partnerLegDTO `json:"legs"`
}

func toPartnerRoute(p app.PartnerRoute) partnerRouteDTO {
	r := p.Route
	out := partnerRouteDTO{GrantorTenantID: p.Grantor.String(), Found: r.Found, DistanceKm: r.DistanceKm.StringFixed(3),
		Source: r.Source, Limiting: toLimiting(r.Limiting), Legs: make([]partnerLegDTO, 0, len(r.Legs))}
	if r.VersionNo > 0 {
		n := r.VersionNo
		out.NetworkVersion = &n
	}
	for _, l := range r.Legs {
		d := partnerLegDTO{From: location{Lat: l.From.Lat, Lng: l.From.Lng}, To: location{Lat: l.To.Lat, Lng: l.To.Lng},
			Found: l.Found, DistanceKm: l.DistanceKm.StringFixed(3), AccessKm: l.AccessKm.StringFixed(3), Source: l.Source,
			SegmentRefs: make([]string, 0, len(l.SegmentRefs)), Limiting: toLimiting(l.Limiting)}
		if l.Found {
			g := latLngLine(l.Path)
			d.Geometry = &g
		}
		for _, s := range l.SegmentRefs {
			d.SegmentRefs = append(d.SegmentRefs, s.String())
		}
		if l.Reason != "" {
			reason := l.Reason
			d.Reason = &reason
		}
		out.Legs = append(out.Legs, d)
	}
	return out
}

// partnerRouteGet: GET /v1/partner/waterway/route?grantor=&from=lat,lng&to=lat,lng&min_load_tons=&version=.
func (h Handler) partnerRouteGet(c *gin.Context) {
	grantor, err := partyport.ParseGrantor(c.Query("grantor"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	from, ok1 := parseFloats(c.Query("from"), 2)
	to, ok2 := parseFloats(c.Query("to"), 2)
	if !ok1 || !ok2 {
		p := problem.Validation
		if !ok1 {
			p = p.WithField("from", "định dạng lat,lng")
		}
		if !ok2 {
			p = p.WithField("to", "định dạng lat,lng")
		}
		httpx.Fail(c, p)
		return
	}
	ml, ok := minLoad(c)
	if !ok {
		return
	}
	ver, ok := queryVersion(c)
	if !ok {
		return
	}
	r, err := h.Svc.PartnerRoute(c.Request.Context(), grantor,
		[]port.LatLng{{Lat: from[0], Lng: from[1]}, {Lat: to[0], Lng: to[1]}}, ml, ver,
		"GET /v1/partner/waterway/route", c.Request.URL.RawQuery)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toPartnerRoute(r))
}

type partnerRouteReq struct {
	Points []struct {
		Lat *float64 `json:"lat"`
		Lng *float64 `json:"lng"`
	} `json:"points"`
	MinLoadTons *string `json:"min_load_tons"`
	Version     *int    `json:"version"`
}

// partnerRoutePost: POST /v1/partner/waterway/route?grantor= — dãy điểm (chuyến nhiều điểm dừng).
// Chỉ đọc; Idempotency-Key bắt buộc như mọi POST (R-51) — gọi lặp trả lại đúng kết quả đã ghi sổ đọc.
func (h Handler) partnerRoutePost(c *gin.Context) {
	grantor, err := partyport.ParseGrantor(c.Query("grantor"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var req partnerRouteReq
	if !bindJSON(c, &req) {
		return
	}
	pts := make([]port.LatLng, 0, len(req.Points))
	for i, p := range req.Points {
		if p.Lat == nil || p.Lng == nil || math.IsNaN(*p.Lat) || math.IsNaN(*p.Lng) {
			httpx.Fail(c, problem.Validation.WithField(fmt.Sprintf("points[%d]", i), "cần lat và lng (WGS84)"))
			return
		}
		pts = append(pts, port.LatLng{Lat: *p.Lat, Lng: *p.Lng})
	}
	ml := decimal.Zero
	if req.MinLoadTons != nil && *req.MinLoadTons != "" {
		d, err := decimal.NewFromString(*req.MinLoadTons)
		if err != nil || d.IsNegative() {
			httpx.Fail(c, problem.Validation.WithField("min_load_tons", "Tải trọng phải là số không âm."))
			return
		}
		ml = d
	}
	ver := 0
	if req.Version != nil {
		if *req.Version <= 0 {
			httpx.Fail(c, problem.Validation.WithField("version", "phải là số nguyên dương"))
			return
		}
		ver = *req.Version
	}
	query := fmt.Sprintf("points=%d&min_load_tons=%s&version=%d", len(pts), ml.String(), ver)
	if g := c.Query("grantor"); g != "" {
		query = "grantor=" + g + "&" + query
	}
	r, err := h.Svc.PartnerRoute(c.Request.Context(), grantor, pts, ml, ver, "POST /v1/partner/waterway/route", query)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toPartnerRoute(r))
}
