package handler

// HTTP lịch báo giá, giờ khóa, nhắc, ngưỡng (W11.74). Hợp đồng: contracts/openapi/application.yaml, mục pf/price
// (W11.74). Số % là chuỗi decimal (schema Decimal) — không float (R-CODE-01).

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

func (h Handler) registerCalendar(rg *gin.RouterGroup) {
	rg.GET("/price/calendar", h.calendar)
	rg.POST("/price/calendar/schedule", h.saveSchedule)
	rg.POST("/price/calendar/exceptions", h.addException)
	rg.POST("/price/calendar/exceptions/:day/retire", h.retireException)
	rg.GET("/price/calendar/runs", h.calendarRuns)
	rg.POST("/price/calendar/reminder-test", h.reminderTest)
	rg.GET("/price/thresholds", h.thresholds)
	rg.POST("/price/thresholds", h.saveThreshold)
	rg.POST("/price/thresholds/:scope/:code/retire", h.retireThreshold)
}

var weekdayCodes = []string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func weekdayList(w domain.Weekdays) []string {
	out := []string{}
	for _, d := range w.List() {
		out = append(out, weekdayCodes[d])
	}
	return out
}

type scheduleDTO struct {
	Weekdays  []string `json:"weekdays"`
	LockAt    string   `json:"lock_at"`
	RespondBy string   `json:"respond_by"`
	Remind1   string   `json:"remind1_at"`
	Remind2   string   `json:"remind2_at"`
	Channels  []string `json:"channels"`
	Template  string   `json:"template"`
	ValidFrom *string  `json:"valid_from"`
	Version   int64    `json:"version"`
}

func toScheduleDTO(s domain.Schedule) scheduleDTO {
	d := scheduleDTO{Weekdays: weekdayList(s.Weekdays), LockAt: s.LockAt.String(), RespondBy: s.RespondBy.String(),
		Remind1: s.Remind1.String(), Remind2: s.Remind2.String(), Channels: []string{}, Template: s.Template, Version: s.Version}
	if s.InApp {
		d.Channels = append(d.Channels, "in_app")
	}
	if s.Email {
		d.Channels = append(d.Channels, "email")
	}
	if !s.ValidFrom.IsZero() {
		v := s.ValidFrom.Format(time.DateOnly)
		d.ValidFrom = &v
	}
	return d
}

type dayDTO struct {
	Day       string  `json:"day"`
	Weekday   string  `json:"weekday"`
	Kind      string  `json:"kind"`
	Quote     bool    `json:"quote"`
	Reason    string  `json:"reason"`
	LockAt    *string `json:"lock_at"`
	RespondBy *string `json:"respond_by"`
	Remind1   *string `json:"remind1_at"`
	Remind2   *string `json:"remind2_at"`
}

