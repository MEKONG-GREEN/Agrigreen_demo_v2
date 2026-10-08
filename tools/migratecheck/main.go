// Lệnh migratecheck là cổng CI cho migration (R-23, R-25):
//
//   - tên tệp NNNNNN_ten.up.sql, số không trùng trong mỗi thư mục cụm (số được cấp
//     trước cho từng module — docs/dev/g1-conventions.md — nên có thể có khoảng trống)
//   - không có câu lệnh UPDATE, DELETE, TRUNCATE ở mức migration (được phép
//     bên trong thân hàm $$...$$)
//   - không tắt cơ chế bảo vệ: DISABLE TRIGGER, DROP TRIGGER trg_immutable/trg_chain,
//     NO FORCE / DISABLE ROW LEVEL SECURITY, DROP POLICY p_tenant
//   - mọi thư mục cụm có mặt trong chỉ thị go:embed của migrations/embed.go (thiếu →
//     cụm không bao giờ được áp, pgtest và migrate up bỏ qua im lặng)
//   - cụm ts (chuỗi thời gian, ADR-0017): không tham chiếu schema pf_* / mkg_* /
//     vault (R-07), không CREATE EXTENSION timescaledb (TSL không dùng — RB-4)
//   - khóa khi sửa bảng đã có (CheckLocking): ALTER TABLE … ADD CONSTRAINT CHECK/FOREIGN KEY
//     phải NOT VALID (rồi VALIDATE CONSTRAINT ở câu riêng — chỉ khóa SHARE UPDATE EXCLUSIVE);
//     tệp có ALTER TABLE trên bảng không tạo trong cùng tệp phải SET LOCAL lock_timeout trước
//     đó. Tệp đã áp trước quy tắc được miễn theo số (lockGrandfathered) — không sửa được (R-24)
//   - cột của bảng sổ (CheckLedgerColumns, ADR-0012): ADD COLUMN phải NULL được, không DEFAULT, và
//     đăng ký bằng shared.register_ledger_columns trong cùng tệp; cấm DROP/RENAME/đổi kiểu cột. Sổ chuyển schema
//     (ALTER TABLE … SET SCHEMA, W3.3) vẫn là sổ dưới tên mới
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

var embedRe = regexp.MustCompile(`(?m)^//go:embed\s+(.+)$`)

// embedded đọc các mẫu go:embed của migrations/embed.go; nil nếu không có tệp.
func embedded(root string) (map[string]bool, error) {
	src, err := os.ReadFile(filepath.Join(root, "embed.go"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, m := range embedRe.FindAllStringSubmatch(string(src), -1) {
		for _, pat := range strings.Fields(m[1]) {
			out[pat] = true
		}
	}
	return out, nil
}

var (
	tsForeignSchemaRe = regexp.MustCompile(`(?i)\b(pf_[a-z_]+|mkg_[a-z_]+|vault)\.`)
	timescaleRe       = regexp.MustCompile(`(?is)\bCREATE\s+EXTENSION\b[^;]*\btimescaledb`)
)

func checkTree(root string) ([]string, error) {
	var problems []string
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	embeds, err := embedded(root)
	if err != nil {
		return nil, err
	}
	ledgers, err := ledgerSet(root, entries)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if embeds != nil && !embeds[e.Name()+"/*.sql"] {
			problems = append(problems, fmt.Sprintf("%s: thư mục cụm %s không có trong //go:embed của embed.go — migration sẽ không bao giờ được áp", filepath.Join(root, "embed.go"), e.Name()))
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
			if !lockGrandfathered[e.Name()][n] {
				for _, p := range CheckLocking(string(src)) {
					problems = append(problems, f+": "+p)
				}
			}
			if !ledgerColumnsGrandfathered[e.Name()][n] {
				for _, p := range CheckLedgerColumns(string(src), ledgers) {
					problems = append(problems, f+": "+p)
				}
			}
			if timescaleRe.MatchString(string(src)) {
				problems = append(problems, f+": ADR-0017: không dùng extension timescaledb (tính năng TSL, RB-4) — phân vùng khai báo PostgreSQL thuần")
			}
			if e.Name() == "ts" {
				for _, stmt := range Statements(string(src)) {
					if m := tsForeignSchemaRe.FindString(stmt); m != "" {
						problems = append(problems, fmt.Sprintf("%s: R-07: migration cụm ts không được tham chiếu schema nghiệp vụ %s (%s)", f, strings.TrimSuffix(m, "."), abbreviate(stmt)))
					}
				}
			}
		}
	}
	return problems, nil
}

// ledgerSet gom tên mọi bảng sổ (shared.apply_ledger) của mọi thư mục cụm.
func ledgerSet(root string, entries []os.DirEntry) (map[string]bool, error) {
	out := map[string]bool{}
	var moves [][2]string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, e.Name(), "*.sql"))
		if err != nil {
			return nil, err
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				return nil, err
			}
			for _, t := range LedgerTables(string(src)) {
				out[t] = true
			}
			moves = append(moves, LedgerMoves(string(src))...)
		}
	}
	// Sổ chuyển schema (ALTER TABLE … SET SCHEMA, W3.3) vẫn là sổ dưới tên mới; lặp tới điểm bất động cho
	// chuyển nhiều chặng. Tên cũ giữ trong tập (tệp cũ đã áp vẫn được kiểm như trước).
	for changed := true; changed; {
		changed = false
		for _, m := range moves {
			if out[m[0]] && !out[m[1]] {
				out[m[1]], changed = true, true
			}
		}
	}
	return out, nil
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
	// ADR-0028 §3 (R-DB-02): migration chạy bằng ag_migrator, không đổi vai, không để lại trạng thái mức phiên.
	// Mệnh đề SET … của CREATE FUNCTION nằm giữa câu lệnh nên không khớp.
	{regexp.MustCompile(`(?is)^\s*SET\s+(LOCAL\s+)?ROLE\b`), "ADR-0028: migration không được SET ROLE"},
	{regexp.MustCompile(`(?is)^\s*(RESET\b|SET\s+(SESSION\b|TIME\s+ZONE\b|[a-z_."]+\s*(=|\bTO\b)))`), "ADR-0028: migration không được đặt biến mức phiên (SET/RESET)"},
	{regexp.MustCompile(`(?is)\bset_config\s*\([^)]*,\s*false\s*\)`), "ADR-0028: migration không được set_config(…, false) mức phiên"},
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

