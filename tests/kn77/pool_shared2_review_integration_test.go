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
	"github.com/jackc/pgx/v5/pgxpool"

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

// intentUntil đọc claim_until của ý định hủy ĐANG GIỮ của kỳ (giờ DB) — hạn thật mà Bản cuối/đóng kỳ phải chờ.
func intentUntil(f *reportFx, adm *pgxpool.Pool, pid string) (time.Time, error) {
	var until time.Time
	err := tenant.WithTenantTx(context.Background(), adm, f.tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT claim_until FROM pf_extension.khtc_cancel_intent
		  WHERE tenant_id = $1 AND period_id = $2::uuid AND claim_id IS NOT NULL`, f.tid, pid).Scan(&until)
	})
	return until, err
}

// C1: cap/forms CancelPeriod chậm bị cắt TRƯỚC hạn ý định hủy (hạn 4 s, biên 1 s), trả 503; ý định được trả ngay.
//
// TEST-143-DEFLAKE (#153): không đo đồng hồ từ một mốc phía test (máy tải → độ trễ trước giao dịch nhận và độ trễ fake
// nhận ra ctx hủy cộng vào số đo, đỏ giả). Kiểm trực tiếp bất biến: hạn ctx mà CancelPeriod nhận phải sớm hơn claim_until
// THẬT của ý định (đọc từ DB trong lúc ý định còn giữ) ít nhất cancelDeadlineSlack, và lời gọi dừng vì chính hạn đó —
// kể cả khi giao dịch nhận phải chờ khóa lâu (hạn neo trước giao dịch nhận, không phải lúc commit).
func TestP0PoolShared2_KHTCCancelFormsDeadlineBeforeLease(t *testing.T) {
	f, h := newHookedReportFx(t)
	const lease = 4 * time.Second
	// Biên sản phẩm = min(lease/4, 15 s) = 1 s cộng thời gian giao dịch nhận; một nửa biên đủ phân biệt với hạn không biên
	// (hạn = mốc + lease ≈ claim_until) mà không phụ thuộc tải máy.
	const cancelDeadlineSlack = 500 * time.Millisecond
	f.ext.UseKHTCCancelLease(lease)
	p := f.open(41, dTT, dTTTT)
	adm := pgtest.Get(t).AdminPool(t, migrations.PF) // mở trên goroutine test (hook chạy ở goroutine máy chủ)
	var mu sync.Mutex
	var (
		deadline   time.Time
		hasDL      bool
		claimUntil time.Time
		untilErr   error
		stopErr    error
	)
	h.set(func(ctx context.Context, _ func(context.Context) error) error {
		dl, ok := ctx.Deadline()
		until, uerr := intentUntil(f, adm, p.ID) // ý định đã commit trước lời gọi cap/forms
		select {
		case <-ctx.Done():
		case <-time.After(3 * lease): // không có hạn → treo tới đây (đỏ)
		}
		mu.Lock()
		deadline, hasDL, claimUntil, untilErr, stopErr = dl, ok, until, uerr, ctx.Err()
		mu.Unlock()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("fixture: cap/forms treo")
	})
	// Giữ khóa khtc_snapshot của kỳ 2 s: giao dịch nhận ý định (BeginCancel) mở giao dịch — claim_until = now() (đầu giao
	// dịch) + hạn — rồi phải chờ khóa. Hạn neo TRƯỚC giao dịch nhận (đúng) vẫn sớm hơn claim_until một biên; hạn neo SAU khi
	// giao dịch nhận commit thì muộn hơn ~1 s → đỏ.
	hold, err := adm.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hold.Exec(context.Background(), `SELECT pg_advisory_xact_lock(hashtextextended('khtc_snapshot:' || $1::uuid::text || ':' || $2::uuid::text, 0))`, f.tid, p.ID); err != nil {
		_ = hold.Rollback(context.Background())
		t.Fatal(err)
	}
	held := make(chan struct{})
	go func() {
		defer close(held)
		time.Sleep(2 * time.Second)
		_ = hold.Rollback(context.Background())
	}()
	c, b := f.khtc(http.MethodPost, "/periods/"+p.ID+"/cancel", f.khtcUser, "", map[string]any{"reason": "Hủy kỳ khi cap/forms đang chậm"})
	<-held
	mu.Lock()
	dl, ok, until, uerr, serr := deadline, hasDL, claimUntil, untilErr, stopErr
	mu.Unlock()
	if uerr != nil {
		t.Fatalf("đọc claim_until của ý định hủy: %v", uerr)
	}
	if !ok || !dl.Before(until.Add(-cancelDeadlineSlack)) {
		t.Fatalf("P0-POOL-SHARED-2 C1: hạn CancelPeriod %s (có hạn=%v) phải sớm hơn hạn ý định claim_until %s ít nhất %s (còn %s)",
			dl.Format(time.RFC3339Nano), ok, until.Format(time.RFC3339Nano), cancelDeadlineSlack, until.Sub(dl))
	}
	if !errors.Is(serr, context.DeadlineExceeded) {
		t.Fatalf("P0-POOL-SHARED-2 C1: CancelPeriod phải dừng vì hạn của chính nó, ctx.Err()=%v", serr)
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
