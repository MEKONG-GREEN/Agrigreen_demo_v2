package port

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// EditHolderNames reads just the authorized ticket's current holder through the audited identity port.
// It is invoked after the price transaction commits, never inside a DB transaction.
type EditHolderNames interface {
	EditHolderName(context.Context, uuid.UUID) (string, error)
}

var ErrEditLeaseLost = problem.New(http.StatusConflict, "price.edit_lease_lost", "Phiên sửa đã hết hạn hoặc phiếu đang được người khác sửa; tải lại quyền sửa")
