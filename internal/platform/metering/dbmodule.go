package metering

import (
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: mọi giao dịch của module chạy SET LOCAL ROLE r_metering (ADR-0028, W2.6). Pool truyền vào là vai đăng
// nhập của đơn vị triển khai (app_*), hoặc vai cũ trong kỳ chuyển — là thành viên r_metering.
var DBModule = shareddb.Module("pf/metering")

// forModule bọc pool để mọi giao dịch mở qua nó chạy dưới vai của module (db.ForModule; nil giữ nil).
func forModule(b tenant.Beginner) tenant.Beginner { return shareddb.ForModule(b, DBModule) }
