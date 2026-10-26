package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// wireCNTSDiary nối ảnh nhật ký nuôi CNTS (W11.48, BM-08/09/19): tệp của pf/extension qua pf/evidence (mục đích
// extension.cnts_diary, khóa của chủ hộ ở kho định danh), pf/extension phân giải chủ thể 'pf/extension' / 'cnts_household'
// cho pf/evidence (R-DB-07). Đồng ý của hộ không kiểm cho mục đích này (giả định tạm, chờ DPIA Q-005 — gia-dinh/W11.48).
func wireCNTSDiary(kn *extension.Module, ev *evidence.Module, vault client.Client) {
	purpose, module, subjectType := extension.DiaryEvidencePurpose()
	ev.UsePurpose(purpose, evport.PurposePolicy{SubjectModule: module, SubjectType: subjectType, RequiresConsent: false})
	ev.UseResolver(module, kn.DiaryFileResolver())
	kn.UseCNTSDiary(vault, ev.Files())
}
