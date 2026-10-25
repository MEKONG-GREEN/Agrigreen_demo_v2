package inbox_test

// P0-POOL-SHARED-2 (#143): inbox.Process gắn bộ nhớ txdeps — I/O ngoài của handler (port module khác tự mở giao dịch trên
// CÙNG pool, kho đối tượng, SMTP) đi qua txdeps.Get chạy NGOÀI giao dịch inbox: pool CỠ 1 không tự khóa chết; handler
// chạy lại trong giao dịch mới chỉ ghi kết quả; giao lặp → không nạp lại.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/txdeps"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestProcessRunsExternalIOOutsideInboxTx(t *testing.T) {
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, "worker_platform", 1)
	tid := ids.New()
	e := outbox.Envelope{EventID: ids.New(), TenantID: tid, EventType: "pf.party.consent.recorded", Payload: json.RawMessage(`{}`)}
	runs, loads := 0, 0
	h := func(ctx context.Context, tx pgx.Tx, _ outbox.Envelope) error {
		runs++
		// "Port module khác": mở giao dịch riêng trên CÙNG pool cỡ 1 — trong giao dịch inbox sẽ chờ mãi.
		n, err := txdeps.Get(ctx, "other-module", func(ctx context.Context) (int, error) {
			loads++
			if got, err := tenant.FromContext(ctx); err != nil || got != tid {
				t.Errorf("nạp ngoài giao dịch phải mang tenant của phong bì: %s %v", got, err)
			}
			var v int
			err := tenant.WithTenantTx(ctx, sized, tid, func(tx pgx.Tx) error { return tx.QueryRow(ctx, "SELECT 42").Scan(&v) })
			return v, err
		})
		if err != nil {
			return err
		}
		var echo int
		return tx.QueryRow(ctx, "SELECT $1::int", n).Scan(&echo)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	processed, err := inbox.Process(ctx, sized, "txdeps.consumer", e, h)
	if err != nil || !processed {
		t.Fatalf("processed=%v err=%v", processed, err)
	}
	if runs != 2 || loads != 1 {
		t.Fatalf("handler chạy %d lần (muốn 2), nạp %d lần (muốn 1)", runs, loads)
	}
	if processed, err := inbox.Process(ctx, sized, "txdeps.consumer", e, h); err != nil || processed || loads != 1 {
		t.Fatalf("giao lặp: processed=%v err=%v loads=%d", processed, err, loads)
	}
}

// Review #143 N6(c): handler đòi khóa mới mỗi lần chạy → sau inbox.MaxTxAttempts lần trả txdeps.ErrExhausted (NACK, giao
// lại), không ghi dòng inbox, không vòng lặp vô hạn.
func TestProcessExhaustedAfterMaxAttempts(t *testing.T) {
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, "worker_platform", 1)
	e := outbox.Envelope{EventID: ids.New(), TenantID: ids.New(), EventType: "pf.party.consent.recorded", Payload: json.RawMessage(`{}`)}
	runs := 0
	h := func(ctx context.Context, _ pgx.Tx, _ outbox.Envelope) error {
		runs++
		_, err := txdeps.Get(ctx, fmt.Sprint("khoa-", runs), func(context.Context) (int, error) { return runs, nil })
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	processed, err := inbox.Process(ctx, sized, "txdeps.exhaust", e, h)
	if !errors.Is(err, txdeps.ErrExhausted) || processed || runs != inbox.MaxTxAttempts {
		t.Fatalf("processed=%v err=%v runs=%d (muốn %d)", processed, err, runs, inbox.MaxTxAttempts)
	}
	// Không có dòng inbox: giao lại xử lý lại từ đầu.
	runs = 0
	if _, err := inbox.Process(ctx, sized, "txdeps.exhaust", e, h); !errors.Is(err, txdeps.ErrExhausted) || runs != inbox.MaxTxAttempts {
		t.Fatalf("giao lại: err=%v runs=%d", err, runs)
	}
}
