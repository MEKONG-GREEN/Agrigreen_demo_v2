package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Agent ghép bộ điều hợp → Store (WAL) → Sender, cùng NTP, health và theo dõi cập nhật.
type Agent struct {
	cfg     *Config
	clk     clock.Clock
	log     *slog.Logger
	key     ed25519.PrivateKey
	store   *Store
	adapter Adapter
	ntp     *NTPTracker
	m       *Metrics
	sender  *Sender
	health  *Health
}

// NewAgent mở trạng thái (kiểm mất đĩa), khóa, bộ điều hợp. client nil → mTLS từ cấu hình.
func NewAgent(cfg *Config, clk clock.Clock, client *http.Client, log *slog.Logger) (*Agent, error) {
	key, err := LoadSourceKey(cfg.KeyFile)
	var pub ed25519.PublicKey
	switch {
	case err == nil:
		pub = key.Public().(ed25519.PublicKey)
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, err
	}
	store, err := OpenStore(cfg, pub, log)
	if err != nil {
		return nil, err
	}
	ad, err := NewAdapter(cfg.Adapter)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	m := NewMetrics(clk)
	ntp := NewNTPTracker(cfg.NTP, clk, log)
	sender, err := NewSender(cfg, store, key, ntp, m, clk, client, log)
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	a := &Agent{cfg: cfg, clk: clk, log: log, key: key, store: store, adapter: ad, ntp: ntp, m: m, sender: sender}
	a.health = &Health{M: m, Store: store, NTP: ntp, Cfg: cfg, Version: version, Start: clk.Now()}
	return a, nil
}

// toReading gán device_time cho một mẫu và kiểm theo hợp đồng edgewire.
func toReading(s Sample, now time.Time) (edgewire.Reading, error) {
	r := edgewire.Reading{Counter: 1, DeviceTime: edgewire.FormatDeviceTime(now), ValueKg: s.ValueKg, Stable: s.Stable, Unit: s.Unit}
	if len(s.Raw) > 0 {
		raw := s.Raw
		if len(raw) > edgewire.MaxRawBytes {
			raw = raw[:edgewire.MaxRawBytes]
		}
		r.Raw = base64.StdEncoding.EncodeToString(raw)
	}
	// Kiểm một số đo bằng lô một phần tử (quy tắc reading của edgewire không xuất ra ngoài).
	b := edgewire.Batch{Format: edgewire.Format, DeviceID: "chk", Epoch: 1, CounterFrom: 1, CounterTo: 1, KeyID: "0000000000000000", Readings: []edgewire.Reading{r}}
	if err := b.Validate(); err != nil {
		return r, err
	}
	return r, nil
}

// ingestOne ghi một mẫu vào đệm. Đệm đầy → từ chối + báo động (không ghi đè).
func (a *Agent) ingestOne(s Sample) error {
	r, err := toReading(s, a.clk.Now())
	if err != nil {
		a.m.AdapterErrors.Add(1)
		a.log.Warn("số đo không hợp lệ theo hợp đồng", "err", err.Error())
		return nil
	}
	if _, err := a.store.Append(r); err != nil {
		if errors.Is(err, ErrBufferFull) {
			if a.m.ReadingsRefused.Add(1) == 1 || a.m.ReadingsRefused.Load()%1000 == 0 {
				a.log.Error("ĐỆM ĐẦY — từ chối số đo mới", "refused_total", a.m.ReadingsRefused.Load())
			}
			a.m.Alarm(AlarmBufferFull, "đệm đầy, đang từ chối số đo mới")
			return nil
		}
		return err
	}
	a.m.Clear(AlarmBufferFull)
	a.m.ReadingsTotal.Add(1)
	a.m.MarkReading()
	a.sender.Notify()
	return nil
}

func (a *Agent) readLoop(ctx context.Context) error {
	for {
		s, err := a.adapter.Next(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			a.m.AdapterErrors.Add(1)
			var fe *frameError
			if errors.As(err, &fe) {
				continue
			}
			a.log.Warn("đọc đầu cân lỗi", "err", err.Error())
			if sleepCtx(ctx, time.Second) != nil {
				return nil
			}
			continue
		}
		if err := a.ingestOne(s); err != nil {
			return fmt.Errorf("ghi đệm: %w", err)
		}
	}
}

// trialWatch xác nhận bản cập nhật đang thử khi khỏe liên tục healthy_after; không khỏe
// quá 3 × healthy_after → thoát lỗi để systemd khởi động lại (guard sẽ đếm và lùi lại).
func (a *Agent) trialWatch(ctx context.Context) error {
	root := a.cfg.Update.Root
	if root == "" {
		return nil
	}
	s, err := ReadSlots(root)
	if err != nil || !s.Trial || runningSlot(root) != s.Active {
		return nil
	}
	a.m.Alarm(AlarmUpdateTrial, "đang thử "+s.Versions[s.Active])
	start := a.clk.Now()
	var healthySince time.Time
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		now := a.clk.Now()
		if ok, _ := a.health.Evaluate(); ok {
			if healthySince.IsZero() {
				healthySince = now
			}
			if now.Sub(healthySince) >= a.cfg.Update.HealthyAfter.D() {
				if _, err := CommitUpdate(root, now); err != nil {
					return fmt.Errorf("xác nhận cập nhật: %w", err)
				}
				a.m.Clear(AlarmUpdateTrial)
				a.log.Info("bản cập nhật đã xác nhận", "version", version)
				return nil
			}
		} else {
			healthySince = time.Time{}
			if now.Sub(start) > 3*a.cfg.Update.HealthyAfter.D() {
				return errors.New("bản cập nhật đang thử không khỏe — thoát để guard lùi lại")
			}
		}
	}
}

// Run chạy agent tới khi ctx hủy hoặc có lỗi không phục hồi được.
func (a *Agent) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	st := a.store.State()
	a.log.Info("edge-agent khởi động", "version", version, "device_id", st.DeviceID, "epoch", st.Epoch, "key_id", st.KeyID,
		"next_counter", a.store.LastCounter()+1, "acked", st.AckedCounter, "adapter", a.cfg.Adapter.Type,
		"buffer_max_bytes", a.cfg.Buffer.MaxBytes)

	ln, err := net.Listen("tcp", a.cfg.Health.Listen)
	if err != nil {
		return fmt.Errorf("health.listen: %w", err)
	}
	srv := &http.Server{Handler: a.health.Handler(), ReadHeaderTimeout: 5 * time.Second}

	var wg sync.WaitGroup
	errc := make(chan error, 5)
	spawn := func(f func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := f(ctx); err != nil {
				errc <- err
				cancel()
			}
		}()
	}
	spawn(func(context.Context) error {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	})
	spawn(func(ctx context.Context) error { a.ntp.Run(ctx); return nil })
	spawn(a.readLoop)
	spawn(a.sender.Run)
	spawn(a.trialWatch)
	spawn(func(ctx context.Context) error { // checkpoint bộ đếm định kỳ
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return nil
			case <-t.C:
				if err := a.store.Checkpoint(); err != nil {
					return err
				}
			}
		}
	})

	<-ctx.Done()
	sctx, scancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(sctx)
	scancel()
	wg.Wait()
	_ = a.adapter.Close()
	close(errc)
	var errs []error
	for e := range errc {
		errs = append(errs, e)
	}
	errs = append(errs, a.store.Close())
	return errors.Join(errs...)
}
