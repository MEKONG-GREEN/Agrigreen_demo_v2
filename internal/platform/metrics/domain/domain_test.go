package domain

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/defs"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/metrics/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
)

// Mọi tệp định nghĩa trong kho mã hợp lệ và phủ đủ 4 KPI KN US-DASH-01, mức đủ máy CGH
// FN-10 theo vụ, diện tích/năng suất vụ, số thửa đã số hoá.
func TestRepoDefinitionsLoad(t *testing.T) {
	c, err := Load(defs.FS)
	if err != nil {
		t.Fatal(err)
	}
	want := map[port.Key]port.Grain{
		"kn.sown_area_ha": port.GrainDay, "kn.cooperatives_reported": port.GrainDay, "kn.open_tasks": port.GrainDay,
		"kn.directive_ack_rate_pct": port.GrainDay, "cgh.coop_sufficiency_pct": port.GrainSeason, "cgh.coops_shortage": port.GrainSeason,
		"cc.season_area_ha": port.GrainSeason, "cc.yield_t_per_ha": port.GrainSeason, "geo.plots_digitised": port.GrainDay,
	}
	for k, g := range want {
		d, ok := c.Effective(k, clock.Date{Year: 2026, Month: 9, Day: 30})
		if !ok || d.Grain != g || d.Version != 1 || d.ProposedBy == d.ApprovedBy || len(d.ViewerRoles) == 0 {
			t.Fatalf("%s: %+v ok=%v", k, d, ok)
		}
	}
	if len(c.Keys()) != len(want) {
		t.Fatalf("số key %d", len(c.Keys()))
	}
	if _, ok := c.Effective("kn.sown_area_ha", clock.Date{Year: 2025, Month: 12, Day: 31}); ok {
		t.Fatal("trước effective_from không có định nghĩa hiệu lực")
	}
}

const okDef = `key: a.b
version: %d
title: T
description: D
unit: ha
grain: day
aggregation: sum
source_module: pf/geo
source: s
rule_version: r1
effective_from: "%s"
proposed_by: p
approved_by: q
`

func def(v int, eff string) string {
	s := strings.Replace(okDef, "%d", string(rune('0'+v)), 1)
	return strings.Replace(s, "%s", eff, 1)
}

func TestDefinitionValidation(t *testing.T) {
	cases := map[string]fstest.MapFS{
		"sai tên tệp":           {"x.yaml": {Data: []byte(def(1, "2026-01-01"))}},
		"người duyệt = đề xuất": {"a.b.v1.yaml": {Data: []byte(strings.Replace(def(1, "2026-01-01"), "approved_by: q", "approved_by: P", 1))}},
		"grain lạ":              {"a.b.v1.yaml": {Data: []byte(strings.Replace(def(1, "2026-01-01"), "grain: day", "grain: week", 1))}},
		"khóa lạ":               {"a.b.v1.yaml": {Data: []byte(def(1, "2026-01-01") + "foo: 1\n")}},
		"thiếu v1":              {"a.b.v2.yaml": {Data: []byte(def(2, "2026-01-01"))}},
		"hiệu lực lùi": {
			"a.b.v1.yaml": {Data: []byte(def(1, "2026-02-01"))},
			"a.b.v2.yaml": {Data: []byte(def(2, "2026-01-01"))},
		},
		"đổi đơn vị": {
			"a.b.v1.yaml": {Data: []byte(def(1, "2026-01-01"))},
			"a.b.v2.yaml": {Data: []byte(strings.Replace(def(2, "2026-03-01"), "unit: ha", "unit: t", 1))},
		},
	}
	for name, fs := range cases {
		if _, err := Load(fs); err == nil {
			t.Fatalf("%s: phải lỗi", name)
		}
	}
	// Hai phiên bản hợp lệ: phiên bản có hiệu lực theo ngày D.
	c, err := Load(fstest.MapFS{"a.b.v1.yaml": {Data: []byte(def(1, "2026-01-01"))}, "a.b.v2.yaml": {Data: []byte(def(2, "2026-03-01"))}})
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := c.Effective("a.b", clock.Date{Year: 2026, Month: 2, Day: 28}); d.Version != 1 {
		t.Fatalf("28/02 → v1, nhận v%d", d.Version)
	}
	if d, _ := c.Effective("a.b", clock.Date{Year: 2026, Month: 3, Day: 1}); d.Version != 2 {
		t.Fatalf("01/03 → v2, nhận v%d", d.Version)
	}
}

func TestInputsHashDeterministic(t *testing.T) {
	d := Def{Definition: port.Definition{Key: "a.b", Version: 1}}
	day := clock.Date{Year: 2026, Month: 9, Day: 1}
	wm := port.Watermark{LastEventID: uuid.New(), RecordedAt: time.Unix(100, 0)}
	h1 := InputsHash(d, port.Scope{Units: []string{"80"}}, day, decimal.RequireFromString("1.50"), wm)
	h2 := InputsHash(d, port.Scope{Units: []string{"80"}}, day, decimal.RequireFromString("1.50"), wm)
	if !bytes.Equal(h1, h2) {
		t.Fatal("cùng đầu vào phải cùng băm")
	}
	d.Version = 2
	if bytes.Equal(h1, InputsHash(d, port.Scope{Units: []string{"80"}}, day, decimal.RequireFromString("1.50"), wm)) {
		t.Fatal("khác phiên bản định nghĩa phải khác băm")
	}
}

// GIS US-REPLAY-01: gập nhật ký theo thứ tự; ngừng dùng giữ thuộc tính; ảnh chụp +
// phần sau = gập toàn bộ.
func TestFoldSnapshotPlusTailEqualsFull(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	ch := func(f uuid.UUID, op port.LayerOp, after string) port.LayerChange {
		return port.LayerChange{FeatureRef: f, Op: op, After: json.RawMessage(after)}
	}
	all := []port.LayerChange{
		ch(a, port.LayerCreate, `{"v":1}`), ch(b, port.LayerCreate, `{"v":10}`), ch(a, port.LayerUpdate, `{"v":2}`),
		ch(b, port.LayerRetire, `{"v":10,"status":"retired"}`), ch(a, port.LayerUpdate, `{"v":3}`), ch(b, port.LayerRestore, `{"v":11}`),
	}
	full := Normalize(Fold(nil, all))
	for cut := 0; cut <= len(all); cut++ {
		snap := Normalize(Fold(nil, all[:cut]))
		got := Normalize(Fold(snap, all[cut:]))
		gb, _ := json.Marshal(got)
		fb, _ := json.Marshal(full)
		if !bytes.Equal(gb, fb) {
			t.Fatalf("cắt %d: %s ≠ %s", cut, gb, fb)
		}
	}
	mid := Fold(nil, all[:4])
	for _, f := range mid {
		if f.FeatureRef == b && !f.Retired {
			t.Fatal("b phải ngừng dùng sau retire")
		}
	}
}

func TestParseSeason(t *testing.T) {
	c, y, ok := port.ParseSeason(port.SeasonDimension("dx", 2026))
	if !ok || c != "DX" || y != 2026 {
		t.Fatalf("%s %d %v", c, y, ok)
	}
	if _, _, ok := port.ParseSeason("DX"); ok {
		t.Fatal("thiếu năm phải lỗi")
	}
}
