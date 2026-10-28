package notify_test

// BF-NOTIFY-POOL (#155): consumer pf.notify.push / pf.notify.email không được giữ giao dịch inbox trong lúc chờ kết nối
// thứ hai, gửi FCM/SMTP hay tra danh bạ thư. Pool CỠ 1 phải chạy xong; trong lúc kênh gửi treo, kết nối duy nhất của
// pool phải rảnh; giao lặp đồng thời không gửi trùng; nhận việc hết hạn thì nhận lại.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	extport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price"
	priceport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/price/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	partyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/party/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// blockingPusher chặn Send tới khi test mở release.
type blockingPusher struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
	fail    atomic.Bool // sau release: lỗi tạm thời (không phải token hỏng)
}

func newBlockingPusher() *blockingPusher {
	return &blockingPusher{entered: make(chan struct{}, 8), release: make(chan struct{})}
}

func (p *blockingPusher) Name() string { return "blocking" }

func (p *blockingPusher) Send(ctx context.Context, _ notify.PushMessage) error {
	p.calls.Add(1)
	p.entered <- struct{}{}
	select {
	case <-p.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if p.fail.Load() {
		return errors.New("fixture FCM unavailable")
	}
	return nil
}

// sizedPool là pool app_platform cỡ n và module notify chạy trên đúng pool đó (như cmd/worker).
type sizedPool struct {
	pool *pgxpool.Pool
	db   tenant.Beginner
	mod  *notify.Module
}

func (f *fixture) sized(n int32, pusher interface {
	Name() string
	Send(context.Context, notify.PushMessage) error
}) sizedPool {
	f.t.Helper()
	return f.sizedMail(n, pusher, f.mailer)
}

// sizedMail như sized nhưng kênh email theo người nhận là mailer.
func (f *fixture) sizedMail(n int32, pusher interface {
	Name() string
	Send(context.Context, notify.PushMessage) error
}, mailer channel.Mailer) sizedPool {
	f.t.Helper()
	pool := pgtest.Get(f.t).SizedPool(f.t, migrations.PF, dbadmin.AppPlatform, n)
	mod := notify.New(pool, f.clk, notify.Deps{Kinds: []port.KindRegistration{extension.NotificationKinds(), price.NotificationKinds()},
		Consumers: []port.ConsumerRegistration{extension.NotificationConsumers(), price.NotificationConsumers()},
		Builders:  price.NotificationBuilders(f.desk), Scope: f.scope, Devices: f.devices, Staff: f.staff, Units: fakeUnits{},
		Pusher: pusher, Emails: f.emails, EmailMailer: mailer, LinkBase: "https://kn.example.vn",
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return sizedPool{pool: pool, db: db.ForModule(pool, notify.DBModule), mod: mod}
}

// pushBatch xếp một lần gửi đẩy (chỉ đạo khẩn tới cán bộ có thiết bị) và trả lô.
func (f *fixture) pushBatch() uuid.UUID {
	f.t.Helper()
	a := f.user(authz.Scope{Units: []string{ward1}}, partyport.RoleKNOfficer)
	a.device(fcmToken + "-" + ids.New().String()[:8])
	source, _ := f.directive(f.tenant, port.SeverityUrgent, []string{ward1}, a.actor)
	f.process(extport.NotifyConsumerDirective, source)
	var batch uuid.UUID
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT d.batch_id FROM pf_notify.push_delivery d
		  JOIN pf_notify.notification n ON n.tenant_id = d.tenant_id AND n.id = d.notification_id
		  WHERE n.source_event_id = $1 LIMIT 1`, source.EventID).Scan(&batch)
	}); err != nil {
		f.t.Fatal(err)
	}
	return batch
}

// emailBatch xếp một email theo người nhận (yêu cầu xác minh giá tới CTV có email) và trả lô + địa chỉ.
func (f *fixture) emailBatch() (uuid.UUID, string) {
	f.t.Helper()
	sc := authz.Scope{Units: []string{ward1}}
	ctv := f.user(sc, authz.KNXa)
	owner := f.user(sc, authz.KNTinh)
	f.desk.officers = []uuid.UUID{owner.actor}
	addr := "ctv-" + ids.New().String()[:8] + "@mekong.vn"
	f.emails.set(ctv.actor, addr)
	req := f.event(f.tenant, priceport.EventVerificationRequested, priceport.VerificationRequestedV1{Profile: "kn", Day: "2026-10-24",
		PointCode: "XBG0001", PointName: "Mỹ Chánh", TicketID: uuid.New(), ItemCodes: []string{"MH00001"},
		RespondBy: "2026-10-24T06:30:00Z", OfficerRef: &ctv.actor, RequesterRef: owner.actor})
	f.process(priceport.NotifyConsumerPrice, req)
	var batch uuid.UUID
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT batch_id FROM pf_notify.email_delivery WHERE recipient_ref = $1`, ctv.actor).Scan(&batch)
	}); err != nil {
		f.t.Fatal(err)
	}
	return batch, addr
}

