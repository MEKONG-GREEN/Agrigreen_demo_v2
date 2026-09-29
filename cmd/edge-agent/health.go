package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Báo động (nhãn reason của edge_alarm). Độ đầy đệm là chỉ số nghiệp vụ bắt buộc có cảnh báo (mục 19).
const (
	AlarmBufferFull      = "buffer_full"      // đệm đầy — đang từ chối số đo mới
	AlarmBufferHigh      = "buffer_high"      // đệm vượt warn_fill_ratio
	AlarmIngestRejected  = "ingest_rejected"  // ingest từ chối lô (hợp đồng/chữ ký/kỳ bị thay/thu hồi) — cần người xử lý
	AlarmPayloadConflict = "payload_conflict" // ingest báo cùng khóa khác payload (409) — thiết bị hỏng hoặc bị giả
	AlarmAdapterStale    = "adapter_stale"    // không có số đo mới quá stale_reading
	AlarmNTPUnsynced     = "ntp_unsynced"     // độ lệch NTP quá cũ / chưa đồng bộ
	AlarmUpdateTrial     = "update_trial"     // đang chạy thử bản cập nhật chưa xác nhận
)

// Metrics là chỉ số của agent (đọc qua /metrics và /healthz).
type Metrics struct {
	clk clock.Clock

	AdapterErrors   atomic.Uint64
	ReadingsTotal   atomic.Uint64
	ReadingsRefused atomic.Uint64
	PushOK          atomic.Uint64
	PushErrors      atomic.Uint64
	BatchesAcked    atomic.Uint64
	lastPushOK      atomic.Int64 // unix nano
	lastReading     atomic.Int64 // unix nano

	mu     sync.Mutex
	alarms map[string]string // reason → chi tiết
}

// NewMetrics tạo bộ chỉ số.
func NewMetrics(clk clock.Clock) *Metrics { return &Metrics{clk: clk, alarms: map[string]string{}} }

// Alarm bật báo động reason.
func (m *Metrics) Alarm(reason, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alarms[reason] = detail
}

// Clear tắt báo động reason.
func (m *Metrics) Clear(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.alarms, reason)
}

// Alarms trả các báo động đang bật.
func (m *Metrics) Alarms() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.alarms))
	for k, v := range m.alarms {
		out[k] = v
	}
	return out
}

// MarkPushOK ghi thời điểm đẩy thành công.
func (m *Metrics) MarkPushOK() { m.lastPushOK.Store(m.clk.Now().UnixNano()) }

// MarkReading ghi thời điểm có số đo.
func (m *Metrics) MarkReading() { m.lastReading.Store(m.clk.Now().UnixNano()) }

// Health gom trạng thái để phục vụ /healthz và /metrics.
type Health struct {
	M       *Metrics
	Store   *Store
	NTP     *NTPTracker
	Cfg     *Config
	Version string
	Start   time.Time
}

// unhealthy là các báo động làm /healthz trả 503.
var unhealthy = []string{AlarmBufferFull, AlarmIngestRejected, AlarmPayloadConflict, AlarmAdapterStale}

// Evaluate cập nhật báo động phụ thuộc thời gian và trả (khỏe?, báo động).
func (h *Health) Evaluate() (bool, map[string]string) {
	now := h.M.clk.Now()
	last := h.M.lastReading.Load()
	ref := h.Start.UnixNano()
	if last > ref {
		ref = last
	}
	if now.Sub(time.Unix(0, ref)) > h.Cfg.Health.StaleReading.D() {
		h.M.Alarm(AlarmAdapterStale, "không có số đo mới")
	} else {
		h.M.Clear(AlarmAdapterStale)
	}
	fill := h.Store.WAL().FillRatio()
	if fill >= h.Cfg.Buffer.WarnFillRatio {
		h.M.Alarm(AlarmBufferHigh, fmt.Sprintf("đệm %.1f%%", 100*fill))
	} else {
		h.M.Clear(AlarmBufferHigh)
	}
	if h.Cfg.NTP.Server != "" && h.NTP.OffsetMs() == nil {
		h.M.Alarm(AlarmNTPUnsynced, "chưa có độ lệch NTP mới")
	} else {
		h.M.Clear(AlarmNTPUnsynced)
	}
	al := h.M.Alarms()
	for _, r := range unhealthy {
		if _, ok := al[r]; ok {
			return false, al
		}
	}
	return true, al
}

