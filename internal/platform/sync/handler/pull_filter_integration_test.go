package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

// KN US-SYNC-02: máy nhận sync_now kèm entities → pull chỉ các entity đó; con trỏ giữ
// vị trí entity khác nên lượt pull đầy đủ sau vẫn nhận đủ. Entity lạ → 422. Source
// từ chối 403 (vai không đọc entity, vd nông dân với task) → bỏ entity, không hỏng pull.
func TestPullEntitiesFilterKNUSSYNC02(t *testing.T) {
	f := newFixture(t)
	c := f.officer()
	c.register()
	plots := f.source.add("plot", unitA, 3)
	f.source.add("task", unitA, 2)

	r := c.get("/v1/sync/pull?entities=plot", nil)
	if r.status != http.StatusOK {
		t.Fatalf("pull lọc: %d %s", r.status, r.raw)
	}
	var pg pullPage
	_ = json.Unmarshal(r.raw, &pg)
	if len(pg.Changes) != 3 {
		t.Fatalf("chỉ plot: %+v", pg.Changes)
	}
	for i, ch := range pg.Changes {
		if ch.Entity != "plot" || ch.ID != plots[i].String() {
			t.Fatalf("thay đổi %d: %+v", i, ch)
		}
	}
	// Lượt đầy đủ từ con trỏ đó: nhận task, không nhận lại plot.
	full, _ := c.pull(pg.NextCursor, 100)
	got := map[string]int{}
	for _, ch := range full.Changes {
		got[ch.Entity]++
	}
	if got["task"] != 2 || got["plot"] != 0 {
		t.Fatalf("pull đầy đủ sau lọc: %v", got)
	}
	if r := c.get("/v1/sync/pull?entities=plot,khong_co", nil); r.status != http.StatusUnprocessableEntity {
		t.Fatalf("entity lạ: %d", r.status)
	}
	// Source trả 403 cho entity task → entity khác vẫn đồng bộ.
	f.source.mu.Lock()
	f.source.forbid = map[string]bool{"task": true}
	f.source.mu.Unlock()
	f.source.add("plot", unitA, 1)
	f.source.add("task", unitA, 1)
	pg2, r2 := c.pull(full.NextCursor, 100)
	if r2.status != http.StatusOK || len(pg2.Changes) != 1 || pg2.Changes[0].Entity != "plot" {
		t.Fatalf("403 bị bỏ qua: %d %+v", r2.status, pg2.Changes)
	}
}
