package app

// Việc xuất bất đồng bộ của Public API (R-52, mẫu mục 12): api-public ghi job + ảnh chụp đầu vào bất
// biến + sự kiện pf.apiclient.export.requested trong MỘT giao dịch; worker (consumer
// pf.apiclient.export) đọc toàn bộ ô qua nguồn của module chủ dữ liệu (cùng ngưỡng k, mốc as_of =
// requested_at), kiểm từng phần tử theo hợp đồng (mặc định từ chối), dựng tệp tất định, SHA-256, ghi
// kho đối tượng trong lãnh thổ. Tệp chỉ tải qua GET /v1/jobs/{id}/file của api-public — KHÔNG có URL
// có chữ ký (ADR-0022 mục 8).

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/contracts/dataproduct"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/objstore"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// ExportRequest là yêu cầu xuất đã kiểm quyền ở api-public.
type ExportRequest struct {
	Caller          port.Caller
	DataTenant      uuid.UUID
	Product         *dataproduct.Product
	Format          string
	Filters         store.ExportFilters
	K               int64
	MaxRows         int64
	IdemKey         string // Idempotency-Key của bên thứ ba
	RequestHash     string // SHA-256 hex của (route, thân)
	OperatorTenant  uuid.UUID
	RequestedAtTime time.Time
}

