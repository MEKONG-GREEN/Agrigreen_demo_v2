// Lệnh migratecheck là cổng CI cho migration (R-23, R-25):
//
//   - tên tệp NNNNNN_ten.up.sql, số không trùng trong mỗi thư mục cụm (số được cấp
//     trước cho từng module — docs/dev/g1-conventions.md — nên có thể có khoảng trống)
//   - không có câu lệnh UPDATE, DELETE, TRUNCATE ở mức migration (được phép
//     bên trong thân hàm $$...$$)
//   - không tắt cơ chế bảo vệ: DISABLE TRIGGER, DROP TRIGGER trg_immutable/trg_chain,
//     NO FORCE / DISABLE ROW LEVEL SECURITY, DROP POLICY p_tenant
//
// Chạy: go run ./tools/migratecheck migrations
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

func main() {
	dir := "migrations"
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	problems, err := checkTree(dir)
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

var nameRe = regexp.MustCompile(`^(\d{6})_[a-z0-9_]+\.up\.sql$`)

func checkTree(root string) ([]string, error) {
	var problems []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
		if err != nil {
			return nil, err
		}
		sort.Strings(files)
		seen := map[int]string{}
		for _, f := range files {
			base := filepath.Base(f)
			m := nameRe.FindStringSubmatch(base)
			if m == nil {
				problems = append(problems, fmt.Sprintf("%s: tên tệp phải là NNNNNN_ten.up.sql (chỉ tiến, R-24)", f))
				continue
			}
			n, _ := strconv.Atoi(m[1])
			if prev, dup := seen[n]; dup {
				problems = append(problems, fmt.Sprintf("%s: trùng số %06d với %s", f, n, prev))
			}
			seen[n] = base
			src, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			for _, p := range CheckSQL(string(src)) {
				problems = append(problems, f+": "+p)
			}
		}
	}
	return problems, nil
}

var forbidden = []struct {
	re  *regexp.Regexp
	msg string
}{
	{regexp.MustCompile(`(?is)^\s*(WITH\b.*\)\s*)?UPDATE\b`), "R-25: migration không được chứa UPDATE"},
	{regexp.MustCompile(`(?is)^\s*(WITH\b.*\)\s*)?DELETE\b`), "R-25: migration không được chứa DELETE"},
	{regexp.MustCompile(`(?is)^\s*TRUNCATE\b`), "R-25: migration không được chứa TRUNCATE"},
	{regexp.MustCompile(`(?is)\bDISABLE\s+TRIGGER\b`), "RB-1: không được tắt trigger"},
	{regexp.MustCompile(`(?is)^\s*DROP\s+TRIGGER\b.*\btrg_(immutable|immutable_trunc|chain)\b`), "RB-1: không được xóa trigger bảo vệ sổ"},
	{regexp.MustCompile(`(?is)\bNO\s+FORCE\s+ROW\s+LEVEL\s+SECURITY\b`), "RB-2: không được bỏ FORCE RLS"},
	{regexp.MustCompile(`(?is)\bDISABLE\s+ROW\s+LEVEL\s+SECURITY\b`), "RB-2: không được tắt RLS"},
	{regexp.MustCompile(`(?is)^\s*DROP\s+POLICY\b.*\bp_tenant\b`), "RB-2: không được xóa policy p_tenant"},
	{regexp.MustCompile(`(?is)\bsession_replication_role\b`), "RB-1: không được đổi session_replication_role"},
}

// CheckSQL trả danh sách vi phạm trong một tệp migration.
func CheckSQL(src string) []string {
	var out []string
	for i, stmt := range Statements(src) {
		for _, f := range forbidden {
			if f.re.MatchString(stmt) {
				out = append(out, fmt.Sprintf("câu lệnh %d: %s: %s", i+1, f.msg, abbreviate(stmt)))
			}
		}
	}
	return out
}

func abbreviate(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

var dollarTagRe = regexp.MustCompile(`^\$[A-Za-z_]*\$`)

// Statements tách câu lệnh ở mức cao nhất: bỏ chú thích, chuỗi '...', định danh
// "..." và thân $tag$...$tag$ (thân hàm được thay bằng khoảng trắng).
func Statements(src string) []string {
	var stmts []string
	var cur strings.Builder
	for i := 0; i < len(src); {
		switch {
		case strings.HasPrefix(src[i:], "--"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				i = len(src)
			} else {
				i += end + 4
			}
			cur.WriteByte(' ')
		case src[i] == '\'' || src[i] == '"':
			q := src[i]
			i++
			for i < len(src) {
				if src[i] == q {
					if i+1 < len(src) && src[i+1] == q {
						i += 2
						continue
					}
					i++
					break
				}
				i++
			}
			cur.WriteString(" '' ")
		case src[i] == '$' && dollarTagRe.MatchString(src[i:]):
			tag := dollarTagRe.FindString(src[i:])
			end := strings.Index(src[i+len(tag):], tag)
			if end < 0 {
				i = len(src)
			} else {
				i += len(tag) + end + len(tag)
			}
			cur.WriteString(" $body$ ")
		case src[i] == ';':
			if s := strings.TrimSpace(cur.String()); s != "" {
				stmts = append(stmts, s)
			}
			cur.Reset()
			i++
		default:
			cur.WriteByte(src[i])
			i++
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		stmts = append(stmts, s)
	}
	return stmts
}
