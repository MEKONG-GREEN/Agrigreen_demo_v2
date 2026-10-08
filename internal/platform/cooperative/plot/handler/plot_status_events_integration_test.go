package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	coopport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/cooperative/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

type outboxRow struct {
	id       uuid.UUID
	producer string
	payload  map[string]any
}

func (f *fixture) outboxOf(t *testing.T, typ string, plot uuid.UUID) []outboxRow {
	t.Helper()
	var out []outboxRow
	if err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT id, producer, payload FROM shared.outbox
		  WHERE event_type = $1 AND payload->>'plot_ref' = $2 ORDER BY id`, typ, plot.String())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r outboxRow
			var b []byte
			if err := rows.Scan(&r.id, &r.producer, &b); err != nil {
				return err
			}
			if err := json.Unmarshal(b, &r.payload); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// conformsTo kiểm payload khớp schema contracts/events/<typ>.v1.json: đúng tập khóa,
// đủ khóa bắt buộc, enum, kiểu cơ bản (string/boolean/null).
func conformsTo(t *testing.T, typ string, payload map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "contracts", "events", typ+".v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Type      any      `json:"type"`
			Enum      []string `json:"enum"`
			MinLength int      `json:"minLength"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	var keys, props []string
	for k := range payload {
		keys = append(keys, k)
	}
	for k := range s.Properties {
		props = append(props, k)
	}
	sort.Strings(keys)
	sort.Strings(props)
	if !slices.Equal(keys, props) {
		t.Fatalf("%s: khóa payload %v ≠ schema %v", typ, keys, props)
	}
	for _, k := range s.Required {
		if _, ok := payload[k]; !ok {
			t.Fatalf("%s: thiếu khóa bắt buộc %s", typ, k)
		}
	}
	for k, p := range s.Properties {
		v := payload[k]
		types := map[string]bool{}
		switch x := p.Type.(type) {
		case string:
			types[x] = true
		case []any:
			for _, y := range x {
				types[y.(string)] = true
			}
		}
		switch vv := v.(type) {
		case nil:
			if !types["null"] {
				t.Fatalf("%s.%s: null không hợp lệ", typ, k)
			}
		case string:
			if !types["string"] {
				t.Fatalf("%s.%s: không phải chuỗi", typ, k)
			}
			if len(p.Enum) > 0 && !slices.Contains(p.Enum, vv) {
				t.Fatalf("%s.%s = %q ngoài enum %v", typ, k, vv, p.Enum)
			}
			if len([]rune(vv)) < p.MinLength {
				t.Fatalf("%s.%s ngắn hơn %d", typ, k, p.MinLength)
			}
			if k == "event_time" {
				if _, err := time.Parse(time.RFC3339, vv); err != nil {
					t.Fatalf("%s.event_time: %v", typ, err)
				}
			}
		case bool:
			if !types["boolean"] {
				t.Fatalf("%s.%s: không phải boolean", typ, k)
			}
		default:
			t.Fatalf("%s.%s: kiểu lạ %T", typ, k, v)
		}
	}
}

