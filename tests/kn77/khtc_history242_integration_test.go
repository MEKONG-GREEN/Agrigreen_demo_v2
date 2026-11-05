package kn77_test

import (
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

type historySnapshot242 struct {
	Period struct {
		ID string `json:"id"`
	} `json:"period"`
	Department string `json:"department"`
	Submission struct {
		ID         string  `json:"id"`
		Attempt    int64   `json:"attempt"`
		Submitter  string  `json:"submitter_ref"`
		Reviewer   *string `json:"reviewer_ref"`
		Decision   string  `json:"decision"`
		ReviewNote string  `json:"review_note"`
	} `json:"submission"`
	SheetID        string  `json:"sheet_id"`
	ContentHash    string  `json:"content_hash"`
	PlanVersionRef *string `json:"plan_version_ref"`
	Content        struct {
		Cells map[string]struct {
			Done string `json:"done"`
		} `json:"cells"`
		Texts map[string]string `json:"texts"`
	} `json:"content"`
	Rows []struct {
		EntryRef string `json:"entry_ref"`
		Name     string `json:"name"`
	} `json:"rows"`
}

func TestKHTCHistory242_SelectedSubmissionKeepsSealedContent(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	sender := ids.New()
	code, body := f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, sender, "",
		map[string]any{"base_attempt": 0, "sheet": ttSheet(f.item, "10")})
	f.ok(code, body, nil)
	firstID, _ := reviewTarget241(f, p.ID, dTT)
	code, body = f.reviewForm(p.ID, dTT, "returned", "Bo sung lai so lieu")
	f.ok(code, body, nil)
	code, body = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "12"))
	f.ok(code, body, nil)
	secondID, _ := reviewTarget241(f, p.ID, dTT)

	code, body = f.do(http.MethodGet, formPath(p.ID, dTT)+"/submissions/"+firstID,
		authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	if code != http.StatusOK {
		t.Fatalf("selected historical submission must be readable, got HTTP %d: %s", code, body)
	}
	var old historySnapshot242
	f.ok(code, body, &old)
	if old.Period.ID != p.ID || old.Department != dTT || old.Submission.ID != firstID || old.Submission.Attempt != 1 ||
		old.Content.Cells[f.item].Done != "10" || old.Submission.Submitter != sender.String() ||
		old.Submission.Reviewer == nil || *old.Submission.Reviewer != f.khtcUser.String() ||
		old.Submission.Decision != "returned" || old.Submission.ReviewNote != "Bo sung lai so lieu" ||
		old.SheetID == "" || len(old.ContentHash) != 64 || old.PlanVersionRef == nil ||
		old.Content.Texts["cong_tac_khac"] != "Tap huan 2 lop IPM" && old.Content.Texts["cong_tac_khac"] == "" {
		t.Fatalf("historical identity/content/actors must come from the selected sealed/native records: %+v", old)
	}
	found := false
	for _, row := range old.Rows {
		if row.EntryRef == f.item && row.Name != "" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing historical plan row label")
	}

	code, body = f.do(http.MethodGet, formPath(p.ID, dTT)+"/submissions/"+secondID,
		authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	var current historySnapshot242
	f.ok(code, body, &current)
	if current.Submission.ID != secondID || current.Submission.Attempt != 2 || current.Content.Cells[f.item].Done != "12" ||
		current.SheetID == old.SheetID || current.ContentHash == old.ContentHash {
		t.Fatalf("second submission must remain a separate immutable value: %+v", current)
	}
	// A new unsubmitted draft must not replace either historical payload.
	code, body = f.reviewForm(p.ID, dTT, "returned", "Can doi chieu")
	f.ok(code, body, nil)
	code, body = f.send(p.ID, dTT, "draft", 2, ttSheet(f.item, "99"))
	f.ok(code, body, nil)
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT)+"/submissions/"+firstID,
		authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	var again historySnapshot242
	f.ok(code, body, &again)
	if again.Content.Cells[f.item].Done != "10" || again.ContentHash != old.ContentHash || again.SheetID != old.SheetID {
		t.Fatalf("new draft contaminated historical payload: %+v", again)
	}
}
