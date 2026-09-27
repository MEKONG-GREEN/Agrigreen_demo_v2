package httpx

import (
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
)

// routeLimits: giới hạn thân request riêng theo route (method + đường dẫn đầy đủ
// của Gin, vd "POST /v1/documents/:ref/attachments"). Dùng cho đường tải tệp
// có giới hạn riêng lớn hơn giới hạn chung của engine.
var routeLimits sync.Map

// AllowBody đặt giới hạn thân request riêng cho một route sắp gắn vào rg (thường
// là route tải tệp multipart). Gọi trong RegisterApp/RegisterAdmin của module,
// cùng đường dẫn tương đối như khi gắn route. BodyLimit của engine dùng giới
// hạn này thay cho giới hạn chung.
func AllowBody(rg *gin.RouterGroup, method, relativePath string, n int64) {
	full := path.Join(rg.BasePath(), relativePath)
	if strings.HasSuffix(relativePath, "/") && !strings.HasSuffix(full, "/") {
		full += "/"
	}
	routeLimits.Store(method+" "+full, n)
}

func routeLimit(c *gin.Context, def int64) int64 {
	if v, ok := routeLimits.Load(c.Request.Method + " " + c.FullPath()); ok {
		return v.(int64)
	}
	return def
}

// BodyLimit giới hạn kích thước thân request (giới hạn riêng theo route nếu có, xem AllowBody).
func BodyLimit(n int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := routeLimit(c, n)
		if limit > 0 && c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}
