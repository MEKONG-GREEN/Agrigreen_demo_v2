package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/hrm/port"
)

// wireHRMDocs nối kho tài liệu nhân sự (W11.24, FN-14): tệp của pf/hrm qua pf/evidence (mục đích hr_records), pf/hrm phân
// giải chủ thể 'pf/hrm' / 'hr_folder' cho pf/evidence (R-DB-07). Gọi sau hr.UseRecords.
func wireHRMDocs(hr *hrm.Module, ev *evidence.Module) {
	hr.UseDocs(ev.Files())
	ev.UseResolver(hrmport.SubjectModule, hr.FileResolver())
}
