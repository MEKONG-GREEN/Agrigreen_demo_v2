package domain

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Phiên bản quy tắc ghi sổ.
const (
	OwnerRuleVersion   = "cgh.owner/v1"
	MachineRuleVersion = "cgh.machine/v1"
	AreaRuleVersion    = "cgh.season_area/v1"
)

// OwnerType là loại chủ sở hữu (FN-04, danh mục chuẩn — không cấu hình rời).
type OwnerType string

// Các loại chủ sở hữu.
const (
	OwnerCoopMember OwnerType = "thanh_vien_htx"
	OwnerCoop       OwnerType = "htx"
	OwnerEnterprise OwnerType = "doanh_nghiep"
	OwnerOther      OwnerType = "khac"
)

// OwnerTypeLabels là nhãn tiếng Việt.
var OwnerTypeLabels = map[OwnerType]string{
	OwnerCoopMember: "Thành viên HTX", OwnerCoop: "HTX", OwnerEnterprise: "Doanh nghiệp", OwnerOther: "Đơn vị khác",
}

// ParseOwnerType nhận mã hoặc nhãn tiếng Việt (có/không dấu).
func ParseOwnerType(s string) (OwnerType, bool) {
	f := Fold(s)
	for t, label := range OwnerTypeLabels {
		if f == string(t) || f == Fold(label) {
			return t, true
		}
	}
	if f == "hop tac xa" {
		return OwnerCoop, true
	}
	return "", false
}

// NeedsCooperative cho biết loại chủ sở hữu phải liên kết đúng một HTX (FN-04 BR-04).
func (t OwnerType) NeedsCooperative() bool { return t == OwnerCoopMember || t == OwnerCoop }

// Owner là hồ sơ chủ sở hữu máy. Không có dữ liệu cá nhân: PersonRef trỏ kho định danh.
type Owner struct {
	ID             uuid.UUID
	Code           string
	Type           OwnerType
	CooperativeRef uuid.UUID
	PersonRef      uuid.UUID
	DisplayName    string // tên tổ chức (DN/đơn vị khác); rỗng với cá nhân
	Active         bool
	StatusReason   string
	LastSeq        int64
}

// MachineStatus là tình trạng máy (FN-05, danh mục chuẩn).
type MachineStatus string

// Các tình trạng. "Chưa có dữ liệu" là trạng thái suy diễn, không lưu.
const (
	StatusActive      MachineStatus = "hoat_dong"
	StatusMaintenance MachineStatus = "bao_tri"
	StatusBroken      MachineStatus = "hong"
	StatusStopped     MachineStatus = "ngung_hoat_dong"
)

// StatusLabels là nhãn tiếng Việt.
var StatusLabels = map[MachineStatus]string{
	StatusActive: "Hoạt động", StatusMaintenance: "Bảo trì", StatusBroken: "Hỏng", StatusStopped: "Ngừng hoạt động",
}

// ParseStatus nhận mã hoặc nhãn tiếng Việt (có/không dấu).
func ParseStatus(s string) (MachineStatus, bool) {
	f := Fold(s)
	for st, label := range StatusLabels {
		if f == string(st) || f == Fold(label) {
			return st, true
		}
	}
	return "", false
}

// Nguồn của một biến động máy (người/nguồn thực hiện — FN-05 BR-08).
const (
	SourceManual = "manual"
	SourceImport = "import"
	SourceAppHTX = "app_htx"
)

// SourceLabels là nhãn nguồn hiển thị (FN-05 BR-05, QT-03).
var SourceLabels = map[string]string{SourceManual: "Nhập tay", SourceImport: "Nhập tay", SourceAppHTX: "App HTX"}

// MachineAction là loại biến động máy.
type MachineAction string

// Các biến động.
const (
	MachineCreated       MachineAction = "created"
	MachineUpdated       MachineAction = "updated"
	MachineStatusChanged MachineAction = "status_changed"
	MachineDeactivated   MachineAction = "deactivated"
	MachineReactivated   MachineAction = "reactivated"
)

