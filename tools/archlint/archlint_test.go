package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestBoundary(t *testing.T) {
	if err := Boundary.Flags.Set("module", "example.com/ag"); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), Boundary,
		"example.com/ag/internal/platform/alpha/app",
		"example.com/ag/internal/shared/x",
		"example.com/ag/cmd/app",
		"example.com/ag/cmd/identity-vault",
		"example.com/ag/cmd/api-internal",
		"example.com/ag/cmd/api-platform",
		// G3: gốc lắp ráp vùng mkg + luật agent biên (ADR-0019).
		"example.com/ag/cmd/edge-agent",
		"example.com/ag/cmd/worker-internal",
		"example.com/ag/cmd/ingest",
		"example.com/ag/internal/mkg/measurement/port/edgewire",
		// G5-0: Public API — gốc lắp ráp vùng nền tảng, không import vault kể cả client (RB-3).
		"example.com/ag/cmd/api-public",
	)
}

func TestNoFloat(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NoFloat,
		"example.com/ag/internal/mkg/costing",
		"example.com/ag/internal/mkg/hub",
		// G4-0: bốn module mkg mới — logistics theo vùng, orders/acctbridge theo tên đoạn (kể cả package con).
		"example.com/ag/internal/mkg/logistics/app",
		"example.com/ag/internal/mkg/orders/domain",
		"example.com/ag/internal/mkg/acctbridge/adapter",
		"example.com/ag/internal/platform/beta/free",
		// G5-0: pf/metering (giá, thành tiền) — theo tên đoạn, kể cả package con.
		"example.com/ag/internal/platform/metering/app",
	)
}

func TestNoTimeNow(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NoTimeNow, "example.com/ag/internal/platform/beta/domain")
}

func TestPIILog(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), PIILog, "example.com/ag/internal/platform/beta/pii")
}

// RB-1, ADR-0011: không xóa cứng dữ liệu nghiệp vụ; chỉ Rebuild*/Clear* dựng lại bảng chiếu.
func TestNoHardDelete_ADR0011(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NoHardDelete, "example.com/ag/internal/platform/beta/store")
}

// ADR-0028, R-DB-02/03: không trạng thái mức phiên trong chuỗi SQL; SET LOCAL ROLE chỉ trong shared/db.
func TestNoSessionSet_ADR0028(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), NoSessionSet,
		"example.com/ag/internal/platform/beta/sqlsess",
		"example.com/ag/internal/shared/db",
	)
}
