package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

// SerialConfig cấu hình bộ điều hợp dòng ASCII chung cho đầu cân (weight indicator).
//
// Thư viện chuẩn Go không có API cổng nối tiếp (đặt baud/parity cần ioctl riêng từng
// OS). Vì vậy bộ điều hợp làm việc trên một LUỒNG BYTE:
//   - "file:/dev/ttyUSB0": mở thiết bị tty đã được cấu hình sẵn bằng
//     `stty -F /dev/ttyUSB0 9600 cs8 -cstopb -parenb raw -echo` (unit systemd ExecStartPre);
//   - "tcp://host:port": cầu nối serial-over-TCP (ser2net, bộ chuyển RS-232/485 → Ethernet)
//     — cũng là chế độ kiểm thử.
//
// Mỗi khung là một dòng kết thúc bằng Delimiter; Pattern (regexp RE2) phải có nhóm
// tên `value`, tùy chọn `unit` (kg|t) và `stable`. Mẫu cân cụ thể chờ C-6.
type SerialConfig struct {
	Transport string `json:"transport"`
	// Delimiter: ký tự kết thúc khung (mặc định "\n"; "\r" cuối dòng tự bỏ khi so mẫu).
	Delimiter string `json:"delimiter"`
	// Pattern: biểu thức chính quy cho một khung (không gồm Delimiter).
	Pattern string `json:"pattern"`
	// StableValues: giá trị của nhóm `stable` nghĩa là ổn định (mặc định ["ST"]).
	// Mẫu không có nhóm `stable` → mọi số đo báo chưa ổn định.
	StableValues []string `json:"stable_values"`
	// DefaultUnit khi mẫu không có nhóm `unit` (mặc định kg).
	DefaultUnit string `json:"default_unit"`
	// PollCommand: lệnh gửi trước mỗi lần đọc (vd "SI\r\n"); rỗng = đầu cân tự phát liên tục.
	PollCommand  string   `json:"poll_command"`
	PollInterval Duration `json:"poll_interval"`
	// ReadTimeout: không nhận được khung trong khoảng này → lỗi, kết nối lại (mặc định 10s).
	ReadTimeout Duration `json:"read_timeout"`
	// MaxFrameBytes: độ dài khung tối đa (mặc định 64, ≤ 256).
	MaxFrameBytes int `json:"max_frame_bytes"`
	// IncludeRaw: ghi khung gốc vào trường raw (pháp chứng).
	IncludeRaw bool `json:"include_raw"`
}

// DefaultSerialPattern khớp dạng phổ biến "ST,GS,+001250.5kg" (ST/US = ổn định/chưa, GS/NT = tổng/tịnh).
const DefaultSerialPattern = `^\s*(?P<stable>ST|US|OL)\s*,\s*(?:GS|NT|TR)\s*,\s*(?P<value>[+-]?\s*[0-9]+(?:\.[0-9]+)?)\s*(?P<unit>kg|t)?\s*$`

func (s *SerialConfig) normalize() error {
	if !strings.HasPrefix(s.Transport, "tcp://") && !strings.HasPrefix(s.Transport, "file:") {
		return errors.New("serial_ascii.transport phải là tcp://host:port hoặc file:/dev/ttyXXX")
	}
	if s.Delimiter == "" {
		s.Delimiter = "\n"
	}
	if s.Pattern == "" {
		s.Pattern = DefaultSerialPattern
	}
	re, err := regexp.Compile(s.Pattern)
	if err != nil {
		return fmt.Errorf("serial_ascii.pattern: %w", err)
	}
	if re.SubexpIndex("value") < 0 {
		return errors.New("serial_ascii.pattern phải có nhóm tên (?P<value>…)")
	}
	if len(s.StableValues) == 0 {
		s.StableValues = []string{"ST"}
	}
	if s.DefaultUnit == "" {
		s.DefaultUnit = edgewire.UnitKg
	}
	if s.DefaultUnit != edgewire.UnitKg && s.DefaultUnit != edgewire.UnitT {
		return errors.New("serial_ascii.default_unit phải là kg hoặc t")
	}
	if s.ReadTimeout == 0 {
		s.ReadTimeout = Duration(10 * time.Second)
	}
	if s.MaxFrameBytes == 0 {
		s.MaxFrameBytes = 64
	}
	if s.MaxFrameBytes < 8 || s.MaxFrameBytes > edgewire.MaxRawBytes {
		return fmt.Errorf("serial_ascii.max_frame_bytes phải trong 8..%d", edgewire.MaxRawBytes)
	}
	return nil
}

// frameError là khung đọc được nhưng không dùng được (không khớp mẫu, quá dài):
// agent đếm lỗi và đọc tiếp ngay, không kết nối lại.
type frameError struct{ msg string }

func (e *frameError) Error() string { return "khung đầu cân: " + e.msg }

// SerialASCII là bộ điều hợp dòng ASCII trên luồng byte.
type SerialASCII struct {
	cfg    SerialConfig
	re     *regexp.Regexp
	iValue int
	iUnit  int
	iStab  int
	open   func(ctx context.Context) (io.ReadWriteCloser, error)

	conn io.ReadWriteCloser
	rd   *bufio.Reader
	last time.Time
}