func calTS(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func toDayDTO(p domain.DayPlan) dayDTO {
	return dayDTO{Day: p.Day.Format(time.DateOnly), Weekday: weekdayCodes[p.Day.Weekday()], Kind: p.Kind, Quote: p.Quote,
		Reason: p.Reason, LockAt: calTS(p.LockAt), RespondBy: calTS(p.RespondBy), Remind1: calTS(p.Remind1), Remind2: calTS(p.Remind2)}
}

type exceptionDTO struct {
	Day     string `json:"day"`
	Kind    string `json:"kind"`
	Reason  string `json:"reason"`
	Version int64  `json:"version"`
}

type ruleHistoryDTO struct {
	scheduleDTO
	ActorRef   string `json:"actor_ref"`
	RecordedAt string `json:"recorded_at"`
}

type calendarDTO struct {
	Profile    string           `json:"profile"`
	Now        string           `json:"now"`
	CanManage  bool             `json:"can_manage"`
	Today      dayDTO           `json:"today"`
	Locked     bool             `json:"locked"`
	Current    scheduleDTO      `json:"current"`
	Upcoming   []scheduleDTO    `json:"upcoming"`
	Earliest   string           `json:"earliest_valid_from"`
	Days       []dayDTO         `json:"days"`
	Exceptions []exceptionDTO   `json:"exceptions"`
	History    []ruleHistoryDTO `json:"history"`
}

func (h Handler) calendar(c *gin.Context) {
	v, err := h.Svc.Calendar(c.Request.Context(), c.Query("profile"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := calendarDTO{Profile: v.Profile, Now: v.Now.UTC().Format(time.RFC3339), CanManage: v.CanManage, Today: toDayDTO(v.Today),
		Locked: v.Today.Locked(v.Now), Current: toScheduleDTO(v.Current), Upcoming: []scheduleDTO{},
		Earliest: v.Earliest.Format(time.DateOnly), Days: make([]dayDTO, 0, len(v.Days)), Exceptions: []exceptionDTO{},
		History: []ruleHistoryDTO{}}
	for _, u := range v.Upcoming {
		out.Upcoming = append(out.Upcoming, toScheduleDTO(u))
	}
	for _, d := range v.Days {
		out.Days = append(out.Days, toDayDTO(d))
	}
	for _, e := range v.Exceptions {
		out.Exceptions = append(out.Exceptions, exceptionDTO{Day: e.Day.Format(time.DateOnly), Kind: e.Kind, Reason: e.Reason, Version: e.Version})
	}
	for _, r := range v.History {
		out.History = append(out.History, ruleHistoryDTO{scheduleDTO: toScheduleDTO(r.Schedule), ActorRef: r.ActorRef.String(),
			RecordedAt: r.RecordedAt.UTC().Format(time.RFC3339)})
	}
	c.JSON(http.StatusOK, out)
}

type scheduleIn struct {
	Weekdays  []string `json:"weekdays"`
	LockAt    string   `json:"lock_at"`
	RespondBy string   `json:"respond_by"`
	Remind1   string   `json:"remind1_at"`
	Remind2   string   `json:"remind2_at"`
	Channels  []string `json:"channels"`
	Template  string   `json:"template"`
	ValidFrom string   `json:"valid_from"`
}

func calDay(s, field string) (time.Time, error) {
	d, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return time.Time{}, problem.Validation.WithDetail(field + " phải là ngày YYYY-MM-DD")
	}
	return d, nil
}

func (in scheduleIn) toDomain() (domain.Schedule, error) {
	var s domain.Schedule
	for _, w := range in.Weekdays {
		found := false
		for i, code := range weekdayCodes {
			if code == w {
				s.Weekdays |= domain.WeekdaysOf(time.Weekday(i))
				found = true
			}
		}
		if !found {
			return s, problem.Validation.WithDetail("ngày trong tuần " + w + " không hợp lệ (mon…sun)")
		}
	}
	for _, f := range []struct {
		dst  *domain.TimeOfDay
		src  string
		name string
	}{{&s.LockAt, in.LockAt, "lock_at"}, {&s.RespondBy, in.RespondBy, "respond_by"}, {&s.Remind1, in.Remind1, "remind1_at"}, {&s.Remind2, in.Remind2, "remind2_at"}} {
		t, err := domain.ParseTimeOfDay(f.src)
		if err != nil {
			return s, problem.Validation.WithDetail(f.name + " phải dạng HH:MM")
		}
		*f.dst = t
	}
	for _, ch := range in.Channels {
		switch ch {
		case "in_app":
			s.InApp = true
		case "email":
			s.Email = true
		default:
			return s, problem.Validation.WithDetail("kênh " + ch + " không hợp lệ (in_app, email)")
		}
	}
	s.Template = in.Template
	d, err := calDay(in.ValidFrom, "valid_from")
	s.ValidFrom = d
	return s, err
}

func (h Handler) saveSchedule(c *gin.Context) {
	var in scheduleIn
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân yêu cầu không hợp lệ"))
		return
	}
	s, err := in.toDomain()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	saved, err := h.Svc.SaveSchedule(c.Request.Context(), c.Query("profile"), idempotency.KeyFromContext(c.Request.Context()), s)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toScheduleDTO(saved))
}

