package app

import (
	"encoding/json"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"testing"
	"time"
)

func TestW1168DurableNativeReceiptStrictPayloadAndTerminal(t *testing.T) {
	f := newFx(t)
	node, actor := ids.New(), ids.New()
	admin := f.as(f.tid, ids.New(), "admin", node)
	ctx := f.as(f.tid, actor, "tinh", node, "U1", "U2")
	refs, e := f.svc.InstallFixtures(admin, "fx", "")
	f.must(e, "templates")
	other, e := f.svc.DefineTemplate(admin, TemplateInput{Profile: "prod", Code: "REAL-01", Title: "Native receipt other profile", Fields: refs[0].Fields}, "other-template")
	f.must(e, "other template")
	from, to := f.clk.Now(), f.clk.Now().Add(6*24*time.Hour)
	in := port.BatchInput{Profile: "fx", TemplateID: refs[0].ID, Units: []string{"U1"}, Periods: []port.PeriodDef{{Kind: "adhoc", From: from, To: to, DueAt: to.Add(time.Hour), Label: "Durable receipt"}}}
	publisher := Publisher{Svc: f.svc}
	old, e := publisher.PublishBatchOnce(ctx, in, "durable-strict")
	f.must(e, "first")
	clone := func() port.BatchInput {
		raw, _ := json.Marshal(in)
		var v port.BatchInput
		if e := json.Unmarshal(raw, &v); e != nil {
			t.Fatal(e)
		}
		return v
	}
	for _, mode := range []string{"label", "due", "units", "from", "to", "key", "template", "profile", "actor", "owner", "add-part"} {
		t.Run(mode, func(t *testing.T) {
			candidate, current := clone(), ctx
			switch mode {
			case "label":
				candidate.Periods[0].Label = "Changed"
			case "due":
				candidate.Periods[0].DueAt = candidate.Periods[0].DueAt.Add(time.Hour)
			case "units":
				candidate.Units = []string{"U2"}
			case "from":
				candidate.Periods[0].From = candidate.Periods[0].From.Add(-24 * time.Hour)
			case "to":
				candidate.Periods[0].To = candidate.Periods[0].To.Add(24 * time.Hour)
			case "key":
				candidate.Periods[0].Key = "different-part"
			case "template":
				candidate.TemplateID = refs[1].ID
			case "profile":
				candidate.Profile = "prod"
				candidate.TemplateID = other.ID
			case "actor":
				current = f.as(f.tid, ids.New(), "tinh", node, "U1")
			case "owner":
				current = f.as(f.tid, actor, "tinh", ids.New(), "U1")
			case "add-part":
				next := candidate.Periods[0]
				next.Key = "second"
				candidate.Periods = append(candidate.Periods, next)
			}
			_, e := publisher.PublishBatchOnce(current, candidate, "durable-strict")
			var p *problem.Problem
			if !errors.As(e, &p) || p.Status != 409 {
				t.Fatalf("changed batch reused key: %v", e)
			}
		})
	}
	e = (Control{Svc: f.svc}).ChangeDue(ctx, "fx", old[0].ID, to.Add(48*time.Hour), "Điều chỉnh hạn cho phép", "native-deadline")
	f.must(e, "change due")
	replay, e := publisher.PublishBatchOnce(ctx, in, "durable-strict")
	f.must(e, "original receipt after deadline")
	if len(replay) != 1 || replay[0].ID != old[0].ID || !replay[0].DueAt.Equal(old[0].DueAt) {
		t.Fatal("receipt was rebound to current deadline")
	}
	e = (Control{Svc: f.svc}).CancelPeriod(ctx, "fx", old[0].ID, "Dừng kỳ theo yêu cầu kiểm thử", "native-cancel")
	f.must(e, "cancel")
	replay, e = publisher.PublishBatchOnce(ctx, in, "durable-strict")
	f.must(e, "terminal receipt")
	if len(replay) != 1 || replay[0].ID != old[0].ID {
		t.Fatal("terminal replay created a new period")
	}
}
