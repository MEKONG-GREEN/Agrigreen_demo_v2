package main

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
)

// SimConfig cấu hình bộ mô phỏng đầu cân: chu kỳ tất định theo hạt giống
// trống → lên tải (chưa ổn định) → ổn định → dỡ tải (chưa ổn định). Dùng cho
// kiểm thử đầu-cuối và phép thử R-95 khi chưa có mẫu cân (C-6).
type SimConfig struct {
	Seed uint64 `json:"seed"`
	// RateHz: số số đo mỗi giây; 0 = không điều nhịp (kiểm thử).
	RateHz float64 `json:"rate_hz"`
	// LoadMinKg..LoadMaxKg: khoảng khối lượng tải ngẫu nhiên (kg nguyên).
	LoadMinKg int64 `json:"load_min_kg"`
	LoadMaxKg int64 `json:"load_max_kg"`
	// LoadsKg: nếu có, dùng lần lượt (vòng lại) các khối lượng tải chính xác này
	// thay cho ngẫu nhiên, vd ["96800.000"] cho ca vàng R-95.
	LoadsKg []string `json:"loads_kg"`
	// Số số đo của từng pha trong một chu kỳ.
	EmptySamples   int `json:"empty_samples"`
	SettleSamples  int `json:"settle_samples"`
	StableSamples  int `json:"stable_samples"`
	ReleaseSamples int `json:"release_samples"`
	// NoiseG: biên độ nhiễu (gam) ở pha ổn định; pha chưa ổn định nhiễu gấp 20.
	NoiseG int64 `json:"noise_g"`

	loadsG []int64
}

func (s *SimConfig) normalize() error {
	if s.RateHz < 0 {
		return errors.New("simulator.rate_hz phải ≥ 0")
	}
	if s.LoadMinKg == 0 && s.LoadMaxKg == 0 {
		s.LoadMinKg, s.LoadMaxKg = 500, 1500
	}
	if s.LoadMinKg <= 0 || s.LoadMaxKg < s.LoadMinKg || s.LoadMaxKg > 99_999_999 {
		return errors.New("simulator: cần 0 < load_min_kg ≤ load_max_kg ≤ 99999999")
	}
	s.loadsG = s.loadsG[:0]
	for _, l := range s.LoadsKg {
		k, err := mass.ParseKg(l)
		if err != nil || k.NonNegative() != nil {
			return fmt.Errorf("simulator.loads_kg %q: cần kg không âm, tối đa 3 chữ số thập phân", l)
		}
		g := k.Decimal().Shift(3).IntPart()
		s.loadsG = append(s.loadsG, g)
	}
	def := func(p *int, v int) {
		if *p == 0 {
			*p = v
		}
	}
	def(&s.EmptySamples, 4)
	def(&s.SettleSamples, 6)
	def(&s.StableSamples, 10)
	def(&s.ReleaseSamples, 4)
	if s.EmptySamples < 1 || s.SettleSamples < 1 || s.StableSamples < 1 || s.ReleaseSamples < 1 {
		return errors.New("simulator: số số đo mỗi pha phải ≥ 1")
	}
	if s.NoiseG == 0 {
		s.NoiseG = 20
	}
	if s.NoiseG < 0 {
		return errors.New("simulator.noise_g phải ≥ 0")
	}
	return nil
}

// Pha của chu kỳ mô phỏng.
const (
	simEmpty = iota
	simSettle
	simStable
	simRelease
)

// Simulator là bộ điều hợp mô phỏng tất định: cùng cấu hình (cùng Seed) → cùng dãy số đo.
type Simulator struct {
	cfg    SimConfig
	rng    *rand.Rand
	phase  int
	i      int   // chỉ số trong pha
	target int64 // gam
	cycle  int
	ticker *time.Ticker
}

// NewSimulator tạo bộ mô phỏng; cfg đã normalize.
func NewSimulator(cfg SimConfig) *Simulator {
	if cfg.loadsG == nil && len(cfg.LoadsKg) > 0 {
		_ = cfg.normalize()
	}
	return &Simulator{cfg: cfg, rng: rand.New(rand.NewPCG(cfg.Seed, cfg.Seed^0x9e3779b97f4a7c15))} //nolint:gosec // mô phỏng tất định, không dùng cho mật mã
}

func (s *Simulator) noise(amp int64) int64 {
	if amp <= 0 {
		return 0
	}
	return s.rng.Int64N(2*amp+1) - amp
}

func (s *Simulator) nextTarget() int64 {
	defer func() { s.cycle++ }()
	if len(s.cfg.loadsG) > 0 {
		return s.cfg.loadsG[s.cycle%len(s.cfg.loadsG)]
	}
	lo, hi := s.cfg.LoadMinKg*1000, s.cfg.LoadMaxKg*1000
	return lo + s.rng.Int64N(hi-lo+1)
}

// sample sinh số đo kế tiếp (không điều nhịp).
func (s *Simulator) sample() Sample {
	var g int64
	stable := false
	n := s.cfg.NoiseG
	switch s.phase {
	case simEmpty:
		g, stable = s.noise(n/2), true
		s.advance(s.cfg.EmptySamples)
	case simSettle:
		if s.i == 0 {
			s.target = s.nextTarget()
		}
		g = s.target*int64(s.i+1)/int64(s.cfg.SettleSamples) + s.noise(20*n)
		s.advance(s.cfg.SettleSamples)
	case simStable:
		g, stable = s.target+s.noise(n), true
		s.advance(s.cfg.StableSamples)
	case simRelease:
		g = s.target*int64(s.cfg.ReleaseSamples-s.i-1)/int64(s.cfg.ReleaseSamples) + s.noise(20*n)
		s.advance(s.cfg.ReleaseSamples)
	}
	return Sample{ValueKg: gramsToKg(g), Stable: stable, Unit: edgewire.UnitKg}
}

func (s *Simulator) advance(n int) {
	s.i++
	if s.i >= n {
		s.i = 0
		s.phase = (s.phase + 1) % 4
	}
}

// Next trả số đo kế tiếp, điều nhịp theo RateHz.
func (s *Simulator) Next(ctx context.Context) (Sample, error) {
	if s.cfg.RateHz > 0 {
		if s.ticker == nil {
			s.ticker = time.NewTicker(time.Duration(float64(time.Second) / s.cfg.RateHz))
		}
		select {
		case <-ctx.Done():
			return Sample{}, ctx.Err()
		case <-s.ticker.C:
		}
	} else if err := ctx.Err(); err != nil {
		return Sample{}, err
	}
	return s.sample(), nil
}

// Close dừng bộ điều nhịp.
func (s *Simulator) Close() error {
	if s.ticker != nil {
		s.ticker.Stop()
	}
	return nil
}
