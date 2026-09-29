// Lệnh edge-agent: nhị phân tĩnh chạy tại hub (vùng biên OT), đọc cân PLC/IoT,
// đệm đĩa ≥72 giờ (đầy → ngừng + báo động), ký Ed25519 tại nguồn, đẩy theo lô qua
// mTLS chỉ đi ra (mục 14; ADR-0019). Hợp đồng dây đã đóng băng ở G3-0:
// internal/mkg/measurement/port/edgewire + contracts/edge/batch.v1.json.
//
// Ranh giới (tools/archlint): chỉ import thư viện chuẩn, internal/shared/{clock,ids,mass}
// và edgewire — không pgx, không DB, không module nghiệp vụ. Hiện thực ở G3-1 (đợt A3);
// bộ điều hợp mẫu cân chờ C-6.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "edge-agent: chưa triển khai — G3-1 đợt A3 (hợp đồng edgewire đã có; mẫu cân chờ C-6)")
	os.Exit(2)
}