// RequestExport ghi job (hoặc trả job đã có với cùng Idempotency-Key — replayed = true). Cùng khóa,
// khác thân → problem.IdempotencyConflict.
func (s *Service) RequestExport(ctx context.Context, r ExportRequest) (store.ExportJob, bool, error) {
	idem := "export:" + r.Caller.Client.ID.String() + ":" + r.IdemKey
	var out store.ExportJob
	replayed := false
	err := tenant.WithTenantTx(ctx, s.DB, r.OperatorTenant, func(tx pgx.Tx) error {
		prev, err := store.JobByIdem(ctx, tx, idem)
		if err != nil {
			return err
		}
		if prev != nil {
			if prev.RequestHash != r.RequestHash {
				return problem.IdempotencyConflict
			}
			out, replayed = *prev, true
			return nil
		}
		j := store.ExportJob{ID: ids.New(), TenantID: r.OperatorTenant, ClientID: r.Caller.Client.ID, ClientTenantID: r.Caller.Client.ClientTenantID,
			DataTenantID: r.DataTenant, Product: r.Product.Name, ContractVersion: r.Product.Version, Format: r.Format, Filters: r.Filters,
			KApplied: int(r.K), MaxRows: r.MaxRows, RequestID: r.Caller.RequestID, RequestHash: r.RequestHash, IdemKey: idem,
			RequestedAt: r.RequestedAtTime.UTC().Truncate(time.Microsecond), Status: string(port.ExportQueued)}
		if err := store.InsertJob(ctx, tx, j); err != nil {
			return err
		}
		out = j
		return outbox.Enqueue(ctx, tx, outbox.Event{ID: ids.New(), TenantID: r.OperatorTenant, Type: port.EventExportRequested, SchemaVersion: 1,
			OccurredAt: j.RequestedAt, Producer: Producer, Payload: port.ExportRequestedV1{JobID: j.ID.String(), ClientID: j.ClientID.String(),
				Product: j.Product, ContractVersion: j.ContractVersion, DataTenantID: j.DataTenantID.String(), RequestedAt: formatTime(j.RequestedAt)}})
	})
	return out, replayed, err
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// JobFor đọc việc xuất của CHÍNH khách hàng (job của khách hàng khác → ErrJobNotFound).
func (s *Service) JobFor(ctx context.Context, operatorTenant, clientID, jobID uuid.UUID) (store.ExportJob, *store.ExportFile, error) {
	var (
		job  store.ExportJob
		file *store.ExportFile
	)
	err := tenant.WithTenantTx(ctx, s.DB, operatorTenant, func(tx pgx.Tx) error {
		j, err := store.JobByID(ctx, tx, jobID, false)
		if err != nil {
			return err
		}
		if j == nil || j.ClientID != clientID {
			return ErrJobNotFound
		}
		job = *j
		if j.Status == string(port.ExportCompleted) {
			file, err = store.FileOf(ctx, tx, jobID)
		}
		return err
	})
	return job, file, err
}

// OpenFile mở tệp kết quả từ kho đối tượng để phát luồng qua api-public.
func (s *Service) OpenFile(ctx context.Context, f store.ExportFile) (io.ReadCloser, error) {
	if s.Store == nil {
		return nil, ErrDependencyUnavailable
	}
	rc, _, err := s.Store.Get(ctx, f.ObjectKey)
	return rc, err
}

// ObjectKey là khóa đối tượng của tệp xuất.
func ObjectKey(tid, jobID uuid.UUID, format string) string {
	return fmt.Sprintf("apiclient/exports/%s/%s.%s", tid, jobID, format)
}

// ContentType của định dạng xuất.
func ContentType(format string) string {
	if format == "csv" {
		return "text/csv; charset=utf-8"
	}
	return "application/json"
}

// pageSize của nguồn khi worker đọc toàn bộ ô.
const pageSize = 500

// HandleExportRequested là inbox.Handler của consumer pf.apiclient.export (pool vai apiclient_app,
// tenant = đơn vị vận hành). Giao lặp an toàn: job đã xử lý → bỏ qua.
func (s *Service) HandleExportRequested(ctx context.Context, tx pgx.Tx, env outbox.Envelope) error {
	var pl port.ExportRequestedV1
	if err := json.Unmarshal(env.Payload, &pl); err != nil {
		return fmt.Errorf("apiclient: payload %s hỏng: %w", env.EventType, err)
	}
	id, err := uuid.Parse(pl.JobID)
	if err != nil {
		return fmt.Errorf("apiclient: job_id không hợp lệ: %w", err)
	}
	j, err := store.JobByID(ctx, tx, id, true)
	if err != nil {
		return err
	}
	if j == nil || j.Status != string(port.ExportQueued) {
		return nil
	}
	now := s.Clock.Now()
	prod, err := dataproduct.Get(j.Product)
	if err != nil || prod.Version != j.ContractVersion {
		return store.FailJob(ctx, tx, j.ID, "apiclient.unknown_product", now)
	}
	if j.Product != port.ProductCollectionVolume {
		return store.FailJob(ctx, tx, j.ID, "apiclient.export_not_supported", now)
	}
	if s.Collection == nil || s.Store == nil {
		return ErrDependencyUnavailable // giao lại → DLQ (R-62); không mất yêu cầu
	}
	items, err := s.collectAll(ctx, *j)
	if err != nil {
		var p *problem.Problem
		if errors.As(err, &p) && (p.Code == port.ErrExportTooLarge.Code || p.Code == port.ErrContractViolation.Code) {
			s.log().WarnContext(ctx, "việc xuất Public API bị từ chối", "event", "apiclient.export", "job_id", j.ID, "outcome", "rejected", "code", p.Code)
			return store.FailJob(ctx, tx, j.ID, p.Code, now)
		}
		return err
	}
	body, err := RenderExport(prod, j.Format, items)
	if err != nil {
		return err
	}
	sha := objstore.SHA256Hex(body)
	key := ObjectKey(j.TenantID, j.ID, j.Format)
	if _, err := s.Store.Put(ctx, key, bytes.NewReader(body), int64(len(body)), sha, ContentType(j.Format)); err != nil {
		if errors.Is(err, objstore.ErrExists) {
			return store.FailJob(ctx, tx, j.ID, "apiclient.export_conflict", now)
		}
		return err
	}
	if err := store.InsertFile(ctx, tx, store.ExportFile{TenantID: j.TenantID, JobID: j.ID, ObjectKey: key, SHA256: sha, SizeBytes: int64(len(body)),
		Rows: int64(len(items)), ContentType: ContentType(j.Format)}); err != nil {
		return err
	}
	return store.CompleteJob(ctx, tx, j.ID, int64(len(items)), sha, now)
}

func (s *Service) collectAll(ctx context.Context, j store.ExportJob) ([]port.CollectionVolumeV1, error) {
	var out []port.CollectionVolumeV1
	after := ""
	for {
		page, err := s.Collection.CollectionCells(ctx, port.CollectionQuery{DataTenant: j.DataTenantID, SeasonCode: j.Filters.SeasonCode,
			ProvinceCode: j.Filters.ProvinceCode, FromMonth: j.Filters.FromMonth, ToMonth: j.Filters.ToMonth, K: int64(j.KApplied),
			AsOf: j.RequestedAt, After: after, Limit: pageSize})
		if err != nil {
			return nil, err
		}
		out = append(out, page.Items...)
		if int64(len(out)) > j.MaxRows {
			return nil, port.ErrExportTooLarge
		}
		if page.Next == "" || len(page.Items) == 0 {
			return out, nil
		}
		after = page.Next
	}
}

// RenderExport dựng tệp tất định (cùng phần tử → cùng byte): mỗi phần tử kiểm theo hợp đồng trước
// (mặc định từ chối). CSV: cột theo thứ tự trường của hợp đồng v1; JSON: {"contract", "items"}.
func RenderExport(p *dataproduct.Product, format string, items []port.CollectionVolumeV1) ([]byte, error) {
	raws, err := EncodeItems(p, items)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	switch format {
	case "json":
		buf.WriteString(`{"contract":` + strconv.Quote(p.ContractHeader()) + `,"items":[`)
		for i, r := range raws {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.Write(r)
		}
		buf.WriteString("]}\n")
	case "csv":
		w := csv.NewWriter(&buf)
		_ = w.Write([]string{"province_code", "commune_code", "season_code", "period_month", "rolls_collected", "area_ha_collected", "k_applied", "as_of"})
		for _, it := range items {
			_ = w.Write([]string{it.ProvinceCode, it.CommuneCode, it.SeasonCode, it.PeriodMonth, strconv.FormatInt(it.RollsCollected, 10),
				it.AreaHaCollected, strconv.FormatInt(it.KApplied, 10), it.AsOf})
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("apiclient: định dạng xuất %q không hỗ trợ", format)
	}
	return buf.Bytes(), nil
}

// EncodeItems mã hóa từng phần tử và kiểm theo hợp đồng sản phẩm (mặc định từ chối): một phần tử có
// trường ngoài hợp đồng, sai kiểu hay thiếu trường bắt buộc → ErrContractViolation, không trả gì.
func EncodeItems[T any](p *dataproduct.Product, items []T) ([]json.RawMessage, error) {
	out := make([]json.RawMessage, 0, len(items))
	for _, it := range items {
		raw, err := json.Marshal(it)
		if err != nil {
			return nil, err
		}
		if err := p.Validate(raw); err != nil {
			return nil, port.ErrContractViolation.WithCause(err)
		}
		out = append(out, raw)
	}
	return out, nil
}
