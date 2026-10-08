package public

import (
	"io"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/ingest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Nạp theo lô (W5.4; R-INT-03, R-INT-04, ADR-0033 §3): POST /v1/batches/{contract} → 202 + mã lô,
// GET /v1/batches/{id} → trạng thái + kết quả từng bản ghi. Cùng đường ống của mọi route /v1 (xác thực khóa
// [+ mTLS ở cổng], giới hạn tần suất, sổ đo lường trước byte). Sổ đo lường: POST ghi call_kind export_request
// (yêu cầu 202, 0 đơn vị), GET ghi job_status; product = tên hợp đồng nạp, job_id = mã lô.

var contractNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,47}$`)

func registerBatches(s *Server, v1 *gin.RouterGroup) {
	v1.POST("/batches/:contract", s.handle(spec{kind: meter.CallExportRequest}, s.submitBatch))
	v1.GET("/batches/:id", s.handle(spec{kind: meter.CallJobStatus}, s.getBatch))
}

type batchDTO struct {
	ID              string          `json:"id"`
	Contract        string          `json:"contract"`
	ContractVersion int             `json:"contract_version"`
	BatchRef        string          `json:"batch_ref"`
	Status          string          `json:"status"`
	Items           int             `json:"items"`
	Accepted        int             `json:"accepted"`
	Rejected        int             `json:"rejected"`
	ProblemCode     *string         `json:"problem_code"`
	ReceivedAt      string          `json:"received_at"`
	CompletedAt     *string         `json:"completed_at"`
	Results         []ingest.Result `json:"results,omitempty"`
}

func batchURL(id uuid.UUID) string { return "/v1/batches/" + id.String() }

func toBatchDTO(b ingest.Batch) batchDTO {
	d := batchDTO{ID: b.ID.String(), Contract: b.Contract, ContractVersion: b.ContractVersion, BatchRef: b.BatchRef.String(), Status: b.Status,
		Items: b.Items, Accepted: b.Accepted, Rejected: b.Rejected, ReceivedAt: b.ReceivedAt.UTC().Format(time.RFC3339Nano), Results: b.Results}
	if b.ProblemCode != "" {
		code := b.ProblemCode
		d.ProblemCode = &code
	}
	if b.CompletedAt != nil {
		at := b.CompletedAt.UTC().Format(time.RFC3339Nano)
		d.CompletedAt = &at
	}
	return d
}

func (s *Server) submitBatch(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	name := c.Param("contract")
	if !contractNameRe.MatchString(name) || !ingest.Known(name) {
		return nil, ingest.ErrContractUnknown
	}
	// Scope nạp của hợp đồng (mặc định từ chối — không nói thiếu điều kiện nào).
	if !caller.Client.HasScope(port.IngestScope(name)) {
		return nil, port.ErrForbidden
	}
	key := c.GetHeader(idempotency.Header)
	if key == "" {
		return nil, problem.IdempotencyRequired
	}
	if s.d.Ingest == nil {
		return nil, app.ErrDependencyUnavailable
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return nil, problem.PayloadTooLarge
	}
	b, replayed, err := s.d.Ingest.Submit(ctx, ingest.Submission{ClientID: caller.Client.ID, ClientTenant: caller.Client.ClientTenantID,
		OperatorTenant: s.d.OperatorTenant, Contract: name, IdemKey: key, Body: raw, ReceivedAt: caller.ReceivedAt, TraceID: telemetry.TraceID(ctx)})
	if err != nil {
		res := s.failResult(c, err)
		if res.problemCode == ingest.ErrQueueSaturated.Code {
			res.header.Set("Retry-After", seconds(s.d.Ingest.Wait()))
		}
		res.product = name
		return res, nil
	}
	r, err := jsonResult(http.StatusAccepted, toBatchDTO(b))
	if err != nil {
		return nil, err
	}
	r.header.Set("Location", batchURL(b.ID))
	r.header.Set(port.HeaderDataContract, b.Contract+".v"+strconv.Itoa(b.ContractVersion))
	if replayed {
		r.header.Set(idempotency.ReplayedHeader, "true")
	}
	r.replayed, r.jobID, r.product, r.contractVersion = replayed, b.ID, b.Contract, b.ContractVersion
	return r, nil
}

func (s *Server) getBatch(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, ingest.ErrBatchNotFound
	}
	if s.d.Ingest == nil {
		return nil, app.ErrDependencyUnavailable
	}
	b, err := s.d.Ingest.Get(c.Request.Context(), caller.Client.ClientTenantID, caller.Client.ID, id)
	if err != nil {
		return nil, err
	}
	r, err := jsonResult(http.StatusOK, toBatchDTO(b))
	if err != nil {
		return nil, err
	}
	r.jobID, r.product, r.contractVersion = b.ID, b.Contract, b.ContractVersion
	return r, nil
}
