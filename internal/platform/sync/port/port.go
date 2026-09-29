// Package port là hợp đồng giữa module đồng bộ (pf/sync) và các module nghiệp
// vụ có dữ liệu đi xuống hoặc đi lên thiết bị hiện trường (docs/dev/sync-contract.md).
//
// Mỗi module nghiệp vụ tự hiện thực Source/Sink trong gói của mình và đưa ra
// qua module.go; pf/sync chỉ gom lại, không đọc bảng của module khác (R-05, R-06).
package port

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// Op của thay đổi chiều xuống.
const (
	OpUpsert    = "upsert"
	OpTombstone = "tombstone" // ngừng hiển thị trên máy — không có xóa cứng ở máy chủ
)

// Change là một thay đổi chiều xuống.
type Change struct {
	Entity  string
	ID      uuid.UUID
	Version int64
	Op      string
	Data    json.RawMessage // nil khi tombstone; KHÔNG chứa dữ liệu cá nhân đầy đủ (mục 10.4)
}

// Source cung cấp thay đổi chiều xuống theo phạm vi của người gọi.
//
// Cursor là số tăng dần do module tự quản (vd cột sync_seq bigint lấy từ một
// sequence, cập nhật mỗi khi bản ghi hiện hành đổi). Changes trả thay đổi có
// cursor > after theo thứ tự tăng, tối đa limit, cùng cursor lớn nhất đã trả.
// Bản ghi ra khỏi phạm vi của người gọi phải trả tombstone khi có thể; nếu
// không, pf/sync sẽ yêu cầu làm mới toàn bộ.
type Source interface {
	Entities() []string
	Changes(ctx context.Context, scope authz.Scope, entity string, after int64, limit int) (changes []Change, last int64, err error)
}

// FarmerScope là phạm vi pull của tài khoản NÔNG DÂN (vai farmer, token không có org_ref
// nên phạm vi pf/party rỗng): các nông hộ liên kết với tài khoản (pf/accounts) và xã của
// chúng (và HTX của chúng). Communes CHỈ dùng cho dữ liệu công khai theo địa bàn (tin/tài liệu, đơn vị hành
// chính); dữ liệu gắn với hộ (thửa, vụ, nhật ký, yêu cầu hỗ trợ) phải lọc theo Households.
type FarmerScope struct {
	Households   []uuid.UUID
	Cooperatives []uuid.UUID // HTX của các nông hộ lúc liên kết (vd SOP riêng của HTX)
	Communes     []string
}

// FarmerSource là Source phục vụ cả thiết bị của nông dân. pf/sync CHỈ gọi FarmerChanges
// (không bao giờ gọi Changes với phạm vi rỗng hay phạm vi suy từ xã) cho các entity trong
// FarmerEntities khi người gọi là nông dân; entity của Source không hiện thực interface
// này, hoặc không có trong FarmerEntities, luôn đóng với nông dân (mặc định chặn).
// Cursor dùng chung với Changes (cùng vị trí trong con trỏ của thiết bị).
type FarmerSource interface {
	Source
	FarmerEntities() []string
	FarmerChanges(ctx context.Context, fs FarmerScope, entity string, after int64, limit int) (changes []Change, last int64, err error)
}

// Op của bản ghi chiều lên: chỉ ghi thêm (mục 15).
const (
	OpCreate  = "create"
	OpCorrect = "correct" // bản ghi bù: bắt buộc SupersedesID + CorrectionReason
)

// Record là một bản ghi máy gửi lên.
type Record struct {
	ID               uuid.UUID // UUIDv7 sinh trên máy (R-11)
	IdemKey          string    // "dev:<device_id>:<seq>"
	Entity           string
	Op               string
	EventTime        time.Time // R-13
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	DeviceID         uuid.UUID
	Payload          json.RawMessage
}

// Trạng thái kết quả theo từng bản ghi.
const (
	StatusAccepted  = "accepted"
	StatusDuplicate = "duplicate" // idem_key đã ghi — máy coi như thành công
	StatusConflict  = "conflict"  // giữ cả hai, chờ xử lý có người phụ trách + SLA
	StatusRejected  = "rejected"  // dữ liệu sai — máy giữ lại, người dùng tạo bản ghi bù
)

// Result là kết quả của một bản ghi.
type Result struct {
	ID         uuid.UUID
	Status     string
	ConflictID *uuid.UUID
	Code       string
	Errors     []problem.FieldError
}

// Sink nhận bản ghi chiều lên cho các entity của module. Accept chạy trong giao
// dịch riêng của module; một bản ghi lỗi không làm hỏng cả lô. Phải idempotent
// theo IdemKey. Kiểm vai và phạm vi (scope.Allows) như API thường.
type Sink interface {
	Entities() []string
	Accept(ctx context.Context, scope authz.Scope, rec Record) Result
}