type exceptionIn struct {
	Day    string `json:"day"`
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

func (h Handler) addException(c *gin.Context) {
	var in exceptionIn
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân yêu cầu không hợp lệ"))
		return
	}
	d, err := calDay(in.Day, "day")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	e, err := h.Svc.AddException(c.Request.Context(), c.Query("profile"), idempotency.KeyFromContext(c.Request.Context()),
		domain.Exception{Day: d, Kind: in.Kind, Reason: in.Reason})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, exceptionDTO{Day: e.Day.Format(time.DateOnly), Kind: e.Kind, Reason: e.Reason, Version: e.Version})
}

func (h Handler) retireException(c *gin.Context) {
	d, err := calDay(c.Param("day"), "day")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.Svc.RemoveException(c.Request.Context(), c.Query("profile"), idempotency.KeyFromContext(c.Request.Context()), d); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type runDTO struct {
	Day         string `json:"day"`
	Kind        string `json:"kind"`
	ScheduledAt string `json:"scheduled_at"`
	RanAt       string `json:"ran_at"`
	LagSeconds  int64  `json:"lag_seconds"`
	Recipients  int    `json:"recipients"`
	Targets     int    `json:"targets"`
	Note        string `json:"note"`
}

func toRunDTO(r store.Run) runDTO {
	return runDTO{Day: r.Day.Format(time.DateOnly), Kind: r.Kind, ScheduledAt: r.ScheduledAt.UTC().Format(time.RFC3339),
		RanAt: r.RanAt.UTC().Format(time.RFC3339), LagSeconds: int64(r.RanAt.Sub(r.ScheduledAt) / time.Second),
		Recipients: r.Recipients, Targets: r.Targets, Note: r.Note}
}

func (h Handler) calendarRuns(c *gin.Context) {
	now := h.Svc.Clock.Now()
	to := domain.BusinessDay(now)
	from := to.AddDate(0, 0, -30)
	var err error
	if s := c.Query("from"); s != "" {
		if from, err = calDay(s, "from"); err != nil {
			httpx.Fail(c, err)
			return
		}
	}
	if s := c.Query("to"); s != "" {
		if to, err = calDay(s, "to"); err != nil {
			httpx.Fail(c, err)
			return
		}
	}
	rs, err := h.Svc.CalendarRuns(c.Request.Context(), c.Query("profile"), from, to)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]runDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, toRunDTO(r))
	}
	c.JSON(http.StatusOK, gin.H{"from": from.Format(time.DateOnly), "to": to.Format(time.DateOnly), "items": out})
}

func (h Handler) reminderTest(c *gin.Context) {
	body, err := h.Svc.SendTestReminder(c.Request.Context(), c.Query("profile"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"body": body})
}

type thresholdDTO struct {
	Scope        string  `json:"scope"`
	Code         string  `json:"code"`
	Pct          string  `json:"pct"`
	LookbackDays *int    `json:"lookback_days,omitempty"`
	FlatPct      *string `json:"flat_pct,omitempty"`
	SurveyPct    *string `json:"survey_pct,omitempty"`
	Version      int64   `json:"version"`
	Status       string  `json:"status,omitempty"`
	ActorRef     string  `json:"actor_ref,omitempty"`
	RecordedAt   string  `json:"recorded_at,omitempty"`
}

func toThresholdDTO(t domain.Threshold) thresholdDTO {
	d := thresholdDTO{Scope: t.Scope, Code: t.Code, Pct: t.Pct.StringFixed(2), Version: t.Version}
	if t.Params != nil {
		lb, f, s := t.Params.LookbackDays, t.Params.FlatPct.StringFixed(2), t.Params.SurveyPct.StringFixed(2)
		d.LookbackDays, d.FlatPct, d.SurveyPct = &lb, &f, &s
	}
	return d
}

func rowDTO(r store.ThresholdRow) thresholdDTO {
	d := toThresholdDTO(r.Threshold)
	d.Status, d.ActorRef, d.RecordedAt = r.Status, r.ActorRef.String(), r.RecordedAt.UTC().Format(time.RFC3339)
	return d
}

type thresholdsDTO struct {
	Profile    string         `json:"profile"`
	CanManage  bool           `json:"can_manage"`
	Default    thresholdDTO   `json:"default"`
	DefaultSet bool           `json:"default_stored"`
	Items      []thresholdDTO `json:"items"`
	History    []thresholdDTO `json:"history"`
}

