// Package e2e là kiểm thử chấp nhận đầu–cuối của cổng G1 (docs/uat/g1-acceptance.md):
// dựng toàn bộ nền tảng trong tiến trình (tests/e2e/harness) trên PostgreSQL thật
// của pgtest và chạy kịch bản "G1 — một vụ Đông Xuân ở Cần Thơ" chỉ qua HTTP,
// trừ hai chỗ mà ở production do worker làm: job leo thang SLA (đồng hồ giả) và
// handler inbox xuất gói MRV.
//
// Cổng G3 (docs/uat/g3-acceptance.md): TestG3FieldToCustomer dựng thêm phía MKG
// (cmd/api-internal/assembly, ingest mTLS) và thay outbox-publisher + RabbitMQ bằng bơm
// outbox gọi đúng sổ consumer của cmd/worker và cmd/worker-internal (g3_stack_test.go).
package e2e

import (
	"os"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }
