package formula

import (
	"strings"

	"github.com/shopspring/decimal"
)

// Kind là loại giá trị.
type Kind uint8

// Loại giá trị theo Excel.
const (
	Blank Kind = iota
	Number
	Text
	Boolean
	Error
)

// Mã lỗi Excel.
const (
	ErrDiv0  = "#DIV/0!"
	ErrValue = "#VALUE!"
	ErrNum   = "#NUM!"
	ErrRef   = "#REF!"
	ErrNA    = "#N/A"
)

// DivPrecision là số chữ số thập phân giữ lại khi chia (Excel giữ 15 chữ số có nghĩa).
const DivPrecision = 20

// Value là giá trị một ô hay kết quả một biểu thức.
type Value struct {
	Kind Kind
	Num  decimal.Decimal
	Str  string
	Bool bool
	Err  string
}

// Giá trị dựng sẵn.
func NumberValue(d decimal.Decimal) Value { return Value{Kind: Number, Num: d} }
func TextValue(s string) Value            { return Value{Kind: Text, Str: s} }
func BoolValue(b bool) Value              { return Value{Kind: Boolean, Bool: b} }
func ErrorValue(code string) Value        { return Value{Kind: Error, Err: code} }

// String in giá trị như ô lưới lưu: số dạng thập phân chuẩn, TRUE/FALSE, mã lỗi.
func (v Value) String() string {
	switch v.Kind {
	case Number:
		return v.Num.String()
	case Text:
		return v.Str
	case Boolean:
		if v.Bool {
			return "TRUE"
		}
		return "FALSE"
	case Error:
		return v.Err
	}
	return ""
}

// Env cung cấp giá trị ô cùng dòng và tham chiếu liên biểu.
type Env interface {
	// Cell trả giá trị ô ở cột col (từ 1) của dòng đang tính; không có → Blank.
	Cell(col int) Value
	// XRef trả giá trị cột letter của mẫu form cùng xã cùng kỳ; không có → Blank.
	XRef(form, letter string) Value
}

// Eval tính cây cú pháp trên một dòng.
func Eval(n Node, env Env) Value {
	switch v := n.(type) {
	case Num:
		return NumberValue(v.V)
	case Str:
		return TextValue(v.V)
	case Bool:
		return BoolValue(v.V)
	case Ref:
		return env.Cell(v.Col)
	case Range:
		if v.From == v.To {
			return env.Cell(v.From)
		}
		return ErrorValue(ErrValue) // dải trong ngữ cảnh một giá trị
	case Unary:
		x := Eval(v.X, env)
		if x.Kind == Error {
			return x
		}
		d, e := toNumber(x)
		if e != "" {
			return ErrorValue(e)
		}
		switch v.Op {
		case "-":
			return NumberValue(d.Neg())
		case "%":
			return NumberValue(d.DivRound(decimal.NewFromInt(100), DivPrecision))
		}
		return NumberValue(d)
	case Binary:
		return evalBinary(v, env)
	case Call:
		return evalCall(v, env)
	}
	return ErrorValue(ErrValue)
}

func evalBinary(v Binary, env Env) Value {
	l := Eval(v.L, env)
	if l.Kind == Error {
		return l
	}
	r := Eval(v.R, env)
	if r.Kind == Error {
		return r
	}
	switch v.Op {
	case "&":
		return TextValue(toText(l) + toText(r))
	case "=", "<>", "<", "<=", ">", ">=":
		c := compare(l, r)
		switch v.Op {
		case "=":
			return BoolValue(c == 0)
		case "<>":
			return BoolValue(c != 0)
		case "<":
			return BoolValue(c < 0)
		case "<=":
			return BoolValue(c <= 0)
		case ">":
			return BoolValue(c > 0)
		}
		return BoolValue(c >= 0)
	}
	a, e := toNumber(l)
	if e != "" {
		return ErrorValue(e)
	}
	b, e := toNumber(r)
	if e != "" {
		return ErrorValue(e)
	}
	switch v.Op {
	case "+":
		return NumberValue(a.Add(b))
	case "-":
		return NumberValue(a.Sub(b))
	case "*":
		return NumberValue(a.Mul(b))
	case "/":
		if b.IsZero() {
			return ErrorValue(ErrDiv0)
		}
		return NumberValue(a.DivRound(b, DivPrecision))
	case "^":
		if !b.Equal(b.Truncate(0)) || b.Abs().GreaterThan(decimal.NewFromInt(64)) {
			return ErrorValue(ErrNum) // chỉ hỗ trợ mũ nguyên |n| ≤ 64 (decimal, không float)
		}
		if a.IsZero() && b.Sign() <= 0 {
			if b.IsZero() {
				return ErrorValue(ErrNum)
			}
			return ErrorValue(ErrDiv0)
		}
		n := b.IntPart()
		res := decimal.NewFromInt(1)
		for i := int64(0); i < absInt(n); i++ {
			res = res.Mul(a)
		}
		if n < 0 {
			res = decimal.NewFromInt(1).DivRound(res, DivPrecision)
		}
		return NumberValue(res)
	}
	return ErrorValue(ErrValue)
}

