package handler

// HTTP duyệt / mở duyệt bảng giá ngày (W11.76, pf 000151; BRD Báo giá v1.3 FN-TI-02.05, 02.06) — Application API
// /v1/price/boards*. Hợp đồng ở contracts/openapi/application.yaml, mục pf/price W11.76. Giá là chuỗi decimal.

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// BoardHandler gói TicketService (bảng giá ngày) cho HTTP.
type BoardHandler struct{ Svc *app.TicketService }

// RegisterApp gắn route Application API của bảng giá ngày.
func (h BoardHandler) RegisterApp(rg *gin.RouterGroup) {
	rg.GET("/price/boards", h.list)
	rg.GET("/price/boards/:day", h.get)
	rg.POST("/price/boards/:day/lock", h.op(h.Svc.LockBoard))
	rg.POST("/price/boards/:day/unlock", h.op(h.Svc.UnlockBoard))
	rg.POST("/price/boards/:day/approve", h.op(h.Svc.ApproveBoard))
	rg.POST("/price/boards/:day/reopen", h.op(h.Svc.ReopenBoard))
	rg.POST("/price/boards/:day/cancel-reopen", h.op(h.Svc.CancelReopen))
}

type boardLineDTO struct {
	ItemCode  string `json:"item_code"`
	ItemName  string `json:"item_name"`
	GroupCode string `json:"group_code"`
	GroupName string `json:"group_name"`
	UnitCode  string `json:"unit_code"`
	UnitName  string `json:"unit_name"`
	Min       string `json:"min"`
	Max       string `json:"max"`
	Avg       string `json:"avg"`
	Points    int    `json:"points"`
}

type boardLogDTO struct {
	Seq       int64   `json:"seq"`
	Action    string  `json:"action"`
	Status    string  `json:"status"`
	Version   int     `json:"version"`
	Reason    *string `json:"reason"`
	ActorRef  string  `json:"actor_ref"`
	ActorName *string `json:"actor_name"`
	At        string  `json:"at"`
}

type boardPublishedDTO struct {
	Version    int            `json:"version"`
	ApprovedAt string         `json:"approved_at"`
	Reopened   bool           `json:"reopened"`
	WeekKey    string         `json:"week_key"`
	Lines      []boardLineDTO `json:"lines"`
}

type boardSummaryDTO struct {
	Points     int `json:"points"`
	Submitted  int `json:"submitted"`
	Late       int `json:"late"`
	Missed     int `json:"missed"`
	PriceLines int `json:"price_lines"`
	Excluded   int `json:"excluded"`
	OpenAlerts int `json:"open_alerts"`
}

type boardDTO struct {
	Day          string             `json:"day"`
	Quote        bool               `json:"quote"`
	LockAt       *string            `json:"lock_at"`
	Locked       bool               `json:"locked"`
	Now          string             `json:"now"`
	State        string             `json:"state"`
	Seq          int64              `json:"seq"`
	Version      int                `json:"version"`
	LockedBy     *string            `json:"locked_by"`
	LockedByName *string            `json:"locked_by_name"`
	LockUntil    *string            `json:"lock_until"`
	Summary      *boardSummaryDTO   `json:"summary"`
	Preview      []boardLineDTO     `json:"preview"`
	Published    *boardPublishedDTO `json:"published"`
	Log          []boardLogDTO      `json:"log"`
	Can          map[string]bool    `json:"can"`
	Blockers     []string           `json:"blockers"`
	ReopenDays   int                `json:"reopen_days"`
}

func lineDTOs(rows []app.BoardLineView) []boardLineDTO {
	out := make([]boardLineDTO, 0, len(rows))
	for _, r := range rows {
		out = append(out, boardLineDTO{ItemCode: r.Item, ItemName: r.ItemName, GroupCode: r.GroupCode, GroupName: r.GroupName,
			UnitCode: r.UnitCode, UnitName: r.UnitName, Min: r.Min.String(), Max: r.Max.String(), Avg: r.Avg.String(), Points: r.Points})
	}
	return out
}

