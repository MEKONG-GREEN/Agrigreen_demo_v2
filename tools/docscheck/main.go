// Lệnh docscheck là cổng CI cho tài liệu dùng chung giữa các gói chạy song song (WH-B, docs/dev/parallel-packages.md):
//
//   - ADR (docs/adr): số NNNN trong tên tệp NNNN-*.md không trùng; bảng chỉ mục README.md có đúng một dòng cho mỗi
//     tệp và không dòng nào trỏ tới tệp không có; mục "Số đã giữ chỗ" không trùng nhau và không trùng ADR đã viết
//     (bài học: cap/export giữ 0050, pf/hrm viết 0050).
//   - Giả định theo gói (docs/kn/gia-dinh/<gói>.md): tên là mã gói, dòng đầu "## <gói> — …", mọi tiêu đề "## " của
//     tệp là của gói đó; liên kết tương đối trỏ tới tệp có thật.
//   - Dòng roadmap theo gói (docs/roadmap.d/<mục>/<gói>.md): mỗi dòng không trống là "- [ ] <gói>…" hoặc
//     "- [x] <gói>…"; liên kết tương đối trỏ tới tệp có thật.
//   - Mã lỗi theo module (docs/errors/<cụm>-<module>.md): có tiêu đề "### "; mã ở cột code không trùng với mã trong
//     docs/errors.md hay tệp khác của thư mục.
//
// Chạy: go run ./tools/docscheck [-root .]
package main

