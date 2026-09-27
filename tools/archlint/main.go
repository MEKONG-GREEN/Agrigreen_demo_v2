// Lệnh archlint là cổng CI cho các quy tắc kiến trúc không thể giao cho
// golangci-lint mặc định:
//
//	agboundary  R-02, R-03, R-04 — ranh giới module
//	agnofloat   R-10 — cấm float trong package tiền và khối lượng
//	agnotimenow R-12 — cấm time.Now trong domain/
//	agpiilog    R-14 — cấm log trường pii:"true"
//
// Chạy: go run ./tools/archlint ./...
package main

import "golang.org/x/tools/go/analysis/multichecker"

func main() {
	multichecker.Main(Boundary, NoFloat, NoTimeNow, PIILog)
}
