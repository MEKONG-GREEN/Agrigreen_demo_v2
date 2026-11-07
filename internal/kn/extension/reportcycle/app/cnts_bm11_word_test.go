package app

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/reportcycle/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

// Mẫu Word BM-11 hợp lệ (đăng ký được vào pf/export) và dựng được .docx cho cả tuần lẫn tháng.
func TestBM11WordTemplates(t *testing.T) {
	ts := BM11WordTemplates()
	if len(ts) != 4 {
		t.Fatalf("bốn mẫu (tuần, kỳ × v1, v2): %d", len(ts))
	}
	for _, tpl := range ts {
		if err := doctpl.Validate(tpl); err != nil {
			t.Fatal(err)
		}
	}
	mgr := uuid.New()
	c := domain.BM11Content{Livestock: map[string]string{"tong_dan_heo": "467495", "hoa_chat_gia_suc": "696.5"},
		Fishery:    domain.BM11Catch{CatchCur: "2858.5", CatchYTD: "103318.86", AquaCur: "38632.42", AquaYTD: "1698301.69", TotalCur: "41490.92"},
		Coverage:   []domain.BM11Coverage{{Form: domain.CNTSBM04, Expected: 102, Reported: 99, Periods: 1, Missing: []string{"1", "2", "3"}, MissingNames: []string{"Xã A", "Xã B", "Xã C"}}},
		Programs:   []domain.BM11Program{{Ref: uuid.NewString(), Name: "Chương trình phát triển chăn nuôi gia súc", Kind: "tinh", ManagerRef: mgr.String(), Sites: 15, Households: 15, Text: "Đã chọn xong 15/15 điểm."}},
		Sections:   map[string]string{"ke_hoach": "Tiếp tục điều tra tổng đàn.", "phuong_huong": "Đẩy mạnh chọn điểm."},
		Supplement: []domain.BM11Supplement{{Period: "Tuần 38", Label: "Tiêm phòng — Heo", Unit: "liều", Sent: "10", Now: "12"}}}
	herds := domain.BM11HerdsOf(domain.BM11HerdInput{Counts: []domain.BM11HerdCount{{Commune: "XA", Type: domain.BM11HerdHousehold, Households: 8},
		{Commune: "XA", Type: domain.BM11HerdFarmSmall, Households: 3}, {Commune: "XA", Type: domain.BM11HerdFarmMedium, Households: 1}},
		Heads: []domain.BM11HeadCount{{Species: "heo", Type: domain.BM11HerdFarmSmall, Heads: "1460"}}}, 102)
	herds.Communes[0].Name = "Xã Mỹ Hòa Hưng"
	c.Herds = &herds
	for _, typ := range []string{domain.BM11Week, domain.BM11Month} {
		r := store.BM11Report{ID: uuid.New(), Type: typ, PeriodKey: "2026-W39", Label: "Tuần 39", Recipients: domain.BM11DefaultRecipients(typ),
			From: time.Now(), To: time.Now()}
		d := BM11WordData(r, c, domain.BM11Sent, "An Giang", "abc", map[string]string{mgr.String(): "Huỳnh Văn Nhân"})
		d.Vars[bm11VarNumber], d.Vars[bm11VarPlaceDate] = "39/BC-CNTS", "An Giang, ngày 29 tháng 09 năm 2026"
		tpl := ts[2]
		if typ != domain.BM11Week {
			tpl = ts[3]
		}
		hs := d.Lists[bm11ListHerds]
		if len(hs) != 2 || hs[0]["tong_ho"] != "12" || hs[0]["trang_trai"] != "4" || hs[1]["xa"] != "Toàn tỉnh (1/102 xã có dữ liệu)" ||
			hs[1]["tong"] != "1" || d.Lists[bm11ListHeads][0]["trang_trai"] != "1.460" {
			t.Fatalf("FN-16 BR-07 bảng theo loại chăn nuôi: %v %v", hs, d.Lists[bm11ListHeads])
		}
		for _, old := range ts[:2] {
			if _, err := doctpl.RenderDOCX(old, d); err != nil {
				t.Fatalf("mẫu v1 vẫn dựng được: %v", err)
			}
		}
		if d.Vars["tong_hop_xa"] != "Tổng hợp từ 99/102 xã" {
			t.Fatalf("AC-03: %q", d.Vars["tong_hop_xa"])
		}
		if d.Vars["khai_thac"] != "2.858,5" || d.Vars["nuoi_trong_luy_ke"] != "1.698.301,69" {
			t.Fatalf("định dạng số VN: %q %q", d.Vars["khai_thac"], d.Vars["nuoi_trong_luy_ke"])
		}
		b, err := doctpl.RenderDOCX(tpl, d)
		if err != nil || len(b) < 1000 {
			t.Fatalf("%s: %v (%d byte)", typ, err, len(b))
		}
	}
}

func TestViNum(t *testing.T) {
	for in, want := range map[string]string{"0": "0", "467495": "467.495", "-1234.5": "-1.234,5", "": "0", "1000000": "1.000.000"} {
		if got := viNum(in); got != want {
			t.Fatalf("viNum(%q) = %q, muốn %q", in, got, want)
		}
	}
}
