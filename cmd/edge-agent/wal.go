package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

// Đệm đĩa: WAL phân đoạn, chỉ ghi thêm, fsync mỗi số đo (mục 14; ADR-0019 §6).
//
// Mỗi đoạn là một tệp <counter đầu, 20 chữ số>.wal gồm các khung
//
//	[độ dài payload: uint32 LE][CRC-32C payload: uint32 LE][payload]
//
// Khung đầu là tiêu đề JSON {"format","device_id","epoch","first_counter"}; các khung
// sau là JSON edgewire.Reading với counter liên tiếp. Đầy (MaxBytes) → Append trả
// ErrBufferFull: KHÔNG BAO GIỜ ghi đè số đo chưa được ingest xác nhận. Chỉ
// TrimThrough (sau khi ingest xác nhận) mới gỡ đoạn — và chỉ gỡ đoạn mà mọi số đo đã
// được xác nhận.
//
// Phục hồi sau sập: khung cuối của đoạn CUỐI bị cắt dở (độ dài/CRC/JSON hỏng) →
// cắt bỏ đuôi (số đo đó chưa được fsync nên chưa từng được coi là đã ghi). Hỏng ở
// giữa hoặc ở đoạn không phải cuối → ErrWALCorrupt, từ chối khởi động.

// WALFormat là tiêu đề đoạn.
const WALFormat = "agrigreen.edge.wal.v1"

const (
	walFrameHeader = 8
	walMaxPayload  = 4096
	walExt         = ".wal"
)

// Lỗi của đệm.
var (
	ErrBufferFull = errors.New("đệm đĩa đầy: ngừng nhận số đo mới (không ghi đè số đo chưa xác nhận)")
	ErrWALCorrupt = errors.New("WAL hỏng")
	ErrWALForeign = errors.New("WAL thuộc thiết bị/kỳ đăng ký khác")
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

type walHeader struct {
	Format       string `json:"format"`
	DeviceID     string `json:"device_id"`
	Epoch        int    `json:"epoch"`
	FirstCounter int64  `json:"first_counter"`
}

type walSegment struct {
	path  string
	first int64
	last  int64 // first−1 khi chưa có số đo
	size  int64
}

// WAL là đệm đĩa của một thiết bị trong một kỳ đăng ký.
type WAL struct {
	mu       sync.Mutex
	dir      string
	deviceID string
	epoch    int
	segBytes int64
	maxBytes int64
	segs     []*walSegment
	total    int64
	cur      *os.File // tệp của đoạn cuối, mở để ghi thêm
	log      *slog.Logger

	// con trỏ đọc (bộ gửi đọc tuần tự): tránh quét lại từ đầu đoạn.
	rdSeg  string
	rdOff  int64
	rdNext int64
}

// OpenWAL mở (hoặc tạo) WAL trong dir và phục hồi sau sập.
func OpenWAL(dir, deviceID string, epoch int, segBytes, maxBytes int64, log *slog.Logger) (*WAL, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	w := &WAL{dir: dir, deviceID: deviceID, epoch: epoch, segBytes: segBytes, maxBytes: maxBytes, log: log}
	names, err := walSegmentNames(dir)
	if err != nil {
		return nil, err
	}
	for i, name := range names {
		first, err := strconv.ParseInt(strings.TrimSuffix(name, walExt), 10, 64)
		if err != nil || first < 1 {
			return nil, fmt.Errorf("%w: tên đoạn %s", ErrWALCorrupt, name)
		}
		seg, err := w.recoverSegment(filepath.Join(dir, name), first, i == len(names)-1)
		if err != nil {
			return nil, err
		}
		if seg == nil { // đoạn cuối chỉ có tiêu đề dở — đã gỡ
			continue
		}
		if n := len(w.segs); n > 0 && seg.first != w.segs[n-1].last+1 {
			return nil, fmt.Errorf("%w: đoạn %s bắt đầu ở %d, cần %d (thiếu đoạn?)", ErrWALCorrupt, name, seg.first, w.segs[n-1].last+1)
		}
		if n := len(w.segs); n > 0 && w.segs[n-1].last < w.segs[n-1].first {
			return nil, fmt.Errorf("%w: đoạn rỗng %s ở giữa WAL", ErrWALCorrupt, w.segs[n-1].path)
		}
		w.segs = append(w.segs, seg)
		w.total += seg.size
	}
	if n := len(w.segs); n > 0 {
		f, err := os.OpenFile(w.segs[n-1].path, os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return nil, err
		}
		w.cur = f
	}
	return w, nil
}

// HasSegments cho biết dir có đoạn WAL nào không (dùng khi trạng thái bị mất).
func HasSegments(dir string) (bool, error) {
	names, err := walSegmentNames(dir)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return len(names) > 0, err
}

func walSegmentNames(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasSuffix(e.Name(), walExt) {
			names = append(names, e.Name())
		}
	}
	slices.Sort(names) // tên 20 chữ số → thứ tự từ điển = thứ tự bộ đếm
	return names, nil
}

