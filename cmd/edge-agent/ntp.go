package main

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// SNTP tối giản (RFC 4330) qua UDP, chỉ thư viện chuẩn. Kết quả CHỈ là thông tin
// (ntp_offset_ms trong lô) — agent không chỉnh đồng hồ và không bao giờ sắp thứ tự
// theo đồng hồ (ADR-0019 §4). Đồng hồ hệ điều hành do chrony/systemd-timesyncd lo.

// ntpEpochOffset: số giây từ 1900-01-01 tới 1970-01-01.
const ntpEpochOffset = 2208988800

func toNTP(t time.Time) uint64 {
	secs := uint64(t.Unix()) + ntpEpochOffset    //nolint:gosec // thời điểm sau 1970
	frac := (uint64(t.Nanosecond()) << 32) / 1e9 //nolint:gosec // 0..1e9
	return secs<<32 | frac
}

func fromNTP(v uint64) time.Time {
	secs := int64(v>>32) - ntpEpochOffset     //nolint:gosec // < 2^32
	nsec := (int64(v&0xffffffff) * 1e9) >> 32 //nolint:gosec // < 2^32
	return time.Unix(secs, nsec).UTC()
}

// QuerySNTP hỏi một máy chủ NTP; trả độ lệch (đồng hồ máy chủ − đồng hồ thiết bị) và RTT.
func QuerySNTP(ctx context.Context, server string, timeout time.Duration, clk clock.Clock) (offset, rtt time.Duration, err error) {
	if _, _, e := net.SplitHostPort(server); e != nil {
		server = net.JoinHostPort(server, "123")
	}
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "udp", server)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	req := make([]byte, 48)
	req[0] = 0<<6 | 4<<3 | 3 // LI=0, VN=4, Mode=3 (client)
	t1 := clk.Now()
	tx := toNTP(t1)
	binary.BigEndian.PutUint64(req[40:], tx)
	if _, err := conn.Write(req); err != nil {
		return 0, 0, err
	}
	resp := make([]byte, 48)
	n, err := conn.Read(resp)
	t4 := clk.Now()
	if err != nil {
		return 0, 0, err
	}
	if n < 48 {
		return 0, 0, errors.New("sntp: phản hồi ngắn")
	}
	if mode := resp[0] & 0x7; mode != 4 {
		return 0, 0, fmt.Errorf("sntp: mode %d, cần 4 (server)", mode)
	}
	if li := resp[0] >> 6; li == 3 {
		return 0, 0, errors.New("sntp: máy chủ chưa đồng bộ (LI=3)")
	}
	if st := resp[1]; st == 0 || st > 15 {
		return 0, 0, fmt.Errorf("sntp: stratum %d không dùng được", st)
	}
	if binary.BigEndian.Uint64(resp[24:]) != tx {
		return 0, 0, errors.New("sntp: originate timestamp không khớp yêu cầu")
	}
	t2 := fromNTP(binary.BigEndian.Uint64(resp[32:]))
	t3raw := binary.BigEndian.Uint64(resp[40:])
	if t3raw == 0 {
		return 0, 0, errors.New("sntp: transmit timestamp rỗng")
	}
	t3 := fromNTP(t3raw)
	offset = (t2.Sub(t1) + t3.Sub(t4)) / 2
	rtt = t4.Sub(t1) - t3.Sub(t2)
	return offset, rtt, nil
}

// NTPTracker giữ độ lệch đo gần nhất.
type NTPTracker struct {
	mu     sync.Mutex
	offset time.Duration
	at     time.Time
	ok     bool
	cfg    NTPConfig
	clk    clock.Clock
	log    *slog.Logger
	errors uint64
}

// NewNTPTracker tạo bộ theo dõi.
func NewNTPTracker(cfg NTPConfig, clk clock.Clock, log *slog.Logger) *NTPTracker {
	return &NTPTracker{cfg: cfg, clk: clk, log: log}
}

// Set ghi một độ lệch mới đo được.
func (t *NTPTracker) Set(off time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.offset, t.at, t.ok = off, t.clk.Now(), true
}

// OffsetMs trả độ lệch (ms) nếu còn mới và hợp lệ, nil nếu chưa đồng bộ.
func (t *NTPTracker) OffsetMs() *int {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.ok || t.clk.Now().Sub(t.at) > t.cfg.MaxAge.D() {
		return nil
	}
	ms := t.offset.Milliseconds()
	if ms > edgewire.MaxNTPOffsetMs || ms < -edgewire.MaxNTPOffsetMs {
		return nil
	}
	v := int(ms)
	return &v
}

// Errors là số lần hỏi NTP lỗi.
func (t *NTPTracker) Errors() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.errors
}

// Run hỏi NTP định kỳ tới khi ctx hủy.
func (t *NTPTracker) Run(ctx context.Context) {
	if t.cfg.Server == "" {
		return
	}
	tick := time.NewTicker(t.cfg.Interval.D())
	defer tick.Stop()
	for {
		off, rtt, err := QuerySNTP(ctx, t.cfg.Server, t.cfg.Timeout.D(), t.clk)
		if err != nil {
			t.mu.Lock()
			t.errors++
			t.mu.Unlock()
			t.log.Warn("NTP: hỏi lỗi", "server", t.cfg.Server, "err", err.Error())
		} else {
			t.Set(off)
			t.log.Debug("NTP", "offset_ms", off.Milliseconds(), "rtt_ms", rtt.Milliseconds())
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