func (f *fixture) pushEnvelope(batch uuid.UUID) outbox.Envelope {
	return f.event(f.tenant, port.EventPushRequested, port.PushRequestedV1{BatchID: batch, Kind: "bf_notify_pool", Deliveries: 1})
}

type pushRow struct {
	status  string
	claimed bool
}

func (f *fixture) pushRows(batch uuid.UUID) []pushRow {
	f.t.Helper()
	var out []pushRow
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT status, claim_id IS NOT NULL FROM pf_notify.push_delivery WHERE batch_id = $1 ORDER BY id`, batch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r pushRow
			if err := rows.Scan(&r.status, &r.claimed); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

func waitPush(t *testing.T, p *blockingPusher) {
	t.Helper()
	select {
	case <-p.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("Pusher.Send không được gọi")
	}
}

// Tái hiện của Codex (#155): pool một kết nối, hạn 5 giây — trước đây FAIL "tenant: mở giao dịch: context deadline exceeded".
func TestBFNotifyPool_PushOnSingleConnPool(t *testing.T) {
	for _, size := range []int32{1, 2} {
		t.Run(fmt.Sprintf("pool_%d", size), func(t *testing.T) {
			f := newFixture(t)
			batch := f.pushBatch()
			sp := f.sized(size, f.pusher)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			ok, err := inbox.Process(ctx, sp.db, port.ConsumerPush, f.pushEnvelope(batch), sp.mod.PushHandler())
			if err != nil || !ok {
				t.Fatalf("pool cỡ %d: đẩy phải xong mà không cần kết nối thứ hai: ok=%v err=%v", size, ok, err)
			}
			if n := len(f.pusher.messages()); n != 1 {
				t.Fatalf("gửi %d thông điệp đẩy, muốn 1", n)
			}
			if rows := f.pushRows(batch); len(rows) != 1 || rows[0].status != "sent" || rows[0].claimed {
				t.Fatalf("lần gửi phải sent và bỏ nhận: %+v", rows)
			}
		})
	}
}

func TestBFNotifyPool_EmailOnSingleConnPool(t *testing.T) {
	f := newFixture(t)
	batch, addr := f.emailBatch()
	sp := f.sized(1, f.pusher)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	env := f.event(f.tenant, port.EventEmailRequested, port.EmailRequestedV1{BatchID: batch, Kind: "price", Deliveries: 1})
	ok, err := inbox.Process(ctx, sp.db, port.ConsumerEmail, env, sp.mod.EmailHandler())
	if err != nil || !ok {
		t.Fatalf("email trên pool cỡ 1 phải xong: ok=%v err=%v", ok, err)
	}
	if n := len(f.mailer.to(addr)); n != 1 {
		t.Fatalf("gửi %d email, muốn 1", n)
	}
	// Giao lại: inbox đã ghi → không gửi lại.
	if ok, err := inbox.Process(ctx, sp.db, port.ConsumerEmail, env, sp.mod.EmailHandler()); err != nil || ok {
		t.Fatalf("giao lại: ok=%v err=%v", ok, err)
	}
	if n := len(f.mailer.to(addr)); n != 1 {
		t.Fatalf("giao lại gửi trùng: %d", n)
	}
}

// Kênh đẩy treo: kết nối DUY NHẤT của pool phải rảnh (không giữ giao dịch inbox trong lúc gửi), lần gửi đang được nhận.
func TestBFNotifyPool_PushSendDoesNotHoldConnection(t *testing.T) {
	f := newFixture(t)
	batch := f.pushBatch()
	bp := newBlockingPusher()
	sp := f.sized(1, bp)
	env := f.pushEnvelope(batch)
	done := make(chan error, 1)
	go func() {
		_, err := inbox.Process(context.Background(), sp.db, port.ConsumerPush, env, sp.mod.PushHandler())
		done <- err
	}()
	waitPush(t, bp)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var claimed bool
	var status string
	if err := tenant.WithTenantTx(ctx, sp.db, f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT claim_id IS NOT NULL, status FROM pf_notify.push_delivery WHERE batch_id = $1`, batch).Scan(&claimed, &status)
	}); err != nil {
		t.Fatalf("pool cỡ 1 bị giữ trong lúc gửi đẩy: %v", err)
	}
	if !claimed || status != "pending" {
		t.Fatalf("lần gửi phải đang được nhận, chưa ghi kết quả: claimed=%v status=%s", claimed, status)
	}
	close(bp.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if rows := f.pushRows(batch); len(rows) != 1 || rows[0].status != "sent" || rows[0].claimed {
		t.Fatalf("sau khi gửi: %+v", rows)
	}
	// Giao lại (thông điệp mới cùng lô): không gửi trùng.
	if _, err := inbox.Process(context.Background(), sp.db, port.ConsumerPush, f.pushEnvelope(batch), sp.mod.PushHandler()); err != nil {
		t.Fatal(err)
	}
	if bp.calls.Load() != 1 {
		t.Fatalf("gửi %d lần, muốn 1", bp.calls.Load())
	}
}

