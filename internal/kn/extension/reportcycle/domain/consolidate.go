package domain

import (
	"sort"

	"github.com/shopspring/decimal"
)

// Loại thay đổi ô của nhật ký cập nhật (FN-TI-02.05 "Gửi mới / Gửi lại / Điều chỉnh theo yêu cầu / Tự tính").
const (
	ChangeNew      = "new"
	ChangeResubmit = "resubmit"
	ChangeAdjusted = "adjusted"
	ChangeComputed = "computed"
)

// Trigger của lần tổng hợp.
const (
	TriggerManual = "manual" // Cấp Tỉnh bấm "Cập nhật"
	TriggerSystem = "system" // một lần tự động lúc chốt kỳ (nhật ký "Hệ thống")
)

// Source là bài nộp "Đã gửi" mới nhất của một xã dùng cho lần tổng hợp.
type Source struct {
	SubmissionID       string  `json:"submission_id"`
	VersionNo          int     `json:"version_no"`
	SubmittedAt        string  `json:"submitted_at"`
	ActorRef           string  `json:"actor_ref"`
	OnBehalfOf         *string `json:"on_behalf_of,omitempty"`
	AdjustmentResponse bool    `json:"adjustment_response"`
}

// Row là một dòng dữ liệu của lần tổng hợp (khóa = mã xã hoặc mã xã/chỉ tiêu).
type Row struct {
	Key       string            `json:"key"`
	Commune   string            `json:"commune"`
	Region    string            `json:"region"`
	Indicator string            `json:"indicator,omitempty"`
	Cells     map[string]string `json:"cells"`
}

// CellChange là một ô thay đổi so với lần tổng hợp trước.
type CellChange struct {
	Commune, RowKey, Letter string
	Old, New                *string
	Kind                    string
	Source                  *Source
}

// Diff so dòng của lần trước (prev, nguồn prevSrc) với lần này (next, nextSrc) — chỉ ô khác nhau (so theo giá
// trị decimal nếu cả hai là số, "1.50" = "1.5"). formula: chữ cái các cột công thức (loại "Tự tính").
// Trả danh sách đã sắp (xã, dòng, cột), số xã thay đổi.
func Diff(prev, next []Row, prevSrc, nextSrc map[string]Source, formula map[string]bool) ([]CellChange, int) {
	pm := map[string]Row{}
	for _, r := range prev {
		pm[r.Key] = r
	}
	nm := map[string]Row{}
	for _, r := range next {
		nm[r.Key] = r
	}
	keys := map[string]bool{}
	for k := range pm {
		keys[k] = true
	}
	for k := range nm {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	var out []CellChange
	communes := map[string]bool{}
	for _, k := range ordered {
		p, pok := pm[k]
		n, nok := nm[k]
		commune := n.Commune
		if !nok {
			commune = p.Commune
		}
		_, hadSource := prevSrc[commune]
		src, hasSource := nextSrc[commune]
		letters := map[string]bool{}
		for l := range p.Cells {
			letters[l] = true
		}
		for l := range n.Cells {
			letters[l] = true
		}
		ls := make([]string, 0, len(letters))
		for l := range letters {
			ls = append(ls, l)
		}
		sort.Slice(ls, func(i, j int) bool { return letterLess(ls[i], ls[j]) })
		for _, l := range ls {
			var ov, nv *string
			if pok {
				if v, ok := p.Cells[l]; ok && v != "" {
					ov = &v
				}
			}
			if nok {
				if v, ok := n.Cells[l]; ok && v != "" {
					nv = &v
				}
			}
			if sameValue(ov, nv) {
				continue
			}
			kind := ChangeResubmit
			switch {
			case formula[l]:
				kind = ChangeComputed
			case !hadSource:
				kind = ChangeNew
			case hasSource && src.AdjustmentResponse:
				kind = ChangeAdjusted
			}
			c := CellChange{Commune: commune, RowKey: k, Letter: l, Old: ov, New: nv, Kind: kind}
			if hasSource {
				s := src
				c.Source = &s
			}
			out = append(out, c)
			communes[commune] = true
		}
	}
	return out, len(communes)
}

func sameValue(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if *a == *b {
		return true
	}
	da, e1 := decimal.NewFromString(*a)
	db, e2 := decimal.NewFromString(*b)
	return e1 == nil && e2 == nil && da.Equal(db)
}

// letterLess sắp chữ cái cột Excel theo thứ tự cột (Z < AA).
func letterLess(a, b string) bool {
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}
