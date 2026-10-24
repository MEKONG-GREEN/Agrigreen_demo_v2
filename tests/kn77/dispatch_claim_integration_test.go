package kn77_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// countMailer đếm email theo địa chỉ; delay giữ worker trong lúc gửi để các worker chồng nhau; crashOn: lần đầu gửi tới
// địa chỉ này thì panic (giả lập worker chết giữa lượt gửi, sau khi đã gửi người trước).
type countMailer struct {
	mu      sync.Mutex
	n       map[string]int
	delay   time.Duration
	crashOn string
}

func (m *countMailer) Send(_ context.Context, msg repport.Message) error {
	m.mu.Lock()
	if m.crashOn != "" && msg.To == m.crashOn {
		m.crashOn = ""
		m.mu.Unlock()
		panic("giả lập worker chết giữa lượt gửi")
	}
	m.mu.Unlock()
	time.Sleep(m.delay)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n[msg.To]++
	return nil
}

func (m *countMailer) count(addr string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n[addr]
}

func (m *countMailer) total() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := 0
	for _, v := range m.n {
		t += v
	}
	return t
}

const (
	claimAddr1 = "trongtrot.chicuc@angiang.gov.vn"
	claimAddr2 = "bvtv.chicuc@angiang.gov.vn"
)

// dispatchWithTwoRecipients dựng một lần xuất (xlsx) có hai người nhận To; trả mã lần xuất và mã người nhận.
func dispatchWithTwoRecipients(t *testing.T, c *cycle) (string, []string) {
	t.Helper()
	f := c.f
	c1 := f.communes89[0]
	c.issue(t, "T01")
	c.xa1.must(submit(c.xa1, str(c.task(t, c.xa1, "T01", c1, c.p1), "id"), "submitted", map[string]string{"F": "12.5"}), http.StatusCreated, "gửi")
	c.tinh.must(c.tinh.post("/v1/extension/consolidations", map[string]any{"period_id": c.p1}), http.StatusCreated, "cập nhật")
	var rids []string
	for i, addr := range []string{claimAddr1, claimAddr2} {
		r := c.tinh.must(c.tinh.post("/v1/reports/dispatch-recipients", map[string]any{"label": []string{"Chi cục TT", "Chi cục BVTV"}[i],
			"address": addr, "kind": "to"}), http.StatusCreated, "người nhận")
		rids = append(rids, str(r.body, "id"))
	}
	ex := c.tinh.must(c.tinh.post("/v1/reports/dispatches", map[string]any{"period_id": c.p1, "formats": []string{"xlsx"}}), http.StatusCreated, "xuất file")
	return str(obj(ex.body, "dispatch"), "id"), rids
}

// deliver chạy handler gửi Chi cục trong một giao dịch tenant riêng — như một worker nhận thông điệp (bỏ qua khử trùng
// inbox để kiểm chính cơ chế nhận việc).
func deliver(f *fixture, ev outbox.Envelope) error {
	return tenant.WithTenantTx(context.Background(), f.repPool, f.tenant, func(tx pgx.Tx) error {
		return f.rep.DispatchHandler()(context.Background(), tx, ev)
	})
}

func attemptOf(t *testing.T, ev outbox.Envelope) int {
	t.Helper()
	var p repport.DispatchRequestedV1
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	return p.Attempt
}