func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// toNumber ép giá trị sang số theo Excel: ô trống = 0, TRUE = 1, chuỗi số được đọc.
func toNumber(v Value) (decimal.Decimal, string) {
	switch v.Kind {
	case Blank:
		return decimal.Zero, ""
	case Number:
		return v.Num, ""
	case Boolean:
		if v.Bool {
			return decimal.NewFromInt(1), ""
		}
		return decimal.Zero, ""
	case Text:
		d, err := decimal.NewFromString(strings.TrimSpace(v.Str))
		if err != nil {
			return decimal.Zero, ErrValue
		}
		return d, ""
	}
	return decimal.Zero, v.Err
}

func toText(v Value) string {
	if v.Kind == Blank {
		return ""
	}
	return v.String()
}

// toBool ép điều kiện của IF/AND/OR theo Excel.
func toBool(v Value) (bool, string) {
	switch v.Kind {
	case Blank:
		return false, ""
	case Boolean:
		return v.Bool, ""
	case Number:
		return !v.Num.IsZero(), ""
	case Text:
		switch strings.ToUpper(strings.TrimSpace(v.Str)) {
		case "TRUE":
			return true, ""
		case "FALSE":
			return false, ""
		}
		return false, ErrValue
	}
	return false, v.Err
}

// compare so sánh theo Excel: số < chuỗi < logic; ô trống bằng 0 hoặc "" hoặc FALSE
// tùy vế kia; chuỗi không phân biệt hoa thường.
func compare(a, b Value) int {
	if a.Kind == Blank {
		a = blankLike(b)
	}
	if b.Kind == Blank {
		b = blankLike(a)
	}
	rank := func(v Value) int {
		switch v.Kind {
		case Number:
			return 0
		case Text:
			return 1
		}
		return 2
	}
	if ra, rb := rank(a), rank(b); ra != rb {
		return ra - rb
	}
	switch a.Kind {
	case Number:
		return a.Num.Cmp(b.Num)
	case Text:
		return strings.Compare(strings.ToLower(a.Str), strings.ToLower(b.Str))
	}
	switch {
	case a.Bool == b.Bool:
		return 0
	case !a.Bool:
		return -1
	}
	return 1
}

func blankLike(other Value) Value {
	switch other.Kind {
	case Text:
		return TextValue("")
	case Boolean:
		return BoolValue(false)
	}
	return NumberValue(decimal.Zero)
}

// numbersOf gom số cho hàm tổng hợp theo Excel: đối số trực tiếp được ép kiểu
// (chuỗi không phải số → #VALUE!), ô/dải chỉ lấy ô số (bỏ trống, chuỗi, logic).
func numbersOf(args []Node, env Env) ([]decimal.Decimal, string) {
	var out []decimal.Decimal
	for _, a := range args {
		switch r := a.(type) {
		case Range, Ref:
			from, to := 0, 0
			if rr, ok := r.(Range); ok {
				from, to = rr.From, rr.To
			} else {
				from, to = r.(Ref).Col, r.(Ref).Col
			}
			for c := from; c <= to; c++ {
				v := env.Cell(c)
				switch v.Kind {
				case Error:
					return nil, v.Err
				case Number:
					out = append(out, v.Num)
				}
			}
		default:
			v := Eval(a, env)
			if v.Kind == Error {
				return nil, v.Err
			}
			if v.Kind == Blank {
				continue
			}
			d, e := toNumber(v)
			if e != "" {
				return nil, e
			}
			out = append(out, d)
		}
	}
	return out, ""
}

