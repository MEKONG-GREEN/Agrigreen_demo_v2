package notify_test

// P0-POOL-SHARED (#126): email tóm tắt không gửi SMTP khi đang giữ giao dịch DB — pool CỠ 1 vẫn phục vụ
// giao dịch khác trong lúc SMTP treo; nhận/kết mục chờ lũy đẳng, lỗi trả mục về hàng chờ, hết hạn nhận thì nhận lại.

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/kn/extension"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/channel"
	notifyport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/notify/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

// blockingMailer chặn Send tới khi test mở release.
type blockingMailer struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
	fail    atomic.Bool
}

func newBlockingMailer() *blockingMailer {
	return &blockingMailer{entered: make(chan struct{}, 8), release: make(chan struct{})}
}

func (m *blockingMailer) Send(ctx context.Context, _ []string, _, _ string) error {
	m.calls.Add(1)
	m.entered <- struct{}{}
	select {
	case <-m.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	if m.fail.Load() {
		return errors.New("fixture SMTP failure")
	}
	return nil
}

func sizedDigestModule(f *fixture, pool *pgxpool.Pool, m channel.Mailer) *notify.Module {
	return notify.New(pool, f.clk, notify.Deps{Kinds: []notifyport.KindRegistration{extension.NotificationKinds()},
		Consumers: []notifyport.ConsumerRegistration{extension.NotificationConsumers()}, Builders: extension.DirectiveNotificationBuilders(nil, nil),
		Digests: []notifyport.DigestRegistration{extension.NotificationDigest()}, Mailer: m, DigestTo: []string{"global@example.vn"}})
}

type digestRow struct {
	sent, claimed bool
}

func digestState(t *testing.T, f *fixture, tid, ref uuid.UUID) digestRow {
	t.Helper()
	var r digestRow
	if err := tenant.WithTenantTx(context.Background(), f.appDB, tid, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT sent_at IS NOT NULL, claim_id IS NOT NULL FROM pf_notify.email_digest_item
		  WHERE directive_id = $1`, ref).Scan(&r.sent, &r.claimed)
	}); err != nil {
		t.Fatal(err)
	}
	return r
}

func waitEntered(t *testing.T, m *blockingMailer) {
	t.Helper()
	select {
	case <-m.entered:
	case <-time.After(30 * time.Second):
		t.Fatal("Mailer.Send không được gọi")
	}
}

func TestDigestSMTPRunsOutsideTxOnSingleConnPool(t *testing.T) {
	f := newFixture(t)
	ref := queueRecipientDigest(t, f, f.tenant)
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	mail := newBlockingMailer()
	f.mod = sizedDigestModule(f, sized, mail)
	run := digestRecipientJob(t, f, f.tenant)
	done := make(chan error, 1)
	go func() { done <- run() }()
	waitEntered(t, mail)

	// SMTP đang treo: kết nối DUY NHẤT của pool phải rảnh — một giao dịch module khác chạy được ngay.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var claimed, sent bool
	if err := tenant.WithTenantTx(ctx, db.ForModule(sized, notify.DBModule), f.tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT claim_id IS NOT NULL, sent_at IS NOT NULL FROM pf_notify.email_digest_item WHERE directive_id = $1`, ref).Scan(&claimed, &sent)
	}); err != nil {
		t.Fatalf("pool cỡ 1 bị giữ trong lúc gửi SMTP: %v", err)
	}
	if !claimed || sent {
		t.Fatal("mục phải đang được nhận (claim) và chưa gửi trong lúc SMTP chạy")
	}
	close(mail.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if st := digestState(t, f, f.tenant, ref); !st.sent || st.claimed {
		t.Fatalf("sau khi gửi: %+v", st)
	}
	// Chạy lại: không gửi trùng.
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if mail.calls.Load() != 1 {
		t.Fatalf("gửi %d lần, muốn 1", mail.calls.Load())
	}
}

func TestDigestSMTPFailureReleasesClaim(t *testing.T) {
	f := newFixture(t)
	ref := queueRecipientDigest(t, f, f.tenant)
	sized := pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	mail := newBlockingMailer()
	mail.fail.Store(true)
	close(mail.release)
	f.mod = sizedDigestModule(f, sized, mail)
	run := digestRecipientJob(t, f, f.tenant)
	if err := run(); err == nil {
		t.Fatal("lỗi SMTP bị nuốt")
	}
	<-mail.entered
	if st := digestState(t, f, f.tenant, ref); st.sent || st.claimed {
		t.Fatalf("lỗi SMTP phải trả mục về hàng chờ: %+v", st)
	}
	mail.fail.Store(false)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	<-mail.entered
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if st := digestState(t, f, f.tenant, ref); !st.sent || st.claimed || mail.calls.Load() != 2 {
		t.Fatalf("gửi lại sau lỗi: %+v, %d lần", st, mail.calls.Load())
	}
}

// Mục đang được worker khác nhận (hạn còn) bị bỏ qua; hết hạn nhận (tiến trình kia chết) thì được nhận lại.
func TestDigestClaimLeaseExpires(t *testing.T) {
	f := newFixture(t)
	ref := queueRecipientDigest(t, f, f.tenant)
	other := ids.New()
	now := f.clk.Now()
	if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
		items, err := store.ClaimDigest(context.Background(), tx, other, now, now.Add(time.Hour), 10)
		if err == nil && len(items) != 1 {
			err = fmt.Errorf("nhận %d mục", len(items))
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	mail := newBlockingMailer()
	close(mail.release)
	f.mod = sizedDigestModule(f, pgtest.Get(t).SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1), mail)
	run := digestRecipientJob(t, f, f.tenant)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	if mail.calls.Load() != 0 {
		t.Fatal("gửi mục đang do worker khác nhận")
	}
	f.clk.Advance(2 * time.Hour)
	if err := run(); err != nil {
		t.Fatal(err)
	}
	<-mail.entered
	if st := digestState(t, f, f.tenant, ref); !st.sent || st.claimed || mail.calls.Load() != 1 {
		t.Fatalf("hết hạn nhận không được nhận lại: %+v", st)
	}
	// Worker cũ "sống lại" kết lần nhận của nó: không còn mục nào giữ claim cũ.
	if err := tenant.WithTenantTx(context.Background(), f.appDB, f.tenant, func(tx pgx.Tx) error {
		n, err := store.MarkDigestSent(context.Background(), tx, other, f.clk.Now())
		if err == nil && n != 0 {
			err = fmt.Errorf("kết %d mục của lần nhận đã hết hạn", n)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
