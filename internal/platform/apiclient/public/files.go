package public

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/ingest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/idempotency"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/meter"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// Kênh tệp cùng lược đồ (W5.8; R-INT-11, ADR-0033 §3): POST /v1/files/{contract}?contract_version=N (thân = tệp
// NDJSON hoặc CSV) → 202 + mã tệp; GET /v1/files/{id} → trạng thái các lô của tệp; GET /v1/files/{id}/results →
// tệp kết quả từng bản ghi (CSV/NDJSON). Cùng đường ống của mọi route /v1 (khóa API [+ mTLS], giới hạn tần suất,
// sổ đo lường = điểm kiểm soát trước byte — đóng khi lỗi) và cùng scope ingest:<hợp đồng>:write như lô; tệp đổi
// thành lô qua ingest.Service.SubmitFile → Submit (W5.4), không có đường kiểm thứ hai. Sổ đo lường: POST =
// export_request (0 đơn vị, job_id = mã tệp), GET = job_status; product = hợp đồng.

func registerFiles(s *Server, v1 *gin.RouterGroup) {
	limit := int64(ingest.DefaultMaxFileBytes)
	if s.d.Ingest != nil {
		limit = s.d.Ingest.FileLimit()
	}
	// +1 byte: phân biệt "đúng giới hạn" với "vượt" (vượt → 413 integration.file_too_large).
	httpx.AllowBody(v1, http.MethodPost, "/files/:contract", limit+1)
	v1.POST("/files/:contract", s.handle(spec{kind: meter.CallExportRequest}, s.submitFile))
	v1.GET("/files/:id", s.handle(spec{kind: meter.CallJobStatus}, s.getFile))
	v1.GET("/files/:id/results", s.handle(spec{kind: meter.CallJobStatus}, s.fileResults))
}

type fileBatchDTO struct {
	Index       int     `json:"index"`
	BatchRef    string  `json:"batch_ref"`
	ID          *string `json:"id"`
	Status      string  `json:"status"`
	Items       int     `json:"items"`
	Accepted    int     `json:"accepted"`
	Rejected    int     `json:"rejected"`
	ProblemCode *string `json:"problem_code"`
}

type fileDTO struct {
	ID              string         `json:"id"`
	Contract        string         `json:"contract"`
	ContractVersion int            `json:"contract_version"`
	Format          string         `json:"format"`
	SHA256          string         `json:"sha256"`
	Bytes           int64          `json:"bytes"`
	Records         int            `json:"records"`
	Status          string         `json:"status"`
	Accepted        int            `json:"accepted"`
	Rejected        int            `json:"rejected"`
	ReceivedAt      string         `json:"received_at"`
	ResultsURL      string         `json:"results_url"`
	Batches         []fileBatchDTO `json:"batches"`
}

func fileURL(id uuid.UUID) string { return "/v1/files/" + id.String() }

func toFileDTO(f ingest.File) fileDTO {
	d := fileDTO{ID: f.ID.String(), Contract: f.Contract, ContractVersion: f.ContractVersion, Format: f.Format, SHA256: f.SHA256, Bytes: f.Bytes,
		Records: f.Records, Status: f.Status, Accepted: f.Accepted, Rejected: f.Rejected, ReceivedAt: f.ReceivedAt.UTC().Format(time.RFC3339Nano),
		ResultsURL: fileURL(f.ID) + "/results", Batches: make([]fileBatchDTO, 0, len(f.Batches))}
	for _, b := range f.Batches {
		x := fileBatchDTO{Index: b.Index, BatchRef: b.BatchRef.String(), Status: b.Status, Items: b.Items, Accepted: b.Accepted, Rejected: b.Rejected}
		if b.ID != uuid.Nil {
			id := b.ID.String()
			x.ID = &id
		}
		if b.ProblemCode != "" {
			code := b.ProblemCode
			x.ProblemCode = &code
		}
		d.Batches = append(d.Batches, x)
	}
	return d
}

