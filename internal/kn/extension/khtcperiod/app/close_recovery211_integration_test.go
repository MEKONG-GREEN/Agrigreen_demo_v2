package app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	fp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"github.com/google/uuid"
)

func TestClose211PrePrepareFailureRecoversOriginalOperation(t *testing.T) {
	for _, mode := range []string{"cancelled_context", "transient_owner_policy"} {
		t.Run(mode, func(t *testing.T) {
			f := newDeadlineFx209(t)
			p := f.open209()
			id := uuid.MustParse(p.ID)
			f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
			svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
			reqCtx, cancel := context.WithCancel(ctx)
			defer cancel()
			policyFailed := false
			if mode == "transient_owner_policy" {
				if err := f.fm.UsePeriodOperationPolicy("kn_khtc", func(c context.Context, p authz.Principal, r fp.PeriodOperationRequest) (fp.PeriodOperationGrant, error) {
					if policyFailed {
						return fp.PeriodOperationGrant{}, errors.New("temporary owner policy unavailable211")
					}
					return svc.CloseOperationPolicy211(c, p, r)
				}); err != nil {
					t.Fatal(err)
				}
			}
			var original fp.PeriodOperationRequest
			probe := &receiptProbe211{PeriodOperations: svc.Operations, before: func(_ context.Context, r fp.PeriodOperationRequest) {
				if original.OperationID == uuid.Nil {
					original = r
					if mode == "cancelled_context" {
						cancel()
					} else {
						policyFailed = true
					}
				}
				if !reflect.DeepEqual(original, r) {
					t.Fatal("recovery replaced immutable original operation/Expected")
				}
			}}
			svc.Operations = probe
			_, err := svc.Close(reqCtx, id, "Đóng kỳ ngoại lệ đủ lý do", "pre-prepare211")
			assertConflict209(t, err)
			if phaseClose211(t, f) != "unresolved" {
				t.Fatal("pre-prepare fault not reached")
			}
			before, err := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", uuid.MustParse(p.FormsPeriodID))
			if err != nil {
				t.Fatal(err)
			}
			if before.Progress.Status != "open" || before.Revision != original.Expected {
				t.Fatal("pre-prepare failure had native effect")
			}
			policyFailed = false
			out, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "pre-prepare211")
			if err != nil || out.State != "closed" || phaseClose211(t, f) != "completed" {
				t.Fatalf("live retry cannot recover original operation: %v", err)
			}
			calls, outbox := probe.calls, f.outboxCount209(t)
			again, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "pre-prepare211")
			if err != nil || !reflect.DeepEqual(out, again) || probe.calls != calls || f.outboxCount209(t) != outbox {
				t.Fatal("completed replay repeated effects or changed stored outcome")
			}
		})
	}
}

func TestClose211RenewedSessionRetainsStableAuthority(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	out, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "renew-session211")
	if err != nil {
		t.Fatal(err)
	}
	original, _ := authz.FromContext(ctx)
	for _, mode := range []string{"expiry_renewal", "relogin"} {
		p := original
		k := *p.KN
		k.ExpiresAt = f.clk.Now().Add(24 * time.Hour)
		if mode == "relogin" {
			k.ID = uuid.New()
		}
		p.KN = &k
		p.SelectedSession = nil
		p = fixture.Bind(t, p)
		again, err := svc.Close(authz.WithPrincipal(context.Background(), p), id, "Đóng kỳ ngoại lệ đủ lý do", "renew-session211")
		if err != nil || !reflect.DeepEqual(out, again) || probe.calls != 1 {
			t.Fatalf("%s lost stable owner replay: %v", mode, err)
		}
	}
}
