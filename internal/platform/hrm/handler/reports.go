package handler

// W11.28: HTTP báo cáo Sở NS01–NS06 (FN-12) và dashboard nhân sự (FN-06) — /v1/hrm/reports*, /v1/hrm/report-exports/*,
// /v1/hrm/dashboard*. Phản hồi có họ tên / trường nhạy cảm đặt Cache-Control: no-store. Xuất (POST) trả bản tóm tắt
// KHÔNG PII (phản hồi được lưu cho Idempotency-Key); tệp tải bằng GET …/file (dựng lại, so SHA-256).

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

func (h Handler) registerReports(rg *gin.RouterGroup) {
	rg.GET("/hrm/reports", h.reportList)
	rg.GET("/hrm/reports/:code", h.reportPreview)
	rg.POST("/hrm/reports/:code/export", h.reportExport)
	rg.GET("/hrm/reports/:code/history", h.reportHistory)
	rg.GET("/hrm/reports/:code/cells", h.reportCell)
	rg.GET("/hrm/report-exports/:id/file", h.reportFile)
	rg.GET("/hrm/dashboard", h.dashboard)
	rg.GET("/hrm/dashboard/members", h.dashboardMembers)
}

func metaOut(m domain.ReportMeta) gin.H {
	sens := m.Sensitive
	if sens == nil {
		sens = []string{}
	}
	return gin.H{"code": m.Code, "name": m.Name, "period": m.Period, "aggregate": m.Aggregate, "sensitive_fields": sens, "source": m.Source}
}

func checkSumOut(cs []store.CheckSummary) []gin.H {
	out := make([]gin.H, 0, len(cs))
	for _, c := range cs {
		out = append(out, gin.H{"rule": c.Rule, "level": c.Level, "ok": c.OK, "count": c.Count})
	}
	return out
}

func logOut(l store.ReportLog) gin.H {
	o := gin.H{"id": l.ID, "action": l.Action, "report": l.Report, "as_of": l.AsOf.Format(time.DateOnly), "period": l.Period,
		"item_count": l.ItemCount, "sensitive_fields": l.Sensitive, "file_name": l.FileName, "sha256": l.SHA256, "byte_size": l.ByteSize,
		"check_status": l.CheckStatus, "checks": checkSumOut(l.Checks), "actor": l.Actor, "recorded_at": l.RecordedAt.UTC().Format(time.RFC3339),
		"export_id": l.ExportID, "drift": l.Drift, "params": l.Params}
	if o["sensitive_fields"] == nil {
		o["sensitive_fields"] = []string{}
	}
	return o
}

func (h Handler) reportList(c *gin.Context) {
	xs, err := h.Svc.Reports(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		o := metaOut(x.Meta)
		o["exports"] = x.Exports
		o["last_export"] = nil
		if x.Last != nil {
			o["last_export"] = logOut(*x.Last)
		}
		out = append(out, o)
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func briefOut(b app.PersonBrief) gin.H {
	return gin.H{"id": b.ID, "code": b.Code, "type": b.Type, "name": b.Name, "unit": b.Unit, "unit_name": b.UnitName, "position": b.Position}
}

func briefsOut(xs []app.PersonBrief) []gin.H {
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, briefOut(x))
	}
	return out
}

func viewOut(v app.ReportView) gin.H {
	cols := make([]gin.H, 0, len(v.Table.Columns))
	for _, c := range v.Table.Columns {
		cols = append(cols, gin.H{"key": c.Key, "group": c.Group, "label": c.Label, "sensitive": c.Sensitive, "numeric": c.Numeric})
	}
	rows := make([]gin.H, 0, len(v.Table.Rows))
	for _, r := range v.Table.Rows {
		o := gin.H{"kind": r.Kind, "key": r.Key, "cells": r.Cells}
		if r.StaffID != uuid.Nil {
			o["staff_id"] = r.StaffID
		}
		rows = append(rows, o)
	}
	checks := make([]gin.H, 0, len(v.Checks))
	for _, c := range v.Checks {
		checks = append(checks, gin.H{"rule": c.Rule, "level": c.Level, "ok": c.OK, "message": c.Message, "people": briefsOut(c.People)})
	}
	return gin.H{"report": metaOut(v.Meta), "title": v.Table.Title, "period": v.Table.Period, "as_of": v.Table.AsOf.Format(time.DateOnly),
		"status": v.Status, "columns": cols, "rows": rows, "checks": checks, "people": len(v.Table.People)}
}

