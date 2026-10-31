package hc30_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

type julyPromotion struct {
	name, grade, oldCoeff, newCoeff, diff, since, due string
	oldStep, newStep                                  int
	id, key                                           string
}

// These tuples are fixed synthetic business oracles, including the 0.20/0.33/
// 0.20 differences in FN09 AC02. They are not calculated by production helpers.
// The fourth tuple and seeded scales/rules are explicit Q-069/Q-070 assumptions.
func julyPromotionOracle() []julyPromotion {
	return []julyPromotion{
		{name: "Trần Thị An", grade: "V.03.09.27", oldStep: 3, newStep: 4, oldCoeff: "2.26", newCoeff: "2.46", diff: "0.20", since: "2024-07-01", due: "2026-07-01"},
		{name: "Lê Thị Bình", grade: "01.003", oldStep: 5, newStep: 6, oldCoeff: "3.66", newCoeff: "3.99", diff: "0.33", since: "2023-07-01", due: "2026-07-01"},
		{name: "Nguyễn Thị Cúc", grade: "V.03.09.27", oldStep: 5, newStep: 6, oldCoeff: "2.66", newCoeff: "2.86", diff: "0.20", since: "2024-07-15", due: "2026-07-15"},
		{name: "Phạm Thị Dung", grade: "01.003", oldStep: 2, newStep: 3, oldCoeff: "2.67", newCoeff: "3.00", diff: "0.33", since: "2023-07-20", due: "2026-07-20"},
	}
}

func assertJulyForm(t *testing.T, content []byte, people []julyPromotion) {
	t.Helper()
	book, err := excelize.OpenReader(bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = book.Close() }()
	rows, err := book.GetRows("mau1")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	header20, julyTitle := false, false
	for _, row := range rows {
		julyTitle = julyTitle || strings.Contains(strings.Join(row, " "), "THÁNG 07/2026")
		if len(row) == 20 {
			numbered := true
			for i := range 20 {
				numbered = numbered && row[i] == strconv.Itoa(i+1)
			}
			header20 = header20 || numbered
		}
		for _, p := range people {
			if len(row) < 2 || !strings.HasPrefix(row[1], p.name+" - ") {
				continue
			}
			if seen[p.id] || len(row) != 20 {
				t.Fatalf("Mẫu 1 must contain each staff once with exactly 20 columns")
			}
			seen[p.id] = true
			comma := func(v string) string { return strings.ReplaceAll(v, ".", ",") }
			if row[7] != strconv.Itoa(p.oldStep) || row[8] != comma(p.oldCoeff) ||
				row[12] != strconv.Itoa(p.newStep) || row[13] != comma(p.newCoeff) ||
				row[16] != comma(p.diff) || row[18] != "" || row[19] != "Dữ liệu tổng hợp W11.31" {
				t.Fatalf("Mẫu 1 step/coefficient/difference/blank column19 differs from fixed oracle for %s", p.id)
			}
		}
	}
	if !header20 || !julyTitle || len(seen) != 4 {
		t.Fatalf("Mẫu 1 missing 20-column header, July2026 title or exact four staff: %v %v %d", header20, julyTitle, len(seen))
	}
}

func assertJulyProjection(t *testing.T, f *fixture, people []julyPromotion, upgraded bool) {
	t.Helper()
	for _, p := range people {
		cur := f.request("hcth", http.MethodGet, "/v1/hrm/staff/"+p.id+"?at=2026-07-31", "", nil, http.StatusOK).body["current"].(map[string]any)
		step, coeff, since := p.oldStep, p.oldCoeff, p.since
		if upgraded {
			step, coeff, since = p.newStep, p.newCoeff, p.due
		}
		if cur["grade"] != p.grade || cur["step"] != json.Number(strconv.Itoa(step)) || cur["coefficient"] != coeff || cur["step_since"] != since {
			t.Fatalf("July grade/step/coefficient projection differs from fixed oracle for %s, upgraded=%v", p.id, upgraded)
		}
		past := f.request("hcth", http.MethodGet, "/v1/hrm/staff/"+p.id+"?at=2026-06-30", "", nil, http.StatusOK).body["current"].(map[string]any)
		if past["step"] != json.Number(strconv.Itoa(p.oldStep)) || past["coefficient"] != p.oldCoeff {
			t.Fatalf("decision changed the pre-July projection for %s", p.id)
		}
	}
}

