package app

import (
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/cntsknqg/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
)

// FN-19 AC-01: mẫu Word của 5 mẫu hợp lệ, dựng được .docx có đủ mục và bảng của mẫu gốc.
func TestWordTemplatesRender(t *testing.T) {
	want := map[string][]string{
		domain.Form01: {"BÁO CÁO TIẾN ĐỘ THỰC HIỆN DỰ ÁN", "Tháng 10/2027", "1. Xây dựng mô hình", "2. Cung ứng giống, thiết bị, vật tư", "CHỦ NHIỆM DỰ ÁN"},
		domain.Form03: {"Kết quả kiểm tra, đánh giá dự án khuyến nông trung ương", "1. Thành phần đoàn kiểm tra", "II.1. Kiểm tra hồ sơ, tài liệu",
			"III. Kiến nghị, đề xuất", "TRƯỞNG ĐOÀN KIỂM TRA"},
		domain.Form04: {"Mẫu 04/DA-NTTH", "LỚP TẬP HUẤN NHÂN RỘNG MÔ HÌNH", "III. Đại diện học viên", "4. Nhận xét chung", "ĐƠN VỊ TỔ CHỨC TẬP HUẤN"},
		domain.Form05: {"Mẫu 05/DA-NTMH", "KẾT QUẢ THỰC HIỆN MÔ HÌNH KHUYẾN NÔNG", "4. Kết quả thực hiện", "Yêu cầu HĐ", "1. Qui mô",
			"5. Giá trị giống, vật tư đã hỗ trợ cho nông dân", "III. Kinh phí mô hình", "PHỤ LỤC I", "PHỤ LỤC II", "Không đạt; thiếu 1 hộ", "(Bản điều chỉnh lần 1)"},
		domain.Form07: {"Mẫu 07/NTKQ-DA", "Năm 2027", "Đại diện Trung tâm Khuyến nông Quốc gia (Bên A)", "2. Kết quả thực hiện tại các điểm trình diễn",
			"7. Đào tạo", "III. Kinh phí trong năm", "ĐẠI DIỆN BÊN A"},
	}
	for _, s := range domain.Specs() {
		tpl := WordTemplate(s)
		if err := doctpl.Validate(tpl); err != nil {
			t.Fatalf("%s: %v", s.Form, err)
		}
		period := map[string]string{domain.Form01: "2027-10", domain.Form07: "2027"}[s.Form]
		c := s.Prefill(domain.Facts{ProjectName: "Dự án gà", Basis: "HĐ 12", Site: &domain.SiteFacts{Name: "Điểm A", Households: 3}})
		d := store.Doc{Form: s.Form, Period: period, Revision: 1, Status: domain.StatusDraft, Content: c}
		if s.Form == domain.Form05 {
			d.Revision = 2
			for _, r := range d.Content.Tables["chi_tieu"] {
				if r["ma"] == "so_ho" {
					r["dat"], r["ghi_chu"] = "khong", "thiếu 1 hộ"
				}
			}
		}
		data := WordData(s, d)
		data.Vars[VarNumber], data.Vars[VarPlaceDate] = "1/BC-TTKN", "An Giang, ngày 13 tháng 10 năm 2027"
		raw, err := doctpl.RenderDOCX(tpl, data)
		if err != nil {
			t.Fatalf("%s: %v", s.Form, err)
		}
		content, err := docx.Extract(raw)
		if err != nil {
			t.Fatal(err)
		}
		text := content.Text()
		for _, w := range want[s.Form] {
			if !strings.Contains(text, w) {
				t.Fatalf("Word %s thiếu %q:\n%s", s.Form, w, text)
			}
		}
	}
}

func TestGroupThousands(t *testing.T) {
	for in, want := range map[string]string{"450000000": "450.000.000", "1234.5": "1.234,5", "12": "12"} {
		if got := groupThousands(in); got != want {
			t.Fatalf("%s → %s", in, got)
		}
	}
}
