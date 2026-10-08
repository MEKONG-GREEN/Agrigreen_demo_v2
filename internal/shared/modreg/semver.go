package modreg

import (
	"fmt"
	"strconv"
	"strings"
)

// Version là phiên bản ngữ nghĩa MAJOR.MINOR.PATCH của module (R-MOD-06).
// Không hỗ trợ hậu tố tiền phát hành: module trong repo phát hành cùng nhị phân.
type Version struct{ Major, Minor, Patch int }

func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Compare trả -1, 0, 1.
func (v Version) Compare(o Version) int {
	for _, d := range [3]int{v.Major - o.Major, v.Minor - o.Minor, v.Patch - o.Patch} {
		if d < 0 {
			return -1
		}
		if d > 0 {
			return 1
		}
	}
	return 0
}

// ParseVersion đọc "1.2.3" (đủ ba thành phần).
func ParseVersion(s string) (Version, error) {
	v, n, err := parseParts(s)
	if err != nil {
		return Version{}, err
	}
	if n != 3 {
		return Version{}, fmt.Errorf("phiên bản %q phải có dạng MAJOR.MINOR.PATCH", s)
	}
	return v, nil
}

// parseParts đọc 1–3 thành phần số; thành phần thiếu là 0. Trả số thành phần đã đọc.
func parseParts(s string) (Version, int, error) {
	parts := strings.Split(s, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return Version{}, 0, fmt.Errorf("phiên bản %q không hợp lệ", s)
	}
	var out [3]int
	for i, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') || strings.TrimLeft(p, "0123456789") != "" {
			return Version{}, 0, fmt.Errorf("phiên bản %q không hợp lệ", s)
		}
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return Version{}, 0, fmt.Errorf("phiên bản %q không hợp lệ", s)
		}
		out[i] = n
	}
	return Version{out[0], out[1], out[2]}, len(parts), nil
}

// Constraint là hội các so sánh, vd ">=1.2 <2". Phiên bản thiếu thành phần được
// bù 0 (">=1.2" nghĩa là ">=1.2.0").
type Constraint struct {
	raw   string
	terms []term
}

type term struct {
	op string // >=, >, <=, <, =
	v  Version
}

func (c Constraint) String() string { return c.raw }

// ParseConstraint đọc ràng buộc gồm các vế cách nhau bằng khoảng trắng.
func ParseConstraint(s string) (Constraint, error) {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return Constraint{}, fmt.Errorf("ràng buộc phiên bản rỗng")
	}
	c := Constraint{raw: strings.Join(fields, " ")}
	for _, f := range fields {
		op := "="
		for _, cand := range []string{">=", "<=", ">", "<", "="} {
			if strings.HasPrefix(f, cand) {
				op = cand
				break
			}
		}
		rest := strings.TrimPrefix(f, op)
		v, _, err := parseParts(rest)
		if err != nil {
			return Constraint{}, fmt.Errorf("ràng buộc %q: %w", s, err)
		}
		c.terms = append(c.terms, term{op: op, v: v})
	}
	return c, nil
}

// Allows cho biết v có thỏa mọi vế của ràng buộc không.
func (c Constraint) Allows(v Version) bool {
	for _, t := range c.terms {
		cmp := v.Compare(t.v)
		ok := false
		switch t.op {
		case ">=":
			ok = cmp >= 0
		case ">":
			ok = cmp > 0
		case "<=":
			ok = cmp <= 0
		case "<":
			ok = cmp < 0
		case "=":
			ok = cmp == 0
		}
		if !ok {
			return false
		}
	}
	return true
}

// DefaultConstraint là ràng buộc mặc định cho phụ thuộc vào phiên bản v:
// cùng MAJOR, không thấp hơn MAJOR.MINOR hiện tại (vd ">=1.0 <2").
func DefaultConstraint(v Version) string {
	return fmt.Sprintf(">=%d.%d <%d", v.Major, v.Minor, v.Major+1)
}
