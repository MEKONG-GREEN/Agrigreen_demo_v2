package main

// Kế hoạch dữ liệu tổng hợp vùng MKG (R-95, G3-2): lệnh gom, số cuộn theo công đoạn,
// phiên cân + lô số đo ký Ed25519 kiểu agent biên, phiếu CUSTOMER_IN của khách với một
// tỷ lệ x % vượt dung sai, độ ẩm. Hàm thuần của (seed, quy mô, x %, ngày) — không float
// cho khối lượng (R-10): mọi khối lượng là kg nguyên, độ ẩm là phần trăm × 100.
//
// Lô đầu tiên luôn là lô GOLDEN theo seed prototype mekong-green: cuộn 5 800 → tập kết
// 5 700 → xếp ghe 4 400 (ghe thứ hai 1 300 cuộn là lệnh gom kế tiếp, chưa cân), HUB_IN
// 138 400 − 41 600 = 96 800 kg, ước 4 400 × 20 = 88 000 kg → +10 % → cờ (hợp đồng theo tấn).

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/google/uuid"
)

// MKGScale là quy mô kế hoạch MKG.
type MKGScale struct {
	Name string
	Lots int // số lô (kể cả lô golden)
}

var mkgScales = map[string]MKGScale{
	"small":  {Name: "small", Lots: 6},
	"medium": {Name: "medium", Lots: 40},
}

// MKGDevice là một thiết bị cân tổng hợp (khóa nguồn Ed25519 tất định theo seed).
type MKGDevice struct {
	ID          string
	Point       string // HUB_IN | HUB_OUT
	FacilityRef uuid.UUID
	KeySeed     [ed25519.SeedSize]byte
}

// Key trả khóa ký nguồn của thiết bị.
func (d MKGDevice) Key() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(d.KeySeed[:]) }

// MKGReading là một số đo thô (kiểu agent biên / bộ mô phỏng: dao động rồi ổn định).
type MKGReading struct {
	ValueKg string
	Stable  bool
}

// MKGWeighing là một phiên cân ở một điểm cân: cửa sổ bộ đếm + số đo + bì + ẩm.
type MKGWeighing struct {
	Device      int // chỉ số trong MKGPlan.Devices
	CounterFrom int64
	CounterTo   int64
	Readings    []MKGReading // Counter = CounterFrom + i; có thể có số đo sau CounterTo (ngoài cửa sổ)
	GrossKg     int64
	TareKg      int64
	NetKg       int64
	MoisturePct string // "18.00"
}

// MKGPlot là số cuộn của một thửa theo ba công đoạn (FM-04: công đoạn sau ≤ công đoạn trước, ít hơn cần lý do).
type MKGPlot struct {
	Ref                      uuid.UUID
	Bale, Gather, BoatLoad   int
	GatherReason, LoadReason string
}

// MKGLot là một lệnh gom = một lô cân (G3: một lần xếp hàng một lệnh gom).
type MKGLot struct {
	Golden        bool
	LotRef        uuid.UUID // lô cân; lệnh gom do fieldops cấp id → liên kết tường minh lot_collection
	HTXRef        uuid.UUID
	Route         string // HUB | DIRECT
	Basis         string // per_tonne | per_bale
	Plots         []MKGPlot
	Rolls         int // Σ xếp ghe/xe
	HubIn, HubOut *MKGWeighing
	CustomerInKg  int64
	CustomerMoist string
	OverTolerance bool // CUSTOMER_IN lệch > 5 % so với HUB_OUT (mở tranh chấp)
	At            time.Time
}

// MKGPlan là toàn bộ dữ liệu MKG sẽ sinh.
type MKGPlan struct {
	Seed        uint64
	Scale       string
	OverPct     int
	CustomerRef uuid.UUID
	Devices     []MKGDevice
	HTX         []uuid.UUID
	Lots        []MKGLot
}

// Batch là nhóm số đo gửi một lô edge (tối đa 4 số đo — như agent gom theo chu kỳ).
const mkgBatchSize = 4

func kgStr(kg int64) string { return fmt.Sprintf("%d.000", kg) }

// pctStr định dạng phần trăm × 100 (vd 1800 → "18.00").
func pctStr(x100 int) string { return fmt.Sprintf("%d.%02d", x100/100, x100%100) }

// readingsFor sinh số đo của một phiên: dao động lúc chất hàng (không ổn định), năm số
// ổn định đối xứng quanh gross (trung vị = gross — weigh.integrate.v1), một số sau cửa sổ.
func readingsFor(r *rand.Rand, gross, tare int64) []MKGReading {
	j := int64(10 + r.IntN(20))
	return []MKGReading{
		{kgStr(tare), false}, {kgStr(tare + (gross-tare)*2/3), false}, {kgStr(gross + j), false},
		{kgStr(gross), true}, {kgStr(gross - j), true}, {kgStr(gross + j), true}, {kgStr(gross), true}, {kgStr(gross), true},
		{kgStr(gross + 2000 + j), false}, // xe kế tiếp lên cân: ngoài cửa sổ
	}
}

