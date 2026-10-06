// Package app: use case của mkg/costing. Giao dịch mở ở đây (tenant.WithTenantTx); kiểm vai ở đây
// (R-55), kể cả chặn tự phê duyệt (R-56 — DB chặn lần hai bằng CHECK). Consumer sự kiện nội bộ
// chạy trong giao dịch inbox được giao (R-61).
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/store"
	hubport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	logport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/logistics/port"
	ordport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer là tên nguồn phát trên phong bì sự kiện.
const Producer = "mkg/costing"

// Mã lỗi của module (docs/errors.md, mục mkg/costing).
var (
	ErrPeriodNotFound       = problem.New(404, "costing.period_not_found", "Không có kỳ giá thành")
	ErrPeriodNotOpen        = problem.New(409, "costing.period_not_open", "Kỳ không mở: kỳ đang đóng / đã đóng không nhận bút toán và không mở lại")
	ErrPeriodNotEnded       = problem.New(409, "costing.period_not_ended", "Kỳ chưa hết tháng — chưa đóng được")
	ErrPeriodNotClosed      = problem.New(409, "costing.period_not_closed", "Kỳ chưa đóng")
	ErrJobNotFound          = problem.New(404, "costing.job_not_found", "Không có job đóng kỳ")
	ErrEntryNotFound        = problem.New(404, "costing.entry_not_found", "Không có bút toán giá thành")
	ErrEntrySuperseded      = problem.New(409, "costing.entry_superseded", "Bút toán đã bị bút toán khác thay — điều chỉnh bút toán hiệu lực")
	ErrAllocationNotFound   = problem.New(404, "costing.allocation_not_found", "Không có kết quả phân bổ")
	ErrAllocationSuperseded = problem.New(409, "costing.allocation_superseded", "Kết quả phân bổ đã bị tính lại — tính lại kết quả hiệu lực")
	ErrLotNotFound          = problem.New(404, "costing.lot_not_found", "Lô chưa có bút toán giá thành")
	ErrConversionNotFound   = problem.New(404, "costing.conversion_not_found", "Lệnh gom chưa có quy đổi cuộn → tấn (thiếu số cuộn hoặc phiếu cân)")
	ErrSettlementNotFound   = problem.New(404, "costing.settlement_not_found", "Không có quyết toán HTX")
	ErrSettlementExists     = problem.New(409, "costing.settlement_exists", "Lệnh gom đã có quyết toán (proto PM-01: một lệnh gom một quyết toán)")
	ErrSettlementNotReady   = problem.New(409, "costing.settlement_not_ready", "Quyết toán chưa duyệt được: chưa có giá (PM-03), chờ cân đủ (PM-02) hoặc số tiền ≤ 0")
	ErrSettlementFrozen     = problem.New(409, "costing.settlement_frozen", "Quyết toán đã duyệt là khoản phải trả — chỉ hoàn bằng quyết toán điều chỉnh (bút toán đảo)")
	ErrSettlementPaid       = problem.New(409, "costing.settlement_paid", "Quyết toán đã trả không hủy được — lập quyết toán điều chỉnh")
	ErrSettlementTransition = problem.New(409, "costing.settlement_transition", "Thao tác không hợp lệ ở trạng thái hiện tại của quyết toán")
	ErrContractsUnavailable = problem.New(503, "costing.contracts_unavailable", "Chưa đọc được hợp đồng mua rơm (mkg/orders) — thử lại")
	ErrParamNotFound        = problem.New(404, "costing.param_not_found", "Không có khóa tham số giá thành")
	ErrParamVersionNotFound = problem.New(404, "costing.param_version_not_found", "Không có phiên bản tham số")
	ErrParamPending         = problem.New(409, "costing.param_pending_exists", "Khóa tham số đang có phiên bản chờ duyệt")
	ErrParamNotPending      = problem.New(409, "costing.param_not_pending", "Phiên bản tham số không còn chờ duyệt")
	ErrCarbonSplit          = problem.New(422, "costing.carbon_split_invalid", "Tỷ lệ chia carbon (nông hộ + cơ quan + VVB) đang áp dụng không bằng 100")
	ErrAmountOutOfRange     = problem.New(422, "costing.amount_out_of_range",
		"Số tiền / khối lượng tính ra vượt giới hạn lưu trữ (numeric(18,2)) — kiểm tra đơn giá, số cuộn, khối lượng, khấu trừ")
)

// Deps là cổng đọc module mkg khác (cùng tiến trình, pool vai của module chủ dữ liệu). Nil = chưa nối.
type Deps struct {
	// Origins: nguồn gốc lô + tỷ lệ (hub/port.LotOrigins) để quy kết hao hụt. Nil → quy kết về chính lô.
	Origins hubport.LotOrigins
	// Contracts: hợp đồng mua rơm (orders/port.Contracts) cho quyết toán. Nil → lập quyết toán trả 503.
	Contracts ordport.Contracts
	// Trips: chuyến (logistics/port.Trips) — ghe chưa cân của lệnh gom (PM-02) và đọc lại khi đóng kỳ.
	Trips logport.Trips
	// Deliveries: giao nhận (orders/port.Deliveries) — đọc lại khi đóng kỳ.
	Deliveries ordport.Deliveries
}

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai costing_app (cụm mkg)
	Clock clock.Clock
	Deps  Deps
}

