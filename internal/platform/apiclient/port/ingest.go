package port

// Nạp theo lô của cổng tích hợp (W5.4, ADR-0033 §3).

// ActionWrite là hành động của scope nạp.
const ActionWrite = "write"

// IngestScope trả scope nạp theo lô của một hợp đồng contracts/integration/ingest: "ingest:<hợp đồng>:write".
// Cấp qua đăng ký (subscription) với product = tên hợp đồng nạp, như scope sản phẩm dữ liệu.
func IngestScope(contract string) string { return "ingest:" + contract + ":" + ActionWrite }

// Biến môi trường của nạp theo lô (cmd/integration-gateway).
const (
	// EnvIngestAllowProposed: "1" cho phép hợp đồng nạp proposed/draft ngoài production (dev/test). Production
	// từ chối khởi động nếu đặt.
	EnvIngestAllowProposed = "AG_GATEWAY_INGEST_ALLOW_PROPOSED"
	// EnvIngestMaxQueued: số lô chờ tối đa mỗi đối tác trước 429 (mặc định 50).
	EnvIngestMaxQueued = "AG_GATEWAY_INGEST_MAX_QUEUED"
	// EnvIngestRetryAfter: Retry-After (giây) khi hàng đợi đối tác đầy (mặc định 30).
	EnvIngestRetryAfter = "AG_GATEWAY_INGEST_RETRY_AFTER"
)
