package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm"
	hrmport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// wireHRMDocs nối kho tài liệu nhân sự (W11.24, FN-14): tệp của pf/hrm qua pf/evidence (mục đích hr_records), pf/hrm phân
// giải chủ thể 'pf/hrm' / 'hr_folder' cho pf/evidence (R-DB-07). Gọi sau hr.UseRecords.
func wireHRMDocs(hr *hrm.Module, ev *evidence.Module) {
	ev.UsePurpose(string(client.PurposeHRRecords), evport.PurposePolicy{SubjectModule: hrmport.SubjectModule, SubjectType: hrmport.SubjectTypeFolder, RequiresConsent: false})
	hr.UseDocs(ev.Files())
	ev.UseResolver(hrmport.SubjectModule, hr.FileResolver())
}
