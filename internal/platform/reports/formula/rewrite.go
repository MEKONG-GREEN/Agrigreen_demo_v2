package formula

import (
	"errors"
	"strconv"
	"strings"
)

// ErrRefDeleted: công thức tham chiếu cột bị xóa (Excel sẽ thành #REF!) — trình
// thiết kế chặn thay vì để công thức hỏng (reports.formula_invalid_ref).
var ErrRefDeleted = errors.New("formula: tham chiếu cột đã bị xóa")

// Format in cây cú pháp thành công thức chuẩn (không có dấu "=" đầu).
func Format(n Node) string {
	var b strings.Builder
	format(&b, n, 0)
	return b.String()
}

// Mức ưu tiên (cao hơn = buộc chặt hơn).
func prec(n Node) int {
	switch v := n.(type) {
	case Binary:
		switch v.Op {
		case "=", "<>", "<", "<=", ">", ">=":
			return 1
		case "&":
			return 2
		case "+", "-":
			return 3
		case "*", "/":
			return 4
		case "^":
			return 5
		}
	case Unary:
		if v.Op == "%" {
			return 7
		}
		return 6
	}
	return 8
}

func format(b *strings.Builder, n Node, parent int) {
	p := prec(n)
	if p < parent {
		b.WriteByte('(')
		defer b.WriteByte(')')
	}
	switch v := n.(type) {
	case Num:
		b.WriteString(v.V.String())
	case Str:
		b.WriteString(`"` + strings.ReplaceAll(v.V, `"`, `""`) + `"`)
	case Bool:
		if v.V {
			b.WriteString("TRUE")
		} else {
			b.WriteString("FALSE")
		}
	case Ref:
		b.WriteString(ColumnLetter(v.Col) + "#")
	case Range:
		b.WriteString(ColumnLetter(v.From) + "#:" + ColumnLetter(v.To) + "#")
	case Unary:
		if v.Op == "%" {
			format(b, v.X, p)
			b.WriteByte('%')
		} else {
			b.WriteString(v.Op)
			format(b, v.X, p)
		}
	case Binary:
		format(b, v.L, p)
		b.WriteString(v.Op)
		// Toán tử kết hợp trái: vế phải cùng mức cần ngoặc (a-(b-c)).
		format(b, v.R, p+1)
	case Call:
		b.WriteString(v.Name + "(")
		for i, a := range v.Args {
			if i > 0 {
				b.WriteByte(',')
			}
			format(b, a, 0)
		}
		b.WriteByte(')')
	}
}

// ToExcel in công thức cho một dòng cụ thể của tệp Excel: "SUM(F#:L#)" ở dòng 7
// → "SUM(F7:L7)". Dùng khi xuất mẫu Excel.
func ToExcel(n Node, row int) string {
	return strings.ReplaceAll(Format(n), "#", strconv.Itoa(row))
}

// Shift viết lại tham chiếu khi chèn (delta > 0) hoặc xóa (delta < 0, một cột)
// cột ở vị trí at (từ 1), theo đúng cách Excel dời công thức:
//   - chèn tại at: mọi cột ≥ at dời +delta; dải chứa at nở ra;
//   - xóa cột at: tham chiếu đúng cột đó → ErrRefDeleted; dải chứa at co lại;
//     dải chỉ gồm cột đó → ErrRefDeleted.
func Shift(n Node, at, delta int) (Node, error) {
	switch v := n.(type) {
	case Ref:
		c, err := shiftCol(v.Col, at, delta)
		return Ref{c}, err
	case Range:
		if delta < 0 {
			if v.From == at && v.To == at {
				return nil, ErrRefDeleted
			}
			from, to := v.From, v.To
			if from > at {
				from--
			}
			if to >= at {
				to--
			}
			return Range{from, to}, nil
		}
		from, to := v.From, v.To
		if from >= at {
			from += delta
		}
		if to >= at {
			to += delta
		}
		return Range{from, to}, nil
	case Unary:
		x, err := Shift(v.X, at, delta)
		return Unary{v.Op, x}, err
	case Binary:
		l, err := Shift(v.L, at, delta)
		if err != nil {
			return nil, err
		}
		r, err := Shift(v.R, at, delta)
		return Binary{v.Op, l, r}, err
	case Call:
		if v.Name == "XREF" { // cột của mẫu khác: không dời
			return v, nil
		}
		args := make([]Node, len(v.Args))
		for i, a := range v.Args {
			x, err := Shift(a, at, delta)
			if err != nil {
				return nil, err
			}
			args[i] = x
		}
		return Call{v.Name, args}, nil
	}
	return n, nil
}

func shiftCol(c, at, delta int) (int, error) {
	if delta < 0 {
		if c == at {
			return 0, ErrRefDeleted
		}
		if c > at {
			return c - 1, nil
		}
		return c, nil
	}
	if c >= at {
		return c + delta, nil
	}
	return c, nil
}

// ShiftString là Shift trên chuỗi công thức; trả công thức đã chuẩn hóa.
func ShiftString(src string, at, delta int) (string, error) {
	n, err := Parse(src)
	if err != nil {
		return "", err
	}
	n, err = Shift(n, at, delta)
	if err != nil {
		return "", err
	}
	return Format(n), nil
}
