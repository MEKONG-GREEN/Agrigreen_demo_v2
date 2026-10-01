// Package handler: Admin API của pf/metering (admin.<domain>, MFA — contracts/openapi/admin.yaml, tag
// metering): sổ sử dụng, bảng kê (yêu cầu 202 + job, duyệt R-56, CSV), tham số hiệu lực, sổ xuất và
// cảnh báo bất thường. Public API (/v1/usage) do cmd/api-public phục vụ qua port.Usage.
package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metering/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Handler gói app.Service cho HTTP.
type Handler struct{ Svc *app.Service }

// RegisterApp: pf/metering không có route Application API.
func (Handler) RegisterApp(*gin.RouterGroup) {}

// RegisterAdmin gắn route Admin API.
func (h Handler) RegisterAdmin(rg *gin.RouterGroup) {
	rg.GET("/metering/usage", h.usage)
	rg.GET("/metering/usage-events", h.usageEvents)
	rg.POST("/metering/statements", h.requestStatement)
	rg.GET("/metering/statements", h.statements)
	rg.GET("/metering/statements/:id", h.statement)
	rg.POST("/metering/statements/:id/approve", h.approve)
	rg.GET("/metering/statements/:id/verify", h.verify)
	rg.GET("/metering/statements/:id/export.csv", h.exportCSV)
	rg.GET("/metering/statement-jobs/:id", h.job)
	rg.GET("/metering/params", h.params)
	rg.GET("/metering/egress", h.egress)
	rg.GET("/metering/egress/anomalies", h.anomalies)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func optTS(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := ts(*t)
	return &s
}

func optRef(u *uuid.UUID) *string {
	if u == nil || *u == uuid.Nil {
		return nil
	}
	s := u.String()
	return &s
}

func limitQ(c *gin.Context) int {
	n, _ := strconv.Atoi(c.Query("limit"))
	return n
}

func uuidQ(c *gin.Context, name string, required bool) (uuid.UUID, bool) {
	s := c.Query(name)
	if s == "" {
		if required {
			httpx.Fail(c, problem.Validation.WithField(name, "bắt buộc"))
			return uuid.Nil, false
		}
		return uuid.Nil, true
	}
	id, err := uuid.Parse(s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "không phải UUID"))
		return uuid.Nil, false
	}
	return id, true
}

func dateQ(c *gin.Context, name string) (*time.Time, bool) {
	s := c.Query(name)
	if s == "" {
		return nil, true
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		httpx.Fail(c, problem.Validation.WithField(name, "phải dạng YYYY-MM-DD (giờ Việt Nam)"))
		return nil, false
	}
	return &t, true
}

func pathID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		httpx.Fail(c, app.ErrNotFound)
		return uuid.Nil, false
	}
	return id, true
}

func next(n *uuid.UUID) *string { return optRef(n) }

// vnDate chuyển ngày YYYY-MM-DD (giờ VN) sang thời điểm giữa trưa giờ VN — an toàn khi quy về ngày nghiệp vụ.
func vnDate(t time.Time) time.Time { return t.Add(5 * time.Hour) }

type usageDayDTO struct {
	Date    string `json:"date"`
	Product string `json:"product"`
	Calls   int64  `json:"calls"`
	Units   int64  `json:"units"`
}

