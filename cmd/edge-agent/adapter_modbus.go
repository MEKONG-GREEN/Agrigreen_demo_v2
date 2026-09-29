package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

// ModbusConfig cấu hình bộ điều hợp Modbus TCP (PLC/đầu cân có cổng Ethernet):
// đọc thanh ghi holding (0x03) hoặc input (0x04) chứa khối lượng và cờ ổn định.
// Bản đồ thanh ghi cụ thể chờ mẫu cân (C-6).
type ModbusConfig struct {
	// Address: host:port (mặc định cổng 502).
	Address string `json:"address"`
	UnitID  uint8  `json:"unit_id"`
	// Function: "holding" (0x03, mặc định) hoặc "input" (0x04).
	Function string `json:"function"`
	// WeightRegister: địa chỉ (0-based) thanh ghi đầu của khối lượng; WeightWords: 1 hoặc 2 (mặc định 2).
	WeightRegister uint16 `json:"weight_register"`
	WeightWords    int    `json:"weight_words"`
	// WordOrder: "big" (từ cao trước, ABCD — mặc định) hoặc "little" (CDAB).
	WordOrder string `json:"word_order"`
	// Unsigned: giá trị không dấu (mặc định có dấu, bù hai).
	Unsigned bool `json:"unsigned"`
	// Decimals: giá trị thật = số nguyên / 10^Decimals, theo Unit.
	Decimals int    `json:"decimals"`
	Unit     string `json:"unit"`
	// StableRegister: thanh ghi trạng thái; bit (StableMask) bật = ổn định (StableInvert: bật = đang dao động).
	// Không cấu hình → phát hiện bằng phần mềm: StableSamples lần đọc liên tiếp bằng nhau.
	StableRegister *uint16 `json:"stable_register,omitempty"`
	StableMask     uint16  `json:"stable_mask"`
	StableInvert   bool    `json:"stable_invert"`
	StableSamples  int     `json:"stable_samples"`
	// PollInterval: chu kỳ đọc (mặc định 500ms) — tốc độ đọc tối đa = 1/PollInterval.
	PollInterval Duration `json:"poll_interval"`
	Timeout      Duration `json:"timeout"`
	IncludeRaw   bool     `json:"include_raw"`
}

// modbusRawBytes: raw = các byte dữ liệu thanh ghi (khối lượng 4 + trạng thái 2).
const modbusRawBytes = 6

func (m *ModbusConfig) normalize() error {
	if m.Address == "" {
		return errors.New("modbus_tcp.address bắt buộc")
	}
	if _, _, err := net.SplitHostPort(m.Address); err != nil {
		m.Address = net.JoinHostPort(m.Address, "502")
	}
	if m.UnitID == 0 {
		m.UnitID = 1
	}
	switch m.Function {
	case "":
		m.Function = "holding"
	case "holding", "input":
	default:
		return errors.New("modbus_tcp.function phải là holding hoặc input")
	}
	if m.WeightWords == 0 {
		m.WeightWords = 2
	}
	if m.WeightWords != 1 && m.WeightWords != 2 {
		return errors.New("modbus_tcp.weight_words phải là 1 hoặc 2")
	}
	switch m.WordOrder {
	case "":
		m.WordOrder = "big"
	case "big", "little":
	default:
		return errors.New("modbus_tcp.word_order phải là big hoặc little")
	}
	if m.Decimals < 0 || m.Decimals > 6 {
		return errors.New("modbus_tcp.decimals phải trong 0..6")
	}
	if m.Unit == "" {
		m.Unit = edgewire.UnitKg
	}
	if m.Unit != edgewire.UnitKg && m.Unit != edgewire.UnitT {
		return errors.New("modbus_tcp.unit phải là kg hoặc t")
	}
	if m.StableMask == 0 {
		m.StableMask = 1
	}
	if m.StableSamples == 0 {
		m.StableSamples = 3
	}
	if m.PollInterval == 0 {
		m.PollInterval = Duration(500 * time.Millisecond)
	}
	if m.Timeout == 0 {
		m.Timeout = Duration(2 * time.Second)
	}
	return nil
}

// ModbusTCP là client Modbus TCP tối giản (chỉ thư viện chuẩn).
type ModbusTCP struct {
	cfg    ModbusConfig
	conn   net.Conn
	tx     uint16
	ticker *time.Ticker
	prev   string
	same   int
}

// NewModbusTCP tạo bộ điều hợp; cfg đã normalize.
func NewModbusTCP(cfg ModbusConfig) *ModbusTCP { return &ModbusTCP{cfg: cfg} }

// ModbusException là phản hồi ngoại lệ của thiết bị.
type ModbusException struct{ Function, Code byte }

func (e *ModbusException) Error() string {
	return fmt.Sprintf("modbus: ngoại lệ hàm 0x%02x mã %d", e.Function, e.Code)
}

func (m *ModbusTCP) reset() {
	if m.conn != nil {
		_ = m.conn.Close()
	}
	m.conn = nil
}

