package main

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

func simRun(t *testing.T, cfg SimConfig, n int) []Sample {
	t.Helper()
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	s := NewSimulator(cfg)
	out := make([]Sample, n)
	for i := range out {
		v, err := s.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		out[i] = v
	}
	return out
}

// Bộ mô phỏng tất định (R-95/e2e): cùng hạt giống → cùng dãy; chu kỳ trống → lên tải → ổn định → dỡ.
func TestSimulatorDeterministicCycles(t *testing.T) {
	a := simRun(t, SimConfig{Seed: 42}, 240)
	b := simRun(t, SimConfig{Seed: 42}, 240)
	c := simRun(t, SimConfig{Seed: 43}, 240)
	same := true
	for i := range a {
		if a[i].ValueKg != b[i].ValueKg || a[i].Stable != b[i].Stable {
			t.Fatalf("không tất định tại %d", i)
		}
		if a[i].ValueKg != c[i].ValueKg {
			same = false
		}
		if _, err := toReading(a[i], testT0); err != nil {
			t.Fatalf("số đo %d sai hợp đồng: %v", i, err)
		}
	}
	if same {
		t.Fatal("hạt giống khác phải cho dãy khác")
	}
	// Chu kỳ mặc định 4+6+10+4 = 24: pha ổn định (chỉ số 10..19) báo stable, lên/dỡ tải thì không.
	for cyc := range 10 {
		base := cyc * 24
		for i := range 24 {
			want := i < 4 || (i >= 10 && i < 20)
			if a[base+i].Stable != want {
				t.Fatalf("chu kỳ %d, mẫu %d: stable %v", cyc, i, a[base+i].Stable)
			}
		}
	}
	// Tải cố định (ca vàng): giá trị ổn định nằm trong ±noise quanh 96800 kg.
	g := simRun(t, SimConfig{Seed: 1, LoadsKg: []string{"96800.000"}, NoiseG: 1}, 24)
	for i := 10; i < 20; i++ {
		if v := g[i].ValueKg; v < "96799.999" || v > "96800.001" {
			t.Fatalf("pha ổn định %s", v)
		}
	}
}

func TestNormalizeKg(t *testing.T) {
	for _, c := range []struct{ in, unit, want string }{
		{"+001250.5", "kg", "1250.500"}, {"-0.000", "kg", "0.000"}, {"1.2345", "t", "1234.500"}, {"-12", "kg", "-12.000"},
	} {
		got, err := normalizeKg(c.in, c.unit)
		if err != nil || got != c.want {
			t.Fatalf("%s %s → %s (%v), cần %s", c.in, c.unit, got, err, c.want)
		}
	}
	for _, bad := range []string{"1.2345", "abc", ""} {
		if _, err := normalizeKg(bad, "kg"); err == nil {
			t.Fatalf("mong lỗi với %q", bad)
		}
	}
	if gramsToKg(-5) != "-0.005" || gramsToKg(0) != "0.000" || gramsToKg(1250520) != "1250.520" {
		t.Fatal("gramsToKg")
	}
}

// serial-ascii qua cầu nối TCP (chế độ kiểm thử của cổng nối tiếp).
func TestSerialASCIIOverTCPBridge(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		_, _ = io.WriteString(c, "ST,GS,+001250.5kg\r\nGARBAGE\r\nUS,NT,-0000.2kg\r\nST,GS,  1.25t\r\n")
		time.Sleep(time.Second)
	}()
	cfg := SerialConfig{Transport: "tcp://" + ln.Addr().String(), IncludeRaw: true, ReadTimeout: Duration(500 * time.Millisecond)}
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	a, err := NewSerialASCII(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	ctx := context.Background()
	s, err := a.Next(ctx)
	if err != nil || s.ValueKg != "1250.500" || !s.Stable || s.Unit != "kg" || string(s.Raw) != "ST,GS,+001250.5kg\r\n" {
		t.Fatalf("khung 1: %+v %v", s, err)
	}
	if r, err := toReading(s, testT0); err != nil || r.Raw != "U1QsR1MsKzAwMTI1MC41a2cNCg==" {
		t.Fatalf("raw base64 khác vector: %+v %v", r, err)
	}
	var fe *frameError
	if _, err := a.Next(ctx); !errors.As(err, &fe) {
		t.Fatalf("khung rác: %v", err)
	}
	s, err = a.Next(ctx)
	if err != nil || s.ValueKg != "-0.200" || s.Stable {
		t.Fatalf("khung 3: %+v %v", s, err)
	}
	s, err = a.Next(ctx)
	if err != nil || s.ValueKg != "1250.000" || s.Unit != "t" || !s.Stable {
		t.Fatalf("khung 4: %+v %v", s, err)
	}
	// Hết dữ liệu → hết hạn đọc → lỗi kết nối (sẽ kết nối lại).
	if _, err := a.Next(ctx); err == nil || errors.As(err, &fe) {
		t.Fatalf("mong lỗi đọc, có %v", err)
	}
}

