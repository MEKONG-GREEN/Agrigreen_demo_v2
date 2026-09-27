// Package e2e là kiểm thử chấp nhận đầu–cuối của cổng G1 (docs/uat/g1-acceptance.md):
// dựng toàn bộ nền tảng trong tiến trình (tests/e2e/harness) trên PostgreSQL thật
// của pgtest và chạy kịch bản "G1 — một vụ Đông Xuân ở Cần Thơ" chỉ qua HTTP,
// trừ hai chỗ mà ở production do worker làm: job leo thang SLA (đồng hồ giả) và
// handler inbox xuất gói MRV.
package e2e

import (
	"os"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }
