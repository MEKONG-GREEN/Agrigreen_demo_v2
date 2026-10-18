package port

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"time"
)

// Frozen grid snapshots are supplied by the registered form application.
// ErrConsolidationNotFound: kỳ chưa có lần tổng hợp nào cho biểu.
var ErrConsolidationNotFound = errors.New("extension: kỳ chưa có dữ liệu tổng hợp")

// PeriodInfo là một kỳ báo cáo.
type PeriodInfo struct {
	ID             uuid.UUID
	BatchID        uuid.UUID
	Label          string
	RangeLabel     string
	SeasonLabel    string
	From, To       time.Time // ngày (00:00 UTC của ngày dương lịch)
	Closed         bool
	ProvinceCode   string
	DepartmentCode string
	ReportType     string
	Slot           int      // kỳ thứ n của đợt
	SlotLabels     []string // nhãn mọi kỳ của đợt theo khe (tiêu đề cột theo kỳ của mẫu nhiều kỳ)
}

// ConsolidationRow là một dòng xã (hoặc xã × chỉ tiêu) của lần tổng hợp.
type ConsolidationRow struct {
	Key         string
	Commune     string
	CommuneName string
	Region      string
	RegionName  string
	Indicator   string
	Cells       map[string]string // chữ cái cột → giá trị (decimal dạng chuỗi)
}

// ConsolidationTotal là dòng Cộng khu vực / Toàn tỉnh.
type ConsolidationTotal struct {
	Key        string
	Kind       string // region | province
	Region     string
	RegionName string
	Indicator  string
	Members    int
	Cells      map[string]string
}

// ConsolidationView là một lần tổng hợp (bất biến) của một biểu trong kỳ.
type ConsolidationView struct {
	RunID           uuid.UUID
	RunNo           int
	TemplateID      uuid.UUID
	VersionID       uuid.UUID
	FormCode        string
	At              time.Time
	Trigger         string
	ActorRef        uuid.UUID
	Provisional     bool
	MissingCommunes []string
	MissingNames    []string // tên xã chưa nộp (cùng thứ tự) — in kèm tệp "Tạm tính"
	Rows            []ConsolidationRow
	Totals          []ConsolidationTotal
}

// Consolidations là cổng đọc lần tổng hợp gần nhất (reports dựng tệp đơn vị nhận; tenant truyền tường minh vì
// gửi chạy ở worker). formCodes rỗng = mọi biểu của kỳ đã tổng hợp.
type Consolidations interface {
	Period(ctx context.Context, tenantID, periodID uuid.UUID) (PeriodInfo, error)
	LatestRuns(ctx context.Context, tenantID, periodID uuid.UUID, formCodes []string) ([]ConsolidationView, error)
}

// DispatchHistory (tùy chọn, reports hiện thực) cho biết báo cáo đơn vị nhận của kỳ đã được gửi — mở lại kỳ đó
// phải cảnh báo "số liệu đã thay đổi" (FN-TI-02.04 luồng phụ).
type DispatchHistory interface {
	SentForPeriod(ctx context.Context, tenantID, periodID uuid.UUID) (bool, error)
}
