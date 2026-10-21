package extension

import geoport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/geo/port"

// UseCNTSMapGeometry (W11.52) nối phép tính trên ranh giới hành chính của pf/geo cho bản đồ vùng nuôi, vùng dịch CNTS:
// vùng vẽ nằm hoàn toàn ngoài địa giới các xã đã chọn → cảnh báo (FN-18 luồng ngoại lệ, không chặn). Chưa nối → không
// cảnh báo.
func (m *Module) UseCNTSMapGeometry(g geoport.AdminGeometry) { m.zmap.Geometry = g }
