package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// Sample là một số đo do bộ điều hợp đầu cân trả về, TRƯỚC khi agent gán bộ đếm và
// device_time. ValueKg đã quy đổi về kg, đúng 3 chữ số thập phân (R-10).
type Sample struct {
	ValueKg string
	Stable  bool
	Unit    string // edgewire.UnitKg | edgewire.UnitT (đơn vị đầu cân hiển thị)
	Raw     []byte // khung gốc (tùy chọn, ≤ edgewire.MaxRawBytes)
}

// Adapter đọc đầu cân. Next chặn tới khi có số đo kế tiếp (bộ điều hợp tự điều
// nhịp theo tốc độ cấu hình) hoặc ctx bị hủy. Lỗi của Next là lỗi đọc tạm thời
// (agent đếm vào edge_adapter_errors_total, chờ rồi gọi lại); bộ điều hợp tự kết
// nối lại. Bộ điều hợp chính thức chờ chốt mẫu cân (C-6).
type Adapter interface {
	Next(ctx context.Context) (Sample, error)
	Close() error
}

// Loại bộ điều hợp.
const (
	AdapterSimulator   = "simulator"
	AdapterSerialASCII = "serial-ascii"
	AdapterModbusTCP   = "modbus-tcp"
)

// AdapterConfig chọn và cấu hình bộ điều hợp.
type AdapterConfig struct {
	Type        string        `json:"type"`
	Simulator   *SimConfig    `json:"simulator,omitempty"`
	SerialASCII *SerialConfig `json:"serial_ascii,omitempty"`
	ModbusTCP   *ModbusConfig `json:"modbus_tcp,omitempty"`
}

func (a *AdapterConfig) normalize() error {
	switch a.Type {
	case AdapterSimulator:
		if a.Simulator == nil {
			a.Simulator = &SimConfig{}
		}
		return a.Simulator.normalize()
	case AdapterSerialASCII:
		if a.SerialASCII == nil {
			return errors.New("adapter.serial_ascii bắt buộc khi type = serial-ascii")
		}
		return a.SerialASCII.normalize()
	case AdapterModbusTCP:
		if a.ModbusTCP == nil {
			return errors.New("adapter.modbus_tcp bắt buộc khi type = modbus-tcp")
		}
		return a.ModbusTCP.normalize()
	default:
		return fmt.Errorf("adapter.type %q không hỗ trợ (simulator | serial-ascii | modbus-tcp)", a.Type)
	}
}

// RawMaxBytes là số byte raw tối đa mỗi số đo (0 = không ghi raw) — dùng định cỡ đệm.
func (a *AdapterConfig) RawMaxBytes() int {
	if a.Type == AdapterSerialASCII && a.SerialASCII != nil && a.SerialASCII.IncludeRaw {
		return a.SerialASCII.MaxFrameBytes
	}
	if a.Type == AdapterModbusTCP && a.ModbusTCP != nil && a.ModbusTCP.IncludeRaw {
		return modbusRawBytes
	}
	return 0
}

// NewAdapter tạo bộ điều hợp theo cấu hình.
func NewAdapter(a AdapterConfig) (Adapter, error) {
	switch a.Type {
	case AdapterSimulator:
		return NewSimulator(*a.Simulator), nil
	case AdapterSerialASCII:
		return NewSerialASCII(*a.SerialASCII)
	case AdapterModbusTCP:
		return NewModbusTCP(*a.ModbusTCP), nil
	}
	return nil, fmt.Errorf("adapter.type %q không hỗ trợ", a.Type)
}

// normalizeKg quy đổi số đọc của đầu cân (chuỗi thập phân, có thể có dấu '+' và số
// 0 đứng đầu) theo đơn vị hiển thị sang chuỗi kg đúng 3 chữ số thập phân.
func normalizeKg(value, unit string) (string, error) {
	v := strings.TrimSpace(value)
	v = strings.TrimPrefix(v, "+")
	if v == "" {
		return "", errors.New("giá trị rỗng")
	}
	var (
		k   mass.Kg
		err error
	)
	switch unit {
	case edgewire.UnitKg:
		k, err = mass.ParseKg(v)
	case edgewire.UnitT:
		k, err = mass.FromTonnes(v)
	default:
		return "", fmt.Errorf("đơn vị %q không hỗ trợ (kg | t)", unit)
	}
	if err != nil {
		return "", err
	}
	s := k.String()
	if s == "-0.000" {
		s = "0.000"
	}
	return s, nil
}

// gramsToKg viết số gam nguyên thành chuỗi kg 3 chữ số thập phân (không float).
func gramsToKg(g int64) string {
	neg := g < 0
	if neg {
		g = -g
	}
	s := fmt.Sprintf("%d.%03d", g/1000, g%1000)
	if neg && g != 0 {
		return "-" + s
	}
	return s
}
