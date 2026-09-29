package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

// Duration là time.Duration đọc từ chuỗi JSON dạng "500ms", "2s", "5m".
type Duration time.Duration

// UnmarshalJSON đọc chuỗi thời lượng Go.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return errors.New("thời lượng phải là chuỗi, vd \"500ms\"")
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("thời lượng %q: %w", s, err)
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON viết thời lượng dạng chuỗi.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// D trả time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// Config là tệp cấu hình JSON của agent (xem docs/runbooks/edge-agent.md).
type Config struct {
	// DeviceID: mã thiết bị trong sổ thiết bị đã duyệt; trùng CN/SAN chứng thư mTLS.
	DeviceID string `json:"device_id"`
	// DataDir: thư mục trạng thái (state.json, WAL, lô đang chờ). Mất thư mục = kỳ đăng ký mới.
	DataDir string `json:"data_dir"`
	// KeyFile: khóa ký nguồn Ed25519 (PEM PKCS#8, quyền 0600). Mặc định <data_dir>/source.key.
	KeyFile string `json:"key_file"`

	Adapter AdapterConfig `json:"adapter"`
	Buffer  BufferConfig  `json:"buffer"`
	Batch   BatchConfig   `json:"batch"`
	Ingest  IngestConfig  `json:"ingest"`
	NTP     NTPConfig     `json:"ntp"`
	Health  HealthConfig  `json:"health"`
	Update  UpdateConfig  `json:"update"`
}

// BufferConfig: đệm đĩa (WAL phân đoạn).
type BufferConfig struct {
	// MaxRateHz: tốc độ đọc tối đa của đầu cân (số đo/giây) — dùng để định cỡ đệm.
	MaxRateHz float64 `json:"max_rate_hz"`
	// Hours: số giờ tối thiểu đệm phải chứa ở MaxRateHz (≥ 72, mục 14).
	Hours int `json:"hours"`
	// MaxBytes: dung lượng tối đa của đệm. 0 = tự tính từ MaxRateHz × Hours. Nhỏ hơn mức cần → từ chối khởi động.
	MaxBytes int64 `json:"max_bytes"`
	// SegmentBytes: kích thước một đoạn WAL (mặc định 8 MiB).
	SegmentBytes int64 `json:"segment_bytes"`
	// WarnFillRatio: ngưỡng cảnh báo độ đầy (mặc định 0.8).
	WarnFillRatio float64 `json:"warn_fill_ratio"`
}

// BatchConfig: đóng lô.
type BatchConfig struct {
	// MaxReadings ≤ 1000 (edgewire.MaxReadings).
	MaxReadings int `json:"max_readings"`
	// MaxWait: thời gian tối đa số đo cũ nhất chờ trước khi đóng lô chưa đầy.
	MaxWait Duration `json:"max_wait"`
}

// IngestConfig: đích đẩy lô qua mTLS (chỉ đi ra).
type IngestConfig struct {
	// URL đầy đủ của điểm nhận lô, vd https://ingest.mkg.internal:8443/v1/ingest/batches.
	URL string `json:"url"`
	// CAFile: CA nội bộ để xác minh máy chủ ingest.
	CAFile string `json:"ca_file"`
	// CertFile / CertKeyFile: chứng thư client mTLS (CN/SAN = device_id).
	CertFile    string `json:"cert_file"`
	CertKeyFile string `json:"cert_key_file"`
	// ServerName: ghi đè SNI/tên xác minh (mặc định host của URL).
	ServerName string `json:"server_name"`
	// Timeout mỗi yêu cầu (mặc định 30s).
	Timeout Duration `json:"timeout"`
	// BackoffBase / BackoffMax: lùi lũy thừa có jitter (mặc định 1s / 5m).
	BackoffBase Duration `json:"backoff_base"`
	BackoffMax  Duration `json:"backoff_max"`
}

