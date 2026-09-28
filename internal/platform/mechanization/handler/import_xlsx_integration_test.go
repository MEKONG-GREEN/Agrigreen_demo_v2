package handler_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tabular"
)

// machineXLSX dựng tệp Excel như người dùng gõ tay: năm, công suất là số; Ngày sở
// hữu là ô ngày thật (định dạng dd/mm/yyyy).
func machineXLSX(t *testing.T, rows [][]any, formulaCell string) []byte {
	t.Helper()
	f := excelize.NewFile()
	header := []any{"Chủng loại", "Hãng", "Model", "Năm SX", "Nhiên liệu", "Công suất", "Năng suất", "Mã CSH", "Số máy/SN", "Số khung", "Ngày sở hữu", "Tình trạng"}
	if err := f.SetSheetRow("Sheet1", "A1", &header); err != nil {
		t.Fatal(err)
	}
	custom := "dd/mm/yyyy"
	dateStyle, err := f.NewStyle(&excelize.Style{CustomNumFmt: &custom})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		cell, _ := excelize.CoordinatesToCellName(1, i+2)
		if err := f.SetSheetRow("Sheet1", cell, &r); err != nil {
			t.Fatal(err)
		}
		k, _ := excelize.CoordinatesToCellName(11, i+2)
		_ = f.SetCellStyle("Sheet1", k, k, dateStyle)
	}
	if formulaCell != "" {
		_ = f.SetCellFormula("Sheet1", formulaCell, "F2*2")
	}
	var buf bytes.Buffer
	if _, err := f.WriteTo(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// FN-05 BR-07/BR-10, US-MAC-02: nhập danh sách máy bằng Excel .xlsx ở chế độ
// "Thay thế toàn bộ" — máy vắng mặt chuyển Inactive bằng sự kiện, không xóa.
func TestImportMachinesXLSXReplace(t *testing.T) {
	f := newFixture(t, defaultNow)
	adm := f.adminUser()
	o := adm.owner(f.c1, "0905000011")
	ownerCode := str(adm.get("/owners/"+o).body, "code")
	a1 := adm.machine(f.c1, o, "MLD", "X1", "2025-02-01")
	a2 := adm.machine(f.c1, o, "MLD", "X2", "2025-02-01")

	importX := func(mode string, body []byte) resp {
		return adm.call(f.admin, http.MethodPost, "/v1/mechanization/machines/import?cooperative_ref="+f.c1.String()+"&mode="+mode, key(), tabular.ContentTypeXLSX, body)
	}
	// 45689 = 01/02/2025, 45731 = 15/03/2025 (số sê-ri ngày của Excel).
	body := machineXLSX(t, [][]any{
		{"MLD", "Iseki", "NT540", 2021, "Dầu", 40, 15, ownerCode, "X1", "", 45689, "Hoạt động"}, // cập nhật hãng
		{"MGD", "Kubota", "DC70", 2022, "Dầu", 70, 20, ownerCode, "X3", "", 45731, "Bảo trì"},   // mới
		{"XYZ", "Kubota", "X", 2020, "Dầu", 30, 10, ownerCode, "X9", "", 45689, "Hoạt động"},    // chủng loại sai
	}, "")
	r := adm.mustOK(importX("replace", body), http.StatusOK, "nhập .xlsx Thay thế toàn bộ").body
	if num(r, "created") != 1 || num(r, "updated") != 1 || num(r, "rejected") != 1 || len(list(r, "deactivated")) != 1 {
		t.Fatalf("kết quả nhập .xlsx: %v", r)
	}
	rows := list(r, "rows")
	if rows[2]["row"] != float64(4) || !strings.Contains(rows[2]["reason"].(string), "Dòng 4") {
		t.Fatalf("dòng lỗi báo đúng số dòng trang tính: %v", rows[2])
	}
	if g := adm.get("/machines/" + a1["ref"].(string)).body; str(g, "brand") != "Iseki" || g["active"] != true {
		t.Fatalf("X1 được cập nhật: %v", g)
	}
	created := adm.get("/machines/" + rows[1]["machine_ref"].(string)).body
	if str(created, "owned_since") != "2025-03-15" || str(created, "brand") != "Kubota" {
		t.Fatalf("ô ngày Excel đọc đúng: %v", created)
	}
	g := adm.get("/machines/" + a2["ref"].(string)).body
	if g["active"] != false || str(g, "deactivated_on") != "2026-09-27" {
		t.Fatalf("X2 vắng mặt → Inactive kèm Ngày vô hiệu hóa: %v", g)
	}
	hist := list(adm.do(f.admin, http.MethodGet, "/v1/mechanization/machines/"+a2["ref"].(string)+"/events", nil).body, "items")
	if last := hist[len(hist)-1]; last["action"] != "deactivated" || last["source"] != "import" {
		t.Fatalf("vô hiệu hóa là sự kiện trong sổ: %v", last)
	}

	// Ô công thức: từ chối cả tệp, không vô hiệu hóa máy nào.
	bad := importX("replace", machineXLSX(t, [][]any{
		{"MLD", "Iseki", "NT540", 2021, "Dầu", 40, 15, ownerCode, "X1", "", 45689, "Hoạt động"},
	}, "G2"))
	if bad.status != http.StatusUnprocessableEntity || str(bad.body, "code") != "mechanization.import_invalid" || !strings.Contains(str(bad.body, "detail"), "G2") {
		t.Fatalf("ô công thức phải bị từ chối: %d %v", bad.status, bad.body)
	}
	if act := list(adm.get("/machines?cooperative_ref="+f.c1.String()).body, "items"); len(act) != 2 {
		t.Fatalf("tệp bị từ chối không đổi máy nào (X1, X3 còn hoạt động): %v", act)
	}
}
