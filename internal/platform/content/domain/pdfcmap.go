package domain

import (
	"encoding/hex"
	"io"
	"strings"
	"unicode/utf16"

	"github.com/ledongthuc/pdf"
)

// toUnicodeMap là bảng ToUnicode (PDF 32000-1 §9.10.3) tự đọc: thư viện chỉ
// tăng byte cuối của đích trong bfrange (đúng chữ của đặc tả), nên dải
// "<0000> <FFFF> <0000>" mà nhiều bộ sinh PDF (fpdf, tFPDF…) dùng cho phông
// Identity-H bị cắt còn 8 bit — "ả" (U+1EA3) thành "£" (U+00A3). Ở đây đích
// được cộng như một số nguyên, giống Poppler/MuPDF.
type toUnicodeMap struct {
	n      int // số byte mỗi mã (từ codespacerange)
	single map[uint32]string
	ranges []cmapRange
}

type cmapRange struct {
	lo, hi uint32
	dst    []uint16 // UTF-16BE của đích đầu dải
	arr    []string // dạng mảng [<d1> <d2> ...]
}

const maxCMapBytes = 1 << 20

// fontEncoder chọn bộ giải mã cho phông: ToUnicode tự đọc nếu có, nếu không thì bộ của thư viện.
func fontEncoder(f pdf.Font) pdf.TextEncoding {
	tu := f.V.Key("ToUnicode")
	if tu.Kind() == pdf.Stream {
		n := 1
		if f.V.Key("Subtype").Name() == "Type0" {
			n = 2
		}
		rc := tu.Reader()
		b, err := io.ReadAll(io.LimitReader(rc, maxCMapBytes))
		_ = rc.Close()
		if err == nil {
			if m := parseToUnicode(string(b), n); m != nil {
				return m
			}
		}
	}
	return f.Encoder()
}

func hexBytes(tok string) ([]byte, bool) {
	if len(tok) < 2 || tok[0] != '<' || tok[len(tok)-1] != '>' {
		return nil, false
	}
	h := strings.Join(strings.Fields(tok[1:len(tok)-1]), "")
	if len(h)%2 == 1 {
		h += "0"
	}
	b, err := hex.DecodeString(h)
	return b, err == nil
}

func codeOf(b []byte) uint32 {
	var v uint32
	for _, x := range b {
		v = v<<8 | uint32(x)
	}
	return v
}

func utf16Units(b []byte) []uint16 {
	u := make([]uint16, 0, len(b)/2)
	for i := 0; i+1 < len(b); i += 2 {
		u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
	}
	return u
}

// cmapTokens tách chuỗi hex <...>, mảng [ ] và từ khóa.
func cmapTokens(s string) []string {
	var out []string
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case '<':
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				return out
			}
			out = append(out, s[i:i+j+1])
			i += j + 1
		case '[', ']':
			out = append(out, string(c))
			i++
		case '%':
			for i < len(s) && s[i] != '\n' && s[i] != '\r' {
				i++
			}
		case ' ', '\n', '\r', '\t', '\f':
			i++
		default:
			j := i
			for j < len(s) && !strings.ContainsRune(" \n\r\t\f<[]%", rune(s[j])) {
				j++
			}
			out = append(out, s[i:j])
			i = j
		}
	}
	return out
}

func parseToUnicode(s string, n int) *toUnicodeMap {
	m := &toUnicodeMap{n: n, single: map[uint32]string{}}
	toks := cmapTokens(s)
	for i := 0; i < len(toks); i++ {
		switch toks[i] {
		case "begincodespacerange":
			if i+1 < len(toks) {
				if b, ok := hexBytes(toks[i+1]); ok && len(b) > 0 && len(b) <= 4 {
					m.n = len(b)
				}
			}
		case "beginbfchar":
			for i++; i+1 < len(toks) && toks[i] != "endbfchar"; i += 2 {
				src, ok1 := hexBytes(toks[i])
				dst, ok2 := hexBytes(toks[i+1])
				if ok1 && ok2 {
					m.single[codeOf(src)] = string(utf16.Decode(utf16Units(dst)))
				}
			}
		case "beginbfrange":
			for i++; i+2 < len(toks) && toks[i] != "endbfrange"; {
				lo, ok1 := hexBytes(toks[i])
				hi, ok2 := hexBytes(toks[i+1])
				if !ok1 || !ok2 {
					i++
					continue
				}
				r := cmapRange{lo: codeOf(lo), hi: codeOf(hi)}
				if toks[i+2] == "[" {
					j := i + 3
					for ; j < len(toks) && toks[j] != "]"; j++ {
						if d, ok := hexBytes(toks[j]); ok {
							r.arr = append(r.arr, string(utf16.Decode(utf16Units(d))))
						}
					}
					i = j + 1
				} else {
					d, _ := hexBytes(toks[i+2])
					r.dst = utf16Units(d)
					i += 3
				}
				if r.hi >= r.lo {
					m.ranges = append(m.ranges, r)
				}
			}
		}
	}
	if len(m.single) == 0 && len(m.ranges) == 0 {
		return nil
	}
	return m
}

func (m *toUnicodeMap) lookup(code uint32) (string, bool) {
	if s, ok := m.single[code]; ok {
		return s, true
	}
	var best *cmapRange // dải hẹp nhất chứa mã (dải chồng nhau: dải cụ thể hơn thắng)
	for i := range m.ranges {
		r := &m.ranges[i]
		if code >= r.lo && code <= r.hi && (best == nil || r.hi-r.lo < best.hi-best.lo) {
			best = r
		}
	}
	if r := best; r != nil {
		off := code - r.lo
		if r.arr != nil {
			if int(off) < len(r.arr) {
				return r.arr[off], true
			}
			return "", false
		}
		if len(r.dst) == 0 {
			return "", false
		}
		u := append([]uint16(nil), r.dst...)
		if len(u) == 1 {
			return string(rune(uint32(u[0]) + off)), true
		}
		u[len(u)-1] += uint16(off) // đích nhiều đơn vị (vd cặp thay thế): cộng đơn vị cuối
		return string(utf16.Decode(u)), true
	}
	return "", false
}

// Decode thực hiện pdf.TextEncoding.
func (m *toUnicodeMap) Decode(raw string) string {
	var b strings.Builder
	for i := 0; i+m.n <= len(raw); i += m.n {
		code := codeOf([]byte(raw[i : i+m.n]))
		if s, ok := m.lookup(code); ok {
			b.WriteString(s)
		} else {
			b.WriteRune('�')
		}
	}
	return b.String()
}