func (h Handler) reportPreview(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	v, err := h.Svc.PreviewReport(c.Request.Context(), c.Param("code"), d)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, viewOut(v))
}

type exportIn struct {
	AsOf       string     `json:"as_of"`
	PreparerID *uuid.UUID `json:"preparer_id"`
	SignerID   *uuid.UUID `json:"signer_id"`
}

func (h Handler) reportExport(c *gin.Context) {
	var in exportIn
	if !decode(c, &in) {
		return
	}
	var d time.Time
	if in.AsOf != "" {
		var err error
		if d, err = time.Parse(time.DateOnly, in.AsOf); err != nil {
			httpx.Fail(c, problem.Validation.WithField("as_of", "YYYY-MM-DD"))
			return
		}
	}
	r, err := h.Svc.ExportReport(c.Request.Context(), c.Param("code"), app.ExportInput{AsOf: d, Preparer: in.PreparerID, Signer: in.SignerID}, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	checks := make([]gin.H, 0, len(r.View.Checks))
	for _, x := range r.View.Checks {
		checks = append(checks, gin.H{"rule": x.Rule, "level": x.Level, "ok": x.OK, "message": x.Message, "count": len(x.StaffIDs)})
	}
	// Không PII: phản hồi này được lưu 48 giờ cho Idempotency-Key.
	c.JSON(http.StatusCreated, gin.H{"id": r.ID, "file_name": r.FileName, "sha256": r.SHA256, "byte_size": r.Size, "status": r.View.Status,
		"checks": checks, "download": "/v1/hrm/report-exports/" + r.ID.String() + "/file"})
}

func (h Handler) reportHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	xs, err := h.Svc.ReportHistory(c.Request.Context(), c.Param("code"), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, logOut(x))
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func (h Handler) reportCell(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	xs, hidden, err := h.Svc.CellPeople(c.Request.Context(), c.Param("code"), d, c.Query("row"), c.Query("col"))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"items": briefsOut(xs), "hidden": hidden})
}

func (h Handler) reportFile(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrExportNotFound)
	if !ok {
		return
	}
	f, err := h.Svc.DownloadExport(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="`+f.FileName+`"`)
	c.Header("X-Export-Drift", strconv.FormatBool(f.Drift))
	c.Header("X-Export-Sha256", f.Log.SHA256)
	c.Data(http.StatusOK, tabular.ContentTypeXLSX, f.Data)
}

func countsOut(xs []domain.Count) []gin.H {
	out := make([]gin.H, 0, len(xs))
	for _, x := range xs {
		out = append(out, gin.H{"key": x.Key, "name": x.Name, "count": x.N})
	}
	return out
}

func (h Handler) dashboard(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	v, err := h.Svc.Dashboard(c.Request.Context(), d)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"as_of": v.AsOf.Format(time.DateOnly), "scope": v.Scope, "total": v.Total, "staff": v.Staff, "by_type": v.ByType,
		"by_unit": countsOut(v.ByUnit), "gender": v.Gender, "age": v.Age, "education": countsOut(v.Edu), "grade_group": v.Grade,
		"joined": v.Joined, "left": v.Left, "retire_soon": v.Soon, "over_retire": v.Over, "pii": v.PII, "elapsed_ms": v.Elapsed.Milliseconds()})
}

func (h Handler) dashboardMembers(c *gin.Context) {
	d, ok := at(c)
	if !ok {
		return
	}
	off, _ := strconv.Atoi(c.Query("offset"))
	lim, _ := strconv.Atoi(c.Query("limit"))
	xs, total, err := h.Svc.DashboardMembers(c.Request.Context(), d, c.Query("metric"), off, lim)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"total": total, "items": briefsOut(xs)})
}
