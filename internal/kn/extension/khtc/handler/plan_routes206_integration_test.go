package handler_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

// The UI routes use this existing native endpoint; keep its parser/domain and
// authorization boundary independent from the client NumberField validation.
func TestPlanRoutes206NativeNumericAndReplay(t *testing.T) {
	f := newFix(t)
	admin := f.user(authz.KNAdmin, "", false)
	seedCatalog(t, admin)
	writer := f.user(authz.KNTinh, khtc, false)
	p1 := program(t, writer, "CT206", "Chương trình206", "program")
	p2 := program(t, writer, "TW206", "Dự án206", "central_project")
	path := "/v1/kn/khtc/plans/2027/versions"
	initial := writer.post(path, planBody(0, p1, p2, nil))
	want(t, initial, http.StatusCreated, "", "initial native snapshot")
	before := writer.get(path)
	want(t, before, http.StatusOK, "", "version ledger before hostile values")
	for _, field := range []string{"sort_order", "points_count", "planned_qty", "budget"} {
		vectors := []struct{ name, raw string }{
			{"large exponent", "1e100000"}, {"small exponent", "1e-100000"},
			{"quoted exponent", `"1e100000"`}, {"long digits", strings.Repeat("9", 100000)},
			{"quoted long digits", `"` + strings.Repeat("9", 100000) + `"`},
			{"huge ordinary", strings.Repeat("9", 40)}, {"precision", "1.00001"}, {"negative", "-1"},
		}
		for _, vector := range vectors {
			t.Run(field+"/"+vector.name, func(t *testing.T) {
				body := planBody(1, p1, p2, map[string]any{"effective_from": "2027-02-01"})
				body["entries"].([]map[string]any)[2][field] = json.RawMessage(vector.raw)
				raw, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				result := writer.send(http.MethodPost, path, key(), "application/json", bytes.NewReader(raw))
				// Typed integer decoding and bounded decimal syntax yield400; domain
				// precision/value bounds yield422. Neither route may allocate a version.
				if result.status != http.StatusBadRequest && result.status != http.StatusUnprocessableEntity {
					t.Fatalf("hostile %s produced status%d", field, result.status)
				}
				after := writer.get(path)
				if after.status != http.StatusOK || !bytes.Equal(before.raw, after.raw) {
					t.Fatal("rejected input changed native version ledger")
				}
			})
		}
	}
	adjustment := planBody(1, p1, p2, map[string]any{"effective_from": "2027-02-01"})
	entry := adjustment["entries"].([]map[string]any)[2]
	entry["planned_qty"] = "9999999999999999.9999"
	entry["budget"] = "999999999999999999.99"
	entry["sort_order"] = 100000
	entry["points_count"] = 100000
	idem := key()
	first := writer.do(http.MethodPost, path, idem, adjustment)
	want(t, first, http.StatusCreated, "", "bounded positive control")
	replay := writer.do(http.MethodPost, path, idem, adjustment)
	want(t, replay, http.StatusCreated, "", "lost acknowledgement replay")
	if str(first.body["version"], "id") != str(replay.body["version"], "id") {
		t.Fatal("replay allocated another version")
	}
	want(t, writer.post(path, adjustment), http.StatusConflict, "extension.khtc_plan_version_conflict", "old base remains stale after real confirmed write")
	want(t, f.user(authz.KNTinh, khtc, true).post(path, adjustment), http.StatusForbidden, "extension.read_only", "readonly cannot apply")
	want(t, f.user(authz.KNTinh, "trong_trot", false).post(path, adjustment), http.StatusForbidden, "", "other department cannot apply")
	old := writer.get("/v1/kn/khtc/plans/2027?version=1")
	want(t, old, http.StatusOK, "", "immutable old snapshot")
	oldEntries, err := json.Marshal(old.body["entries"])
	if err != nil {
		t.Fatal(err)
	}
	originalEntries, err := json.Marshal(initial.body["entries"])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldEntries, originalEntries) {
		t.Fatal("adjustment changed old snapshot entries")
	}
}
