package kn77_test

// P0-POOL-SHARED-2 — review #143 C1: "Hủy kỳ" KHTC.
//   - Lời gọi cap/forms CancelPeriod có hạn TRƯỚC khi ý định hủy hết hạn (không treo quá hạn rồi để Bản cuối chen vào).
//   - FinishCancel kiểm token còn giữ ý định còn hạn; mất ý định → nhận lại ý định nếu chưa có Bản cuối, có Bản cuối →
//     409 extension.khtc_period_cancel_interrupted (cảnh báo đối soát).
//   - cap/forms lỗi → AbortCancel trả ý định ngay (Bản cuối không phải chờ hết hạn).
//   - "Đóng kỳ" trong lúc ý định hủy còn hạn → 409 cancelling, không chốt kỳ native.

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	formsport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/forms/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// hookControl bọc cổng điều khiển kỳ native cap/forms: cancel (nếu đặt) thay CancelPeriod, next gọi cổng thật.
type hookControl struct {
	formsport.PeriodControl
	mu     sync.Mutex
	cancel func(ctx context.Context, next func(context.Context) error) error
}

func (h *hookControl) set(fn func(ctx context.Context, next func(context.Context) error) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cancel = fn
}

func (h *hookControl) CancelPeriod(ctx context.Context, profile string, id uuid.UUID, reason, idem string) error {
	h.mu.Lock()
	fn := h.cancel
	h.mu.Unlock()
	next := func(ctx context.Context) error { return h.PeriodControl.CancelPeriod(ctx, profile, id, reason, idem) }
	if fn == nil {
		return next(ctx)
	}
	return fn(ctx, next)
}

func newHookedReportFx(t *testing.T) (*reportFx, *hookControl) {
	h := &hookControl{}
	f := newReportFxWith(t, func(c formsport.PeriodControl) formsport.PeriodControl { h.PeriodControl = c; return h })
	return f, h
}

// intentSQL chạy câu lệnh trên khtc_cancel_intent của kỳ ($1 tenant, $2 kỳ) bằng vai quản trị; SELECT trả một số.
func intentSQL(t *testing.T, f *reportFx, pid, q string) int {
	t.Helper()
	n := 0
	if err := tenant.WithTenantTx(context.Background(), pgtest.Get(t).AdminPool(t, migrations.PF), f.tid, func(tx pgx.Tx) error {
		if strings.HasPrefix(q, "SELECT") {
			return tx.QueryRow(context.Background(), q, f.tid, pid).Scan(&n)
		}
		_, err := tx.Exec(context.Background(), q, f.tid, pid)
		return err
	}); err != nil {
		t.Error(err)
	}
	return n
}

const (
	sqlLiveIntents = `SELECT count(*)::int FROM pf_extension.khtc_cancel_intent WHERE tenant_id = $1 AND period_id = $2::uuid AND claim_id IS NOT NULL`
	sqlExpire      = `UPDATE pf_extension.khtc_cancel_intent SET claim_until = now() - interval '1 second' WHERE tenant_id = $1 AND period_id = $2::uuid`
	sqlHoldIntent  = `INSERT INTO pf_extension.khtc_cancel_intent (tenant_id, period_id, claim_id, claim_until)
	  VALUES ($1, $2::uuid, gen_random_uuid(), now() + interval '1 minute')`
)

// C1: cap/forms CancelPeriod chậm bị cắt TRƯỚC hạn ý định hủy (hạn 4 s, biên 1 s), trả 503; ý định được trả ngay.
func TestP0PoolShared2_KHTCCancelFormsDeadlineBeforeLease(t *testing.T) {
	f, h := newHookedReportFx(t)
	const lease = 4 * time.Second
	f.ext.UseKHTCCancelLease(lease)
	p := f.open(41, dTT, dTTTT)
	var mu sync.Mutex
	var stopped time.Duration
	hadDeadline := false
	start := time.Now()
	h.set(func(ctx context.Context, _ func(context.Context) error) error {
		_, dl := ctx.Deadline()
		select {
		case <-ctx.Done():
		case <-time.After(3 * lease): // không có hạn → treo tới đây (đỏ)
		}
		mu.Lock()
		stopped, hadDeadline = time.Since(start), dl
		mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("fixture: cap/forms treo")
	})
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ khi cap/forms đang chậm"})
	mu.Lock()
	got, dl := stopped, hadDeadline
	mu.Unlock()
	if !dl || got >= lease {
		t.Fatalf("P0-POOL-SHARED-2 C1: CancelPeriod dừng sau %s (có hạn=%v), muốn trước hạn ý định %s", got, dl, lease)
	}
	f.fail(c, b, "extension.dependency_unavailable")
	if n := intentSQL(t, f, p.ID, sqlLiveIntents); n != 0 {
		t.Fatalf("ý định hủy chưa được trả sau lỗi: %d", n)
	}
	h.set(nil)
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy lại sau khi cap/forms ổn"})
	f.ok(c, b, nil)
	if st := f.get(p.ID); st.State != "cancelled" || st.FormsStatus != "cancelled" {
		t.Fatalf("hủy lại: %+v", st)
	}
}

