package kn77_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	repport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/reports/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
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

// flakyMailer: SMTP từ chối lần đầu gửi tới failOnce; block: lần đầu gửi tới địa chỉ này thì báo entered rồi giữ tới khi
// release hoặc ctx hết hạn (SMTP thật tôn trọng deadline của ctx); ignoreCtx: giữ tới release bất kể ctx (thư viện SMTP xấu).
// Chỉ lời gọi trả nil được đếm (SMTP đã chấp nhận).
type flakyMailer struct {
	mu        sync.Mutex
	n         map[string]int
	failOnce  string
	block     string
	ignoreCtx bool
	entered   chan struct{}
	release   chan struct{}
}

func (m *flakyMailer) Send(ctx context.Context, msg repport.Message) error {
	m.mu.Lock()
	if m.failOnce != "" && msg.To == m.failOnce {
		m.failOnce = ""
		m.mu.Unlock()
		return errors.New("550 hộp thư tạm từ chối")
	}
	blk := m.block != "" && msg.To == m.block
	if blk {
		m.block = ""
	}
	m.mu.Unlock()
	if blk {
		close(m.entered)
		if m.ignoreCtx {
			<-m.release
		} else {
			select {
			case <-m.release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n[msg.To]++
	return nil
}

func (m *flakyMailer) count(addr string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.n[addr]
}

// TestP0WorkerReports_PartialFailureRetryKeepsMailed — #128 review [P1]: người 1 nhận, SMTP từ chối người 2 → "Gửi lỗi";
// "Gửi lại" cùng danh sách (lượt attempts mới) tiếp tục VÒNG GỬI cũ: chỉ người 2 được gửi, người 1 không nhận lần hai.
// Sau khi đã "sent", "Gửi lại" là vòng mới chủ động → mọi người nhận lại.
func TestP0WorkerReports_PartialFailureRetryKeepsMailed(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	m := &flakyMailer{n: map[string]int{}, failOnce: claimAddr2}
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi email")
	evs := f.events(repport.EventDispatchRequested)
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatal(err)
	}
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 0 {
		t.Fatalf("lượt 1: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'failed'`, f.tenant, did); n != 1 {
		t.Fatal("lượt 1 phải Gửi lỗi")
	}
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi lại")
	evs = f.events(repport.EventDispatchRequested)
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatal(err)
	}
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 1 {
		t.Fatalf("gửi lại sau lỗi một phần gửi trùng người đã nhận: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sent' AND attempts = 2`,
		f.tenant, did); n != 1 {
		t.Fatal("trạng thái sent ở lượt 2")
	}
	// Vòng mới chủ động sau khi đã gửi xong → gửi lại cho mọi người.
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi vòng mới")
	evs = f.events(repport.EventDispatchRequested)
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatal(err)
	}
	if m.count(claimAddr1) != 2 || m.count(claimAddr2) != 2 {
		t.Fatalf("vòng mới phải gửi mọi người: %v", m.n)
	}
}

// TestP0WorkerReports_LeaseExpiryDuringSMTPSendsOnce — #128 review [P1]: worker A giữ việc và treo trong SMTP của người 1;
// hạn nhận việc (2s) trôi qua → B được nhận lại. Lời gọi SMTP của A phải bị cắt trước khi hạn hết (deadline < hạn − biên)
// nên chỉ một lời gọi SMTP thành công tới người 1.
func TestP0WorkerReports_LeaseExpiryDuringSMTPSendsOnce(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	const lease = 2 * time.Second
	f.rep.UseDispatchLease(lease)
	m := &flakyMailer{n: map[string]int{}, block: claimAddr1, entered: make(chan struct{}), release: make(chan struct{})}
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi email")
	evs := f.events(repport.EventDispatchRequested)
	errA := make(chan error, 1)
	go func() { errA <- deliver(f, evs[len(evs)-1]) }()
	<-m.entered
	aDone := false
	select {
	case err := <-errA:
		aDone = true
		if err != nil {
			t.Fatalf("worker A: %v", err)
		}
	case <-time.After(lease + 500*time.Millisecond):
	}
	f.setClock(f.clk.Now().Add(lease + time.Second))
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi lại sau hạn")
	evs = f.events(repport.EventDispatchRequested)
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatalf("worker B: %v", err)
	}
	close(m.release)
	if !aDone {
		if err := <-errA; err != nil {
			t.Logf("worker A kết thúc muộn: %v", err)
		}
	}
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 1 {
		t.Fatalf("SMTP thành công tới người 1 phải đúng một lần: %v", m.n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sent' AND claim_token IS NULL`,
		f.tenant, did); n != 1 {
		t.Fatal("trạng thái sent, đã nhả việc")
	}
}

// TestP0WorkerReports_LateFinisherMarkTolerated — #128 review [P1]: thư viện SMTP bỏ qua deadline (trường hợp xấu nhất)
// → A gửi xong sau khi B đã gửi và ghi dấu. Ghi dấu muộn của A gặp idem đã có (23505) → coi là đã ghi (ghi log trùng),
// handler không lỗi (không NACK/giao lại vô ích), sổ chỉ một dấu cho người 1.
func TestP0WorkerReports_LateFinisherMarkTolerated(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	const lease = 2 * time.Second
	f.rep.UseDispatchLease(lease)
	m := &flakyMailer{n: map[string]int{}, block: claimAddr1, ignoreCtx: true, entered: make(chan struct{}), release: make(chan struct{})}
	f.rep.UseMailer(m)
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi email")
	evs := f.events(repport.EventDispatchRequested)
	errA := make(chan error, 1)
	go func() { errA <- deliver(f, evs[len(evs)-1]) }()
	<-m.entered
	f.setClock(f.clk.Now().Add(lease + time.Second))
	c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi lại sau hạn")
	evs = f.events(repport.EventDispatchRequested)
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatalf("worker B: %v", err)
	}
	close(m.release)
	if err := <-errA; err != nil {
		t.Fatalf("worker A ghi dấu muộn phải dung thứ dấu đã có: %v", err)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch_event WHERE tenant_id = $1 AND dispatch_id::text = $2 AND action = 'recipient_mailed'`,
		f.tenant, did); n != 2 {
		t.Fatalf("dấu recipient_mailed: %d", n)
	}
	if n := f.count(`SELECT count(*) FROM pf_reports.dispatch WHERE tenant_id = $1 AND id::text = $2 AND status = 'sent' AND claim_token IS NULL`,
		f.tenant, did); n != 1 {
		t.Fatal("trạng thái sent do B ghi")
	}
}

// resendState đọc can_resend/resumable_at của lần gửi qua GET chi tiết và danh sách của kỳ (cùng giá trị ở hai nơi).
func resendState(t *testing.T, c *cycle, did string) (bool, string) {
	t.Helper()
	d := obj(c.tinh.must(c.tinh.get("/v1/reports/dispatches/"+did), http.StatusOK, "chi tiết").body, "dispatch")
	can, ok := d["can_resend"].(bool)
	if !ok {
		t.Fatalf("thiếu can_resend: %v", d)
	}
	if _, ok := d["resumable_at"]; !ok {
		t.Fatalf("thiếu resumable_at: %v", d)
	}
	at := str(d, "resumable_at")
	for _, it := range arr(c.tinh.must(c.tinh.get("/v1/reports/dispatches?period_id="+c.p1), http.StatusOK, "danh sách").body, "items") {
		if str(it, "id") == did && (it["can_resend"] != can || str(it, "resumable_at") != at) {
			t.Fatalf("danh sách lệch chi tiết: %v vs %v/%s", it, can, at)
		}
	}
	return can, at
}

func sameInstant(rfc string, want time.Time) bool {
	got, err := time.Parse(time.RFC3339Nano, rfc)
	return err == nil && got.Equal(want)
}

func dispatchTimes(t *testing.T, f *fixture, did string) (claimUntil *time.Time, updatedAt time.Time) {
	t.Helper()
	if err := f.env.AdminPool(t, migrations.PF).QueryRow(context.Background(), `SELECT claim_until, updated_at FROM pf_reports.dispatch
	  WHERE tenant_id = $1 AND id::text = $2`, f.tenant, did).Scan(&claimUntil, &updatedAt); err != nil {
		t.Fatal(err)
	}
	return claimUntil, updatedAt
}

// TestP0WorkerReports_ResendStateMatchesServer — review #128 (nút "Gửi lại" web): DTO đọc lần gửi có can_resend +
// resumable_at do máy chủ tính, đúng điều kiện SendDispatch dùng: chưa ai nhận → updated_at + hạn; worker giữ rồi chết →
// claim_until. Trước mốc: can_resend=false và POST 409 reports.dispatch_sending; sau mốc: can_resend=true, POST 202 tiếp
// tục cùng lượt (người đã nhận không nhận lần hai); xong → can_resend=true, resumable_at=null.
func TestP0WorkerReports_ResendStateMatchesServer(t *testing.T) {
	c := setupCycle(t)
	f := c.f
	did, rids := dispatchWithTwoRecipients(t, c)
	m := &countMailer{n: map[string]int{}, crashOn: claimAddr2}
	f.rep.UseMailer(m)
	lease := 15 * time.Minute // dispatch.DefaultSendLease — fixture không đặt hạn riêng.

	if can, at := resendState(t, c, did); !can || at != "" {
		t.Fatalf("chưa gửi: can_resend=true, resumable_at=null; nhận %v %q", can, at)
	}
	sent := c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi email")
	if d := obj(sent.body, "dispatch"); d["can_resend"] != false || str(d, "resumable_at") == "" {
		t.Fatalf("202 vừa xếp gửi: can_resend=false kèm mốc; nhận %v", d)
	}

	// Chưa worker nào nhận: mốc = updated_at + hạn.
	claim, upd := dispatchTimes(t, f, did)
	if claim != nil {
		t.Fatal("chưa ai nhận việc")
	}
	can, at := resendState(t, c, did)
	if want := upd.Add(lease); can || !sameInstant(at, want) {
		t.Fatalf("chưa nhận việc: muốn false/%s, nhận %v/%s", want, can, at)
	}

	// Worker nhận việc, gửi người 1 rồi chết: mốc = claim_until.
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
	claim, _ = dispatchTimes(t, f, did)
	if claim == nil {
		t.Fatal("worker phải đang giữ việc")
	}
	can, at = resendState(t, c, did)
	if want := *claim; can || !sameInstant(at, want) {
		t.Fatalf("worker giữ: muốn false/%s, nhận %v/%s", want, can, at)
	}
	c.tinh.fail(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusConflict,
		"reports.dispatch_sending", "gửi lại khi còn hạn")

	// Đúng mốc (claim_until <= now): máy chủ mở — DTO và POST cùng kết luận.
	f.setClock(*claim)
	if can, at = resendState(t, c, did); !can || !sameInstant(at, *claim) {
		t.Fatalf("quá hạn: muốn can_resend=true giữ mốc, nhận %v/%s", can, at)
	}
	res := c.tinh.must(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusAccepted, "gửi lại sau hạn")
	if d := obj(res.body, "dispatch"); d["can_resend"] != false || num(d, "attempts") != 1 || !sameInstant(str(d, "resumable_at"), f.clk.Now().Add(lease)) {
		t.Fatalf("tiếp tục cùng lượt 1, khóa lại tới now + hạn (nhả claim cũ): %v", d)
	}
	if claim, _ = dispatchTimes(t, f, did); claim != nil {
		t.Fatal("Gửi lại phải nhả việc quá hạn")
	}
	c.tinh.fail(c.tinh.post("/v1/reports/dispatches/"+did+"/send", map[string]any{"recipient_ids": rids}), http.StatusConflict,
		"reports.dispatch_sending", "bấm Gửi lại lần hai ngay sau khi tiếp tục")
	evs = f.events(repport.EventDispatchRequested)
	if err := deliver(f, evs[len(evs)-1]); err != nil {
		t.Fatal(err)
	}
	if m.count(claimAddr1) != 1 || m.count(claimAddr2) != 1 {
		t.Fatalf("người 1 không nhận lần hai: %v", m.n)
	}
	if can, at = resendState(t, c, did); !can || at != "" {
		t.Fatalf("đã gửi: can_resend=true, resumable_at=null; nhận %v %q", can, at)
	}
}
