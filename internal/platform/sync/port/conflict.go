package port

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// Vai trò của một phiên bản trong chi tiết xung đột (US-SYNC-03).
const (
	VersionRoleRecord      = "record"      // bản ghi máy gửi lên gây xung đột
	VersionRoleCurrent     = "current"     // phiên bản hiện hành của cùng thực thể trên máy chủ
	VersionRoleOverlapping = "overlapping" // thực thể khác bị chồng lấn (vd thửa của HTX khác)
)

// ConflictVersion là một phiên bản trong chi tiết xung đột — KHÔNG có dữ liệu cá
// nhân (mục 10.4): chỉ mã tham chiếu, số liệu, hình.
type ConflictVersion struct {
	RecordID  uuid.UUID // id bản ghi / phiên bản
	Role      string
	Label     string // nhãn hiển thị, vd mã thửa
	Current   bool   // đang là phiên bản hiện hành
	EventTime time.Time
	Data      json.RawMessage
}

// ErrConflictUnknown: module không có xung đột với mã này (hoặc ngoài phạm vi).
var ErrConflictUnknown = errors.New("sync: module không có xung đột này")

// ConflictViewer (tùy chọn) là Sink cho xem cả hai phiên bản của một xung đột
// do chính nó mở (US-SYNC-03). pf/sync gọi khi đọc chi tiết xung đột của entity.
// scope là phạm vi của người xem; ngoài phạm vi → ErrConflictUnknown.
type ConflictViewer interface {
	ConflictVersions(ctx context.Context, scope authz.Scope, conflictID uuid.UUID) ([]ConflictVersion, error)
}
