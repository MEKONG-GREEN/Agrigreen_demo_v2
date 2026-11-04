package app_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	khtcpapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcperiod/app"
	reportapp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/app"
	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
)

type legacyCloseProbe211 struct {
	formsport.PeriodControl
	before func(context.Context)
}

func (p legacyCloseProbe211) ClosePeriod(ctx context.Context, profile string, id uuid.UUID, key string) error {
	if p.before != nil {
		p.before(ctx)
	}
	return p.PeriodControl.ClosePeriod(ctx, profile, id, key)
}

func TestClose211HeldOriginalCannotCloseSuccessor(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	native := f.fm.ConditionalControl()
	var once sync.Once
	successor := func(ctx context.Context) {
		once.Do(func() {
			snap, err := native.ControlSnapshot(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID))
			if err != nil {
				t.Fatal(err)
			}
			if _, err = native.ChangeDueCAS(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID), snap.Revision, p.DueAt.Add(time.Hour), "successor close211", "successor-close211"); err != nil {
				t.Fatal(err)
			}
		})
	}
	probe := &casProbe209{ConditionalPeriodControl: native}
	probe.snapshotHook = func(ctx context.Context, _ formsport.PeriodControlSnapshot) { successor(ctx) }
	svc, ctx := closeConsumer211(t, f, probe)
	svc.Control = legacyCloseProbe211{PeriodControl: f.fm.Control(), before: successor}
	_, err := svc.Close(ctx, uuid.MustParse(p.ID), "đóng ngoại lệ để kiểm thử CAS", "held-close211")
	assertConflict209(t, err)
	snap, err := native.ControlSnapshot(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Progress.Status != "open" {
		t.Fatalf("old close changed successor native status: %s", snap.Progress.Status)
	}
}

func closeConsumer211(t *testing.T, f *deadlineFx209, control formsport.ConditionalPeriodControl) (*khtcpapp.Service, context.Context) {
	t.Helper()
	svc, ctx := f.consumer209(control)
	svc.Operations = f.fm.PeriodOperations()
	svc.CloseGuard = &reportapp.Service{DB: f.roleDB, Clock: f.clk}
	if err := f.fm.UsePeriodOperationPolicy("kn_khtc", svc.CloseOperationPolicy211); err != nil {
		t.Fatal(err)
	}
	return svc, ctx
}

type receiptProbe211 struct {
	formsport.PeriodOperations
	after        func(formsport.PeriodOperationReceipt, error) (formsport.PeriodOperationReceipt, error)
	calls, reads int
}

func (p *receiptProbe211) ExecutePeriodOperation(ctx context.Context, in formsport.PeriodOperationRequest) (formsport.PeriodOperationReceipt, error) {
	p.calls++
	r, e := p.PeriodOperations.ExecutePeriodOperation(ctx, in)
	if p.after != nil {
		return p.after(r, e)
	}
	return r, e
}
func (p *receiptProbe211) ReadPeriodOperation(ctx context.Context, in formsport.PeriodOperationRequest) (formsport.PeriodOperationReceipt, error) {
	p.reads++
	return p.PeriodOperations.ReadPeriodOperation(ctx, in)
}
func phaseClose211(t *testing.T, f *deadlineFx209) string {
	t.Helper()
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	var phase string
	if err := pool.QueryRow(context.Background(), `SELECT phase FROM pf_extension.khtc_close_intent211 WHERE tenant_id=$1 ORDER BY seq DESC LIMIT 1`, f.tid).Scan(&phase); err != nil {
		t.Fatal(err)
	}
	return phase
}
func TestClose211ReceiptReplayForcedAndMissingPorts(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	out, err := svc.Close(ctx, uuid.MustParse(p.ID), "Đóng kỳ ngoại lệ đủ lý do", "close-forced211")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "closed" || !out.Events[len(out.Events)-1].Forced || out.Events[len(out.Events)-1].Reason == nil {
		t.Fatal("forced close lost local decision")
	}
	before := f.outboxCount209(t)
	f.clk.Advance(time.Hour)
	again, err := svc.Close(ctx, uuid.MustParse(p.ID), "Đóng kỳ ngoại lệ đủ lý do", "close-forced211")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(out)
	b, _ := json.Marshal(again)
	if !reflect.DeepEqual(a, b) || probe.calls != 1 || f.outboxCount209(t) != before {
		t.Fatal("not own immutable replay")
	}
	if phaseClose211(t, f) != "completed" {
		t.Fatal("phase not completed")
	}
	svc.Operations = nil
	_, err = svc.Close(ctx, uuid.MustParse(p.ID), "Đóng kỳ ngoại lệ đủ lý do", "no-port211")
	if err == nil {
		t.Fatal("missing port accepted")
	}
}
func TestClose211LostNativeACKReadsReceiptNeverReexecutes(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	probe.after = func(r formsport.PeriodOperationReceipt, e error) (formsport.PeriodOperationReceipt, error) {
		if e != nil {
			return r, e
		}
		return formsport.PeriodOperationReceipt{Outcome: formsport.PeriodOperationUnknown}, errors.New("lost native commit ACK211")
	}
	svc.Operations = probe
	_, err := svc.Close(ctx, uuid.MustParse(p.ID), "Đóng kỳ ngoại lệ đủ lý do", "lost-ack211")
	if err == nil || phaseClose211(t, f) != "unresolved" {
		t.Fatal("unknown claimed complete")
	}
	before := f.outboxCount209(t)
	out, err := svc.Close(ctx, uuid.MustParse(p.ID), "Đóng kỳ ngoại lệ đủ lý do", "lost-ack211")
	if err != nil {
		t.Fatal(err)
	}
	if out.State != "closed" || probe.calls != 1 || probe.reads != 1 || f.outboxCount209(t) != before {
		t.Fatal("recovery reexecuted native effect")
	}
}
