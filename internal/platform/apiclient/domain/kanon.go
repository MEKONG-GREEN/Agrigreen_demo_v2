package domain

// Quy tắc k của tập tổng hợp (ADR-0035 §3; hợp đồng contracts/integration aggregation):
//
//  1. Ẩn sơ cấp: ô được phát khi số đơn vị đếm PHÂN BIỆT ≥ ngưỡng; ngưỡng = k, hoặc k + hysteresis nếu ô đã ẩn
//     ở phiên bản trước (trễ ẩn — không bật/tắt quanh ngưỡng giữa các phiên bản 15 phút).
//  2. Dòng withheld của nhóm (tỉnh × vụ [× tháng]) = HỢP đơn vị của các ô bị ẩn; chỉ phát khi chính nó ≥ ngưỡng
//     của nó (cũng có trễ ẩn).
//  3. Ẩn ô bù: dòng withheld được phát mà chỉ gom MỘT ô ẩn thì nó lộ đúng ô đó → ẩn thêm ô đã phát có ít đơn vị
//     nhất của nhóm (khóa nhỏ hơn khi bằng nhau) và gộp vào withheld; nhóm không còn ô nào để ẩn bù → withheld
//     cũng ẩn.
//
// Trạng thái ẩn của ô vắng mặt ở phiên bản này giữ nguyên (ô xuất hiện lại vẫn phải vượt k + hysteresis).

import (
	"slices"
	"strings"
)

// Lý do ẩn (cột reason của pf_apiclient.dataset_cell_state).
const (
	SuppressBelowK         = "below_k"
	SuppressComplementary  = "complementary"
	SuppressWithheldBelowK = "withheld_below_k"
)

// KCell là một ô thô đã gộp mọi nguồn: khóa thực thể, nhóm ẩn bù, khóa thực thể của dòng withheld của nhóm và
// tập đơn vị đếm phân biệt.
type KCell struct {
	Key         string
	Group       string
	WithheldKey string
	Units       map[string]struct{}
}

// KRule là tham số áp: K ≥ 2, Hysteresis ≥ 1.
type KRule struct {
	K          int
	Hysteresis int
}

// KMark là trạng thái ẩn của một khóa sau khi áp (Reason rỗng = được phát).
type KMark struct {
	Suppressed bool
	Reason     string
}

// KWithheld là dòng withheld được phát của một nhóm: khóa và các ô thành phần (đã sắp).
type KWithheld struct {
	Key     string
	Group   string
	Members []string
}

// KResult là kết quả áp quy tắc k cho một phiên bản.
type KResult struct {
	Published  []string    // khóa ô được phát (đã sắp)
	Withheld   []KWithheld // dòng withheld được phát (sắp theo khóa)
	Marks      map[string]KMark
	Suppressed int // số ô (kể cả dòng withheld) bị ẩn ở phiên bản này
}

func (r KRule) threshold(prev map[string]bool, key string) int {
	if prev[key] {
		return r.K + r.Hysteresis
	}
	return r.K
}

// ApplyK áp quy tắc k. prev: khóa → đã ẩn ở phiên bản trước (dataset_cell_state). Thuần, tất định.
func ApplyK(cells []KCell, rule KRule, prev map[string]bool) KResult {
	groups := map[string][]KCell{}
	for _, c := range cells {
		groups[c.Group] = append(groups[c.Group], c)
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	slices.Sort(names)
	res := KResult{Marks: map[string]KMark{}}
	for _, g := range names {
		cs := groups[g]
		slices.SortFunc(cs, func(a, b KCell) int { return strings.Compare(a.Key, b.Key) })
		var pub, hidden []KCell
		for _, c := range cs {
			if len(c.Units) >= rule.threshold(prev, c.Key) {
				pub = append(pub, c)
				continue
			}
			hidden = append(hidden, c)
			res.Marks[c.Key] = KMark{Suppressed: true, Reason: SuppressBelowK}
		}
		if len(hidden) == 0 {
			for _, c := range pub {
				res.Marks[c.Key] = KMark{}
			}
			res.Published = append(res.Published, keys(pub)...)
			continue
		}
		wk := cs[0].WithheldKey
		union := unionUnits(hidden)
		withheldOK := len(union) >= rule.threshold(prev, wk)
		if withheldOK && len(hidden) == 1 {
			if len(pub) == 0 {
				withheldOK = false
			} else {
				i := smallest(pub)
				c := pub[i]
				pub = slices.Delete(pub, i, i+1)
				hidden = append(hidden, c)
				res.Marks[c.Key] = KMark{Suppressed: true, Reason: SuppressComplementary}
				for u := range c.Units {
					union[u] = struct{}{}
				}
			}
		}
		for _, c := range pub {
			res.Marks[c.Key] = KMark{}
		}
		res.Published = append(res.Published, keys(pub)...)
		res.Suppressed += len(hidden)
		switch {
		case withheldOK:
			res.Marks[wk] = KMark{}
			m := keys(hidden)
			slices.Sort(m)
			res.Withheld = append(res.Withheld, KWithheld{Key: wk, Group: g, Members: m})
		case len(union) < rule.threshold(prev, wk):
			res.Marks[wk] = KMark{Suppressed: true, Reason: SuppressWithheldBelowK}
			res.Suppressed++
		default: // một ô ẩn, không còn ô để ẩn bù
			res.Marks[wk] = KMark{Suppressed: true, Reason: SuppressComplementary}
			res.Suppressed++
		}
	}
	slices.Sort(res.Published)
	slices.SortFunc(res.Withheld, func(a, b KWithheld) int { return strings.Compare(a.Key, b.Key) })
	return res
}

func keys(cs []KCell) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Key
	}
	return out
}

func unionUnits(cs []KCell) map[string]struct{} {
	u := map[string]struct{}{}
	for _, c := range cs {
		for x := range c.Units {
			u[x] = struct{}{}
		}
	}
	return u
}

// smallest: chỉ số ô ít đơn vị nhất (khóa nhỏ hơn khi bằng nhau — cs đã sắp theo khóa).
func smallest(cs []KCell) int {
	best := 0
	for i := 1; i < len(cs); i++ {
		if len(cs[i].Units) < len(cs[best].Units) {
			best = i
		}
	}
	return best
}
