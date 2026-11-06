package app

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/outbreak/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/doctpl"
)

// #252 (UR-XA-10, FN-11 "xuất Word theo mẫu"): Word BM-10 ghi DB-15 số hộ, bảng hộ DB-04/DB-14, DB-16 khi có; dữ liệu cũ v1
// ghi 1 hộ, bảng hộ rỗng.
func TestWordDataHouseholds(t *testing.T) {
	day := time.Date(2027, 10, 12, 0, 0, 0, 0, time.UTC)
	farmed := decimal.RequireFromString("2.5")
	v := OutbreakView{DiseaseName: "Hoại tử gan tụy", SpeciesName: "Tôm", OutbreakRow: store.OutbreakRow{Outbreak: domain.Outbreak{ReceivedAt: day,
		AsOf: day, HerdSize: 10, Source: "can_bo", Symptoms: "Tôm chết", CommuneCode: "30001",
		AquaFarmedHa: &farmed}, Meta: store.Meta{Seq: 1, Event: domain.EventReported}}, Status: domain.StatusReported, Affected: 2,
		Households: []HouseholdView{
			{HouseholdLine: store.HouseholdLine{Code: "HCN0000001", Hamlet: "Ấp 1", LivestockType: "nong_ho"}, OwnerLabel: "Nguyễn V. A."},
			{HouseholdLine: store.HouseholdLine{Code: "HCN0000002", LivestockType: "trang_trai_vua"}},
		}}
	d := WordData([]OutbreakView{v}, "Xã A")
	hs := d.Lists[listHouseholds]
	if d.Vars["so_ho"] != "2" || len(hs) != 2 || hs[0]["chu_ho"] != "Nguyễn V. A." || hs[1]["loai"] != "Trang trại vừa" ||
		hs[1]["dia_chi"] != "—" || d.Vars["thuy_san"] != "nuôi trồng 2.5 ha, khai thác 0 ha" {
		t.Fatalf("dữ liệu Word: %v %v", d.Vars, hs)
	}
	if err := doctpl.Validate(WordTemplate()); err != nil {
		t.Fatalf("mẫu: %v", err)
	}
	d.Vars[VarNumber], d.Vars[VarPlaceDate] = "1/BC-TTKN", "An Giang, ngày 13 tháng 10 năm 2027"
	if _, err := doctpl.RenderDOCX(WordTemplate(), d); err != nil {
		t.Fatalf("dựng Word: %v", err)
	}
	legacy := v
	legacy.Households, legacy.Affected, legacy.AquaFarmedHa, legacy.Legacy = []HouseholdView{}, 1, nil, true
	d = WordData([]OutbreakView{legacy}, "Xã A")
	if d.Vars["so_ho"] != "1" || len(d.Lists[listHouseholds]) != 0 || d.Vars["thuy_san"] != "" {
		t.Fatalf("dữ liệu cũ: %v", d.Vars)
	}
}
