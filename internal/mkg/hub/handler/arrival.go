package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

type arrivalDTO struct {
	TripID             string     `json:"trip_id"`
	TripCode           string     `json:"trip_code"`
	FacilityRef        string     `json:"facility_ref"`
	Status             string     `json:"status"`
	ProtoStatus        string     `json:"proto_status"` // cho_den | da_den | huy (nhãn prototype FM-05)
	Mode               string     `json:"mode"`
	VesselCode         *string    `json:"vessel_code"`
	CollectionOrderRef *uuid.UUID `json:"collection_order_ref"`
	LotRefs            []string   `json:"lot_refs"`
	PlannedKg          *string    `json:"planned_kg"`
	ExpectedSince      *time.Time `json:"expected_since"`
	ArrivedAt          *time.Time `json:"arrived_at"`
	CancelledAt        *time.Time `json:"cancelled_at"`
	LastEventID        string     `json:"last_event_id"`
}

var protoArrival = map[string]string{"expected": "cho_den", "arrived": "da_den", "cancelled": "huy"}

func toArrival(a store.Arrival) arrivalDTO {
	lots := make([]string, 0, len(a.LotRefs))
	for _, l := range a.LotRefs {
		lots = append(lots, l.String())
	}
	var kg *string
	if a.PlannedKg != nil {
		s := a.PlannedKg.String()
		kg = &s
	}
	return arrivalDTO{TripID: a.TripID.String(), TripCode: a.TripCode, FacilityRef: a.FacilityRef.String(), Status: a.Status,
		ProtoStatus: protoArrival[a.Status], Mode: a.Mode, VesselCode: a.VesselCode, CollectionOrderRef: a.CollectionOrderRef, LotRefs: lots,
		PlannedKg: kg, ExpectedSince: a.ExpectedSince, ArrivedAt: a.ArrivedAt, CancelledAt: a.CancelledAt, LastEventID: a.LastEventID.String()}
}

func optUUID(c *gin.Context, name string) (*uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		return nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải là UUID"))
		return nil, false
	}
	return &id, true
}

// arrivals: GET /v1/hub/arrivals?facility_id=&status=expected|arrived|cancelled&cursor=&limit= — thông báo hàng
// đến mang mã chuyến (proto FM-05), mới nhất trước.
func (h Handler) arrivals(c *gin.Context) {
	fac, ok := optUUID(c, "facility_id")
	if !ok {
		return
	}
	before, ok := optUUID(c, "cursor")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	items, next, err := h.Svc.Arrivals(c.Request.Context(), fac, c.Query("status"), before, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]arrivalDTO, 0, len(items))
	for _, a := range items {
		out = append(out, toArrival(a))
	}
	var nc *string
	if next != nil {
		s := next.String()
		nc = &s
	}
	c.JSON(http.StatusOK, gin.H{"items": out, "next_cursor": nc})
}
