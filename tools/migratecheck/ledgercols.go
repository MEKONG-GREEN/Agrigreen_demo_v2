package main

import (
	"fmt"
	"regexp"
	"strings"
)

// ledgerColumnsGrandfathered: tệp đã thêm cột vào sổ trước quy tắc CheckLedgerColumns — cột của chúng
// được đăng ký bù trong shared 000011 (R-24: tệp đã áp không sửa). Danh sách ĐÓNG.
var ledgerColumnsGrandfathered = map[string]map[int]bool{
	"pf":  set(79),
	"mkg": set(6),
}

var (
	applyLedgerRe = regexp.MustCompile(`(?i)\bapply_ledger\s*\(\s*'([a-z0-9_.]+)'`)
	registerColRe = regexp.MustCompile(`(?is)\bregister_ledger_columns\s*\(\s*'([a-z0-9_.]+)'(?:\s*::\s*regclass)?\s*,\s*ARRAY\s*\[([^\]]*)\]`)
	quotedNameRe  = regexp.MustCompile(`'([a-z0-9_]+)'`)
	addColumnRe   = regexp.MustCompile(`(?is)^ADD\s+(?:COLUMN\s+)?(?:IF\s+NOT\s+EXISTS\s+)?([a-z0-9_]+)\b(.*)$`)
	dropColumnRe  = regexp.MustCompile(`(?is)^DROP\s+(?:COLUMN\s+)?(?:IF\s+EXISTS\s+)?([a-z0-9_]+)\b`)
	alterTypeRe   = regexp.MustCompile(`(?is)^ALTER\s+(?:COLUMN\s+)?[a-z0-9_]+\s+(?:SET\s+DATA\s+)?TYPE\b`)
	renameColRe   = regexp.MustCompile(`(?is)^RENAME\s+(?:COLUMN\s+)?([a-z0-9_]+)\s+TO\b`)
	badDefaultRe  = regexp.MustCompile(`(?is)\b(DEFAULT|GENERATED|NOT\s+NULL)\b`)
)

// Từ khóa theo sau ADD/DROP không phải tên cột.
var notColumn = map[string]bool{"constraint": true, "primary": true, "unique": true, "check": true, "foreign": true, "exclude": true}

// LedgerTables trả tên các bảng sổ khai báo qua shared.apply_ledger('<bảng>', …) trong một tệp.
func LedgerTables(src string) []string {
	var out []string
	for _, m := range applyLedgerRe.FindAllStringSubmatch(src, -1) {
		out = append(out, strings.ToLower(m[1]))
	}
	return out
}

// CheckLedgerColumns (ADR-0012, shared 000011): row_hash băm cả dòng, nên đổi tập cột của sổ làm dòng cũ
// băm lại khác. Trên bảng sổ (ledgers):
//
//   - ADD COLUMN phải NULL được, không DEFAULT / NOT NULL / GENERATED, và được đăng ký trong cùng tệp bằng
//     SELECT shared.register_ledger_columns('<bảng>', ARRAY['cột', …]) — verify_chain bỏ khóa null của
//     đợt này khi băm lại dòng có trước;
//   - không DROP COLUMN, RENAME COLUMN, ALTER COLUMN … TYPE (đổi khóa/biểu diễn của dòng đã băm).
//
// ALTER trong khối DO (EXECUTE động) không thấy được — cũng như các quy tắc khác.
func CheckLedgerColumns(src string, ledgers map[string]bool) []string {
	registered := map[string]bool{}
	for _, m := range registerColRe.FindAllStringSubmatch(src, -1) {
		for _, c := range quotedNameRe.FindAllStringSubmatch(m[2], -1) {
			registered[strings.ToLower(m[1])+"."+strings.ToLower(c[1])] = true
		}
	}
	var out []string
	for i, stmt := range Statements(src) {
		m := alterTableRe.FindStringSubmatchIndex(stmt)
		if m == nil {
			continue
		}
		table := strings.ToLower(stmt[m[2]:m[3]])
		if !ledgers[table] {
			continue
		}
		for _, clause := range splitTopLevel(stmt[m[1]:]) {
			bad := func(msg string) {
				out = append(out, fmt.Sprintf("câu lệnh %d: ADR-0012: sổ %s: %s: %s", i+1, table, msg, abbreviate(stmt)))
			}
			switch {
			case addColumnRe.MatchString(clause):
				am := addColumnRe.FindStringSubmatch(clause)
				col := strings.ToLower(am[1])
				if notColumn[col] {
					continue
				}
				if badDefaultRe.MatchString(stripParens(am[2])) {
					bad("cột thêm " + col + " phải NULL được, không DEFAULT/NOT NULL/GENERATED (dòng cũ sẽ hiện giá trị chưa từng ghi)")
				}
				if !registered[table+"."+col] {
					bad("cột thêm " + col + " phải đăng ký trong cùng tệp: SELECT shared.register_ledger_columns('" + table + "', ARRAY['" + col + "', …])")
				}
			case dropColumnRe.MatchString(clause) && !notColumn[strings.ToLower(dropColumnRe.FindStringSubmatch(clause)[1])]:
				bad("không được DROP COLUMN (làm đứt chuỗi băm của dòng đã ghi)")
			case alterTypeRe.MatchString(clause):
				bad("không được đổi kiểu cột (làm đứt chuỗi băm của dòng đã ghi)")
			case renameColRe.MatchString(clause):
				bad("không được đổi tên cột (làm đứt chuỗi băm của dòng đã ghi)")
			}
		}
	}
	return out
}

// splitTopLevel tách phần sau "ALTER TABLE <tên>" thành các mệnh đề theo dấu phẩy ngoài ngoặc.
func splitTopLevel(s string) []string {
	var out []string
	depth, start := 0, 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	return append(out, strings.TrimSpace(s[start:]))
}

// stripParens bỏ nội dung trong ngoặc (vd CHECK (x IS NOT NULL)) để không bắt nhầm từ khóa.
func stripParens(s string) string {
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '(':
			depth++
		case r == ')':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}
