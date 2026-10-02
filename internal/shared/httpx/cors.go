package httpx

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORSEnv là biến môi trường chứa danh sách origin được phép, phân tách bằng dấu phẩy,
// vd "http://localhost:3000,https://ops.agrigreen.vn".
const CORSEnv = "AG_CORS_ORIGINS"

// Header CORS cố định (mục 11). Bearer token đi qua header Authorization, không
// dùng cookie — nên KHÔNG bật Access-Control-Allow-Credentials và không bao giờ
// trả "*".
var (
	corsAllowMethods  = "GET, POST, PUT, PATCH, DELETE"
	corsAllowHeaders  = "Authorization, Content-Type, Idempotency-Key, X-Device-Id, X-Purpose, X-AG-Context, X-AG-Reason, traceparent"
	corsExposeHeaders = "X-Trace-Id, Idempotent-Replayed, Location"
	corsMaxAge        = "600"
)

// ParseCORSOrigins đọc danh sách origin. Mỗi mục phải là origin đầy đủ
// scheme://host[:port], không có đường dẫn, query hay dấu "/" cuối. Ký tự đại
// diện "*" bị từ chối: so khớp chỉ theo đúng từng origin.
func ParseCORSOrigins(s string) ([]string, error) {
	var out []string
	for _, raw := range strings.Split(s, ",") {
		o := strings.TrimSpace(raw)
		if o == "" {
			continue
		}
		if strings.Contains(o, "*") {
			return nil, fmt.Errorf("httpx: %s không được chứa ký tự đại diện: %q", CORSEnv, o)
		}
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" ||
			u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return nil, fmt.Errorf("httpx: origin không hợp lệ trong %s: %q (dạng scheme://host[:port])", CORSEnv, o)
		}
		o = strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host)
		if !slices.Contains(out, o) {
			out = append(out, o)
		}
	}
	return out, nil
}

// CORSOriginsFromEnv đọc AG_CORS_ORIGINS. Không đặt → danh sách rỗng (không
// origin trình duyệt nào được gọi chéo).
func CORSOriginsFromEnv() ([]string, error) {
	return ParseCORSOrigins(os.Getenv(CORSEnv))
}

// CORS trả middleware CORS so khớp origin CHÍNH XÁC với danh sách cho phép.
//
//   - Request không có header Origin (server-to-server, app di động) đi qua bình thường.
//   - Origin được phép: phản hồi mang Access-Control-Allow-Origin = đúng origin đó,
//     Vary: Origin và Access-Control-Expose-Headers.
//   - Preflight (OPTIONS + Access-Control-Request-Method) từ origin được phép → 204;
//     từ origin khác → 403, không có header CORS.
//   - Request thường từ origin không được phép vẫn chạy nhưng không có header
//     CORS — trình duyệt chặn đọc phản hồi. Kiểm quyền vẫn do backend (R-55).
//
// Gắn bằng engine.Use NGAY sau NewEngine, trước khi khai báo route, để cả
// preflight tới route chỉ có GET/POST (đi vào nhánh 405 của Gin) cũng qua đây.
func CORS(origins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		allowed[strings.ToLower(o)] = struct{}{}
	}
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		h := c.Writer.Header()
		h.Add("Vary", "Origin")
		_, ok := allowed[strings.ToLower(origin)]
		preflight := c.Request.Method == http.MethodOptions && c.GetHeader("Access-Control-Request-Method") != ""
		if preflight {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
			if !ok {
				c.AbortWithStatus(http.StatusForbidden)
				return
			}
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Methods", corsAllowMethods)
			h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
			h.Set("Access-Control-Max-Age", corsMaxAge)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		if ok {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", corsExposeHeaders)
		}
		c.Next()
	}
}
