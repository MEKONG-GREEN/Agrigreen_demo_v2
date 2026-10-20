package app_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	source "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcsource/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func wordDraft(kind string, defaults map[string]string) domain.Draft {
	day := time.Date(2026, 12, 29, 0, 0, 0, 0, time.UTC)
	qty, pct, big := "100", "85", "12500.75"
	parent := uuid.MustParse("01920000-0000-7000-8000-000000000001")
	child := uuid.MustParse("01920000-0000-7000-8000-000000000002")
	child2 := uuid.MustParse("01920000-0000-7000-8000-000000000003")
	area := decimal.RequireFromString("1234.5")
	data := domain.Data{Period: domain.Period{ID: uuid.MustParse("01920000-0000-7000-8000-0000000000aa"), Type: kind, Year: 2026, Name: "Kỳ kiểm chứng",
		From: day.AddDate(0, 0, -6), To: day, Cutoff: day}, Template: domain.Template(kind), InputHash: strings.Repeat("ab", 32),
		Rows: []domain.ProgramRow{
			{Ref: parent, Kind: "program", Name: "Chương trình XIII", Roman: "XIII"},
			{Ref: child, ParentRef: &parent, Kind: "item", Name: "Mô hình lúa", Tab: "programs", Unit: "ha", Planned: &qty, InPeriod: "20", Adjustment: "-5", YearToDate: "85", Percent: &pct, Content: "Xã A", Status: "Đang triển khai", Communes: []string{"A"}},
			{Ref: child2, ParentRef: &parent, Kind: "item", Name: "Tập huấn", Tab: "programs", Unit: "lớp", Planned: &big, InPeriod: "0", YearToDate: "0", Department: "trong_trot", Missing: true},
		},
		Metrics: []domain.SourceMetric{
			{Label: "Dưa leo", Unit: "đ/kg", Value: source.Value{Mode: source.ModePrice, PriceDays: []source.PriceDay{{Day: "2026-12-28", Min: decimal.NewFromInt(8000), Max: decimal.NewFromInt(13500)}, {Day: "2026-12-29", Min: decimal.NewFromInt(8500), Max: decimal.NewFromInt(14000)}}}},
			{Label: "Diện tích gieo trồng lúa", Unit: "ha", Department: "trong_trot", Value: source.Value{Mode: "cumulative", Cumulative: &area, Increment: &area, YearToDate: &area, Closed: true}},
		},
		FormTexts:   map[string]map[string]string{"trong_trot": {"cong_tac_khac": "Kiểm tra đồng ruộng"}, "hanh_chinh_tong_hop": {"cchc": "Một cửa liên thông"}},
		Provisional: []string{"Phòng trong_trot chưa gửi số liệu"},
	}
	return domain.Refresh(domain.Draft{}, data, defaults)
}

func render(t *testing.T, d domain.Draft, data doctpl.Data) []byte {
	t.Helper()
	body, err := doctpl.RenderDOCX(app.WordTemplate(d.Data.Template), data)
	if err != nil {
		t.Fatal(err)
	}
	return body
}

// TestW10_17_WordAllFive: mẫu Word Mẫu 1–5 hợp lệ, dựng được, đọc lại có lời văn, số (kiểu Việt Nam, chính xác), bảng.
func TestW10_17_WordAllFive(t *testing.T) {
	for _, tpl := range app.WordTemplates() {
		if err := doctpl.Validate(tpl); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"week", "month", "quarter", "year", "program"} {
		t.Run(kind, func(t *testing.T) {
			d := wordDraft(kind, map[string]string{"place": "An Giang", "report_date": "2026-12-30", "document_no": "27", "recipient": "Sở Nông nghiệp và Môi trường",
				"recipients": "- Như trên;\n- Lưu: VT, KHTC.", "authority": "KT. GIÁM ĐỐC", "signer_title": "PHÓ GIÁM ĐỐC", "signer": "Nguyễn Văn A"})
			data := app.WordData(d, domain.Filter{}, 3)
			body := render(t, d, data)
			c, err := docx.Extract(body)
			if err != nil {
				t.Fatal(err)
			}
			text := c.Text()
			for _, want := range []string{"SỞ NÔNG NGHIỆP VÀ MÔI TRƯỜNG AN GIANG", "Số: 27/BC-TTKN", "An Giang, ngày 30 tháng 12 năm 2026", "BÁO CÁO",
				"Số liệu đến ngày 29/12/2026 — bản xuất Excel phiên bản 3", "TẠM TÍNH", "Phòng Trồng trọt chưa gửi số liệu",
				"Trên đây là báo cáo Kỳ kiểm chứng gửi Sở Nông nghiệp và Môi trường tổng hợp./.", "KT. GIÁM ĐỐC", "PHÓ GIÁM ĐỐC", "Nguyễn Văn A",
				"- Lưu: VT, KHTC.", "Dấu dữ liệu: " + strings.Repeat("ab", 32)} {
				if !strings.Contains(text, want) {
					t.Fatalf("thiếu %q:\n%s", want, text)
				}
			}
			var item []string
			for _, tb := range c.Tables {
				for _, row := range tb {
					if len(row) == 8 && row[1] == "Mô hình lúa" {
						item = row
					}
				}
			}
			if kind != "program" {
				if item == nil || item[3] != "100" || item[4] != "20" || item[5] != "85" || item[6] != "85%" || item[7] != "Đang triển khai\nXã A" {
					t.Fatalf("dòng hạng mục = %q", item)
				}
				if !strings.Contains(text, "12.500,75") || !strings.Contains(text, "Phòng Trồng trọt chưa gửi số liệu.") {
					t.Fatalf("số lớn kiểu Việt Nam / hạng mục thiếu số liệu:\n%s", text)
				}
			}
			if kind == "week" {
				if !strings.Contains(text, "1.234,5") || !strings.Contains(text, "28/12/2026") || !strings.Contains(text, "13.500") || !strings.Contains(text, "Kiểm tra đồng ruộng") || !strings.Contains(text, "Một cửa liên thông") {
					t.Fatalf("Mẫu 1 thiếu chỉ tiêu/giá/công tác:\n%s", text)
				}
			}
			if kind == "quarter" || kind == "year" || kind == "program" {
				if !strings.Contains(text, "PHỤ LỤC") || !strings.Contains(text, "(Kèm theo Báo cáo số 27/BC-TTKN ngày 30/12/2026)") || !strings.Contains(text, "85/100; đạt 85% kế hoạch") {
					t.Fatalf("phụ lục:\n%s", text)
				}
			}
			if kind == "program" && !strings.Contains(text, "Kính gửi: Sở Nông nghiệp và Môi trường") {
				t.Fatalf("Mẫu 5 thiếu Kính gửi:\n%s", text)
			}
		})
	}
}