var (
	createTableRe = regexp.MustCompile(`(?is)^\s*CREATE\s+(?:UNLOGGED\s+)?TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-z0-9_.]+)`)
	alterTableRe  = regexp.MustCompile(`(?is)^\s*ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:ONLY\s+)?([a-z0-9_.]+)`)
	// ADD [CONSTRAINT tên] CHECK | FOREIGN KEY — không khớp ADD COLUMN … CHECK (cột mới).
	addValidatedRe = regexp.MustCompile(`(?is)\bADD\s+(?:CONSTRAINT\s+[a-z0-9_]+\s+)?(?:CHECK|FOREIGN\s+KEY)\b`)
	notValidRe     = regexp.MustCompile(`(?is)\bNOT\s+VALID\b`)
	lockTimeoutRe  = regexp.MustCompile(`(?is)^\s*SET\s+LOCAL\s+lock_timeout\b`)
)

// CheckLocking trả vi phạm quy tắc khóa của một tệp migration:
//
//   - ALTER TABLE … ADD CONSTRAINT … CHECK / FOREIGN KEY trên bảng đã có mà thiếu NOT VALID:
//     PostgreSQL giữ ACCESS EXCLUSIVE suốt lúc quét toàn bảng (bảng sổ phân vùng: mọi phân
//     vùng). Mẫu đúng: ADD CONSTRAINT … NOT VALID; rồi ALTER TABLE … VALIDATE CONSTRAINT …
//     ở câu riêng (SHARE UPDATE EXCLUSIVE, không chặn ghi).
//   - ALTER TABLE trên bảng đã có mà chưa SET LOCAL lock_timeout ở câu trước đó: chờ khóa sau
//     một giao dịch dài sẽ chặn mọi truy vấn xếp hàng sau nó. SET LOCAL (không phải SET) vì
//     migration không được đặt biến mức phiên (ADR-0028); mỗi tệp chạy trong một giao dịch.
//
// Bảng tạo trong cùng tệp (CREATE TABLE, kể cả PARTITION OF) còn rỗng nên được miễn cả hai;
// ràng buộc khai trong CREATE TABLE luôn được phép.
func CheckLocking(src string) []string {
	var out []string
	created := map[string]bool{}
	timeout := false
	for i, stmt := range Statements(src) {
		if m := createTableRe.FindStringSubmatch(stmt); m != nil {
			created[strings.ToLower(m[1])] = true
			continue
		}
		if lockTimeoutRe.MatchString(stmt) {
			timeout = true
			continue
		}
		m := alterTableRe.FindStringSubmatch(stmt)
		if m == nil || created[strings.ToLower(m[1])] {
			continue
		}
		if !timeout {
			out = append(out, fmt.Sprintf("câu lệnh %d: khóa: ALTER TABLE bảng đã có phải đặt SET LOCAL lock_timeout trước (vd SET LOCAL lock_timeout = '5s'): %s", i+1, abbreviate(stmt)))
		}
		if len(addValidatedRe.FindAllStringIndex(stmt, -1)) > len(notValidRe.FindAllStringIndex(stmt, -1)) {
			out = append(out, fmt.Sprintf("câu lệnh %d: khóa: ADD CONSTRAINT CHECK/FOREIGN KEY trên bảng đã có phải NOT VALID, rồi VALIDATE CONSTRAINT ở câu riêng: %s", i+1, abbreviate(stmt)))
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
