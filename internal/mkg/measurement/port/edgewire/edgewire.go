// Package edgewire là hợp đồng dây ĐÃ ĐÓNG BĂNG giữa agent biên đọc cân
// (cmd/edge-agent) và dịch vụ nhận (cmd/ingest): lô số đo có chữ ký nguồn
// (tài liệu kiến trúc mục 14; ADR-0019). JSON Schema: contracts/edge/batch.v1.json;
// bộ vector kiểm thử: contracts/edge/batch.v1.vectors.json.
//
// Package CHỈ dùng thư viện chuẩn (archlint cưỡng chế) để edge-agent — nhị phân
// tĩnh ở vùng biên OT — không kéo theo pgx, DB hay mã nghiệp vụ.
//
// # Lô
//
// Một lô là dãy số đo có bộ đếm đơn điệu LIÊN TIẾP counter_from..counter_to của
// một thiết bị trong một kỳ đăng ký (epoch). Bộ đếm bắt đầu từ 1 ở mỗi kỳ, không
// bao giờ dùng lại; mất đĩa hoặc cài lại → đăng ký kỳ mới (epoch + 1). Khóa
// idempotency là (device_id, epoch, counter). Đồng hồ thiết bị (device_time) và
// độ lệch NTP chỉ là thông tin — KHÔNG BAO GIỜ dùng làm thứ tự.
//
// # Dạng chuẩn (canonical) — thứ được ký
//
// Chữ ký Ed25519 và payload_sha256 tính trên dạng chuẩn, không trên JSON (thứ tự
// khóa, khoảng trắng, cách viết số của JSON không ổn định). Dạng chuẩn là văn bản
// ASCII, mỗi dòng kết thúc bằng một ký tự LF (0x0A), không có BOM, không có CR:
//
//	agrigreen.edge.batch.v1
//	device_id=<device_id>
//	epoch=<epoch thập phân>
//	counter_from=<số thập phân>
//	counter_to=<số thập phân>
//	key_id=<key_id>
//	ntp_offset_ms=<số nguyên có dấu thập phân | null>
//	reading=<counter>;<device_time>;<value_kg>;<1|0>;<unit>;<raw>
//	... (một dòng reading cho mỗi số đo, theo counter tăng dần)
//
// Số nguyên viết thập phân không dấu cộng, không số 0 đứng đầu (strconv.FormatInt).
// stable viết 1 (true) hoặc 0 (false). raw rỗng khi không có. Các trường chuỗi đã
// bị giới hạn bằng biểu thức chính quy (Validate) nên không chứa ';', '=' hay LF —
// dạng chuẩn tách được duy nhất và tái dựng được byte-cho-byte từ các cột của
// ts_weigh.reading_batch + ts_weigh.raw_reading (xác minh lại bất kỳ lúc nào).
//
// signature = base64 chuẩn (có đệm) của Ed25519(khóa nguồn, dạng chuẩn), 64 byte.
// key_id = hex của 8 byte đầu SHA-256(khóa công khai 32 byte).
package edgewire

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Format là giá trị trường "format" và dòng đầu của dạng chuẩn (tách miền chữ ký).
const Format = "agrigreen.edge.batch.v1"

// Giới hạn của một lô.
const (
	MaxReadings = 1000
	MaxRawBytes = 256
	// MaxNTPOffsetMs: độ lệch lớn hơn 1 ngày coi như sai cấu hình.
	MaxNTPOffsetMs = 86_400_000
	// MaxBodyBytes là giới hạn thân HTTP ingest nên áp cho một lô.
	MaxBodyBytes = 1 << 20
)

// Đơn vị đầu cân hiển thị. value_kg LUÔN là kg — bộ điều hợp của mẫu cân đã quy đổi.
const (
	UnitKg = "kg"
	UnitT  = "t"
)

// Reading là một số đo.
type Reading struct {
	Counter int64 `json:"counter"`
	// DeviceTime: đồng hồ thiết bị, UTC, đúng dạng 2006-01-02T15:04:05.000Z.
	DeviceTime string `json:"device_time"`
	// ValueKg: khối lượng kg, decimal dạng chuỗi, ĐÚNG 3 chữ số thập phân (R-10), có thể âm (trôi điểm 0).
	ValueKg string `json:"value_kg"`
	// Stable: đầu cân báo ổn định.
	Stable bool `json:"stable"`
	// Unit: đơn vị đầu cân hiển thị (kg | t).
	Unit string `json:"unit"`
	// Raw: khung gốc từ đầu cân (tùy chọn), base64 chuẩn có đệm, 1..256 byte sau giải mã.
	Raw string `json:"raw,omitempty"`
}

