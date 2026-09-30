// Package app: use case của mkg/acctbridge. Giao dịch mở ở đây (tenant.WithTenantTx) hoặc do
// inbox.Process giao (worker-internal); kiểm vai ở đây (R-55), kể cả chặn tự phê duyệt (R-56).
package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/acctbridge/port"
	costingport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Vai.
const (
	RoleOperator = port.RoleAccountingBridgeOperator
	RoleChief    = port.RoleChiefAccountant
)

// Producer là tên nguồn phát trên phong bì sự kiện.
const Producer = "mkg/acctbridge"

// SystemActorRef là actor_ref của job hệ thống (thử lại đẩy theo lịch giãn cách).
var SystemActorRef = uuid.MustParse("00000000-0000-7000-8000-0000000acb01")

// Mã lỗi HTTP của module (docs/errors.md, mục mkg/acctbridge).
var (
	ErrJournalNotFound   = problem.New(404, "acctbridge.journal_not_found", "Không tìm thấy bút toán tổng hợp")
	ErrJobNotFound       = problem.New(404, "acctbridge.job_not_found", "Không tìm thấy job")
	ErrMappingNotFound   = problem.New(404, "acctbridge.mapping_not_found", "Không tìm thấy phiên bản ánh xạ tài khoản")
	ErrPeriodUnknown     = problem.New(404, "acctbridge.period_unknown", "Chưa nhận sự kiện đóng kỳ của kỳ này từ mkg/costing")
	ErrNotBuilt          = problem.New(409, "acctbridge.journal_not_built", "Chỉ duyệt bút toán ở trạng thái built")
	ErrNotRetryable      = problem.New(409, "acctbridge.journal_not_retryable", "Chỉ đẩy lại bút toán push_failed (đã vào DLQ)")
	ErrPushedImmutable   = problem.New(409, "acctbridge.journal_pushed_immutable", "Bút toán đã đẩy sang phần mềm kế toán — không sửa; điều chỉnh ở kỳ sau")
	ErrRebuildNoChange   = problem.New(409, "acctbridge.rebuild_no_change", "Bút toán hiện hành đã dựng theo đúng lần đóng kỳ và phiên bản ánh xạ này")
	ErrMappingPending    = problem.New(409, "acctbridge.mapping_pending", "Đang có một phiên bản ánh xạ chờ duyệt")
	ErrMappingDecided    = problem.New(409, "acctbridge.mapping_decided", "Phiên bản ánh xạ đã được duyệt hoặc từ chối")
	ErrFormatUnsupported = problem.New(422, "acctbridge.format_unsupported", "Định dạng xuất phải là csv hoặc xml")
)

// Mã lỗi của job / lần đẩy (cột failure_code, error_code — không phải lỗi HTTP).
const (
	CodeMappingMissing  = "acctbridge.mapping_missing"
	CodePeriodNotClosed = "acctbridge.period_not_closed"
	CodeSourceMismatch  = "acctbridge.source_mismatch"
	CodeEmptyPeriod     = "acctbridge.empty_period"
	CodePushedImmutable = "acctbridge.journal_pushed_immutable"
	CodeNotPushable     = "acctbridge.journal_not_pushable"
	CodeUnavailable     = "acctbridge.adapter_unavailable"
	CodeRejected        = "acctbridge.adapter_rejected"
	CodeRetryExhausted  = "acctbridge.retry_exhausted"
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai acctbridge_app (cụm mkg)
	Clock clock.Clock
	// Periods: kết quả đóng kỳ của mkg/costing (cùng tiến trình, pool vai costing_app). nil = chưa nối →
	// job dựng trả lỗi tạm thời (hàng đợi giao lại, cuối cùng DLQ + cảnh báo R-62).
	Periods costingport.ClosedPeriods
	// Adapter: bộ điều hợp phần mềm kế toán (xuất tệp / giả / chưa cấu hình).
	Adapter port.LedgerAdapter
	// RetryMax: số lần đẩy tối đa khi đích tạm không nhận (mặc định domain.DefaultRetryMax — tạm, chờ khách).
	RetryMax int
	// PushTimeout: hạn mỗi lần gọi bộ điều hợp (mặc định 30 s).
	PushTimeout time.Duration
	Metrics     *Metrics
}

func (s *Service) retryMax() int {
	if s.RetryMax > 0 {
		return s.RetryMax
	}
	return domain.DefaultRetryMax
}

func (s *Service) metrics() *Metrics {
	if s.Metrics == nil {
		return fallbackMetrics
	}
	return s.Metrics
}

var fallbackMetrics = NewMetrics(nil)

// write chạy fn trong giao dịch tenant; replay được thử trước (gọi lặp) và sau ledger.ErrDuplicate.
func (s *Service) write(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error, replay func(pgx.Tx) (bool, error)) (replayed bool, err error) {
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ok, err := replay(tx)
		if err != nil || ok {
			replayed = ok
			return err
		}
		return fn(tx)
	})
	if errors.Is(err, ledger.ErrDuplicate) {
		rerr := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
			ok, e := replay(tx)
			if e == nil && !ok {
				return err
			}
			return e
		})
		if rerr == nil {
			return true, nil
		}
	}
	return replayed, err
}

func (s *Service) read(ctx context.Context, fn func(pgx.Tx) error) error {
	p, err := authz.Require(ctx, RoleOperator, RoleChief)
	if err != nil {
		return err
	}
	return tenant.WithTenantTx(ctx, s.DB, p.TenantID, fn)
}

func requireIdem(key string) error {
	if key == "" {
		return problem.IdempotencyRequired
	}
	return nil
}

// insert quy lỗi ghi sổ (trùng idem_key → ledger.ErrDuplicate).
func insert(err error) error {
	if err != nil {
		return ledger.Classify(err)
	}
	return nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func intPtr(n int) *int { return &n }
