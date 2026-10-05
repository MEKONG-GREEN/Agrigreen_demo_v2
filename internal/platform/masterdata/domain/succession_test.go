package domain

import (
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/masterdata/port"
)

func dec(s string) *decimal.Decimal { d := decimal.RequireFromString(s); return &d }

// Địa giới giả: A tách thành B (60 %) và C (40 %) ngày R có căn cứ chính thức; D nhập
// trọn vào B (chỉ có successor_codes); F tách G, H chưa có tỉ lệ; B nhập vào E ngày R2.
func successionFixture() (fakeGazetteer, EdgesFunc) {
	r, r2 := ReorgDate, Day(2027, 1, 1)
	g := fakeGazetteer{
		{Code: "A", ValidFrom: Day(2020, 1, 1), ValidTo: dp(r)},
		{Code: "D", ValidFrom: Day(2020, 1, 1), ValidTo: dp(r), Successors: []string{"B"}},
		{Code: "F", ValidFrom: Day(2020, 1, 1), ValidTo: dp(r), Successors: []string{"G", "H"}},
		{Code: "B", ValidFrom: r, ValidTo: dp(r2)},
		{Code: "C", ValidFrom: r},
		{Code: "G", ValidFrom: r},
		{Code: "H", ValidFrom: r},
		{Code: "E", ValidFrom: Day(2020, 1, 1)},
	}
	edge := func(p, s string, eff, sFrom string, share *decimal.Decimal, status string) port.Succession {
		pe, _ := ParseDay(eff)
		sf, _ := ParseDay(sFrom)
		return port.Succession{PredecessorCode: p, PredecessorValidFrom: Day(2020, 1, 1), SuccessorCode: s,
			SuccessorValidFrom: sf, EffectiveOn: pe, AreaShare: share, LegalBasis: "NQ giả", BasisStatus: status}
	}
	rows := map[string][]port.Succession{
		"A": PreferOfficial([]port.Succession{
			edge("A", "B", "2025-07-01", "2025-07-01", nil, port.BasisPendingC9),
			edge("A", "B", "2025-07-01", "2025-07-01", dec("0.6"), port.BasisOfficial),
			edge("A", "C", "2025-07-01", "2025-07-01", dec("0.4"), port.BasisOfficial),
		}),
	}
	bToE := edge("B", "E", "2027-01-01", "2020-01-01", nil, port.BasisOfficial)
	bToE.PredecessorValidFrom = r
	rows["B"] = []port.Succession{bToE}
	edges := func(u Unit) ([]port.Succession, error) {
		if es, ok := rows[u.Code]; ok {
			return es, nil
		}
		return LegacyEdges(u, g.at)
	}
	return g, edges
}

func TestPreferOfficial(t *testing.T) {
	_, edges := successionFixture()
	es, _ := edges(Unit{Code: "A"})
	if len(es) != 2 || es[0].SuccessorCode != "B" || es[0].BasisStatus != port.BasisOfficial || !es[0].AreaShare.Equal(decimal.RequireFromString("0.6")) {
		t.Fatalf("dòng official phải thay dòng pending_c9 cùng cặp: %+v", es)
	}
}

func TestRollForward(t *testing.T) {
	g, edges := successionFixture()
	type want struct {
		code      string
		share     string // "" = chưa biết
		est, pend bool
	}
	cases := []struct {
		code   string
		target string
		want   []want
	}{
		{"A", "2026-01-01", []want{{"B", "0.6", true, false}, {"C", "0.4", true, false}}},
		{"A", "2028-01-01", []want{{"C", "0.4", true, false}, {"E", "0.6", true, false}}},
		{"D", "2026-01-01", []want{{"B", "1", false, true}}},
		{"D", "2028-01-01", []want{{"E", "1", false, true}}},
		{"F", "2026-01-01", []want{{"G", "", true, true}, {"H", "", true, true}}},
		{"A", "2024-01-01", []want{{"A", "1", false, false}}},
	}
	for _, c := range cases {
		start, _ := g.at(c.code, Day(2024, 1, 1))
		target, _ := ParseDay(c.target)
		got, err := RollForward(*start, target, g.at, edges)
		if err != nil || len(got) != len(c.want) {
			t.Fatalf("RollForward(%s → %s) = %+v, %v", c.code, c.target, got, err)
		}
		for i, w := range c.want {
			r := got[i]
			okShare := (w.share == "" && r.Share == nil) || (w.share != "" && r.Share != nil && r.Share.Equal(decimal.RequireFromString(w.share)))
			if r.Code != w.code || !okShare || r.Estimated != w.est || r.Pending != w.pend {
				t.Errorf("RollForward(%s → %s)[%d] = %+v (share %v); muốn %+v", c.code, c.target, i, r, r.Share, w)
			}
		}
	}
	// Đi ngược thời gian bị từ chối.
	b, _ := g.at("B", Day(2026, 1, 1))
	if _, err := RollForward(*b, Day(2024, 1, 1), g.at, edges); !errors.Is(err, ErrRollBackward) {
		t.Fatalf("đi ngược: %v", err)
	}
}

func TestRollForwardAbolished(t *testing.T) {
	g := fakeGazetteer{{Code: "X", ValidFrom: Day(2020, 1, 1), ValidTo: dp(ReorgDate)}}
	edges := func(u Unit) ([]port.Succession, error) { return LegacyEdges(u, g.at) }
	if _, err := RollForward(g[0], Day(2026, 1, 1), g.at, edges); !errors.Is(err, port.ErrUnitNotFound) {
		t.Fatalf("bãi bỏ không kế thừa: %v", err)
	}
}

func TestSuccessorChain(t *testing.T) {
	g, edges := successionFixture()
	start, _ := g.at("A", Day(2024, 1, 1))
	got, err := SuccessorChain(*start, Day(2026, 1, 1), g.at, edges)
	if err != nil || len(got) != 2 || got[0].SuccessorCode != "B" || got[1].SuccessorCode != "C" {
		t.Fatalf("chuỗi tới 2026: %+v %v", got, err)
	}
	got, err = SuccessorChain(*start, Day(2028, 1, 1), g.at, edges)
	if err != nil || len(got) != 3 || got[2].PredecessorCode != "B" || got[2].SuccessorCode != "E" {
		t.Fatalf("chuỗi tới 2028: %+v %v", got, err)
	}
	// Chưa tới ngày đóng hiệu lực → rỗng.
	if got, err := SuccessorChain(*start, Day(2025, 6, 30), g.at, edges); err != nil || len(got) != 0 {
		t.Fatalf("trước sắp xếp: %+v %v", got, err)
	}
}

func TestSuccessorChainStopsOnCycle(t *testing.T) {
	g := fakeGazetteer{
		{Code: "X", ValidFrom: Day(2020, 1, 1), ValidTo: dp(Day(2021, 1, 1)), Successors: []string{"Y"}},
		{Code: "Y", ValidFrom: Day(2021, 1, 1), ValidTo: dp(Day(2022, 1, 1)), Successors: []string{"X"}},
	}
	edges := func(u Unit) ([]port.Succession, error) { return LegacyEdges(u, g.at) }
	got, err := SuccessorChain(g[0], Day(2030, 1, 1), g.at, edges)
	if err != nil || len(got) != 1 {
		t.Fatalf("X không có hiệu lực ngày 2022 nên chuỗi dừng ở Y: %+v %v", got, err)
	}
}