// Giao lặp đồng thời cùng thông điệp (RabbitMQ giao lại khi lượt đầu còn chạy): chỉ một lần gửi.
func TestBFNotifyPool_PushConcurrentRedeliverySendsOnce(t *testing.T) {
	f := newFixture(t)
	batch := f.pushBatch()
	bp := newBlockingPusher()
	sp := f.sized(2, bp)
	env := f.pushEnvelope(batch)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	run := func() {
		defer wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := inbox.Process(ctx, sp.db, port.ConsumerPush, env, sp.mod.PushHandler())
		errs <- err
	}
	wg.Add(1)
	go run()
	waitPush(t, bp)
	wg.Add(1)
	go run()
	time.Sleep(700 * time.Millisecond) // lượt hai đã nhận thấy lần gửi đang do lượt đầu giữ
	close(bp.release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if bp.calls.Load() != 1 {
		t.Fatalf("giao lặp đồng thời gửi %d lần, muốn 1", bp.calls.Load())
	}
	if rows := f.pushRows(batch); len(rows) != 1 || rows[0].status != "sent" || rows[0].claimed {
		t.Fatalf("sau khi gửi: %+v", rows)
	}
}

// Worker khác đang giữ (hạn còn) → không gửi, không ghi inbox (thông điệp giao lại); hết hạn giữ → nhận lại và gửi.
func TestBFNotifyPool_PushClaimLeaseExpires(t *testing.T) {
	f := newFixture(t)
	batch := f.pushBatch()
	other := ids.New()
	if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
		tag, err := tx.Exec(context.Background(), `UPDATE pf_notify.push_delivery SET claim_id = $1, claim_until = $2 WHERE batch_id = $3`,
			other, f.clk.Now().Add(time.Hour), batch)
		if err == nil && tag.RowsAffected() != 1 {
			err = fmt.Errorf("nhận %d lần gửi", tag.RowsAffected())
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sp := f.sized(1, f.pusher)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if ok, err := inbox.Process(ctx, sp.db, port.ConsumerPush, f.pushEnvelope(batch), sp.mod.PushHandler()); err == nil || ok {
		t.Fatalf("lần gửi đang do worker khác giữ: phải trả lỗi để giao lại, ok=%v err=%v", ok, err)
	}
	if n := len(f.pusher.messages()); n != 0 {
		t.Fatalf("gửi lần gửi đang do worker khác giữ: %d", n)
	}
	f.clk.Advance(2 * time.Hour)
	if ok, err := inbox.Process(context.Background(), sp.db, port.ConsumerPush, f.pushEnvelope(batch), sp.mod.PushHandler()); err != nil || !ok {
		t.Fatalf("hết hạn giữ phải nhận lại: ok=%v err=%v", ok, err)
	}
	if n := len(f.pusher.messages()); n != 1 {
		t.Fatalf("gửi %d, muốn 1", n)
	}
	if rows := f.pushRows(batch); len(rows) != 1 || rows[0].status != "sent" || rows[0].claimed {
		t.Fatalf("sau khi nhận lại: %+v", rows)
	}
}

// takeOver mô phỏng worker B (review #155 P1): hạn giữ của lượt A hết (đồng hồ giả tiến quá DefaultDeliveryLease), B nhận
// lại lần gửi của lô. done=false: B chết trước khi gửi/ghi — nhận của B còn hạn, lần gửi vẫn mở. done=true: B gửi xong.
func (f *fixture) takeOver(table string, batch uuid.UUID, done bool) {
	f.t.Helper()
	f.clk.Advance(2 * time.Minute)
	now := f.clk.Now()
	q := `UPDATE pf_notify.` + table + ` SET claim_id = $1, claim_until = $2 + interval '1 minute'
	  WHERE batch_id = $3 AND claim_id IS NOT NULL AND claim_until <= $2`
	args := []any{ids.New(), now, batch}
	if done {
		q = `UPDATE pf_notify.` + table + ` SET status = 'sent', attempts = attempts + 1, sent_at = $1, finished_at = $1,
		    claim_id = NULL, claim_until = NULL
		  WHERE batch_id = $2 AND claim_id IS NOT NULL AND claim_until <= $1`
		args = []any{now, batch}
	}
	if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
		tag, err := tx.Exec(context.Background(), q, args...)
		if err == nil && tag.RowsAffected() != 1 {
			err = fmt.Errorf("worker B nhận lại %d lần gửi, muốn 1 (nhận của A phải đang giữ và đã hết hạn)", tag.RowsAffected())
		}
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
}

// deliveryRows trả trạng thái + còn nhận của các lần gửi trong lô (push_delivery | email_delivery).
func (f *fixture) deliveryRows(table string, batch uuid.UUID) []pushRow {
	f.t.Helper()
	var out []pushRow
	if err := tenant.WithTenantTx(context.Background(), f.pool, f.tenant, func(tx pgx.Tx) error {
		rows, err := tx.Query(context.Background(), `SELECT status, claim_id IS NOT NULL FROM pf_notify.`+table+` WHERE batch_id = $1 ORDER BY id`, batch)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var r pushRow
			if err := rows.Scan(&r.status, &r.claimed); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rows.Err()
	}); err != nil {
		f.t.Fatal(err)
	}
	return out
}

type processResult struct {
	ok  bool
	err error
}

func awaitProcess(t *testing.T, done <-chan processResult) processResult {
	t.Helper()
	select {
	case r := <-done:
		return r
	case <-time.After(60 * time.Second):
		t.Fatal("lượt xử lý không kết thúc")
		return processResult{}
	}
}

// Review #155 P1 (đẩy): A gửi lỗi tạm thời, ghi kết quả sau khi hạn giữ đã hết; B nhận lại rồi chết trước khi gửi/ghi.
// A mất nhận → KHÔNG được ACK (lần gửi còn mở); hạn giữ của B hết → lượt giao lại nhận lại và gửi đúng một lần.
func TestBFNotifyPool_PushLostClaimIsNotAcked(t *testing.T) {
	f := newFixture(t)
	batch := f.pushBatch()
	bp := newBlockingPusher()
	sp := f.sized(1, bp)
	env := f.pushEnvelope(batch)
	process := func() (bool, error) {
		return inbox.Process(context.Background(), sp.db, port.ConsumerPush, env, sp.mod.PushHandler())
	}
	done := make(chan processResult, 1)
	go func() { ok, err := process(); done <- processResult{ok, err} }()
	waitPush(t, bp)
	f.takeOver("push_delivery", batch, false)
	bp.fail.Store(true)
	close(bp.release)
	if r := awaitProcess(t, done); r.err == nil || r.ok {
		t.Fatalf("A mất nhận, lần gửi chưa gửi: không được ACK sự kiện, ok=%v err=%v", r.ok, r.err)
	}
	if rows := f.deliveryRows("push_delivery", batch); len(rows) != 1 || rows[0].status != "pending" || !rows[0].claimed {
		t.Fatalf("A không được ghi đè nhận của B: %+v", rows)
	}
	f.clk.Advance(2 * time.Minute) // B đã chết: hạn giữ của B hết
	bp.fail.Store(false)
	if ok, err := process(); err != nil || !ok {
		t.Fatalf("giao lại phải nhận lại (nhận của B đã hết hạn) và gửi: ok=%v err=%v", ok, err)
	}
	if n := bp.calls.Load(); n != 2 {
		t.Fatalf("Pusher.Send %d lần, muốn 2 (lần lỗi của A + đúng một lần khi giao lại)", n)
	}
	if rows := f.deliveryRows("push_delivery", batch); len(rows) != 1 || rows[0].status != "sent" || rows[0].claimed {
		t.Fatalf("sau khi giao lại: %+v", rows)
	}
	if ok, err := process(); err != nil || ok {
		t.Fatalf("giao lại sau khi đã ghi inbox: ok=%v err=%v", ok, err)
	}
	if n := bp.calls.Load(); n != 2 {
		t.Fatalf("gửi trùng sau khi đã ghi inbox: %d", n)
	}
}

// A mất nhận nhưng B đã gửi xong (lần gửi kết thúc bền vững) → A ACK, không gửi lại, không ghi đè kết quả của B.
func TestBFNotifyPool_PushLostClaimDoneByOtherAcks(t *testing.T) {
	f := newFixture(t)
	batch := f.pushBatch()
	bp := newBlockingPusher()
	sp := f.sized(1, bp)
	env := f.pushEnvelope(batch)
	done := make(chan processResult, 1)
	go func() {
		ok, err := inbox.Process(context.Background(), sp.db, port.ConsumerPush, env, sp.mod.PushHandler())
		done <- processResult{ok, err}
	}()
	waitPush(t, bp)
	f.takeOver("push_delivery", batch, true)
	bp.fail.Store(true)
	close(bp.release)
	if r := awaitProcess(t, done); r.err != nil || !r.ok {
		t.Fatalf("lần gửi đã kết thúc bởi B: A phải ACK, ok=%v err=%v", r.ok, r.err)
	}
	if rows := f.deliveryRows("push_delivery", batch); len(rows) != 1 || rows[0].status != "sent" || rows[0].claimed {
		t.Fatalf("A ghi đè kết quả của B: %+v", rows)
	}
	if n := bp.calls.Load(); n != 1 {
		t.Fatalf("Pusher.Send %d lần, muốn 1", n)
	}
}

// Review #155 P1 (email): như đẩy — A mất nhận khi lần gửi email chưa gửi → không ACK; giao lại gửi đúng một lần.
func TestBFNotifyPool_EmailLostClaimIsNotAcked(t *testing.T) {
	f := newFixture(t)
	batch, _ := f.emailBatch()
	m := newBlockingMailer()
	sp := f.sizedMail(1, f.pusher, m)
	env := f.event(f.tenant, port.EventEmailRequested, port.EmailRequestedV1{BatchID: batch, Kind: "price", Deliveries: 1})
	process := func() (bool, error) {
		return inbox.Process(context.Background(), sp.db, port.ConsumerEmail, env, sp.mod.EmailHandler())
	}
	done := make(chan processResult, 1)
	go func() { ok, err := process(); done <- processResult{ok, err} }()
	waitEntered(t, m)
	f.takeOver("email_delivery", batch, false)
	m.fail.Store(true)
	close(m.release)
	if r := awaitProcess(t, done); !errors.Is(r.err, notify.ErrEmailRetry) || r.ok {
		t.Fatalf("A mất nhận, email chưa gửi: phải trả ErrEmailRetry, không ACK; ok=%v err=%v", r.ok, r.err)
	}
	if rows := f.deliveryRows("email_delivery", batch); len(rows) != 1 || rows[0].status != "pending" || !rows[0].claimed {
		t.Fatalf("A không được ghi đè nhận của B: %+v", rows)
	}
	f.clk.Advance(2 * time.Minute)
	m.fail.Store(false)
	if ok, err := process(); err != nil || !ok {
		t.Fatalf("giao lại phải nhận lại và gửi email: ok=%v err=%v", ok, err)
	}
	if n := m.calls.Load(); n != 2 {
		t.Fatalf("Mailer.Send %d lần, muốn 2 (lần lỗi của A + đúng một lần khi giao lại)", n)
	}
	if rows := f.deliveryRows("email_delivery", batch); len(rows) != 1 || rows[0].status != "sent" || rows[0].claimed {
		t.Fatalf("sau khi giao lại: %+v", rows)
	}
	if ok, err := process(); err != nil || ok {
		t.Fatalf("giao lại sau khi đã ghi inbox: ok=%v err=%v", ok, err)
	}
	if n := m.calls.Load(); n != 2 {
		t.Fatalf("gửi email trùng sau khi đã ghi inbox: %d", n)
	}
}
