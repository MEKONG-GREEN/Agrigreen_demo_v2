package kn77_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/google/uuid"
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
		old.Content.Texts["cong_tac_khac"] != "Tập huấn 2 lớp IPM" {
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

type historyEntry242 struct {
	formsport.Entry
	reads, writes int
	alter         func([]formsport.SubmissionRef)
}

func (e *historyEntry242) Submissions(ctx context.Context, profile string, period uuid.UUID, dept string) ([]formsport.SubmissionRef, error) {
	e.reads++
	rows, err := e.Entry.Submissions(ctx, profile, period, dept)
	if err == nil && e.alter != nil {
		e.alter(rows)
	}
	return rows, err
}
func (e *historyEntry242) Submit(ctx context.Context, profile string, period uuid.UUID, dept string, values map[string]string, base int64, idem string) (formsport.SubmissionRef, error) {
	e.writes++
	return e.Entry.Submit(ctx, profile, period, dept, values, base, idem)
}
func (e *historyEntry242) Review(ctx context.Context, profile string, id uuid.UUID, decision, note, idem string) (formsport.SubmissionRef, error) {
	e.writes++
	return e.Entry.Review(ctx, profile, id, decision, note, idem)
}

func TestKHTCHistory242_CurrentAuthorityAndTargetBinding(t *testing.T) {
	var entry *historyEntry242
	f := newKHTCFormFxPorts(t, nil, func(native formsport.Entry) formsport.Entry {
		entry = &historyEntry242{Entry: native}
		return entry
	})
	p := f.open(41, dTT, dCNTS)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(code, body, nil)
	id, _ := reviewTarget241(f, p.ID, dTT)
	endpoint := formPath(p.ID, dTT) + "/submissions/" + id
	baseline := entry.writes
	for _, tc := range []struct {
		name, path, role, dept string
		want                   int
	}{
		{"reviewer", endpoint, authz.KNTinh, khtcDept, 200},
		{"own-department", endpoint, authz.KNTinh, dTT, 200},
		{"admin", endpoint, authz.KNAdmin, "", 200},
		{"other-department", endpoint, authz.KNTinh, dCNTS, 403},
		{"commune", endpoint, authz.KNXa, dTT, 403},
		{"national-reports-only", endpoint, authz.KNTW, khtcDept, 403},
		{"other-submission", formPath(p.ID, dTT) + "/submissions/" + ids.New().String(), authz.KNTinh, khtcDept, 404},
		{"other-form", formPath(p.ID, dCNTS) + "/submissions/" + id, authz.KNTinh, khtcDept, 404},
		{"unknown-query", endpoint + "?version=1", authz.KNTinh, khtcDept, 404},
		{"duplicate-query", endpoint + "?department=trong_trot&department=hanh_chinh_tong_hop", authz.KNTinh, khtcDept, 404},
		{"exponent-id", formPath(p.ID, dTT) + "/submissions/1e100000", authz.KNTinh, khtcDept, 404},
		{"tiny-id", formPath(p.ID, dTT) + "/submissions/1e-100000", authz.KNTinh, khtcDept, 404},
		{"long-id", formPath(p.ID, dTT) + "/submissions/" + strings.Repeat("9", 10000), authz.KNTinh, khtcDept, 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, b := f.do(http.MethodGet, tc.path, tc.role, tc.dept, f.khtcUser, "", nil)
			if c != tc.want {
				t.Fatalf("HTTP %d want %d: %s", c, tc.want, b)
			}
			if c != 200 && strings.Contains(string(b), `"content_hash"`) {
				t.Fatal("rejected resource leaked historical payload")
			}
		})
	}
	originalTenant := f.tid
	f.tid = ids.New()
	code, body = f.do(http.MethodGet, endpoint, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.tid = originalTenant
	if code != 404 {
		t.Fatalf("cross-tenant history: HTTP %d %s", code, body)
	}

	var list struct {
		Period struct {
			ID string `json:"id"`
		} `json:"period"`
		Department string `json:"department"`
		Items      []struct {
			ID        string `json:"id"`
			Submitter string `json:"submitter_ref"`
		} `json:"items"`
	}
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT)+"/submissions", authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(code, body, &list)
	if list.Period.ID != p.ID || list.Department != dTT || len(list.Items) != 1 || list.Items[0].ID != id || list.Items[0].Submitter == "" ||
		strings.Contains(string(body), `"values"`) || strings.Contains(string(body), `"content"`) {
		t.Fatalf("history headers must be native scoped metadata only: %s", body)
	}
	if entry.writes != baseline {
		t.Fatal("history read performed a native write")
	}
}

