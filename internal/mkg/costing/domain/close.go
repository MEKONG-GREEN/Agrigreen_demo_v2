package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/costing/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/mass"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/money"
)

// SnapEntry là một bút toán của kỳ trong ảnh chụp đầu vào (delta so với dòng bị thay).
type SnapEntry struct {
	ID            uuid.UUID  `json:"id"`
	LotRef        uuid.UUID  `json:"lot_ref"`
	Category      string     `json:"category"`
	DimensionKind string     `json:"dimension_kind"`
	DimensionRef  *uuid.UUID `json:"dimension_ref"`
	DeltaAmount   string     `json:"delta_amount"`
	DeltaMassKg   *string    `json:"delta_mass_kg"`
	EntryKind     string     `json:"entry_kind"`
	ActorRef      uuid.UUID  `json:"actor_ref"`
	RuleVersion   string     `json:"rule_version"`
}

// SnapSettlement là một quyết toán HTX đã duyệt trong kỳ (gốc hoặc điều chỉnh).
type SnapSettlement struct {
	ID           uuid.UUID `json:"id"` // bản ghi sổ 'approved'
	SettlementID uuid.UUID `json:"settlement_id"`
	HTXRef       uuid.UUID `json:"htx_ref"`
	Kind         string    `json:"kind"`
	Amount       string    `json:"amount"`
	MassKg       *string   `json:"mass_kg"`
	RuleVersion  string    `json:"rule_version"`
}

// SnapCarbon là một khoản doanh thu carbon đã ghi trong kỳ.
type SnapCarbon struct {
	ID          uuid.UUID `json:"id"`
	Retained    string    `json:"retained"`
	Shares      string    `json:"shares"` // nông hộ/HTX + cơ quan + VVB (phải trả)
	RuleVersion string    `json:"rule_version"`
}

// Snapshot là ảnh chụp đầu vào BẤT BIẾN của đóng kỳ (mẫu mục 12). Thứ tự trường cố định, các
// danh sách sắp theo id → json.Marshal tất định → SHA-256 ổn định.
type Snapshot struct {
	PeriodID    uuid.UUID        `json:"period_id"`
	PeriodCode  string           `json:"period_code"`
	From        time.Time        `json:"from"`
	To          time.Time        `json:"to"`
	AsOf        time.Time        `json:"as_of"`
	RuleVersion string           `json:"rule_version"`
	Entries     []SnapEntry      `json:"entries"`
	Settlements []SnapSettlement `json:"settlements"`
	Carbon      []SnapCarbon     `json:"carbon"`
}

// Normalize sắp danh sách và đưa thời điểm về UTC (gọi trước khi băm).
func (s *Snapshot) Normalize() {
	s.From, s.To, s.AsOf = s.From.UTC(), s.To.UTC(), s.AsOf.UTC()
	if s.Entries == nil {
		s.Entries = []SnapEntry{}
	}
	if s.Settlements == nil {
		s.Settlements = []SnapSettlement{}
	}
	if s.Carbon == nil {
		s.Carbon = []SnapCarbon{}
	}
	sort.Slice(s.Entries, func(i, j int) bool { return s.Entries[i].ID.String() < s.Entries[j].ID.String() })
	sort.Slice(s.Settlements, func(i, j int) bool { return s.Settlements[i].ID.String() < s.Settlements[j].ID.String() })
	sort.Slice(s.Carbon, func(i, j int) bool { return s.Carbon[i].ID.String() < s.Carbon[j].ID.String() })
}

// Canonical trả dạng byte tất định của ảnh chụp.
func (s Snapshot) Canonical() ([]byte, error) {
	s.Normalize()
	return json.Marshal(s)
}

// SHA256 là băm hex của dạng tất định.
func (s Snapshot) SHA256() (string, error) {
	b, err := s.Canonical()
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}