// Máy chủ Modbus TCP giả: thanh ghi 0..1 = khối lượng (int32, ABCD), 2 = trạng thái.
func fakeModbus(t *testing.T, regs []uint16) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = c.Close() }()
		for {
			req := make([]byte, 12)
			if _, err := io.ReadFull(c, req); err != nil {
				return
			}
			fc := req[7]
			addr := int(binary.BigEndian.Uint16(req[8:]))
			qty := int(binary.BigEndian.Uint16(req[10:]))
			var pdu []byte
			if fc != 0x03 || addr+qty > len(regs) {
				pdu = []byte{fc | 0x80, 2}
			} else {
				pdu = []byte{fc, byte(2 * qty)}
				for _, r := range regs[addr : addr+qty] {
					pdu = binary.BigEndian.AppendUint16(pdu, r)
				}
			}
			resp := make([]byte, 7, 7+len(pdu))
			copy(resp, req[:4])
			binary.BigEndian.PutUint16(resp[4:], uint16(len(pdu)+1))
			resp[6] = req[6]
			if _, err := c.Write(append(resp, pdu...)); err != nil {
				return
			}
		}
	}()
	return ln.Addr().String()
}

func TestModbusTCPReadsWeightAndStable(t *testing.T) {
	// −1250.5 kg với decimals = 1 → −12505 = 0xFFFFCF27.
	addr := fakeModbus(t, []uint16{0xFFFF, 0xCF27, 0x0001})
	st := uint16(2)
	cfg := ModbusConfig{Address: addr, Decimals: 1, StableRegister: &st, PollInterval: Duration(time.Millisecond), IncludeRaw: true}
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	m := NewModbusTCP(cfg)
	defer func() { _ = m.Close() }()
	s, err := m.Next(context.Background())
	if err != nil || s.ValueKg != "-1250.500" || !s.Stable || len(s.Raw) != 6 {
		t.Fatalf("%+v %v", s, err)
	}
	// Thanh ghi ngoài phạm vi → ngoại lệ Modbus.
	bad := uint16(9)
	m.cfg.StableRegister = &bad
	var ex *ModbusException
	if _, err := m.Next(context.Background()); !errors.As(err, &ex) || ex.Code != 2 {
		t.Fatalf("mong ngoại lệ 2, có %v", err)
	}
}

func TestModbusSoftwareStability(t *testing.T) {
	addr := fakeModbus(t, []uint16{0x0000, 0x3039}) // 12345 → 12.345 t = 12345.000 kg
	cfg := ModbusConfig{Address: addr, Decimals: 3, Unit: "t", PollInterval: Duration(time.Millisecond)}
	if err := cfg.normalize(); err != nil {
		t.Fatal(err)
	}
	m := NewModbusTCP(cfg)
	defer func() { _ = m.Close() }()
	for i := range 4 {
		s, err := m.Next(context.Background())
		if err != nil || s.ValueKg != "12345.000" {
			t.Fatalf("%+v %v", s, err)
		}
		if s.Stable != (i >= 2) {
			t.Fatalf("lần %d stable %v", i, s.Stable)
		}
	}
	if scaledDecimal(-5, 3) != "-0.005" || scaledDecimal(12, 0) != "12" {
		t.Fatal("scaledDecimal")
	}
}