// GIS US-EVL-01 / GIS BR-17: ngừng dùng và khôi phục thửa phát đúng một sự kiện outbox
// (pf.geo.plot.retired / pf.geo.plot.restored) cùng giao dịch với sổ; payload khớp schema;
// phát lại POST cùng Idempotency-Key không phát thêm.
func TestPlotRetireRestoreEmitEvents(t *testing.T) {
	f := newFixture(t)
	nat := f.national()
	plot, _ := nat.createPlot("31117", f.coopA, rect(0, 0, 100, 100))
	draft, _ := nat.createPlot("31117", uuid.Nil, rect(400, 0, 100, 100))

	post := func(path, idem string, body map[string]any) resp {
		b, _ := json.Marshal(body)
		return nat.do(f.app.URL, http.MethodPost, path, idem, "application/json", b)
	}

	// Ngừng dùng: hai lần cùng khóa → một dòng sổ, một sự kiện.
	k := key()
	retBody := map[string]any{"reason": "Thửa đã chuyển đổi sang đất ở"}
	r1 := post("/v1/plots/"+plot.String()+"/retire", k, retBody)
	r2 := post("/v1/plots/"+plot.String()+"/retire", k, retBody)
	expect(t, r1, http.StatusOK, "ngừng dùng")
	expect(t, r2, http.StatusOK, "phát lại ngừng dùng")
	ev := f.outboxOf(t, coopport.EventPlotRetired, plot)
	if len(ev) != 1 {
		t.Fatalf("pf.geo.plot.retired: %d sự kiện, muốn 1", len(ev))
	}
	p := ev[0].payload
	conformsTo(t, coopport.EventPlotRetired, p)
	if ev[0].producer != "pf/cooperative" || p["status_before"] != coopport.PlotActive || p["status_after"] != coopport.PlotRetired ||
		p["reason"] != "Thửa đã chuyển đổi sang đất ở" || p["cooperative_ref"] != f.coopA.String() || p["commune_code"] != "31117" ||
		p["actor_ref"] != nat.actor.String() || p["code"] == "" || p["plot_event_id"] == "" {
		t.Fatalf("payload retired: %v", p)
	}
	// Thao tác bị từ chối (đã ngừng dùng) không phát sự kiện.
	expect(t, post("/v1/plots/"+plot.String()+"/retire", key(), retBody), http.StatusConflict, "ngừng dùng lần hai")
	if n := len(f.outboxOf(t, coopport.EventPlotRetired, plot)); n != 1 {
		t.Fatalf("từ chối vẫn phát sự kiện: %d", n)
	}

	// Khôi phục: hai lần cùng khóa → một sự kiện; về active theo HTX.
	k = key()
	resBody := map[string]any{"reason": "Ngừng dùng nhầm, khôi phục lại"}
	expect(t, post("/v1/plots/"+plot.String()+"/restore", k, resBody), http.StatusOK, "khôi phục")
	expect(t, post("/v1/plots/"+plot.String()+"/restore", k, resBody), http.StatusOK, "phát lại khôi phục")
	ev = f.outboxOf(t, coopport.EventPlotRestored, plot)
	if len(ev) != 1 {
		t.Fatalf("pf.geo.plot.restored: %d sự kiện, muốn 1", len(ev))
	}
	p = ev[0].payload
	conformsTo(t, coopport.EventPlotRestored, p)
	if p["status_before"] != coopport.PlotRetired || p["status_after"] != coopport.PlotActive || p["needs_review"] != false ||
		p["reason"] != "Ngừng dùng nhầm, khôi phục lại" {
		t.Fatalf("payload restored: %v", p)
	}

	// Thửa nháp (chưa gán HTX): cooperative_ref null, khôi phục về draft.
	expect(t, nat.appDo(http.MethodPost, "/v1/plots/"+draft.String()+"/retire", retBody), http.StatusOK, "ngừng dùng nháp")
	expect(t, nat.appDo(http.MethodPost, "/v1/plots/"+draft.String()+"/restore", resBody), http.StatusOK, "khôi phục nháp")
	d1, d2 := f.outboxOf(t, coopport.EventPlotRetired, draft), f.outboxOf(t, coopport.EventPlotRestored, draft)
	if len(d1) != 1 || len(d2) != 1 {
		t.Fatalf("thửa nháp: %d/%d sự kiện", len(d1), len(d2))
	}
	conformsTo(t, coopport.EventPlotRetired, d1[0].payload)
	conformsTo(t, coopport.EventPlotRestored, d2[0].payload)
	if d1[0].payload["cooperative_ref"] != nil || d1[0].payload["status_before"] != coopport.PlotDraft || d2[0].payload["status_after"] != coopport.PlotDraft {
		t.Fatalf("thửa nháp: %v / %v", d1[0].payload, d2[0].payload)
	}
}