// TestW10_17_WordGolden: cùng bản đóng băng → cùng byte (băm khóa cứng — đổi mẫu phải tăng WordTemplateVersion và cập nhật
// có chủ ý); ô số/ngày trống để pf/export cấp.
func TestW10_17_WordGolden(t *testing.T) {
	d := wordDraft("quarter", map[string]string{"place": "An Giang", "report_date": "", "document_no": "", "signer": "Nguyễn Văn A", "signer_title": "GIÁM ĐỐC"})
	data := app.WordData(d, domain.Filter{}, 1)
	if data.Vars[app.VarNumber] != "" || data.Vars[app.VarPlaceDate] != "" {
		t.Fatalf("ô trống phải để pf/export cấp: %q %q", data.Vars[app.VarNumber], data.Vars[app.VarPlaceDate])
	}
	// Mô phỏng pf/export: số tự cấp và dòng ngày theo giờ Việt Nam.
	data.Vars[app.VarNumber] = "5/" + app.WordSeries
	data.Vars[app.VarPlaceDate] = doctpl.PlaceDateLine("An Giang", time.Date(2027, 1, 2, 18, 0, 0, 0, time.UTC))
	a := render(t, d, data)
	if _, err := doctpl.RenderDOCX(app.WordTemplate("mau_3"), app.WordData(d, domain.Filter{}, 1)); err == nil {
		t.Fatal("thiếu dòng địa danh/ngày (biến bắt buộc) phải lỗi khi không qua pf/export")
	}
	again := render(t, d, data)
	if !bytes.Equal(a, again) {
		t.Fatal("không tất định")
	}
	sum := sha256.Sum256(a)
	const golden = "969a48f00825449568001c15582480974d55ca47f01fad2c1aad140f2237c7ce"
	if got := hex.EncodeToString(sum[:]); got != golden {
		t.Fatalf("băm Word Mẫu 3 = %s (golden %s)", got, golden)
	}
	c, _ := docx.Extract(a)
	if !strings.Contains(c.Text(), "Số: 5/BC-TTKN") || !strings.Contains(c.Text(), "An Giang, ngày 03 tháng 01 năm 2027") {
		t.Fatalf("số/ngày:\n%s", c.Text())
	}
	// Bộ lọc khi xuất được ghi trong văn bản và chỉ giữ dòng thỏa (giữ cây cha).
	filtered := app.WordData(d, domain.Filter{Department: "trong_trot"}, 1)
	if filtered.Vars["loc"] != "Phòng Trồng trọt" || len(filtered.Lists["ct_trong_nam"]) != 2 {
		t.Fatalf("lọc: %q %v", filtered.Vars["loc"], filtered.Lists["ct_trong_nam"])
	}
}

func TestW10_17_VNNumber(t *testing.T) {
	for in, want := range map[string]string{"0": "0", "85": "85", "1234": "1.234", "1234567.891": "1.234.567,891", "-12500.5": "-12.500,5", "–": "–", "abc": "abc", "100": "100"} {
		if got := app.VNNumber(in); got != want {
			t.Errorf("VNNumber(%q) = %q, muốn %q", in, got, want)
		}
	}
}
