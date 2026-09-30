// Package app: use case của mkg/orders. Giao dịch mở ở đây bằng tenant.WithTenantTx; kiểm vai ở đây
// (R-55), kể cả chặn tự phê duyệt (R-56 — CHECK DB là lớp hai).
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/orders/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Vai.
const (
	RoleOfficer  = port.RoleSalesOfficer
	RoleApprover = port.RoleSalesApprover
)

// Producer là tên nguồn phát trên phong bì sự kiện.
const Producer = "mkg/orders"

// ConsumerActor là actor_ref của consumer sự kiện nội bộ trong sổ orders (không phải người).
var ConsumerActor = uuid.MustParse("00000000-0000-0000-0000-00000000de01")

// Mã lỗi của module (docs/errors.md, mục mkg/orders).
var (
	ErrContractNotFound   = problem.New(404, "orders.contract_not_found", "Không tìm thấy hợp đồng mua rơm")
	ErrNoContract         = problem.New(404, "orders.no_contract", "HTX không có hợp đồng mua rơm hiệu lực tại ngày này — không đoán giá")
	ErrPONotFound         = problem.New(404, "orders.po_not_found", "Không tìm thấy đơn mua")
	ErrReceiptNotFound    = problem.New(404, "orders.receipt_not_found", "Không tìm thấy phiếu nhận hàng")
	ErrOrderNotFound      = problem.New(404, "orders.sales_order_not_found", "Không tìm thấy đơn bán")
	ErrDeliveryNotFound   = problem.New(404, "orders.delivery_not_found", "Không tìm thấy giao nhận")
	ErrParamNotFound      = problem.New(404, "orders.param_not_found", "Không có khóa / phiên bản tham số này")
	ErrContractOverlap    = problem.New(409, "orders.contract_overlap", "HTX đã có hợp đồng hiệu lực chồng thời gian này (proto CT-01)")
	ErrContractTransition = problem.New(409, "orders.contract_transition", "Hợp đồng không ở trạng thái cho phép thao tác này")
	ErrPOTransition       = problem.New(409, "orders.po_transition", "Đơn mua không ở trạng thái cho phép thao tác này")
	ErrPONotApproved      = problem.New(409, "orders.po_not_approved", "Chỉ lập phiếu nhận hàng cho đơn mua đã duyệt")
	ErrReceiptTransition  = problem.New(409, "orders.receipt_transition", "Phiếu nhận hàng đã được quyết định")
	ErrReceiptNotWeighed  = problem.New(409, "orders.receipt_not_weighed", "Phiếu nhận hàng chưa có số nhận (chờ phiếu cân HUB_IN)")
	ErrOrderTransition    = problem.New(409, "orders.order_transition", "Đơn bán chỉ chuyển trạng thái tiến (proto advanceSalesOrder)")
	ErrDeliveryTransition = problem.New(409, "orders.delivery_transition", "Giao nhận không ở trạng thái cho phép thao tác này")
	ErrCustomerInMissing  = problem.New(409, "orders.customer_in_missing", "Chưa có phiếu cân CUSTOMER_IN của lô — biên bản giao nhận cần khối lượng measurement")
	ErrParamDecided       = problem.New(409, "orders.param_already_decided", "Phiên bản tham số đã được duyệt hoặc từ chối")
	ErrPriceApproval      = problem.New(403, "orders.price_approval_required", "Giá / chiết khấu vượt ngưỡng — cần sales_approver khác người lập xác nhận đơn")
	ErrBasisMismatch      = problem.New(422, "orders.basis_mismatch", "Đơn vị đơn mua khác cách tính giá của hợp đồng (per_tonne ↔ kg, per_bale ↔ bale)")
	ErrPriceRequired      = problem.New(422, "orders.price_required", "Không có hợp đồng hiệu lực và không nhập đơn giá — không đoán giá")
	ErrTicketUnknown      = problem.New(422, "orders.ticket_unknown", "Phiếu cân không có (chưa nhận từ measurement) hoặc không thuộc lô / điểm cân này")
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai orders_app (cụm mkg)
	Clock clock.Clock
}

var (
	_ port.Contracts  = (*Service)(nil)
	_ port.Deliveries = (*Service)(nil)
)

// mapErr quy lỗi domain về problem.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Msg)
	}
	return err
}

// write chạy fn trong giao dịch tenant; replay được thử trước (gọi lặp) và sau ledger.ErrDuplicate
// (hai request cùng khóa song song).
func (s *Service) write(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error, replay func(pgx.Tx) (bool, error)) (replayed bool, err error) {
	err = tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		ok, err := replay(tx)
		if err != nil || ok {
			replayed = ok
			return err
		}
		return fn(tx)
	})
	if errors.Is(ledger.Classify(err), ledger.ErrDuplicate) {
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
	p, err := authz.Require(ctx, RoleOfficer, RoleApprover)
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

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullDec(d *decimal.Decimal) decimal.NullDecimal {
	if d == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *d, Valid: true}
}
