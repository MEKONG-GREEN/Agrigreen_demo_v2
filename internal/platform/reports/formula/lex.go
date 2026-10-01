// Package formula là bộ tính công thức con của Excel cho mẫu báo cáo KN v7.7
// (FN-AD-04.06, NFR tương thích Excel; kn-v7.7-plan.md P3 mục 2).
//
// Công thức của một cột chạy trên MỘT dòng dữ liệu: tham chiếu viết theo chữ cái
// cột Excel kèm dấu "#" thay số dòng, vd "SUM(F#:L#)", "IF(G#=0,0,I#/G#)". Bộ nạp
// Excel (reports/formimport) đổi "SUM(F7:L7)" ở dòng dữ liệu 7 thành "SUM(F#:L#)".
//
// Tập hỗ trợ: số, chuỗi, TRUE/FALSE, + - * / ^ (mũ nguyên) & % và so sánh
// = <> < <= > >=; hàm SUM, MIN, MAX, AVERAGE, COUNT, ROUND, ROUNDUP, ROUNDDOWN,
// ABS, IF, IFERROR, AND, OR, NOT, ISBLANK, ISNUMBER; XREF("<mã mẫu>","<cột>") chỉ
// dùng trong ràng buộc của mẫu (tham chiếu liên biểu cùng xã cùng kỳ).
//
// Mọi số là decimal (shopspring/decimal) — không float (R-10): kết quả trùng
// Excel tới 15 chữ số có nghĩa (kiểm bằng bộ ca thử vàng so với excelize).
package formula

import (
	"fmt"
	"strings"
	"unicode"
)

type tokKind uint8

const (
	tEOF tokKind = iota
	tNum
	tStr
	tIdent // tên hàm, TRUE, FALSE
	tRef   // F#, $F#
	tOp    // + - * / ^ & % = <> < <= > >=
	tLParen
	tRParen
	tComma
	tColon
)

type token struct {
	kind tokKind
	text string
	pos  int
}

// SyntaxError là lỗi cú pháp công thức (vị trí tính theo ký tự, từ 0).
type SyntaxError struct {
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("công thức sai ở vị trí %d: %s", e.Pos, e.Msg)
}

func lex(src string) ([]token, error) {
	rs := []rune(src)
	var out []token
	i := 0
	for i < len(rs) {
		c := rs[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(rs) && rs[i+1] >= '0' && rs[i+1] <= '9':
			start := i
			for i < len(rs) && (rs[i] >= '0' && rs[i] <= '9' || rs[i] == '.') {
				i++
			}
			if i < len(rs) && (rs[i] == 'e' || rs[i] == 'E') {
				j := i + 1
				if j < len(rs) && (rs[j] == '+' || rs[j] == '-') {
					j++
				}
				if j < len(rs) && rs[j] >= '0' && rs[j] <= '9' {
					i = j
					for i < len(rs) && rs[i] >= '0' && rs[i] <= '9' {
						i++
					}
				}
			}
			out = append(out, token{tNum, string(rs[start:i]), start})
		case c == '"':
			start := i
			i++
			var b strings.Builder
			closed := false
			for i < len(rs) {
				if rs[i] == '"' {
					if i+1 < len(rs) && rs[i+1] == '"' { // "" là dấu nháy trong chuỗi
						b.WriteRune('"')
						i += 2
						continue
					}
					i++
					closed = true
					break
				}
				b.WriteRune(rs[i])
				i++
			}
			if !closed {
				return nil, &SyntaxError{start, "chuỗi chưa đóng nháy"}
			}
			out = append(out, token{tStr, b.String(), start})
		case c == '$' || unicode.IsLetter(c) || c == '_':
			start := i
			if c == '$' {
				i++
			}
			for i < len(rs) && (unicode.IsLetter(rs[i]) || unicode.IsDigit(rs[i]) || rs[i] == '_' || rs[i] == '.') {
				i++
			}
			word := string(rs[start:i])
			if i < len(rs) && rs[i] == '#' {
				i++
				out = append(out, token{tRef, word + "#", start})
				continue
			}
			if strings.HasPrefix(word, "$") {
				return nil, &SyntaxError{start, "tham chiếu cột phải có dạng F# (cùng dòng)"}
			}
			out = append(out, token{tIdent, word, start})
		case c == '(':
			out = append(out, token{tLParen, "(", i})
			i++
		case c == ')':
			out = append(out, token{tRParen, ")", i})
			i++
		case c == ',' || c == ';':
			out = append(out, token{tComma, ",", i})
			i++
		case c == ':':
			out = append(out, token{tColon, ":", i})
			i++
		case c == '<' || c == '>':
			if i+1 < len(rs) && (rs[i+1] == '=' || c == '<' && rs[i+1] == '>') {
				out = append(out, token{tOp, string(rs[i : i+2]), i})
				i += 2
			} else {
				out = append(out, token{tOp, string(c), i})
				i++
			}
		case strings.ContainsRune("+-*/^&%=", c):
			out = append(out, token{tOp, string(c), i})
			i++
		default:
			return nil, &SyntaxError{i, fmt.Sprintf("ký tự không hỗ trợ %q", c)}
		}
	}
	out = append(out, token{tEOF, "", len(rs)})
	return out, nil
}
