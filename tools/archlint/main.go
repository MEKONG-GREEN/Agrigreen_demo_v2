// Lệnh archlint là cổng CI cho các quy tắc kiến trúc không thể giao cho
// golangci-lint mặc định:
//
//	agboundary  R-02, R-03, R-04 — ranh giới module; gốc lắp ráp theo vùng
//	            (api-platform, integration-gateway, api-public, worker, drill, formimport ↛ mkg; api-internal, ingest,
//	            edge-agent, worker-internal ↛ platform; identity-vault ↛ platform, mkg);
//	            vault ↛ platform, mkg (vùng chủ quyền riêng, mục 4); port/ (và client/ của vault) chỉ
//	            interface + DTO — không import store/app/handler/adapter của chính module hay pgx (mục 8);
//	            ADR-0019 — cmd/edge-agent chỉ thư viện chuẩn + shared/{clock,ids,mass} + edgewire,
//	            edgewire chỉ thư viện chuẩn
//	agnofloat   R-10 — cấm float trong package tiền và khối lượng
//	agtenantgate H4 — tenant.WithID chỉ ở middleware xác thực / danh sách duyệt; tenant.MintGrant chỉ ở
//	            mã kiểm cấp quyền chia sẻ (pf/party); đổi sang tenant bên cấp chỉ qua tenant.Grant
//	agnotimenow R-12 — cấm time.Now trong domain/
//	agpiilog    R-14 — cấm log trường pii:"true"
//	agnoharddelete RB-1, ADR-0011 — cấm DELETE FROM / TRUNCATE trong internal/platform, internal/mkg
//	            (trừ hàm Rebuild*/Clear* dựng lại bảng chiếu từ sổ, R-22)
//	agmodgraph  W1.2, ADR-0027, R-MOD-05 — đồ thị module.yaml: không chu trình, chỉ đi xuống theo kind,
//	            import có trong depends, phiên bản thỏa, owns_schema khớp migration; ngoại lệ có tên
//	            trong modgraph_exceptions.yaml (chỉ được co lại)
//	agnoappname W9.1 — module kind: capability/kernel không so sánh với mã ứng dụng (app/*/app.yaml:
//	            KN, HTX, CGH, SIM, MKG, NH, PF); năng lực được cấu hình, không rẽ nhánh theo ứng dụng
//
// Chạy: go run ./tools/archlint ./...
package main

import "golang.org/x/tools/go/analysis/multichecker"

func main() {
	multichecker.Main(Boundary, NoFloat, NoTimeNow, PIILog, NoHardDelete, ModGraph, NoSessionSet, TenantGate, NoAppName)
}
