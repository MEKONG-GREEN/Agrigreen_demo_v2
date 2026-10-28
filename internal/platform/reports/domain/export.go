package domain

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// JobStatus là trạng thái việc xuất.
type JobStatus string

// Các trạng thái.
const (
	JobQueued    JobStatus = "queued"
	JobCompleted JobStatus = "completed"
	JobFailed    JobStatus = "failed"
)

// Nguồn kích hoạt việc xuất.
const (
	TriggerAPI      = "api"
	TriggerSchedule = "schedule"
)

// Params là bộ lọc của việc xuất (ảnh chụp đã chuẩn hóa, lưu jsonb).
type Params struct {
	Level       string `json:"level"`
	UnitCode    string `json:"unit_code,omitempty"`
	Period      string `json:"period,omitempty"` // YYYY-MM | YYYY-Qn | YYYY
	SeasonCode  string `json:"season_code,omitempty"`
	SeasonYear  int    `json:"season_year,omitempty"`
	SeasonCodeB string `json:"season_code_b,omitempty"`
	SeasonYearB int    `json:"season_year_b,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	MachineType string `json:"machine_type,omitempty"`
	// HTX US-DASH-02: lọc theo HTX và giống như bảng điều hành. Với quản lý HTX,
	// cooperative_ref luôn là HTX của người yêu cầu (phạm vi, không chỉ là bộ lọc).
	CooperativeRef string `json:"cooperative_ref,omitempty"`
	VarietyCode    string `json:"variety_code,omitempty"`
}

// CoopScoped cho biết phạm vi của việc xuất/lịch là MỘT HTX (quản lý HTX, HTX
// US-DASH-02): không có phạm vi đơn vị, cooperative_ref là HTX của người yêu cầu.
func (p Params) CoopScoped(scopeAll bool, scopeUnits []string) bool {
	return !scopeAll && len(scopeUnits) == 0 && p.CooperativeRef != ""
}

// Job là một việc xuất.
type Job struct {
	ID              uuid.UUID
	TenantID        uuid.UUID
	TemplateKey     string
	TemplateVersion int
	Def             Definition
	Format          string
	Params          Params
	SourceArgs      *FrozenSourceArgs `json:"-"`
	ScopeAll        bool
	ScopeUnits      []string
	RequestedBy     uuid.UUID
	RequestedAt     time.Time
	Trigger         string
	ScheduleID      *uuid.UUID
	ScheduleSlot    *string
	IdemKey         string
	Status          JobStatus
	ObjectKey       *string
	SHA256          *string
	SizeBytes       *int64
	RowCount        *int
	Provisional     *bool
	ErrorCode       *string
	CompletedAt     *time.Time
}

// ObjectKey là khóa tệp trong kho đối tượng: reports/<tenant>/<job>.<định dạng>.
func ObjectKey(tenant, job uuid.UUID, format string) string {
	return "reports/" + tenant.String() + "/" + job.String() + "." + format
}

// ContentType của định dạng.
func ContentType(format string) string {
	if format == FormatPDF {
		return "application/pdf"
	}
	return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
}

// FileName gợi ý tên tệp tải về (không dấu).
func FileName(j Job) string {
	return strings.ReplaceAll(j.TemplateKey, ".", "_") + "_" + j.RequestedAt.In(clock.Zone()).Format("20060102_1504") + "." + j.Format
}
