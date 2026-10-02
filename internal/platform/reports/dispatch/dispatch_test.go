package dispatch

import (
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
)

// FN-TI-02.06 / RB-7 (gap §4.3, ADR-0026 "Đề xuất"): Google Sheet TẮT mặc định — chỉ bật khi đủ cờ, mã ADR đã chấp nhận
// và allowlist; van mặc định chặn mọi cột không phải số/nhãn tổ chức.
func TestFN_TI_02_06_GSheetDisabledByDefaultAndValve(t *testing.T) {
	env := map[string]string{}
	get := func(k string) string { return env[k] }
	if GSheetConfigFromEnv(get).Enabled {
		t.Fatal("mặc định tắt")
	}
	env["AG_REPORTS_GSHEET"] = "on"
	if GSheetConfigFromEnv(get).Enabled {
		t.Fatal("thiếu ADR/allowlist vẫn tắt")
	}
	env["AG_EXTDATA_GSHEET_ADR"], env["AG_NONPII_ALLOWLIST"] = "ADR-0026", "weather, reports.gsheet"
	if c := GSheetConfigFromEnv(get); !c.Enabled || c.ShareMode != "anyone_with_link" {
		t.Fatalf("đủ điều kiện: %+v", c)
	}
	def := port.FormDefinition{Code: "T01", Title: "Tiến độ", Columns: []port.FormColumn{
		{Letter: "A", Kind: port.ColumnFixed, DType: port.DTypeInteger, FixedSource: port.FixedRowNo},
		{Letter: "C", Kind: port.ColumnFixed, DType: port.DTypeText, FixedSource: port.FixedCommuneName},
		{Letter: "F", Kind: port.ColumnInput, DType: port.DTypeNumber},
	}}
	sh, err := Valve(def, []map[string]string{{"A": "1", "C": "Xã A", "F": "12.5"}})
	if err != nil || len(sh.Rows) != 1 || sh.Rows[0][2] != "12.5" {
		t.Fatalf("van cho số + nhãn: %+v %v", sh, err)
	}
	if _, err := Valve(def, []map[string]string{{"F": "abc"}}); !errors.Is(err, ErrValve) {
		t.Fatal("ô số không phải số bị chặn")
	}
	def.Columns = append(def.Columns, port.FormColumn{Letter: "N", Kind: port.ColumnInput, DType: port.DTypeText})
	if _, err := Valve(def, nil); !errors.Is(err, ErrValve) {
		t.Fatal("cột chữ tự do (có thể chứa PII) bị chặn")
	}
}

func TestFN_TI_02_06_MessageHelpers(t *testing.T) {
	if !ValidAddress("trongtrot.chicuc@angiang.gov.vn") || ValidAddress("a@b") || ValidAddress("x@y.vn\r\nBcc: z@w.vn") || ValidAddress("Tên <a@b.vn>") {
		t.Fatal("kiểm địa chỉ hộp thư")
	}
	if Mask("trongtrot@angiang.gov.vn") != "tr•••@angiang.gov.vn" {
		t.Fatal(Mask("trongtrot@angiang.gov.vn"))
	}
	if s := Subject("Phòng Trồng trọt", "Tuần 36", "T01", true); !strings.HasSuffix(s, "(Tạm tính)") {
		t.Fatal(s)
	}
	b := Body("Tuần 36", "31/08–06/09/2026", []string{"T01"}, true, []string{"Xã A"}, []string{"T01.xlsx: https://x"})
	if !strings.Contains(b, "TẠM TÍNH") || !strings.Contains(b, "Xã A") || !strings.Contains(b, "https://x") {
		t.Fatal(b)
	}
	if FileName("T01", "Tuần 1/2", "xlsx") != "T01_Tuần 1_2.xlsx" {
		t.Fatal(FileName("T01", "Tuần 1/2", "xlsx"))
	}
}
