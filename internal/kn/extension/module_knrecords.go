package extension

// Nối hồ sơ KN riêng HTX / Nông hộ / Tổ hợp tác (KNSPLIT-2-KN-RECORDS, #67): sổ pf_extension.kn_producer của KN thay
// đường ghi legacy /v1/kn/cooperatives (pf/party, pf/cooperative). Từ #306 KN không còn đọc HTX đã công bố (liên kết
// đối chiếu hoãn tới pha kết nối); không gọi module HTX trong tiến trình, không ghi dữ liệu HTX.

import (
	"github.com/gin-gonic/gin"

	krapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knrecords/app"
	krhandler "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/knrecords/handler"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
)

// newKNRecords dựng use case trên pool đã gắn vai của module.
func (m *Module) newKNRecords(moduleDB tenant.Beginner, clk clock.Clock, units partyport.AdminUnits) *krapp.Service {
	return &krapp.Service{DB: moduleDB, Clock: clk, Units: units, Names: func() partyport.ActorNames { return m.rc.Names }}
}

// UseKNRecordsVault nối kho định danh cho hồ sơ KN riêng: họ tên, SĐT người đại diện/chủ hộ (mục đích extension.contact).
// Chưa nối → thêm/sửa người đại diện trả 503, đọc hiện "Không hiển thị được".
func (m *Module) UseKNRecordsVault(v client.Client) { m.records.Vault = v }

func (m *Module) registerKNRecords(rg *gin.RouterGroup) {
	krhandler.Handler{Svc: m.records}.RegisterApp(rg)
}
