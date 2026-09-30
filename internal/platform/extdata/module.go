// Package extdata là module pf/extdata: proxy KHỬ ĐỊNH DANH cho nguồn dữ liệu ngoài
// (thời tiết, độ mặn, tile bản đồ/vệ tinh) theo ADR-0007 và RB-7.
//
//   - Không gửi định danh: yêu cầu đi ra dựng mới, context gỡ Principal/token/tenant,
//     không header của người gọi (provider.HTTP, app.Detached).
//   - Tọa độ làm thô theo lưới (mặc định 0,1°, tối thiểu 0,05°); tile giới hạn zoom.
//   - Danh mục nguồn theo cấu hình, mặc định chặn (AG_EXTDATA_SOURCES); nguồn ngoài
//     lãnh thổ ở production cần AG_NONPII_ALLOWLIST và ADR riêng (app.LoadConfig).
//   - Bộ nhớ đệm theo ô/tile dùng chung giữa người gọi (app.Cache).
//
// Module không có bảng (không vai DB): dữ liệu ngoài là dữ liệu tham khảo, không
// phải dữ liệu nghiệp vụ. Nhà cung cấp cụ thể chờ khách chọn (tong-hop.md Q2-10).
package extdata

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/handler"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extdata/provider"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Module là pf/extdata đã nối.
type Module struct{ svc *app.Service }

// New dựng module từ nguồn đã có nhà cung cấp (kiểm thử, gốc lắp ráp).
func New(clk clock.Clock, log *slog.Logger, sources []app.Source) (*Module, error) {
	svc, err := app.NewService(clk, log, sources, app.DefaultCacheEntries)
	if err != nil {
		return nil, err
	}
	return &Module{svc: svc}, nil
}

// FromEnv đọc danh mục nguồn (app.LoadConfig) và dựng nhà cung cấp: "fake" → provider.Fake,
// còn lại → provider.HTTP theo mẫu URL. Không có nguồn nào → module chỉ trả danh sách rỗng.
func FromEnv(getenv func(string) string, clk clock.Clock, log *slog.Logger) (*Module, error) {
	cfgs, err := app.LoadConfig(getenv)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	sources := make([]app.Source, 0, len(cfgs))
	for _, c := range cfgs {
		s := app.Source{Info: c.Info}
		if c.Info.Fake {
			f := &provider.Fake{}
			s.Point, s.Tile = f, f
		} else {
			p := provider.NewHTTP(c.URL, client)
			s.Point, s.Tile = p, p
		}
		sources = append(sources, s)
	}
	return New(clk, log, sources)
}

// RegisterApp gắn route Application API.
func (m *Module) RegisterApp(rg *gin.RouterGroup) { handler.Handler{Svc: m.svc}.RegisterApp(rg) }

// RegisterAdmin: không có route quản trị (danh mục nguồn là cấu hình triển khai).
func (m *Module) RegisterAdmin(*gin.RouterGroup) {}
