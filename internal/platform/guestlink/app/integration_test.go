package app_test

import (
	"context"
	"errors"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/guestlink/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type traceKey struct{}
type trackedDB struct{ pool *pgxpool.Pool }
type trackedTx struct {
	pgx.Tx
	active *atomic.Int32
	closed atomic.Bool
}

func (b trackedDB) BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	tx, err := b.pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	active, _ := ctx.Value(traceKey{}).(*atomic.Int32)
	if active != nil {
		active.Add(1)
	}
	return &trackedTx{Tx: tx, active: active}, nil
}
func (t *trackedTx) end() {
	if !t.closed.Swap(true) && t.active != nil {
		t.active.Add(-1)
	}
}
func (t *trackedTx) Commit(ctx context.Context) error   { defer t.end(); return t.Tx.Commit(ctx) }
func (t *trackedTx) Rollback(ctx context.Context) error { defer t.end(); return t.Tx.Rollback(ctx) }
func callbackIO(ctx context.Context, pool *pgxpool.Pool) error {
	if active, ok := ctx.Value(traceKey{}).(*atomic.Int32); ok && active.Load() != 0 {
		return errors.New("callback called inside module transaction")
	}
	var one int
	return pool.QueryRow(ctx, "SELECT 1").Scan(&one) // pool size1, actual I/O, no fake success
}

// A generic test owner, not HRM integration or a verified-contact attestation implementation.
type ownerDouble struct {
	mu      sync.RWMutex
	binding port.Binding
	state   port.State
	pool    *pgxpool.Pool
	calls   atomic.Int32
}

func (o *ownerDouble) Current(ctx context.Context, b port.Binding) (port.State, error) {
	o.calls.Add(1)
	if err := callbackIO(ctx, o.pool); err != nil {
		return port.State{}, err
	}
	o.mu.RLock()
	defer o.mu.RUnlock()
	if b != o.binding {
		return port.State{}, port.ErrDenied
	}
	return o.state, nil
}
func (o *ownerDouble) MayIssue(ctx context.Context, b port.Binding) (port.State, error) {
	p, ok := authz.FromContext(ctx)
	if !ok || !p.Has("issuer") || !p.MFA {
		return port.State{}, port.ErrDenied
	}
	return o.Current(ctx, b)
}
func (o *ownerDouble) change(fn func(*port.State)) { o.mu.Lock(); defer o.mu.Unlock(); fn(&o.state) }

type senderDouble struct {
	mu        sync.Mutex
	pool      *pgxpool.Pool
	sent      map[uuid.UUID]port.Delivery
	attempted map[uuid.UUID]port.Delivery
	mode      port.DeliveryResult
	calls     int
}

func (s *senderDouble) Send(ctx context.Context, d port.Delivery) (port.DeliveryResult, error) {
	if err := callbackIO(ctx, s.pool); err != nil {
		return port.Dropped, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.attempted[d.Key] = d
	if s.mode != port.Sent {
		return s.mode, nil
	}
	if _, exists := s.sent[d.Key]; !exists {
		s.sent[d.Key] = d
	}
	return port.Sent, nil
}
func (s *senderDouble) code(id uuid.UUID) port.Secret {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent[id].Code
}
func (s *senderDouble) delivered() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.sent) }
func (s *senderDouble) setMode(mode port.DeliveryResult) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = mode
}

type guestFx struct {
	t       *testing.T
	pool    *pgxpool.Pool
	b       tenant.Beginner
	clk     *clock.Fixed
	svc     *app.Service
	owner   *ownerDouble
	sender  *senderDouble
	binding port.Binding
	issuer  authz.Principal
	cfg     app.Config
}