func TestKHTCHistory242_RejectsMismatchedNativeAndSealedReferences(t *testing.T) {
	var entry *historyEntry242
	f := newKHTCFormFxPorts(t, nil, func(native formsport.Entry) formsport.Entry {
		entry = &historyEntry242{Entry: native}
		return entry
	})
	p := f.open(41, dTT)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(code, body, nil)
	id, _ := reviewTarget241(f, p.ID, dTT)
	for _, tc := range []struct {
		name  string
		alter func([]formsport.SubmissionRef)
		want  int
	}{
		{"wrong-unit", func(s []formsport.SubmissionRef) { s[0].Unit = dCNTS }, 503},
		{"wrong-version", func(s []formsport.SubmissionRef) { s[0].Version++ }, 404},
		{"wrong-actor", func(s []formsport.SubmissionRef) { s[0].Submitter = ids.New() }, 404},
		{"missing-sheet", func(s []formsport.SubmissionRef) { s[0].Values = map[string]string{} }, 404},
		{"unbound-sheet", func(s []formsport.SubmissionRef) { s[0].Values = map[string]string{"bang_so_lieu": ids.New().String()} }, 404},
		{"missing-review-actor", func(s []formsport.SubmissionRef) { s[0].Decision = "accepted" }, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry.alter = tc.alter
			c, b := f.do(http.MethodGet, formPath(p.ID, dTT)+"/submissions/"+id, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
			entry.alter = nil
			if c != tc.want || strings.Contains(string(b), `"content"`) {
				t.Fatalf("mismatched binding HTTP %d want %d: %s", c, tc.want, b)
			}
		})
	}
}

func TestKHTCHistory242_RecordedPlanSurvivesNewVersion(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(code, body, nil)
	id, _ := reviewTarget241(f, p.ID, dTT)
	var before historySnapshot242
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT)+"/submissions/"+id, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(code, body, &before)
	var entries []map[string]any
	for _, old := range f.planBody["entries"].([]map[string]any) {
		row := map[string]any{}
		for k, v := range old {
			row[k] = v
		}
		if row["ref"] == f.item {
			row["name"] = "Ten hang muc phien ban moi"
		}
		entries = append(entries, row)
	}
	in := map[string]any{"base_version": 1, "decision_no": "242/QD", "decision_date": "2026-10-13", "effective_from": "2026-10-14", "entries": entries}
	code, body = f.do(http.MethodPost, "/v1/kn/khtc/plans/2026/versions", authz.KNTinh, khtcDept, f.khtcUser, "", in)
	f.ok(code, body, nil)
	var current struct {
		Plan struct {
			VersionID string `json:"version_id"`
		} `json:"plan"`
	}
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(code, body, &current)
	if before.PlanVersionRef == nil || current.Plan.VersionID == "" || current.Plan.VersionID == *before.PlanVersionRef {
		t.Fatal("fixture must actually select a different current plan")
	}
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT)+"/submissions/"+id, authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	var after historySnapshot242
	f.ok(code, body, &after)
	beforeRows, _ := json.Marshal(before.Rows)
	afterRows, _ := json.Marshal(after.Rows)
	if after.PlanVersionRef == nil || *after.PlanVersionRef != *before.PlanVersionRef || string(beforeRows) != string(afterRows) || after.ContentHash != before.ContentHash {
		t.Fatalf("current plan contaminated historical labels/content: before=%+v after=%+v", before, after)
	}
}
