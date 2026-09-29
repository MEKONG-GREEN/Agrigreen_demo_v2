package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// MovementKind là loại dịch chuyển tồn.
type MovementKind string

// Loại dịch chuyển. Dấu của Qty: + vào lô, − ra khỏi lô.
const (
	MoveReceive      MovementKind = "receive"
	MoveTransformIn  MovementKind = "transform_in"
	MoveTransformOut MovementKind = "transform_out"
	MoveDispatch     MovementKind = "dispatch"
	MoveCorrection   MovementKind = "correction"
)

// Movement là một dòng sổ mkg_hub.stock_movement — bất biến; sửa sai bằng
// dòng bù ngược dấu (kind = correction, supersedes_id + lý do).
type Movement struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	FacilityID       uuid.UUID
	LotID            uuid.UUID
	Seq              int64
	Kind             MovementKind
	Qty              mass.Kg // có dấu
	BalanceAfter     mass.Kg
	RefType          *string
	RefID            *uuid.UUID
	EventTime        time.Time
	RecordedAt       time.Time
	SupersedesID     *uuid.UUID
	CorrectionReason *string
	ActorRef         uuid.UUID
	IdemKey          string
}

// StreamKey của sổ dịch chuyển: một chuỗi băm cho mỗi lô.
func StreamKey(lotID uuid.UUID) string { return "lot:" + lotID.String() }

// Apply cộng dịch chuyển vào tồn còn lại; kết quả âm → không đủ tồn.
func Apply(remaining, qty mass.Kg) (mass.Kg, error) {
	after := remaining.Add(qty)
	if after.Decimal().IsNegative() {
		return remaining, ErrInsufficientStock
	}
	return after, nil
}

// Compensate dựng dòng bù cho một dịch chuyển: ngược dấu, trỏ supersedes_id về
// dòng gốc, kèm lý do ≥ 10 ký tự. Chỉ nhận/xuất được bù trực tiếp; mẻ biến đổi
// có nhiều lô ra/vào nên sửa bằng nghiệp vụ mẻ bù (docs/design/mkg.md, câu hỏi mở).
func Compensate(orig Movement, reason string) (qty mass.Kg, err error) {
	if orig.Kind != MoveReceive && orig.Kind != MoveDispatch {
		return qty, ErrCorrectionKind
	}
	if len([]rune(strings.TrimSpace(reason))) < 10 {
		return qty, fieldErr("reason", "lý do sửa sai bắt buộc, tối thiểu 10 ký tự")
	}
	return mass.Kg{}.Sub(orig.Qty), nil
}
