package kn77_test

import (
	"context"
	"encoding/json"
	"strings"

	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/google/uuid"
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

type reviewEntry241 struct {
	formsport.Entry
	calls    int
	before   func()
	wrongACK bool
}

func (e *reviewEntry241) Review(ctx context.Context, profile string, id uuid.UUID, decision, note, idem string) (formsport.SubmissionRef, error) {
	e.calls++
	if e.before != nil {
		before := e.before
		e.before = nil
		before()
	}
	ack, err := e.Entry.Review(ctx, profile, id, decision, note, idem)
	if err == nil && e.wrongACK {
		ack.Version++
	}
	return ack, err
}

func TestKHTCReview241_ValidIntentBoundsACKAndReplay(t *testing.T) {
	var entry *reviewEntry241
	f := newKHTCFormFxPorts(t, nil, func(native formsport.Entry) formsport.Entry {
		entry = &reviewEntry241{Entry: native}
		return entry
	})
	p := f.open(41, dTT)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(code, body, nil)
	id, attempt := reviewTarget241(f, p.ID, dTT)
	for _, tc := range []struct {
		name   string
		body   map[string]any
		status int
	}{
		{"old-client", map[string]any{"decision": "accepted"}, 422},
		{"bad-id", map[string]any{"decision": "accepted", "expected_submission_id": "bad", "base_attempt": attempt}, 400},
		{"zero-id", map[string]any{"decision": "accepted", "expected_submission_id": uuid.Nil, "base_attempt": attempt}, 422},
		{"zero-attempt", map[string]any{"decision": "accepted", "expected_submission_id": id, "base_attempt": 0}, 422},
		{"large-exponent", map[string]any{"decision": "accepted", "expected_submission_id": id, "base_attempt": json.RawMessage("1e100000")}, 400},
		{"tiny-exponent", map[string]any{"decision": "accepted", "expected_submission_id": id, "base_attempt": json.RawMessage("1e-100000")}, 400},
		{"long-integer", map[string]any{"decision": "accepted", "expected_submission_id": id, "base_attempt": json.RawMessage(strings.Repeat("9", 10000))}, 400},
		{"wrong-type", map[string]any{"decision": "accepted", "expected_submission_id": id, "base_attempt": "1"}, 400},
		{"mismatched-attempt", map[string]any{"decision": "accepted", "expected_submission_id": id, "base_attempt": attempt + 1}, 409},
		{"foreign-target", map[string]any{"decision": "accepted", "expected_submission_id": ids.New(), "base_attempt": attempt}, 409},
		{"unknown-decision", map[string]any{"decision": "confirmed", "expected_submission_id": id, "base_attempt": attempt}, 422},
		{"required-note", map[string]any{"decision": "returned", "note": "  ", "expected_submission_id": id, "base_attempt": attempt}, 422},
		{"note-1001-unicode", map[string]any{"decision": "accepted", "note": strings.Repeat("🌾", 1001), "expected_submission_id": id, "base_attempt": attempt}, 422},
		{"body-bound", map[string]any{"decision": "accepted", "note": strings.Repeat("x", 20000), "expected_submission_id": id, "base_attempt": attempt}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, b := f.do(http.MethodPost, formPath(p.ID, dTT)+"/review", authz.KNTinh, khtcDept, f.khtcUser, ids.New().String(), tc.body)
			if c != tc.status {
				t.Fatalf("got %d want %d: %s", c, tc.status, b)
			}
			if entry.calls != 0 {
				t.Fatalf("rejected intent reached native review: %d", entry.calls)
			}
		})
	}
	note := strings.Repeat("🌾", 1000)
	in := map[string]any{"decision": "returned", "note": "  " + note + "  ", "expected_submission_id": id, "base_attempt": attempt}
	key := ids.New().String()
	code, body = f.do(http.MethodPost, formPath(p.ID, dTT)+"/review", authz.KNTinh, khtcDept, f.khtcUser, key, in)
	var ack struct {
		ID          string `json:"reviewed_submission_id"`
		Attempt     int64  `json:"reviewed_attempt"`
		Decision    string `json:"review_decision"`
		RequestNote string `json:"request_note"`
	}
	f.ok(code, body, &ack)
	if ack.ID != id || ack.Attempt != attempt || ack.Decision != "returned" || ack.RequestNote != note || entry.calls != 1 {
		t.Fatalf("actual native ACK not returned: %+v calls=%d", ack, entry.calls)
	}
	code, replayed := f.do(http.MethodPost, formPath(p.ID, dTT)+"/review", authz.KNTinh, khtcDept, f.khtcUser, key, in)
	if code != 200 || string(replayed) != string(body) || entry.calls != 1 {
		t.Fatalf("same intent/key must replay exact ACK without a second native decision: %d %s", code, replayed)
	}
	code, body = f.reviewForm(p.ID, dTT, "accepted", "")
	if code != 409 || entry.calls != 1 {
		t.Fatalf("already reviewed target must not be decided twice: %d %s", code, body)
	}
}

func TestKHTCReview241_NativeRejectsSupersededAfterRead(t *testing.T) {
	var entry *reviewEntry241
	f := newKHTCFormFxPorts(t, nil, func(native formsport.Entry) formsport.Entry { entry = &reviewEntry241{Entry: native}; return entry })
	p := f.open(41, dTT)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(code, body, nil)
	id, attempt := reviewTarget241(f, p.ID, dTT)
	entry.before = func() {
		c, b := f.reviewForm(p.ID, dTT, "returned", "Bổ sung trước khi tác vụ cũ đến native")
		f.ok(c, b, nil)
		c, b = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "12"))
		f.ok(c, b, nil)
	}
	code, body = f.do(http.MethodPost, formPath(p.ID, dTT)+"/review", authz.KNTinh, khtcDept, f.khtcUser, ids.New().String(), map[string]any{"decision": "accepted", "expected_submission_id": id, "base_attempt": attempt})
	if code != 409 {
		t.Fatalf("native must reject superseded target after KHTC read: %d %s", code, body)
	}
	var current struct {
		Submissions []struct {
			ID       string
			Decision string
		}
	}
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(code, body, &current)
	if len(current.Submissions) != 2 || current.Submissions[0].ID == id || current.Submissions[0].Decision != "" {
		t.Fatalf("native must leave successor unreviewed: %s", body)
	}
}

func TestKHTCReview241_MismatchedNativeACKCannotClaimSuccess(t *testing.T) {
	f := newKHTCFormFxPorts(t, nil, func(native formsport.Entry) formsport.Entry { return &reviewEntry241{Entry: native, wrongACK: true} })
	p := f.open(41, dTT)
	code, body := f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "10"))
	f.ok(code, body, nil)
	code, body = f.reviewForm(p.ID, dTT, "accepted", "")
	if code != 503 || strings.Contains(string(body), "reviewed_submission_id") {
		t.Fatalf("mismatched native ACK must stay unresolved: %d %s", code, body)
	}
	var current struct{ Submissions []struct{ Decision string } }
	code, body = f.do(http.MethodGet, formPath(p.ID, dTT), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(code, body, &current)
	if len(current.Submissions) != 1 || current.Submissions[0].Decision != "accepted" {
		t.Fatalf("the native write occurred; a late bad ACK must not be treated as a no-op: %s", body)
	}
}
