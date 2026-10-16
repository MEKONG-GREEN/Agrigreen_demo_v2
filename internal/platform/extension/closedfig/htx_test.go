package closedfig

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

type fakeHTX map[uuid.UUID]string

func (f fakeHTX) Confirmations(_ context.Context, coops []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	for _, c := range coops {
		if s, ok := f[c]; ok {
			out[c] = s
		} else {
			out[c] = port.ConfirmationUnconfirmed
		}
	}
	return out, nil
}

// D2 (ADR-0053 §8): dòng HTX lấy trạng thái từ cổng D2; dòng không phải HTX giữ trạng thái nguồn; kỳ × biểu unconfirmed
// khi có một dòng HTX chưa xác nhận; tổng tách phần chưa xác nhận; nhận quyền HTX chỉ đổi băm trạng thái.
func TestApplyHTX_D2_PFPUB(t *testing.T) {
	claimed, provisional := ids.New(), ids.New()
	r := cntsRule(t)
	mk := func() port.ClosedFigure {
		f := weekFig(r, d(2026, 9, 14), "BM-04", nil)
		f.Rows, _ = json.Marshal([]map[string]any{
			{"key": "X1", "unit": "X1", "unit_kind": "commune", "cells": map[string]string{"heo": "10"}, "confirmation": "confirmed"},
			{"key": claimed.String(), "unit": claimed.String(), "unit_kind": port.UnitKindCooperative, "cells": map[string]string{"heo": "3"}, "confirmation": "confirmed"},
			{"key": provisional.String(), "unit": provisional.String(), "unit_kind": port.UnitKindCooperative, "cells": map[string]string{"heo": "2"}, "confirmation": "confirmed"}})
		f.FiguresHash, f.ConfirmationStatus = hashOf("fig"), port.ConfirmationConfirmed
		return f
	}
	figs := []port.ClosedFigure{mk()}
	if err := applyHTX(context.Background(), fakeHTX{claimed: port.ConfirmationConfirmed}, figs); err != nil {
		t.Fatal(err)
	}
	if figs[0].ConfirmationStatus != port.ConfirmationUnconfirmed || *figs[0].FiguresHash != "fig" || figs[0].StatusHash == nil {
		t.Fatalf("kỳ có HTX tạm: %+v", figs[0])
	}
	o := convert(t, figs, port.CrosswalkTarget{})[0]
	if o.Sum["heo"] != "15" || o.SumConfirmed["heo"] != "13" || o.SumUnconfirmed["heo"] != "2" {
		t.Fatalf("tách xác nhận: %+v", o)
	}
	// HTX tạm nhận quyền → mọi dòng xác nhận; băm số giữ nguyên, băm trạng thái đổi.
	after := []port.ClosedFigure{mk()}
	if err := applyHTX(context.Background(), fakeHTX{claimed: port.ConfirmationConfirmed, provisional: port.ConfirmationConfirmed}, after); err != nil {
		t.Fatal(err)
	}
	a := convert(t, after, port.CrosswalkTarget{})[0]
	if after[0].ConfirmationStatus != port.ConfirmationConfirmed || a.FiguresHash != o.FiguresHash || a.StatusHash == o.StatusHash ||
		len(a.SumUnconfirmed) != 0 {
		t.Fatalf("sau nhận quyền: %+v", a)
	}
	// Không nối cổng → dòng HTX "Chưa xác nhận"; kỳ không có dòng HTX → không đổi.
	none := []port.ClosedFigure{mk(), weekFig(r, d(2026, 9, 14), "BM-17", map[string]string{"ca": "1"})}
	before := string(none[1].Rows)
	if err := applyHTX(context.Background(), nil, none); err != nil {
		t.Fatal(err)
	}
	if none[0].ConfirmationStatus != port.ConfirmationUnconfirmed || string(none[1].Rows) != before {
		t.Fatalf("không nối cổng: %+v", none)
	}
}

type historicalHTX struct {
	fakeHTX
	at   time.Time
	seen *time.Time
}

func (h historicalHTX) ConfirmationsAt(_ context.Context, refs []uuid.UUID, at time.Time) (map[uuid.UUID]string, error) {
	*h.seen = at
	out := map[uuid.UUID]string{}
	for _, ref := range refs {
		out[ref] = port.ConfirmationUnconfirmed
		if !at.Before(h.at) {
			out[ref] = port.ConfirmationConfirmed
		}
	}
	return out, nil
}
func TestKH_M6ConfirmationAtCutoffInsteadOfCurrent(t *testing.T) {
	id := ids.New()
	claimed := time.Date(2026, 10, 14, 1, 0, 0, 0, time.UTC)
	seen := time.Time{}
	h := historicalHTX{fakeHTX: fakeHTX{id: port.ConfirmationConfirmed}, at: claimed, seen: &seen}
	makeFig := func() []port.ClosedFigure {
		raw, _ := json.Marshal([]map[string]any{{"key": id.String(), "unit": id.String(), "unit_kind": port.UnitKindCooperative, "cells": map[string]string{"members": "70"}, "confirmation": "confirmed"}})
		return []port.ClosedFigure{{Rows: raw}}
	}
	cutoff := time.Date(2026, 10, 13, 16, 59, 59, 999999999, time.UTC)
	figs := makeFig()
	if err := applyHTXAt(context.Background(), h, figs, &cutoff); err != nil {
		t.Fatal(err)
	}
	if !seen.Equal(cutoff) || figs[0].ConfirmationStatus != port.ConfirmationUnconfirmed {
		t.Fatalf("after-cutoff claim leaked to old report: seen %s figure %+v", seen, figs)
	}
	next := claimed.Add(time.Hour)
	figs = makeFig()
	if err := applyHTXAt(context.Background(), h, figs, &next); err != nil {
		t.Fatal(err)
	}
	if figs[0].ConfirmationStatus != port.ConfirmationConfirmed {
		t.Fatal("next cutoff missed claim")
	}
	if err := applyHTXAt(context.Background(), fakeHTX{id: port.ConfirmationConfirmed}, makeFig(), &cutoff); err == nil {
		t.Fatal("historical reader must not fall back to current state")
	}
}
