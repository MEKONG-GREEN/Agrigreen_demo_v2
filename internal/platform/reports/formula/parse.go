package formula

import (
	"fmt"
	"strings"

	"github.com/shopspring/decimal"
)

// MaxColumn là cột cuối của Excel (XFD).
const MaxColumn = 16384

// ColumnIndex đổi chữ cái cột Excel ("A", "HR") thành số thứ tự từ 1; sai → 0.
func ColumnIndex(letter string) int {
	letter = strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(letter)), "$")
	if letter == "" || len(letter) > 3 {
		return 0
	}
	n := 0
	for _, c := range letter {
		if c < 'A' || c > 'Z' {
			return 0
		}
		n = n*26 + int(c-'A'+1)
	}
	if n > MaxColumn {
		return 0
	}
	return n
}

// ColumnLetter đổi số thứ tự cột (từ 1) thành chữ cái Excel.
func ColumnLetter(n int) string {
	if n < 1 || n > MaxColumn {
		return ""
	}
	var b []byte
	for n > 0 {
		n--
		b = append([]byte{byte('A' + n%26)}, b...)
		n /= 26
	}
	return string(b)
}

// Node là một nút của cây cú pháp.
type Node interface{ node() }

// Num là hằng số.
type Num struct{ V decimal.Decimal }

// Str là hằng chuỗi.
type Str struct{ V string }

// Bool là TRUE/FALSE.
type Bool struct{ V bool }

// Ref là tham chiếu một ô cùng dòng (cột Col, từ 1).
type Ref struct{ Col int }

// Range là dải ô cùng dòng From..To (From ≤ To).
type Range struct{ From, To int }

// Unary là toán tử một ngôi: "-" , "+" hoặc "%" (hậu tố).
type Unary struct {
	Op string
	X  Node
}

// Binary là toán tử hai ngôi.
type Binary struct {
	Op   string
	L, R Node
}

// Call là lời gọi hàm (tên viết hoa).
type Call struct {
	Name string
	Args []Node
}

func (Num) node()    {}
func (Str) node()    {}
func (Bool) node()   {}
func (Ref) node()    {}
func (Range) node()  {}
func (Unary) node()  {}
func (Binary) node() {}
func (Call) node()   {}

// Hàm được hỗ trợ và số đối số (min, max; max = -1 là không giới hạn).
var funcs = map[string][2]int{
	"SUM": {1, -1}, "MIN": {1, -1}, "MAX": {1, -1}, "AVERAGE": {1, -1}, "COUNT": {1, -1},
	"ROUND": {2, 2}, "ROUNDUP": {2, 2}, "ROUNDDOWN": {2, 2}, "ABS": {1, 1},
	"IF": {2, 3}, "IFERROR": {2, 2}, "AND": {1, -1}, "OR": {1, -1}, "NOT": {1, 1},
	"ISBLANK": {1, 1}, "ISNUMBER": {1, 1},
	"XREF": {2, 2},
}

// Functions trả danh sách hàm được hỗ trợ (cho tài liệu/hợp đồng).
func Functions() []string {
	out := make([]string, 0, len(funcs))
	for k := range funcs {
		out = append(out, k)
	}
	return out
}

// Parse phân tích công thức (dấu "=" đầu tùy chọn).
func Parse(src string) (Node, error) {
	s := strings.TrimSpace(src)
	s = strings.TrimPrefix(s, "=")
	if strings.TrimSpace(s) == "" {
		return nil, &SyntaxError{0, "công thức rỗng"}
	}
	toks, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	n, err := p.comparison()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, &SyntaxError{p.peek().pos, fmt.Sprintf("thừa %q", p.peek().text)}
	}
	return n, nil
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }
func (p *parser) next() token { t := p.toks[p.i]; p.i++; return t }

func (p *parser) isOp(ops ...string) bool {
	t := p.peek()
	if t.kind != tOp {
		return false
	}
	for _, o := range ops {
		if t.text == o {
			return true
		}
	}
	return false
}

func (p *parser) comparison() (Node, error) {
	l, err := p.concat()
	if err != nil {
		return nil, err
	}
	for p.isOp("=", "<>", "<", "<=", ">", ">=") {
		op := p.next().text
		r, err := p.concat()
		if err != nil {
			return nil, err
		}
		l = Binary{op, l, r}
	}
	return l, nil
}

func (p *parser) concat() (Node, error) {
	l, err := p.additive()
	if err != nil {
		return nil, err
	}
	for p.isOp("&") {
		p.next()
		r, err := p.additive()
		if err != nil {
			return nil, err
		}
		l = Binary{"&", l, r}
	}
	return l, nil
}

func (p *parser) additive() (Node, error) {
	l, err := p.multiplicative()
	if err != nil {
		return nil, err
	}
	for p.isOp("+", "-") {
		op := p.next().text
		r, err := p.multiplicative()
		if err != nil {
			return nil, err
		}
		l = Binary{op, l, r}
	}
	return l, nil
}

func (p *parser) multiplicative() (Node, error) {
	l, err := p.power()
	if err != nil {
		return nil, err
	}
	for p.isOp("*", "/") {
		op := p.next().text
		r, err := p.power()
		if err != nil {
			return nil, err
		}
		l = Binary{op, l, r}
	}
	return l, nil
}

