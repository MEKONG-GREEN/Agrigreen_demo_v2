package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/sync/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func rfc(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

type integrationDTO struct {
	Consumer        string  `json:"consumer"`
	Source          string  `json:"source"`
	Status          string  `json:"status"`
	ProcessedCount  int64   `json:"processed_count"`
	LastProcessedAt *string `json:"last_processed_at"`
	LastEventType   *string `json:"last_event_type"`
	FailureCount    int64   `json:"failure_count"`
	DeadLettered    int64   `json:"dead_lettered_count"`
	LastFailureAt   *string `json:"last_failure_at"`
	LastErrorCode   *string `json:"last_error_code"`
	DLQDepth        *int64  `json:"dlq_depth"`
}

// integrations: GET /v1/sync/integrations?window_hours=&consumer=&status= (GIS US-SYNC-01).
func (h Handler) integrations(c *gin.Context) {
	window := 24 * time.Hour
	if s := c.Query("window_hours"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			httpx.Fail(c, problem.Validation.WithField("window_hours", "phải là số nguyên 1–720"))
			return
		}
		window = time.Duration(n) * time.Hour
	}
	res, err := h.Svc.Integrations(c.Request.Context(), window, c.Query("consumer"), c.Query("status"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]integrationDTO, 0, len(res.Items))
	for _, st := range res.Items {
		items = append(items, integrationDTO{
			Consumer: st.Consumer, Source: st.Source, Status: st.Status, ProcessedCount: st.Processed,
			LastProcessedAt: rfc(st.LastProcessed), LastEventType: st.LastEventType, FailureCount: st.Failures,
			DeadLettered: st.DeadLettered, LastFailureAt: rfc(st.LastFailure), LastErrorCode: st.LastErrorCode, DLQDepth: st.DLQDepth,
		})
	}
	c.JSON(http.StatusOK, gin.H{"since": res.Since.UTC().Format(time.RFC3339), "dlq_available": res.DLQAvailable, "items": items})
}

type integrationLogDTO struct {
	At            string  `json:"at"`
	Consumer      string  `json:"consumer"`
	Source        string  `json:"source"`
	EventType     string  `json:"event_type"`
	EventID       string  `json:"event_id"`
	Status        string  `json:"status"`
	RecordCount   int     `json:"record_count"`
	ErrorCode     *string `json:"error_code"`
	DeliveryCount int     `json:"delivery_count"`
	DeadLettered  bool    `json:"dead_lettered"`
}

func parseTimeParam(c *gin.Context, name string) (time.Time, bool) {
	s := c.Query(name)
	if s == "" {
		return time.Time{}, true
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải theo RFC 3339"))
		return time.Time{}, false
	}
	return t, true
}

// integrationLog: GET /v1/sync/integrations/log?consumer=&status=&from=&to=&cursor=&limit=.
func (h Handler) integrationLog(c *gin.Context) {
	q := app.LogQuery{Consumer: c.Query("consumer"), Status: c.Query("status"), Limit: limitParam(c)}
	var ok bool
	if q.From, ok = parseTimeParam(c, "from"); !ok {
		return
	}
	if q.To, ok = parseTimeParam(c, "to"); !ok {
		return
	}
	if cur := c.Query("cursor"); cur != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cur)
		ts, id, found := strings.Cut(string(raw), "|")
		at, terr := time.Parse(time.RFC3339Nano, ts)
		ev, uerr := uuid.Parse(id)
		if err != nil || !found || terr != nil || uerr != nil {
			httpx.Fail(c, problem.Validation.WithField("cursor", "không hợp lệ"))
			return
		}
		q.AfterAt, q.AfterEvent = &at, ev
	}
	pg, err := h.Svc.IntegrationLog(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]integrationLogDTO, 0, len(pg.Items))
	for _, r := range pg.Items {
		items = append(items, integrationLogDTO{
			At: r.At.UTC().Format(time.RFC3339Nano), Consumer: r.Consumer, Source: app.SourceLabel(r.Consumer), EventType: r.EventType,
			EventID: r.EventID.String(), Status: r.Status, RecordCount: 1, ErrorCode: r.ErrorCode, DeliveryCount: r.DeliveryCount,
			DeadLettered: r.DeadLettered,
		})
	}
	var next any
	if pg.HasNext {
		next = base64.RawURLEncoding.EncodeToString([]byte(pg.NextAt.UTC().Format(time.RFC3339Nano) + "|" + pg.NextID.String()))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