// TestP0WorkerReports_ConcurrentWorkersSendOnce — #128: nhiều worker cùng nhận sự kiện gửi của một lần gửi (giao lặp,
// nhiều bản worker) → chỉ một worker nhận việc (UPDATE … WHERE status='sending' RETURNING), mỗi người nhận đúng một email,
// một dòng "sent", hai dấu recipient_mailed, trạng thái "sent", việc đã nhả.
func TestP0WorkerReports_ConcurrentWorkersSendOnce(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	m := &countMailer{n: map[string]int{}, delay: 150 * time.Millisecond}
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi email")
	evs := f.events(repport.EventDispatchRequested)
	ev := evs[len(evs)-1]

	const workers = 4
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	start := make(chan struct{})
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- deliver(f, ev)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("worker lỗi: %v", err)
		}
	}
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 1 || m.total() != 2 {
		t.Fatalf("mỗi người nhận đúng một email: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch_event WHERE tenant_id = $1 AND dispatch_id::text = $2 AND action = 'recipient_mailed'`,
		f.tenant, did); n != 2 {
		t.Fatalf("dấu recipient_mailed: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch_event WHERE tenant_id = $1 AND dispatch_id::text = $2 AND action = 'sent'`,
		f.tenant, did); n != 1 {
		t.Fatalf("dòng sent: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sent'
	    AND claim_token IS NULL AND claim_until IS NULL AND attempts = 1`, f.tenant, did); n != 1 {
		t.Fatal("trạng thái sent, đã nhả việc, một lượt")
	}
	// Giao lại sau khi xong (giao dịch inbox hủy sau khi ghi kết quả) → không gửi gì thêm.
	if err := deliver(f, ev); err != nil {
		t.Fatal(err)
	}
	if m.total() != 2 {
		t.Fatalf("giao lại gửi trùng: %v", m.n)
	}
}

// TestP0WorkerReports_StaleSendingResumes — #128: worker chết giữa lượt (đã gửi người 1) → lần gửi kẹt "sending" với việc
// còn hạn: giao lại không gửi song song, "Gửi lại" bị chặn (409); quá hạn (AG_REPORT_DISPATCH_LEASE, mặc định 15 phút; kiểm thử tiến đồng hồ 16 phút) →
// "Gửi lại" TIẾP TỤC CÙNG LƯỢT: chỉ gửi người 2, người 1 không nhận lần hai; trạng thái "sent".
func TestP0WorkerReports_StaleSendingResumes(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	m := &countMailer{n: map[string]int{}, crashOn: claimAddr2}
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi email")
	evs := f.events(repport.EventDispatchRequested)
	ev := evs[len(evs)-1]

	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("muốn worker chết giữa lượt")
			}
		}()
		_ = deliver(f, ev)
	}()
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 0 {
		t.Fatalf("trước khi chết: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sending'
	    AND claim_token IS NOT NULL AND claim_until > now() - interval '100 years'`, f.tenant, did); n != 1 {
		t.Fatal("kẹt sending, việc còn giữ")
	}

	// Còn hạn: giao lại không gửi (worker khác đang giữ); "Gửi lại" → 409.
	if err := deliver(f, ev); err != nil {
		t.Fatal(err)
	}
	if m.total() != 1 {
		t.Fatalf("giao lại khi việc còn hạn đã gửi: %v", m.n)
	}
	c.tinh.fail(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusConflict,
		"reports.dispatch_sending", "gửi lại khi còn hạn")

	// Quá hạn → "Gửi lại" tiếp tục cùng lượt.
	f.setClock(f.clk.Now().Add(16 * time.Minute))
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi lại sau hạn")
	evs = f.events(repport.EventDispatchRequested)
	ev2 := evs[len(evs)-1]
	if ev2.EventID == ev.EventID || attemptOf(t, ev2) != attemptOf(t, ev) {
		t.Fatalf("gửi lại sau hạn phải là sự kiện mới cùng lượt: %d vs %d", attemptOf(t, ev2), attemptOf(t, ev))
	}
	if err := deliver(f, ev2); err != nil {
		t.Fatal(err)
	}
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 1 {
		t.Fatalf("người 1 không nhận lần hai, người 2 nhận một lần: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sent'
	    AND claim_token IS NULL AND attempts = 1`, f.tenant, did); n != 1 {
		t.Fatal("trạng thái sent, đã nhả việc, cùng lượt 1")
	}
	// Sự kiện cũ giao lại muộn → không gửi thêm.
	if err := deliver(f, ev); err != nil {
		t.Fatal(err)
	}
	if m.total() != 2 {
		t.Fatalf("sự kiện cũ gửi trùng: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch_event WHERE tenant_id = $1 AND dispatch_id::text = $2 AND action = 'send_requested'
	    AND payload->>'resumed' = 'true'`, f.tenant, did); n != 1 {
		t.Fatalf("sổ ghi lần tiếp tục: %d", n)
	}
}
