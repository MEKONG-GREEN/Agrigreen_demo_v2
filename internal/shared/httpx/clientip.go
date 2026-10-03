package httpx

import (
	"log/slog"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
)

// TrustedProxiesEnv liệt kê CIDR/IP của proxy được tin (vd dải pod của Traefik), cách nhau dấu phẩy.
// Chỉ khi kết nối tới từ các địa chỉ này Gin mới đọc X-Forwarded-For/X-Real-IP cho ClientIP;
// rỗng → không tin header nào, ClientIP = địa chỉ kết nối (không giả mạo được).
const TrustedProxiesEnv = "AG_TRUSTED_PROXIES"

// configureProxies đặt danh sách proxy tin cậy cho engine. Mặc định của Gin là tin MỌI
// proxy — khi đó người gọi tự đặt X-Forwarded-For để giả IP (sổ truy cập, giới hạn theo IP).
func configureProxies(r *gin.Engine, log *slog.Logger) {
	var list []string
	for _, s := range strings.Split(os.Getenv(TrustedProxiesEnv), ",") {
		if s = strings.TrimSpace(s); s != "" {
			list = append(list, s)
		}
	}
	if err := r.SetTrustedProxies(list); err != nil {
		// CIDR sai: không tin proxy nào (an toàn) và báo lỗi cấu hình.
		_ = r.SetTrustedProxies(nil)
		log.Error("AG_TRUSTED_PROXIES không hợp lệ — bỏ qua, không tin proxy nào", "event", "startup", "outcome", "error", "error", err.Error())
	}
}
