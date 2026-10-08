package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Trạng thái tệp (gộp từ các lô của tệp).
const (
	// FileIncomplete: còn lô chưa gửi được (vd 429 giữa chừng) — tải lại CÙNG tệp để gửi tiếp.
	FileIncomplete = "incomplete"
)

// FileLimit trả giới hạn byte của kênh tệp.
func (s *Service) FileLimit() int64 {
	if s.MaxFileBytes <= 0 {
		return DefaultMaxFileBytes
	}
	return s.MaxFileBytes
}

func (s *Service) fileRecords() int {
	if s.MaxFileRecords <= 0 {
		return DefaultMaxFileRecords
	}
	return s.MaxFileRecords
}

// FileUpload là một POST /v1/files/{contract} đã xác thực.
type FileUpload struct {
	ClientID        uuid.UUID
	ClientTenant    uuid.UUID // tenant đối tác — từ bản ghi khóa
	OperatorTenant  uuid.UUID
	Contract        string
	ContractVersion int
	Format          string
	Body            []byte
	ReceivedAt      time.Time
	TraceID         string
}

// FileBatch là một lô của tệp.
type FileBatch struct {
	Index       int
	BatchRef    uuid.UUID
	ID          uuid.UUID // Nil khi lô chưa gửi
	Status      string    // queued | completed | dead_lettered | incomplete
	Items       int
	Accepted    int
	Rejected    int
	ProblemCode string
}

// File là trạng thái một tệp nạp.
type File struct {
	ID              uuid.UUID
	Contract        string
	ContractVersion int
	Format          string
	SHA256          string
	Bytes           int64
	Records         int
	Status          string // incomplete | queued | completed | dead_lettered
	Accepted        int
	Rejected        int
	ReceivedAt      time.Time
	Batches         []FileBatch
}

// SubmitFile nhận một tệp: kiểm hợp đồng (mặc định từ chối, như lô) + kênh tệp + giới hạn, tách bản ghi, ghi sổ
// ingest_file (idempotent theo tổng kiểm SHA-256 của tệp), rồi gửi từng lô qua Submit của W5.4 — cùng phong bì,
// cùng hàng đợi đối tác, cùng kiểm hợp đồng ở inbox. Tải lại cùng tệp → cùng mã tệp (replayed = true), lô đã gửi
// là gọi lặp, lô còn thiếu được gửi tiếp. Lỗi giữa chừng (429 hàng đợi đầy, 503) trả nguyên lỗi; tệp ở trạng
// thái incomplete tới khi tải lại.
func (s *Service) SubmitFile(ctx context.Context, u FileUpload) (File, bool, error) {
	if s.Queue == nil {
		return File{}, false, ErrQueueUnavailable.WithCause(errNoQueue)
	}
	c, err := s.Contract(u.Contract, u.ContractVersion)
	if err != nil {
		return File{}, false, err
	}
	if !FileChannel(c) {
		return File{}, false, ErrFileChannelNotAllowed
	}
	if int64(len(u.Body)) > s.FileLimit() {
		return File{}, false, ErrFileTooLarge
	}
	if len(u.Body) == 0 {
		return File{}, false, ErrFileInvalid.WithDetail("tệp rỗng")
	}
	records, err := ParseFile(c, u.Format, u.Body, s.fileRecords())
	if err != nil {
		return File{}, false, err
	}
	sum := Hash(u.Body)
	chunk := c.Ingest.MaxBatch
	// Micro giây: event_time lưu ở PostgreSQL — tải lại dựng đúng cùng sent_at (cùng thân lô).
	row := fileRow{ID: ids.New(), TenantID: u.ClientTenant, ClientID: u.ClientID, OperatorTenant: u.OperatorTenant, Contract: c.Name,
		ContractVersion: c.Version, Format: u.Format, SHA256: sum, Bytes: int64(len(u.Body)), Records: len(records), ChunkSize: chunk,
		Batches: BatchCount(len(records), chunk), TraceID: u.TraceID, ReceivedAt: u.ReceivedAt.UTC().Truncate(time.Microsecond)}

	var prev *fileRow
	lookup := func(tx pgx.Tx) error {
		var err error
		prev, err = fileByChecksum(ctx, tx, u.ClientID, c.Name, c.Version, sum)
		return err
	}
	if err := tenant.WithTenantTx(ctx, s.DB, u.ClientTenant, lookup); err != nil {
		return File{}, false, err
	}
	replayed := prev != nil
	if !replayed {
		err := tenant.WithTenantTx(ctx, s.DB, u.ClientTenant, func(tx pgx.Tx) error { return insertFile(ctx, tx, row, c.ID()) })
		if errors.Is(err, ledger.ErrDuplicate) { // cùng tệp gửi đồng thời
			err = tenant.WithTenantTx(ctx, s.DB, u.ClientTenant, lookup)
			replayed = prev != nil
		}
		if err != nil {
			return File{}, false, err
		}
	}
	if replayed {
		row = *prev
	}
	chunks, err := FileEnvelopes(c, row.ID, records, row.ChunkSize, row.ReceivedAt)
	if err != nil {
		return File{}, false, err
	}
	for _, ch := range chunks {
		if _, _, err := s.Submit(ctx, Submission{ClientID: u.ClientID, ClientTenant: u.ClientTenant, OperatorTenant: u.OperatorTenant,
			Contract: c.Name, IdemKey: ch.BatchRef.String(), Body: ch.Body, ReceivedAt: u.ReceivedAt, TraceID: u.TraceID}); err != nil {
			s.log().WarnContext(ctx, "tệp nạp: lô chưa gửi được — tải lại cùng tệp để gửi tiếp", "event", "ingest.file_partial",
				"file_id", row.ID, "batch_index", ch.Index, "client_id", u.ClientID, "outcome", "error")
			return File{}, false, err
		}
	}
	f, err := s.GetFile(ctx, u.ClientTenant, u.ClientID, row.ID)
	return f, replayed, err
}

