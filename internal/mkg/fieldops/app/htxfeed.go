package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// PlatformSyncActor là người ghi của xác nhận kênh platform (job kéo luồng — hệ thống).
// Người phản hồi thật (quản lý HTX) ở sổ pf_cropcycle.collection_response; MKG giữ id phản
// hồi đó ở evidence_ref để truy vết, không nhận định danh người dùng nền tảng.
var PlatformSyncActor = uuid.MustParse("00000000-0000-7000-8000-00000000f5c1")

// Số trang tối đa mỗi lượt và cỡ trang của job kéo phản hồi.
const (
	feedPageSize = 200
	feedMaxPages = 50
)

// FeedResult là kết quả một lượt kéo phản hồi của HTX.
type FeedResult struct {
	Recorded  int   // ghi thành htx_confirmation mới
	Duplicate int   // đã có (phát lại / lượt trước)
	Skipped   int   // bảng kê không có ở MKG, revision / số cuộn đã cũ, sai HTX/thửa hoặc sai quy tắc
	Parked    int   // lỗi vĩnh viễn khi ghi: gác vào htx_feed_parked, con trỏ vẫn tiến (cảnh báo vận hành)
	Cursor    int64 // con trỏ sau lượt
	// LastParked: mã lỗi của mục bị gác cuối cùng trong lượt (rỗng = không có) — cho dòng log của job.
	LastParked string
}

// feedState giữ con trỏ luồng trong tiến trình (dựng lại từ sổ khi khởi động).
type feedState struct {
	mu     sync.Mutex
	loaded map[uuid.UUID]bool
	cursor map[uuid.UUID]int64
}

// SyncHTXResponses kéo phản hồi của HTX từ nền tảng (Application API công khai, tài khoản
// dịch vụ tenant MKG) và ghi thành htx_confirmation kênh platform, cập nhật đối soát chiều mua
// (RB-6). Idempotent: khóa chống trùng = (con trỏ, id phản hồi). Phản hồi theo revision cũ hơn
// bảng kê hiện hành của MKG bị bỏ qua (HTX sẽ phản hồi lại theo bản mới). Chỉ job leader gọi.
func (s *Service) SyncHTXResponses(ctx context.Context, tid uuid.UUID) (FeedResult, error) {
	var res FeedResult
	if s.Feed == nil {
		return res, nil
	}
	s.feed.mu.Lock()
	defer s.feed.mu.Unlock()
	if s.feed.loaded == nil {
		s.feed.loaded, s.feed.cursor = map[uuid.UUID]bool{}, map[uuid.UUID]int64{}
	}
	if !s.feed.loaded[tid] {
		var c int64
		if err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			var e error
			c, e = store.LastPlatformCursor(ctx, tx)
			return e
		}); err != nil {
			return res, err
		}
		s.feed.cursor[tid], s.feed.loaded[tid] = c, true
	}
	res.Cursor = s.feed.cursor[tid]
	for range feedMaxPages {
		pg, err := s.Feed.HTXResponses(ctx, res.Cursor, feedPageSize)
		if err != nil {
			return res, mapErr(err)
		}
		if len(pg.Items) == 0 {
			break
		}
		for _, it := range pg.Items {
			outcome, err := s.applyHTXResponse(ctx, tid, it)
			if err != nil {
				// Lỗi vĩnh viễn (dữ liệu / ràng buộc): gác mục kèm lỗi rồi đi tiếp — không để một mục hỏng chặn
				// luồng mãi (job 5 phút lặp lại cùng mục). Lỗi tạm thời (DB, mạng): dừng lượt, con trỏ giữ nguyên.
				code, permanent := classifyFeedErr(err)
				if !permanent {
					return res, err
				}
				if perr := s.parkHTXResponse(ctx, tid, it, code, err); perr != nil {
					return res, perr
				}
				s.metrics.parked(ctx, code)
				res.LastParked = code
				outcome = feedParked
			}
			switch outcome {
			case feedRecorded:
				res.Recorded++
			case feedDuplicate:
				res.Duplicate++
			case feedParked:
				res.Parked++
			default:
				res.Skipped++
			}
			if it.Cursor > res.Cursor {
				res.Cursor = it.Cursor
				s.feed.cursor[tid] = it.Cursor
			}
		}
		if len(pg.Items) < feedPageSize {
			break
		}
	}
	return res, nil
}

type feedOutcome int

const (
	feedRecorded feedOutcome = iota + 1
	feedDuplicate
	feedSkipped
	feedParked
)

// classifyFeedErr phân loại lỗi khi ghi một phản hồi: permanent = ghi lại bao nhiêu lần cũng hỏng như nhau
// (lỗi dữ liệu / ràng buộc của PostgreSQL lớp 22, 23 trừ 23505; lỗi nghiệp vụ 4xx; lỗi trường của domain). Còn
// lại (mất kết nối, hết thời gian, khóa chết, trùng khóa do ghi song song, thiếu quyền do cấu hình…) là tạm
// thời — job thử lại lượt sau (trùng idem_key thật đã là Duplicate ở applyHTXResponse).
func classifyFeedErr(err error) (code string, permanent bool) {
	var pe *pgconn.PgError
	var pr *problem.Problem
	var fe domain.FieldError
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "", false
	case errors.As(err, &pe):
		return "sqlstate." + pe.Code, strings.HasPrefix(pe.Code, "22") || (strings.HasPrefix(pe.Code, "23") && pe.Code != "23505")
	case errors.As(err, &pr):
		return pr.Code, pr.Status >= 400 && pr.Status < 500
	case errors.As(err, &fe):
		return "validation." + fe.Field, true
	}
	return "", false
}

