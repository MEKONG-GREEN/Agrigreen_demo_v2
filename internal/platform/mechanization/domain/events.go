package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// OwnerAction là loại biến động chủ sở hữu.
type OwnerAction string

// Các biến động chủ sở hữu.
const (
	OwnerCreated     OwnerAction = "created"
	OwnerDeactivated OwnerAction = "deactivated"
	OwnerReactivated OwnerAction = "reactivated"
)

// OwnerEvent là một dòng sổ owner_event (ảnh chụp sau sự việc).
type OwnerEvent struct {
	ID               uuid.UUID
	TenantID         uuid.UUID
	Owner            Owner
	Action           OwnerAction
	Reason           string
	MachinesAffected int
	EventTime        time.Time
	ActorRef         uuid.UUID
	IdemKey          string
	Seq              int64
	Recorded         time.Time
}

// OwnerStreamKey là stream của chủ sở hữu.
func OwnerStreamKey(id uuid.UUID) string { return "owner:" + id.String() }

// MachineStreamKey là stream của máy.
func MachineStreamKey(ref uuid.UUID) string { return "machine:" + ref.String() }

// AreaStreamKey là stream của bản ghi diện tích vụ.
func AreaStreamKey(id uuid.UUID) string { return "area:" + id.String() }

// RunStreamKey là stream các lần cân đối của một vụ.
func RunStreamKey(s Season) string { return "season:" + s.Key() }

// ResultStreamKey là stream các dòng kết quả của một lần cân đối.
func ResultStreamKey(run uuid.UUID) string { return "run:" + run.String() }

// AreaAction là loại biến động diện tích vụ.
type AreaAction string

// Các biến động diện tích vụ.
const (
	AreaRecorded AreaAction = "recorded" // nhập tay
	AreaSynced   AreaAction = "synced"   // App HTX (FN-07, G2)
	AreaLocked   AreaAction = "locked"
	AreaUnlocked AreaAction = "unlocked"
)

// AreaEvent là một dòng sổ season_area_event.
type AreaEvent struct {
	ID        uuid.UUID
	TenantID  uuid.UUID
	Area      SeasonArea
	Action    AreaAction
	Reason    string
	EventTime time.Time
	ActorRef  uuid.UUID
	IdemKey   string
	Seq       int64
	Recorded  time.Time
}

// BalanceRun là một lần cân đối đã lưu.
type BalanceRun struct {
	ID            uuid.UUID
	TenantID      uuid.UUID
	Season        Season
	ReferenceDate time.Time
	PilotStages   []string
	Thresholds    Thresholds
	ResultCount   int
	StatusCounts  map[string]int
	MissingNorms  []string
	EventTime     time.Time
	ActorRef      uuid.UUID
	IdemKey       string
	RuleVersion   string
	Seq           int64
	Recorded      time.Time
}

// BalanceResult là một dòng kết quả HTX × khâu × chủng loại đã lưu (bất biến).
type BalanceResult struct {
	ID             uuid.UUID
	RunID          uuid.UUID
	Season         Season
	ReferenceDate  time.Time
	CooperativeRef uuid.UUID
	CommuneCode    string
	ProvinceCode   string
	FarmStage      string
	MachineType    string
	AreaHa         *decimal.Decimal
	AreaSource     string
	AreaLocked     bool
	NormKey        string
	Norm           *ParamRef
	MachinesTotal  int
	Capacity       int
	Figures        BalanceFigures
	Seq            int64
}

// Row trả phần cần cho tổng hợp.
func (r BalanceResult) Row() ResultRow {
	return ResultRow{
		Stage: r.FarmStage, MachineType: r.MachineType, Required: r.Figures.Required, Capacity: r.Capacity,
		MachinesTotal: r.MachinesTotal, Status: r.Figures.Status,
	}
}
