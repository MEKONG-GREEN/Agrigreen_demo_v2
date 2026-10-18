package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/migrator"
)

// baseFiles đọc mọi tệp *.sql dưới root ở ref git base: "<thư mục>/<tên>". Lỗi nếu ref không tồn tại (CI phải
// fetch nhánh đích: actions/checkout fetch-depth 0).
func baseFiles(root, base string) (map[string]bool, error) {
	if err := exec.Command("git", "-C", root, "rev-parse", "--verify", "--quiet", base+"^{commit}").Run(); err != nil {
		return nil, fmt.Errorf("migratecheck -base: không đọc được ref %q (git fetch origin +refs/heads/dev:refs/remotes/origin/dev?): %w", base, err)
	}
	// ls-tree với -C <root> và pathspec "." in đường dẫn tương đối root.
	out, err := exec.Command("git", "-C", root, "ls-tree", "-r", "--name-only", base, "--", ".").Output()
	if err != nil {
		return nil, fmt.Errorf("migratecheck -base: git ls-tree %s: %w", base, err)
	}
	files := map[string]bool{}
	for _, l := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		l = strings.TrimSpace(l)
		if strings.Count(l, "/") == 1 && strings.HasSuffix(l, ".sql") {
			files[l] = true
		}
	}
	return files, nil
}

// checkBase áp quy tắc so với nhánh tích hợp (WH-B): tệp không có trên base là tệp mới — phải dạng mốc thời gian
// (không cấp số tập trung) và có phiên bản lớn hơn phiên bản lớn nhất của thư mục trên base. Tệp xếp trước số đã
// có trên base sẽ bị golang-migrate (bộ chạy mặc định) bỏ qua im lặng ở mọi DB đã áp base. Tệp đã có trên base luôn
// được chấp nhận — kể cả số tuần tự của gói song song đã gộp.
func checkBase(root, base string) ([]string, error) {
	onBase, err := baseFiles(root, base)
	if err != nil {
		return nil, err
	}
	maxV := map[string]int64{}
	maxName := map[string]string{}
	for k := range onBase {
		dir, name := path.Split(k)
		dir = strings.TrimSuffix(dir, "/")
		if nm, ok := migrator.ParseName(name); ok && nm.Version > maxV[dir] {
			maxV[dir], maxName[dir] = nm.Version, name
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var problems []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, e.Name(), "*.sql"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		for _, f := range files {
			name := filepath.Base(f)
			if onBase[e.Name()+"/"+name] {
				continue
			}
			nm, ok := migrator.ParseName(name)
			if !ok {
				continue // tên sai: checkTree đã báo
			}
			if nm.Scheme != migrator.SchemeTimestamp {
				problems = append(problems, fmt.Sprintf("%s: tệp mới (không có trên %s) phải dùng tên mốc UTC YYYYMMDDHHMMSS_%s_<module>_… — không cấp số tuần tự nữa (go run ./tools/migratecheck -new <tên>)", f, base, e.Name()))
				continue
			}
			if nm.Version <= maxV[e.Name()] {
				problems = append(problems, fmt.Sprintf("%s: phiên bản %d không lớn hơn phiên bản lớn nhất trên %s (%s) — golang-migrate bỏ qua im lặng tệp xếp trước số đã áp; đổi tên sang mốc mới: go run ./tools/migratecheck -new %s", f, nm.Version, base, maxName[e.Name()], nm.Stem))
			}
		}
	}
	return problems, nil
}