// power: "^" kết hợp trái như Excel (2^3^2 = 64).
func (p *parser) power() (Node, error) {
	l, err := p.unary()
	if err != nil {
		return nil, err
	}
	for p.isOp("^") {
		p.next()
		r, err := p.unary()
		if err != nil {
			return nil, err
		}
		l = Binary{"^", l, r}
	}
	return l, nil
}

// unary: dấu âm ưu tiên cao hơn "^" như Excel (-2^2 = 4).
func (p *parser) unary() (Node, error) {
	if p.isOp("-", "+") {
		op := p.next().text
		x, err := p.unary()
		if err != nil {
			return nil, err
		}
		return Unary{op, x}, nil
	}
	return p.postfix()
}

func (p *parser) postfix() (Node, error) {
	x, err := p.primary()
	if err != nil {
		return nil, err
	}
	for p.isOp("%") {
		p.next()
		x = Unary{"%", x}
	}
	return x, nil
}

func (p *parser) primary() (Node, error) {
	t := p.next()
	switch t.kind {
	case tNum:
		d, err := decimal.NewFromString(t.text)
		if err != nil {
			return nil, &SyntaxError{t.pos, "số không hợp lệ " + t.text}
		}
		return Num{d}, nil
	case tStr:
		return Str{t.text}, nil
	case tRef:
		col := ColumnIndex(strings.TrimSuffix(t.text, "#"))
		if col == 0 {
			return nil, &SyntaxError{t.pos, "chữ cái cột không hợp lệ " + t.text}
		}
		if p.peek().kind == tColon {
			p.next()
			t2 := p.next()
			if t2.kind != tRef {
				return nil, &SyntaxError{t2.pos, "dải ô phải có dạng F#:L#"}
			}
			col2 := ColumnIndex(strings.TrimSuffix(t2.text, "#"))
			if col2 == 0 {
				return nil, &SyntaxError{t2.pos, "chữ cái cột không hợp lệ " + t2.text}
			}
			if col2 < col {
				col, col2 = col2, col
			}
			return Range{col, col2}, nil
		}
		return Ref{col}, nil
	case tIdent:
		name := strings.ToUpper(t.text)
		if p.peek().kind != tLParen {
			switch name {
			case "TRUE":
				return Bool{true}, nil
			case "FALSE":
				return Bool{false}, nil
			}
			return nil, &SyntaxError{t.pos, fmt.Sprintf("%q không phải tham chiếu cùng dòng (viết F#) hay hàm", t.text)}
		}
		arity, ok := funcs[name]
		if !ok {
			return nil, &SyntaxError{t.pos, "hàm không hỗ trợ " + name}
		}
		p.next() // (
		var args []Node
		if p.peek().kind != tRParen {
			for {
				a, err := p.comparison()
				if err != nil {
					return nil, err
				}
				args = append(args, a)
				if p.peek().kind == tComma {
					p.next()
					continue
				}
				break
			}
		}
		if r := p.next(); r.kind != tRParen {
			return nil, &SyntaxError{r.pos, "thiếu dấu )"}
		}
		if len(args) < arity[0] || arity[1] >= 0 && len(args) > arity[1] {
			return nil, &SyntaxError{t.pos, fmt.Sprintf("%s nhận sai số đối số (%d)", name, len(args))}
		}
		if name == "XREF" {
			for _, a := range args {
				if _, ok := a.(Str); !ok {
					return nil, &SyntaxError{t.pos, `XREF nhận hai chuỗi: XREF("T01","M")`}
				}
			}
		}
		return Call{name, args}, nil
	case tLParen:
		x, err := p.comparison()
		if err != nil {
			return nil, err
		}
		if r := p.next(); r.kind != tRParen {
			return nil, &SyntaxError{r.pos, "thiếu dấu )"}
		}
		return x, nil
	case tEOF:
		return nil, &SyntaxError{t.pos, "công thức kết thúc đột ngột"}
	}
	return nil, &SyntaxError{t.pos, fmt.Sprintf("không mong đợi %q", t.text)}
}

// Refs trả các cột (từ 1) mà công thức tham chiếu, kể cả mọi cột trong dải; không trùng, tăng dần.
func Refs(n Node) []int {
	set := map[int]bool{}
	walk(n, func(x Node) {
		switch v := x.(type) {
		case Ref:
			set[v.Col] = true
		case Range:
			for c := v.From; c <= v.To; c++ {
				set[c] = true
			}
		}
	})
	out := make([]int, 0, len(set))
	for c := 1; len(out) < len(set); c++ {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

// UsesXRef cho biết công thức có XREF (chỉ được dùng trong ràng buộc, không trong cột).
func UsesXRef(n Node) bool {
	found := false
	walk(n, func(x Node) {
		if c, ok := x.(Call); ok && c.Name == "XREF" {
			found = true
		}
	})
	return found
}

// XRefs trả các cặp (mã mẫu, cột) mà công thức tham chiếu liên biểu.
func XRefs(n Node) [][2]string {
	var out [][2]string
	walk(n, func(x Node) {
		if c, ok := x.(Call); ok && c.Name == "XREF" && len(c.Args) == 2 {
			out = append(out, [2]string{c.Args[0].(Str).V, strings.ToUpper(c.Args[1].(Str).V)})
		}
	})
	return out
}

func walk(n Node, fn func(Node)) {
	fn(n)
	switch v := n.(type) {
	case Unary:
		walk(v.X, fn)
	case Binary:
		walk(v.L, fn)
		walk(v.R, fn)
	case Call:
		for _, a := range v.Args {
			walk(a, fn)
		}
	}
}