// NewSerialASCII tạo bộ điều hợp; cfg đã normalize.
func NewSerialASCII(cfg SerialConfig) (*SerialASCII, error) {
	re, err := regexp.Compile(cfg.Pattern)
	if err != nil {
		return nil, err
	}
	a := &SerialASCII{cfg: cfg, re: re, iValue: re.SubexpIndex("value"), iUnit: re.SubexpIndex("unit"), iStab: re.SubexpIndex("stable")}
	a.open = a.dial
	return a, nil
}

func (a *SerialASCII) dial(ctx context.Context) (io.ReadWriteCloser, error) {
	if addr, ok := strings.CutPrefix(a.cfg.Transport, "tcp://"); ok {
		var d net.Dialer
		cctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		return d.DialContext(cctx, "tcp", addr)
	}
	path := strings.TrimPrefix(strings.TrimPrefix(a.cfg.Transport, "file:"), "//")
	return os.OpenFile(filepath.Clean(path), os.O_RDWR, 0) //nolint:gosec // thiết bị tty do người vận hành cấu hình
}

type deadliner interface{ SetReadDeadline(time.Time) error }

func (a *SerialASCII) reset() {
	if a.conn != nil {
		_ = a.conn.Close()
	}
	a.conn, a.rd = nil, nil
}

// Next đọc khung kế tiếp và giải mã.
func (a *SerialASCII) Next(ctx context.Context) (Sample, error) {
	if err := ctx.Err(); err != nil {
		return Sample{}, err
	}
	if a.conn == nil {
		c, err := a.open(ctx)
		if err != nil {
			return Sample{}, fmt.Errorf("mở %s: %w", a.cfg.Transport, err)
		}
		a.conn, a.rd = c, bufio.NewReaderSize(c, 4*edgewire.MaxRawBytes)
	}
	if a.cfg.PollCommand != "" {
		if wait := a.cfg.PollInterval.D() - time.Since(a.last); wait > 0 {
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return Sample{}, ctx.Err()
			case <-t.C:
			}
		}
		a.last = time.Now()
		if _, err := io.WriteString(a.conn, a.cfg.PollCommand); err != nil {
			a.reset()
			return Sample{}, fmt.Errorf("gửi lệnh hỏi: %w", err)
		}
	}
	if d, ok := a.conn.(deadliner); ok {
		_ = d.SetReadDeadline(time.Now().Add(a.cfg.ReadTimeout.D()))
	}
	frame, err := a.readFrame()
	if err != nil {
		var fe *frameError
		if errors.As(err, &fe) {
			return Sample{}, err
		}
		a.reset()
		if ctx.Err() != nil {
			return Sample{}, ctx.Err()
		}
		return Sample{}, fmt.Errorf("đọc %s: %w", a.cfg.Transport, err)
	}
	return a.Parse(frame)
}

// readFrame đọc tới Delimiter (kể cả Delimiter).
func (a *SerialASCII) readFrame() ([]byte, error) {
	delim := []byte(a.cfg.Delimiter)
	last := delim[len(delim)-1]
	var buf []byte
	for {
		chunk, err := a.rd.ReadSlice(last)
		buf = append(buf, chunk...)
		if err == nil && bytes.HasSuffix(buf, delim) {
			break
		}
		if err != nil && !errors.Is(err, bufio.ErrBufferFull) {
			return nil, err
		}
		if len(buf) > 4*a.cfg.MaxFrameBytes {
			// Bỏ tới hết dòng hiện tại để đồng bộ lại khung.
			if _, err := a.rd.ReadSlice(last); err != nil && !errors.Is(err, bufio.ErrBufferFull) {
				return nil, err
			}
			return nil, &frameError{"khung vượt độ dài tối đa"}
		}
	}
	if len(buf) > a.cfg.MaxFrameBytes {
		return nil, &frameError{fmt.Sprintf("khung %d byte vượt max_frame_bytes %d", len(buf), a.cfg.MaxFrameBytes)}
	}
	return buf, nil
}

// Parse giải mã một khung (kể cả Delimiter) thành số đo.
func (a *SerialASCII) Parse(frame []byte) (Sample, error) {
	line := strings.TrimSuffix(string(frame), a.cfg.Delimiter)
	line = strings.TrimRight(line, "\r\n")
	m := a.re.FindStringSubmatch(line)
	if m == nil {
		return Sample{}, &frameError{"không khớp mẫu"}
	}
	unit := a.cfg.DefaultUnit
	if a.iUnit >= 0 && m[a.iUnit] != "" {
		unit = strings.ToLower(m[a.iUnit])
	}
	val := strings.ReplaceAll(m[a.iValue], " ", "")
	kg, err := normalizeKg(val, unit)
	if err != nil {
		return Sample{}, &frameError{err.Error()}
	}
	s := Sample{ValueKg: kg, Unit: unit}
	if a.iStab >= 0 {
		s.Stable = slices.Contains(a.cfg.StableValues, m[a.iStab])
	}
	if a.cfg.IncludeRaw {
		s.Raw = bytes.Clone(frame)
	}
	return s, nil
}

// Close đóng luồng.
func (a *SerialASCII) Close() error {
	a.reset()
	return nil
}
