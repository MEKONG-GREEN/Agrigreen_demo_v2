// Package app: use case của mkg/fieldops. Giao dịch mở ở đây bằng
// tenant.WithTenantTx; kiểm vai ở đây (R-55), kể cả chặn tự phê duyệt (R-56).
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Vai.
const (
	RoleCollector  = port.RoleFieldCollector
	RoleSupervisor = port.RoleFieldSupervisor
)

// Producer là tên nguồn phát trên phong bì sự kiện.
const Producer = "mkg/fieldops"

// Mã lỗi của module (docs/errors.md, mục mkg/fieldops).
var (
	ErrOrderNotFound       = problem.New(404, "fieldops.order_not_found", "Không tìm thấy lệnh gom")
	ErrRollCountNotFound   = problem.New(404, "fieldops.roll_count_not_found", "Không tìm thấy bản ghi đếm cuộn")
	ErrOrderTransition     = problem.New(409, "fieldops.order_transition", "Lệnh gom không ở trạng thái cho phép thao tác này")
	ErrOrderNotOpen        = problem.New(409, "fieldops.order_not_open", "Chỉ ghi số trên lệnh gom đã duyệt, chưa đóng/hủy")
	ErrStageAlreadyCounted = problem.New(409, "fieldops.stage_already_counted", "Công đoạn này của thửa đã có số — sửa bằng bản ghi bù")
	ErrAlreadyCorrected    = problem.New(409, "fieldops.already_corrected", "Bản ghi đã được bù; bù bản ghi hiệu lực mới nhất")
	ErrIDReused            = problem.New(409, "fieldops.id_reused", "id đã dùng cho bản ghi khác")
	ErrNothingToConfirm    = problem.New(409, "fieldops.nothing_to_confirm", "Thửa chưa có số xếp ghe/xe nên chưa có bảng kê để HTX xác nhận")
	ErrStatementStale      = problem.New(409, "fieldops.statement_stale", "Bảng kê đã có phiên bản mới hơn — xác nhận theo phiên bản hiện hành")
	ErrPlotNotInOrder      = problem.New(422, "fieldops.plot_not_in_order", "Thửa không thuộc lệnh gom")
	ErrPreviousMissing     = problem.New(422, "fieldops.previous_stage_missing", "Công đoạn trước chưa có số (proto FM-03)")
	ErrStageExceeds        = problem.New(422, "fieldops.stage_exceeds_previous", "Số cuộn nhiều hơn công đoạn trước — đếm lại (proto FM-04)")
	ErrDiscrepancy         = problem.New(422, "fieldops.discrepancy_reason_required", "Số cuộn ít hơn công đoạn trước — bắt buộc lý do chênh (proto FM-04)")
	ErrUnitMismatch        = problem.New(422, "fieldops.unit_mismatch", "Công đoạn trước đếm cuộn thì công đoạn sau cũng đếm cuộn (rơm rời cũng vậy)")
	ErrNextStage           = problem.New(422, "fieldops.next_stage_conflict", "Giá trị sửa làm công đoạn sau vi phạm FM-04 — sửa công đoạn sau trước")
	ErrHTXNotFound         = problem.New(422, "fieldops.htx_not_found", "Nền tảng không có (hoặc chưa chia sẻ) HTX này")
	ErrPlotNotFound        = problem.New(422, "fieldops.plot_not_found", "Nền tảng không có (hoặc chưa chia sẻ) thửa này")
	ErrPlotOtherHTX        = problem.New(422, "fieldops.plot_other_htx", "Thửa không thuộc HTX của lệnh gom hoặc đã ngừng dùng")
	ErrDeviceMismatch      = problem.New(403, "fieldops.device_mismatch", "device_id của lô đồng bộ khác header X-Device-Id")
	ErrCursorInvalid       = problem.New(400, "fieldops.cursor_invalid", "Con trỏ đồng bộ không hợp lệ — pull lại từ đầu")
	ErrDirectory           = problem.New(503, "fieldops.directory_unavailable", "Không gọi được Application API của nền tảng — thử lại sau")
	ErrValveDenied         = problem.New(500, "fieldops.valve_denied", "Van công bố từ chối payload")
)

// Service là use case của module.
type Service struct {
	DB        tenant.Beginner // pool vai fieldops_app (cụm mkg)
	Clock     clock.Clock
	Directory port.PlatformDirectory
	// Weighed: khối lượng cân của lô gom (mkg/measurement). nil = chưa nối → không học kg/cuộn.
	Weighed port.WeighedMass
	// ConfiguredKgPerRoll: kg/cuộn cấu hình (bậc 3 của chuỗi dự phòng); nil = không có. Tạm, chờ khách.
	ConfiguredKgPerRoll *decimal.Decimal
	// CursorKey: khóa HMAC của con trỏ pull.
	CursorKey []byte
	// Feed: luồng phản hồi của HTX trên nền tảng (G3-2); nil = không kéo.
	Feed port.HTXResponseFeed
	// Harvest: ngày gặt theo vụ của thửa (bề mặt partner của nền tảng, FM-01/02); nil = chưa
	// nối → mọi kiểm ngày gặt gắn cờ unavailable (vẫn cho ghi, không bao giờ im lặng).
	Harvest port.HarvestCalendar

	feed feedState
}

var _ port.RollTotals = (*Service)(nil)

// mapErr quy lỗi domain / port về problem.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var fe domain.FieldError
	switch {
	case errors.As(err, &fe):
		return problem.Validation.WithField(fe.Field, fe.Msg)
	case errors.Is(err, domain.ErrPreviousStageMissing):
		return ErrPreviousMissing
	case errors.Is(err, domain.ErrStageExceedsPrevious):
		return ErrStageExceeds
	case errors.Is(err, domain.ErrDiscrepancyNoReason):
		return ErrDiscrepancy
	case errors.Is(err, domain.ErrUnitMismatch):
		return ErrUnitMismatch
	case errors.Is(err, domain.ErrNextStageExceeds), errors.Is(err, domain.ErrNextStageUnexplained):
		return ErrNextStage.WithDetail(err.Error())
	case errors.Is(err, domain.ErrOrderTransition):
		return ErrOrderTransition
	case errors.Is(err, port.ErrDirectoryUnavailable):
		return ErrDirectory
	}
	return err
}

// write chạy fn trong giao dịch tenant; replay được thử trước (gọi lặp) và sau
// ledger.ErrDuplicate (hai request cùng khóa song song).
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

func (s *Service) read(ctx context.Context, fn func(pgx.Tx) error) error {
	p, err := authz.Require(ctx, RoleCollector, RoleSupervisor)
	if err != nil {
		return err
	}
	return mapErr(tenant.WithTenantTx(ctx, s.DB, p.TenantID, fn))
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
