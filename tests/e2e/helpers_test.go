package e2e

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func readAll(r io.Reader) ([]byte, error) { return io.ReadAll(r) }

// scanDir đọc mọi tệp trong kho đối tượng cục bộ và báo nếu có chuỗi dữ liệu cá
// nhân dạng rõ (gói MRV, tệp minh chứng — chỉ được có bản mã và mã tham chiếu).
func scanDir(dir string, needles []string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p) //nolint:gosec // thư mục tạm của kiểm thử
		if err != nil {
			return err
		}
		for _, n := range needles {
			if strings.Contains(string(b), n) {
				return fmt.Errorf("kho đối tượng %s chứa dữ liệu cá nhân %q", p, n)
			}
		}
		return nil
	})
}
