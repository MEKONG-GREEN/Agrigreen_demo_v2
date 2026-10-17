package notify_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// syncBuffer là bộ đệm log an toàn khi ghi đồng thời.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// fakePriceDesk: người nhận Cấp Tỉnh của Báo giá (gói NOTIF) theo tenant.
type fakePriceDesk struct {
	mu       sync.Mutex
	officers []uuid.UUID
	stewards []uuid.UUID
	calls    int
}

func (d *fakePriceDesk) Stewards(context.Context, uuid.UUID, string) ([]uuid.UUID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	return append([]uuid.UUID(nil), d.stewards...), nil
}

func (d *fakePriceDesk) ProvinceOfficers(context.Context, uuid.UUID, string) ([]uuid.UUID, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]uuid.UUID(nil), d.officers...), nil
}

// fakeEmails: danh bạ thư giả (pf/accounts); không có → port.ErrNoEmail.
type fakeEmails struct {
	mu   sync.Mutex
	addr map[uuid.UUID]string
}

func (e *fakeEmails) set(ref uuid.UUID, a string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.addr[ref] = a
}

func (e *fakeEmails) EmailOf(_ context.Context, _, ref uuid.UUID) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if a, ok := e.addr[ref]; ok {
		return a, nil
	}
	return "", port.ErrNoEmail
}

// rcptMailer ghi lại email; địa chỉ trong flaky → lỗi tạm thời n lần.
type rcptMailer struct {
	mu    sync.Mutex
	sent  []rcptMail
	flaky map[string]int
}

type rcptMail struct{ to, subject, body string }

func (m *rcptMailer) Send(_ context.Context, to []string, subject, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.flaky[to[0]] > 0 {
		m.flaky[to[0]]--
		return errors.New("smtp: 451 thử lại sau")
	}
	m.sent = append(m.sent, rcptMail{to: strings.Join(to, ","), subject: subject, body: body})
	return nil
}

func (m *rcptMailer) to(addr string) []rcptMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []rcptMail
	for _, s := range m.sent {
		if s.to == addr {
			out = append(out, s)
		}
	}
	return out
}