// AdjustingAuthors trả người lập bút toán điều chỉnh của kỳ (sắp, không trùng) — R-56.
func (s Snapshot) AdjustingAuthors() []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	var out []uuid.UUID
	for _, e := range s.Entries {
		if e.EntryKind == "adjusting" && !seen[e.ActorRef] {
			seen[e.ActorRef] = true
			out = append(out, e.ActorRef)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	if out == nil {
		out = []uuid.UUID{}
	}
	return out
}

// Line là một dòng tổng hợp của kỳ (lưu jsonb; số là chuỗi).
type Line struct {
	Category      string     `json:"category"`
	DimensionKind string     `json:"dimension_kind"`
	DimensionRef  *uuid.UUID `json:"dimension_ref"`
	Amount        string     `json:"amount"`
	MassKg        *string    `json:"mass_kg"`
	Entries       int        `json:"entries"`
}

type lineKey struct {
	cat, kind, ref string
}

type acc struct {
	line    Line
	amount  decimal.Decimal
	mass    decimal.Decimal
	hasMass bool
}

// Summarize tổng hợp ảnh chụp thành dòng theo (nhóm, chiều) — tất định: sắp theo Category,
// DimensionKind, DimensionRef (nil trước). Quyết toán HTX → straw_purchase × htx (một lần, không
// cộng lại khoản phải trả — proto D03); carbon → carbon_revenue (phần giữ lại) + carbon_share.
func Summarize(s Snapshot) []Line {
	m := map[lineKey]*acc{}
	add := func(cat, kind string, ref *uuid.UUID, amount string, kg *string) {
		k := lineKey{cat: cat, kind: kind}
		if ref != nil {
			k.ref = ref.String()
		}
		a := m[k]
		if a == nil {
			var r *uuid.UUID
			if ref != nil {
				rr := *ref
				r = &rr
			}
			a = &acc{line: Line{Category: cat, DimensionKind: kind, DimensionRef: r}}
			m[k] = a
		}
		d, _ := decimal.NewFromString(amount)
		a.amount = a.amount.Add(d)
		if kg != nil {
			x, _ := decimal.NewFromString(*kg)
			a.mass, a.hasMass = a.mass.Add(x), true
		}
		a.line.Entries++
	}
	for _, e := range s.Entries {
		add(e.Category, e.DimensionKind, e.DimensionRef, e.DeltaAmount, e.DeltaMassKg)
	}
	for _, st := range s.Settlements {
		h := st.HTXRef
		add(string(port.CatStrawPurchase), "htx", &h, st.Amount, st.MassKg)
	}
	for _, c := range s.Carbon {
		add(string(port.CatCarbonRevenue), "none", nil, c.Retained, nil)
		add(string(port.CatCarbonShare), "none", nil, c.Shares, nil)
	}
	keys := make([]lineKey, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.cat != b.cat {
			return a.cat < b.cat
		}
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.ref < b.ref
	})
	out := make([]Line, 0, len(keys))
	for _, k := range keys {
		a := m[k]
		a.line.Amount = a.amount.StringFixed(money.AmountScale)
		if a.hasMass {
			s := a.mass.StringFixed(mass.Scale)
			a.line.MassKg = &s
		}
		out = append(out, a.line)
	}
	return out
}

// ResultSetSHA256 băm tập kết quả đã tổng hợp (= source_hash của mkg/acctbridge). Không gồm id
// của lần chạy: cùng ảnh chụp + cùng rule_version → cùng băm.
func ResultSetSHA256(periodCode string, lines []Line) (string, error) {
	b, err := json.Marshal(struct {
		PeriodCode  string `json:"period_code"`
		RuleVersion string `json:"rule_version"`
		Lines       []Line `json:"lines"`
	}{periodCode, RulePeriodClose, lines})
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}

// ToPort đổi dòng tổng hợp sang port.SummaryLine.
func ToPort(lines []Line) ([]port.SummaryLine, error) {
	out := make([]port.SummaryLine, 0, len(lines))
	for _, l := range lines {
		a, err := money.ParseAmount(l.Amount)
		if err != nil {
			return nil, err
		}
		sl := port.SummaryLine{Category: port.CostCategory(l.Category), DimensionKind: l.DimensionKind, DimensionRef: l.DimensionRef,
			Amount: a, Entries: l.Entries}
		if l.MassKg != nil {
			k, err := mass.ParseKg(*l.MassKg)
			if err != nil {
				return nil, err
			}
			sl.MassKg = &k
		}
		out = append(out, sl)
	}
	return out, nil
}

// InputsSHA256 băm tất định một giá trị đầu vào (struct có thứ tự trường cố định).
func InputsSHA256(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return sha256Hex(b), nil
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
