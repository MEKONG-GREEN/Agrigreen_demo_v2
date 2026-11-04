package handler

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtc/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const initialDraftPattern225 = "/v1/kn/khtc/plans/:year/draft"

// FullPath is the registered semantic family, independent of year spelling.
func IsInitialDraftRoute225(pattern string) bool {
	return pattern == initialDraftPattern225 || strings.HasPrefix(pattern, initialDraftPattern225+"/")
}
func InitialDraftPolicy225(s *app.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsInitialDraftRoute225(c.FullPath()) {
			c.Next()
			return
		}
		if s == nil {
			httpx.Fail(c, app.ErrDependency)
			return
		}
		write := c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions
		stamp, err := s.AuthorizeInitialDraft225(c.Request.Context(), write)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		// Current trusted policy was checked above. This NEW family alone uses the
		// same complete canonical capability as its durable app receipt, without
		// changing the live principal/session or any other route's grant stamp.
		c.Request = c.Request.WithContext(idempotency.WithAuthorizationScope(c.Request.Context(), stamp))
		c.Next()
	}
}
func (h Handler) registerInitialDraft225(rg *gin.RouterGroup) {
	g := rg.Group("/kn/khtc/plans/:year/draft")
	g.GET("", h.getInitialDraft225)
	g.POST("", h.saveInitialDraft225)
	g.POST("/discard", h.discardInitialDraft225)
	g.GET("/history", h.initialDraftHistory225)
	g.GET("/compare", h.compareInitialDraft225)
}
func initialYear225(c *gin.Context) (int, bool) {
	raw := c.Param("year")
	if len(raw) != 4 || raw < "2000" || raw > "2100" {
		httpx.Fail(c, problem.Validation.WithField("year", "năm 2000–2100"))
		return 0, false
	}
	for _, x := range raw {
		if x < '0' || x > '9' {
			httpx.Fail(c, problem.Validation.WithField("year", "năm 2000–2100"))
			return 0, false
		}
	}
	y, _ := strconv.Atoi(raw)
	return y, true
}

// Stream entries one at a time: a tiny [{}...] body must not allocate an
// unbounded []entryReq before the 5000-row cap. Each decimal occurrence still
// executes planDecimal.UnmarshalJSON, including duplicates/case-folded keys.
type initialEntries225 []entryReq

func (e *initialEntries225) UnmarshalJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	token, err := d.Token()
	if err != nil || token != json.Delim('[') {
		return errors.New("entries array required")
	}
	out := make([]entryReq, 0)
	for d.More() {
		if len(out) >= domain.MaxEntries {
			return errors.New("too many entries")
		}
		var x entryReq
		if err = d.Decode(&x); err != nil {
			return err
		}
		out = append(out, x)
	}
	if _, err = d.Token(); err != nil {
		return err
	}
	*e = out
	return nil
}

type initialSaveReq225 struct {
	DraftID  *string            `json:"draft_id"`
	Expected *int64             `json:"expected_revision"`
	Entries  *initialEntries225 `json:"entries"`
	Note     string             `json:"note"`
}
type initialDiscardReq225 struct {
	DraftID  string `json:"draft_id"`
	Expected *int64 `json:"expected_revision"`
	Note     string `json:"note"`
}

