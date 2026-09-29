package ingest

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

var (
	goldenBatchA = uuid.MustParse("01920000-0000-7000-8000-00000000000a")
	goldenBatchB = uuid.MustParse("01920000-0000-7000-8000-00000000000b")
	goldenT0     = time.Date(2026, 12, 3, 0, 14, 22, 0, time.UTC)
)

// mk dựng cửa sổ từ các cặp (giá trị, ổn định) bắt đầu ở counter from; nửa đầu thuộc lô A, nửa sau lô B.
func mk(from int64, vals ...any) []WindowReading {
	var out []WindowReading
	n := len(vals) / 2
	for i := 0; i < n; i++ {
		b := goldenBatchA
		if i >= n/2 {
			b = goldenBatchB
		}
		out = append(out, WindowReading{
			Counter: from + int64(i), DeviceTime: goldenT0.Add(time.Duration(i) * 500 * time.Millisecond),
			ValueKg: mass.MustKg(vals[2*i].(string)), Stable: vals[2*i+1].(bool), Unit: "kg", BatchID: b,
			ReceivedAt: goldenT0.Add(time.Duration(i) * time.Second),
		})
	}
	return out
}

func win(from, to int64) port.Window {
	return port.Window{DeviceID: "scale-hub01-in", Epoch: 1, CounterFrom: from, CounterTo: to}
}

// Golden weigh.integrate.v1 (tạm, chờ C-6). Số từ seed prototype: proto plant-weighing
// gross 138 400 − tare 41 600 = 96,8 t (lần xếp 5800/5700 cuộn, cờ +10 %);
// tests/field-bales: 130 000 − 42 000 = 88 t; 40 000 ≤ 41 000 → từ chối.
func TestIntegrateV1Golden(t *testing.T) {
	cases := []struct {
		name     string
		w        port.Window
		readings []WindowReading
		tare     string
		net      string
		used     int
		err      error
		gaps     []int64
	}{
		{
			name: "proto seed 96.8 t (xe lên cân, dao động, ổn định, một số đo nhiễu không ổn định)",
			w:    win(1, 9),
			readings: mk(1,
				"12000.000", false, "98000.000", false, "138420.000", false,
				"138400.000", true, "138380.000", true, "138400.000", true, "138410.000", true, "138400.000", true,
				"150000.000", false),
			tare: "41600.000", net: "96800.000", used: 5,
		},
		{
			name:     "proto field-bales 88 t, số chẵn số ổn định → trung bình hai số giữa",
			w:        win(10, 13),
			readings: mk(10, "129990.000", true, "130000.000", true, "130000.000", true, "130020.000", true),
			tare:     "42000.000", net: "88000.000", used: 4,
		},
		{
			name:     "trung bình hai số giữa làm tròn nửa xa 0 về 3 chữ số",
			w:        win(1, 2),
			readings: mk(1, "1000.001", true, "1000.002", true),
			tare:     "0.000", net: "1000.002", used: 2,
		},
		{
			name:     "proto: tổng ≤ bì → từ chối",
			w:        win(1, 2),
			readings: mk(1, "40000.000", true, "40000.000", true),
			tare:     "41000.000", err: ErrNonPositiveNet,
		},
		{
			name:     "không có số đo ổn định",
			w:        win(1, 2),
			readings: mk(1, "500.000", false, "510.000", false),
			tare:     "0.000", err: ErrNoStableReadings,
		},
		{
			name:     "thiếu bộ đếm giữa và cuối cửa sổ → ErrWindowIncomplete, không nội suy",
			w:        win(1, 6),
			readings: append(mk(1, "1.000", true, "1.000", true), mk(5, "1.000", true)...),
			tare:     "0.000", err: port.ErrWindowIncomplete, gaps: []int64{3, 4, 6},
		},
		{
			name:     "trùng bộ đếm → lỗi toàn vẹn",
			w:        win(1, 3),
			readings: append(mk(1, "1.000", true, "1.000", true), mk(2, "1.000", true)...),
			tare:     "0.000", err: ErrWindowCorrupt,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res, err := IntegrateV1(c.w, c.readings, mass.MustKg(c.tare))
			if !errors.Is(err, c.err) {
				t.Fatalf("lỗi: mong %v, nhận %v", c.err, err)
			}
			if !reflect.DeepEqual(res.Gaps, c.gaps) {
				t.Fatalf("gaps %v, mong %v", res.Gaps, c.gaps)
			}
			if c.err != nil {
				return
			}
			if res.NetKg.String() != c.net || res.ReadingsUsed != c.used || res.ReadingsTotal != len(c.readings) {
				t.Fatalf("net %s used %d/%d; mong %s used %d", res.NetKg, res.ReadingsUsed, res.ReadingsTotal, c.net, c.used)
			}
			if res.RuleVersion != RuleV1 || res.Window != c.w || len(res.ReadingsSHA256) != 64 {
				t.Fatalf("thiếu rule_version/cửa sổ/băm: %+v", res.Integration)
			}
		})
	}
}

// Tích phân tất định: cùng cửa sổ + cùng rule_version → cùng kết quả, cùng băm (g3-conventions §7).
func TestIntegrateV1Deterministic(t *testing.T) {
	r := mk(1, "138400.000", true, "138380.000", true, "138400.000", true, "138410.000", true)
	a, err := IntegrateV1(win(1, 4), r, mass.MustKg("41600.000"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		b, err := IntegrateV1(win(1, 4), append([]WindowReading(nil), r...), mass.MustKg("41600.000"))
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("không tất định: %+v vs %+v (%v)", a, b, err)
		}
	}
	if fmt.Sprint(a.BatchIDs) != fmt.Sprint([]uuid.UUID{goldenBatchA, goldenBatchB}) {
		t.Fatalf("batch ids: %v", a.BatchIDs)
	}
	if !a.LastReceivedAt.Equal(goldenT0.Add(3 * time.Second)) {
		t.Fatalf("EventTime = received_at số đo cuối: %v", a.LastReceivedAt)
	}
	// Băm golden của dạng chuẩn cửa sổ — đổi thuật toán băm phải là rule_version mới.
	const golden = "7d8b22e81e39b654b35f931112d4d07e47903902eaacaca62bf7f09343db4843"
	if a.ReadingsSHA256 != golden {
		t.Fatalf("băm cửa sổ %s, mong %s", a.ReadingsSHA256, golden)
	}
	// Sửa một số đo → băm khác.
	r2 := append([]WindowReading(nil), r...)
	r2[1].ValueKg = mass.MustKg("138381.000")
	c, _ := IntegrateV1(win(1, 4), r2, mass.MustKg("41600.000"))
	if c.ReadingsSHA256 == a.ReadingsSHA256 {
		t.Fatal("băm phải đổi khi số đo đổi")
	}
}
