// Package handler: route bộ đọc nguồn module của Kỳ KHTC /v1/extension/khtc/periods/{id}/sources* (W11.64 — BRD KHTC
// v2.2 BR-DL-02…04): trạng thái nguồn (mốc dữ liệu, "Nguồn đã thay đổi", số đã chụp), "Cập nhật số liệu", lịch sử.
package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	rc "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
)

// Handler gắn route bộ đọc nguồn.
type Handler struct{ Svc *app.Service }

// RegisterApp gắn route vào nhóm /v1 (đã có xác thực, ngữ cảnh phiên, Idempotency-Key).
func (h Handler) RegisterApp(rg *gin.RouterGroup) {
	g := rg.Group("/extension/khtc/periods/:id/sources")
	g.GET("", h.status)
	g.POST("/refresh", h.refresh)
	g.GET("/history", h.history)
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

// ---------------------------------------------------------------------------
// DTO
// ---------------------------------------------------------------------------

type snapshotDTO struct {
	ID               uuid.UUID  `json:"id"`
	No               int64      `json:"no"`
	TakenAt          time.Time  `json:"taken_at"`
	ActorRef         uuid.UUID  `json:"actor_ref"`
	ActorName        string     `json:"actor_name,omitempty"`
	Closed           bool       `json:"closed"`
	TargetCalendar   string     `json:"target_calendar"`
	CrosswalkVersion string     `json:"crosswalk_version"`
	PlanVersionID    *uuid.UUID `json:"plan_version_id"`
	FiguresHash      string     `json:"figures_hash"`
	StatusHash       string     `json:"status_hash"`
	RowCount         int        `json:"row_count"`
	ValueCount       int        `json:"value_count"`
	ChangeCount      int        `json:"change_count"`
}

func snapshotOf(s store.Snapshot, name string) snapshotDTO {
	return snapshotDTO{ID: s.ID, No: s.No, TakenAt: s.EventTime, ActorRef: s.ActorRef, ActorName: name, Closed: s.Closed,
		TargetCalendar: rc.CalendarRef(s.TargetCalendarID, s.TargetCalendarVersion), CrosswalkVersion: s.CrosswalkVersion,
		PlanVersionID: s.PlanVersionID, FiguresHash: s.FiguresHash, StatusHash: s.StatusHash, RowCount: s.RowCount, ValueCount: s.ValueCount,
		ChangeCount: s.ChangeCount}
}

type asOfDTO struct {
	Department   string     `json:"department"`
	SourceModule string     `json:"source_module"`
	FormCode     *string    `json:"form_code"`
	NativeKeys   []string   `json:"native_keys"`
	ClosedAt     *time.Time `json:"closed_at"`
	SnapshotNo   *int64     `json:"snapshot_no"`
	Closed       bool       `json:"closed"`
	Missing      bool       `json:"missing"`
	Unavailable  bool       `json:"unavailable"`
	Fallback     bool       `json:"fallback"`
	Confirmation string     `json:"confirmation"`
}

type entryDTO struct {
	Ref  uuid.UUID `json:"ref"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

type priceDayDTO struct {
	Day string `json:"day"`
	Min string `json:"min"`
	Max string `json:"max"`
}

type valueDTO struct {
	SourceKey     string        `json:"source_key"`
	Entries       []entryDTO    `json:"entries"`
	Mode          string        `json:"mode"`
	Invalid       bool          `json:"invalid"`
	Cumulative    *string       `json:"cumulative"`
	Base          *string       `json:"base"`
	BaseRef       string        `json:"base_ref,omitempty"`
	Increment     *string       `json:"increment"`
	YearToDate    *string       `json:"year_to_date"`
	Sum           *string       `json:"sum"`
	PriceMin      *string       `json:"price_min"`
	PriceMax      *string       `json:"price_max"`
	PriceDay      *string       `json:"price_day"`
	PriceDays     []priceDayDTO `json:"price_days"`
	Unconfirmed   *string       `json:"unconfirmed"`
	Closed        bool          `json:"closed"`
	Provisional   bool          `json:"provisional"`
	NoSource      bool          `json:"no_source"`
	Frozen        bool          `json:"frozen"`
	SourceChanged bool          `json:"source_changed"`
	Fallback      bool          `json:"fallback"`
	Unavailable   bool          `json:"unavailable"`
	SourceKeys    []string      `json:"source_keys"`
}

func dstr(d *decimal.Decimal) *string {
	if d == nil {
		return nil
	}
	s := d.String()
	return &s
}

func entriesOf(refs []uuid.UUID, all map[uuid.UUID]port.KHTCPlanEntry) []entryDTO {
	out := make([]entryDTO, 0, len(refs))
	for _, r := range refs {
		e := all[r]
		out = append(out, entryDTO{Ref: r, Code: e.Code, Name: e.Name})
	}
	return out
}

func valueOf(v store.Value, all map[uuid.UUID]port.KHTCPlanEntry) valueDTO {
	x := v.V
	d := valueDTO{SourceKey: v.SourceKey, Entries: entriesOf(v.EntryRefs, all), Mode: x.Mode, Invalid: v.Invalid, Cumulative: dstr(x.Cumulative),
		Base: dstr(x.Base), BaseRef: x.BaseRef, Increment: dstr(x.Increment), YearToDate: dstr(x.YearToDate), Sum: dstr(x.Sum),
		PriceMin: dstr(x.Min), PriceMax: dstr(x.Max), Unconfirmed: dstr(x.Unconfirmed), Closed: x.Closed && !v.Invalid,
		Provisional: x.Provisional() || v.Invalid, NoSource: x.NoSource, Frozen: x.Frozen, SourceChanged: x.SourceChanged, Fallback: x.Fallback,
		Unavailable: x.Unavailable, SourceKeys: x.SourceKeys, PriceDays: []priceDayDTO{}}
	if x.PriceDay != "" {
		d.PriceDay = &x.PriceDay
	}
	for _, p := range x.PriceDays {
		d.PriceDays = append(d.PriceDays, priceDayDTO{Day: p.Day, Min: p.Min.String(), Max: p.Max.String()})
	}
	if d.SourceKeys == nil {
		d.SourceKeys = []string{}
	}
	return d
}

type changeDTO struct {
	Kind         string     `json:"kind"`
	TargetKey    string     `json:"target_key"`
	Department   string     `json:"department"`
	SourceModule string     `json:"source_module"`
	FormCode     string     `json:"form_code"`
	NativeKey    string     `json:"native_key"`
	ClosedAt     *time.Time `json:"closed_at"`
	WasClosedAt  *time.Time `json:"was_closed_at"`
}

type statusDTO struct {
	PeriodID      uuid.UUID    `json:"period_id"`
	OwnerRef      uuid.UUID    `json:"owner_ref"`
	OwnerName     string       `json:"owner_name,omitempty"`
	Terminal      *string      `json:"terminal"`
	CanRefresh    bool         `json:"can_refresh"`
	SourceChanged bool         `json:"source_changed"`
	Live          bool         `json:"live"`
	LiveError     string       `json:"live_error,omitempty"`
	Snapshot      *snapshotDTO `json:"snapshot"`
	AsOf          []asOfDTO    `json:"as_of"`
	Values        []valueDTO   `json:"values"`
	Changes       []changeDTO  `json:"changes"`
}

func statusOf(s app.Status) statusDTO {
	d := statusDTO{PeriodID: s.PeriodID, OwnerRef: s.OwnerRef, OwnerName: s.OwnerName, CanRefresh: s.CanRefresh, SourceChanged: s.SourceChanged(),
		Live: s.Live, LiveError: s.LiveError, AsOf: []asOfDTO{}, Values: []valueDTO{}, Changes: []changeDTO{}}
	if s.Terminal != "" {
		d.Terminal = &s.Terminal
	}
	if s.Snapshot != nil {
		x := snapshotOf(*s.Snapshot, s.ActorName)
		d.Snapshot = &x
	}
	for _, a := range s.AsOf {
		x := asOfDTO{Department: a.Department, SourceModule: a.SourceModule, NativeKeys: a.NativeKeys, ClosedAt: a.ClosedAt, SnapshotNo: a.SnapshotNo,
			Closed: a.Closed, Missing: a.Missing, Unavailable: a.Unavailable, Fallback: a.Fallback, Confirmation: a.Confirmation}
		if a.FormCode != "" {
			f := a.FormCode
			x.FormCode = &f
		}
		if x.NativeKeys == nil {
			x.NativeKeys = []string{}
		}
		d.AsOf = append(d.AsOf, x)
	}
	for _, v := range s.Values {
		d.Values = append(d.Values, valueOf(v, s.Entries))
	}
	for _, c := range s.Changes {
		x := changeDTO{Kind: c.Kind, TargetKey: c.Row.TargetKey, Department: c.Row.Department, SourceModule: c.Row.SourceModule,
			FormCode: c.Row.FormCode, NativeKey: c.Row.NativeKey, ClosedAt: c.Row.ClosedAt}
		if c.Was != nil {
			x.WasClosedAt = c.Was.ClosedAt
		}
		d.Changes = append(d.Changes, x)
	}
	return d
}

type historyChangeDTO struct {
	SourceKey string     `json:"source_key"`
	Entries   []entryDTO `json:"entries"`
	Field     string     `json:"field"`
	Old       *string    `json:"old"`
	New       *string    `json:"new"`
}

type historyDTO struct {
	Snapshot snapshotDTO        `json:"snapshot"`
	Changes  []historyChangeDTO `json:"changes"`
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

func (h Handler) status(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	s, err := h.Svc.Status(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, statusOf(s))
}

func (h Handler) refresh(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	s, err := h.Svc.Refresh(c.Request.Context(), id, idempotency.KeyFromContext(c.Request.Context()))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, statusOf(s))
}

func (h Handler) history(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	hs, entries, err := h.Svc.History(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]historyDTO, 0, len(hs))
	for _, x := range hs {
		it := historyDTO{Snapshot: snapshotOf(x.Snapshot, x.ActorName), Changes: []historyChangeDTO{}}
		for _, ch := range x.Changes {
			it.Changes = append(it.Changes, historyChangeDTO{SourceKey: ch.SourceKey, Entries: entriesOf(ch.EntryRefs, entries), Field: ch.Field,
				Old: dstr(ch.Old), New: dstr(ch.New)})
		}
		items = append(items, it)
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}