// Handler phục vụ /healthz và /metrics (chỉ nghe localhost — xem cấu hình health.listen).
func (h *Health) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		ok, al := h.Evaluate()
		w.Header().Set("Content-Type", "application/json")
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		st := h.Store.State()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": ok, "version": h.Version, "device_id": st.DeviceID, "epoch": st.Epoch, "key_id": st.KeyID,
			"last_counter": h.Store.LastCounter(), "acked_counter": h.Store.Acked(),
			"buffer_fill_ratio": h.Store.WAL().FillRatio(), "ntp_offset_ms": h.NTP.OffsetMs(), "alarms": al,
		})
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		_, al := h.Evaluate()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(h.metricsText(al)))
	})
	return mux
}

func (h *Health) metricsText(al map[string]string) string {
	var b strings.Builder
	g := func(name, help, typ string, v any) {
		_, _ = fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %v\n", name, help, name, typ, name, v)
	}
	w := h.Store.WAL()
	g("edge_buffer_bytes", "Dung lượng đệm đĩa đang dùng", "gauge", w.Bytes())
	g("edge_buffer_max_bytes", "Dung lượng đệm đĩa tối đa", "gauge", w.MaxBytes())
	g("edge_buffer_fill_ratio", "Độ đầy đệm 0..1", "gauge", fmt.Sprintf("%.6f", w.FillRatio()))
	g("edge_last_counter", "Bộ đếm đã cấp cao nhất", "gauge", h.Store.LastCounter())
	g("edge_acked_counter", "Bộ đếm cao nhất ingest đã xác nhận", "gauge", h.Store.Acked())
	g("edge_unacked_readings", "Số đo chưa được xác nhận", "gauge", h.Store.LastCounter()-h.Store.Acked())
	g("edge_readings_total", "Số đo đã ghi đệm", "counter", h.M.ReadingsTotal.Load())
	g("edge_readings_refused_total", "Số đo bị từ chối vì đệm đầy", "counter", h.M.ReadingsRefused.Load())
	g("edge_adapter_errors_total", "Lỗi đọc đầu cân", "counter", h.M.AdapterErrors.Load())
	g("edge_push_errors_total", "Lỗi đẩy lô", "counter", h.M.PushErrors.Load())
	g("edge_batches_acked_total", "Lô đã được ingest xác nhận", "counter", h.M.BatchesAcked.Load())
	g("edge_last_push_success_timestamp_seconds", "Thời điểm đẩy thành công gần nhất", "gauge", h.M.lastPushOK.Load()/1e9)
	g("edge_last_reading_timestamp_seconds", "Thời điểm số đo gần nhất", "gauge", h.M.lastReading.Load()/1e9)
	if off := h.NTP.OffsetMs(); off != nil {
		g("edge_ntp_offset_ms", "Độ lệch đồng hồ thiết bị so với NTP", "gauge", *off)
		g("edge_ntp_synced", "1 nếu có độ lệch NTP mới", "gauge", 1)
	} else {
		g("edge_ntp_synced", "1 nếu có độ lệch NTP mới", "gauge", 0)
	}
	g("edge_ntp_errors_total", "Lỗi hỏi NTP", "counter", h.NTP.Errors())
	b.WriteString("# HELP edge_alarm Báo động đang bật\n# TYPE edge_alarm gauge\n")
	keys := make([]string, 0, len(al))
	for k := range al {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		_, _ = fmt.Fprintf(&b, "edge_alarm{reason=%q} 1\n", k)
	}
	_, _ = fmt.Fprintf(&b, "edge_build_info{version=%q} 1\n", h.Version)
	return b.String()
}
