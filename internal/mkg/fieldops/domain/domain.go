// Package domain: quy tắc nghiệp vụ của mkg/fieldops — không phụ thuộc hạ tầng,
// không time.Now() (R-12), không float (R-10: khối lượng mass.Kg, kg/cuộn decimal).
//
// Quy tắc lấy từ prototype mekong-green src/erp/field/service.ts (FM-01..10) —
// chép QUY TẮC, không chép mã:
//   - FM-03: công đoạn sau chỉ ghi khi công đoạn trước đã có số;
//   - FM-04 (G3, g3-conventions §1.1): số cuộn mỗi công đoạn phải BẰNG công đoạn
//     trước, hoặc ít hơn kèm lý do chênh; nhiều hơn thì bị từ chối (rơm không tự
//     sinh ra giữa hai công đoạn — đếm lại / sửa công đoạn trước bằng bản ghi bù);
//   - FM-09: bắt buộc số cuộn; chỉ ghi khối lượng (rơm rời) khi không có cuộn;
//   - kg/cuộn học theo HTX: Σ kg cân / Σ cuộn của 30 lô đã cân gần nhất → toàn hệ
//     thống → cấu hình → 20 (tham số tạm, chờ khách).
package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/fieldops/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Phiên bản quy tắc ghim vào mỗi dòng sổ (cột rule_version).
const (
	RuleOrder        = "fieldops.order.v1"
	RuleRollCount    = "fieldops.roll_count.v1" // FM-03/FM-04/FM-09 như trên
	RuleCrewShift    = "fieldops.crew_shift.v1"
	RuleConfirmation = "fieldops.htx_confirmation.v1"
	RuleValve        = "fieldops.valve.v1"
	// RuleKgPerRoll: Σkg/Σcuộn 30 lô cân gần nhất → hệ thống → cấu hình → 20 (tạm, chờ khách).
	RuleKgPerRoll = "fieldops.kg_per_roll.v1"
)

// FieldError là lỗi dữ liệu vào của một trường.
type FieldError struct {
	Field string
	Msg   string
}

func (e FieldError) Error() string { return e.Field + ": " + e.Msg }

func fieldErr(field, msg string) error { return FieldError{Field: field, Msg: msg} }

// Lỗi nghiệp vụ; app/ quy về problem.
var (
	ErrPreviousStageMissing = errors.New("fieldops: công đoạn trước chưa có số (FM-03)")
	ErrStageExceedsPrevious = errors.New("fieldops: số cuộn nhiều hơn công đoạn trước (FM-04)")
	ErrDiscrepancyNoReason  = errors.New("fieldops: số cuộn ít hơn công đoạn trước mà không có lý do chênh (FM-04)")
	ErrUnitMismatch         = errors.New("fieldops: công đoạn trước đếm cuộn thì công đoạn sau cũng đếm cuộn (và ngược lại với rơm rời)")
	ErrNextStageExceeds     = errors.New("fieldops: công đoạn sau đang có số lớn hơn giá trị sửa (FM-04) — sửa công đoạn sau trước")
	ErrNextStageUnexplained = errors.New("fieldops: công đoạn sau sẽ ít hơn giá trị sửa mà không có lý do chênh — sửa công đoạn sau kèm lý do trước")
	ErrOrderTransition      = errors.New("fieldops: chuyển trạng thái lệnh gom không hợp lệ")
)

const maxClockSkew = 5 * time.Minute

// CheckEventTime: bắt buộc, không ở tương lai quá độ lệch cho phép (R-13).
func CheckEventTime(field string, t, now time.Time) error {
	if t.IsZero() {
		return fieldErr(field, "bắt buộc, RFC 3339 có múi giờ")
	}
	if t.After(now.Add(maxClockSkew)) {
		return fieldErr(field, "ở tương lai")
	}
	return nil
}

