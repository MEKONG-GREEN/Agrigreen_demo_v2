package main

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
)

// Trạng thái bền vững của agent trong data_dir:
//
//	state.json    kỳ đăng ký, key_id, bộ đếm đã cấp cao nhất đã ghi, bộ đếm đã được ingest xác nhận
//	wal/          đệm đĩa (WAL phân đoạn)
//	pending.json  lô đã ký đang chờ xác nhận (gửi lại byte-cho-byte sau khi khởi động lại)
//	source.key    khóa ký nguồn Ed25519 (0600)
//
// Bộ đếm đơn điệu: counter của số đo được fsync CÙNG số đo trong WAL, nên bộ đếm bền
// vững = max(state.last_counter, state.acked_counter, WAL.Last()). state.json được ghi
// nguyên tử (tệp tạm + fsync + rename + fsync thư mục) mỗi lần xác nhận và khi dừng.
// Mọi dấu hiệu mất đĩa (state mất nhưng còn WAL, WAL thiếu số đo so với state, khóa
// mất) → từ chối khởi động: phải đăng ký KỲ MỚI (ADR-0019 §5), không bao giờ dùng
// lại bộ đếm.

// StateFormat là giá trị trường format của state.json.
const StateFormat = "agrigreen.edge.state.v1"

// Lỗi khởi động do mất trạng thái.
var (
	ErrNotEnrolled = errors.New("thiết bị chưa đăng ký: chạy `edge-agent enroll`")
	ErrStateLost   = errors.New("mất state.json nhưng đệm còn số đo — mất đĩa một phần: cần đăng ký kỳ mới (`edge-agent enroll -epoch N+1 -abandon-buffer`)")
	ErrKeyLost     = errors.New("mất khóa ký nguồn — cần đăng ký kỳ mới (`edge-agent enroll -epoch N+1 -rotate`)")
	ErrCounterGap  = errors.New("đệm thiếu số đo so với state.json (mất dữ liệu đĩa) — cần đăng ký kỳ mới")
)

// State là nội dung state.json.
type State struct {
	Format       string `json:"format"`
	DeviceID     string `json:"device_id"`
	Epoch        int    `json:"epoch"`
	KeyID        string `json:"key_id"`
	LastCounter  int64  `json:"last_counter"`
	AckedCounter int64  `json:"acked_counter"`
	EnrolledAt   string `json:"enrolled_at"`
}

func statePath(dataDir string) string   { return filepath.Join(dataDir, "state.json") }
func walDir(dataDir string) string      { return filepath.Join(dataDir, "wal") }
func pendingPath(dataDir string) string { return filepath.Join(dataDir, "pending.json") }

// ReadState đọc state.json; os.ErrNotExist nếu chưa có.
func ReadState(dataDir string) (State, error) {
	var s State
	b, err := readFile(statePath(dataDir))
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("state.json hỏng: %w", err)
	}
	if s.Format != StateFormat {
		return s, fmt.Errorf("state.json hỏng: format %q, cần %q", s.Format, StateFormat)
	}
	return s, nil
}

// WriteState ghi state.json nguyên tử.
func WriteState(dataDir string, s State) error {
	s.Format = StateFormat
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(statePath(dataDir), append(b, '\n'), 0o600)
}

// writeFileAtomic ghi tệp tạm, fsync, đổi tên, fsync thư mục.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Chmod(perm); err != nil && !isWindows() {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	return syncDir(dir)
}

// Store gói state + WAL + lô chờ, bảo vệ bất biến bộ đếm.
type Store struct {
	mu      sync.Mutex
	dataDir string
	st      State
	wal     *WAL
	next    int64 // counter sẽ cấp cho số đo kế tiếp
	log     *slog.Logger
}

