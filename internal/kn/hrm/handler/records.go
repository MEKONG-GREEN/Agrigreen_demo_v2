package handler

// W11.22: HTTP hồ sơ nhân sự (FN-07, FN-08, FN-20) — /v1/hrm/access, /v1/hrm/units, /v1/hrm/staff*, /v1/hrm/permissions*.
// Phản hồi chứa dữ liệu cá nhân (chi tiết, thẻ nhạy cảm, tệp xuất) đặt Cache-Control: no-store.

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

func (h Handler) registerRecords(rg *gin.RouterGroup) {
	rg.GET("/hrm/access", h.myAccess)
	rg.GET("/hrm/units", h.units)
	rg.GET("/hrm/staff", h.staffList)
	rg.POST("/hrm/staff", h.staffCreate)
	rg.GET("/hrm/staff/export", h.staffExport)
	rg.GET("/hrm/staff/:id", h.staffGet)
	rg.POST("/hrm/staff/:id/profile", h.staffProfile)
	rg.GET("/hrm/staff/:id/sensitive", h.staffSensitive)
	rg.POST("/hrm/staff/:id/sensitive", h.staffSensitiveWrite)
	rg.POST("/hrm/staff/:id/events", h.eventAdd)
	rg.POST("/hrm/staff/:id/events/:event/replace", h.eventReplace)
	rg.POST("/hrm/staff/:id/events/:event/void", h.eventVoid)
	rg.GET("/hrm/staff/:id/access-log", h.accessLog)
	rg.GET("/hrm/permissions", h.permissions)
	rg.GET("/hrm/permissions/history", h.permissionHistory)
	rg.POST("/hrm/permissions", h.permissionSet)
}

func idem(c *gin.Context) string { return idempotency.KeyFromContext(c.Request.Context()) }

func uuidParam(c *gin.Context, name string, nf *problem.Problem) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil || id == uuid.Nil {
		httpx.Fail(c, nf)
		return uuid.Nil, false
	}
	return id, true
}

func matrixOut(m domain.Matrix) map[string]string {
	out := map[string]string{}
	for _, p := range domain.Perms {
		if s, ok := m[p]; ok {
			out[string(p)] = string(s)
		}
	}
	return out
}

