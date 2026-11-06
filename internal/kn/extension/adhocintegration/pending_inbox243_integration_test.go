package adhocintegration_test

import (
	"encoding/json"
	forms "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
)

type inboxRow243 struct {
	PeriodID     string `json:"period_id"`
	Department   string `json:"department"`
	SubmissionID string `json:"submission_id"`
	Attempt      int64  `json:"attempt"`
}
type inbox243 struct {
	Rows []inboxRow243 `json:"rows"`
	Next *string       `json:"next_cursor"`
}

func readInbox243(t *testing.T, f *kfFx, cursor string) inbox243 {
	t.Helper()
	code, body := f.khtc(http.MethodGet, "/pending-review"+cursor, f.khtcUser, "", nil)
	if code != http.StatusOK {
		t.Fatalf("native pending inbox: HTTP%d %s", code, body)
	}
	var out inbox243
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	if out.Rows == nil {
		t.Fatal("pending inbox rows must be an array")
	}
	return out
}

func TestKHTCPendingInbox243_NativeLatestReviewAndDraftIndependence(t *testing.T) {
	f := newKHTCFormFxWith(t, nil)
	p := f.open(40, dTT)
	code, body := f.send(p.ID, dTT, "draft", 0, ttSheet(f.item, "20"))
	f.ok(code, body, nil)
	if out := readInbox243(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("draft was counted as sent: %+v", out)
	}
	code, body = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "25"))
	f.ok(code, body, nil)
	out := readInbox243(t, f, "")
	if len(out.Rows) != 1 || out.Rows[0].PeriodID != p.ID || out.Rows[0].Department != dTT || out.Rows[0].Attempt != 1 || out.Rows[0].SubmissionID == "" {
		t.Fatalf("missing actual latest pending submission: %+v", out)
	}
	first := out.Rows[0].SubmissionID
	code, body = f.reviewForm241(p.ID, dTT, "returned", "Bổ sung số liệu kỳ kiểm thử inbox")
	f.ok(code, body, nil)
	if out := readInbox243(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("returned submission is not pending review: %+v", out)
	}
	code, body = f.send(p.ID, dTT, "draft", 1, ttSheet(f.item, "30"))
	f.ok(code, body, nil)
	if out := readInbox243(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("new draft is not a native submission: %+v", out)
	}
	code, body = f.send(p.ID, dTT, "submit", 1, ttSheet(f.item, "30"))
	f.ok(code, body, nil)
	out = readInbox243(t, f, "")
	if len(out.Rows) != 1 || out.Rows[0].Attempt != 2 || out.Rows[0].SubmissionID == first {
		t.Fatalf("resubmission must replace old target: %+v", out)
	}
	code, body = f.reviewForm241(p.ID, dTT, "accepted", "")
	f.ok(code, body, nil)
	if out := readInbox243(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("accepted submission stayed pending: %+v", out)
	}
	p = f.open(41, dTT)
	code, body = f.send(p.ID, dTT, "submit", 0, ttSheet(f.item, "35"))
	f.ok(code, body, nil)
	code, body = f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(code, body, nil)
	if out := readInbox243(t, f, ""); len(out.Rows) != 0 {
		t.Fatalf("closed period is not reviewable: %+v", out)
	}
	other := newKHTCFormFxWith(t, nil)
	if out := readInbox243(t, other, ""); len(out.Rows) != 0 {
		t.Fatalf("tenant leaked: %+v", out)
	}
}

func TestKHTCPendingInbox243_PagesEmptyWindowsAndCurrentAuthority(t *testing.T) {
	var probe *pendingProbe243
	f := newKHTCFormFxWith(t, func(port forms.PeriodControl) forms.PeriodControl {
		probe = &pendingProbe243{PeriodControl: port}
		return probe
	})
	first := f.open(1, dTT)
	code, body := f.send(first.ID, dTT, "submit", 0, ttSheet(f.item, "25"))
	f.ok(code, body, nil)
	for no := 2; no <= 27; no++ {
		f.open(no, dTT)
	}
	probe.calls = 0
	out := readInbox243(t, f, "")
	if len(out.Rows) != 0 || out.Next == nil {
		t.Fatalf("empty first page must retain next cursor: %+v", out)
	}
	if probe.calls != 25 {
		t.Fatalf("first-page native budget: got%d want25", probe.calls)
	}
	probe.calls = 0
	last := readInbox243(t, f, "?before="+*out.Next)
	if probe.calls != 2 {
		t.Fatalf("last-page native budget: got%d want2", probe.calls)
	}
	if len(last.Rows) != 1 || last.Rows[0].PeriodID != first.ID || last.Next != nil {
		t.Fatalf("later pending period lost or page truncation: %+v", last)
	}
	for _, role := range []struct{ role, dept string }{{authz.KNTinh, dTT}, {authz.KNXa, dTT}, {authz.KNTW, khtcDept}} {
		code, body := f.do(http.MethodGet, "/v1/extension/khtc/pending-review", role.role, role.dept, f.khtcUser, "", nil)
		if code != http.StatusForbidden {
			t.Fatalf("role %s/%s: want403 got%d %s", role.role, role.dept, code, body)
		}
	}
	for _, before := range []string{"not-uuid", "00000000-0000-0000-0000-000000000000", "1e100000"} {
		code, _ := f.khtc(http.MethodGet, "/pending-review?before="+before, f.khtcUser, "", nil)
		if code != http.StatusBadRequest {
			t.Fatalf("bad cursor %q: HTTP%d", before, code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/extension/khtc/pending-review", nil)
	raw, _ := json.Marshal(authz.KNContext{Role: authz.KNTinh, Department: khtcDept, OrgRef: f.node, Scope: authz.Scope{Units: []string{"X1"}}})
	request.Header.Set("X-Test-KN", string(raw))
	request.Header.Set("X-Test-Actor", f.khtcUser.String())
	response := httptest.NewRecorder()
	f.eng.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("pending response must be private/no-store: HTTP%d %v", response.Code, response.Header())
	}
}
