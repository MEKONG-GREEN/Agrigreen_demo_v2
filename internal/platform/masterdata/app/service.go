// Package app: use case của pf/masterdata. Giao dịch mở ở đây; kiểm vai ở đây.
package app

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Producer là tên nguồn phát sự kiện.
const Producer = "pf/masterdata"

// Mã lỗi của module (docs/errors.md).
var (
	ErrUnitNotFound = problem.New(http.StatusNotFound, "masterdata.admin_unit_not_found", "Không có đơn vị hành chính tại thời điểm tra").
			WithCause(port.ErrUnitNotFound)
	ErrUnitConflict = problem.New(http.StatusConflict, "masterdata.admin_unit_conflict", "Phiên bản đơn vị hành chính đã có với nội dung khác")
	ErrUnitOverlap  = problem.New(http.StatusConflict, "masterdata.admin_unit_overlap", "Hiệu lực chồng lấp với phiên bản khác của cùng mã")
	ErrUnitRef      = problem.New(http.StatusUnprocessableEntity, "masterdata.admin_unit_invalid_reference", "Đơn vị cha hoặc đơn vị kế thừa không tồn tại trong khoảng hiệu lực")

	ErrCatalogNotFound = problem.New(http.StatusNotFound, "masterdata.catalog_not_found", "Không có danh mục này")
	ErrItemNotFound    = problem.New(http.StatusNotFound, "masterdata.catalog_item_not_found", "Không có mục danh mục").
				WithCause(port.ErrCatalogItemNotFound)
	ErrItemExists = problem.New(http.StatusConflict, "masterdata.catalog_item_exists", "Mã đã có trong danh mục")
	ErrItemState  = problem.New(http.StatusConflict, "masterdata.catalog_item_state", "Mục đã ở trạng thái yêu cầu")
	ErrStageRef   = problem.New(http.StatusUnprocessableEntity, "masterdata.farm_stage_invalid", "Khâu sản xuất không có hoặc đã ngừng dùng")

	ErrParamNotFound = problem.New(http.StatusNotFound, "masterdata.param_not_found", "Không có tham số đã phê duyệt có hiệu lực tại thời điểm tra").
				WithCause(port.ErrParamNotFound)
	ErrVersionNotFound    = problem.New(http.StatusNotFound, "masterdata.param_version_not_found", "Không có phiên bản tham số")
	ErrTransition         = problem.New(http.StatusConflict, "masterdata.param_invalid_transition", "Trạng thái phiên bản không cho phép thao tác này")
	ErrNotSetter          = problem.New(http.StatusForbidden, "masterdata.param_not_setter", "Chỉ người đặt được gửi duyệt phiên bản của mình")
	ErrCorrectionRequired = problem.New(http.StatusUnprocessableEntity, "masterdata.param_correction_required",
		"Đã có phiên bản được duyệt cùng ngày hiệu lực: phải nêu correction_reason")
)

// Service là use case của module.
type Service struct {
	DB    tenant.Beginner // pool vai masterdata_app
	Clock clock.Clock
}

// Page là một trang kết quả phân trang bằng cursor (R-53).
type Page[T any] struct {
	Items []T
	Next  *string // cursor trang sau; nil = hết
}

func clampLimit(n int) int {
	if n <= 0 || n > 200 {
		return 50
	}
	return n
}

// reader: dữ liệu chủ là dữ liệu tham chiếu chung của tenant — mọi người gọi đã
// xác thực đều được đọc (không lọc theo phạm vi địa bàn).
func reader(ctx context.Context) (authz.Principal, error) {
	p, ok := authz.FromContext(ctx)
	if !ok {
		return p, problem.Unauthenticated
	}
	return p, nil
}

// asProblem quy lỗi kiểm dữ liệu của domain về problem theo trường.
func asProblem(err error) error {
	var fe domain.FieldError
	if errors.As(err, &fe) {
		return problem.Validation.WithField(fe.Field, fe.Err.Error())
	}
	return err
}

func (s *Service) tx(ctx context.Context, tid uuid.UUID, fn func(pgx.Tx) error) error {
	return tenant.WithTenantTx(ctx, s.DB, tid, fn)
}
