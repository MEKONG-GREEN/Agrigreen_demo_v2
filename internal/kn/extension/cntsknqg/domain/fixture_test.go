package domain

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixturePath là đặc tả 5 mẫu KNQG cho giả lập web (apps/web-ops) — một nguồn sự thật: sinh từ đặc tả Go, không sửa tay.
var fixturePath = filepath.Join("..", "..", "..", "..", "..", "apps", "web-ops", "src", "api", "mock", "fixtures", "knqg-specs.json")

func fixture() []byte {
	rows := map[string][]Row{Form05: IndicatorRows(Form05), Form07: IndicatorRows(Form07)}
	raw, err := json.MarshalIndent(map[string]any{"specs": Specs(), "indicator_rows": rows, "doc_checklist": DocChecklist}, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(raw, '\n')
}

// TestWebSpecsFixture: AG_UPDATE_WEB_FIXTURES=1 ghi lại tệp; mặc định kiểm tệp khớp đặc tả.
func TestWebSpecsFixture(t *testing.T) {
	want := fixture()
	if os.Getenv("AG_UPDATE_WEB_FIXTURES") == "1" {
		if err := os.WriteFile(fixturePath, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("thiếu %s — chạy AG_UPDATE_WEB_FIXTURES=1 go test ./internal/kn/extension/cntsknqg/domain: %v", fixturePath, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n")), want) {
		t.Fatalf("%s lệch đặc tả Go — chạy AG_UPDATE_WEB_FIXTURES=1 go test ./internal/kn/extension/cntsknqg/domain", fixturePath)
	}
}
