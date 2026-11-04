package app_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	rs "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/khtcreport/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms"
	fp "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	fixture "github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/fixtures/khtcdeadline209"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestClose211PrePrepareFailureRecoversOriginalOperation(t *testing.T) {
	for _, mode := range []string{"cancelled_context", "transient_owner_policy", "renewed_unresolved_session"} {
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
					if mode != "transient_owner_policy" {
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
			if mode == "renewed_unresolved_session" {
				principal, _ := authz.FromContext(ctx)
				k := *principal.KN
				k.ID = uuid.New()
				k.ExpiresAt = f.clk.Now().Add(time.Hour)
				principal.KN = &k
				principal.SelectedSession = nil
				ctx = authz.WithPrincipal(context.Background(), fixture.Bind(t, principal))
			}
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

// Count real native effects/anchors, not transport attempts. The supplier's
// OperationID lock is responsible for deduplication of exact-command retries.
func nativeCountsFix211(t *testing.T, f *deadlineFx209) [3]int {
	t.Helper()
	pool := pgtest.Get(t).AdminPool(t, migrations.PF)
	var out [3]int
	if err := pool.QueryRow(context.Background(), `SELECT
 (SELECT count(*) FROM pf_forms.period_event WHERE tenant_id=$1),
 (SELECT count(*) FROM pf_forms.close_snapshot WHERE tenant_id=$1),
 (SELECT count(*) FROM pf_forms.period_operation_key WHERE tenant_id=$1)`, f.tid).Scan(&out[0], &out[1], &out[2]); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestClose211RenewedSessionCannotAdoptChangedAuthority(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	probe := &receiptProbe211{PeriodOperations: svc.Operations}
	svc.Operations = probe
	if _, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "reject-renew211"); err != nil {
		t.Fatal(err)
	}
	original, _ := authz.FromContext(ctx)
	before := nativeCountsFix211(t, f)
	calls, reads := probe.calls, probe.reads
	for _, mode := range []string{"assignment", "org", "scope", "roles", "noMFA", "expired"} {
		principal := original
		k := *principal.KN
		k.ID = uuid.New()
		k.ExpiresAt = f.clk.Now().Add(time.Hour)
		switch mode {
		case "assignment":
			k.AssignmentID = uuid.New()
		case "org":
			k.OrgRef = uuid.New()
		case "scope":
			k.Scope = authz.Scope{Units: []string{"X2"}}
		case "roles":
			principal.Roles = []string{"changed-realm-role211"}
		case "noMFA":
			principal.MFA = false
		case "expired":
			k.ExpiresAt = f.clk.Now().Add(-time.Second)
		}
		principal.KN = &k
		principal.SelectedSession = nil
		principal = fixture.Bind(t, principal)
		_, err := svc.Close(authz.WithPrincipal(context.Background(), principal), id, "Đóng kỳ ngoại lệ đủ lý do", "reject-renew211")
		var pe *problem.Problem
		if !errors.As(err, &pe) || (pe.Status != 403 && pe.Status != 409) {
			t.Fatalf("%s changed authority replay not rejected: %v", mode, err)
		}
		if nativeCountsFix211(t, f) != before || probe.calls != calls || probe.reads != reads {
			t.Fatal("changed authority/expired session reached supplier or wrote native effects")
		}
	}
}

// Missing retained receipt is a read-boundary fault. The real permanent key,
// native ledger and append-only protections remain present and unchanged.
type missingReceiptFix211 struct{ tenant.Beginner }
type missingReceiptTxFix211 struct{ pgx.Tx }
type missingReceiptRowFix211 struct{}

func (missingReceiptRowFix211) Scan(...any) error { return pgx.ErrNoRows }
func (b missingReceiptFix211) BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error) {
	tx, e := b.Beginner.BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	return missingReceiptTxFix211{tx}, nil
}
func (tx missingReceiptTxFix211) QueryRow(ctx context.Context, q string, args ...any) pgx.Row {
	if q == "SELECT content,recorded_at FROM pf_forms.period_operation_receipt WHERE tenant_id=$1 AND id=$2 ORDER BY seq LIMIT 1" {
		return missingReceiptRowFix211{}
	}
	return tx.Tx.QueryRow(ctx, q, args...)
}
func TestClose211PermanentKeyWithoutReceiptNeverRepeatsEffects(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	var original fp.PeriodOperationRequest
	probe := &receiptProbe211{PeriodOperations: svc.Operations, before: func(_ context.Context, in fp.PeriodOperationRequest) { original = in }, after: func(_ fp.PeriodOperationReceipt, e error) (fp.PeriodOperationReceipt, error) {
		if e != nil {
			return fp.PeriodOperationReceipt{}, e
		}
		return fp.PeriodOperationReceipt{}, errors.New("lost applied ACK211")
	}}
	svc.Operations = probe
	_, err := svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "purged-receipt-fix211")
	assertConflict209(t, err)
	beforeCounts := nativeCountsFix211(t, f)
	beforeOutbox := f.outboxCount209(t)
	masked, err := forms.New(missingReceiptFix211{f.fm.Pool}, f.clk, forms.Deps{Profiles: extension.FormProfiles()})
	if err != nil {
		t.Fatal(err)
	}
	if err = masked.UsePeriodOperationPolicy("kn_khtc", svc.CloseOperationPolicy211); err != nil {
		t.Fatal(err)
	}
	retry := &receiptProbe211{PeriodOperations: masked.PeriodOperations(), before: func(_ context.Context, in fp.PeriodOperationRequest) {
		if in != original {
			t.Error("key retention retry replaced original command")
		}
	}}
	svc.Operations = retry
	for i := 0; i < 2; i++ {
		_, err = svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "purged-receipt-fix211")
		assertConflict209(t, err)
	}
	var reserved bool
	if err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error { var e error; reserved, e = rs.CloseReserved211(ctx, tx, f.tid, id); return e }); err != nil {
		t.Fatal(err)
	}
	if retry.calls != 2 || phaseClose211(t, f) != "unresolved" || !reserved || nativeCountsFix211(t, f) != beforeCounts || f.outboxCount209(t) != beforeOutbox {
		t.Fatal("unknown with permanent anchor repeated effects or released reservation")
	}
}

