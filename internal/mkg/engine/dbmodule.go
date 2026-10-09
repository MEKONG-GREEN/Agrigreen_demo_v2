package engine

import (
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: mọi giao dịch của module chạy SET LOCAL ROLE r_engine (ADR-0028). Pool truyền vào là vai đăng nhập của
// đơn vị triển khai (app_internal) — thành viên r_engine (vai nhóm suy từ registry, module sinh sau ADR-0028: không có
// vai cũ engine_app).
var DBModule = shareddb.Module("mkg/engine")

// forModule bọc pool để mọi giao dịch mở qua nó chạy dưới vai của module (db.ForModule; nil giữ nil).
func forModule(b tenant.Beginner) tenant.Beginner { return shareddb.ForModule(b, DBModule) }