func TestHCTHFourJulyNLTXDecisionCompensation_W1131(t *testing.T) {
	f := newFixture(t)
	f.request("admin", http.MethodPost, "/v1/hrm/defaults", "hc30-july-defaults", nil, http.StatusCreated)
	people := julyPromotionOracle()
	for i := range people {
		p := &people[i]
		body := staff(p.name, fmt.Sprintf("09120200%02d", i), "vien_chuc", "trong_trot")
		body["events"] = append(body["events"].([]any), event("grade", "2020-01-01", map[string]any{
			"grade": p.grade, "step": strconv.Itoa(p.oldStep), "step_since": p.since,
		}))
		p.id = f.request("hcth", http.MethodPost, "/v1/hrm/staff", "hc30-july-create-"+strconv.Itoa(i), body, http.StatusCreated).body["id"].(string)
		for _, year := range []int{2023, 2024, 2025} {
			f.request("hcth", http.MethodPost, "/v1/hrm/staff/"+p.id+"/ratings", fmt.Sprintf("hc30-july-rating-%d-%d", i, year),
				map[string]any{"year": year, "rating": "HTTNV", "source": "conversion"}, http.StatusCreated)
		}
	}
	t.Run("exact_four_eligible_in_July", func(t *testing.T) {
		scoped := f.withTest(t)
		r := scoped.request("hcth", http.MethodGet, "/v1/hrm/promotion?at=2026-07-01&window=1", "", nil, http.StatusOK)
		items := r.body["items"].([]any)
		if r.body["as_of"] != "2026-07-01" || len(items) != 4 || r.body["summary"].(map[string]any)["nltx"] != json.Number("4") {
			t.Fatal("July list does not contain exactly four NLTX cases")
		}
		seen := map[string]bool{}
		for _, raw := range items {
			item := raw.(map[string]any)
			id := item["staff_id"].(string)
			index := slices.IndexFunc(people, func(p julyPromotion) bool { return p.id == id })
			if index < 0 || seen[id] {
				t.Fatal("July list contains an unexpected or duplicate staff")
			}
			seen[id] = true
			p := &people[index]
			if item["kind"] != "nltx" || item["cond"] != "ok" || item["due"] != p.due || item["grade"] != p.grade ||
				item["from_step"] != json.Number(strconv.Itoa(p.oldStep)) || item["to_step"] != json.Number(strconv.Itoa(p.newStep)) ||
				item["from_coeff"] != p.oldCoeff || item["to_coeff"] != p.newCoeff || item["diff"] != p.diff {
				t.Fatalf("July NLTX eligibility/tuple differs from fixed oracle for %s", id)
			}
			// The opaque case key is transport identity, never an expected outcome.
			p.key = item["key"].(string)
		}
		assertJulyProjection(t, scoped, people, false)
	})
	if t.Failed() {
		return
	}
	keys, notes := []string{}, map[string]string{}
	for _, p := range people {
		keys = append(keys, p.key)
		notes[p.key] = "Dữ liệu tổng hợp W11.31"
	}
	var filePath, exportID, expectedSHA string
	var originalFile []byte
	t.Run("July_Mau1_four_rows_twenty_columns", func(t *testing.T) {
		scoped := f.withTest(t)
		r := scoped.request("hcth", http.MethodPost, "/v1/hrm/promotion/exports", "hc30-july-export",
			map[string]any{"keys": keys, "review_month": "2026-07", "preparer_id": people[0].id, "signer_title": "Giám đốc", "notes": notes}, http.StatusCreated)
		if len(r.body["items"].([]any)) != 1 {
			t.Fatal("NLTX export must produce only Mẫu 1")
		}
		file := r.body["items"].([]any)[0].(map[string]any)
		if file["form"] != "mau1" || file["count"] != json.Number("4") || file["file_name"] != "MauSo1_T07_2026.xlsx" {
			t.Fatal("Mẫu 1 metadata differs from July four-person oracle")
		}
		exportID, filePath, expectedSHA = file["id"].(string), file["download"].(string), file["sha256"].(string)
		dl := scoped.request("hcth", http.MethodGet, filePath, "", nil, http.StatusOK)
		sum := sha256.Sum256(dl.raw)
		if hex.EncodeToString(sum[:]) != expectedSHA || dl.header.Get("X-Export-Drift") != "false" {
			t.Fatal("initial July file SHA or drift flag differs")
		}
		originalFile = bytes.Clone(dl.raw)
		assertJulyForm(t, dl.raw, people)
	})
	if t.Failed() {
		return
	}
	decisionPath := "/v1/hrm/promotion/decisions"
	var decisionID string
	decisionPeople := []map[string]string{}
	for _, p := range people {
		decisionPeople = append(decisionPeople, map[string]string{"case_key": p.key})
	}
	decisionBody := map[string]any{"export_id": exportID, "decision_no": "HC30/QD-TONG-HOP",
		"decided_on": "2026-07-25", "issuer": "Đơn vị tổng hợp kiểm thử", "people": decisionPeople}
	unchangedFile := func(t *testing.T, scoped *fixture) {
		t.Helper()
		dl := scoped.request("hcth", http.MethodGet, filePath, "", nil, http.StatusOK)
		sum := sha256.Sum256(dl.raw)
		if !bytes.Equal(dl.raw, originalFile) || hex.EncodeToString(sum[:]) != expectedSHA ||
			dl.header.Get("X-Export-Sha256") != expectedSHA || dl.header.Get("X-Export-Drift") != "false" {
			t.Fatal("historical July file changed after decision/compensation")
		}
	}
	t.Run("decision_updates_all_four_and_retains_historical_file", func(t *testing.T) {
		scoped := f.withTest(t)
		scoped.request("readonly", http.MethodPost, decisionPath, "hc30-july-readonly", decisionBody, http.StatusForbidden)
		r := scoped.request("hcth", http.MethodPost, decisionPath, "hc30-july-decision", decisionBody, http.StatusCreated)
		if r.body["updated"] != json.Number("4") || r.body["returned"] != json.Number("0") {
			t.Fatal("decision did not update exactly four profiles")
		}
		decisionID = r.body["id"].(string)
		replay := scoped.request("hcth", http.MethodPost, decisionPath, "hc30-july-decision", decisionBody, http.StatusCreated)
		if replay.body["id"] != decisionID {
			t.Fatal("decision retry did not replay the original decision")
		}
		assertJulyProjection(t, scoped, people, true)
		unchangedFile(t, scoped)
	})
	if t.Failed() {
		return
	}
	t.Run("cancel_compensates_four_profiles_and_preserves_issue_history", func(t *testing.T) {
		scoped := f.withTest(t)
		reason := "Hủy quyết định tổng hợp để kiểm sự kiện bù"
		scoped.request("hcth", http.MethodPost, decisionPath+"/"+decisionID+"/cancel", "hc30-july-cancel",
			map[string]any{"reason": reason}, http.StatusOK)
		assertJulyProjection(t, scoped, people, false)
		unchangedFile(t, scoped)
		d := scoped.request("hcth", http.MethodGet, decisionPath+"/"+decisionID, "", nil, http.StatusOK).body
		if d["status"] != "cancelled" || d["count"] != json.Number("4") || d["cancel_reason"] != reason || d["export_id"] != exportID {
			t.Fatal("cancelled decision lost original identity/count/export/reason")
		}
		history := d["history"].([]any)
		actions := []string{}
		for _, raw := range history {
			actions = append(actions, raw.(map[string]any)["action"].(string))
		}
		slices.Sort(actions)
		if !slices.Equal(actions, []string{"cancel", "issue"}) {
			t.Fatalf("decision history must retain issue and compensation, got %v", actions)
		}
		scoped.request("other_tenant", http.MethodGet, decisionPath+"/"+decisionID, "", nil, http.StatusNotFound)
		scoped.request("other_tenant", http.MethodGet, filePath, "", nil, http.StatusNotFound)
	})
}
