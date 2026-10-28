package apiclient

import (
	"os"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// UsePublishedFeedSources (W11.81) nối nguồn feed tham chiếu của dữ liệu đã công bố (tên feed → module chủ: pf/price
// price_board_daily) và đơn vị công bố của từng feed — gốc lắp ráp cmd/worker gọi; kiểm thử gọi trực tiếp.
func (m *Module) UsePublishedFeedSources(src map[string]port.PublishedSource, publishers map[string][]port.Publisher) {
	m.svc.PublishedSources, m.svc.Publishers = src, publishers
}

// PricePublishersFromEnv đọc port.EnvPricePublishers (rỗng → không đơn vị nào, không dựng price_board_daily).
func PricePublishersFromEnv() ([]port.Publisher, error) {
	return port.ParsePublishers(os.Getenv(port.EnvPricePublishers))
}