var readRoles = []string{port.RoleCostAccountant, port.RolePeriodCloser, port.RoleChiefAccountant}

func (s *Service) now() time.Time { return s.Clock.Now().UTC() }

// mapErr quy lỗi domain / port / PostgreSQL về problem.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var p *problem.Problem
	if errors.As(err, &p) {
		return err
	}
	var fe domain.FieldError
	switch {
	case errors.As(err, &fe):
		return problem.Validation.WithField(fe.Field, fe.Msg)
	case errors.Is(err, domain.ErrInvalidPeriodCode):
		return problem.Validation.WithField("code", "YYYY-MM")
	case errors.Is(err, domain.ErrCarbonSplit):
		return ErrCarbonSplit
	case errors.Is(err, domain.ErrOutOfRange):
		return ErrAmountOutOfRange.WithCause(err)
	case errors.Is(err, authz.ErrSelfApproval):
		return problem.SelfApproval.WithCause(err)
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		switch {
		case pg.ConstraintName == "costing_period_not_open":
			return ErrPeriodNotOpen.WithDetail(pg.Message)
		case pg.ConstraintName == "close_job_closer_not_adjuster" || pg.ConstraintName == "close_job_check":
			return problem.SelfApproval.WithDetail("người đóng kỳ là người lập bút toán điều chỉnh của kỳ (R-56)")
		case pg.Code == "23505" && pg.ConstraintName == "settlement_current_one_per_order":
			return ErrSettlementExists
		case pg.Code == "23505" && pg.ConstraintName == "close_job_one_live":
			return ErrPeriodNotOpen.WithDetail("kỳ đã có job đóng kỳ")
		}
	}
	return err
}

// write chạy fn trong giao dịch tenant; replay trả true nếu idem_key đã có bản ghi. Trùng khóa do
// ghi song song → chạy replay trong giao dịch mới (R-51, R-61).
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
	return replayed, mapErr(err)
}

func (s *Service) read(ctx context.Context, fn func(pgx.Tx, authz.Principal) error) error {
	p, err := authz.Require(ctx, readRoles...)
	if err != nil {
		return err
	}
	return mapErr(tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error { return fn(tx, p) }))
}

func requireIdem(key string) error {
	if key == "" {
		return problem.IdempotencyRequired
	}
	return nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ---------------------------------------------------------------------------
// Kỳ: mở theo nhu cầu, chọn kỳ nhận bút toán
// ---------------------------------------------------------------------------

// ensurePeriod trả kỳ của mã; chưa có → mở (tác nhân hệ thống, sổ period_event 'opened').
func (s *Service) ensurePeriod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, code string, actor uuid.UUID) (store.Period, error) {
	if err := store.LockKey(ctx, tx, "period:"+code); err != nil {
		return store.Period{}, err
	}
	p, err := store.PeriodByCode(ctx, tx, code, false)
	if err != nil || p != nil {
		if p == nil {
			return store.Period{}, err
		}
		return *p, err
	}
	from, to, err := domain.PeriodBounds(code)
	if err != nil {
		return store.Period{}, err
	}
	id := ids.New()
	e := store.PeriodEvent{ID: ids.New(), PeriodID: id, Code: code, Action: "opened", From: from, To: to, EventTime: s.now(), ActorRef: actor,
		IdemKey: "period-open:" + code}
	if err := store.InsertPeriodEvent(ctx, tx, tid, &e); err != nil {
		return store.Period{}, err
	}
	np, err := store.PeriodByCode(ctx, tx, code, false)
	if err != nil || np == nil {
		return store.Period{}, fmt.Errorf("costing: không đọc lại được kỳ %s: %w", code, err)
	}
	return *np, nil
}

// targetPeriod chọn kỳ nhận bút toán của một sự việc: kỳ tháng của event_time nếu còn mở; kỳ đó
// đã đóng/đang đóng → kỳ mở kế tiếp, không sớm hơn tháng hiện tại (lateFrom = kỳ của sự việc).
func (s *Service) targetPeriod(ctx context.Context, tx pgx.Tx, tid uuid.UUID, at time.Time) (store.Period, *string, error) {
	code := domain.PeriodCode(at)
	p, err := s.ensurePeriod(ctx, tx, tid, code, domain.SystemActorRef)
	if err != nil || p.Status == port.PeriodOpen {
		return p, nil, err
	}
	late := code
	next := domain.PeriodCode(s.now())
	for range 36 {
		if next <= code {
			if next, err = domain.NextPeriodCode(code); err != nil {
				return store.Period{}, nil, err
			}
		}
		p, err = s.ensurePeriod(ctx, tx, tid, next, domain.SystemActorRef)
		if err != nil || p.Status == port.PeriodOpen {
			return p, &late, err
		}
		code = next
	}
	return store.Period{}, nil, ErrPeriodNotOpen
}

// paramAt đọc tham số theo thời điểm sự việc (asOf nil = mọi quyết định đã ghi).
func paramAt(ctx context.Context, tx pgx.Tx, key string, eventTime time.Time, asOf *time.Time) (domain.ParamValue, error) {
	bound := time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
	if asOf != nil {
		bound = *asOf
	}
	return store.ParamAt(ctx, tx, key, eventTime, bound)
}
