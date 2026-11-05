package kn77_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// Capture the public identity actually rendered to the reviewer. Tests for old
// intents deliberately retain this value instead of refreshing the target.
func reviewTarget241(f *kfFx, period, department string) (string, int64) {
	f.t.Helper()
	code, body := f.do(http.MethodGet, formPath(period, department), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	var sheet struct {
		Submissions []struct {
			ID      string `json:"id"`
			Attempt int64  `json:"attempt"`
		} `json:"submissions"`
	}
	f.ok(code, body, &sheet)
	if len(sheet.Submissions) == 0 || sheet.Submissions[0].ID == "" || sheet.Submissions[0].Attempt < 1 {
		f.t.Fatalf("missing public review target: %s", body)
	}
	return sheet.Submissions[0].ID, sheet.Submissions[0].Attempt
}

func TestKHTCReview241_StaleRenderedSubmissionCannotReviewSuccessor(t *testing.T) {
	f := newKHTCFormFx(t)
	p := f.open(41, dTT)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(code, body, nil)
	firstID, firstAttempt := reviewTarget241(f, p.ID, dTT)

	code, body = f.reviewForm(p.ID, dTT, "returned", "Bổ sung số liệu")
	f.ok(code, body, nil)
	code, body = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "12"))
	f.ok(code, body, nil)
	secondID, secondAttempt := reviewTarget241(f, p.ID, dTT)
	if secondID == firstID || firstAttempt != 1 || secondAttempt != 2 {
		t.Fatalf("expected two distinct native submissions: %s/%d -> %s/%d", firstID, firstAttempt, secondID, secondAttempt)
	}

	code, body = f.do(http.MethodPost, formPath(p.ID, dTT)+"/review", authz.KNTinh, khtcDept, f.khtcUser, ids.New().String(),
		map[string]any{"decision": "accepted", "note": "", "expected_submission_id": firstID, "base_attempt": firstAttempt})

	// Check the business effect even if a broken producer notices a mismatched
	// ACK only after it has already reviewed the successor.
	freshCode, freshBody := f.do(http.MethodGet, formPath(p.ID, dTT), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	var current struct {
		Attempt     int64 `json:"attempt"`
		Submissions []struct {
			ID       string `json:"id"`
			Decision string `json:"decision"`
		} `json:"submissions"`
	}
	f.ok(freshCode, freshBody, &current)
	if current.Attempt != 2 || len(current.Submissions) == 0 || current.Submissions[0].ID != secondID {
		t.Fatalf("successor identity changed: %s", freshBody)
	}
	if current.Submissions[0].Decision != "" {
		t.Fatalf("stale rendered intent reviewed successor: decision=%s http=%d", current.Submissions[0].Decision, code)
	}
	if code != http.StatusConflict {
		t.Fatalf("stale rendered target must conflict, got %d: %s", code, body)
	}
	var problem struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(body, &problem); err != nil || problem.Code != "extension.khtc_form_not_reviewable" {
		t.Fatalf("wrong conflict: %s (%v)", body, err)
	}
}
