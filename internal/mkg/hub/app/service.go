// Package app: use case của mkg/hub. Giao dịch mở ở đây bằng tenant.WithTenantTx;
// kiểm vai ở đây (R-55), kể cả chặn tự phê duyệt (R-56).
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/hub/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ledger"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Vai — khai ở port để lớp ngoài không phải import app.
const (
	RoleHubOperator = port.RoleHubOperator
	RoleHubApprover = port.RoleHubApprover
)

// Producer là tên nguồn phát trên phong bì sự kiện.
const Producer = "mkg/hub"

// Mã lỗi của module (docs/errors.md, mục mkg/hub).
var (
	ErrFacilityNotFound   = problem.New(404, "hub.facility_not_found", "Không tìm thấy cơ sở")
	ErrVersionNotFound    = problem.New(404, "hub.version_not_found", "Không tìm thấy phiên bản dữ liệu chủ")
	ErrLotNotFound        = problem.New(404, "hub.lot_not_found", "Không tìm thấy lô")
	ErrMovementNotFound   = problem.New(404, "hub.movement_not_found", "Không tìm thấy dịch chuyển tồn")
	ErrCodeTaken          = problem.New(409, "hub.code_taken", "Mã cơ sở đã được dùng")
	ErrPendingExists      = problem.New(409, "hub.pending_version_exists", "Cơ sở đang có phiên bản chờ duyệt")
	ErrNotPending         = problem.New(409, "hub.version_not_pending", "Phiên bản không còn chờ duyệt")
	ErrFacilityInactive   = problem.New(409, "hub.facility_inactive", "Cơ sở đã ngừng dùng")
	ErrInsufficientStock  = problem.New(409, "hub.insufficient_stock", "Không đủ tồn theo lô")
	ErrAlreadyCorrected   = problem.New(409, "hub.already_corrected", "Dịch chuyển này đã được bù")
	ErrIDReused           = problem.New(409, "hub.id_reused", "id đã dùng cho bản ghi khác")
	ErrNegativeLoss       = problem.New(422, "hub.negative_loss", "Khối lượng ra lớn hơn khối lượng vào")
	ErrLotOtherFacility   = problem.New(422, "hub.lot_other_facility", "Lô không thuộc cơ sở của thao tác")
	ErrCorrectionKind     = problem.New(422, "hub.correction_unsupported", "Loại dịch chuyển này không bù trực tiếp được")
	ErrRatioInvalid       = problem.New(422, "hub.ratio_invalid", "Tỷ lệ nguồn không hợp lệ")
	ErrParentInvalid      = problem.New(422, "hub.parent_invalid", "Cơ sở cha phải là hub hiện hành khác chính nó")
	ErrBoundaryInvalid    = problem.New(422, "hub.boundary_invalid", "Ranh giới không phải MultiPolygon hợp lệ")
	ErrDuplicateLotInput  = problem.New(422, "hub.duplicate_lot_input", "Một lô xuất hiện hai lần trong thao tác")
	ErrAllocationMismatch = problem.New(422, "hub.allocation_mismatch", "Tổng phân bổ theo lô phải bằng lượng xuất")
	ErrValveDenied        = problem.New(500, "hub.valve_denied", "Van công bố từ chối payload")
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai hub_app (cụm mkg)
	Clock clock.Clock
}

var _ port.Lots = (*Service)(nil)

// mapErr quy lỗi domain / PostgreSQL về problem.
func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var fe domain.FieldError
	switch {
	case errors.As(err, &fe):
		return problem.Validation.WithField(fe.Field, fe.Msg)
	case errors.Is(err, domain.ErrInsufficientStock):
		return ErrInsufficientStock
	case errors.Is(err, domain.ErrNegativeLoss):
		return ErrNegativeLoss
	case errors.Is(err, domain.ErrLotOtherFacility):
		return ErrLotOtherFacility
	case errors.Is(err, domain.ErrCorrectionKind):
		return ErrCorrectionKind
	case errors.Is(err, domain.ErrFacilityInactive):
		return ErrFacilityInactive
	case errors.Is(err, domain.ErrDuplicateLotInput):
		return ErrDuplicateLotInput
	case errors.Is(err, domain.ErrRatioSum), errors.Is(err, domain.ErrRatioPrecision), errors.Is(err, domain.ErrShareTooSmall),
		errors.Is(err, domain.ErrMixedSourceWeights):
		return ErrRatioInvalid.WithDetail(err.Error())
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "facility_current_tenant_id_code_key" {
		return ErrCodeTaken
	}
	return err
}

// write chạy fn trong giao dịch tenant; gặp ledger.ErrDuplicate (hai request
// cùng khóa chạy song song) thì chạy replay trong giao dịch mới để trả bản ghi
// của bên thắng.
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
	p, err := authz.Require(ctx, RoleHubOperator, RoleHubApprover)
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