import (
	"flag"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

func main() {
	root := flag.String("root", ".", "gốc repo")
	flag.Parse()
	problems, err := Check(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	for _, p := range problems {
		fmt.Println(p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
}

// Check chạy mọi kiểm.
func Check(root string) ([]string, error) {
	var out []string
	for _, f := range []func(string) ([]string, error){checkADR, checkAssumptions, checkRoadmapD, checkErrors} {
		p, err := f(root)
		if err != nil {
			return nil, err
		}
		out = append(out, p...)
	}
	return out, nil
}

func read(p string) (string, error) {
	b, err := os.ReadFile(p)
	return strings.ReplaceAll(string(b), "\r\n", "\n"), err
}

// mdFiles trả các tệp *.md (trừ README.md) của dir, theo tên; dir không có → rỗng.
func mdFiles(dir string) ([]string, error) {
	fs, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range fs {
		if filepath.Base(f) != "README.md" {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}

var (
	adrFileRe    = regexp.MustCompile(`^(\d{4})-[a-z0-9-]+\.md$`)
	adrIndexRe   = regexp.MustCompile(`^\| (\d{4}) \| .*?\]\(([^)]+\.md)\)`)
	adrReserveRe = regexp.MustCompile(`^\| (\d{4}) \|`)
)

const adrReserveHeading = "## Số đã giữ chỗ"

func checkADR(root string) ([]string, error) {
	dir := filepath.Join(root, "docs", "adr")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	files := map[string]string{} // số → tệp
	for _, e := range entries {
		m := adrFileRe.FindStringSubmatch(e.Name())
		if e.IsDir() || m == nil {
			continue
		}
		if prev, dup := files[m[1]]; dup {
			out = append(out, fmt.Sprintf("docs/adr/%s: trùng số ADR %s với %s — lấy số trống kế tiếp (README mục %q)", e.Name(), m[1], prev, adrReserveHeading))
			continue
		}
		files[m[1]] = e.Name()
	}
	readme, err := read(filepath.Join(dir, "README.md"))
	if err != nil {
		return nil, err
	}
	indexed := map[string]string{}
	reserved := map[string]bool{}
	inReserve := false
	for _, l := range strings.Split(readme, "\n") {
		if strings.HasPrefix(l, "## ") {
			inReserve = strings.TrimSpace(l) == adrReserveHeading
			continue
		}
		if inReserve {
			if m := adrReserveRe.FindStringSubmatch(l); m != nil {
				if reserved[m[1]] {
					out = append(out, fmt.Sprintf("docs/adr/README.md: số %s giữ chỗ hai lần", m[1]))
				}
				reserved[m[1]] = true
				if f, ok := files[m[1]]; ok {
					out = append(out, fmt.Sprintf("docs/adr/README.md: số %s đang giữ chỗ nhưng đã có ADR %s — viết ADR thì xóa dòng giữ chỗ; nếu là ADR khác thì đổi giữ chỗ sang số trống", m[1], f))
				}
			}
			continue
		}
		m := adrIndexRe.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		if prev, dup := indexed[m[1]]; dup {
			out = append(out, fmt.Sprintf("docs/adr/README.md: số %s có hai dòng chỉ mục (%s, %s)", m[1], prev, m[2]))
		}
		indexed[m[1]] = m[2]
		switch f, ok := files[m[1]]; {
		case !ok:
			out = append(out, fmt.Sprintf("docs/adr/README.md: dòng %s trỏ tới %s — không có tệp ADR số đó", m[1], m[2]))
		case f != m[2]:
			out = append(out, fmt.Sprintf("docs/adr/README.md: dòng %s trỏ tới %s, tệp là %s", m[1], m[2], f))
		}
	}
	for n, f := range files {
		if _, ok := indexed[n]; !ok {
			out = append(out, fmt.Sprintf("docs/adr/%s: thiếu dòng trong bảng chỉ mục README.md", f))
		}
	}
	sort.Strings(out)
	return out, nil
}

var (
	// Mã gói: W11.42, W11.05b, hoặc gói có tên (KHTCQA, WH-B).
	pkgFileRe = regexp.MustCompile(`^(W\d+\.\d+[a-z]?|[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)*)\.md$`)
	linkRe    = regexp.MustCompile(`\]\(([^)\s]+)\)`)
)

// checkLinks: liên kết tương đối (không http, không chỉ #neo) phải trỏ tới tệp/thư mục có thật.
func checkLinks(root, file, text string) []string {
	var out []string
	for _, m := range linkRe.FindAllStringSubmatch(text, -1) {
		target := m[1]
		if strings.Contains(target, "://") || strings.HasPrefix(target, "#") || strings.HasPrefix(target, "mailto:") {
			continue
		}
		target, _, _ = strings.Cut(target, "#")
		if t, err := url.PathUnescape(target); err == nil {
			target = t
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(file), filepath.FromSlash(target))); err != nil {
			out = append(out, fmt.Sprintf("%s: liên kết hỏng %s (tương đối thư mục của tệp)", rel(root, file), m[1]))
		}
	}
	return out
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return filepath.ToSlash(r)
	}
	return p
}

func checkAssumptions(root string) ([]string, error) {
	files, err := mdFiles(filepath.Join(root, "docs", "kn", "gia-dinh"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		m := pkgFileRe.FindStringSubmatch(filepath.Base(f))
		if m == nil {
			out = append(out, fmt.Sprintf("%s: tên tệp phải là mã gói, vd W11.42.md", rel(root, f)))
			continue
		}
		text, err := read(f)
		if err != nil {
			return nil, err
		}
		if !strings.HasPrefix(text, "## "+m[1]+" — ") {
			out = append(out, fmt.Sprintf("%s: dòng đầu phải là \"## %s — <tên gói>\"", rel(root, f), m[1]))
		}
		for _, l := range strings.Split(text, "\n") {
			if strings.HasPrefix(l, "## ") && !strings.HasPrefix(l, "## "+m[1]+" ") {
				out = append(out, fmt.Sprintf("%s: tiêu đề %q không thuộc gói %s — mỗi gói một tệp", rel(root, f), l, m[1]))
			}
		}
		out = append(out, checkLinks(root, f, text)...)
	}
	return out, nil
}

func checkRoadmapD(root string) ([]string, error) {
	base := filepath.Join(root, "docs", "roadmap.d")
	entries, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := mdFiles(filepath.Join(base, e.Name()))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			m := pkgFileRe.FindStringSubmatch(filepath.Base(f))
			if m == nil {
				out = append(out, fmt.Sprintf("%s: tên tệp phải là mã gói, vd W11.42.md", rel(root, f)))
				continue
			}
			text, err := read(f)
			if err != nil {
				return nil, err
			}
			n := 0
			for _, l := range strings.Split(text, "\n") {
				if strings.TrimSpace(l) == "" {
					continue
				}
				n++
				if !strings.HasPrefix(l, "- [ ] "+m[1]) && !strings.HasPrefix(l, "- [x] "+m[1]) {
					out = append(out, fmt.Sprintf("%s: mỗi dòng phải là \"- [ ] %s…\" hoặc \"- [x] %s…\": %.60q", rel(root, f), m[1], m[1], l))
				}
			}
			if n == 0 {
				out = append(out, fmt.Sprintf("%s: tệp rỗng", rel(root, f)))
			}
			out = append(out, checkLinks(root, f, text)...)
		}
	}
	return out, nil
}

// errCodeRe: dòng bảng mã lỗi "| 409 | `code` | …".
var errCodeRe = regexp.MustCompile("(?m)^\\| \\d{3} \\| `([a-z0-9_.]+)`")

func checkErrors(root string) ([]string, error) {
	files, err := mdFiles(filepath.Join(root, "docs", "errors"))
	if err != nil || len(files) == 0 {
		return nil, err
	}
	main, err := read(filepath.Join(root, "docs", "errors.md"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	where := map[string]string{}
	for _, m := range errCodeRe.FindAllStringSubmatch(main, -1) {
		where[m[1]] = "docs/errors.md"
	}
	var out []string
	for _, f := range files {
		text, err := read(f)
		if err != nil {
			return nil, err
		}
		if !regexp.MustCompile(`(?m)^### `).MatchString(text) {
			out = append(out, fmt.Sprintf("%s: thiếu tiêu đề \"### <cụm>/<module> …\"", rel(root, f)))
		}
		seen := map[string]bool{}
		for _, m := range errCodeRe.FindAllStringSubmatch(text, -1) {
			if seen[m[1]] {
				out = append(out, fmt.Sprintf("%s: mã %s xuất hiện hai lần", rel(root, f), m[1]))
				continue
			}
			seen[m[1]] = true
			if w, ok := where[m[1]]; ok {
				out = append(out, fmt.Sprintf("%s: mã %s đã có ở %s — mã lỗi là hợp đồng, không dùng lại", rel(root, f), m[1], w))
			}
		}
		for c := range seen {
			where[c] = rel(root, f)
		}
		out = append(out, checkLinks(root, f, text)...)
	}
	return out, nil
}