// readFrame đọc một khung; io.EOF sạch khi hết tệp đúng ranh giới khung.
func readWALFrame(r *bufio.Reader) ([]byte, error) {
	var h [walFrameHeader]byte
	n, err := io.ReadFull(r, h[:])
	if n == 0 && errors.Is(err, io.EOF) {
		return nil, io.EOF
	}
	if err != nil {
		return nil, io.ErrUnexpectedEOF
	}
	ln := binary.LittleEndian.Uint32(h[0:])
	if ln == 0 || ln > walMaxPayload {
		return nil, errors.New("độ dài khung không hợp lệ")
	}
	p := make([]byte, ln)
	if _, err := io.ReadFull(r, p); err != nil {
		return nil, io.ErrUnexpectedEOF
	}
	if crc32.Checksum(p, crcTable) != binary.LittleEndian.Uint32(h[4:]) {
		return nil, errors.New("CRC khung sai")
	}
	return p, nil
}

func encodeWALFrame(payload []byte) []byte {
	f := make([]byte, walFrameHeader+len(payload))
	binary.LittleEndian.PutUint32(f[0:], uint32(len(payload))) //nolint:gosec // ≤ walMaxPayload
	binary.LittleEndian.PutUint32(f[4:], crc32.Checksum(payload, crcTable))
	copy(f[walFrameHeader:], payload)
	return f
}

// recoverSegment kiểm một đoạn; với đoạn cuối, cắt đuôi dở. Trả nil nếu đoạn cuối
// chỉ có tiêu đề dở (tạo đoạn bị sập giữa chừng — không chứa số đo nào) và đã gỡ.
func (w *WAL) recoverSegment(path string, first int64, isLast bool) (*walSegment, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600) //nolint:gosec // tệp trong data_dir của agent
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	r := bufio.NewReader(f)
	hp, err := readWALFrame(r)
	if err != nil {
		if isLast {
			w.log.Warn("WAL: đoạn cuối có tiêu đề dở — gỡ (không chứa số đo)", "segment", path)
			_ = f.Close()
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			return nil, syncDir(w.dir)
		}
		return nil, fmt.Errorf("%w: tiêu đề %s: %w", ErrWALCorrupt, path, err)
	}
	var h walHeader
	if err := json.Unmarshal(hp, &h); err != nil || h.Format != WALFormat {
		return nil, fmt.Errorf("%w: tiêu đề %s không hợp lệ", ErrWALCorrupt, path)
	}
	if h.DeviceID != w.deviceID || h.Epoch != w.epoch {
		return nil, fmt.Errorf("%w: %s thuộc %s kỳ %d, agent là %s kỳ %d", ErrWALForeign, path, h.DeviceID, h.Epoch, w.deviceID, w.epoch)
	}
	if h.FirstCounter != first {
		return nil, fmt.Errorf("%w: %s tiêu đề first_counter %d khác tên tệp", ErrWALCorrupt, path, h.FirstCounter)
	}
	seg := &walSegment{path: path, first: first, last: first - 1, size: int64(walFrameHeader + len(hp))}
	for {
		p, err := readWALFrame(r)
		if errors.Is(err, io.EOF) {
			break
		}
		var rd edgewire.Reading
		if err == nil {
			if jerr := json.Unmarshal(p, &rd); jerr != nil {
				err = fmt.Errorf("JSON số đo: %w", jerr)
			} else if rd.Counter != seg.last+1 {
				return nil, fmt.Errorf("%w: %s counter %d, cần %d", ErrWALCorrupt, path, rd.Counter, seg.last+1)
			}
		}
		if err != nil {
			if !isLast {
				return nil, fmt.Errorf("%w: %s tại byte %d: %w", ErrWALCorrupt, path, seg.size, err)
			}
			w.log.Warn("WAL: cắt đuôi dở sau sập", "segment", path, "offset", seg.size, "reason", err.Error())
			if err := f.Truncate(seg.size); err != nil {
				return nil, err
			}
			if err := f.Sync(); err != nil {
				return nil, err
			}
			break
		}
		seg.last = rd.Counter
		seg.size += int64(walFrameHeader + len(p))
	}
	return seg, nil
}

