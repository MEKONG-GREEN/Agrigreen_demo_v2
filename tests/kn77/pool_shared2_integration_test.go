package kn77_test

// P0-POOL-SHARED-2 (#143): tiếp #126/#139 cho consumer inbox/outbox và hai chỗ KN bàn giao từ #139.
//   - Gửi Chi cục (pf.reports.dispatch.requested): lượt gửi (nhận việc, đọc kho, SMTP, ghi dấu) chạy NGOÀI giao dịch inbox
//     — pool chung cỡ 2 (gốc lắp ráp thật) không còn kết nối nào bị mượn trong lúc SMTP treo.
//   - Hủy kỳ KHTC: khóa chống "Bản cuối" không còn giữ giao dịch quanh Get/HasFinal/cap/forms CancelPeriod (giao dịch lồng
//     nhau) — hủy song song nhiều kỳ trên pool chung cỡ 2 xong trong hạn.

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports"
	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
)

// gateMailer chặn mọi lần gửi tới khi test mở; đếm theo địa chỉ.
type gateMailer struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	n       map[string]int
}

func newGateMailer(t *testing.T) *gateMailer {
	m := &gateMailer{entered: make(chan struct{}, 16), release: make(chan struct{}), n: map[string]int{}}
	t.Cleanup(m.open)
	return m
}

func (m *gateMailer) open() { m.once.Do(func() { close(m.release) }) }

func (m *gateMailer) Send(ctx context.Context, msg repport.Message) error {
	m.entered <- struct{}{}
	select {
	case <-m.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n[msg.To]++
	return nil
}

func (m *gateMailer) total() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := 0
	for _, v := range m.n {
		t += v
	}
	return t
}

func TestP0PoolShared2_DispatchSendOutsideInboxTx(t *testing.T) {
	c := setupCycleWith(t, fixtureOpts{sharedPool: 2})
	f := c.f
	pool := f.repPool.(*pgxpool.Pool)
	did, rids := dispatchWithTwoRecipients(t, c)
	m := newGateMailer(t)
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi email")
	evs := f.events(repport.EventDispatchRequested)
	ev := evs[len(evs)-1]
	inboxDB := db.ForModule(pool, reports.DBModule)

	done := make(chan error, 1)
	go func() {
		_, err := inbox.Process(context.Background(), inboxDB, repport.DispatchConsumer, ev, f.rep.DispatchHandler())
		done <- err
	}()
	select {
	case <-m.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("Mailer.Send không được gọi")
	}
	// SMTP đang treo: không giao dịch nào (inbox hay giao dịch riêng của lượt gửi) giữ kết nối của pool chung.
	if n := pool.Stat().AcquiredConns(); n != 0 {
		t.Fatalf("P0-POOL-SHARED-2: gửi SMTP Chi cục trong lúc giữ %d kết nối pool (giao dịch inbox mở)", n)
	}
	m.open()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if m.total() != 2 {
		t.Fatalf("mỗi người nhận một email: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sent'
	    AND claim_token IS NULL AND claim_until IS NULL`, f.tenant, did); n != 1 {
		t.Fatal("trạng thái sent, đã nhả việc")
	}
	// Giao lặp (cùng event_id) → inbox bỏ qua; event_id khác → lượt đã xong, không gửi thêm.
	if processed, err := inbox.Process(context.Background(), inboxDB, repport.DispatchConsumer, ev, f.rep.DispatchHandler()); err != nil || processed {
		t.Fatalf("giao lặp: processed=%v err=%v", processed, err)
	}
	dup := ev
	dup.EventID = ids.New()
	if _, err := inbox.Process(context.Background(), inboxDB, repport.DispatchConsumer, dup, f.rep.DispatchHandler()); err != nil {
		t.Fatal(err)
	}
	if m.total() != 2 {
		t.Fatalf("giao lặp gửi trùng: %v", m.n)
	}
}

// TestP0PoolShared2_KHTCCancelNoNestedTx: hủy SONG SONG nhiều kỳ KHTC trên pool chung cỡ 2 (pf/extension + cap/forms). Trước
// sửa: GuardCancel giữ giao dịch khóa trong khi Get/HasFinal/CancelPeriod mở giao dịch riêng → cạn pool, lời gọi treo.
func TestP0PoolShared2_KHTCCancelNoNestedTx(t *testing.T) {
	const poolSize = 2
	env := pgtest.Get(t)
	f := newKHTCFxPool(t, sharedUnitPool(t, env, poolSize))
	tt, cn := partyport.DeptTrongTrot, partyport.DeptChanNuoiThuySan
	c, b := f.do(http.MethodPost, "/v1/extension/khtc/form-template/install", authz.KNAdmin, "", ids.New(), "", nil)
	f.ok(c, b, nil)
	var periods []khtcPeriod
	for _, no := range []int{38, 39, 40, 41} {
		var p khtcPeriod
		c, b := f.khtc(http.MethodPost, "/periods", ids.New(), "", map[string]any{"report_type": "week", "year": 2026, "no": no, "departments": []string{tt, cn}})
		f.ok(c, b, &p)
		periods = append(periods, p)
	}
	var calls []poolCall
	for _, p := range periods {
		calls = append(calls, poolCall{name: "Cancel#" + p.ID, want: []int{http.StatusOK}, do: func(ctx context.Context) (int, string, error) {
			return f.callCtx(ctx, http.MethodPost, "/v1/extension/khtc/periods/"+p.ID+"/cancel", authz.KNTinh, khtcDept,
				map[string]any{"reason": fmt.Sprintf("Mở nhầm tuần báo cáo %s", p.ID[:8])})
		}})
	}
	runPoolStorm(t, "KHTC hủy kỳ", poolSize, calls)
	for _, p := range periods {
		if got := f.get(p.ID); got.State != "cancelled" || got.FormsStatus != "cancelled" {
			t.Fatalf("kỳ %s sau hủy: %s / %s", p.ID, got.State, got.FormsStatus)
		}
	}
}
