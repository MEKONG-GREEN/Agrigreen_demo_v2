package kn77_test

import (
	"bytes"
	"encoding/json"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/domain"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/docx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type adhocTestRequest struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	ReturnApplied *bool  `json:"return_applied"`
}

func (f *reportFx) openAdhoc(name string) khtcPeriod {
	f.t.Helper()
	var p khtcPeriod
	c, b := f.khtc(http.MethodPost, "/periods", f.khtcUser, "", map[string]any{"report_type": "adhoc", "year": 2026, "name": name, "from_date": "2026-10-01", "to_date": "2026-10-13", "due_at": "2026-10-20T10:00:00+07:00", "export_due_at": "2026-10-21T16:00:00+07:00", "departments": []string{dTT, dTTTT}})
	f.ok(c, b, &p)
	return p
}
func adhocRequestPath(id string) string {
	return "/v1/extension/khtc/periods/" + id + "/adhoc-requests"
}
func adhocRequestBody() map[string]any {
	return map[string]any{"department": dTT, "instruction": "Bổ sung số lớp tập huấn theo chỉ đạo 15", "due_at": "2026-10-19T10:00:00+07:00", "indicators": []map[string]string{{"code": "SO_LOP", "name": "Số lớp yêu cầu mới", "unit": "lớp"}}}
}
func (f *reportFx) request(pid, key string) adhocTestRequest {
	f.t.Helper()
	var r adhocTestRequest
	c, b := f.do(http.MethodPost, adhocRequestPath(pid), authz.KNTinh, khtcDept, f.khtcUser, key, adhocRequestBody())
	f.ok(c, b, &r)
	if r.State != "issued" {
		f.t.Fatal("request not issued", r)
	}
	return r
}
func answerSheet(token, value string) map[string]any {
	return map[string]any{"cells": map[string]any{}, "adjustments": []any{}, "texts": map[string]string{}, "adhoc_request_id": token, "adhoc_values": map[string]string{"SO_LOP": value}, "adhoc_text": "Phòng đã đối chiếu danh sách lớp."}
}

