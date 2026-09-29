package handler

import (
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/mechanization/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
)

func (h Handler) registerAreaSyncApp(g *gin.RouterGroup) {
	g.GET("/area-sync-status", h.areaSyncStatus)
	g.GET("/area-sync-log", h.areaSyncLog)
}

type syncLogDTO struct {
	ID             string  `json:"id"`
	CooperativeRef string  `json:"cooperative_ref"`
	SeasonCode     string  `json:"season_code"`
	SeasonYear     int     `json:"season_year"`
	Source         string  `json:"source"`
	SourceLabel    string  `json:"source_label"`
	Outcome        string  `json:"outcome"`
	AreaHa         string  `json:"area_ha"`
	SourceAt       string  `json:"source_at"`
	EventID        *string `json:"event_id"`
	RecordedAt     string  `json:"recorded_at"`
}

func toSyncLogDTO(l store.AreaSyncLog) syncLogDTO {
	d := syncLogDTO{ID: l.ID.String(), CooperativeRef: l.CooperativeRef.String(), SeasonCode: l.Season.Code, SeasonYear: l.Season.Year,
		Source: l.Source, SourceLabel: sourceLabel(l.Source), Outcome: l.Outcome, AreaHa: l.AreaHa.String(),
		SourceAt: l.SourceAt.UTC().Format(time.RFC3339), RecordedAt: l.RecordedAt.UTC().Format(time.RFC3339Nano)}
	if l.EventID != nil {
		s := l.EventID.String()
		d.EventID = &s
	}
	return d
}

func sourceLabel(s string) string {
	if s == "app_htx" {
		return "App HTX"
	}
	return "Nhập tay"
}

// areaSyncStatus: GET /v1/mechanization/area-sync-status?season_code=&season_year= (CGH FN-07 B5, AC-03).
func (h Handler) areaSyncStatus(c *gin.Context) {
	var fe fieldErrs
	sn := fe.season(c, true)
	if fe.fail(c) {
		return
	}
	st, err := h.Svc.AreaSyncStatus(c.Request.Context(), sn)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	type coopDTO struct {
		syncLogDTO
		SyncCount int64 `json:"sync_count"`
		Stale     bool  `json:"stale"`
	}
	items := make([]coopDTO, 0, len(st.Items))
	for _, it := range st.Items {
		items = append(items, coopDTO{syncLogDTO: toSyncLogDTO(it.AreaSyncLog), SyncCount: it.Count, Stale: it.Stale})
	}
	var last *string
	if st.LastAppHTXAt != nil {
		s := st.LastAppHTXAt.UTC().Format(time.RFC3339)
		last = &s
	}
	var msg *string
	if st.Stale {
		m := "Dữ liệu App HTX cập nhật lần cuối lúc " + st.LastAppHTXAt.In(clock.Zone()).Format("15:04 02/01/2006") +
			", quá ngưỡng độ tươi " + st.ThresholdHours.String() + " giờ — đang dùng dữ liệu nhập tay dự phòng."
		msg = &m
	}
	c.JSON(http.StatusOK, gin.H{"last_app_htx_at": last, "threshold_hours": st.ThresholdHours.String(), "stale": st.Stale,
		"fallback_manual": st.Stale, "message": msg, "items": items})
}

// areaSyncLog: GET /v1/mechanization/area-sync-log?cooperative_ref=&season_code=&season_year=&source=&cursor=&limit=.
func (h Handler) areaSyncLog(c *gin.Context) {
	var fe fieldErrs
	q := app.SyncLogQuery{CooperativeRef: fe.ref("cooperative_ref", c.Query("cooperative_ref")), Season: fe.season(c, false),
		Source: c.Query("source")}
	if n, err := strconv.Atoi(c.DefaultQuery("limit", "50")); err == nil {
		q.Limit = n
	}
	if cur := c.Query("cursor"); cur != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cur)
		ts, id, found := strings.Cut(string(raw), "|")
		at, terr := time.Parse(time.RFC3339Nano, ts)
		ref, uerr := uuid.Parse(id)
		if err != nil || !found || terr != nil || uerr != nil {
			fe.add("cursor", "không hợp lệ")
		} else {
			q.AfterAt, q.AfterID = &at, ref
		}
	}
	if fe.fail(c) {
		return
	}
	rows, more, err := h.Svc.AreaSyncLog(c.Request.Context(), q)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]syncLogDTO, 0, len(rows))
	for _, r := range rows {
		items = append(items, toSyncLogDTO(r))
	}
	var next any
	if more && len(rows) > 0 {
		l := rows[len(rows)-1]
		next = base64.RawURLEncoding.EncodeToString([]byte(l.RecordedAt.UTC().Format(time.RFC3339Nano) + "|" + l.ID.String()))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next})
}
