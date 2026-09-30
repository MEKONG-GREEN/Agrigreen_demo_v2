package main

import (
	"context"
	"crypto/ed25519"
	"errors"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

var mkgToday = time.Date(2026, 9, 30, 10, 0, 0, 0, clock.Zone())

// R-95: kế hoạch MKG tất định theo seed; seed khác → dữ liệu khác.
func TestMKGPlanDeterministic(t *testing.T) {
	a := BuildMKGPlan(7, mkgScales["small"], 10, mkgToday)
	b := BuildMKGPlan(7, mkgScales["small"], 10, mkgToday)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("cùng seed phải ra cùng kế hoạch")
	}
	c := BuildMKGPlan(8, mkgScales["small"], 10, mkgToday)
	if reflect.DeepEqual(a.Lots[1], c.Lots[1]) {
		t.Fatal("seed khác phải khác dữ liệu")
	}
}

// Golden theo seed prototype (proto seed.ts, FM-09/FM-10): 5 800 → 5 700 → 4 400 cuộn,
// HUB_IN 138 400 − 41 600 = 96 800 kg, ước 4 400 × 20 = 88 000 kg → +10 % (> 5 % → cờ).
func TestMKGPlanGoldenPrototypeLot(t *testing.T) {
	for _, seed := range []uint64{1, 42} {
		g := BuildMKGPlan(seed, mkgScales["small"], 10, mkgToday).Lots[0]
		if !g.Golden || g.Route != "HUB" || g.Basis != "per_tonne" || len(g.Plots) != 1 {
			t.Fatalf("lô golden: %+v", g)
		}
		p := g.Plots[0]
		if p.Bale != 5800 || p.Gather != 5700 || p.BoatLoad != 4400 || p.GatherReason == "" || p.LoadReason == "" || g.Rolls != 4400 {
			t.Fatalf("cuộn golden: %+v", p)
		}
		if g.HubIn.GrossKg != 138_400 || g.HubIn.TareKg != 41_600 || g.HubIn.NetKg != 96_800 || g.HubIn.MoisturePct != "18.00" {
			t.Fatalf("HUB_IN golden: %+v", g.HubIn)
		}
		est := int64(g.Rolls) * 20
		if est != 88_000 || (g.HubIn.NetKg-est)*1000/est != 100 {
			t.Fatalf("chênh golden phải +10,0 %%: ước %d", est)
		}
		if g.OverTolerance || g.CustomerInKg <= 0 {
			t.Fatalf("CUSTOMER_IN golden: %+v", g)
		}
	}
}

// x % lô vượt dung sai thương mại (làm tròn gần nhất), chỉ lô HUB không golden; có lô DIRECT
// và cả hai cách tính giá; ẩm 12–22 %; bộ đếm thiết bị tăng dần không chồng.
func TestMKGPlanShares(t *testing.T) {
	for _, tc := range []struct {
		scale     string
		pct, want int
	}{{"small", 10, 1}, {"medium", 10, 4}, {"medium", 25, 10}, {"small", 0, 0}} {
		p := BuildMKGPlan(3, mkgScales[tc.scale], tc.pct, mkgToday)
		over, direct, perBale := 0, 0, 0
		last := [2]int64{}
		for _, l := range p.Lots {
			if l.OverTolerance {
				over++
				if l.Route != "HUB" || l.Golden {
					t.Fatalf("lô vượt dung sai phải là HUB không golden: %+v", l)
				}
				pct := (l.HubOut.NetKg - l.CustomerInKg) * 1000 / l.HubOut.NetKg
				if pct <= 50 {
					t.Fatalf("lô vượt dung sai lệch %d‰ ≤ 5 %%", pct)
				}
			}
			if l.Route == "DIRECT" {
				direct++
				if l.HubIn != nil || l.HubOut != nil {
					t.Fatal("DIRECT không cân ở hub")
				}
			}
			if l.Basis == "per_bale" {
				perBale++
			}
			for _, w := range []*MKGWeighing{l.HubIn, l.HubOut} {
				if w == nil {
					continue
				}
				if w.CounterFrom <= last[w.Device] {
					t.Fatalf("bộ đếm chồng: %d ≤ %d", w.CounterFrom, last[w.Device])
				}
				last[w.Device] = w.CounterFrom + int64(len(w.Readings)) - 1
				m, _ := strconv.ParseFloat(w.MoisturePct, 64)
				if m < 10 || m >= 22 {
					t.Fatalf("ẩm %s ngoài 10–22 %%", w.MoisturePct)
				}
				if med := medianStable(w); med != kgStr(w.GrossKg) {
					t.Fatalf("trung vị số đo ổn định %s ≠ gross %d", med, w.GrossKg)
				}
			}
		}
		if over != tc.want || direct == 0 || perBale == 0 {
			t.Fatalf("%s %d%%: vượt %d (muốn %d), DIRECT %d, per_bale %d", tc.scale, tc.pct, over, tc.want, direct, perBale)
		}
	}
}

// medianStable là trung vị số đo ổn định trong cửa sổ (weigh.integrate.v1 — tạm C-6).
func medianStable(w *MKGWeighing) string {
	var xs []int64
	for i, r := range w.Readings {
		if w.CounterFrom+int64(i) > w.CounterTo || !r.Stable {
			continue
		}
		n, _ := strconv.ParseInt(strings.TrimSuffix(r.ValueKg, ".000"), 10, 64)
		xs = append(xs, n)
	}
	sort.Slice(xs, func(a, b int) bool { return xs[a] < xs[b] })
	return kgStr(xs[len(xs)/2])
}

// Lô edge của kế hoạch ký bằng khóa tất định và kiểm được bằng edgewire.Verify.
func TestMKGPlanBatchesVerify(t *testing.T) {
	p := BuildMKGPlan(5, mkgScales["small"], 10, mkgToday)
	w := p.Lots[0].HubIn
	dev := p.Devices[w.Device]
	for _, b := range batchesOf(w) {
		batch := edgewire.Batch{Format: edgewire.Format, DeviceID: dev.ID, Epoch: 1, CounterFrom: w.CounterFrom + int64(b[0]),
			CounterTo: w.CounterFrom + int64(b[1]) - 1}
		for k := b[0]; k < b[1]; k++ {
			batch.Readings = append(batch.Readings, edgewire.Reading{Counter: w.CounterFrom + int64(k),
				DeviceTime: edgewire.FormatDeviceTime(mkgToday), ValueKg: w.Readings[k].ValueKg, Stable: w.Readings[k].Stable, Unit: edgewire.UnitKg})
		}
		if err := edgewire.Sign(dev.Key(), &batch); err != nil {
			t.Fatal(err)
		}
		if err := edgewire.Verify(dev.Key().Public().(ed25519.PublicKey), batch); err != nil {
			t.Fatal(err)
		}
	}
}

// R-95: kế hoạch MKG cũng từ chối production; tham số sai bị từ chối.
func TestMKGRefusesProduction(t *testing.T) {
	if _, err := RunMKG(context.Background(), MKGConfig{Env: "production", Scale: "small"}); !errors.Is(err, ErrProduction) {
		t.Fatalf("muốn ErrProduction, nhận %v", err)
	}
	for _, c := range []MKGConfig{{Env: "dev", Scale: "huge"}, {Env: "dev", Scale: "small", OverPct: 101}, {Env: "dev", Scale: "small"}} {
		if _, err := RunMKG(context.Background(), c); err == nil {
			t.Fatalf("%+v phải lỗi", c)
		}
	}
	if _, err := parseFlags([]string{"-plan", "x"}, nil); err == nil {
		t.Fatal("-plan lạ phải lỗi")
	}
}
