package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// DLQDepth đọc số thông điệp trong mọi DLQ (<consumer>.dlq) qua RabbitMQ
// management API (GET /api/queues). mgmtURL dạng http://user:pass@rabbitmq:15672;
// thông tin đăng nhập gửi bằng Basic auth, không bao giờ ghi vào log hay lỗi.
func DLQDepth(ctx context.Context, client *http.Client, mgmtURL string) (map[string]int64, error) {
	u, err := url.Parse(mgmtURL)
	if err != nil {
		return nil, errors.New("inbox: AG_RABBITMQ_MGMT_URL không hợp lệ")
	}
	user := u.User
	u.User = nil
	u.Path = strings.TrimRight(u.Path, "/") + "/api/queues"
	u.RawQuery = url.Values{"columns": {"name,vhost,messages"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if user != nil {
		pw, _ := user.Password()
		req.SetBasicAuth(user.Username(), pw)
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		// u đã bỏ user:pass nên thông điệp lỗi không lộ credential.
		return nil, fmt.Errorf("inbox: gọi management API: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("inbox: management API trả %d", resp.StatusCode)
	}
	var qs []struct {
		Name     string `json:"name"`
		VHost    string `json:"vhost"`
		Messages int64  `json:"messages"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&qs); err != nil {
		return nil, fmt.Errorf("inbox: đọc danh sách queue: %w", err)
	}
	out := map[string]int64{}
	for _, q := range qs {
		if strings.HasSuffix(q.Name, ".dlq") {
			out[q.Name] = q.Messages
		}
	}
	return out, nil
}

// RegisterDLQMetrics đăng ký gauge ag.rabbitmq.dlq.depth theo queue (R-62: sự
// kiện vào DLQ sinh cảnh báo). No-op khi OTel tắt hoặc mgmtURL rỗng.
func RegisterDLQMetrics(client *http.Client, mgmtURL string) (metric.Registration, error) {
	if mgmtURL == "" {
		return nil, nil
	}
	m := telemetry.Meter()
	g, err := m.Int64ObservableGauge(telemetry.MetricDLQDepth, metric.WithDescription("Số thông điệp trong DLQ"))
	if err != nil {
		return nil, err
	}
	return m.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		depth, err := DLQDepth(ctx, client, mgmtURL)
		if err != nil {
			return err
		}
		for q, n := range depth {
			o.ObserveInt64(g, n, metric.WithAttributes(attribute.String("queue", q)))
		}
		return nil
	}, g)
}