// NTPConfig: SNTP tới NTP nội bộ (mục 18). Rỗng Server = không đồng bộ → ntp_offset_ms null.
type NTPConfig struct {
	Server   string   `json:"server"`
	Interval Duration `json:"interval"`
	Timeout  Duration `json:"timeout"`
	// MaxAge: độ lệch cũ hơn MaxAge coi như chưa đồng bộ (null).
	MaxAge Duration `json:"max_age"`
}

// HealthConfig: /healthz và /metrics cục bộ (mặc định 127.0.0.1:9108, không mở ra mạng).
type HealthConfig struct {
	Listen string `json:"listen"`
	// StaleReading: không có số đo mới sau khoảng này → adapter coi như lỗi (mặc định 60s).
	StaleReading Duration `json:"stale_reading"`
}

// UpdateConfig: tự cập nhật có chữ ký với hai khe A/B.
type UpdateConfig struct {
	// Root: thư mục cài đặt chứa slots/, slots.json (vd /opt/edge-agent). Rỗng = tắt.
	Root string `json:"root"`
	// ReleasePublicKey: khóa công khai Ed25519 phát hành (hex 64 ký tự) — KHÁC khóa thiết bị.
	ReleasePublicKey string `json:"release_public_key"`
	// HealthyAfter: bản mới chạy khỏe liên tục chừng này thì được xác nhận (mặc định 2m).
	HealthyAfter Duration `json:"healthy_after"`
	// MaxAttempts: số lần khởi động thử tối đa trước khi lùi về khe ổn định (mặc định 3).
	MaxAttempts int `json:"max_attempts"`
}

// Mặc định.
const (
	defaultHours         = 72
	defaultMaxRateHz     = 10
	defaultSegmentBytes  = 8 << 20
	defaultWarnFill      = 0.8
	defaultMaxWait       = 2 * time.Second
	defaultTimeout       = 30 * time.Second
	defaultBackoffBase   = time.Second
	defaultBackoffMax    = 5 * time.Minute
	defaultNTPInterval   = 5 * time.Minute
	defaultNTPTimeout    = 3 * time.Second
	defaultNTPMaxAge     = 30 * time.Minute
	defaultHealthListen  = "127.0.0.1:9108"
	defaultStaleReading  = 60 * time.Second
	defaultHealthyAfter  = 2 * time.Minute
	defaultMaxAttempts   = 3
	defaultIngestPath    = "/v1/ingest/batches"
	minBufferHours       = 72
	recordOverheadBytes  = 8   // khung: độ dài + CRC
	recordEstimateNoRaw  = 120 // JSON một số đo không raw (đo thực tế ~105 byte)
	recordRawJSONOverhad = 10  // ,"raw":"…"
)

var deviceIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

// LoadConfig đọc và kiểm cấu hình, điền mặc định.
func LoadConfig(path string) (*Config, error) {
	b, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("đọc cấu hình: %w", err)
	}
	var c Config
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("cấu hình %s: %w", path, err)
	}
	if err := c.normalize(); err != nil {
		return nil, fmt.Errorf("cấu hình %s: %w", path, err)
	}
	return &c, nil
}

