package main

import (
	"fmt"
	"io"
	"math"
	"slices"
	"sort"
	"sync"
	"time"
)

// Recorder gom độ trễ và kết quả theo thao tác (an toàn đồng thời).
type Recorder struct {
	mu       sync.Mutex
	ops      map[string]*opStats
	counters map[string]int64
}

type opStats struct {
	lat      []time.Duration
	ok       int64
	errors   int64
	dropped  int64 // lượt không phát được vì đã đủ số yêu cầu đang bay (máy phát bão hòa)
	byStatus map[string]int64
}

// NewRecorder tạo Recorder rỗng.
func NewRecorder() *Recorder {
	return &Recorder{ops: map[string]*opStats{}, counters: map[string]int64{}}
}

// Count cộng bộ đếm phụ (vd số bản ghi accepted/duplicate của sync push, số số đo đã nhận).
func (r *Recorder) Count(key string, n int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counters[key] += n
}

// Counters trả bản sao bộ đếm phụ.
func (r *Recorder) Counters() map[string]int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int64, len(r.counters))
	for k, v := range r.counters {
		out[k] = v
	}
	return out
}

func (r *Recorder) get(op string) *opStats {
	s := r.ops[op]
	if s == nil {
		s = &opStats{byStatus: map[string]int64{}}
		r.ops[op] = s
	}
	return s
}

// Observe ghi một lượt: status là mã HTTP hoặc nhãn lỗi ("net", "timeout"); ok = thành công nghiệp vụ.
func (r *Recorder) Observe(op string, d time.Duration, status string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.get(op)
	s.lat = append(s.lat, d)
	s.byStatus[status]++
	if ok {
		s.ok++
	} else {
		s.errors++
	}
}

// Drop ghi một lượt bị bỏ vì máy phát bão hòa.
func (r *Recorder) Drop(op string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.get(op).dropped++
}

// Summary là kết quả của một thao tác.
type Summary struct {
	Op        string           `json:"op"`
	Count     int64            `json:"count"`
	OK        int64            `json:"ok"`
	Errors    int64            `json:"errors"`
	Dropped   int64            `json:"dropped"`
	ErrorRate float64          `json:"error_rate"`
	RPS       float64          `json:"rps"`
	P50       time.Duration    `json:"p50_ns"`
	P95       time.Duration    `json:"p95_ns"`
	P99       time.Duration    `json:"p99_ns"`
	Max       time.Duration    `json:"max_ns"`
	ByStatus  map[string]int64 `json:"by_status"`
}

// Percentile theo phương pháp nearest-rank trên mẫu đã sắp xếp (p trong (0,100]).
func Percentile(sorted []time.Duration, p float64) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	rank := int(math.Ceil(p / 100 * float64(len(sorted))))
	if rank < 1 {
		rank = 1
	}
	if rank > len(sorted) {
		rank = len(sorted)
	}
	return sorted[rank-1]
}

// Summaries trả kết quả theo tên thao tác, elapsed là thời gian đo.
func (r *Recorder) Summaries(elapsed time.Duration) []Summary {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, 0, len(r.ops))
	for n := range r.ops {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]Summary, 0, len(names))
	for _, n := range names {
		s := r.ops[n]
		lat := slices.Clone(s.lat)
		slices.Sort(lat)
		total := s.ok + s.errors
		sum := Summary{Op: n, Count: total, OK: s.ok, Errors: s.errors, Dropped: s.dropped, ByStatus: map[string]int64{}}
		for k, v := range s.byStatus {
			sum.ByStatus[k] = v
		}
		if total+s.dropped > 0 {
			// Lượt bị bỏ tính là lỗi: mục tiêu tải không đạt được.
			sum.ErrorRate = float64(s.errors+s.dropped) / float64(total+s.dropped)
		}
		if elapsed > 0 {
			sum.RPS = float64(total) / elapsed.Seconds()
		}
		sum.P50, sum.P95, sum.P99 = Percentile(lat, 50), Percentile(lat, 95), Percentile(lat, 99)
		if len(lat) > 0 {
			sum.Max = lat[len(lat)-1]
		}
		out = append(out, sum)
	}
	return out
}

// PrintTable in bảng kết quả.
func PrintTable(w io.Writer, sums []Summary) {
	outf(w, "%-14s %9s %9s %8s %8s %9s %9s %9s %9s %9s\n", "thao tác", "số lượt", "rps", "lỗi", "bỏ", "tỷ lệ lỗi", "p50", "p95", "p99", "max")
	for _, s := range sums {
		outf(w, "%-14s %9d %9.1f %8d %8d %8.3f%% %9s %9s %9s %9s\n", s.Op, s.Count, s.RPS, s.Errors, s.Dropped, 100*s.ErrorRate,
			round(s.P50), round(s.P95), round(s.P99), round(s.Max))
	}
}

func round(d time.Duration) time.Duration {
	switch {
	case d >= time.Second:
		return d.Round(time.Millisecond)
	case d >= time.Millisecond:
		return d.Round(10 * time.Microsecond)
	default:
		return d.Round(time.Microsecond)
	}
}

// outf / outln ghi ra luồng báo cáo; lỗi ghi (stdout đóng) không làm hỏng lượt đo.
func outf(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }

func outln(w io.Writer, a ...any) { _, _ = fmt.Fprintln(w, a...) }