func (h Handler) thresholds(c *gin.Context) {
	v, err := h.Svc.ThresholdsView(c.Request.Context(), c.Query("profile"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := thresholdsDTO{Profile: v.Profile, CanManage: v.CanManage, Default: toThresholdDTO(v.Set.Default), DefaultSet: v.DefaultSet,
		Items: []thresholdDTO{}, History: []thresholdDTO{}}
	for _, r := range v.Rows {
		if r.Threshold.Scope != domain.ThresholdScopeDefault {
			out.Items = append(out.Items, rowDTO(r))
		}
	}
	for _, r := range v.History {
		out.History = append(out.History, rowDTO(r))
	}
	c.JSON(http.StatusOK, out)
}

type thresholdIn struct {
	Scope        string  `json:"scope"`
	Code         string  `json:"code"`
	Pct          string  `json:"pct"`
	LookbackDays *int    `json:"lookback_days"`
	FlatPct      *string `json:"flat_pct"`
	SurveyPct    *string `json:"survey_pct"`
}

var calendarPercent = regexp.MustCompile(`^[0-9]{1,3}(?:\.[0-9]{1,2})?$`)

func calDecimal(s, name string) (decimal.Decimal, error) {
	invalid := func() (decimal.Decimal, error) {
		return decimal.Zero, problem.Validation.WithField(name, "nhập số thập phân thường trong (0; 100], tối đa 2 số lẻ")
	}
	// AUTO-REVIEW §4: độ dài, cú pháp và biên được kiểm TRƯỚC NewFromString hay tính toán decimal.
	if len(s) > 32 {
		return invalid()
	}
	s = strings.TrimSpace(s)
	if !calendarPercent.MatchString(s) {
		return invalid()
	}
	integer, fraction, _ := strings.Cut(s, ".")
	whole, _ := strconv.Atoi(integer) // tối đa 3 chữ số đã kiểm cú pháp
	decimalPart := strings.Trim(fraction, "0")
	if whole > 100 || (whole == 100 && decimalPart != "") || (whole == 0 && decimalPart == "") {
		return invalid()
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return invalid()
	}
	return d, nil
}

func (in thresholdIn) toDomain() (domain.Threshold, error) {
	t := domain.Threshold{Scope: in.Scope, Code: in.Code}
	var err error
	if t.Pct, err = calDecimal(in.Pct, "pct"); err != nil {
		return t, err
	}
	if in.Scope != domain.ThresholdScopeDefault {
		if in.LookbackDays != nil || in.FlatPct != nil || in.SurveyPct != nil {
			return t, problem.Validation.WithDetail("chỉ ngưỡng mặc định có lookback_days, flat_pct, survey_pct")
		}
		return t, nil
	}
	if in.LookbackDays == nil || in.FlatPct == nil || in.SurveyPct == nil {
		return t, problem.Validation.WithDetail("ngưỡng mặc định cần lookback_days, flat_pct, survey_pct")
	}
	p := &domain.AlertParams{LookbackDays: *in.LookbackDays}
	if p.FlatPct, err = calDecimal(*in.FlatPct, "flat_pct"); err != nil {
		return t, err
	}
	if p.SurveyPct, err = calDecimal(*in.SurveyPct, "survey_pct"); err != nil {
		return t, err
	}
	t.Params = p
	return t, nil
}

func (h Handler) saveThreshold(c *gin.Context) {
	var in thresholdIn
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, problem.Validation.WithDetail("thân yêu cầu không hợp lệ (số % là chuỗi decimal)"))
		return
	}
	t, err := in.toDomain()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	saved, err := h.Svc.SaveThreshold(c.Request.Context(), c.Query("profile"), idempotency.KeyFromContext(c.Request.Context()), t)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, toThresholdDTO(saved))
}

func (h Handler) retireThreshold(c *gin.Context) {
	if err := h.Svc.RemoveThreshold(c.Request.Context(), c.Query("profile"), idempotency.KeyFromContext(c.Request.Context()),
		c.Param("scope"), c.Param("code")); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