// readRegisters gửi một yêu cầu đọc thanh ghi và trả các từ 16 bit.
func (m *ModbusTCP) readRegisters(ctx context.Context, addr uint16, qty int) ([]uint16, error) {
	if m.conn == nil {
		d := net.Dialer{Timeout: m.cfg.Timeout.D()}
		c, err := d.DialContext(ctx, "tcp", m.cfg.Address)
		if err != nil {
			return nil, fmt.Errorf("modbus: kết nối %s: %w", m.cfg.Address, err)
		}
		m.conn = c
	}
	fc := byte(0x03)
	if m.cfg.Function == "input" {
		fc = 0x04
	}
	m.tx++
	req := make([]byte, 12)
	binary.BigEndian.PutUint16(req[0:], m.tx)
	binary.BigEndian.PutUint16(req[2:], 0) // protocol id
	binary.BigEndian.PutUint16(req[4:], 6) // unit + PDU(5)
	req[6] = m.cfg.UnitID
	req[7] = fc
	binary.BigEndian.PutUint16(req[8:], addr)
	binary.BigEndian.PutUint16(req[10:], uint16(qty)) //nolint:gosec // qty ≤ 3
	_ = m.conn.SetDeadline(time.Now().Add(m.cfg.Timeout.D()))
	if _, err := m.conn.Write(req); err != nil {
		m.reset()
		return nil, fmt.Errorf("modbus: gửi: %w", err)
	}
	hdr := make([]byte, 7)
	if _, err := io.ReadFull(m.conn, hdr); err != nil {
		m.reset()
		return nil, fmt.Errorf("modbus: đọc MBAP: %w", err)
	}
	if binary.BigEndian.Uint16(hdr[0:]) != m.tx || binary.BigEndian.Uint16(hdr[2:]) != 0 {
		m.reset()
		return nil, errors.New("modbus: transaction/protocol id không khớp")
	}
	n := int(binary.BigEndian.Uint16(hdr[4:]))
	if n < 2 || n > 253 {
		m.reset()
		return nil, errors.New("modbus: độ dài MBAP không hợp lệ")
	}
	pdu := make([]byte, n-1)
	if _, err := io.ReadFull(m.conn, pdu); err != nil {
		m.reset()
		return nil, fmt.Errorf("modbus: đọc PDU: %w", err)
	}
	if pdu[0] == fc|0x80 {
		return nil, &ModbusException{Function: fc, Code: pdu[1]}
	}
	if pdu[0] != fc || len(pdu) < 2 || int(pdu[1]) != 2*qty || len(pdu) != 2+2*qty {
		m.reset()
		return nil, errors.New("modbus: phản hồi sai dạng")
	}
	out := make([]uint16, qty)
	for i := range out {
		out[i] = binary.BigEndian.Uint16(pdu[2+2*i:])
	}
	return out, nil
}

// Next đọc khối lượng + cờ ổn định theo chu kỳ PollInterval.
func (m *ModbusTCP) Next(ctx context.Context) (Sample, error) {
	if m.ticker == nil {
		m.ticker = time.NewTicker(m.cfg.PollInterval.D())
	}
	select {
	case <-ctx.Done():
		return Sample{}, ctx.Err()
	case <-m.ticker.C:
	}
	words, err := m.readRegisters(ctx, m.cfg.WeightRegister, m.cfg.WeightWords)
	if err != nil {
		return Sample{}, err
	}
	var status []uint16
	if m.cfg.StableRegister != nil {
		if status, err = m.readRegisters(ctx, *m.cfg.StableRegister, 1); err != nil {
			return Sample{}, err
		}
	}
	return m.decode(words, status)
}

// decode quy đổi thanh ghi thành số đo.
func (m *ModbusTCP) decode(words, status []uint16) (Sample, error) {
	var v int64
	if m.cfg.WeightWords == 1 {
		if m.cfg.Unsigned {
			v = int64(words[0])
		} else {
			v = int64(int16(words[0])) //nolint:gosec // bù hai có chủ đích
		}
	} else {
		hi, lo := words[0], words[1]
		if m.cfg.WordOrder == "little" {
			hi, lo = lo, hi
		}
		u := uint32(hi)<<16 | uint32(lo)
		if m.cfg.Unsigned {
			v = int64(u)
		} else {
			v = int64(int32(u)) //nolint:gosec // bù hai có chủ đích
		}
	}
	kg, err := normalizeKg(scaledDecimal(v, m.cfg.Decimals), m.cfg.Unit)
	if err != nil {
		return Sample{}, fmt.Errorf("modbus: giá trị %d: %w", v, err)
	}
	s := Sample{ValueKg: kg, Unit: m.cfg.Unit}
	if status != nil {
		s.Stable = status[0]&m.cfg.StableMask != 0
		if m.cfg.StableInvert {
			s.Stable = !s.Stable
		}
	} else {
		if kg == m.prev {
			m.same++
		} else {
			m.prev, m.same = kg, 1
		}
		s.Stable = m.same >= m.cfg.StableSamples
	}
	if m.cfg.IncludeRaw {
		raw := make([]byte, 0, modbusRawBytes)
		for _, w := range append(append([]uint16(nil), words...), status...) {
			raw = binary.BigEndian.AppendUint16(raw, w)
		}
		s.Raw = raw
	}
	return s, nil
}

// scaledDecimal viết v / 10^dec thành chuỗi thập phân (không float).
func scaledDecimal(v int64, dec int) string {
	neg := v < 0
	if neg {
		v = -v
	}
	s := strconv.FormatInt(v, 10)
	if dec > 0 {
		if len(s) <= dec {
			s = strings.Repeat("0", dec-len(s)+1) + s
		}
		s = s[:len(s)-dec] + "." + s[len(s)-dec:]
	}
	if neg {
		s = "-" + s
	}
	return s
}

// Close đóng kết nối.
func (m *ModbusTCP) Close() error {
	if m.ticker != nil {
		m.ticker.Stop()
	}
	m.reset()
	return nil
}
