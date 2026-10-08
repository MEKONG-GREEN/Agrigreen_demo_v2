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
		// Gốc lắp ráp vùng nền tảng còn thiếu (worker-platform, diễn tập, bộ nạp mẫu) — không nối mkg/*.
		"example.com/ag/cmd/worker",
		"example.com/ag/cmd/drill",
		"example.com/ag/cmd/formimport",
		// Mục 4, RB-3: vault là vùng chủ quyền riêng.
		"example.com/ag/internal/vault/identity/app",
		// Mục 8: port/ (client/ của vault) chỉ interface + DTO; ngoại lệ có tên grantlookup (review M9).
		"example.com/ag/internal/platform/delta/port/bad",
		"example.com/ag/internal/vault/identity/client/bad",
		"example.com/ag/internal/platform/party/port/grantlookup",
	)
}

// H4: tenant.WithID chỉ ở middleware / danh sách duyệt (theo tệp); tenant.MintGrant chỉ ở pf/party (và tệp
// có tên); bản giả porttest chỉ import từ _test.go; tệp _test.go và chính gói tenant được miễn.
func TestTenantGate_H4(t *testing.T) {
	if err := Boundary.Flags.Set("module", "example.com/ag"); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), TenantGate,
		"example.com/ag/internal/shared/tenant",
		"example.com/ag/internal/shared/httpx",
		"example.com/ag/internal/platform/party/app",
		"example.com/ag/internal/platform/party/port/porttest",
		"example.com/ag/internal/platform/beta/tenantuse",
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
		// mkg/fieldops, pf/mechanization: cấm float trong mã không phải kiểm thử (tệp _test.go miễn).
		"example.com/ag/internal/mkg/fieldops/domain",
		"example.com/ag/internal/platform/mechanization/app",
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

// W9.1: module capability/kernel không so sánh với mã ứng dụng trong app/*/app.yaml; domain được phép.
func TestNoAppName(t *testing.T) {
	if err := Boundary.Flags.Set("module", "example.com/ag"); err != nil {
		t.Fatal(err)
	}
	analysistest.Run(t, analysistest.TestData(), NoAppName,
		"example.com/ag/internal/platform/capx/app",
		"example.com/ag/internal/platform/kernx",
		"example.com/ag/internal/platform/domx",
	)
}