func (h Handler) myAccess(c *gin.Context) {
	a, err := h.Svc.MyAccess(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": a.Enabled, "group": a.Group, "unit": a.Unit, "perms": matrixOut(a.Perms)})
}

func (h Handler) units(c *gin.Context) {
	us, err := h.Svc.Units(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(us))
	for _, u := range us {
		out = append(out, gin.H{"code": u.Code, "name": u.Name, "kind": u.Kind, "active": u.Active})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func filterOf(c *gin.Context) (app.ListFilter, bool) {
	d, ok := at(c)
	if !ok {
		return app.ListFilter{}, false
	}
	f := app.ListFilter{At: d, Type: c.Query("type"), Unit: c.Query("unit"), Status: c.Query("status"), Position: c.Query("position"),
		Grade: c.Query("grade"), Q: c.Query("q")}
	f.Offset, _ = strconv.Atoi(c.Query("offset"))
	f.Limit, _ = strconv.Atoi(c.Query("limit"))
	return f, true
}

func snapOut(s domain.Snapshot, coef string, steps int) gin.H {
	date := func(t *time.Time) any {
		if t == nil {
			return nil
		}
		return t.Format(time.DateOnly)
	}
	sec := make([]gin.H, 0, len(s.Secondments))
	for _, e := range s.Secondments {
		sec = append(sec, gin.H{"id": e.ID, "to_org": e.Payload["to_org"], "valid_from": domain.FormatDate(e.ValidFrom, e.Precision),
			"valid_to": date(e.ValidTo)})
	}
	return gin.H{"status": s.Status, "started_on": date(s.StartedOn), "ended_on": date(s.EndedOn), "leave_reason": s.LeaveReason,
		"unit": s.Unit, "commune": s.Commune, "mgmt_unit": s.MgmtUnit, "position": s.Position, "title": s.Title, "allowance": s.Allowance,
		"appointed_on": s.AppointedOn, "grade": s.Grade, "step": s.Step, "steps": steps, "coefficient": coef, "step_since": s.StepSince,
		"reserved_coeff": s.Reserved, "disciplined": s.Disciplined, "top_degree": s.TopDegree, "secondments": sec}
}

func itemsOutStaff(xs []app.StaffItem) []gin.H {
	out := make([]gin.H, 0, len(xs))
	for _, it := range xs {
		out = append(out, gin.H{"id": it.ID, "code": it.Code, "type": it.Type, "label": it.Label, "masked_phone": it.MaskedPhone,
			"current": snapOut(it.Snap, it.Coefficient, it.Steps)})
	}
	return out
}

func (h Handler) staffList(c *gin.Context) {
	f, ok := filterOf(c)
	if !ok {
		return
	}
	r, err := h.Svc.ListStaff(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"day": r.Day.Format(time.DateOnly), "total": r.Total, "scope": r.Scope, "items": itemsOutStaff(r.Items)})
}

var statusLabel = map[string]string{domain.StatusActive: "Đang công tác", domain.StatusLeft: "Đã nghỉ việc", domain.StatusNone: "Chưa vào làm"}
var typeLabel = map[domain.StaffType]string{domain.StaffVienChuc: "Viên chức", domain.StaffHopDong: "Hợp đồng lao động",
	domain.StaffCanBoXa: "Cán bộ khuyến nông cấp xã"}

// cell chặn công thức bảng tính (CSV injection): ô bắt đầu bằng = + - @ được thêm dấu nháy.
func cell(s string) string {
	if s != "" && strings.ContainsRune("=+-@\t\r", rune(s[0])) {
		return "'" + s
	}
	return s
}

// staffExport: tệp CSV (UTF-8 có BOM, mở được bằng Excel) của danh sách đang lọc — không có trường TK (BR-03).
func (h Handler) staffExport(c *gin.Context) {
	f, ok := filterOf(c)
	if !ok {
		return
	}
	day, rows, err := h.Svc.ExportStaff(c.Request.Context(), f)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF}) // BOM: Excel nhận UTF-8
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"Mã hồ sơ", "Họ tên (che bớt)", "Loại nhân sự", "Trạng thái", "Đơn vị", "Xã", "Đơn vị quản lý", "Chức vụ",
		"Ngạch/CDNN", "Bậc", "Hệ số bậc"})
	for _, r := range rows {
		step := ""
		if r.Snap.Step > 0 {
			step = strconv.Itoa(r.Snap.Step)
			if r.Steps > 0 {
				step += "/" + strconv.Itoa(r.Steps)
			}
		}
		_ = w.Write([]string{cell(r.Code), cell(r.Label), typeLabel[r.Type], statusLabel[r.Snap.Status], cell(r.Snap.Unit), cell(r.Snap.Commune),
			cell(r.Snap.MgmtUnit), cell(r.Snap.Position), cell(r.Snap.Grade), step, r.Coefficient})
	}
	w.Flush()
	c.Header("Cache-Control", "no-store")
	c.Header("Content-Disposition", `attachment; filename="ho-so-nhan-su-`+day.Format(time.DateOnly)+`.csv"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}

type profileIn struct {
	FullName  *string `json:"full_name"`
	Phone     *string `json:"phone"`
	Address   *string `json:"address"`
	BirthDate *string `json:"birth_date"`
	Gender    *string `json:"gender"`
	Email     *string `json:"email"`
	Note      *string `json:"note"`
}

func (p profileIn) input() app.ProfileInput {
	return app.ProfileInput{FullName: p.FullName, Phone: p.Phone, Address: p.Address, BirthDate: p.BirthDate, Gender: p.Gender, Email: p.Email,
		Note: p.Note}
}

type eventIn struct {
	Kind      string            `json:"kind"`
	ValidFrom string            `json:"valid_from"`
	ValidTo   string            `json:"valid_to"`
	Payload   map[string]string `json:"payload"`
	Reason    string            `json:"reason"`
}

func (e eventIn) input() (domain.EventInput, error) {
	k, ok := domain.ParseEventKind(e.Kind)
	if !ok {
		return domain.EventInput{}, problem.Validation.WithField("kind", "loại quá trình không hợp lệ")
	}
	return domain.EventInput{Kind: k, ValidFrom: e.ValidFrom, ValidTo: e.ValidTo, Payload: e.Payload}, nil
}

func optUUID(s *string, field string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	u, err := uuid.Parse(*s)
	if err != nil || u == uuid.Nil {
		return nil, problem.Validation.WithField(field, "UUID")
	}
	return &u, nil
}

func (h Handler) staffCreate(c *gin.Context) {
	var req struct {
		Type       string            `json:"type"`
		Profile    profileIn         `json:"profile"`
		Sensitive  map[string]string `json:"sensitive"`
		AccountRef *string           `json:"account_ref"`
		Events     []eventIn         `json:"events"`
	}
	if !decode(c, &req) {
		return
	}
	acc, err := optUUID(req.AccountRef, "account_ref")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	in := app.CreateInput{Type: req.Type, Profile: req.Profile.input(), Sensitive: client.Fields(req.Sensitive), AccountRef: acc}
	for _, e := range req.Events {
		ev, err := e.input()
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		in.Events = append(in.Events, ev)
	}
	r, err := h.Svc.CreateStaff(c.Request.Context(), in, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, writeOut(r))
}

func writeOut(r app.WriteStaffResult) gin.H {
	w := r.Warnings
	if w == nil {
		w = []string{}
	}
	return gin.H{"id": r.ID, "code": r.Code, "version": r.Version, "warnings": w}
}

func eventsOut(evs []domain.Event) []gin.H {
	live := map[uuid.UUID]bool{}
	for _, e := range domain.Effective(evs) {
		live[e.ID] = true
	}
	out := make([]gin.H, 0, len(evs))
	for _, e := range evs {
		var to any
		if e.ValidTo != nil {
			to = e.ValidTo.Format(time.DateOnly)
		}
		payload := e.Payload
		if payload == nil {
			payload = map[string]string{}
		}
		out = append(out, gin.H{"id": e.ID, "kind": e.Kind, "valid_from": domain.FormatDate(e.ValidFrom, e.Precision), "precision": e.Precision,
			"valid_to": to, "payload": payload, "void": e.Void, "supersedes": e.Supersedes, "reason": e.Reason, "version": e.Seq,
			"recorded_at": e.RecordedAt.UTC(), "actor_ref": e.Actor, "effective": live[e.ID]})
	}
	return out
}

func (h Handler) staffGet(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	d, ok2 := at(c)
	if !ok2 {
		return
	}
	r, err := h.Svc.GetStaff(c.Request.Context(), id, d)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	hist := make([]gin.H, 0, len(r.History))
	for _, x := range r.History {
		hist = append(hist, gin.H{"version": x.Seq, "change": x.Change, "changed_fields": x.ChangedFields, "recorded_at": x.RecordedAt.UTC(),
			"actor_ref": x.Actor})
	}
	can := gin.H{}
	for p, v := range r.Can {
		can[string(p)] = v
	}
	var acc any
	if r.Head.AccountRef != nil {
		acc = r.Head.AccountRef.String()
	}
	p := r.Profile
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"id": r.Head.StaffID, "code": r.Head.Code, "type": r.Head.Type, "account_ref": acc, "day": r.Day.Format(time.DateOnly),
		"erased": r.Erased, "created_at": r.Created.RecordedAt.UTC(), "age": r.Age,
		"profile": gin.H{"full_name": p.FullName, "phone": p.Phone, "address": p.Address, "birth_date": p.BirthDate,
			"birth_precision": p.BirthPrecision, "gender": p.Gender, "email": p.Email, "note": p.Note},
		"current": snapOut(r.Snap, r.Coefficient, r.Steps), "events": eventsOut(r.Events), "history": hist, "can": can})
}

func (h Handler) staffProfile(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	var req struct {
		profileIn
		AccountRef json.RawMessage `json:"account_ref"`
	}
	if !decode(c, &req) {
		return
	}
	var acc *uuid.UUID
	clearAcc := false
	if len(req.AccountRef) > 0 {
		if string(req.AccountRef) == "null" {
			clearAcc = true
		} else {
			var s string
			if err := json.Unmarshal(req.AccountRef, &s); err != nil {
				httpx.Fail(c, problem.Validation.WithField("account_ref", "UUID hoặc null"))
				return
			}
			var err error
			if acc, err = optUUID(&s, "account_ref"); err != nil {
				httpx.Fail(c, err)
				return
			}
			clearAcc = acc == nil
		}
	}
	r, err := h.Svc.UpdateProfile(c.Request.Context(), id, req.input(), acc, clearAcc, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, writeOut(r))
}

func (h Handler) staffSensitive(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	f, err := h.Svc.Sensitive(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if f == nil {
		f = client.Fields{}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"id": id, "fields": map[string]string(f)})
}

func (h Handler) staffSensitiveWrite(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	var req struct {
		Fields map[string]string `json:"fields"`
	}
	if !decode(c, &req) {
		return
	}
	r, err := h.Svc.UpdateSensitive(c.Request.Context(), id, client.Fields(req.Fields), idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, writeOut(r))
}

func (h Handler) eventWrite(c *gin.Context, replace bool) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	var target uuid.UUID
	if replace {
		if target, ok = uuidParam(c, "event", port.ErrEventNotFound); !ok {
			return
		}
	}
	var req eventIn
	if !decode(c, &req) {
		return
	}
	in, err := req.input()
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	r, err := h.Svc.AddEvent(c.Request.Context(), id, in, target, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": r.ID, "version": r.Version})
}

func (h Handler) eventAdd(c *gin.Context)     { h.eventWrite(c, false) }
func (h Handler) eventReplace(c *gin.Context) { h.eventWrite(c, true) }

func (h Handler) eventVoid(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	ev, ok := uuidParam(c, "event", port.ErrEventNotFound)
	if !ok {
		return
	}
	var req struct {
		Reason string `json:"reason"`
	}
	if !decode(c, &req) {
		return
	}
	r, err := h.Svc.VoidEvent(c.Request.Context(), id, ev, req.Reason, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": r.ID, "version": r.Version})
}

func (h Handler) accessLog(c *gin.Context) {
	id, ok := uuidParam(c, "id", port.ErrStaffNotFound)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	rows, err := h.Svc.AccessLog(c.Request.Context(), id, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	out := make([]gin.H, 0, len(rows))
	for _, a := range rows {
		out = append(out, gin.H{"id": a.ID, "action": a.Action, "item_count": a.ItemCount, "scope": a.Scope, "query": a.Query,
			"actor_ref": a.Actor, "recorded_at": a.RecordedAt.UTC()})
	}
	c.JSON(http.StatusOK, gin.H{"items": out})
}

func permOut(xs []app.PermEntry) []gin.H {
	out := make([]gin.H, 0, len(xs))
	for _, p := range xs {
		var def any
		if p.Default != nil {
			def = matrixOut(p.Default)
		}
		var at any
		if !p.RecordedAt.IsZero() {
			at = p.RecordedAt.UTC()
		}
		var actor any
		if p.Actor != uuid.Nil {
			actor = p.Actor
		}
		out = append(out, gin.H{"grantee_kind": p.Kind, "grantee": p.Grantee, "perms": matrixOut(p.Perms), "default": def, "version": p.Version,
			"note": p.Note, "recorded_at": at, "actor_ref": actor, "locked": p.Kind == domain.GranteeRole && p.Grantee == domain.GroupAdmin})
	}
	return out
}

func (h Handler) permissions(c *gin.Context) {
	xs, err := h.Svc.Permissions(c.Request.Context(), false)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	perms := make([]string, 0, len(domain.Perms))
	for _, p := range domain.Perms {
		perms = append(perms, string(p))
	}
	c.JSON(http.StatusOK, gin.H{"perms": perms, "scopes": domain.Scopes, "groups": domain.Groups, "items": permOut(xs)})
}

func (h Handler) permissionHistory(c *gin.Context) {
	xs, err := h.Svc.Permissions(c.Request.Context(), true)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"items": permOut(xs)})
}

func (h Handler) permissionSet(c *gin.Context) {
	var req struct {
		GranteeKind string          `json:"grantee_kind"`
		Grantee     string          `json:"grantee"`
		Perms       json.RawMessage `json:"perms"`
		Note        string          `json:"note"`
	}
	if !decode(c, &req) {
		return
	}
	v, err := h.Svc.SetPermission(c.Request.Context(), req.GranteeKind, req.Grantee, req.Perms, req.Note, idem(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"version": v})
}
