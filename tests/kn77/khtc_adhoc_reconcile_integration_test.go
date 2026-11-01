package kn77_test

import (
	"context"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/google/uuid"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type adhocUncertainEntry struct {
	formsport.Entry
	reviewBefore, reviewAfter, submitAfter atomic.Bool
	reviewCalls, submitCalls               atomic.Int64
	reviewEntered                          chan struct{}
	reviewRelease                          chan struct{}
}

func (e *adhocUncertainEntry) Review(ctx context.Context, profile string, id uuid.UUID, decision, note, key string) (formsport.SubmissionRef, error) {
	e.reviewCalls.Add(1)
	if e.reviewEntered != nil {
		close(e.reviewEntered)
		select {
		case <-e.reviewRelease:
		case <-ctx.Done():
			return formsport.SubmissionRef{}, ctx.Err()
		}
	}
	if e.reviewBefore.Swap(false) {
		return formsport.SubmissionRef{}, context.DeadlineExceeded
	}
	out, err := e.Entry.Review(ctx, profile, id, decision, note, key)
	if err == nil && e.reviewAfter.Swap(false) {
		return formsport.SubmissionRef{}, problem.Forbidden.WithDetail("response lost after native committed")
	}
	return out, err
}
func (e *adhocUncertainEntry) Submit(ctx context.Context, profile string, id uuid.UUID, unit string, values map[string]string, base int64, key string) (formsport.SubmissionRef, error) {
	e.submitCalls.Add(1)
	out, err := e.Entry.Submit(ctx, profile, id, unit, values, base, key)
	if err == nil && e.submitAfter.Swap(false) {
		return formsport.SubmissionRef{}, context.DeadlineExceeded
	}
	return out, err
}
func (f *reportFx) replaceEntry(entry formsport.Entry) {
	f.ext.UseKHTCPeriods(extension.KHTCDeps{Forms: f.nativeForms, Control: f.nativeControl, Entry: entry, MaxSubmissions: 3})
}
func TestW1168ReconcileLostNativeResponses(t *testing.T) {
	for _, mode := range []string{"before-return", "after-return", "after-submit"} {
		t.Run(mode, func(t *testing.T) {
			f := newReportFx(t)
			p := f.openAdhoc("Đối chiếu phản hồi thất lạc")
			r := f.request(p.ID, "initial-request")
			e := &adhocUncertainEntry{Entry: f.nativeEntry}
			f.replaceEntry(e)
			actor := ids.New()
			if mode == "after-submit" {
				e.submitAfter.Store(true)
				body := map[string]any{"base_attempt": 0, "sheet": answerSheet(r.ID, "4")}
				c, b := f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "lost-submit", body)
				if c < 500 {
					t.Fatalf("expected unknown %d %s", c, b)
				}
				c, b = f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "lost-submit", body)
				f.ok(c, b, nil)
				if e.submitCalls.Load() != 1 {
					t.Fatal("native Submit repeated", e.submitCalls.Load())
				}
				form := f.deptForm(p.ID, dTT)
				if form.Attempt != 1 {
					t.Fatal("duplicate native submission", form.Attempt)
				}
			} else {
				c, b := f.do(http.MethodPost, formPath(p.ID, dTT)+"/submit", authz.KNTinh, dTT, actor, "", map[string]any{"base_attempt": 0, "sheet": answerSheet(r.ID, "4")})
				f.ok(c, b, nil)
				if mode == "before-return" {
					e.reviewBefore.Store(true)
				} else {
					e.reviewAfter.Store(true)
				}
				c, b = f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "lost-return", adhocRequestBody())
				if mode == "before-return" {
					if c < 500 {
						t.Fatalf("expected unknown %d %s", c, b)
					}
					// Same logical request and immutable payload retries the intent.
					c, b = f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "lost-return", adhocRequestBody())
				}
				var delivered adhocTestRequest
				f.ok(c, b, &delivered)
				if delivered.State != "issued" || delivered.ReturnApplied == nil || !*delivered.ReturnApplied {
					t.Fatal("successful Return hidden", delivered)
				}
				form := f.deptForm(p.ID, dTT)
				if form.State != "supplement" || form.Attempt != 1 {
					t.Fatalf("native state %+v", form)
				}
				expected := int64(1)
				if mode == "before-return" {
					expected = 2
				}
				if e.reviewCalls.Load() != expected {
					t.Fatal("unexpected Return retry", e.reviewCalls.Load())
				}
			}
		})
	}
}
func TestW1168RequestCloseRaceNeverReopensNative(t *testing.T) {
	f := newReportFx(t)
	p := f.openAdhoc("Đóng kỳ đang phát hành yêu cầu")
	r := f.request(p.ID, "first-request")
	c, b := f.send(p.ID, dTT, "submit", 0, answerSheet(r.ID, "1"))
	f.ok(c, b, nil)
	e := &adhocUncertainEntry{Entry: f.nativeEntry, reviewEntered: make(chan struct{}), reviewRelease: make(chan struct{})}
	f.replaceEntry(e)
	result := make(chan int, 1)
	go func() {
		c, _ := f.do(http.MethodPost, adhocRequestPath(p.ID), authz.KNTinh, khtcDept, f.khtcUser, "racing-close", adhocRequestBody())
		result <- c
	}()
	select {
	case <-e.reviewEntered:
	case <-time.After(20 * time.Second):
		t.Fatal("Return did not start")
	}
	f.clk.Advance(11 * 24 * time.Hour)
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{"reason": "Đóng kỳ ngoại lệ theo quyết định chỉ đạo"})
	f.ok(c, b, nil)
	close(e.reviewRelease)
	select {
	case c := <-result:
		if c < 400 {
			t.Fatal("request unexpectedly issued after close", c)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("request held transaction across native I/O")
	}
	c, b = f.send(p.ID, dTT, "submit", 1, answerSheet(r.ID, "2"))
	if c < 400 {
		t.Fatalf("native period reopened %d %s", c, b)
	}
}
func TestW1168AdminPublishesThroughOwnedIntent(t *testing.T) {
	f := newReportFx(t)
	c, b := f.do(http.MethodPost, "/v1/extension/khtc/periods", authz.KNAdmin, "", f.khtcUser, "", map[string]any{"report_type": "adhoc", "year": 2026, "name": "Admin tạo phát sinh", "from_date": "2026-10-01", "to_date": "2026-10-13", "due_at": "2026-10-20T10:00:00+07:00", "export_due_at": "2026-10-21T16:00:00+07:00", "departments": []string{dTT}})
	if c != 201 {
		t.Fatalf("owned Admin publish %d %s", c, b)
	}
}
