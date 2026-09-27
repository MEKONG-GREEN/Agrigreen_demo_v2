// Lệnh edge-agent: nhị phân tĩnh chạy tại hub, đọc cân PLC/IoT, đệm đĩa ≥72 giờ,
// ký Ed25519 tại nguồn, đẩy theo lô qua mTLS (mục 14). Làm ở G3 theo mẫu cân C-6.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "edge-agent: chưa triển khai — thuộc cổng G3 (chờ mẫu cân và giao thức, C-6)")
	os.Exit(2)
}
