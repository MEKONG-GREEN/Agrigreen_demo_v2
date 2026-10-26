package extension

import (
	diaryapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsdiary/app"
	evport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/evidence/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// UseCNTSDiary (W11.48) nối kho định danh (khóa công khai chủ hộ — mã hóa ảnh, nhãn chủ hộ) và kho tệp pf/evidence cho ảnh
// nhật ký nuôi BM-08/09/19. Chưa nối → tải/xem ảnh trả 503; lần ghi không ảnh vẫn ghi được. Gốc lắp ráp đăng ký thêm mục
// đích extension.diary và DiaryFileResolver với pf/evidence (chủ thể 'pf/extension' / 'cnts_household').
func (m *Module) UseCNTSDiary(v client.Client, f evport.Files) { m.diary.Vault, m.diary.Files = v, f }

// DiaryFileResolver là Resolver (kèm FileAuthorizer) của pf/extension cho pf/evidence: ảnh nhật ký CNTS (W11.48).
func (m *Module) DiaryFileResolver() evport.Resolver { return diaryapp.FileResolver{S: m.diary} }

// DiaryEvidencePurpose trả mục đích và loại chủ thể tệp ảnh nhật ký để gốc lắp ráp đăng ký với pf/evidence.
func DiaryEvidencePurpose() (purpose, subjectModule, subjectType string) {
	return diaryapp.Purpose, diaryapp.SubjectModule, diaryapp.SubjectType
}