func (h Handler) usage(c *gin.Context) {
	client, ok := uuidQ(c, "client_id", true)
	if !ok {
		return
	}
	from, ok := dateQ(c, "from")
	if !ok {
		return
	}
	to, ok := dateQ(c, "to")
	if !ok {
		return
	}
	if from == nil || to == nil {
		httpx.Fail(c, problem.Validation.WithField("from", "from và to bắt buộc"))
		return
	}
	days, err := h.Svc.AdminUsage(c.Request.Context(), client, vnDate(*from), vnDate(*to))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]usageDayDTO, 0, len(days))
	for _, d := range days {
		items = append(items, usageDayDTO{Date: d.Date, Product: d.Product, Calls: d.Calls, Units: d.Units})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type usageEventDTO struct {
	ID              string  `json:"id"`
	Seq             int64   `json:"seq"`
	ClientID        string  `json:"client_id"`
	KeyPrefix       string  `json:"key_prefix"`
	AuthMethod      string  `json:"auth_method"`
	CallKind        string  `json:"call_kind"`
	Product         string  `json:"product"`
	ContractVersion int     `json:"contract_version"`
	RouteTemplate   string  `json:"route_template"`
	Status          int     `json:"status"`
	ProblemCode     *string `json:"problem_code"`
	DataTenantID    *string `json:"data_tenant_id"`
	Units           int64   `json:"units"`
	Rows            int64   `json:"rows_out"`
	Bytes           int64   `json:"bytes_out"`
	LatencyMS       int64   `json:"latency_ms"`
	Replayed        bool    `json:"replayed"`
	RuleVersion     string  `json:"rule_version"`
	EventTime       string  `json:"event_time"`
	RecordedAt      string  `json:"recorded_at"`
}

func (h Handler) usageEvents(c *gin.Context) {
	client, ok := uuidQ(c, "client_id", true)
	if !ok {
		return
	}
	after, ok := uuidQ(c, "cursor", false)
	if !ok {
		return
	}
	pg, err := h.Svc.UsageEvents(c.Request.Context(), client, after, limitQ(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]usageEventDTO, 0, len(pg.Items))
	for _, u := range pg.Items {
		items = append(items, usageEventDTO{ID: u.ID.String(), Seq: u.Seq, ClientID: u.ClientID.String(), KeyPrefix: u.KeyPrefix,
			AuthMethod: u.AuthMethod, CallKind: u.CallKind, Product: u.Product, ContractVersion: u.ContractVer, RouteTemplate: u.RouteTemplate,
			Status: u.Status, ProblemCode: u.ProblemCode, DataTenantID: optRef(u.DataTenantID), Units: u.Units, Rows: u.RowsOut, Bytes: u.BytesOut,
			LatencyMS: u.LatencyMS, Replayed: u.Replayed, RuleVersion: u.RuleVersion, EventTime: ts(u.EventTime), RecordedAt: ts(u.RecordedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}

type statementReq struct {
	Period           string  `json:"period"`
	ClientID         *string `json:"client_id"`
	SupersedesID     *string `json:"supersedes_id"`
	CorrectionReason *string `json:"correction_reason"`
}

type jobDTO struct {
	ID               string   `json:"id"`
	Period           string   `json:"period"`
	ClientID         *string  `json:"client_id"`
	SupersedesID     *string  `json:"supersedes_id"`
	CorrectionReason *string  `json:"correction_reason"`
	Status           string   `json:"status"`
	StatementIDs     []string `json:"statement_ids"`
	Error            *string  `json:"error"`
	RequestedBy      string   `json:"requested_by"`
	RequestedAt      string   `json:"requested_at"`
	CompletedAt      *string  `json:"completed_at"`
}

func toJobDTO(j store.Job) jobDTO {
	ids := make([]string, 0, len(j.StatementIDs))
	for _, id := range j.StatementIDs {
		ids = append(ids, id.String())
	}
	return jobDTO{ID: j.ID.String(), Period: j.Period, ClientID: optRef(j.ClientID), SupersedesID: optRef(j.SupersedesID),
		CorrectionReason: j.CorrectionReason, Status: j.Status, StatementIDs: ids, Error: j.Error, RequestedBy: j.RequestedBy.String(),
		RequestedAt: ts(j.RequestedAt), CompletedAt: optTS(j.CompletedAt)}
}

func parseOptID(field string, s *string) (*uuid.UUID, error) {
	if s == nil || *s == "" {
		return nil, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil, problem.Validation.WithField(field, "không phải UUID")
	}
	return &id, nil
}

func (h Handler) requestStatement(c *gin.Context) {
	var req statementReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpx.Fail(c, problem.BadRequest.WithDetail("Thân request phải là JSON hợp lệ"))
		return
	}
	in := app.StatementRequest{Period: req.Period, CorrectionReason: req.CorrectionReason, IdemKey: idempotency.KeyFromContext(c.Request.Context())}
	var err error
	if in.ClientID, err = parseOptID("client_id", req.ClientID); err != nil {
		httpx.Fail(c, err)
		return
	}
	if in.SupersedesID, err = parseOptID("supersedes_id", req.SupersedesID); err != nil {
		httpx.Fail(c, err)
		return
	}
	j, _, err := h.Svc.RequestStatement(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Location", "/v1/metering/statement-jobs/"+j.ID.String())
	c.JSON(http.StatusAccepted, toJobDTO(j))
}

func (h Handler) job(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	j, err := h.Svc.StatementJob(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toJobDTO(j))
}

type lineDTO struct {
	Product      string  `json:"product"`
	StatusClass  string  `json:"status_class"`
	Calls        int64   `json:"calls"`
	Units        int64   `json:"units"`
	UnitPrice    *string `json:"unit_price_vnd"`
	Amount       *string `json:"amount_vnd"`
	PriceVersion *string `json:"price_version"`
}

type statementDTO struct {
	ID               string    `json:"id"`
	ClientID         string    `json:"client_id"`
	ClientTenantID   string    `json:"client_tenant_id"`
	Period           string    `json:"period"`
	Status           string    `json:"status"`
	Lines            []lineDTO `json:"lines"`
	Subscription     *string   `json:"subscription_vnd"`
	Total            *string   `json:"total_vnd"`
	PriceVersion     *string   `json:"price_version"`
	FirstSeq         int64     `json:"first_seq"`
	LastSeq          int64     `json:"last_seq"`
	ChainHeadHash    string    `json:"chain_head_hash"`
	SnapshotSHA256   string    `json:"snapshot_sha256"`
	RuleVersion      string    `json:"rule_version"`
	JobID            string    `json:"job_id"`
	RequestedBy      string    `json:"requested_by"`
	RequestedAt      string    `json:"requested_at"`
	ApprovedBy       *string   `json:"approved_by"`
	ApprovedAt       *string   `json:"approved_at"`
	SupersedesID     *string   `json:"supersedes_id"`
	CorrectionReason *string   `json:"correction_reason"`
	SupersededBy     *string   `json:"superseded_by"`
}

func toStatementDTO(v app.StatementView) statementDTO {
	d := statementDTO{ID: v.ID.String(), ClientID: v.ClientID.String(), ClientTenantID: v.ClientTenantID.String(), Period: v.Period,
		Status: string(v.Status), Lines: make([]lineDTO, 0, len(v.Lines)), PriceVersion: v.PriceVersion, FirstSeq: v.FirstSeq, LastSeq: v.LastSeq,
		ChainHeadHash: v.ChainHeadHash, SnapshotSHA256: v.SnapshotSHA256, RuleVersion: v.RuleVersion, JobID: v.JobID.String(),
		RequestedBy: v.RequestedBy.String(), RequestedAt: ts(v.RequestedAt), ApprovedBy: optRef(&v.ApprovedBy), ApprovedAt: optTS(v.ApprovedAt),
		SupersedesID: optRef(&v.SupersedesID), CorrectionReason: v.CorrectionReason, SupersededBy: optRef(v.SupersededBy)}
	if v.Subscription != nil {
		s := v.Subscription.String()
		d.Subscription = &s
	}
	if v.Total != nil {
		s := v.Total.String()
		d.Total = &s
	}
	for _, l := range v.Lines {
		ld := lineDTO{Product: l.Product, StatusClass: l.StatusClass, Calls: l.Calls, Units: l.Units}
		if l.UnitPrice != nil {
			s := l.UnitPrice.String()
			ld.UnitPrice = &s
		}
		if l.Amount != nil {
			s := l.Amount.String()
			ld.Amount = &s
		}
		if l.PriceVersion != "" {
			s := l.PriceVersion
			ld.PriceVersion = &s
		}
		d.Lines = append(d.Lines, ld)
	}
	return d
}

func (h Handler) statements(c *gin.Context) {
	client, ok := uuidQ(c, "client_id", false)
	if !ok {
		return
	}
	after, ok := uuidQ(c, "cursor", false)
	if !ok {
		return
	}
	pg, err := h.Svc.Statements(c.Request.Context(), c.Query("period"), client, after, limitQ(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]statementDTO, 0, len(pg.Items))
	for _, v := range pg.Items {
		items = append(items, toStatementDTO(v))
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}

func (h Handler) statement(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.Svc.StatementDetail(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, toStatementDTO(v))
}

func (h Handler) approve(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	_, replayed, err := h.Svc.ApproveStatement(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	v, err := h.Svc.StatementDetail(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	c.JSON(status, toStatementDTO(v))
}

func (h Handler) verify(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	v, err := h.Svc.VerifyStatement(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"statement_id": id.String(), "stored_sha256": v.Stored, "rebuilt_sha256": v.Rebuilt, "match": v.Match})
}

func (h Handler) exportCSV(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	body, st, err := h.Svc.StatementCSV(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="bang-ke-`+st.Period+`-`+st.ID.String()+`.csv"`)
	c.Header("Cache-Control", "no-store")
	c.Header("AG-Snapshot-SHA256", st.SnapshotSHA256)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", body)
}

type paramDTO struct {
	Key    string  `json:"key"`
	Value  *string `json:"value"`
	Source string  `json:"source"`
}

func (h Handler) params(c *gin.Context) {
	at := h.Svc.Clock.Now()
	if s := c.Query("at"); s != "" {
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			httpx.Fail(c, problem.Validation.WithField("at", "phải theo RFC 3339 có múi giờ"))
			return
		}
		at = t
	}
	ps, err := h.Svc.EffectiveParams(c.Request.Context(), at)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]paramDTO, 0, len(ps))
	for _, p := range ps {
		items = append(items, paramDTO{Key: p.Key, Value: p.Value, Source: p.Source})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

type egressDTO struct {
	ID               string `json:"id"`
	Seq              int64  `json:"seq"`
	Channel          string `json:"channel"`
	SourceID         string `json:"source_id"`
	CounterpartyKind string `json:"counterparty_kind"`
	CounterpartyRef  string `json:"counterparty_ref"`
	Product          string `json:"product"`
	ContractVersion  int    `json:"contract_version"`
	Records          int64  `json:"records"`
	Bytes            int64  `json:"bytes"`
	BusinessDate     string `json:"business_date"`
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
	Mode             string `json:"mode"`
	DailyRecords     int64  `json:"daily_records"`
	DailyQuota       int64  `json:"daily_quota"`
	Anomaly          bool   `json:"anomaly"`
	ActorRef         string `json:"actor_ref"`
	EventTime        string `json:"event_time"`
	RecordedAt       string `json:"recorded_at"`
}

func (h Handler) egress(c *gin.Context) {
	after, ok := uuidQ(c, "cursor", false)
	if !ok {
		return
	}
	from, ok := dateQ(c, "from")
	if !ok {
		return
	}
	to, ok := dateQ(c, "to")
	if !ok {
		return
	}
	pg, err := h.Svc.EgressEvents(c.Request.Context(), store.EgressFilter{Channel: c.Query("channel"), From: from, To: to, After: after, Limit: limitQ(c)})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]egressDTO, 0, len(pg.Items))
	for _, e := range pg.Items {
		items = append(items, egressDTO{ID: e.ID.String(), Seq: e.Seq, Channel: e.Channel, SourceID: e.SourceID.String(),
			CounterpartyKind: e.CounterpartyKind, CounterpartyRef: e.CounterpartyRef.String(), Product: e.Product, ContractVersion: e.ContractVersion,
			Records: e.Records, Bytes: e.Bytes, BusinessDate: e.BusinessDate.Format(time.DateOnly), Decision: e.Decision, Reason: e.Reason,
			Mode: e.Mode, DailyRecords: e.DailyRecords, DailyQuota: e.DailyQuota, Anomaly: e.Anomaly, ActorRef: e.ActorRef.String(),
			EventTime: ts(e.EventTime), RecordedAt: ts(e.RecordedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}

type anomalyDTO struct {
	ID               string `json:"id"`
	BusinessDate     string `json:"business_date"`
	Channel          string `json:"channel"`
	CounterpartyKind string `json:"counterparty_kind"`
	CounterpartyRef  string `json:"counterparty_ref"`
	Product          string `json:"product"`
	EgressEventID    string `json:"egress_event_id"`
	DailyRecords     int64  `json:"daily_records"`
	Threshold        int64  `json:"threshold"`
	Basis            string `json:"basis"`
	Mode             string `json:"mode"`
	DetectedAt       string `json:"detected_at"`
}

func (h Handler) anomalies(c *gin.Context) {
	after, ok := uuidQ(c, "cursor", false)
	if !ok {
		return
	}
	pg, err := h.Svc.Anomalies(c.Request.Context(), c.Query("channel"), after, limitQ(c))
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	items := make([]anomalyDTO, 0, len(pg.Items))
	for _, a := range pg.Items {
		items = append(items, anomalyDTO{ID: a.ID.String(), BusinessDate: a.BusinessDate.Format(time.DateOnly), Channel: a.Channel,
			CounterpartyKind: a.CounterpartyKind, CounterpartyRef: a.CounterpartyRef.String(), Product: a.Product,
			EgressEventID: a.EgressEventID.String(), DailyRecords: a.DailyRecords, Threshold: a.Threshold, Basis: a.Basis, Mode: a.Mode,
			DetectedAt: ts(a.DetectedAt)})
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "next_cursor": next(pg.Next)})
}
