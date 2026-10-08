package port

// JobWebhookDispatch là job của cmd/worker (-jobs, chạy ở leader) giao chỉ báo cursor tới webhook đã đăng ký và
// điều khiển ngắt mạch (W5.7, R-INT-07, R-INT-09; ADR-0033 §4).
const JobWebhookDispatch = "apiclient_webhook_dispatch"