func newGuestFx(t *testing.T, adjust func(*app.Config)) *guestFx {
	t.Helper()
	env := pgtest.Get(t)
	pool := env.SizedPool(t, migrations.PF, dbadmin.AppPlatform, 1)
	f := &guestFx{t: t, pool: pool, b: trackedDB{pool: pool}, clk: clock.NewFixed(time.Date(2030, 1, 2, 3, 0, 0, 123456000, time.UTC))}
	f.binding = port.Binding{Tenant: uuid.New(), Owner: "pf/hrm", Subject: uuid.New(), Resource: uuid.New(), Step: "self"}
	f.issuer = authz.Principal{ActorRef: uuid.New(), TenantID: f.binding.Tenant, Roles: []string{"issuer"}, MFA: true}
	f.owner = &ownerDouble{pool: pool, binding: f.binding, state: port.State{Enabled: true, Until: f.clk.Now().Add(8 * time.Hour), OwnerRevision: uuid.New(), ContactRevision: uuid.New(), ContactVerified: true, Channel: "email", Contact: port.NewSecret("verified@example.test")}}
	f.sender = &senderDouble{pool: pool, sent: map[uuid.UUID]port.Delivery{}, attempted: map[uuid.UUID]port.Delivery{}, mode: port.Sent}
	f.cfg = app.Config{Enabled: true, Tenant: f.binding.Tenant, Key: []byte(strings.Repeat("k", 32))}
	if adjust != nil {
		adjust(&f.cfg)
	}
	var err error
	f.svc, err = app.New(f.b, f.clk, f.cfg, map[string]port.Owner{"pf/hrm": f.owner}, f.sender)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *guestFx) ctx(native bool) context.Context {
	ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), traceKey{}, &atomic.Int32{}), 30*time.Second)
	f.t.Cleanup(cancel)
	if native {
		return authz.WithPrincipal(ctx, f.issuer)
	}
	return ctx
}
func (f *guestFx) issue(key string) port.Grant {
	f.t.Helper()
	g, err := f.svc.Issue(f.ctx(true), f.binding, time.Time{}, key)
	if err != nil {
		f.t.Fatal(err)
	}
	return g
}
func (f *guestFx) open(g port.Grant, key string) port.Challenge {
	f.t.Helper()
	c, err := f.svc.Open(f.ctx(false), g.Link, key, "127.0.0.1")
	if err != nil {
		f.t.Fatal(err)
	}
	return c
}
func (f *guestFx) verify(g port.Grant, c port.Challenge) port.Session {
	f.t.Helper()
	s, err := f.svc.Verify(f.ctx(false), g.Link, c.ID, f.sender.code(c.ID), "127.0.0.1")
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}
func (f *guestFx) auditCount(action string) int {
	f.t.Helper()
	var n int
	err := db.WithModuleTenantTx(context.Background(), f.b, app.DBModule, f.binding.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT count(*) FROM pf_guestlink.access_event WHERE action=$1`, action).Scan(&n)
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func TestGuest184RoundTripNarrowBindingAuditAndNoPII(t *testing.T) {
	f := newGuestFx(t, nil)
	g := f.issue("issue-first")
	same := f.issue("issue-first")
	if same.ID != g.ID || same.Link.Reveal() != g.Link.Reveal() {
		t.Fatal("issue retry changed token")
	}
	c := f.open(g, "open-first")
	again := f.open(g, "open-first")
	if again.ID != c.ID || f.sender.delivered() != 1 {
		t.Fatal("open retry sent another code")
	}
	session := f.verify(g, c)
	ctx := f.ctx(false)
	got, err := f.svc.Authorize(ctx, session.Token, "127.0.0.1")
	if err != nil || got != f.binding {
		t.Fatal("wrong guest scope", err)
	}
	if _, ok := authz.FromContext(ctx); ok {
		t.Fatal("capability minted a native principal")
	}
	if _, err = f.svc.Verify(f.ctx(false), g.Link, c.ID, f.sender.code(c.ID), "127.0.0.1"); err == nil {
		t.Fatal("OTP replay accepted")
	}
	if f.auditCount("opened") != 2 || f.auditCount("verified") != 1 || f.auditCount("session_ok") != 1 {
		t.Fatal("missing access audit")
	}
	var data string
	err = db.WithModuleTenantTx(context.Background(), f.b, app.DBModule, f.binding.Tenant, func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), `SELECT coalesce(string_agg(row_to_json(x)::text,''),'') FROM (SELECT to_jsonb(g) AS row FROM pf_guestlink.grant_record g UNION ALL SELECT to_jsonb(e) FROM pf_guestlink.access_event e)x`).Scan(&data)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{g.Link.Reveal(), session.Token.Reveal(), f.sender.code(c.ID).Reveal(), "verified@example.test", "127.0.0.1", "issue-first", "open-first"} {
		if strings.Contains(data, `"`+secret+`"`) {
			t.Fatal("plaintext secret/contact/IP stored")
		}
	}
}

func TestGuest184FiveAttemptsPersistAndResendCannotReset(t *testing.T) {
	f := newGuestFx(t, nil)
	g := f.issue("issue")
	c := f.open(g, "open")
	wrong := port.NewSecret("000000")
	if wrong.Reveal() == f.sender.code(c.ID).Reveal() {
		wrong = port.NewSecret("000001")
	}
	for i := 0; i < 5; i++ {
		if _, err := f.svc.Verify(f.ctx(false), g.Link, c.ID, wrong, "127.0.0.1"); err == nil {
			t.Fatal("wrong code accepted")
		}
	}
	if f.auditCount("otp_wrong") != 5 {
		t.Fatal("denial rolled back attempts/audit")
	}
	if _, err := f.svc.Verify(f.ctx(false), g.Link, c.ID, f.sender.code(c.ID), "127.0.0.1"); err == nil {
		t.Fatal("sixth attempt accepted")
	}
	f.clk.Advance(time.Minute + time.Second)
	next := f.open(g, "resend")
	if _, err := f.svc.Verify(f.ctx(false), g.Link, next.ID, f.sender.code(next.ID), "127.0.0.1"); err == nil {
		t.Fatal("resend reset attempt budget")
	}
	if f.auditCount("verified") != 0 {
		t.Fatal("exhausted code minted session")
	}
}

func TestGuest184OpenLimitAndCooldown(t *testing.T) {
	f := newGuestFx(t, nil)
	g := f.issue("issue")
	f.open(g, "same-open")
	for i := 1; i < 10; i++ {
		f.open(g, "same-open")
	}
	if _, err := f.svc.Open(f.ctx(false), g.Link, "same-open", "127.0.0.1"); err == nil {
		t.Fatal("eleventh open accepted")
	}
	if f.auditCount("opened") != 10 || f.sender.delivered() != 1 {
		t.Fatal("open limit/idempotent delivery wrong")
	}
	other := newGuestFx(t, nil)
	g = other.issue("issue")
	c := other.open(g, "first")
	if _, err := other.svc.Open(other.ctx(false), g.Link, "too-soon", "127.0.0.1"); err == nil {
		t.Fatal("resend ignored cooldown")
	}
	other.clk.Advance(time.Minute + time.Second)
	next := other.open(g, "after-cooldown")
	if next.ID == c.ID {
		t.Fatal("new challenge not created")
	}
	if _, err := other.svc.Verify(other.ctx(false), g.Link, c.ID, other.sender.code(c.ID), "127.0.0.1"); err == nil {
		t.Fatal("old challenge reused after resend")
	}
	other.verify(g, next)
}

func TestGuest184ConcurrentVerifySingleWinnerPoolOne(t *testing.T) {
	f := newGuestFx(t, nil)
	g := f.issue("issue")
	c := f.open(g, "open")
	const workers = 16
	var wg sync.WaitGroup
	var successes atomic.Int32
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.svc.Verify(f.ctx(false), g.Link, c.ID, f.sender.code(c.ID), "127.0.0.1"); err == nil {
				successes.Add(1)
			}
		}()
	}
	wg.Wait()
	if successes.Load() != 1 || f.auditCount("verified") != 1 {
		t.Fatal("OTP verification was not atomic", successes.Load())
	}
}

func TestGuest184ConcurrentIssueAndOpenIdempotency(t *testing.T) {
	f := newGuestFx(t, nil)
	const workers = 8
	grants := make(chan port.Grant, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			g, err := f.svc.Issue(f.ctx(true), f.binding, time.Time{}, "same-issue")
			grants <- g
			errs <- err
		}()
	}
	wg.Wait()
	close(grants)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var g port.Grant
	for x := range grants {
		if g.ID == uuid.Nil {
			g = x
		}
		if x.ID != g.ID || x.Link.Reveal() != g.Link.Reveal() {
			t.Fatal("concurrent issue forked")
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.Open(f.ctx(false), g.Link, "same-open", "127.0.0.1")
			if err != nil {
				t.Errorf("concurrent open: %v", err)
			}
		}()
	}
	wg.Wait()
	if f.sender.delivered() != 1 || f.auditCount("sent") != 1 {
		t.Fatal("delivery retry was duplicated")
	}
}

func TestGuest184LocksRevokeRotationAndRevisions(t *testing.T) {
	t.Run("lock is permanent for the old link even after reopen", func(t *testing.T) {
		f := newGuestFx(t, nil)
		g := f.issue("issue")
		session := f.verify(g, f.open(g, "open"))
		f.owner.change(func(s *port.State) { s.Locked = true })
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
			t.Fatal("locked owner allowed session")
		}
		f.owner.change(func(s *port.State) { s.Locked = false })
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
			t.Fatal("old session revived after reopen")
		}
		newer := f.issue("reopened-new-link")
		f.verify(newer, f.open(newer, "open-new"))
	})
	t.Run("unobserved close-reopen or contact change changes pinned revision", func(t *testing.T) {
		for _, field := range []string{"owner", "contact"} {
			t.Run(field, func(t *testing.T) {
				f := newGuestFx(t, nil)
				g := f.issue("issue")
				session := f.verify(g, f.open(g, "open"))
				f.owner.change(func(s *port.State) {
					if field == "owner" {
						s.OwnerRevision = uuid.New()
					} else {
						s.ContactRevision = uuid.New()
						s.Contact = port.NewSecret("changed@example.test")
					}
				})
				if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
					t.Fatal("revision change left old session valid")
				}
				if _, err := f.svc.Open(f.ctx(false), g.Link, "after-change", "127.0.0.1"); err == nil {
					t.Fatal("revision change left old link usable")
				}
			})
		}
	})
	t.Run("explicit revoke and new link invalidate old session", func(t *testing.T) {
		f := newGuestFx(t, nil)
		g := f.issue("issue")
		session := f.verify(g, f.open(g, "open"))
		replacement := f.issue("replace")
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
			t.Fatal("rotation did not invalidate old session")
		}
		current := f.verify(replacement, f.open(replacement, "new-open"))
		if err := f.svc.Revoke(f.ctx(true), f.binding); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Authorize(f.ctx(false), current.Token, "127.0.0.1"); err == nil {
			t.Fatal("revocation did not invalidate session")
		}
		var actor uuid.UUID
		err := db.WithModuleTenantTx(context.Background(), f.b, app.DBModule, f.binding.Tenant, func(tx pgx.Tx) error {
			return tx.QueryRow(context.Background(), `SELECT actor_ref FROM pf_guestlink.access_event WHERE action='revoked' AND reason='issuer' ORDER BY recorded_at DESC LIMIT 1`).Scan(&actor)
		})
		if err != nil || actor != f.issuer.ActorRef {
			t.Fatal("revoke audit lost native issuer", err)
		}
	})
}

func TestGuest184TTLsDoNotSlideOrRevive(t *testing.T) {
	t.Run("link and session are capped by earliest deadline", func(t *testing.T) {
		f := newGuestFx(t, func(c *app.Config) { c.LinkTTL = 2 * time.Minute })
		g := f.issue("issue")
		challenge := f.open(g, "open")
		session := f.verify(g, challenge)
		if challenge.Until.After(g.Until) || session.Until.After(g.Until) {
			t.Fatal("deadline cap ignored")
		}
		f.clk.Advance(2*time.Minute + time.Second)
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
			t.Fatal("expired link allowed session")
		}
		f.clk.Advance(-2 * time.Minute)
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
			t.Fatal("expired link revived after clock rollback")
		}
	})
	t.Run("OTP expiry requires a new challenge", func(t *testing.T) {
		f := newGuestFx(t, func(c *app.Config) { c.OTPTTL = time.Minute })
		g := f.issue("issue")
		old := f.open(g, "old")
		f.clk.Advance(time.Minute + time.Second)
		if _, err := f.svc.Verify(f.ctx(false), g.Link, old.ID, f.sender.code(old.ID), "127.0.0.1"); err == nil {
			t.Fatal("expired challenge accepted")
		}
		current := f.open(g, "new")
		f.verify(g, current)
	})
	t.Run("session use does not extend its TTL", func(t *testing.T) {
		f := newGuestFx(t, func(c *app.Config) { c.SessionTTL = time.Minute })
		g := f.issue("issue")
		session := f.verify(g, f.open(g, "open"))
		f.clk.Advance(30 * time.Second)
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err != nil {
			t.Fatal(err)
		}
		f.clk.Advance(31 * time.Second)
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
			t.Fatal("session TTL slid on access")
		}
		f.clk.Advance(-30 * time.Second)
		if _, err := f.svc.Authorize(f.ctx(false), session.Token, "127.0.0.1"); err == nil {
			t.Fatal("expired session revived on clock rollback")
		}
	})
}

func TestGuest184MissingDependenciesAndVerifiedContact(t *testing.T) {
	for _, kind := range []string{"unverified", "missing_revision", "header_injection", "phone_in_email_mode"} {
		t.Run(kind, func(t *testing.T) {
			f := newGuestFx(t, nil)
			f.owner.change(func(s *port.State) {
				switch kind {
				case "unverified":
					s.ContactVerified = false
				case "missing_revision":
					s.ContactRevision = uuid.Nil
				case "header_injection":
					s.Contact = port.NewSecret("verified@example.test\nBcc: victim@example.test")
				case "phone_in_email_mode":
					s.Contact = port.NewSecret("0912345678")
				}
			})
			if _, err := f.svc.Issue(f.ctx(true), f.binding, time.Time{}, "issue"); err == nil {
				t.Fatal("unsafe contact allowed link")
			}
			if f.sender.delivered() != 0 {
				t.Fatal("unsafe contact sent code")
			}
		})
	}
	t.Run("disabled delivery never verifies and retry keeps challenge", func(t *testing.T) {
		f := newGuestFx(t, nil)
		g := f.issue("issue")
		f.sender.setMode(port.Disabled)
		c, err := f.svc.Open(f.ctx(false), g.Link, "same-open", "127.0.0.1")
		if err == nil || c.Delivery == port.Sent {
			t.Fatal("disabled sender reported sent")
		}
		// Test observes the attempted fake delivery; even a correct code is unusable until delivery was confirmed.
		code := f.sender.attemptedCode(c.ID)
		if _, err = f.svc.Verify(f.ctx(false), g.Link, c.ID, code, "127.0.0.1"); err == nil {
			t.Fatal("undelivered OTP verified")
		}
		f.sender.setMode(port.Sent)
		again := f.open(g, "same-open")
		if again.ID != c.ID || f.sender.delivered() != 1 {
			t.Fatal("delivery retry changed challenge or duplicated message")
		}
		f.verify(g, again)
	})
}

func TestGuest184RateBucketsCannotBeResetByIPAliasOrRotation(t *testing.T) {
	t.Run("IPv6 zone aliases share the same IP quota", func(t *testing.T) {
		f := newGuestFx(t, func(c *app.Config) { c.MaxIP = 1 })
		g := f.issue("issue")
		if _, err := f.svc.Open(f.ctx(false), port.NewSecret("invalid"), "bad", "2001:db8::1%first"); err == nil {
			t.Fatal("bad token accepted")
		}
		if _, err := f.svc.Open(f.ctx(false), g.Link, "open", "2001:db8::1%second"); err == nil {
			t.Fatal("IP alias bypassed quota")
		}
		if f.sender.delivered() != 0 || f.auditCount("quota") < 1 {
			t.Fatal("rate rejection missing")
		}
	})
	t.Run("subject quota survives a new link and a new IP", func(t *testing.T) {
		f := newGuestFx(t, func(c *app.Config) { c.MaxSubject = 1 })
		g := f.issue("issue")
		f.open(g, "open")
		newer := f.issue("replace")
		if _, err := f.svc.Open(f.ctx(false), newer.Link, "new-open", "127.0.0.2"); err == nil {
			t.Fatal("rotation/IP reset subject budget")
		}
		f.clk.Advance(15*time.Minute + time.Second)
		if _, err := f.svc.Open(f.ctx(false), newer.Link, "after-window", "127.0.0.2"); err != nil {
			t.Fatal("expired rate window never cleared", err)
		}
	})
}

func (s *senderDouble) attemptedCode(id uuid.UUID) port.Secret {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attempted[id].Code
}

func TestGuest184NativePermissionScopeAndRLS(t *testing.T) {
	f := newGuestFx(t, nil)
	for _, kind := range []string{"anonymous", "role", "mfa", "tenant", "subject", "resource", "step", "owner"} {
		t.Run(kind, func(t *testing.T) {
			b := f.binding
			ctx := f.ctx(false)
			p := f.issuer
			switch kind {
			case "role":
				p.Roles = []string{"viewer"}
			case "mfa":
				p.MFA = false
			case "tenant":
				p.TenantID = uuid.New()
			case "subject":
				b.Subject = uuid.New()
			case "resource":
				b.Resource = uuid.New()
			case "step":
				b.Step = "leader"
			case "owner":
				b.Owner = "pf/other"
			}
			if kind != "anonymous" {
				ctx = authz.WithPrincipal(ctx, p)
			}
			if _, err := f.svc.Issue(ctx, b, time.Time{}, "forbidden-"+kind); err == nil {
				t.Fatal("unauthorized scope issued")
			}
		})
	}
	g := f.issue("allowed")
	session := f.verify(g, f.open(g, "open"))
	for _, kind := range []string{"link_as_session", "session_as_link", "altered", "oversized", "other_tenant"} {
		t.Run(kind, func(t *testing.T) {
			switch kind {
			case "link_as_session":
				if _, err := f.svc.Authorize(f.ctx(false), g.Link, "127.0.0.1"); err == nil {
					t.Fatal("purpose confusion")
				}
			case "session_as_link":
				if _, err := f.svc.Open(f.ctx(false), session.Token, "mixed", "127.0.0.1"); err == nil {
					t.Fatal("purpose confusion")
				}
			case "altered":
				raw := g.Link.Reveal()
				replacement := "A"
				if raw[len(raw)-1:] == replacement {
					replacement = "B"
				}
				if _, err := f.svc.Open(f.ctx(false), port.NewSecret(raw[:len(raw)-1]+replacement), "tamper", "127.0.0.1"); err == nil {
					t.Fatal("tampered token accepted")
				}
			case "oversized":
				if _, err := f.svc.Open(f.ctx(false), port.NewSecret(strings.Repeat("1e100000", 20000)), "long", "127.0.0.1"); err == nil {
					t.Fatal("oversized token accepted")
				}
			case "other_tenant":
				other := newGuestFx(t, nil)
				if _, err := other.svc.Authorize(other.ctx(false), session.Token, "127.0.0.1"); err == nil {
					t.Fatal("cross-tenant token accepted")
				}
			}
		})
	}
	var visible int
	err := db.WithModuleTenantTx(context.Background(), f.b, app.DBModule, uuid.New(), func(tx pgx.Tx) error {
		return tx.QueryRow(context.Background(), "SELECT count(*) FROM pf_guestlink.grant_record").Scan(&visible)
	})
	if err != nil || visible != 0 {
		t.Fatal("tenant RLS leaked grants", err, visible)
	}
	if f.sender.delivered() != 1 {
		t.Fatal("denial triggered delivery")
	}
}

func TestGuest184NewSessionReplacesPreviousAndConsumedOpenDenies(t *testing.T) {
	f := newGuestFx(t, nil)
	g := f.issue("issue")
	first := f.open(g, "first")
	old := f.verify(g, first)
	if _, err := f.svc.Open(f.ctx(false), g.Link, "first", "127.0.0.1"); err == nil {
		t.Fatal("consumed challenge reopened")
	}
	f.clk.Advance(time.Minute + time.Second)
	current := f.verify(g, f.open(g, "second"))
	if _, err := f.svc.Authorize(f.ctx(false), old.Token, "127.0.0.1"); err == nil {
		t.Fatal("replaced session accepted")
	}
	if got, err := f.svc.Authorize(f.ctx(false), current.Token, "127.0.0.1"); err != nil || got != f.binding {
		t.Fatal("current session denied", err)
	}
}
