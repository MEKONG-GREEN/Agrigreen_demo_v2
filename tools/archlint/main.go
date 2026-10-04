// Lệnh archlint là cổng CI cho các quy tắc kiến trúc không thể giao cho
// golangci-lint mặc định:
//
//	agboundary  R-02, R-03, R-04 — ranh giới module; gốc lắp ráp theo vùng
//	            (api-platform ↛ mkg; api-internal, ingest, edge-agent, worker-internal ↛ platform);
//	            ADR-0019 — cmd/edge-agent chỉ thư viện chuẩn + shared/{clock,ids,mass} + edgewire,
//	            edgewire chỉ thư viện chuẩn
//	agnofloat   R-10 — cấm float trong package tiền và khối lượng
//	agnotimenow R-12 — cấm time.Now trong domain/
//	agpiilog    R-14 — cấm log trường pii:"true"
//	agnoharddelete RB-1, ADR-0011 — cấm DELETE FROM / TRUNCATE trong internal/platform, internal/mkg
//	            (trừ hàm Rebuild*/Clear* dựng lại bảng chiếu từ sổ, R-22)
//	agmodgraph  W1.2, ADR-0027, R-MOD-05 — đồ thị module.yaml: không chu trình, chỉ đi xuống theo kind,
//	            import có trong depends, phiên bản thỏa, owns_schema khớp migration; ngoại lệ có tên
//	            trong modgraph_exceptions.yaml (chỉ được co lại)
//
// Chạy: go run ./tools/archlint ./...
package main

import "golang.org/x/tools/go/analysis/multichecker"

func main() {
	multichecker.Main(Boundary, NoFloat, NoTimeNow, PIILog, NoHardDelete, ModGraph)
}