// Batch là một lô số đo đã (hoặc sắp) ký.
type Batch struct {
	Format      string `json:"format"`
	DeviceID    string `json:"device_id"`
	Epoch       int    `json:"epoch"`
	CounterFrom int64  `json:"counter_from"`
	CounterTo   int64  `json:"counter_to"`
	KeyID       string `json:"key_id"`
	// NTPOffsetMs: độ lệch đồng hồ thiết bị so với NTP (ms, có dấu). Bắt buộc có
	// mặt; null = thiết bị báo chưa đồng bộ NTP.
	NTPOffsetMs *int      `json:"ntp_offset_ms"`
	Readings    []Reading `json:"readings"`
	// Signature: base64 chuẩn của chữ ký Ed25519 trên Canonical.
	Signature string `json:"signature"`
}

// Error là lỗi hợp đồng có mã ổn định (vector kiểm thử dùng Code).
type Error struct {
	Code string // edge.decode | edge.format | edge.device_id | edge.epoch | edge.counter_range | edge.key_id | edge.ntp_offset | edge.reading | edge.signature
	Msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }

func fail(code, format string, a ...any) error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, a...)}
}

// CodeOf trả mã của lỗi hợp đồng, "" nếu không phải.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

var (
	deviceIDRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)
	keyIDRe      = regexp.MustCompile(`^[0-9a-f]{16}$`)
	deviceTimeRe = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{3}Z$`)
	valueKgRe    = regexp.MustCompile(`^-?(0|[1-9][0-9]{0,10})\.[0-9]{3}$`)
)

// DeviceTimeLayout là dạng device_time.
const DeviceTimeLayout = "2006-01-02T15:04:05.000Z"

// FormatDeviceTime viết t theo DeviceTimeLayout (UTC, cắt xuống mili giây).
func FormatDeviceTime(t time.Time) string {
	return t.UTC().Truncate(time.Millisecond).Format(DeviceTimeLayout)
}

// Decode đọc một lô từ JSON: khóa lạ bị từ chối, mọi khóa bắt buộc phải có mặt
// (kể cả ntp_offset_ms = null). Decode KHÔNG kiểm chữ ký — gọi Verify.
func Decode(data []byte) (Batch, error) {
	var b Batch
	if len(data) > MaxBodyBytes {
		return b, fail("edge.decode", "lô vượt %d byte", MaxBodyBytes)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		return b, fail("edge.decode", "JSON không hợp lệ")
	}
	for _, k := range []string{"format", "device_id", "epoch", "counter_from", "counter_to", "key_id", "ntp_offset_ms", "readings", "signature"} {
		if _, ok := keys[k]; !ok {
			return b, fail("edge.decode", "thiếu trường %s", k)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return Batch{}, fail("edge.decode", "JSON sai hợp đồng: %v", err)
	}
	if dec.More() {
		return Batch{}, fail("edge.decode", "dữ liệu thừa sau lô")
	}
	return b, nil
}

// Validate kiểm mọi ràng buộc của hợp đồng trừ chữ ký.
func (b Batch) Validate() error {
	if b.Format != Format {
		return fail("edge.format", "format phải là %q", Format)
	}
	if !deviceIDRe.MatchString(b.DeviceID) {
		return fail("edge.device_id", "device_id phải khớp %s", deviceIDRe)
	}
	if b.Epoch < 1 || b.Epoch > 1<<31-1 {
		return fail("edge.epoch", "epoch phải từ 1")
	}
	if b.CounterFrom < 1 || b.CounterTo < b.CounterFrom {
		return fail("edge.counter_range", "cần 1 ≤ counter_from ≤ counter_to")
	}
	n := b.CounterTo - b.CounterFrom + 1
	if n > MaxReadings {
		return fail("edge.counter_range", "lô tối đa %d số đo", MaxReadings)
	}
	if int64(len(b.Readings)) != n {
		return fail("edge.counter_range", "số số đo (%d) phải bằng counter_to − counter_from + 1 (%d)", len(b.Readings), n)
	}
	if !keyIDRe.MatchString(b.KeyID) {
		return fail("edge.key_id", "key_id phải là 16 chữ số hex thường")
	}
	if b.NTPOffsetMs != nil && (*b.NTPOffsetMs > MaxNTPOffsetMs || *b.NTPOffsetMs < -MaxNTPOffsetMs) {
		return fail("edge.ntp_offset", "|ntp_offset_ms| tối đa %d", MaxNTPOffsetMs)
	}
	for i, r := range b.Readings {
		if r.Counter != b.CounterFrom+int64(i) {
			return fail("edge.counter_range", "số đo %d: counter %d, cần %d (liên tiếp, tăng dần)", i, r.Counter, b.CounterFrom+int64(i))
		}
		if err := r.validate(); err != nil {
			return fail("edge.reading", "số đo counter %d: %v", r.Counter, err)
		}
	}
	return nil
}

func (r Reading) validate() error {
	if !deviceTimeRe.MatchString(r.DeviceTime) {
		return fmt.Errorf("device_time phải có dạng %s", DeviceTimeLayout)
	}
	if _, err := time.Parse(DeviceTimeLayout, r.DeviceTime); err != nil {
		return errors.New("device_time không phải thời điểm hợp lệ")
	}
	if !valueKgRe.MatchString(r.ValueKg) || r.ValueKg == "-0.000" {
		return errors.New("value_kg phải là decimal có đúng 3 chữ số thập phân, tối đa 11 chữ số nguyên")
	}
	if r.Unit != UnitKg && r.Unit != UnitT {
		return errors.New("unit phải là kg hoặc t")
	}
	if r.Raw != "" {
		raw, err := base64.StdEncoding.Strict().DecodeString(r.Raw)
		if err != nil || len(raw) == 0 || len(raw) > MaxRawBytes || base64.StdEncoding.EncodeToString(raw) != r.Raw {
			return fmt.Errorf("raw phải là base64 chuẩn có đệm của 1..%d byte", MaxRawBytes)
		}
	}
	return nil
}

// Canonical trả dạng chuẩn của lô (xem tài liệu package). Lô phải hợp lệ.
func (b Batch) Canonical() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.Grow(160 + 64*len(b.Readings))
	buf.WriteString(Format + "\n")
	buf.WriteString("device_id=" + b.DeviceID + "\n")
	buf.WriteString("epoch=" + strconv.Itoa(b.Epoch) + "\n")
	buf.WriteString("counter_from=" + strconv.FormatInt(b.CounterFrom, 10) + "\n")
	buf.WriteString("counter_to=" + strconv.FormatInt(b.CounterTo, 10) + "\n")
	buf.WriteString("key_id=" + b.KeyID + "\n")
	if b.NTPOffsetMs == nil {
		buf.WriteString("ntp_offset_ms=null\n")
	} else {
		buf.WriteString("ntp_offset_ms=" + strconv.Itoa(*b.NTPOffsetMs) + "\n")
	}
	for _, r := range b.Readings {
		stable := "0"
		if r.Stable {
			stable = "1"
		}
		buf.WriteString("reading=" + strconv.FormatInt(r.Counter, 10) + ";" + r.DeviceTime + ";" + r.ValueKg + ";" +
			stable + ";" + r.Unit + ";" + r.Raw + "\n")
	}
	return buf.Bytes(), nil
}

// PayloadSHA256 là SHA-256 của dạng chuẩn (cột ts_weigh.reading_batch.payload_sha256).
func (b Batch) PayloadSHA256() ([32]byte, error) {
	c, err := b.Canonical()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(c), nil
}

// KeyID là mã khóa: hex của 8 byte đầu SHA-256(khóa công khai).
func KeyID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

// Sign đặt KeyID theo khóa và ký lô (agent biên). Lô phải hợp lệ.
func Sign(priv ed25519.PrivateKey, b *Batch) error {
	if len(priv) != ed25519.PrivateKeySize {
		return fail("edge.signature", "khóa riêng Ed25519 sai độ dài")
	}
	b.KeyID = KeyID(priv.Public().(ed25519.PublicKey))
	c, err := b.Canonical()
	if err != nil {
		return err
	}
	b.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, c))
	return nil
}

// SignatureBytes giải mã chữ ký (64 byte).
func (b Batch) SignatureBytes() ([]byte, error) {
	sig, err := base64.StdEncoding.Strict().DecodeString(b.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return nil, fail("edge.signature", "signature phải là base64 chuẩn của 64 byte")
	}
	return sig, nil
}

// Verify kiểm hợp đồng, key_id khớp khóa công khai đã đăng ký, và chữ ký nguồn.
// ingest gọi Verify với khóa lấy từ sổ thiết bị ĐÃ DUYỆT (mkg/measurement
// DeviceRegistry) — không bao giờ với khóa đi kèm lô.
func Verify(pub ed25519.PublicKey, b Batch) error {
	if len(pub) != ed25519.PublicKeySize {
		return fail("edge.signature", "khóa công khai Ed25519 sai độ dài")
	}
	c, err := b.Canonical()
	if err != nil {
		return err
	}
	if b.KeyID != KeyID(pub) {
		return fail("edge.key_id", "key_id của lô không khớp khóa đã đăng ký")
	}
	sig, err := b.SignatureBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, c, sig) {
		return fail("edge.signature", "chữ ký nguồn không hợp lệ")
	}
	return nil
}
