package app

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// readTable tự giới hạn nguồn (MaxImportBytes) — vượt → lỗi rõ ràng, không đọc hết vào bộ nhớ.
func TestReadTableSizeLimit(t *testing.T) {
	rows, err := readTable(FormatCSV, strings.NewReader("email,full_name\na@b.vn,Nguyen Van A\n"))
	if err != nil || len(rows) != 2 {
		t.Fatalf("CSV hợp lệ: %v, %v", rows, err)
	}
	big := bytes.Repeat([]byte("a"), MaxImportBytes+1)
	for _, f := range []string{FormatCSV, FormatXLSX} {
		_, err := readTable(f, bytes.NewReader(big))
		var p *problem.Problem
		if !errors.As(err, &p) || !strings.Contains(p.Detail, "MB") {
			t.Errorf("%s vượt giới hạn: lỗi = %v", f, err)
		}
	}
}