func bindInitial225(c *gin.Context, v any) bool {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request nháp không hợp lệ"))
		return false
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		httpx.Fail(c, problem.BadRequest.WithDetail("Chỉ nhận một đối tượng JSON"))
		return false
	}
	return true
}
func initialEventDTO225(e store.DraftEvent225) gin.H {
	state := "active"
	if e.Action == "discard" {
		state = "discarded"
	}
	return gin.H{"event_id": e.ID.String(), "draft_id": e.DraftID.String(), "year": e.Year, "revision": e.Seq, "action": e.Action, "state": state, "base_published_version": 0, "entries_count": e.EntriesCount, "entries_hash": hex.EncodeToString(e.EntriesHash), "note": e.Note, "event_time": ts(e.EventTime), "recorded_at": ts(e.RecordedAt), "actor_ref": e.ActorRef.String()}
}
func initialDraftDTO225(d app.InitialDraft225) gin.H {
	out := initialEventDTO225(d.Event)
	out["entries"] = entryDTOs(d.Entries)
	return out
}
func initialStateDTO225(s app.InitialDraftState225) gin.H {
	var draft any
	if s.Draft != nil {
		draft = initialDraftDTO225(*s.Draft)
	}
	return gin.H{"year": s.Year, "revision": s.Revision, "draft": draft, "available": s.Available, "unavailable_reason": optStr(s.UnavailableReason)}
}
func (h Handler) getInitialDraft225(c *gin.Context) {
	y, ok := initialYear225(c)
	if !ok {
		return
	}
	s, err := h.Svc.GetInitialDraft225(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, initialStateDTO225(s))
}
func (h Handler) saveInitialDraft225(c *gin.Context) {
	y, ok := initialYear225(c)
	if !ok {
		return
	}
	var req initialSaveReq225
	if !bindInitial225(c, &req) {
		return
	}
	if req.Expected == nil || req.Entries == nil {
		httpx.Fail(c, problem.Validation.WithField("entries", "bắt buộc cây và expected_revision"))
		return
	}
	var generation uuid.UUID
	if req.DraftID != nil {
		var err error
		generation, err = uuid.Parse(*req.DraftID)
		if err != nil || generation == uuid.Nil {
			httpx.Fail(c, problem.Validation.WithField("draft_id", "phải là generation hợp lệ"))
			return
		}
	}
	es, err := (saveReq{Entries: []entryReq(*req.Entries)}).entries()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	result, replay, err := h.Svc.SaveInitialDraft225(c.Request.Context(), app.InitialDraftInput225{Year: y, DraftID: generation, ExpectedRevision: *req.Expected, Entries: es, Note: req.Note, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replay, initialDraftDTO225(result))
}
func (h Handler) discardInitialDraft225(c *gin.Context) {
	y, ok := initialYear225(c)
	if !ok {
		return
	}
	var req initialDiscardReq225
	if !bindInitial225(c, &req) {
		return
	}
	id, err := uuid.Parse(req.DraftID)
	if err != nil || id == uuid.Nil || req.Expected == nil {
		httpx.Fail(c, problem.Validation.WithField("draft_id", "bắt buộc generation và expected_revision"))
		return
	}
	result, replay, err := h.Svc.DiscardInitialDraft225(c.Request.Context(), app.InitialDraftInput225{Year: y, DraftID: id, ExpectedRevision: *req.Expected, Note: req.Note, IdemKey: idem(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	created(c, replay, initialDraftDTO225(result))
}
func (h Handler) initialDraftHistory225(c *gin.Context) {
	y, ok := initialYear225(c)
	if !ok {
		return
	}
	limit := 50
	var after int64
	if raw := c.Query("limit"); raw != "" {
		if len(raw) > 3 {
			httpx.Fail(c, problem.Validation)
			return
		}
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			httpx.Fail(c, problem.Validation)
			return
		}
		limit = n
	}
	if raw := c.Query("before_revision"); raw != "" {
		if len(raw) > 19 {
			httpx.Fail(c, problem.Validation)
			return
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			httpx.Fail(c, problem.Validation)
			return
		}
		after = n
	}
	rows, err := h.Svc.InitialDraftHistory225(c.Request.Context(), y, after, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	more := len(rows) > limit
	if more {
		rows = rows[:limit]
	}
	items := make([]gin.H, 0, len(rows))
	for _, e := range rows {
		items = append(items, initialEventDTO225(e))
	}
	var next any
	if more {
		next = rows[len(rows)-1].Seq
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_before_revision": next})
}
func (h Handler) compareInitialDraft225(c *gin.Context) {
	y, ok := initialYear225(c)
	if !ok {
		return
	}
	s, err := h.Svc.GetInitialDraft225(c.Request.Context(), y)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := initialStateDTO225(s)
	out["baseline_entries"] = []any{}
	out["baseline_published_version"] = 0
	c.JSON(http.StatusOK, out)
}