// Append ghi thêm một số đo (counter phải = Last()+1) và fsync. Đầy → ErrBufferFull.
func (w *WAL) Append(r edgewire.Reading) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if n := len(w.segs); n > 0 && r.Counter != w.segs[n-1].last+1 {
		return fmt.Errorf("WAL: counter %d không liên tiếp sau %d", r.Counter, w.segs[n-1].last)
	}
	p, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(p) > walMaxPayload {
		return errors.New("WAL: số đo quá lớn")
	}
	frame := encodeWALFrame(p)
	need := int64(len(frame))
	n := len(w.segs)
	newSeg := n == 0 || (w.segs[n-1].size+need > w.segBytes && w.segs[n-1].last >= w.segs[n-1].first)
	var hdr []byte
	if newSeg {
		hp, _ := json.Marshal(walHeader{Format: WALFormat, DeviceID: w.deviceID, Epoch: w.epoch, FirstCounter: r.Counter})
		hdr = encodeWALFrame(hp)
		need += int64(len(hdr))
	}
	if w.total+need > w.maxBytes {
		return ErrBufferFull
	}
	if newSeg {
		if err := w.createSegment(r.Counter, hdr); err != nil {
			return err
		}
	}
	seg := w.segs[len(w.segs)-1]
	if _, err := w.cur.Write(frame); err != nil {
		_ = w.cur.Truncate(seg.size) // bỏ khung dở
		return fmt.Errorf("WAL: ghi: %w", err)
	}
	if err := w.cur.Sync(); err != nil {
		return fmt.Errorf("WAL: fsync: %w", err)
	}
	seg.last = r.Counter
	seg.size += int64(len(frame))
	w.total += int64(len(frame))
	return nil
}

func (w *WAL) createSegment(first int64, hdr []byte) error {
	if w.cur != nil {
		if err := w.cur.Close(); err != nil {
			return err
		}
		w.cur = nil
	}
	path := filepath.Join(w.dir, fmt.Sprintf("%020d%s", first, walExt))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|os.O_APPEND, 0o600) //nolint:gosec // tệp trong data_dir
	if err != nil {
		return fmt.Errorf("WAL: tạo đoạn: %w", err)
	}
	if _, err := f.Write(hdr); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := syncDir(w.dir); err != nil {
		_ = f.Close()
		return err
	}
	w.cur = f
	w.segs = append(w.segs, &walSegment{path: path, first: first, last: first - 1, size: int64(len(hdr))})
	w.total += int64(len(hdr))
	return nil
}

func (w *WAL) lastLocked() int64 {
	if n := len(w.segs); n > 0 {
		return w.segs[n-1].last
	}
	return 0
}

// Last là counter lớn nhất trong WAL (0 nếu rỗng).
func (w *WAL) Last() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastLocked()
}

// First là counter nhỏ nhất còn trong WAL (0 nếu rỗng).
func (w *WAL) First() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, s := range w.segs {
		if s.last >= s.first {
			return s.first
		}
	}
	return 0
}