// OpenStore mở trạng thái đã đăng ký và kiểm mọi dấu hiệu mất đĩa.
func OpenStore(cfg *Config, pub ed25519.PublicKey, log *slog.Logger) (*Store, error) {
	st, err := ReadState(cfg.DataDir)
	if errors.Is(err, os.ErrNotExist) {
		has, herr := HasSegments(walDir(cfg.DataDir))
		if herr != nil {
			return nil, herr
		}
		_, perr := os.Stat(pendingPath(cfg.DataDir))
		if has || perr == nil {
			return nil, ErrStateLost
		}
		return nil, ErrNotEnrolled
	}
	if err != nil {
		return nil, err
	}
	if st.DeviceID != cfg.DeviceID {
		return nil, fmt.Errorf("state.json thuộc thiết bị %s, cấu hình là %s", st.DeviceID, cfg.DeviceID)
	}
	if pub == nil {
		return nil, ErrKeyLost
	}
	if kid := edgewire.KeyID(pub); kid != st.KeyID {
		return nil, fmt.Errorf("khóa ký nguồn (key_id %s) không khớp kỳ đăng ký %d (key_id %s) — %w", kid, st.Epoch, st.KeyID, ErrKeyLost)
	}
	if st.AckedCounter > st.LastCounter || st.AckedCounter < 0 {
		return nil, fmt.Errorf("state.json mâu thuẫn: acked %d > last %d", st.AckedCounter, st.LastCounter)
	}
	w, err := OpenWAL(walDir(cfg.DataDir), cfg.DeviceID, st.Epoch, cfg.Buffer.SegmentBytes, cfg.Buffer.MaxBytes, log)
	if err != nil {
		return nil, err
	}
	first, last := w.First(), w.Last()
	switch {
	case first > 0 && first > st.AckedCounter+1:
		_ = w.Close()
		return nil, fmt.Errorf("%w: WAL bắt đầu ở %d nhưng mới xác nhận tới %d", ErrCounterGap, first, st.AckedCounter)
	case last < st.LastCounter && st.LastCounter > st.AckedCounter:
		_ = w.Close()
		return nil, fmt.Errorf("%w: state ghi đã cấp tới %d, WAL chỉ còn tới %d (xác nhận tới %d)", ErrCounterGap, st.LastCounter, last, st.AckedCounter)
	}
	s := &Store{dataDir: cfg.DataDir, st: st, wal: w, log: log}
	s.next = max(st.LastCounter, st.AckedCounter, last) + 1
	// Sập giữa lúc ghi state (xác nhận) và gỡ đoạn: gỡ nốt.
	if err := w.TrimThrough(st.AckedCounter); err != nil {
		return nil, err
	}
	return s, nil
}

// State trả bản sao trạng thái.
func (s *Store) State() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st
}

// WAL trả đệm.
func (s *Store) WAL() *WAL { return s.wal }

// Epoch trả kỳ đăng ký.
func (s *Store) Epoch() int { return s.st.Epoch }

// Append cấp counter kế tiếp cho số đo và ghi bền vững. Đệm đầy → ErrBufferFull và
// counter KHÔNG bị tiêu (không tạo khoảng trống).
func (s *Store) Append(r edgewire.Reading) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.Counter = s.next
	if err := s.wal.Append(r); err != nil {
		return 0, err
	}
	s.next++
	return r.Counter, nil
}

// LastCounter là counter đã cấp cao nhất.
func (s *Store) LastCounter() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next - 1
}

// Acked là counter cao nhất ingest đã xác nhận.
func (s *Store) Acked() int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.st.AckedCounter
}

// Ack ghi mức xác nhận mới (fsync) rồi gỡ các đoạn WAL đã xác nhận hết.
func (s *Store) Ack(high int64) error {
	s.mu.Lock()
	if high <= s.st.AckedCounter {
		s.mu.Unlock()
		return nil
	}
	if high > s.next-1 {
		s.mu.Unlock()
		return fmt.Errorf("xác nhận %d vượt counter đã cấp %d", high, s.next-1)
	}
	st := s.st
	st.AckedCounter = high
	st.LastCounter = s.next - 1
	if err := WriteState(s.dataDir, st); err != nil {
		s.mu.Unlock()
		return err
	}
	s.st = st
	s.mu.Unlock()
	return s.wal.TrimThrough(high)
}

// Checkpoint ghi last_counter hiện tại vào state.json (khi dừng, định kỳ).
func (s *Store) Checkpoint() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.st
	st.LastCounter = s.next - 1
	if st == s.st {
		return nil
	}
	if err := WriteState(s.dataDir, st); err != nil {
		return err
	}
	s.st = st
	return nil
}

// Close ghi checkpoint và đóng WAL.
func (s *Store) Close() error {
	err := s.Checkpoint()
	return errors.Join(err, s.wal.Close())
}

// SavePending lưu lô đã ký trước lần gửi đầu (fsync), để sau sập gửi lại đúng các byte đó.
func (s *Store) SavePending(body []byte) error {
	return writeFileAtomic(pendingPath(s.dataDir), body, 0o600)
}

// LoadPending đọc lô chờ nếu còn hợp lệ với mức xác nhận hiện tại; lô đã xác nhận
// hoặc lạc (không bắt đầu ngay sau mức xác nhận) bị bỏ.
func (s *Store) LoadPending() (*edgewire.Batch, []byte, error) {
	b, err := readFile(pendingPath(s.dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	bt, derr := edgewire.Decode(b)
	if derr == nil {
		derr = bt.Validate()
	}
	acked := s.Acked()
	if derr != nil || bt.DeviceID != s.st.DeviceID || bt.Epoch != s.st.Epoch || bt.CounterFrom != acked+1 || bt.CounterTo > s.LastCounter() {
		s.log.Warn("bỏ lô chờ không còn khớp", "reason", fmt.Sprint(derr), "acked", acked)
		return nil, nil, s.ClearPending()
	}
	return &bt, b, nil
}

// ClearPending gỡ lô chờ sau khi đã được xác nhận.
func (s *Store) ClearPending() error {
	err := os.Remove(pendingPath(s.dataDir))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDir(s.dataDir)
}
