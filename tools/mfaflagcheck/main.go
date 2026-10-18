// Lệnh mfaflagcheck là cổng CI cho cờ demo AG_MFA_DISABLED (docs/dev/sync-main-2026-10-13.md):
//
//   - Không tệp cấu hình triển khai nào ngoài tệp demo được BẬT AG_MFA_DISABLED (1/true, kể cả dạng mặc định
//     ${AG_MFA_DISABLED:-1}) hay đặt AG_DEPLOY_ENV=demo (kể cả ${AG_DEPLOY_ENV:-demo}).
//   - Tệp demo: đường dẫn (tên tệp hoặc thư mục) chứa "demo", vd .github/workflows/trien-khai-demo.yml.
//   - Phạm vi: deploy/, .github/, scripts/ và mọi tệp .env*, *.env, docker-compose*.y*ml, Dockerfile* trong repo
//     (bỏ .git, node_modules, testdata, docs). Dòng chú thích (#, //) bỏ qua. Dạng k8s "name: AG_MFA_DISABLED"
//     kèm "value: ..." ở dòng sau cũng được kiểm.
//
// Guard lúc chạy (authz.DemoMFABypassAllowed) vẫn là hàng rào chính; cổng này bắt lỗi trước khi lên máy.
//
// Chạy: go run ./tools/mfaflagcheck [-root .]
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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

var (
	// KEY: v | KEY=v | KEY: "${KEY:-v}" ...; v bật = 1/true; AG_DEPLOY_ENV bật = demo.
	reMFAOn    = regexp.MustCompile(`(?i)\bAG_MFA_DISABLED["']?\s*[:=]\s*["']?(?:\$\{AG_MFA_DISABLED:?-)?(1|true)\b`)
	reDeployOn = regexp.MustCompile(`(?i)\bAG_DEPLOY_ENV["']?\s*[:=]\s*["']?(?:\$\{AG_DEPLOY_ENV:?-)?demo\b`)
	// k8s env: - name: AG_MFA_DISABLED / value: "1".
	reK8sName  = regexp.MustCompile(`^\s*-?\s*name:\s*["']?(AG_MFA_DISABLED|AG_DEPLOY_ENV)["']?\s*$`)
	reK8sValue = regexp.MustCompile(`(?i)^\s*value:\s*["']?(1|true|demo)["']?\s*$`)
)

var skipDirs = map[string]bool{".git": true, "node_modules": true, "testdata": true, "docs": true, ".next": true, "build": true}

// inScope: tệp cấu hình triển khai cần kiểm.
func inScope(rel string) bool {
	rel = filepath.ToSlash(rel)
	base := strings.ToLower(filepath.Base(rel))
	ext := strings.ToLower(filepath.Ext(rel))
	switch {
	case strings.HasPrefix(base, ".env") || strings.HasSuffix(base, ".env"),
		strings.HasPrefix(base, "docker-compose") && (ext == ".yml" || ext == ".yaml"),
		strings.HasPrefix(base, "dockerfile"):
		return true
	}
	for _, d := range []string{"deploy/", ".github/", "scripts/"} {
		if strings.HasPrefix(rel, d) {
			switch ext {
			case ".yml", ".yaml", ".env", ".sh", ".ps1", ".json", ".tpl", ".toml", ".tf", ".tfvars", "":
				return true
			}
		}
	}
	return false
}

// isDemo: tệp dành riêng cho máy demo được phép bật cờ.
func isDemo(rel string) bool { return strings.Contains(strings.ToLower(filepath.ToSlash(rel)), "demo") }

// Check trả danh sách vi phạm dạng "đường-dẫn:dòng: ...".
func Check(root string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if d.IsDir() {
			if path != root && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !inScope(rel) || isDemo(rel) {
			return nil
		}
		p, err := checkFile(path, filepath.ToSlash(rel))
		out = append(out, p...)
		return err
	})
	return out, err
}

func checkFile(path, rel string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	n, pendingK8s := 0, ""
	for sc.Scan() {
		n++
		line := sc.Text()
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") {
			continue
		}
		if pendingK8s != "" {
			if m := reK8sValue.FindStringSubmatch(line); m != nil {
				v := strings.ToLower(m[1])
				if (pendingK8s == "AG_MFA_DISABLED" && v != "demo") || (pendingK8s == "AG_DEPLOY_ENV" && v == "demo") {
					out = append(out, fmt.Sprintf("%s:%d: %s=%s trong tệp triển khai không phải demo", rel, n, pendingK8s, m[1]))
				}
			}
			pendingK8s = ""
		}
		if m := reK8sName.FindStringSubmatch(line); m != nil {
			pendingK8s = m[1]
			continue
		}
		if reMFAOn.MatchString(line) {
			out = append(out, fmt.Sprintf("%s:%d: AG_MFA_DISABLED bật trong tệp triển khai không phải demo (chỉ máy demo, đặt trong .env của máy)", rel, n))
		}
		if reDeployOn.MatchString(line) {
			out = append(out, fmt.Sprintf("%s:%d: AG_DEPLOY_ENV=demo trong tệp triển khai không phải demo", rel, n))
		}
	}
	return out, sc.Err()
}