// Bytes / MaxBytes: dung lượng đang dùng / tối đa.
func (w *WAL) Bytes() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.total
}

// MaxBytes là dung lượng tối đa.
func (w *WAL) MaxBytes() int64 { return w.maxBytes }

// FillRatio là độ đầy đệm 0..1.
func (w *WAL) FillRatio() float64 { return float64(w.Bytes()) / float64(w.maxBytes) }

// ReadFrom đọc tối đa max số đo liên tiếp bắt đầu từ counter from.
func (w *WAL) ReadFrom(from int64, maxN int) ([]edgewire.Reading, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	var seg *walSegment
	for _, s := range w.segs {
		if s.first <= from && from <= s.last {
			seg = s
			break
		}
	}
	if seg == nil {
		return nil, nil
	}
	var out []edgewire.Reading
	for seg != nil && len(out) < maxN {
		rs, err := w.readSegment(seg, from, maxN-len(out))
		if err != nil {
			return nil, err
		}
		out = append(out, rs...)
		from += int64(len(rs))
		seg = nil
		for _, s := range w.segs {
			if s.first == from && s.last >= s.first {
				seg = s
			}
		}
	}
	return out, nil
}

func (w *WAL) readSegment(seg *walSegment, from int64, maxN int) ([]edgewire.Reading, error) {
	f, err := os.Open(seg.path) //nolint:gosec // tệp trong data_dir
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	off, next := int64(0), int64(0)
	if w.rdSeg == seg.path && w.rdNext <= from && w.rdNext >= seg.first {
		off, next = w.rdOff, w.rdNext
	}
	if _, err := f.Seek(off, io.SeekStart); err != nil {
		return nil, err
	}
	r := bufio.NewReader(io.LimitReader(f, seg.size-off))
	if off == 0 {
		hp, err := readWALFrame(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrWALCorrupt, seg.path, err)
		}
		off, next = int64(walFrameHeader+len(hp)), seg.first
	}
	var out []edgewire.Reading
	for next <= seg.last && len(out) < maxN {
		p, err := readWALFrame(r)
		if err != nil {
			return nil, fmt.Errorf("%w: %s counter %d: %w", ErrWALCorrupt, seg.path, next, err)
		}
		off += int64(walFrameHeader + len(p))
		if next >= from {
			var rd edgewire.Reading
			if err := json.Unmarshal(p, &rd); err != nil || rd.Counter != next {
				return nil, fmt.Errorf("%w: %s counter %d", ErrWALCorrupt, seg.path, next)
			}
			out = append(out, rd)
		}
		next++
	}
	w.rdSeg, w.rdOff, w.rdNext = seg.path, off, next
	return out, nil
}

// TrimThrough gỡ mọi đoạn mà toàn bộ số đo có counter ≤ acked (ingest đã xác nhận).
func (w *WAL) TrimThrough(acked int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	removed := false
	for len(w.segs) > 0 {
		s := w.segs[0]
		if s.last > acked || s.last < s.first {
			break
		}
		if len(w.segs) == 1 && w.cur != nil {
			if err := w.cur.Close(); err != nil {
				return err
			}
			w.cur = nil
		}
		if err := os.Remove(s.path); err != nil {
			return fmt.Errorf("WAL: gỡ đoạn đã xác nhận: %w", err)
		}
		w.total -= s.size
		w.segs = w.segs[1:]
		removed = true
		if w.rdSeg == s.path {
			w.rdSeg = ""
		}
	}
	if removed {
		return syncDir(w.dir)
	}
	return nil
}

// Close đóng tệp đang ghi.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cur != nil {
		err := w.cur.Close()
		w.cur = nil
		return err
	}
	return nil
}

// syncDir fsync thư mục để việc tạo/đổi tên/gỡ tệp bền vững (Linux). Windows không
// hỗ trợ fsync thư mục — bỏ qua (chỉ để chạy kiểm thử trên máy phát triển).
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir) //nolint:gosec // thư mục của agent
	if err != nil {
		return err
	}
	defer func() { _ = d.Close() }()
	return d.Sync()
}