// BuildMKGPlan dựng kế hoạch MKG — tất định theo (seed, quy mô, x %, ngày).
func BuildMKGPlan(seed uint64, sc MKGScale, overPct int, today time.Time) MKGPlan {
	r := rand.New(rand.NewPCG(seed^0x6d6b67, seed^0x9e3779b97f4a7c15)) //nolint:gosec // dữ liệu tổng hợp tất định, không dùng cho bảo mật
	seq := 0
	next := func() uuid.UUID { seq++; return uuidV7(r, seq) }
	p := MKGPlan{Seed: seed, Scale: sc.Name, OverPct: overPct, CustomerRef: next()}
	for i, pt := range []string{"HUB_IN", "HUB_OUT"} {
		d := MKGDevice{ID: fmt.Sprintf("syn-scale-%d-%s", seed, map[int]string{0: "in", 1: "out"}[i]), Point: pt, FacilityRef: next()}
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], seed)
		d.KeySeed = sha256.Sum256(append([]byte("agrigreen-seed-synthetic-edge-key:"+pt+":"), b[:]...))
		p.Devices = append(p.Devices, d)
	}
	for i := 0; i < 3; i++ {
		p.HTX = append(p.HTX, next())
	}
	base := time.Date(today.Year(), today.Month(), today.Day(), 6, 0, 0, 0, today.Location()).AddDate(0, 0, -1)
	counters := [2]int64{1, 1}
	weigh := func(dev int, gross, tare int64, moist int) *MKGWeighing {
		rd := readingsFor(r, gross, tare)
		w := &MKGWeighing{Device: dev, CounterFrom: counters[dev], GrossKg: gross, TareKg: tare, NetKg: gross - tare,
			MoisturePct: pctStr(moist), Readings: rd}
		w.CounterTo = w.CounterFrom + int64(len(rd)) - 2 // số cuối ngoài cửa sổ
		counters[dev] += int64(len(rd))
		return w
	}
	// Số lô vượt dung sai thương mại (làm tròn gần nhất), chọn trong các lô HUB không golden.
	over := (sc.Lots*overPct + 50) / 100
	for i := 0; i < sc.Lots; i++ {
		lot := MKGLot{LotRef: next(), HTXRef: p.HTX[i%len(p.HTX)], Route: "HUB", Basis: "per_tonne",
			At: base.Add(time.Duration(i) * 20 * time.Minute)}
		if i == 0 {
			lot.Golden = true
			lot.Plots = []MKGPlot{{Ref: next(), Bale: 5800, Gather: 5700, BoatLoad: 4400,
				GatherReason: "100 cuộn ướt để lại bờ ruộng (proto seed)", LoadReason: "Ghe thứ hai 1 300 cuộn thuộc lệnh gom kế tiếp"}}
			lot.Rolls = 4400
			lot.HubIn = weigh(0, 138_400, 41_600, 1800)
			out := lot.HubIn.NetKg - lot.HubIn.NetKg*15/1000 // hao hụt sấy/lưu 1,5 %
			lot.HubOut = weigh(1, out+41_600, 41_600, 1450)
			lot.CustomerInKg, lot.CustomerMoist = out-out*5/1000, "14.00"
			p.Lots = append(p.Lots, lot)
			continue
		}
		if i%3 == 2 {
			lot.Basis = "per_bale"
		}
		if i%4 == 3 {
			lot.Route = "DIRECT"
		}
		kgPerRoll := int64(180 + r.IntN(50)) // 18,0–22,9 kg/cuộn × 10
		for k, n := 0, 1+r.IntN(3); k < n; k++ {
			bale := 800 + r.IntN(1700)
			pl := MKGPlot{Ref: next(), Bale: bale, Gather: bale, BoatLoad: bale}
			if r.IntN(3) == 0 {
				pl.Gather = bale - 1 - r.IntN(40)
				pl.GatherReason = "Cuộn rách khi tập kết, để lại bờ"
				pl.BoatLoad = pl.Gather
			}
			lot.Plots = append(lot.Plots, pl)
			lot.Rolls += pl.BoatLoad
		}
		net := int64(lot.Rolls) * kgPerRoll / 10
		moist := 1200 + r.IntN(1000) // 12,00–21,99 %
		if lot.Route == "DIRECT" {
			lot.CustomerInKg, lot.CustomerMoist = net, pctStr(moist)
			p.Lots = append(p.Lots, lot)
			continue
		}
		tare := int64(30_000 + r.IntN(15_000))
		lot.HubIn = weigh(0, net+tare, tare, moist)
		out := net - net*int64(5+r.IntN(20))/1000 // 0,5–2,4 %
		lot.HubOut = weigh(1, out+tare, tare, moist-200)
		diff := int64(r.IntN(30)) - 15 // ±1,5 %
		if over > 0 {
			lot.OverTolerance, diff = true, -int64(70+r.IntN(50)) // −7 … −11,9 %
			over--
		}
		lot.CustomerInKg, lot.CustomerMoist = out+out*diff/1000, pctStr(moist-250)
		p.Lots = append(p.Lots, lot)
	}
	return p
}

// batchesOf chia số đo của phiên thành các lô edge (bộ đếm liên tiếp).
func batchesOf(w *MKGWeighing) [][2]int {
	var out [][2]int
	for i := 0; i < len(w.Readings); i += mkgBatchSize {
		j := i + mkgBatchSize
		if j > len(w.Readings) {
			j = len(w.Readings)
		}
		out = append(out, [2]int{i, j})
	}
	return out
}