// CheckReason: lý do 10..500 ký tự.
func CheckReason(field, s string, required bool) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		if required {
			return "", fieldErr(field, "bắt buộc, 10–500 ký tự")
		}
		return "", nil
	}
	if n := len([]rune(s)); n < 10 || n > 500 {
		return "", fieldErr(field, "10–500 ký tự")
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Công đoạn (FM-03, FM-04, FM-09)
// ---------------------------------------------------------------------------

// Stages theo thứ tự.
var Stages = []string{port.StageBale, port.StageGather, port.StageBoatLoad}

// ValidStage cho biết s là công đoạn hợp lệ.
func ValidStage(s string) bool {
	for _, x := range Stages {
		if x == s {
			return true
		}
	}
	return false
}

// PreviousStage trả công đoạn trước ("" với bale).
func PreviousStage(s string) string {
	for i, x := range Stages {
		if x == s && i > 0 {
			return Stages[i-1]
		}
	}
	return ""
}

// NextStage trả công đoạn sau ("" với boat_load).
func NextStage(s string) string {
	for i, x := range Stages {
		if x == s && i+1 < len(Stages) {
			return Stages[i+1]
		}
	}
	return ""
}

// Count là số của một công đoạn: số cuộn, hoặc khối lượng rơm rời khi không có cuộn.
type Count struct {
	Rolls   int
	LooseKg *mass.Kg // chỉ khi Rolls = 0
	// DiscrepancyReason: lý do khi ít hơn công đoạn trước.
	DiscrepancyReason string
}

// Loose cho biết đây là số rơm rời (không có cuộn).
func (c Count) Loose() bool { return c.Rolls == 0 && c.LooseKg != nil }

func (c Count) empty() bool { return c.Rolls == 0 && c.LooseKg == nil }

// ValidateCount: bắt buộc số cuộn ≥ 0; khối lượng chỉ khi không có cuộn (FM-09);
// một bản ghi gốc phải có cuộn hoặc khối lượng rơm rời (bản bù được về 0).
func ValidateCount(c Count, correction bool) error {
	if c.Rolls < 0 || c.Rolls > 1_000_000 {
		return fieldErr("rolls", "0..1000000")
	}
	if c.LooseKg != nil {
		if c.Rolls != 0 {
			return fieldErr("loose_kg", "chỉ ghi khối lượng rơm rời khi không có cuộn (rolls = 0) — ở ruộng đếm cuộn, không cân (FM-09)")
		}
		if c.LooseKg.Decimal().Sign() <= 0 {
			return fieldErr("loose_kg", "phải > 0")
		}
	}
	if !correction && c.Rolls == 0 && c.LooseKg == nil {
		return fieldErr("rolls", "bắt buộc số cuộn > 0 (hoặc rolls = 0 kèm loose_kg với rơm rời)")
	}
	if _, err := CheckReason("discrepancy_reason", c.DiscrepancyReason, false); err != nil {
		return err
	}
	return nil
}

// cmp so c với prev: -1 ít hơn, 0 bằng, 1 nhiều hơn. Đơn vị phải khớp.
func cmp(c, prev Count) (int, error) {
	// Số rỗng (bản bù về 0 — nhập nhầm thửa) ít hơn mọi số khác 0, bằng số rỗng.
	cz, pz := c.empty(), prev.empty()
	switch {
	case cz && pz:
		return 0, nil
	case cz:
		return -1, nil
	case pz:
		return 1, nil
	}
	if c.Loose() != prev.Loose() {
		return 0, ErrUnitMismatch
	}
	if prev.Loose() {
		return c.LooseKg.Cmp(*prev.LooseKg), nil
	}
	switch {
	case c.Rolls < prev.Rolls:
		return -1, nil
	case c.Rolls > prev.Rolls:
		return 1, nil
	}
	return 0, nil
}

// CheckAgainstPrevious áp FM-03/FM-04 cho công đoạn stage với số công đoạn trước
// (nil = chưa có). bale không có công đoạn trước.
func CheckAgainstPrevious(stage string, c Count, prev *Count) error {
	if PreviousStage(stage) == "" {
		return nil
	}
	if prev == nil {
		return ErrPreviousStageMissing
	}
	d, err := cmp(c, *prev)
	if err != nil {
		return err
	}
	switch {
	case d > 0:
		return ErrStageExceedsPrevious
	case d < 0 && strings.TrimSpace(c.DiscrepancyReason) == "":
		return ErrDiscrepancyNoReason
	}
	return nil
}

// CheckAgainstNext: khi SỬA công đoạn stage thành c, công đoạn sau (nếu có) vẫn
// phải thỏa FM-04 với giá trị mới.
func CheckAgainstNext(c Count, next *Count) error {
	if next == nil {
		return nil
	}
	d, err := cmp(*next, c)
	if err != nil {
		return err
	}
	switch {
	case d > 0:
		return ErrNextStageExceeds
	case d < 0 && strings.TrimSpace(next.DiscrepancyReason) == "":
		return ErrNextStageUnexplained
	}
	return nil
}

// ---------------------------------------------------------------------------
// Lệnh gom
// ---------------------------------------------------------------------------

// Trạng thái / loại sự kiện lệnh gom.
const (
	OrderPlanned   = "planned"
	OrderApproved  = "approved"
	OrderAssigned  = "assigned"
	OrderClosed    = "closed"
	OrderCancelled = "cancelled"
)

// Transition kiểm chuyển trạng thái from → sự kiện kind.
//   - planned (sửa kế hoạch): chỉ khi đang planned;
//   - approved: planned → approved;
//   - assigned: approved | assigned (đổi tổ) → assigned;
//   - closed: assigned → closed;
//   - cancelled: mọi trạng thái chưa kết thúc.
func Transition(from, kind string) error {
	ok := false
	switch kind {
	case OrderPlanned, OrderApproved:
		ok = from == OrderPlanned
	case OrderAssigned:
		ok = from == OrderApproved || from == OrderAssigned
	case OrderClosed:
		ok = from == OrderAssigned
	case OrderCancelled:
		ok = from != OrderClosed && from != OrderCancelled
	}
	if !ok {
		return ErrOrderTransition
	}
	return nil
}

// Countable: chỉ đếm cuộn trên lệnh gom đã duyệt (approved / assigned).
func Countable(status string) bool { return status == OrderApproved || status == OrderAssigned }

// ScheduleStatus quy trạng thái lệnh gom sang trạng thái của hợp đồng van
// collection_schedule. ok = false: trạng thái không công bố (planned chưa từng duyệt).
func ScheduleStatus(status string, published bool) (string, bool) {
	switch status {
	case OrderApproved:
		return "confirmed", true
	case OrderAssigned:
		return "in_progress", true
	case OrderClosed:
		return "completed", true
	case OrderCancelled:
		return "cancelled", published // chỉ báo hủy cho lịch đã từng công bố
	}
	return "", false
}

// ---------------------------------------------------------------------------
// Đối soát chiều mua (RB-6)
// ---------------------------------------------------------------------------

// Trạng thái đối soát.
const (
	ReconPending  = "pending"
	ReconMatch    = "match"
	ReconMismatch = "mismatch"
)

// ReconStatus: pending khi HTX chưa xác nhận; match khi số HTX = số MKG hiện
// hành; ngược lại mismatch (gồm cả khi MKG sửa số sau khi HTX đã xác nhận).
func ReconStatus(mkgRolls int, htxRolls *int) string {
	if htxRolls == nil {
		return ReconPending
	}
	if *htxRolls == mkgRolls {
		return ReconMatch
	}
	return ReconMismatch
}

// ---------------------------------------------------------------------------
// kg/cuộn học theo HTX
// ---------------------------------------------------------------------------

// Nguồn của kg/cuộn.
const (
	KgSourceHTX     = "htx"     // học từ các lô đã cân của chính HTX
	KgSourceSystem  = "system"  // học từ các lô đã cân toàn hệ thống (tenant)
	KgSourceConfig  = "config"  // cấu hình triển khai
	KgSourceDefault = "default" // 20 kg — mặc định prototype (tạm, chờ khách)
)

// LearnWindow là số lô đã cân gần nhất dùng để học.
const LearnWindow = 30

// DefaultKgPerRoll là kg/cuộn mặc định khi chưa có số cân nào (prototype DEFAULT_BALE_KG).
var DefaultKgPerRoll = decimal.NewFromInt(20)

// WeighedSample là một lô gom đã cân.
type WeighedSample struct {
	LotRef    uuid.UUID
	Rolls     int
	WeighedKg mass.Kg
}

// KgPerRoll là kết quả học kg/cuộn.
type KgPerRoll struct {
	Kg      decimal.Decimal // làm tròn 0,1 kg
	Source  string
	Samples int
}

func learn(samples []WeighedSample) (decimal.Decimal, int, bool) {
	var kg decimal.Decimal
	rolls, n := int64(0), 0
	for _, s := range samples {
		if s.Rolls <= 0 || s.WeighedKg.Decimal().Sign() <= 0 {
			continue
		}
		kg = kg.Add(s.WeighedKg.Decimal())
		rolls += int64(s.Rolls)
		n++
		if n == LearnWindow {
			break
		}
	}
	if n == 0 || rolls == 0 {
		return decimal.Zero, 0, false
	}
	return kg.Div(decimal.NewFromInt(rolls)).Round(1), n, true
}

// ResolveKgPerRoll áp chuỗi dự phòng: HTX (≤ 30 lô gần nhất, mới nhất trước) →
// hệ thống → cấu hình (> 0) → 20.
func ResolveKgPerRoll(htx, system []WeighedSample, configured *decimal.Decimal) KgPerRoll {
	if kg, n, ok := learn(htx); ok {
		return KgPerRoll{Kg: kg, Source: KgSourceHTX, Samples: n}
	}
	if kg, n, ok := learn(system); ok {
		return KgPerRoll{Kg: kg, Source: KgSourceSystem, Samples: n}
	}
	if configured != nil && configured.Sign() > 0 {
		return KgPerRoll{Kg: configured.Round(1), Source: KgSourceConfig}
	}
	return KgPerRoll{Kg: DefaultKgPerRoll.Round(1), Source: KgSourceDefault}
}