func (c *Config) normalize() error {
	if !deviceIDRe.MatchString(c.DeviceID) {
		return fmt.Errorf("device_id phải khớp %s", deviceIDRe)
	}
	if c.DataDir == "" {
		return errors.New("thiếu data_dir")
	}
	if c.KeyFile == "" {
		c.KeyFile = filepath.Join(c.DataDir, "source.key")
	}
	b := &c.Buffer
	if b.Hours == 0 {
		b.Hours = defaultHours
	}
	if b.Hours < minBufferHours {
		return fmt.Errorf("buffer.hours phải ≥ %d (mục 14)", minBufferHours)
	}
	if b.MaxRateHz == 0 {
		b.MaxRateHz = defaultMaxRateHz
	}
	if b.MaxRateHz < 0 || b.MaxRateHz > 1000 {
		return errors.New("buffer.max_rate_hz phải trong (0, 1000]")
	}
	if b.SegmentBytes == 0 {
		b.SegmentBytes = defaultSegmentBytes
	}
	if b.SegmentBytes < 4096 {
		return errors.New("buffer.segment_bytes phải ≥ 4096")
	}
	if b.WarnFillRatio == 0 {
		b.WarnFillRatio = defaultWarnFill
	}
	if c.Batch.MaxReadings == 0 {
		c.Batch.MaxReadings = edgewire.MaxReadings
	}
	if c.Batch.MaxReadings < 1 || c.Batch.MaxReadings > edgewire.MaxReadings {
		return fmt.Errorf("batch.max_readings phải trong 1..%d", edgewire.MaxReadings)
	}
	if c.Batch.MaxWait == 0 {
		c.Batch.MaxWait = Duration(defaultMaxWait)
	}
	in := &c.Ingest
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || u.Host == "" {
			return errors.New("ingest.url không hợp lệ")
		}
		if u.Scheme != "https" {
			return errors.New("ingest.url phải là https (mTLS, ADR-0019)")
		}
		if u.Path == "" || u.Path == "/" {
			u.Path = defaultIngestPath
			in.URL = u.String()
		}
	}
	if in.Timeout == 0 {
		in.Timeout = Duration(defaultTimeout)
	}
	if in.BackoffBase == 0 {
		in.BackoffBase = Duration(defaultBackoffBase)
	}
	if in.BackoffMax == 0 {
		in.BackoffMax = Duration(defaultBackoffMax)
	}
	if in.BackoffMax < in.BackoffBase {
		return errors.New("ingest.backoff_max phải ≥ backoff_base")
	}
	if c.NTP.Interval == 0 {
		c.NTP.Interval = Duration(defaultNTPInterval)
	}
	if c.NTP.Timeout == 0 {
		c.NTP.Timeout = Duration(defaultNTPTimeout)
	}
	if c.NTP.MaxAge == 0 {
		c.NTP.MaxAge = Duration(defaultNTPMaxAge)
	}
	if c.Health.Listen == "" {
		c.Health.Listen = defaultHealthListen
	}
	if c.Health.StaleReading == 0 {
		c.Health.StaleReading = Duration(defaultStaleReading)
	}
	if c.Update.HealthyAfter == 0 {
		c.Update.HealthyAfter = Duration(defaultHealthyAfter)
	}
	if c.Update.MaxAttempts == 0 {
		c.Update.MaxAttempts = defaultMaxAttempts
	}
	if err := c.Adapter.normalize(); err != nil {
		return err
	}
	need := c.RequiredBufferBytes()
	if b.MaxBytes == 0 {
		b.MaxBytes = need + need/10
	}
	if b.MaxBytes < need {
		return fmt.Errorf("buffer.max_bytes = %d nhỏ hơn mức cần cho %d giờ ở %.1f Hz (%d byte) — mục 14 yêu cầu ≥ 72 giờ", b.MaxBytes, b.Hours, b.MaxRateHz, need)
	}
	if b.MaxBytes < b.SegmentBytes*2 {
		return errors.New("buffer.max_bytes phải ≥ 2 × segment_bytes")
	}
	return nil
}

// RecordEstimateBytes là kích thước ước tính (trên đĩa) của một số đo.
func (c *Config) RecordEstimateBytes() int64 {
	n := int64(recordOverheadBytes + recordEstimateNoRaw)
	if r := int64(c.Adapter.RawMaxBytes()); r > 0 {
		n += recordRawJSONOverhad + 4*((r+2)/3)
	}
	return n
}

// RequiredBufferBytes là dung lượng đệm cần để chứa Hours giờ ở MaxRateHz.
func (c *Config) RequiredBufferBytes() int64 {
	readings := int64(c.Buffer.MaxRateHz * float64(c.Buffer.Hours) * 3600)
	return readings * c.RecordEstimateBytes()
}

// readFile đọc tệp do người vận hành cấu hình.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(filepath.Clean(path)) //nolint:gosec // đường dẫn do người vận hành cấu hình
}
