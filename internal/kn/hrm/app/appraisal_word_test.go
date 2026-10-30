package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/hrm/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
)

func TestAppraisalWordPaperAndDeterminism_W1126(t *testing.T) {
	for _, kind := range []string{"MS05", "MS06"} {
		tpl := AppraisalWordTemplate(kind)
		if err := doctpl.Validate(tpl); err != nil {
			t.Fatal(err)
		}
		s := domain.DefaultAppraisalSummary(kind)
		p := AppraisalPeriod{Year: 2026, Quarter: 3}
		x := AppraisalItem{Name: "Người Kiểm Tra", AppraisalEntry: domain.AppraisalEntry{Member: domain.AppraisalMember{PositionName: "Viên chức", UnitName: "Phòng Trồng trọt"},
			SelfRating: "HTXSNV", CollectiveRating: "HTTNV", Reason: "Căn cứ nhiệm vụ", PersonnelProposal: "Tiếp tục phân công", Note: "Ghi chú", Self: domain.AppraisalScoring{Total: "82"}}}
		data := appraisalWordData(s, p, []AppraisalItem{x}, "test-stamp")
		a, err := doctpl.RenderDOCX(tpl, data)
		if err != nil {
			t.Fatal(err)
		}
		b, err := doctpl.RenderDOCX(tpl, data)
		if err != nil || !bytes.Equal(a, b) {
			t.Fatal("Word không tất định", err)
		}
		content, err := docx.Extract(a)
		if err != nil {
			t.Fatal(err)
		}
		text := content.Text()
		for _, want := range []string{"QUÝ III, NĂM 2026", "Người Kiểm Tra", s.Signature, "Hoàn thành tốt nhiệm vụ", "Tiếp tục phân công"} {
			if !strings.Contains(text, want) {
				t.Fatalf("thiếu %q trong biểu %s", want, kind)
			}
		}
		if strings.Contains(text, "82") || len(tpl.Body[3].Columns) != 8 {
			t.Fatal("biểu giấy thêm điểm/cột ngoài mẫu")
		}
		p.Quarter = 0
		if title := appraisalTitle(s, p); strings.Contains(title, "QUÝ") || !strings.Contains(title, "NĂM 2026") {
			t.Fatal("nhãn đánh giá năm:", title)
		}
	}
}