func TestW1168SupplementNativeTokenAndFrozenExports(t *testing.T) {
	f := newReportFx(t)
	withExporter(t, f)
	p := f.openAdhoc("Báo cáo phục vụ chỉ đạo")
	stale := answerSheet("", "5")
	r := f.request(p.ID, "request-one")
	c, b := f.send(p.ID, dTT, "submit", 0, stale)
	if c != 409 {
		t.Fatalf("stale opened form HTTP %d %s", c, b)
	}
	for _, raw := range []string{"1e100000", "1e-100000", strings.Repeat("9", 100000)} {
		c, b = f.send(p.ID, dTT, "submit", 0, answerSheet(r.ID, raw))
		if c != 422 {
			t.Fatalf("hostile len%d HTTP %d %s", len(raw), c, b)
		}
	}
	// Other departments cannot read or answer this assigned task.
	c, b = f.do(http.MethodGet, formPath(p.ID, dTT), authz.KNTinh, dCNTS, ids.New(), "", nil)
	if c != 403 {
		t.Fatalf("other dept read %d %s", c, b)
	}
	actor := ids.New()
	submit := map[string]any{"base_attempt": 0, "sheet": answerSheet(r.ID, "12.5")}
	c, b = f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "answer-one", submit)
	f.ok(c, b, nil)
	c, b = f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "answer-one", submit)
	f.ok(c, b, nil)
	changed := map[string]any{"base_attempt": 0, "sheet": answerSheet(r.ID, "99")}
	c, b = f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "answer-one", changed)
	if c != 422 {
		t.Fatalf("changed retry %d %s", c, b)
	}
	c, b = f.reviewForm(p.ID, dTT, "accepted", "Đã đối chiếu yêu cầu bổ sung")
	f.ok(c, b, nil)
	d := f.refresh(p.ID, 0)
	c, b = f.call(http.MethodGet, p.ID, "", nil)
	f.ok(c, b, nil)
	var content struct {
		Content domain.Draft `json:"content"`
	}
	if err := json.Unmarshal(b, &content); err != nil {
		t.Fatal(err)
	}
	key := "bo_sung:" + r.ID + ":SO_LOP"
	found := false
	for _, m := range content.Content.Data.Metrics {
		if m.Key == key {
			found = m.Value.Sum != nil && m.Value.Sum.String() == "12.5" && len(m.Trace) > 0 && m.Trace[0].FiguresHash != nil
		}
	}
	if !found {
		t.Fatalf("native provenance missing: %s", b)
	}
	c, b = f.call(http.MethodPost, p.ID, "/draft", map[string]any{"base_version": d.Version, "texts": map[string]string{"noi_dung": "Đánh giá báo cáo phát sinh."}, "admin": map[string]any{"basis": "Văn bản chỉ đạo 15"}, "adhoc": map[string]any{"rows": []string{}, "metrics": []string{key}}})
	f.ok(c, b, &d)
	x := f.export(p.ID, d.Version, false)
	frozen := f.download(p.ID, x.ID)
	book, err := excelize.OpenReader(bytes.NewReader(frozen))
	if err != nil {
		t.Fatal(err)
	}
	defer book.Close()
	rows, err := book.GetRows("Báo cáo")
	if err != nil {
		t.Fatal(err)
	}
	all := []string{}
	for _, row := range rows {
		all = append(all, strings.Join(row, "|"))
	}
	text := strings.Join(all, "\n")
	if !strings.Contains(text, "Số lớp yêu cầu mới") || !strings.Contains(text, "12.5") || !strings.Contains(text, "Văn bản chỉ đạo 15") {
		t.Fatalf("xlsx %s", text)
	}
	w := f.word(p.ID, x.ID, "word-adhoc")
	c, b = f.call(http.MethodGet, p.ID, "/words/"+w.ID+"/download", nil)
	f.ok(c, b, nil)
	word, err := docx.Extract(b)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(word.Text(), "Số lớp yêu cầu mới") || !strings.Contains(word.Text(), "12,5") || !strings.Contains(word.Text(), "Đánh giá báo cáo phát sinh") {
		t.Fatal(word.Text())
	}
	// A later request is a new namespace; old values cannot acknowledge it.
	var newer adhocTestRequest
	nextBody := adhocRequestBody()
	nextBody["department"] = dTTTT
	c, b = f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "request-two", nextBody)
	f.ok(c, b, &newer)
	c, b = f.send(p.ID, dTTTT, "submit", 0, answerSheet(r.ID, "12.5"))
	if c != 409 {
		t.Fatalf("old token accepted %d %s", c, b)
	}
	if newer.ID == r.ID {
		t.Fatal("request reused")
	}
	if !bytes.Equal(frozen, f.download(p.ID, x.ID)) {
		t.Fatal("historical export changed")
	}
	c, b = f.call(http.MethodPost, p.ID, "/exports", map[string]any{"base_version": d.Version, "acknowledge_provisional": true, "filter": map[string]any{}})
	if c != 409 {
		t.Fatalf("stale report exported %d %s", c, b)
	}
	c, b = f.do(http.MethodGet, reportPath(p.ID), authz.KNTW, khtcDept, ids.New(), "", nil)
	if c != 403 {
		t.Fatalf("TW draft %d %s", c, b)
	}
}
func TestW1168ConcurrentRequestsAndNativeLimit(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Đối chiếu yêu cầu song song")
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, key := range []string{"parallel-a", "parallel-b"} {
		wg.Add(1)
		go func(key string) {
			defer wg.Done()
			c, _ := f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, key, adhocRequestBody())
			codes <- c
		}(key)
	}
	wg.Wait()
	close(codes)
	success, conflict := 0, 0
	for c := range codes {
		if c == 201 {
			success++
		} else if c == 409 {
			conflict++
		} else {
			t.Fatalf("request race %d", c)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatalf("race %d/%d", success, conflict)
	}
	var list struct{ Items []adhocTestRequest }
	c, b := f.do(http.MethodGet, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	f.ok(c, b, &list)
	if len(list.Items) != 1 {
		t.Fatal(list)
	}
	r := list.Items[0]
	actor := ids.New()
	for attempt := int64(0); attempt < 3; attempt++ {
		c, b = f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "", map[string]any{"base_attempt": attempt, "sheet": answerSheet(r.ID, "1")})
		f.ok(c, b, nil)
		if attempt < 2 {
			r = f.request(p.ID, "req-next-"+ids.New().String())
			if r.ReturnApplied == nil || !*r.ReturnApplied {
				t.Fatal("native Return not observed", r)
			}
		}
	}
	c, b = f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "req-fourth", adhocRequestBody())
	if c != 409 {
		t.Fatalf("native 3 cap bypass %d %s", c, b)
	}
}
func TestW1168RequestScopesAndClosedPeriod(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Kiểm phạm vi yêu cầu")
	for _, k := range []struct{ role, dept string }{{authz.KNTW, khtcDept}, {authz.KNXa, dTT}, {authz.KNTinh, dCNTS}} {
		c, b := f.do(http.MethodPost, adhocRequestPath(p.ID), k.role, k.dept, ids.New(), "", adhocRequestBody())
		if c != 403 {
			t.Fatalf("role %s/%s %d %s", k.role, k.dept, c, b)
		}
	}
	r := f.request(p.ID, "same-key")
	altered := adhocRequestBody()
	altered["instruction"] = "Nội dung khác nhưng cùng khóa"
	c, b := f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "same-key", altered)
	if c != 422 {
		t.Fatalf("key altered %d %s", c, b)
	}
	original := f.tid
	f.tid = ids.New()
	c, b = f.do(http.MethodGet, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "", nil)
	if c != 404 {
		t.Fatalf("tenant leak %d %s", c, b)
	}
	f.tid = original
	f.clk.Advance(11 * 24 * time.Hour)
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{"reason": "Đóng sớm theo quyết định chỉ đạo"})
	f.ok(c, b, nil)
	c, b = f.do(http.MethodPost, adhocRequestPath(p.ID)+"/"+r.ID+"/reconcile", authz.KNTinh, khtcDept, f.khtcUser, "", map[string]any{})
	var cancelled adhocTestRequest
	f.ok(c, b, &cancelled)
	if cancelled.State != "cancelled" {
		t.Fatal(cancelled)
	}
	c, b = f.send(p.ID, dTT, "submit", 0, answerSheet(r.ID, "1"))
	if c < 400 {
		t.Fatalf("closed period reopened %d %s", c, b)
	}
	_ = uuid.Nil
}
