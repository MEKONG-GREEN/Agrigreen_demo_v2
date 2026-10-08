package measurement

import (
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/ingest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mặt nối của dịch vụ ingest cho gốc lắp ráp cmd/ingest (R-02: gốc lắp ráp chỉ import
// gốc module hoặc port/). Hiện thực ở internal/mkg/measurement/ingest.
type (
	// IngestPorts là cổng measurement mà ingest dùng (sổ thiết bị, phiên cân, bì, phiếu cân).
	IngestPorts = ingest.MKGPorts
	// IngestReceiver nhận lô POST /v1/ingest/batches (mTLS).
	IngestReceiver = ingest.Receiver
	// IngestLoop tích phân phiên cân đã đóng thành phiếu (R-70).
	IngestLoop = ingest.Loop
	// IngestTSStore ghi/đọc ts_weigh (r_measurement; dựng bằng NewIngestTSStore).
	IngestTSStore = ingest.TSStore
	// IngestIntegrator là hiện thực port.Integrator (weigh.integrate.v1).
	IngestIntegrator = ingest.Integrator
)

// Hàm và hằng của ingest cho gốc lắp ráp.
var (
	IngestServerTLS = ingest.ServerTLS
	// IngestRequireDeviceCA: AG_INGEST_CLIENT_CA phải là CA thiết bị riêng (không ký chứng thư máy chủ).
	IngestRequireDeviceCA  = ingest.RequireSeparateDeviceCA
	NewIngestMetrics       = ingest.NewMetrics
	NewIngestRateLimiter   = ingest.NewRateLimiter
	ErrIngestPortsNotWired = ingest.ErrPortsNotWired
)

// Mặc định tạm của ingest (chờ khách / vận hành).
const (
	IngestDefaultSkewFlagMs = ingest.DefaultSkewFlagMs
	IngestDefaultStuckAfter = ingest.DefaultStuckAfter
)

// NewIngestTSStore dựng kho ts_weigh của cmd/ingest: mọi giao dịch trên writer/reader chạy SET LOCAL ROLE
// r_measurement (W2.6 — vai đăng nhập app_ingest; quyền của ingest_ts + ts_reader gộp ở ts 000004).
func NewIngestTSStore(writer, reader tenant.Beginner) *IngestTSStore {
	return &IngestTSStore{Writer: forModule(writer), Reader: forModule(reader)}
}
