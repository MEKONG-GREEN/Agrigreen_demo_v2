package db_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/modreg"
)

// repoRoot: gốc repo tính từ thư mục gói (internal/shared/db).
const repoRoot = "../../.."

// Cổng W2.6 (ADR-0028, R-DB-02/03), phần tĩnh: MỌI module có schema (owns_schema) khai `DBModule = …Module("<tên>")`
// ở gốc module và bọc pool bằng ForModule — giao dịch của module luôn chạy SET LOCAL ROLE r_<module>. Phần động
// (vai đăng nhập app_* không quyền bảng, truy vấn ngoài giao dịch của module bị 42501) ở
// migrations/login_roles_gate_integration_test.go.
func TestEveryModuleWrapsPoolForModule_ADR0028(t *testing.T) {
	var dirs []string
	err := filepath.WalkDir(filepath.Join(repoRoot, "internal"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == modreg.FileName {
			dirs = append(dirs, filepath.Dir(p))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, dir := range dirs {
		raw, err := os.ReadFile(filepath.Join(dir, modreg.FileName))
		if err != nil {
			t.Fatal(err)
		}
		m, err := modreg.Parse(raw, filepath.Join(dir, modreg.FileName))
		if err != nil {
			t.Fatal(err)
		}
		if len(m.OwnsSchema) == 0 {
			continue
		}
		decl := regexp.MustCompile(`\bDBModule\s*=\s*\w+\.Module\("` + regexp.QuoteMeta(m.Name) + `"\)`)
		var hasDecl, hasWrap bool
		// Gốc module, hoặc app/ khi gốc chỉ đặt lại (pf/kref: DBModule = app.DBModule).
		for _, sub := range []string{dir, filepath.Join(dir, "app")} {
			ents, err := os.ReadDir(sub)
			if err != nil {
				continue
			}
			for _, e := range ents {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
					continue
				}
				src, err := os.ReadFile(filepath.Join(sub, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				hasDecl = hasDecl || decl.Match(src)
				hasWrap = hasWrap || strings.Contains(string(src), "ForModule(")
			}
		}
		if !hasDecl || !hasWrap {
			t.Errorf("%s (%s): thiếu `var DBModule = db.Module(%q)` hoặc db.ForModule ở gốc module (docs/dev/modules.md)", m.Name, dir, m.Name)
		}
		checked++
	}
	if checked < 26 {
		t.Fatalf("chỉ kiểm %d module có schema (muốn ≥ 26)", checked)
	}
}

// legacyLogin khớp chuỗi tên vai đăng nhập cũ (<module>_app, vai không theo module) trong mã không phải kiểm thử.
var legacyLogin = regexp.MustCompile(`"([a-z]+_app|platform_api|public_api|worker_platform|mkg_api|worker_internal|ingest_mkg|ingest_ts|ts_reader)"|\.DBRole\b`)

// Cổng W2.6: cmd/* chỉ mở pool bằng vai đăng nhập đơn vị (dbadmin.App*), ag_outbox_publisher, ag_migrator — không
// còn tham chiếu vai đăng nhập cũ (giữ tên một kỳ cho lùi bản, xóa ở bản phát hành sau).
func TestCmdOpensOnlyUnitLogins_ADR0028(t *testing.T) {
	err := filepath.WalkDir(filepath.Join(repoRoot, "cmd"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return err
		}
		src, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(src), "\n") {
			if code, _, _ := strings.Cut(line, "//"); legacyLogin.MatchString(code) {
				t.Errorf("%s:%d: vai đăng nhập cũ — dùng dbadmin.App* (ADR-0028): %s", p, i+1, strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