func (s *Server) submitFile(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	ctx := c.Request.Context()
	name := c.Param("contract")
	if !contractNameRe.MatchString(name) || !ingest.Known(name) {
		return nil, ingest.ErrContractUnknown
	}
	// Cùng scope với POST /v1/batches/{contract}: cùng hợp đồng, khác kênh (R-INT-11).
	if !caller.Client.HasScope(port.IngestScope(name)) {
		return nil, port.ErrForbidden
	}
	version, err := strconv.Atoi(c.Query("contract_version"))
	if err != nil || version < 1 {
		return nil, problem.Validation.WithField("contract_version", "số nguyên ≥ 1 (phiên bản hợp đồng của tệp)")
	}
	format, err := ingest.FormatOf(c.GetHeader("Content-Type"))
	if err != nil {
		return nil, err
	}
	if s.d.Ingest == nil {
		return nil, app.ErrDependencyUnavailable
	}
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return nil, ingest.ErrFileTooLarge
		}
		return nil, ingest.ErrFileInvalid.WithDetail("không đọc được thân request")
	}
	f, replayed, err := s.d.Ingest.SubmitFile(ctx, ingest.FileUpload{ClientID: caller.Client.ID, ClientTenant: caller.Client.ClientTenantID,
		OperatorTenant: s.d.OperatorTenant, Contract: name, ContractVersion: version, Format: format, Body: raw, ReceivedAt: caller.ReceivedAt,
		TraceID: telemetry.TraceID(ctx)})
	if err != nil {
		res := s.failResult(c, err)
		if res.problemCode == ingest.ErrQueueSaturated.Code {
			res.header.Set("Retry-After", seconds(s.d.Ingest.Wait()))
		}
		res.product = name
		return res, nil
	}
	r, err := jsonResult(http.StatusAccepted, toFileDTO(f))
	if err != nil {
		return nil, err
	}
	r.header.Set("Location", fileURL(f.ID))
	r.header.Set(port.HeaderDataContract, f.Contract+".v"+strconv.Itoa(f.ContractVersion))
	if replayed {
		r.header.Set(idempotency.ReplayedHeader, "true")
	}
	r.replayed, r.jobID, r.product, r.contractVersion = replayed, f.ID, f.Contract, f.ContractVersion
	return r, nil
}

func (s *Server) getFile(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, ingest.ErrFileNotFound
	}
	if s.d.Ingest == nil {
		return nil, app.ErrDependencyUnavailable
	}
	f, err := s.d.Ingest.GetFile(c.Request.Context(), caller.Client.ClientTenantID, caller.Client.ID, id)
	if err != nil {
		return nil, err
	}
	r, err := jsonResult(http.StatusOK, toFileDTO(f))
	if err != nil {
		return nil, err
	}
	r.jobID, r.product, r.contractVersion = f.ID, f.Contract, f.ContractVersion
	return r, nil
}

// fileResults phát tệp kết quả (?format=csv|ndjson, mặc định cùng định dạng tệp tải lên). Chỉ khóa thực thể, kết
// cục và mã problem — không giá trị bản ghi. Chưa xong → 409 integration.file_results_pending + Retry-After.
func (s *Server) fileResults(c *gin.Context, caller port.Caller, _ port.Plan) (*result, error) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		return nil, ingest.ErrFileNotFound
	}
	format := c.Query("format")
	if format != "" && format != ingest.FormatCSV && format != ingest.FormatNDJSON {
		return nil, problem.Validation.WithField("format", "csv | ndjson")
	}
	if s.d.Ingest == nil {
		return nil, app.ErrDependencyUnavailable
	}
	f, rows, err := s.d.Ingest.FileResults(c.Request.Context(), caller.Client.ClientTenantID, caller.Client.ID, id)
	if err != nil {
		if isCode(err, ingest.ErrFileResultsPending.Code) {
			res := s.failResult(c, err)
			res.header.Set("Retry-After", "10")
			return res, nil
		}
		return nil, err
	}
	if format == "" {
		format = f.Format
	}
	var buf bytes.Buffer
	if err := ingest.WriteResults(&buf, format, rows); err != nil {
		return nil, err
	}
	ext := ".ndjson"
	if format == ingest.FormatCSV {
		ext = ".csv"
	}
	r := newResult(http.StatusOK)
	r.body = buf.Bytes()
	r.header.Set("Content-Type", ingest.ContentType(format))
	r.header.Set("Content-Disposition", `attachment; filename="`+f.ID.String()+`.results`+ext+`"`)
	r.header.Set(port.HeaderDataContract, f.Contract+".v"+strconv.Itoa(f.ContractVersion))
	r.rows, r.jobID, r.product, r.contractVersion = int64(len(rows)), f.ID, f.Contract, f.ContractVersion
	return r, nil
}