func TestClose211UnpreparedOriginalRejectsSuccessorAndReleasesFence(t *testing.T) {
	f := newDeadlineFx209(t)
	p := f.open209()
	id := uuid.MustParse(p.ID)
	f.clk.Advance(p.DueAt.Sub(f.clk.Now()) + 4*24*time.Hour)
	svc, ctx := closeConsumer211(t, f, f.fm.ConditionalControl())
	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var original fp.PeriodOperationRequest
	probe := &receiptProbe211{PeriodOperations: svc.Operations, before: func(_ context.Context, in fp.PeriodOperationRequest) {
		if original.OperationID == uuid.Nil {
			original = in
			cancel()
		} else if in != original {
			t.Error("successor recovery replaced original Expected")
		}
	}}
	svc.Operations = probe
	_, err := svc.Close(reqCtx, id, "Đóng kỳ ngoại lệ đủ lý do", "successor-preprepare211")
	assertConflict209(t, err)
	successor, err := f.fm.ConditionalControl().ChangeDueCAS(ctx, "kn_khtc", original.PeriodID, original.Expected, p.DueAt.Add(time.Hour), "Hạn của thao tác kế tiếp211", "successor-preprepare-native211")
	if err != nil {
		t.Fatal(err)
	}
	before := nativeCountsFix211(t, f)
	_, err = svc.Close(ctx, id, "Đóng kỳ ngoại lệ đủ lý do", "successor-preprepare211")
	assertConflict209(t, err)
	current, err := f.fm.ConditionalControl().ControlSnapshot(ctx, "kn_khtc", original.PeriodID)
	if err != nil {
		t.Fatal(err)
	}
	var reserved bool
	if err = tenant.WithTenantTx(ctx, f.roleDB, f.tid, func(tx pgx.Tx) error { var e error; reserved, e = rs.CloseReserved211(ctx, tx, f.tid, id); return e }); err != nil {
		t.Fatal(err)
	}
	after := nativeCountsFix211(t, f)
	if reserved || phaseClose211(t, f) != "rejected" || current.Revision != successor || current.Progress.Status != "open" || after[0] != before[0] || after[1] != before[1] || after[2] != before[2]+1 {
		t.Fatal("original retry consumed successor or failed to release durably rejected fence")
	}
}