// flushEmail chạy consumer pf.notify.email cho mọi lô email đang mở của tenant; trả lỗi cuối (ErrEmailRetry…).
func (f *fixture) flushEmail(tid uuid.UUID) error {
	f.t.Helper()
	var batches []uuid.UUID
	err := tenant.WithTenantTx(context.Background(), f.pool, tid, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT DISTINCT batch_id FROM pf_notify.email_delivery
		  WHERE status IN ('pending','failed') AND attempts < 5`)
		if err != nil {
			return err
		}
		batches, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	})
	if err != nil {
		f.t.Fatal(err)
	}
	var last error
	for _, b := range batches {
		env := f.event(tid, port.EventEmailRequested, port.EmailRequestedV1{BatchID: b, Kind: "price", Deliveries: 1})
		if _, err := inbox.Process(context.Background(), f.appDB, port.ConsumerEmail, env, f.mod.EmailHandler()); err != nil {
			last = err
		}
	}
	return last
}

// emailDeliveryState is an observation of stored retry state, not a notification implementation DTO.
type emailDeliveryState struct {
	BatchID  uuid.UUID
	Kind     string
	Status   string
	Attempts int
}

func (f *fixture) emailRows(kind string) map[uuid.UUID]emailDeliveryState {
	f.t.Helper()
	out := map[uuid.UUID]emailDeliveryState{}
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), "SELECT recipient_ref, batch_id, kind, status, attempts FROM pf_notify.email_delivery WHERE kind = $1", kind)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var ref uuid.UUID
			var state emailDeliveryState
			if err := rows.Scan(&ref, &state.BatchID, &state.Kind, &state.Status, &state.Attempts); err != nil {
				return err
			}
			out[ref] = state
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func kindsOf(in []map[string]any) []string {
	var out []string
	for _, n := range in {
		out = append(out, n["kind"].(string))
	}
	return out
}

// Gói NOTIF (G1, D5; Báo giá FN-TI-02.03, FN-XA-03.05, FN-XA-03.04 ngoại lệ, NFR "Thông báo"): sự kiện pf/price →
// chuông + email đúng người nhận — Xã: cán bộ Xã phụ trách trong sự kiện; Tỉnh: người được phân công (hoặc mọi Cấp Tỉnh
// khi xã chưa phân công) + QTNV. Giao lặp / xử lý lại không tạo thông báo hay email trùng; email đã gửi không gửi lại khi
// lô được giao lại; lỗi máy chủ thư → thử lại; người chưa có email → chỉ trong ứng dụng; địa chỉ không lưu ở pf_notify.
func TestPriceNotificationsAndEmail_NOTIF(t *testing.T) {
	f := newFixture(t)
	sc := authz.Scope{Units: []string{ward1}}
	ctv := f.user(sc, authz.KNXa)
	owner := f.user(sc, authz.KNTinh)
	other := f.user(sc, authz.KNTinh)
	steward := f.user(sc, authz.KNTinh)
	f.desk.officers = []uuid.UUID{owner.actor, other.actor}
	f.desk.stewards = []uuid.UUID{steward.actor, owner.actor}
	f.emails.set(ctv.actor, "nguyen.van.ctv@mekong.vn")
	f.emails.set(owner.actor, "tran.thi.owner@mekong.vn")
	f.emails.set(other.actor, "le.van.other@mekong.vn")
	// steward chưa có email đã xác minh → chỉ trong ứng dụng.
	ticket := uuid.New()
	day := "2026-10-12"

	// 1. Yêu cầu xác minh → CTV phụ trách (chuông + email); giao lặp và xử lý thẳng lần hai không tạo trùng.
	req := f.event(f.tenant, priceport.EventVerificationRequested, priceport.VerificationRequestedV1{Profile: "kn", Day: day,
		PointCode: "XBG0001", PointName: "Mỹ Chánh", TicketID: ticket, ItemCodes: []string{"MH00001"},
		RespondBy: "2026-10-12T06:30:00Z", OfficerRef: &ctv.actor, RequesterRef: owner.actor})
	if !f.process(priceport.NotifyConsumerPrice, req) || f.process(priceport.NotifyConsumerPrice, req) {
		t.Fatal("lần giao đầu xử lý, giao lặp bị inbox bỏ qua")
	}
	if _, err := inbox.Process(context.Background(), f.appDB, priceport.NotifyConsumerPrice+".replay", req, f.mod.EventHandler()); err != nil {
		t.Fatal(err)
	}
	in := ctv.inbox()
	if len(in) != 1 || in[0]["kind"] != priceport.KindPriceVerifyRequest || in[0]["deep_link"] != "/kn/bao-gia/xac-minh" ||
		!strings.Contains(in[0]["title"].(string), "Mỹ Chánh") {
		t.Fatalf("CTV nhận một yêu cầu xác minh: %v", in)
	}
	for name, c := range map[string]caller{"owner": owner, "other": other, "steward": steward} {
		if n := c.inbox(); len(n) != 0 {
			t.Fatalf("%s không nhận yêu cầu xác minh của Xã: %v", name, n)
		}
	}
	if rows := f.emailRows(priceport.KindPriceVerifyRequest); len(rows) != 1 || rows[ctv.actor].Status != "pending" {
		t.Fatalf("một lần gửi email cho CTV: %+v", rows)
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	if m := f.mailer.to("nguyen.van.ctv@mekong.vn"); len(m) != 1 || !strings.Contains(m[0].subject, "Yêu cầu xác minh giá") ||
		!strings.Contains(m[0].body, "https://kn.example.vn/kn/bao-gia/xac-minh") {
		t.Fatalf("CTV nhận đúng một email: %+v", m)
	}
	// Lô được giao lại sau khi đã gửi → không gửi lặp.
	var batch uuid.UUID
	for _, r := range f.emailRows(priceport.KindPriceVerifyRequest) {
		batch = r.BatchID
	}
	env := f.event(f.tenant, port.EventEmailRequested, port.EmailRequestedV1{BatchID: batch, Kind: priceport.KindPriceVerifyRequest, Deliveries: 1})
	if err := f.mod.EmailHandler()(context.Background(), nil, env); err != nil {
		t.Fatal(err)
	}
	if m := f.mailer.to("nguyen.van.ctv@mekong.vn"); len(m) != 1 {
		t.Fatalf("giao lại lô đã gửi không gửi lặp: %d", len(m))
	}

	// 2. Cảnh báo Tạm tính của xã chưa phân công → mọi Cấp Tỉnh + QTNV (không trùng); máy chủ thư lỗi tạm → thử lại.
	f.mailer.mu.Lock()
	f.mailer.flaky = map[string]int{"tran.thi.owner@mekong.vn": 1}
	f.mailer.mu.Unlock()
	alert := f.event(f.tenant, priceport.EventAlertRaised, priceport.AlertRaisedV1{Profile: "kn", Day: day, PointCode: "XBG0002",
		PointName: "Bình Đức", TicketID: uuid.New(), ItemCodes: []string{"MH00002"}})
	f.process(priceport.NotifyConsumerPrice, alert)
	for name, c := range map[string]caller{"owner": owner, "other": other, "steward": steward} {
		if k := kindsOf(c.inbox()); len(k) != 1 || k[0] != priceport.KindPriceAlert {
			t.Fatalf("%s nhận một cảnh báo: %v", name, k)
		}
	}
	if k := kindsOf(ctv.inbox()); len(k) != 1 {
		t.Fatalf("CTV không nhận cảnh báo của Cấp Tỉnh: %v", k)
	}
	if err := f.flushEmail(f.tenant); !errors.Is(err, notify.ErrEmailRetry) {
		t.Fatalf("lỗi tạm của máy chủ thư → giao lại: %v", err)
	}
	if err := f.flushEmail(f.tenant); err != nil {
		t.Fatal(err)
	}
	rows := f.emailRows(priceport.KindPriceAlert)
	if len(rows) != 3 || rows[owner.actor].Status != "sent" || rows[owner.actor].Attempts != 2 ||
		rows[other.actor].Status != "sent" || rows[other.actor].Attempts != 1 ||
		rows[steward.actor].Status != "dropped" || rows[steward.actor].Attempts != 1 {
		t.Fatalf("trạng thái email cảnh báo: %+v", rows)
	}
	if len(f.mailer.to("tran.thi.owner@mekong.vn")) != 1 || len(f.mailer.to("le.van.other@mekong.vn")) != 1 {
		t.Fatal("mỗi Cấp Tỉnh có email nhận đúng một email cảnh báo")
	}

	// 3. Xã đã phân công → chỉ người được phân công + QTNV; người phản hồi (CTV) không tự nhận.
	f.process(priceport.NotifyConsumerPrice, f.event(f.tenant, priceport.EventVerificationResponded, priceport.VerificationRespondedV1{
		Profile: "kn", Day: day, PointCode: "XBG0001", PointName: "Mỹ Chánh", TicketID: ticket, ItemCode: "MH00001", Choice: "keep",
		OwnerRef: &owner.actor, ResponderRef: ctv.actor}))
	if k := kindsOf(owner.inbox()); len(k) != 2 || k[0] != priceport.KindPriceVerifyReply {
		t.Fatalf("người được phân công nhận phản hồi: %v", k)
	}
	if k := kindsOf(other.inbox()); len(k) != 1 {
		t.Fatalf("Cấp Tỉnh khác không nhận phản hồi của xã đã phân công: %v", k)
	}
	if k := kindsOf(steward.inbox()); len(k) != 2 {
		t.Fatalf("QTNV nhận phản hồi: %v", k)
	}

	// 4. Tổng hợp 12:00 → mọi Cấp Tỉnh + QTNV; giao lặp không trùng.
	lock := f.event(f.tenant, priceport.EventLockSummarized, priceport.LockSummarizedV1{Profile: "kn", Day: day,
		LockAt: "2026-10-12T05:00:00Z", Points: 2, Submitted: 1, Missed: 1})
	f.process(priceport.NotifyConsumerPrice, lock)
	f.process(priceport.NotifyConsumerPrice, lock)
	for name, c := range map[string]caller{"owner": owner, "other": other, "steward": steward} {
		n := 0
		for _, k := range kindsOf(c.inbox()) {
			if k == priceport.KindPriceLockSummary {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%s nhận đúng một tổng hợp khóa nhập: %d", name, n)
		}
	}

	// 5. Địa chỉ email không nằm trong pf_notify (chỉ tra lúc gửi).
	var leaked int
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_notify.notification
		  WHERE title LIKE '%@mekong.vn%' OR body LIKE '%@mekong.vn%'`).Scan(&leaked)
	}); err != nil || leaked != 0 {
		t.Fatalf("không lưu địa chỉ email: %d %v", leaked, err)
	}
	// 6. Log chỉ ghi địa chỉ đã che (R-14).
	logs := f.logs.String()
	if !strings.Contains(logs, "ng•••@mekong.vn") || strings.Contains(logs, "nguyen.van.ctv@") || strings.Contains(logs, "tran.thi.owner@") {
		t.Fatalf("log phải che email: %s", logs)
	}
}

