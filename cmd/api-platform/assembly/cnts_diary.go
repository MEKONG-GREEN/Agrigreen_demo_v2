package assembly

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	mdport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
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

// masterdataParamRegistrations là các đăng ký tham số của pf/masterdata ở API: đăng ký KN cũ (giữ nguyên) và đăng ký riêng
// tham số nhật ký nuôi CNTS (KN-PARAMS-CNTS-DIARY #84 / #85 — hạn sửa lần ghi, hạn lưu GPS; nạp vào tenant khi nạp dữ liệu
// nền, đổi qua luồng duyệt tham số). Worker không nạp dữ liệu nền nên giữ đăng ký cũ.
func masterdataParamRegistrations() []mdport.ParamRegistration {
	return []mdport.ParamRegistration{extension.MasterdataParams(), extension.CNTSDiaryMasterdataParams()}
}