// Machine là hồ sơ máy tại một thời điểm (ảnh chụp trong sổ và bảng hiện tại).
type Machine struct {
	Ref            uuid.UUID
	Code           string
	MachineType    string
	FarmStage      string
	CooperativeRef uuid.UUID
	OwnerID        uuid.UUID
	Brand          string
	Model          string
	SerialNo       string
	ChassisNo      string
	YearMade       *int
	Fuel           string
	PowerHP        *decimal.Decimal
	CapacityHa     *decimal.Decimal
	OwnedSince     time.Time // ngày (00:00 UTC của ngày nghiệp vụ)
	Status         MachineStatus
	StatusSource   string
	StatusAt       time.Time
	Active         bool
	DeactivatedOn  *time.Time
	StatusReason   string
	LastSeq        int64
	// W4.2 (ADR-0030 mục 4): đơn vị chủ của máy và cờ đọc qua cấp phép chia sẻ (tenant ≠ tenant phiên). Chỉ đặt khi
	// đọc (store), không ghi vào sổ.
	TenantID      uuid.UUID
	ViaShareGrant bool
}

// MachineEvent là một dòng sổ machine_event.
type MachineEvent struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Machine   Machine
	Action    MachineAction
	Source    string
	Reason    string
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
	Seq       int64
	Recorded  time.Time
}

// FieldError là lỗi theo trường.
type FieldError struct {
	Field string
	Err   error
}

func (e FieldError) Error() string { return e.Field + ": " + e.Err.Error() }

// Lỗi kiểm dữ liệu máy.
var (
	ErrIdentifier   = errors.New("cần nhập Số máy/SN hoặc Số khung")
	ErrOwnedFuture  = errors.New("ngày HTX sở hữu không được sau ngày hiện tại")
	ErrDeactBefore  = errors.New("ngày vô hiệu hóa phải sau Ngày HTX sở hữu")
	ErrDeactFuture  = errors.New("ngày vô hiệu hóa không được sau ngày hiện tại")
	ErrYear         = errors.New("năm sản xuất không hợp lý")
	ErrNegative     = errors.New("phải là số không âm")
	ErrStatus       = errors.New("một trong: Hoạt động / Bảo trì / Hỏng / Ngừng hoạt động")
	ErrStageDerived = errors.New("khâu sản xuất tự gán theo chủng loại, không nhập độc lập")
)

// NormalizeIdent chuẩn hóa Số máy/SN, Số khung: bỏ khoảng trắng hai đầu, viết hoa.
func NormalizeIdent(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

// ValidateMachine kiểm hồ sơ máy trước khi ghi. today là ngày nghiệp vụ hiện tại.
func ValidateMachine(m Machine, today time.Time) error {
	if m.SerialNo == "" && m.ChassisNo == "" {
		return FieldError{"serial_no", ErrIdentifier}
	}
	if m.OwnedSince.IsZero() {
		return FieldError{"owned_since", errors.New("ngày HTX sở hữu/tiếp nhận máy là bắt buộc")}
	}
	if m.OwnedSince.After(today) {
		return FieldError{"owned_since", ErrOwnedFuture}
	}
	if m.YearMade != nil && (*m.YearMade < 1950 || *m.YearMade > today.Year()) {
		return FieldError{"year_made", ErrYear}
	}
	if m.PowerHP != nil && m.PowerHP.IsNegative() {
		return FieldError{"power_hp", ErrNegative}
	}
	if m.CapacityHa != nil && m.CapacityHa.IsNegative() {
		return FieldError{"capacity_ha", ErrNegative}
	}
	if _, ok := StatusLabels[m.Status]; !ok {
		return FieldError{"status", ErrStatus}
	}
	return nil
}

// ValidateDeactivation kiểm ngày vô hiệu hóa (FN-05 luồng ngoại lệ, BR-09).
func ValidateDeactivation(ownedSince, on, today time.Time) error {
	if on.Before(ownedSince) {
		return FieldError{"deactivated_on", ErrDeactBefore}
	}
	if on.After(today) {
		return FieldError{"deactivated_on", ErrDeactFuture}
	}
	return nil
}

// CountsAt cho biết máy có thuộc HTX tại ngày T theo FN-05 BR-09: Ngày HTX sở
// hữu ≤ T và (chưa vô hiệu hóa hoặc Ngày vô hiệu hóa > T).
func CountsAt(ownedSince time.Time, deactivatedOn *time.Time, t time.Time) bool {
	if ownedSince.After(t) {
		return false
	}
	return deactivatedOn == nil || deactivatedOn.After(t)
}