func evalCall(c Call, env Env) Value {
	switch c.Name {
	case "SUM", "MIN", "MAX", "AVERAGE", "COUNT":
		nums, e := numbersOf(c.Args, env)
		if e != "" {
			if c.Name == "COUNT" {
				return NumberValue(decimal.Zero)
			}
			return ErrorValue(e)
		}
		switch c.Name {
		case "COUNT":
			return NumberValue(decimal.NewFromInt(int64(len(nums))))
		case "SUM":
			s := decimal.Zero
			for _, d := range nums {
				s = s.Add(d)
			}
			return NumberValue(s)
		case "AVERAGE":
			if len(nums) == 0 {
				return ErrorValue(ErrDiv0)
			}
			s := decimal.Zero
			for _, d := range nums {
				s = s.Add(d)
			}
			return NumberValue(s.DivRound(decimal.NewFromInt(int64(len(nums))), DivPrecision))
		}
		if len(nums) == 0 {
			return NumberValue(decimal.Zero)
		}
		m := nums[0]
		for _, d := range nums[1:] {
			if c.Name == "MIN" && d.LessThan(m) || c.Name == "MAX" && d.GreaterThan(m) {
				m = d
			}
		}
		return NumberValue(m)
	case "ROUND", "ROUNDUP", "ROUNDDOWN":
		x, e := numArg(c.Args[0], env)
		if e != "" {
			return ErrorValue(e)
		}
		p, e := numArg(c.Args[1], env)
		if e != "" {
			return ErrorValue(e)
		}
		places := int32(p.Truncate(0).IntPart())
		if places > 30 || places < -30 {
			return ErrorValue(ErrNum)
		}
		switch c.Name {
		case "ROUND": // nửa xa số 0, như Excel
			return NumberValue(x.Round(places))
		case "ROUNDUP": // xa số 0
			if x.Sign() < 0 {
				return NumberValue(x.RoundFloor(places))
			}
			return NumberValue(x.RoundCeil(places))
		}
		// ROUNDDOWN: về phía 0 (Truncate chỉ nhận số chữ số ≥ 0 → dời dấu phẩy).
		return NumberValue(x.Shift(places).Truncate(0).Shift(-places))
	case "ABS":
		x, e := numArg(c.Args[0], env)
		if e != "" {
			return ErrorValue(e)
		}
		return NumberValue(x.Abs())
	case "IF":
		cond := Eval(c.Args[0], env)
		if cond.Kind == Error {
			return cond
		}
		b, e := toBool(cond)
		if e != "" {
			return ErrorValue(e)
		}
		if b {
			return orZero(Eval(c.Args[1], env))
		}
		if len(c.Args) == 3 {
			return orZero(Eval(c.Args[2], env))
		}
		return BoolValue(false)
	case "IFERROR":
		v := Eval(c.Args[0], env)
		if v.Kind == Error {
			return orZero(Eval(c.Args[1], env))
		}
		return orZero(v)
	case "AND", "OR":
		res := c.Name == "AND"
		for _, a := range c.Args {
			v := Eval(a, env)
			if v.Kind == Error {
				return v
			}
			b, e := toBool(v)
			if e != "" {
				return ErrorValue(e)
			}
			if c.Name == "AND" {
				res = res && b
			} else {
				res = res || b
			}
		}
		return BoolValue(res)
	case "NOT":
		v := Eval(c.Args[0], env)
		if v.Kind == Error {
			return v
		}
		b, e := toBool(v)
		if e != "" {
			return ErrorValue(e)
		}
		return BoolValue(!b)
	case "ISBLANK":
		return BoolValue(Eval(c.Args[0], env).Kind == Blank)
	case "ISNUMBER":
		return BoolValue(Eval(c.Args[0], env).Kind == Number)
	case "XREF":
		return env.XRef(c.Args[0].(Str).V, strings.ToUpper(c.Args[1].(Str).V))
	}
	return ErrorValue(ErrValue)
}

// orZero: ô trống trả về từ IF/IFERROR hiển thị 0 trong Excel.
func orZero(v Value) Value {
	if v.Kind == Blank {
		return NumberValue(decimal.Zero)
	}
	return v
}

func numArg(n Node, env Env) (decimal.Decimal, string) {
	v := Eval(n, env)
	if v.Kind == Error {
		return decimal.Zero, v.Err
	}
	return toNumber(v)
}

// Program là công thức đã phân tích, dùng lại cho nhiều dòng.
type Program struct {
	Src  string
	Root Node
}

// Compile phân tích một lần.
func Compile(src string) (*Program, error) {
	n, err := Parse(src)
	if err != nil {
		return nil, err
	}
	return &Program{Src: Format(n), Root: n}, nil
}

// Run tính chương trình trên một dòng.
func (p *Program) Run(env Env) Value { return Eval(p.Root, env) }
