// Lệnh ingest: nhận lô số đo có chữ ký nguồn từ agent biên qua mTLS (mục 13.2,
// 14; ADR-0019), ghi ts_weigh (cụm ts, vai ingest_ts) và tích phân phiếu cân (R-70,
// cụm mkg, vai ingest_mkg). Gốc lắp ráp vùng mkg — không import internal/platform/*.
// Hợp đồng đã đóng băng ở G3-0: internal/mkg/measurement/port (+ edgewire).
// Hiện thực ở G3-1 (đợt A2).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ingest: chưa triển khai — G3-1 đợt A2 (hợp đồng measurement/port + edgewire đã có)")
	os.Exit(2)
}