// parkHTXResponse gác phản hồi lỗi vĩnh viễn vào htx_feed_parked (giao dịch riêng — giao dịch ghi đã hủy).
func (s *Service) parkHTXResponse(ctx context.Context, tid uuid.UUID, it port.HTXResponse, code string, cause error) error {
	clean := it
	if it.Reason != nil {
		r := sanitizeText(*it.Reason, 2000)
		clean.Reason = &r
	}
	clean.Outcome = sanitizeText(it.Outcome, 100)
	payload, err := json.Marshal(clean)
	if err != nil {
		return err
	}
	p := store.ParkedResponse{Cursor: it.Cursor, ResponseID: it.ResponseID, StatementRef: it.StatementRef,
		Code: sanitizeText(code, 100), Detail: sanitizeText(cause.Error(), 1000), Payload: payload, ParkedAt: s.Clock.Now()}
	return tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error { return store.ParkResponse(ctx, tx, tid, p) })
}

// sanitizeText bỏ ký tự NUL / UTF-8 hỏng (PostgreSQL text và jsonb từ chối) và cắt còn tối đa n ký tự.
func sanitizeText(v string, n int) string {
	v = strings.ReplaceAll(strings.ToValidUTF8(v, "\uFFFD"), "\x00", "")
	if r := []rune(v); len(r) > n {
		v = string(r[:n])
	}
	return v
}

// MetricFeedParked: phản hồi HTX bị gác vì lỗi vĩnh viễn, theo code (> 0 → cảnh báo vận hành xử lý tay).
const MetricFeedParked = "ag.fieldops.htx_feed.parked"

// feedMetrics đếm mục bị gác (OTLP khi telemetry bật; bản sao trong tiến trình cho kiểm thử).
type feedMetrics struct {
	once    sync.Once
	counter metric.Int64Counter
	mu      sync.Mutex
	total   int64
}

func (m *feedMetrics) parked(ctx context.Context, code string) {
	m.mu.Lock()
	m.total++
	m.mu.Unlock()
	if !telemetry.Enabled() {
		return
	}
	m.once.Do(func() {
		m.counter, _ = telemetry.Meter().Int64Counter(MetricFeedParked,
			metric.WithDescription("Phản hồi HTX bị gác vì lỗi vĩnh viễn — cần vận hành xử lý"))
	})
	if m.counter != nil {
		m.counter.Add(ctx, 1, metric.WithAttributes(attribute.String("code", code)))
	}
}

// FeedParkedTotal trả số phản hồi HTX đã gác trong tiến trình (kiểm thử, chẩn đoán).
func (s *Service) FeedParkedTotal() int64 {
	s.metrics.mu.Lock()
	defer s.metrics.mu.Unlock()
	return s.metrics.total
}

func (s *Service) applyHTXResponse(ctx context.Context, tid uuid.UUID, it port.HTXResponse) (feedOutcome, error) {
	key := store.PlatformIdemKey(it.Cursor, it.ResponseID)
	out := feedSkipped
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		if c, err := store.ConfirmationByIdem(ctx, tx, key); err != nil || c != nil {
			if c != nil {
				out = feedDuplicate
			}
			return err
		}
		r, err := store.ReconByStatement(ctx, tx, it.StatementRef)
		if err != nil || r == nil {
			return err // bảng kê không do tenant này gửi: bỏ qua
		}
		if r.HTXRef != it.HTXRef || r.PlotRef != it.PlotRef || r.StatementRevision != it.StatementRevision || r.MKGRolls != it.MKGRolls {
			return nil // đã có bản sửa sau (hoặc dữ liệu lệch): chờ HTX phản hồi bản hiện hành
		}
		var reason *string
		if it.Reason != nil && strings.TrimSpace(*it.Reason) != "" {
			rs, err := domain.CheckReason("reason", *it.Reason, false)
			if err != nil {
				return nil
			}
			reason = &rs
		}
		switch it.Outcome {
		case "confirmed":
			if it.HTXRolls != r.MKGRolls {
				return nil
			}
		case "disputed":
			if reason == nil || it.HTXRolls == r.MKGRolls || it.HTXRolls < 0 {
				return nil
			}
		default:
			return nil
		}
		rid := it.ResponseID
		c := store.Confirmation{
			ID: ids.New(), TenantID: tid, OrderID: r.OrderID, PlotRef: r.PlotRef, HTXRef: r.HTXRef, StatementRef: r.StatementRef,
			StatementRevision: r.StatementRevision, RollCountID: r.MKGRollCountID, MKGRolls: r.MKGRolls, Outcome: it.Outcome,
			HTXRolls: it.HTXRolls, Reason: reason, Channel: ChannelPlatform, EvidenceRef: &rid, EventTime: it.RespondedAt.UTC(),
			ActorRef: PlatformSyncActor, IdemKey: key,
		}
		if err := insert(store.InsertConfirmation(ctx, tx, &c)); err != nil {
			return err
		}
		htx, o, rev := c.HTXRolls, c.Outcome, c.StatementRevision
		r.HTXRolls, r.Outcome, r.ConfirmationID, r.ConfirmedRevision = &htx, &o, &c.ID, &rev
		r.Status = domain.ReconStatus(r.MKGRolls, r.HTXRolls)
		if err := store.PutRecon(ctx, tx, tid, *r); err != nil {
			return err
		}
		out = feedRecorded
		return nil
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		return feedDuplicate, nil
	}
	return out, err
}
