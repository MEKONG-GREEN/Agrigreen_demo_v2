package handler

import (
	"encoding/hex"
	"net/http"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcform/domain"
	ps "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/store"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type historyPeriodDTO struct {
	ID         uuid.UUID `json:"id"`
	Name       string    `json:"name"`
	ReportType string    `json:"report_type"`
	Year       int       `json:"year"`
}
type historySubmissionDTO struct {
	ID          uuid.UUID  `json:"id"`
	Attempt     int64      `json:"attempt"`
	SubmittedAt time.Time  `json:"submitted_at"`
	Submitter   uuid.UUID  `json:"submitter_ref"`
	NativeLate  bool       `json:"native_late"`
	Decision    string     `json:"decision"`
	ReviewNote  string     `json:"review_note"`
	Reviewer    *uuid.UUID `json:"reviewer_ref"`
	ReviewedAt  *time.Time `json:"reviewed_at"`
}
type historyPlanDTO struct {
	VersionID  uuid.UUID `json:"version_id"`
	Version    int       `json:"version"`
	DecisionNo string    `json:"decision_no"`
}
type historyRowDTO struct {
	EntryRef uuid.UUID `json:"entry_ref"`
	Name     string    `json:"name"`
	Unit     string    `json:"unit"`
}

func historyPeriodOf(p ps.Period) historyPeriodDTO {
	return historyPeriodDTO{ID: p.ID, Name: p.Name, ReportType: p.Type, Year: p.Year}
}
func historySubmissionOf(s forms.SubmissionRef) historySubmissionDTO {
	return historySubmissionDTO{ID: s.ID, Attempt: s.Version, SubmittedAt: s.SubmittedAt, Submitter: s.Submitter, NativeLate: s.Late,
		Decision: s.Decision, ReviewNote: s.ReviewNote, Reviewer: s.Reviewer, ReviewedAt: s.ReviewedAt}
}
func historyPath(c *gin.Context) (uuid.UUID, string, bool) {
	raw, dept := c.Param("id"), c.Param("dept")
	// Bound syntax before parsing; unknown/duplicate query parameters are not
	// silently interpreted as a different resource or comparison operation.
	if len(raw) != 36 || len(c.Request.URL.RawQuery) != 0 || !historyDepartment(dept) {
		httpx.Fail(c, app.ErrFormNotFound)
		return uuid.Nil, "", false
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		httpx.Fail(c, app.ErrFormNotFound)
		return uuid.Nil, "", false
	}
	return id, dept, true
}
func historyDepartment(dept string) bool {
	switch dept {
	case "trong_trot", "chan_nuoi_thuy_san", "thong_tin_truyen_thong", "hanh_chinh_tong_hop", "ke_hoach_tai_chinh":
		return true
	}
	return false
}
func (h Handler) submissionHistory(c *gin.Context) {
	id, dept, ok := historyPath(c)
	if !ok {
		return
	}
	data, err := h.Svc.History(c.Request.Context(), id, dept)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]historySubmissionDTO, 0, len(data.Items))
	for _, sub := range data.Items {
		items = append(items, historySubmissionOf(sub))
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"period": historyPeriodOf(data.Period), "department": dept,
		"department_name": app.DeptName(dept), "items": items})
}
func (h Handler) historicalSubmission(c *gin.Context) {
	id, dept, ok := historyPath(c)
	if !ok {
		return
	}
	raw := c.Param("submissionID")
	if len(raw) != 36 {
		httpx.Fail(c, app.ErrFormNotFound)
		return
	}
	submissionID, err := uuid.Parse(raw)
	if err != nil || submissionID == uuid.Nil {
		httpx.Fail(c, app.ErrFormNotFound)
		return
	}
	data, err := h.Svc.HistoricalSubmission(c.Request.Context(), id, dept, submissionID)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	rows := make([]historyRowDTO, 0, len(data.Rows))
	for _, row := range data.Rows {
		rows = append(rows, historyRowDTO{EntryRef: row.EntryRef, Name: row.Name, Unit: row.Unit})
	}
	var plan *historyPlanDTO
	if data.Plan != nil {
		plan = &historyPlanDTO{VersionID: data.Plan.VersionID, Version: data.Plan.Version, DecisionNo: data.Plan.DecisionNo}
	}
	sections := data.Sections
	if sections == nil {
		sections = []domain.Section{}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"period": historyPeriodOf(data.Period), "department": dept,
		"department_name": app.DeptName(dept), "submission": historySubmissionOf(data.Submission), "sheet_id": data.Sheet.ID,
		"content_hash": hex.EncodeToString(data.Sheet.ContentHash), "plan_version_ref": data.Sheet.PlanVersionRef, "plan": plan,
		"template_code": data.Sheet.TemplateCode, "template_version": data.Sheet.TemplateVersion,
		"content": data.Sheet.Content, "rows": rows, "sections": sections})
}