func boardDTOOf(v app.BoardView) boardDTO {
	lock, _ := planTimes(v.Plan)
	out := boardDTO{Day: v.Day.Format(time.DateOnly), Quote: v.Plan.Quote, LockAt: lock, Locked: v.Locked, Now: ts(v.Now), State: v.State,
		Seq: v.Board.Seq, Version: v.Board.Version, LockedBy: uuidPtr(v.LockedBy), LockedByName: strPtr(v.LockedByName),
		Preview: []boardLineDTO{}, Log: []boardLogDTO{}, Blockers: v.Blockers, ReopenDays: v.ReopenDays,
		Can: map[string]bool{"approve": v.Can.Approve, "unlock": v.Can.Unlock, "reopen": v.Can.Reopen, "cancel_reopen": v.Can.CancelReopen}}
	if out.Blockers == nil {
		out.Blockers = []string{}
	}
	if v.LockedBy != uuid.Nil {
		out.LockUntil = tsPtr(v.Board.LockUntil)
	}
	if v.Full {
		out.Summary = &boardSummaryDTO{Points: v.Summary.Points, Submitted: v.Summary.Submitted, Late: v.Summary.Late, Missed: v.Summary.Missed,
			PriceLines: v.PriceLines, Excluded: v.Summary.Excluded, OpenAlerts: v.Summary.OpenAlerts}
		out.Preview = lineDTOs(v.Preview)
		for _, h := range v.Log {
			out.Log = append(out.Log, boardLogDTO{Seq: h.Seq, Action: h.Action, Status: h.Status, Version: h.Version, Reason: strPtr(h.Reason),
				ActorRef: h.Actor.String(), ActorName: strPtr(h.ActorName), At: ts(h.EventTime)})
		}
	}
	if v.Published != nil {
		out.Published = &boardPublishedDTO{Version: v.Published.Version, ApprovedAt: ts(v.Published.ApprovedAt), Reopened: v.Published.Reopened,
			WeekKey: v.Published.WeekKey, Lines: lineDTOs(v.PublishedRow)}
	}
	return out
}

func pathDay(c *gin.Context) (time.Time, bool) {
	d, err := day(c.Param("day"), "day")
	if err == nil && d.IsZero() {
		err = problem.Validation.WithField("day", "bắt buộc")
	}
	if err != nil {
		httpx.Fail(c, err)
		return d, false
	}
	return d, true
}

func (h BoardHandler) get(c *gin.Context) {
	d, ok := pathDay(c)
	if !ok {
		return
	}
	v, err := h.Svc.GetBoard(c.Request.Context(), c.Query("profile"), d)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, boardDTOOf(v))
}

func (h BoardHandler) list(c *gin.Context) {
	from, ok := queryDate(c, "from")
	if !ok {
		return
	}
	to, ok := queryDate(c, "to")
	if !ok {
		return
	}
	days, err := h.Svc.ListBoards(c.Request.Context(), c.Query("profile"), from, to)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(days))
	for _, d := range days {
		out = append(out, gin.H{"day": d.Day.Format(time.DateOnly), "quote": d.Quote, "state": d.State, "version": d.Version,
			"approved_at": tsPtr(d.ApprovedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"boards": out})
}

type boardOpReq struct {
	Seq    int64  `json:"seq"`
	Reason string `json:"reason"`
}

type boardOpFn func(ctx context.Context, profile string, in app.BoardInput, idem string) (app.BoardView, error)

func (h BoardHandler) op(fn boardOpFn) gin.HandlerFunc {
	return func(c *gin.Context) {
		d, ok := pathDay(c)
		if !ok {
			return
		}
		var req boardOpReq
		if !bind(c, &req) {
			return
		}
		v, err := fn(c.Request.Context(), c.Query("profile"), app.BoardInput{Day: d, Version: req.Seq, Reason: req.Reason}, idem(c))
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.JSON(http.StatusCreated, boardDTOOf(v))
	}
}
