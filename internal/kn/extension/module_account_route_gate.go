package extension

import (
	"net/http"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
)

// ErrSurfaceRemoved (KN-STANDALONE-1-ADMIN-TOOLS #264): API của 10 công cụ nền tảng không có FN trong BRD 2.0 (tham số &
// phê duyệt, cây tổ chức G1, mẫu xuất & lịch gửi, xung đột/giám sát/đồng bộ thiết bị, Public API, đo lường, bảng kê, nhật
// ký xuất) bị từ chối trong phiên KN (X-AG-Context). Phiên nền tảng (không ngữ cảnh KN) giữ quyền theo vai realm; mã và dữ
// liệu của các module đó không đổi.
var ErrSurfaceRemoved = problem.New(http.StatusForbidden, "extension.surface_removed",
	"Chức năng nền tảng này không thuộc ứng dụng Khuyến nông (BRD 2.0) — không dùng được trong phiên Khuyến nông")

// removedRoutes: mẫu route (gin FullPath, cả Application API và Admin API) của các màn đã gỡ khỏi KN. Mục kết thúc "/"
// là tiền tố; mục khác so khớp đúng. Không chặn hạ tầng KN còn dùng: /v1/sync/push|pull, POST /v1/devices và
// /v1/devices/:id/push-token (đồng bộ ngoại tuyến field-app), /v1/masterdata/params/value (đọc tham số),
// /v1/orgs/tree|:ref/move|region|impact (Cơ cấu tổ chức 3 cấp FN-AD-04.02), /v1/reports/forms… (phát hành, chốt kỳ).
var removedRoutes = []string{
	// /quan-tri/tham-so
	"/v1/masterdata/params",
	"/v1/masterdata/params/versions",
	"/v1/masterdata/params/versions/",
	"/v1/masterdata/params/restore-default",
	// /quan-tri/to-chuc (cây tổ chức G1 — KN dùng /kn/quan-tri/co-cau-to-chuc)
	"/v1/orgs",
	"/v1/orgs/export.xlsx",
	"/v1/orgs/import",
	"/v1/orgs/:ref/rename",
	"/v1/orgs/:ref/events",
	// /quan-tri/mau-bao-cao
	"/v1/reports/templates",
	"/v1/reports/templates/",
	"/v1/reports/schedules",
	"/v1/reports/schedules/",
	// /quan-tri/xung-dot, /quan-tri/giam-sat-dong-bo, /dong-bo
	"/v1/sync/conflicts",
	"/v1/sync/conflicts/",
	"/v1/geo/conflicts",
	"/v1/geo/conflicts/",
	"/v1/sync/integrations",
	"/v1/sync/integrations/",
	"/v1/sync/nudge",
	"/v1/devices/:id/revoke",
	// /quan-tri/khach-hang-api, /quan-tri/su-dung-api, /quan-tri/bang-ke, /quan-tri/nhat-ky-xuat
	"/v1/api-clients",
	"/v1/api-clients/",
	"/v1/metering/",
}

// accountRouteGate là RouteGate của accountsport.SessionPolicies: route thuộc công cụ đã gỡ → ErrSurfaceRemoved.
// GET /v1/devices (danh sách thiết bị của màn /dong-bo) chặn theo phương thức vì POST /v1/devices là đăng ký thiết bị
// của field-app KN.
func accountRouteGate(method, route string) error {
	if route == "/v1/devices" {
		if method == http.MethodGet {
			return ErrSurfaceRemoved
		}
		return nil
	}
	for _, r := range removedRoutes {
		if r == route || strings.HasSuffix(r, "/") && strings.HasPrefix(route, r) {
			return ErrSurfaceRemoved
		}
	}
	return nil
}
