package cooperative

import (
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// DBModule: mọi giao dịch của module chạy SET LOCAL ROLE r_cooperative (ADR-0028, W3.3 — schema pf_coop). Pool truyền
// vào là vai đăng nhập của đơn vị triển khai (app_platform, app_worker, app_public) — là thành viên r_cooperative do
// `migrate bootstrap` cấp theo modreg.UnitModules. Module không có vai cũ cooperative_app.
var DBModule = shareddb.Module(Name)

// forModule bọc pool để mọi giao dịch mở qua nó chạy dưới vai của module (db.ForModule; nil giữ nil).
func forModule(b tenant.Beginner) tenant.Beginner { return shareddb.ForModule(b, DBModule) }