// C1/N6: cap/forms lỗi → AbortCancel trả ý định ngay — Bản cuối không bị chặn tới hết hạn.
func TestP0PoolShared2_KHTCCancelAbortAfterFormsError(t *testing.T) {
	f, h := newHookedReportFx(t)
	p := f.open(41, dTT, dTTTT)
	f.submit(p.ID, "10")
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, false)
	h.set(func(context.Context, func(context.Context) error) error {
		return errors.New("fixture: cap/forms lỗi")
	})
	c, _ := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ khi cap/forms lỗi"})
	if c < 500 {
		t.Fatalf("cap/forms lỗi: %d", c)
	}
	if n := intentSQL(t, f, p.ID, sqlLiveIntents); n != 0 {
		t.Fatalf("ý định hủy còn sau lỗi cap/forms: %d", n)
	}
	c, b := f.call(http.MethodPost, p.ID, "/exports/"+x.ID+"/final", map[string]any{})
	f.ok(c, b, &d)
	if st := f.get(p.ID); st.State == "cancelled" || st.FormsStatus == "cancelled" {
		t.Fatalf("hủy lỗi vẫn đổi trạng thái: %+v", st)
	}
}

// C1: ý định hết hạn SAU khi cap/forms đã hủy, chưa có Bản cuối → nhận lại ý định, ghi sự kiện hủy (nhất quán).
func TestP0PoolShared2_KHTCCancelLostIntentRecovered(t *testing.T) {
	f, h := newHookedReportFx(t)
	p := f.open(41, dTT, dTTTT)
	h.set(func(ctx context.Context, next func(context.Context) error) error {
		if err := next(ctx); err != nil {
			return err
		}
		intentSQL(t, f, p.ID, sqlExpire)
		return nil
	})
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ, ý định hết hạn giữa chừng"})
	f.ok(c, b, nil)
	if st := f.get(p.ID); st.State != "cancelled" || st.FormsStatus != "cancelled" {
		t.Fatalf("sau khi nhận lại ý định: %+v", st)
	}
	if n := intentSQL(t, f, p.ID, sqlLiveIntents); n != 0 {
		t.Fatalf("ý định còn giữ: %d", n)
	}
}

// C1: ý định hết hạn sau khi cap/forms đã hủy và Bản cuối chen vào → KHÔNG ghi sự kiện hủy (bất biến: sổ KHTC không vừa
// hủy vừa có Bản cuối), 409 extension.khtc_period_cancel_interrupted để đối soát.
func TestP0PoolShared2_KHTCCancelLostIntentFinalWins(t *testing.T) {
	f, h := newHookedReportFx(t)
	p := f.open(41, dTT, dTTTT)
	f.submit(p.ID, "10")
	d := f.refresh(p.ID, 0)
	x := f.export(p.ID, d.Version, false)
	finalCode := 0
	h.set(func(ctx context.Context, next func(context.Context) error) error {
		if err := next(ctx); err != nil {
			return err
		}
		intentSQL(t, f, p.ID, sqlExpire)
		finalCode, _ = f.call(http.MethodPost, p.ID, "/exports/"+x.ID+"/final", map[string]any{})
		return nil
	})
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ, Bản cuối chen vào khi ý định hết"})
	if finalCode >= 300 {
		t.Fatalf("Bản cuối sau khi ý định hết hạn: %d", finalCode)
	}
	f.fail(c, b, "extension.khtc_period_cancel_interrupted")
	if st := f.get(p.ID); st.State == "cancelled" {
		t.Fatalf("sổ KHTC ghi hủy dù đã có Bản cuối: %+v", st)
	}
	if f.read(p.ID).Final == nil {
		t.Fatal("Bản cuối mất")
	}
}

// N6: "Đóng kỳ" trong lúc ý định hủy còn hạn → 409 cancelling, kỳ native KHÔNG bị chốt; hết hạn → đóng được.
func TestP0PoolShared2_KHTCCloseBlockedByCancelIntent(t *testing.T) {
	f := newReportFx(t)
	p := f.open(41, dTT, dTTTT)
	f.submit(p.ID, "10")
	f.refresh(p.ID, 0)
	intentSQL(t, f, p.ID, sqlHoldIntent)
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{})
	f.fail(c, b, "extension.khtc_period_cancelling")
	if st := f.get(p.ID); st.State == "closed" || st.FormsStatus == "closed" {
		t.Fatalf("đóng kỳ chạy khi đang hủy: %+v", st)
	}
	intentSQL(t, f, p.ID, sqlExpire)
	c, b = f.khtc(http.MethodPost, "/periods/"+p.ID+"/close", f.khtcUser, "", map[string]any{})
	f.ok(c, b, nil)
}
