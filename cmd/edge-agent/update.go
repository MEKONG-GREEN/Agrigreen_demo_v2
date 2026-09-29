package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Tự cập nhật có chữ ký với hai khe A/B và lùi lại (mục 14). Bố cục dưới update.root:
//
//	slots/a/edge-agent, slots/b/edge-agent   hai khe nhị phân
//	slots.json                                khe đang chạy (active), khe ổn định (stable), trạng thái thử
//	active -> slots/X   stable -> slots/Y     liên kết mềm (Linux) cho unit systemd
//
// Quy trình: `update apply` xác minh manifest (chữ ký Ed25519 của khóa PHÁT HÀNH,
// sha256, kích thước, os/arch, phiên bản mới hơn) → chép vào khe không chạy →
// active = khe mới, trial = true. Unit systemd chạy `stable/edge-agent update guard`
// (ExecStartPre, luôn là bản đã xác nhận) trước mỗi lần khởi động: đang thử thì tăng
// attempts; vượt max_attempts → lùi active về stable. Bản mới chạy khỏe liên tục
// healthy_after → tự xác nhận (stable = active). Xem docs/runbooks/edge-agent.md.

// UpdateFormat là tách miền của manifest.
const UpdateFormat = "agrigreen.edge.update.v1"

// Manifest là bản kê phát hành đã ký.
type Manifest struct {
	Format    string `json:"format"`
	Version   string `json:"version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	SHA256    string `json:"sha256"`
	Size      int64  `json:"size"`
	Signature string `json:"signature"`
}

var (
	versionRe = regexp.MustCompile(`^(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})\.(0|[1-9][0-9]{0,5})$`)
	sha256Re  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Canonical là dạng được ký của manifest.
func (m Manifest) Canonical() ([]byte, error) {
	if m.Format != UpdateFormat {
		return nil, fmt.Errorf("manifest.format phải là %s", UpdateFormat)
	}
	if !versionRe.MatchString(m.Version) {
		return nil, errors.New("manifest.version phải dạng MAJOR.MINOR.PATCH")
	}
	if !sha256Re.MatchString(m.SHA256) || m.Size <= 0 || m.OS == "" || m.Arch == "" {
		return nil, errors.New("manifest thiếu sha256/size/os/arch hợp lệ")
	}
	return fmt.Appendf(nil, "%s\nversion=%s\nos=%s\narch=%s\nsha256=%s\nsize=%d\n", UpdateFormat, m.Version, m.OS, m.Arch, m.SHA256, m.Size), nil
}

// SignManifest ký manifest bằng khóa phát hành.
func SignManifest(priv ed25519.PrivateKey, m *Manifest) error {
	m.Format = UpdateFormat
	c, err := m.Canonical()
	if err != nil {
		return err
	}
	m.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, c))
	return nil
}

// VerifyManifest kiểm chữ ký manifest.
func VerifyManifest(pub ed25519.PublicKey, m Manifest) error {
	c, err := m.Canonical()
	if err != nil {
		return err
	}
	sig, err := base64.StdEncoding.Strict().DecodeString(m.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("manifest.signature không hợp lệ")
	}
	if !ed25519.Verify(pub, c, sig) {
		return errors.New("chữ ký manifest không hợp lệ (khóa phát hành)")
	}
	return nil
}

// ParseReleaseKey đọc khóa công khai phát hành hex.
func ParseReleaseKey(h string) (ed25519.PublicKey, error) {
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("update.release_public_key phải là 64 chữ số hex (Ed25519)")
	}
	return ed25519.PublicKey(b), nil
}

// CompareVersions so a, b dạng MAJOR.MINOR.PATCH (-1, 0, 1). Phiên bản không hợp lệ coi như 0.0.0.
func CompareVersions(a, b string) int {
	pa, pb := parseVer(a), parseVer(b)
	for i := range 3 {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseVer(v string) [3]int {
	var out [3]int
	m := versionRe.FindStringSubmatch(v)
	if m == nil {
		return out
	}
	for i := range 3 {
		out[i], _ = strconv.Atoi(m[i+1])
	}
	return out
}

// Slots là nội dung slots.json.
type Slots struct {
	Active   string            `json:"active"`
	Stable   string            `json:"stable"`
	Trial    bool              `json:"trial"`
	Attempts int               `json:"attempts"`
	Versions map[string]string `json:"versions"`
	History  []string          `json:"history"`
}

const binaryName = "edge-agent"

func slotsPath(root string) string        { return filepath.Join(root, "slots.json") }
func slotDir(root, slot string) string    { return filepath.Join(root, "slots", slot) }
func slotBinary(root, slot string) string { return filepath.Join(slotDir(root, slot), binaryName) }
func otherSlot(s string) string {
	if s == "a" {
		return "b"
	}
	return "a"
}

// ReadSlots đọc slots.json; chưa có → khe a là bản đầu (đã xác nhận).
func ReadSlots(root string) (Slots, error) {
	b, err := readFile(slotsPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return Slots{Active: "a", Stable: "a", Versions: map[string]string{}}, nil
	}
	if err != nil {
		return Slots{}, err
	}
	var s Slots
	if err := json.Unmarshal(b, &s); err != nil || (s.Active != "a" && s.Active != "b") || (s.Stable != "a" && s.Stable != "b") {
		return Slots{}, errors.New("slots.json hỏng")
	}
	if s.Versions == nil {
		s.Versions = map[string]string{}
	}
	return s, nil
}

// writeSlots ghi slots.json nguyên tử và cập nhật liên kết active/stable.
func writeSlots(root string, s Slots, now time.Time, event string) error {
	if event != "" {
		s.History = append(s.History, now.UTC().Format(time.RFC3339)+" "+event)
		if len(s.History) > 50 {
			s.History = s.History[len(s.History)-50:]
		}
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := writeFileAtomic(slotsPath(root), append(b, '\n'), 0o644); err != nil {
		return err
	}
	return errors.Join(relink(root, "active", s.Active), relink(root, "stable", s.Stable))
}

// relink đổi liên kết mềm root/name → slots/<slot> nguyên tử (Linux). Windows: bỏ qua.
func relink(root, name, slot string) error {
	if isWindows() {
		return nil
	}
	tmp := filepath.Join(root, "."+name+".tmp")
	_ = os.Remove(tmp)
	if err := os.Symlink(filepath.Join("slots", slot), tmp); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(root, name))
}

// ApplyUpdate xác minh manifest + nhị phân rồi cài vào khe không chạy, bật chế độ thử.
func ApplyUpdate(root string, pub ed25519.PublicKey, m Manifest, bin io.Reader, allowDowngrade bool, now time.Time) (Slots, error) {
	if err := VerifyManifest(pub, m); err != nil {
		return Slots{}, err
	}
	if m.OS != runtime.GOOS || m.Arch != runtime.GOARCH {
		return Slots{}, fmt.Errorf("manifest cho %s/%s, thiết bị là %s/%s", m.OS, m.Arch, runtime.GOOS, runtime.GOARCH)
	}
	s, err := ReadSlots(root)
	if err != nil {
		return Slots{}, err
	}
	if s.Trial {
		return Slots{}, errors.New("đang thử một bản cập nhật chưa xác nhận — chờ xác nhận hoặc `update rollback`")
	}
	if cur := s.Versions[s.Active]; cur != "" && CompareVersions(m.Version, cur) <= 0 && !allowDowngrade {
		return Slots{}, fmt.Errorf("phiên bản %s không mới hơn bản đang chạy %s (chống hạ cấp; -allow-downgrade nếu chủ ý)", m.Version, cur)
	}
	target := otherSlot(s.Active)
	if err := os.MkdirAll(slotDir(root, target), 0o755); err != nil { //nolint:gosec // thư mục nhị phân cần đọc/thực thi
		return Slots{}, err
	}
	f, err := os.CreateTemp(slotDir(root, target), ".edge-agent.tmp*")
	if err != nil {
		return Slots{}, err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(bin, m.Size+1))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Slots{}, err
	}
	if n != m.Size {
		return Slots{}, fmt.Errorf("kích thước nhị phân %d khác manifest %d", n, m.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != m.SHA256 {
		return Slots{}, fmt.Errorf("sha256 nhị phân %s khác manifest %s", got, m.SHA256)
	}
	if err := os.Chmod(tmp, 0o755); err != nil && !isWindows() { //nolint:gosec // nhị phân thực thi
		return Slots{}, err
	}
	if err := os.Rename(tmp, slotBinary(root, target)); err != nil {
		return Slots{}, err
	}
	if err := syncDir(slotDir(root, target)); err != nil {
		return Slots{}, err
	}
	s.Versions[target] = m.Version
	s.Active, s.Trial, s.Attempts = target, true, 0
	return s, writeSlots(root, s, now, "apply "+m.Version+" → khe "+target+" (thử)")
}

// GuardStart chạy trước mỗi lần khởi động (từ bản ổn định): đang thử thì đếm lần
// thử; vượt maxAttempts → lùi về khe ổn định. Trả true nếu vừa lùi lại.
func GuardStart(root string, maxAttempts int, now time.Time) (bool, Slots, error) {
	s, err := ReadSlots(root)
	if err != nil || !s.Trial {
		return false, s, err
	}
	if s.Attempts >= maxAttempts {
		failed := s.Versions[s.Active]
		s.Active, s.Trial, s.Attempts = s.Stable, false, 0
		return true, s, writeSlots(root, s, now, fmt.Sprintf("rollback: %s không khỏe sau %d lần thử → khe %s (%s)", failed, maxAttempts, s.Stable, s.Versions[s.Stable]))
	}
	s.Attempts++
	return false, s, writeSlots(root, s, now, fmt.Sprintf("thử khởi động %d/%d bản %s", s.Attempts, maxAttempts, s.Versions[s.Active]))
}

// CommitUpdate xác nhận bản đang thử (gọi khi bản mới đã khỏe healthy_after).
func CommitUpdate(root string, now time.Time) (Slots, error) {
	s, err := ReadSlots(root)
	if err != nil || !s.Trial {
		return s, err
	}
	s.Stable, s.Trial, s.Attempts = s.Active, false, 0
	return s, writeSlots(root, s, now, "commit "+s.Versions[s.Active]+" (khe "+s.Active+")")
}

// RollbackUpdate lùi bằng tay: đang thử → về stable; không thử → về khe kia nếu có bản.
func RollbackUpdate(root string, now time.Time) (Slots, error) {
	s, err := ReadSlots(root)
	if err != nil {
		return s, err
	}
	target := s.Stable
	if !s.Trial {
		target = otherSlot(s.Active)
		if s.Versions[target] == "" {
			return s, errors.New("khe còn lại chưa có bản nào để lùi về")
		}
	}
	s.Active, s.Stable, s.Trial, s.Attempts = target, target, false, 0
	return s, writeSlots(root, s, now, "rollback tay → khe "+target+" ("+s.Versions[target]+")")
}

// runningSlot đoán khe của tiến trình hiện tại từ đường dẫn nhị phân ("" nếu không nằm trong root/slots).
func runningSlot(root string) string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	for _, sl := range []string{"a", "b"} {
		d := slotDir(root, sl)
		if r, err := filepath.EvalSymlinks(d); err == nil {
			d = r
		}
		if strings.HasPrefix(exe, d+string(filepath.Separator)) {
			return sl
		}
	}
	return ""
}
