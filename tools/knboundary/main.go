// Lệnh knboundary là cổng ranh giới hệ thống Khuyến nông (gói KNSPLIT-0, ADR-0055, yêu cầu QA "fitness test"):
// quét mã Go, migration, OpenAPI và web-ops, liệt kê mọi ghép nối giữa KN và phần còn lại theo quy tắc ĐÍCH ở
// tools/knboundary/boundary.yaml, rồi so với danh sách nền tools/knboundary/baseline.yaml.
//
//   - Ghép nối MỚI (không có trong baseline) → hỏng.
//   - Mục baseline LỖI THỜI (ghép nối đã gỡ) → hỏng: xóa mục trong cùng PR (chạy -prune).
//   - -base <ref>: baseline không được có mục nào vắng ở baseline của <ref> (CI so với nhánh đích) — danh sách chỉ co lại.
//
// Chạy:
//
//	go run ./tools/knboundary                 # kiểm (make gates, CI)
//	go run ./tools/knboundary -base origin/dev
//	go run ./tools/knboundary -list           # in mọi ghép nối hiện có theo dạng baseline (kiểm kê)
//	go run ./tools/knboundary -prune          # ghi lại baseline, chỉ BỎ mục lỗi thời (không bao giờ thêm)
package main

import (
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

func main() {
	root := flag.String("root", ".", "gốc repo")
	cfgFile := flag.String("config", "tools/knboundary/boundary.yaml", "cấu hình, tương đối gốc repo")
	baseFile := flag.String("baseline", "tools/knboundary/baseline.yaml", "baseline, tương đối gốc repo")
	base := flag.String("base", "", "git ref: baseline không được lớn hơn baseline ở ref này")
	list := flag.Bool("list", false, "in mọi ghép nối hiện có (dạng baseline) rồi thoát 0")
	prune := flag.Bool("prune", false, "bỏ mục baseline lỗi thời rồi ghi lại tệp")
	flag.Parse()
	os.Exit(run(os.Stdout, *root, *cfgFile, *baseFile, *base, *list, *prune))
}

func run(w io.Writer, root, cfgFile, baseFile, base string, list, prune bool) int {
	cfg, err := LoadConfig(filepath.Join(root, cfgFile))
	if err != nil {
		say(w, "%v\n", err)
		return 2
	}
	cur, err := Scan(root, cfg)
	if err != nil {
		say(w, "%v\n", err)
		return 2
	}
	if list {
		_, _ = w.Write(FormatBaseline(cur))
		return 0
	}
	bl, err := LoadBaseline(filepath.Join(root, baseFile))
	if err != nil {
		say(w, "%v\n", err)
		return 2
	}
	if prune {
		kept := Prune(bl, cur)
		if err := os.WriteFile(filepath.Join(root, baseFile), FormatBaseline(kept), 0o644); err != nil {
			say(w, "%v\n", err)
			return 2
		}
		say(w, "knboundary: baseline %d → %d mục\n", len(bl), len(kept))
		bl = kept
	}
	problems := Compare(bl, cur)
	if base != "" {
		old, err := baselineAt(root, base, baseFile)
		if err != nil {
			say(w, "%v\n", err)
			return 2
		}
		if old != nil {
			problems = append(problems, Growth(old, bl)...)
		}
		oldConfig, err := configAt(root, base, cfgFile)
		if err != nil {
			say(w, "%v\n", err)
			return 2
		}
		if oldConfig != nil && !maps.Equal(oldConfig.CanonicalPaths, cfg.CanonicalPaths) {
			problems = append(problems, "canonical_paths đã đổi so với nhánh đích — không được thêm/sửa alias để che tăng baseline")
		}
	}
	for _, p := range problems {
		say(w, "%v\n", p)
	}
	if len(problems) > 0 {
		say(w, "knboundary: %d lỗi — quy tắc ở %s, quy trình ở docs/kn/tach-he-thong/kiem-ke.md\n", len(problems), cfgFile)
		return 1
	}
	say(w, "knboundary: %d ghép nối, đúng baseline\n", len(cur))
	return 0
}

// LoadBaseline đọc baseline.yaml (thiếu tệp → rỗng).
func LoadBaseline(file string) ([]Coupling, error) {
	b, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return parseBaseline(b, file)
}

func parseBaseline(b []byte, name string) ([]Coupling, error) {
	var doc struct {
		Couplings []Coupling `yaml:"couplings"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	seen := map[string]bool{}
	for _, c := range doc.Couplings {
		if c.Rule == "" || c.From == "" || c.To == "" {
			return nil, fmt.Errorf("%s: mục thiếu rule/from/to: %+v", name, c)
		}
		if seen[c.key()] {
			return nil, fmt.Errorf("%s: mục trùng: %s", name, c)
		}
		seen[c.key()] = true
	}
	return doc.Couplings, nil
}

// readAtRef chỉ coi tệp chưa có là lần khởi tạo. Ref sai/không fetch được phải lỗi,
// không được âm thầm bỏ cổng chỉ giảm baseline.
func readAtRef(root, ref, file string) ([]byte, error) {
	cmd := exec.Command("git", "ls-tree", "--name-only", ref, "--", filepath.ToSlash(file))
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("không đọc được nhánh đích %s: %w", ref, err)
	}
	if strings.TrimSpace(string(out)) == "" {
		return nil, nil
	}
	cmd = exec.Command("git", "show", ref+":"+filepath.ToSlash(file))
	cmd.Dir = root
	b, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("không đọc được %s:%s: %w", ref, file, err)
	}
	return b, nil
}

func baselineAt(root, ref, file string) ([]Coupling, error) {
	b, err := readAtRef(root, ref, file)
	if err != nil || b == nil {
		return nil, err
	}
	return parseBaseline(b, ref+":"+file)
}

func configAt(root, ref, file string) (*Config, error) {
	b, err := readAtRef(root, ref, file)
	if err != nil || b == nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%s:%s: %w", ref, file, err)
	}
	return &cfg, nil
}

// Compare: ghép nối mới (không có trong baseline) và mục baseline lỗi thời.
func Compare(baseline, cur []Coupling) []string {
	inBase := map[string]bool{}
	for _, c := range baseline {
		inBase[c.key()] = true
	}
	inCur := map[string]bool{}
	var out []string
	for _, c := range cur {
		inCur[c.key()] = true
		if !inBase[c.key()] {
			out = append(out, "MỚI    "+c.String())
		}
	}
	for _, c := range baseline {
		if !inCur[c.key()] {
			out = append(out, "LỖI THỜI "+c.String()+" (đã gỡ — xóa khỏi baseline: go run ./tools/knboundary -prune)")
		}
	}
	return out
}

// Growth: mục có trong baseline hiện tại mà baseline ở ref không có — baseline chỉ được co lại.
func Growth(old, now []Coupling) []string {
	had := map[string]bool{}
	for _, c := range old {
		had[c.key()] = true
	}
	var out []string
	for _, c := range now {
		if !had[c.key()] {
			out = append(out, "THÊM VÀO BASELINE "+c.String()+" (baseline chỉ được co lại — gỡ ghép nối thay vì ghi nhận)")
		}
	}
	return out
}

// Prune giữ các mục baseline còn xảy ra.
func Prune(baseline, cur []Coupling) []Coupling {
	inCur := map[string]bool{}
	for _, c := range cur {
		inCur[c.key()] = true
	}
	var out []Coupling
	for _, c := range baseline {
		if inCur[c.key()] {
			out = append(out, c)
		}
	}
	sortCouplings(out)
	return out
}

const baselineHeader = `# Baseline của cổng knboundary (KNSPLIT-0, ADR-0055) — ghép nối KN ↔ phần còn lại ĐANG CÓ, sinh từ kiểm kê
# (go run ./tools/knboundary -list) tại thời điểm quét. Quy tắc: tools/knboundary/boundary.yaml; kiểm kê và cách phân loại
# (chuyển sang KN / ở lại nền tảng / cần port): docs/kn/tach-he-thong/kiem-ke.md.
#
# CHỈ ĐƯỢC CO LẠI: ghép nối mới làm hỏng cổng; mục lỗi thời cũng làm hỏng cổng (xóa bằng -prune trong cùng PR đã gỡ);
# CI chạy -base <nhánh đích> nên không thêm được mục mới. Gói gỡ: KNSPLIT-1 (không gian tên), KNSPLIT-2 (tách phần KN
# khỏi module dùng chung), KNSPLIT-3 (app web KN riêng).
`

// FormatBaseline ghi baseline dạng một mục mỗi dòng (diff dễ đọc), đã sắp xếp.
func FormatBaseline(cs []Coupling) []byte {
	cs = append([]Coupling(nil), cs...)
	sortCouplings(cs)
	var b strings.Builder
	b.WriteString(baselineHeader)
	if len(cs) == 0 {
		b.WriteString("couplings: []\n")
		return []byte(b.String())
	}
	b.WriteString("couplings:\n")
	for _, c := range cs {
		fmt.Fprintf(&b, "  - {rule: %s, from: %s, to: %s}\n", c.Rule, yamlScalar(c.From), yamlScalar(c.To))
	}
	return []byte(b.String())
}

func yamlScalar(s string) string {
	out, err := yaml.Marshal(s)
	if err != nil {
		return fmt.Sprintf("%q", s)
	}
	v := strings.TrimSuffix(string(out), "\n")
	if strings.ContainsAny(v, "{}[],") && !strings.HasPrefix(v, "'") && !strings.HasPrefix(v, "\"") {
		return fmt.Sprintf("%q", s)
	}
	return v
}

func say(w io.Writer, format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
