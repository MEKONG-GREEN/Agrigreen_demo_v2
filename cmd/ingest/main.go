// Lệnh ingest: nhận số đo từ agent biên và telemetry qua mTLS (mục 13.2, 14).
// Thuộc module mkg/measurement — làm ở G3 khi chốt mẫu cân (câu hỏi C-6).
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ingest: chưa triển khai — thuộc cổng G3 (mkg/measurement, chờ C-6)")
	os.Exit(2)
}