// GetFile đọc tệp của chính khách hàng kèm trạng thái từng lô. Tệp của khách hàng khác → ErrFileNotFound.
func (s *Service) GetFile(ctx context.Context, clientTenant, clientID, id uuid.UUID) (File, error) {
	f, _, err := s.file(ctx, clientTenant, clientID, id, false)
	return f, err
}

// FileResults trả tệp và kết quả từng bản ghi theo thứ tự trong tệp. Tệp chưa xử lý xong → ErrFileResultsPending.
// Lô dead_lettered: mọi bản ghi của lô là rejected với mã problem của kết cục lô.
func (s *Service) FileResults(ctx context.Context, clientTenant, clientID, id uuid.UUID) (File, []FileResult, error) {
	return s.file(ctx, clientTenant, clientID, id, true)
}

func (s *Service) file(ctx context.Context, clientTenant, clientID, id uuid.UUID, withResults bool) (File, []FileResult, error) {
	var (
		f    File
		rows []FileResult
	)
	err := tenant.WithTenantTx(ctx, s.DB, clientTenant, func(tx pgx.Tx) error {
		r, err := fileByID(ctx, tx, id)
		if err != nil {
			return err
		}
		if r == nil || r.ClientID != clientID {
			return ErrFileNotFound
		}
		f = File{ID: r.ID, Contract: r.Contract, ContractVersion: r.ContractVersion, Format: r.Format, SHA256: r.SHA256, Bytes: r.Bytes,
			Records: r.Records, ReceivedAt: r.ReceivedAt}
		type done struct {
			b   *batchRow
			out *outcomeRow
		}
		var all []done
		for k := 0; k < r.Batches; k++ {
			fb := FileBatch{Index: k, BatchRef: FileBatchRef(r.ID, k), Status: FileIncomplete}
			b, err := batchByRef(ctx, tx, clientID, fb.BatchRef)
			if err != nil {
				return err
			}
			var o *outcomeRow
			if b != nil {
				if o, err = outcomeOf(ctx, tx, b.ID); err != nil {
					return err
				}
				v := view(*b, o, nil)
				fb.ID, fb.Status, fb.Items, fb.Accepted, fb.Rejected, fb.ProblemCode = v.ID, v.Status, v.Items, v.Accepted, v.Rejected, v.ProblemCode
				if o != nil && o.Outcome == StatusDeadLettered {
					fb.Rejected = b.ItemCount
				}
			}
			f.Accepted += fb.Accepted
			f.Rejected += fb.Rejected
			f.Batches = append(f.Batches, fb)
			all = append(all, done{b, o})
		}
		f.Status = fileStatus(f.Batches)
		if !withResults {
			return nil
		}
		if f.Status != StatusCompleted && f.Status != StatusDeadLettered {
			return ErrFileResultsPending
		}
		for k, d := range all {
			base := k * r.ChunkSize
			if d.out.Outcome == StatusDeadLettered {
				var items []Item
				if err := json.Unmarshal(d.b.Items, &items); err != nil {
					return err
				}
				for i, it := range items {
					rows = append(rows, FileResult{Record: base + i + 1, EntityKey: it.EntityKey, Outcome: OutcomeRejected, ProblemCode: d.out.ProblemCode})
				}
				continue
			}
			res, err := resultsOf(ctx, tx, d.b.ID)
			if err != nil {
				return err
			}
			for _, x := range res {
				rows = append(rows, FileResult{Record: base + x.Index + 1, EntityKey: x.EntityKey, Outcome: x.Outcome, ProblemCode: x.ProblemCode,
					Detail: x.Detail})
			}
		}
		return nil
	})
	return f, rows, err
}

// fileStatus gộp trạng thái lô: còn lô chưa gửi → incomplete; còn lô chờ → queued; có lô dead_lettered →
// dead_lettered; còn lại completed.
func fileStatus(bs []FileBatch) string {
	st := StatusCompleted
	for _, b := range bs {
		switch b.Status {
		case FileIncomplete:
			return FileIncomplete
		case StatusQueued:
			st = StatusQueued
		case StatusDeadLettered:
			if st != StatusQueued {
				st = StatusDeadLettered
			}
		}
	}
	return st
}