// Gói NOTIF: kết quả xử lý cảnh báo → CTV phụ trách (chuông + email), người quyết định không tự nhận.
func TestPriceDecisionToOfficer_NOTIF(t *testing.T) {
	f := newFixture(t)
	ctv := f.user(authz.Scope{Units: []string{ward1}}, authz.KNXa)
	tinh := f.user(authz.Scope{Units: []string{ward1}}, authz.KNTinh)
	f.emails.set(ctv.actor, "a.b@mekong.vn")
	f.process(priceport.NotifyConsumerPrice, f.event(f.tenant, priceport.EventVerificationDecided, priceport.VerificationDecidedV1{Profile: "kn",
		Day: "2026-10-12", PointName: "Mỹ Chánh", TicketID: uuid.New(), ItemCodes: []string{"MH00001"},
		Decision: priceport.DecisionKept, OfficerRef: &ctv.actor, DeciderRef: tinh.actor}))
	if k := kindsOf(ctv.inbox()); len(k) != 1 || k[0] != priceport.KindPriceVerifyDecision {
		t.Fatalf("kết quả xử lý → CTV: %v", k)
	}
	if k := kindsOf(tinh.inbox()); len(k) != 0 {
		t.Fatalf("người quyết định không tự nhận: %v", k)
	}
	if rows := f.emailRows(priceport.KindPriceVerifyDecision); len(rows) != 1 || rows[ctv.actor].Kind != priceport.KindPriceVerifyDecision {
		t.Fatalf("xếp một email cho CTV: %+v", rows)
	}
}
