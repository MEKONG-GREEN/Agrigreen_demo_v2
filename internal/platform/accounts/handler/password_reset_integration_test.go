package handler_test

// #255 AUTH-V2-RESET-NOPIN (ADR-0056; BRD 2.0 Báo giá v2.0 FN-CH-00.01 "Quên mật khẩu?"): yêu cầu không lộ email, tối đa
// 5 yêu cầu/giờ/tài khoản, mã chỉ lưu băm, dùng một lần, hết hạn 30 phút, sai mã bị từ chối, đổi xong thu hồi phiên.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/keycloak"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/accounts/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	shareddb "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/db"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

type sentMail struct {
	to   []string
	body string
}

// fakeMailer ghi email đã gửi; fail ≠ nil → Send lỗi.
type fakeMailer struct {
	mu   sync.Mutex
	sent []sentMail
	fail error
}

func (m *fakeMailer) Send(_ context.Context, to []string, _ string, body string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return m.fail
	}
	m.sent = append(m.sent, sentMail{to: to, body: body})
	return nil
}

func (m *fakeMailer) all() []sentMail {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMail(nil), m.sent...)
}

type resetFixture struct {
	t      *testing.T
	clk    *clock.Fixed
	dir    *keycloak.Memory
	mail   *fakeMailer
	mod    *accounts.Module
	db     tenant.Beginner
	srv    *httptest.Server
	tenant uuid.UUID
}

const resetBase = "https://kn.example.vn/login/dat-lai-mat-khau"

func newResetFixture(t *testing.T) *resetFixture {
	env := pgtest.Get(t)
	clk := clock.NewFixed(time.Now())
	dir := keycloak.NewMemory()
	mail := &fakeMailer{}
	unit := env.Pool(t, migrations.PF, dbadmin.AppPlatform)
	mod := accounts.New(unit, clk, accounts.Deps{Directory: dir})
	mod.UsePasswordReset(mail, resetBase, telemetry.NewLogger("test", io.Discard))
	eng := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<20)
	mod.RegisterPublic(eng.Group("/v1/public"))
	srv := httptest.NewServer(eng)
	t.Cleanup(srv.Close)
	return &resetFixture{t: t, clk: clk, dir: dir, mail: mail, mod: mod, db: shareddb.ForModule(unit, accounts.DBModule), srv: srv, tenant: ids.New()}
}

func (f *resetFixture) user(email string, enabled bool) uuid.UUID {
	id := f.dir.Seed(port.NewUser{Username: strings.Split(email, "@")[0], Email: email, FirstName: "Cán", LastName: "Bộ", TenantID: f.tenant, OrgRef: ids.New()}, enabled, "kn_officer")
	f.dir.SetEmailVerified(id, true)
	return id
}

func (f *resetFixture) post(path string, body any) (int, []byte) {
	f.t.Helper()
	b, _ := json.Marshal(body)
	r, err := http.Post(f.srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		f.t.Fatal(err)
	}
	defer func() { _ = r.Body.Close() }()
	raw, _ := io.ReadAll(r.Body)
	return r.StatusCode, raw
}

func (f *resetFixture) request(email string) []byte {
	f.t.Helper()
	st, raw := f.post("/v1/public/password-reset", map[string]any{"email": email})
	if st != http.StatusAccepted {
		f.t.Fatalf("request %d %s", st, raw)
	}
	return raw
}

func (f *resetFixture) confirm(token, password string) (int, string) {
	f.t.Helper()
	st, raw := f.post("/v1/public/password-reset/confirm", map[string]any{"token": token, "password": password})
	var p struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(raw, &p)
	return st, p.Code
}

type resetRow struct {
	id        uuid.UUID
	hash      []byte
	sent, use *time.Time
}

func (f *resetFixture) rows(user uuid.UUID) []resetRow {
	f.t.Helper()
	var out []resetRow
	err := tenant.WithTenantTx(context.Background(), f.db, f.tenant, func(tx pgx.Tx) error {
		rs, err := tx.Query(context.Background(), `SELECT id, token_hash, sent_at, used_at FROM pf_accounts.password_reset
		  WHERE user_ref = $1 ORDER BY requested_at, id`, user)
		if err != nil {
			return err
		}
		defer rs.Close()
		for rs.Next() {
			var r resetRow
			if err := rs.Scan(&r.id, &r.hash, &r.sent, &r.use); err != nil {
				return err
			}
			out = append(out, r)
		}
		return rs.Err()
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

// deliver chạy consumer cho yêu cầu (như cmd/worker nhận pf.accounts.password_reset.requested).
func (f *resetFixture) deliver(r resetRow, user uuid.UUID) error {
	b, _ := json.Marshal(port.PasswordResetRequestedV1{ResetID: r.id, UserRef: user, RequestedAt: f.clk.Now().Format(time.RFC3339Nano)})
	env := outbox.Envelope{EventID: ids.New(), EventType: port.EventPasswordResetRequested, SchemaVersion: 1, TenantID: f.tenant,
		OccurredAt: f.clk.Now(), Producer: "pf/accounts", Payload: b}
	return f.mod.PasswordResetHandler()(context.Background(), nil, env)
}

var tokenRe = regexp.MustCompile(`/login/dat-lai-mat-khau#token=([A-Za-z0-9_-]{86})`)

// link yêu cầu, gửi và trả mã trong email mới nhất.
func (f *resetFixture) link(email string, user uuid.UUID) string {
	f.t.Helper()
	f.request(email)
	rs := f.rows(user)
	if err := f.deliver(rs[len(rs)-1], user); err != nil {
		f.t.Fatal(err)
	}
	mails := f.mail.all()
	m := tokenRe.FindStringSubmatch(mails[len(mails)-1].body)
	if m == nil {
		f.t.Fatalf("email không có liên kết đặt lại: %q", mails[len(mails)-1].body)
	}
	return m[1]
}

const newPassword = "Mat-khau-moi-2026!"

func TestPasswordResetHappyPathStoresOnlyHashAndRevokesSessions(t *testing.T) {
	f := newResetFixture(t)
	user := f.user("can.bo.a@kn.example.vn", true)
	tok := f.link("  Can.Bo.A@KN.example.vn ", user)

	mails := f.mail.all()
	if len(mails) != 1 || len(mails[0].to) != 1 || mails[0].to[0] != "can.bo.a@kn.example.vn" {
		t.Fatalf("email gửi tới chính tài khoản: %+v", mails)
	}
	rs := f.rows(user)
	raw, _ := base64.RawURLEncoding.DecodeString(tok)
	sum := sha256.Sum256(raw[32:])
	if len(rs) != 1 || rs[0].sent == nil || !bytes.Equal(rs[0].hash, sum[:]) || bytes.Contains(rs[0].hash, raw[32:]) {
		t.Fatalf("DB chỉ giữ SHA-256 của phần bí mật: %+v", rs)
	}
	// Mật khẩu không đạt chính sách → 422, liên kết vẫn dùng lại được.
	if st, code := f.confirm(tok, "ngan"); st != http.StatusUnprocessableEntity || code != "accounts.password_policy" {
		t.Fatalf("mật khẩu yếu: %d %s", st, code)
	}
	before := f.dir.Logouts(user)
	if st, code := f.confirm(tok, newPassword); st != http.StatusNoContent {
		t.Fatalf("đặt lại: %d %s", st, code)
	}
	if !f.dir.PasswordMatches(user, newPassword) {
		t.Fatal("Keycloak chưa nhận mật khẩu mới")
	}
	if f.dir.Logouts(user) < before+2 {
		t.Fatalf("phải thu hồi phiên trước và sau khi đổi mật khẩu: %d → %d", before, f.dir.Logouts(user))
	}
	if rs := f.rows(user); rs[0].use == nil {
		t.Fatal("mã chưa được đánh dấu đã dùng")
	}
	// Redelivery của sự kiện đã gửi không gửi email thứ hai.
	if err := f.deliver(f.rows(user)[0], user); err != nil || len(f.mail.all()) != 1 {
		t.Fatalf("giao lại sự kiện đã gửi: %v, %d email", err, len(f.mail.all()))
	}
}

func TestPasswordResetLinkIsSingleUse(t *testing.T) {
	f := newResetFixture(t)
	user := f.user("can.bo.b@kn.example.vn", true)
	tok := f.link("can.bo.b@kn.example.vn", user)
	if st, _ := f.confirm(tok, newPassword); st != http.StatusNoContent {
		t.Fatalf("lần đầu: %d", st)
	}
	if st, code := f.confirm(tok, "Mat-khau-khac-2026!"); st != http.StatusGone || code != "accounts.reset_link_invalid" {
		t.Fatalf("dùng lại mã: %d %s", st, code)
	}
	if !f.dir.PasswordMatches(user, newPassword) {
		t.Fatal("dùng lại mã không được đổi mật khẩu")
	}
}

func TestPasswordResetLinkExpiresAfter30Minutes(t *testing.T) {
	f := newResetFixture(t)
	user := f.user("can.bo.c@kn.example.vn", true)
	tok := f.link("can.bo.c@kn.example.vn", user)
	f.clk.Advance(30*time.Minute + time.Second)
	if st, code := f.confirm(tok, newPassword); st != http.StatusGone || code != "accounts.reset_link_invalid" {
		t.Fatalf("mã hết hạn: %d %s", st, code)
	}
	if f.dir.PasswordMatches(user, newPassword) {
		t.Fatal("mã hết hạn không được đổi mật khẩu")
	}
}

func TestPasswordResetWrongOrTamperedTokenRejected(t *testing.T) {
	f := newResetFixture(t)
	user := f.user("can.bo.d@kn.example.vn", true)
	tok := f.link("can.bo.d@kn.example.vn", user)
	flip := []byte(tok)
	if flip[60] == 'A' {
		flip[60] = 'B'
	} else {
		flip[60] = 'A'
	}
	forged := tok[:43] + strings.Repeat("A", 43) // đúng tenant + id, sai bí mật
	for name, bad := range map[string]string{"sửa một ký tự": string(flip), "sai bí mật": forged, "sai định dạng": "abc", "rỗng": ""} {
		if st, code := f.confirm(bad, newPassword); st != http.StatusGone || code != "accounts.reset_link_invalid" {
			t.Fatalf("%s: %d %s", name, st, code)
		}
	}
	if f.dir.PasswordMatches(user, newPassword) {
		t.Fatal("mã sai không được đổi mật khẩu")
	}
	// Mã đúng vẫn dùng được sau các lần thử sai.
	if st, _ := f.confirm(tok, newPassword); st != http.StatusNoContent {
		t.Fatalf("mã đúng: %d", st)
	}
}

func TestPasswordResetRateLimitFivePerHourWithoutLeak(t *testing.T) {
	f := newResetFixture(t)
	user := f.user("can.bo.e@kn.example.vn", true)
	want := f.request("khong.ton.tai@kn.example.vn")
	locked := f.user("bi.khoa@kn.example.vn", false)
	if got := f.request("bi.khoa@kn.example.vn"); !bytes.Equal(got, want) || len(f.rows(locked)) != 0 {
		t.Fatalf("tài khoản bị khóa: thân %s, %d dòng", got, len(f.rows(locked)))
	}
	for i := range 7 {
		if got := f.request("can.bo.e@kn.example.vn"); !bytes.Equal(got, want) {
			t.Fatalf("yêu cầu %d lộ khác biệt: %s vs %s", i+1, got, want)
		}
		f.clk.Advance(time.Minute)
	}
	if n := len(f.rows(user)); n != 5 {
		t.Fatalf("tối đa 5 yêu cầu/giờ/tài khoản, có %d", n)
	}
	f.clk.Advance(time.Hour)
	f.request("can.bo.e@kn.example.vn")
	if n := len(f.rows(user)); n != 6 {
		t.Fatalf("sau 1 giờ được yêu cầu lại: %d dòng", n)
	}
	if st, _ := f.post("/v1/public/password-reset", map[string]any{"email": "khong-phai-email"}); st != http.StatusUnprocessableEntity {
		t.Fatalf("email sai cú pháp: %d", st)
	}
}

func TestPasswordResetNewLinkUseRetiresOlderLinksAndMailFailureRetries(t *testing.T) {
	f := newResetFixture(t)
	user := f.user("can.bo.f@kn.example.vn", true)
	old := f.link("can.bo.f@kn.example.vn", user)

	f.mail.fail = errors.New("smtp tạm lỗi")
	f.request("can.bo.f@kn.example.vn")
	rs := f.rows(user)
	if err := f.deliver(rs[1], user); err == nil {
		t.Fatal("gửi lỗi phải trả lỗi để giao lại")
	}
	if rs := f.rows(user); rs[1].sent != nil {
		t.Fatal("gửi lỗi không được ghi sent_at")
	}
	f.mail.fail = nil
	if err := f.deliver(rs[1], user); err != nil {
		t.Fatal(err)
	}
	mails := f.mail.all()
	fresh := tokenRe.FindStringSubmatch(mails[len(mails)-1].body)[1]
	if st, _ := f.confirm(fresh, newPassword); st != http.StatusNoContent {
		t.Fatalf("liên kết mới: %d", st)
	}
	if st, code := f.confirm(old, "Mat-khau-khac-2026!"); st != http.StatusGone || code != "accounts.reset_link_invalid" {
		t.Fatalf("liên kết cũ phải hết dùng: %d %s", st, code)
	}
}

func TestPasswordResetIPRateLimitSameForAnyEmail(t *testing.T) {
	f := newResetFixture(t)
	f.user("can.bo.g@kn.example.vn", true)
	for i := range 5 {
		email := "khong.ton.tai@kn.example.vn"
		if i%2 == 0 {
			email = "can.bo.g@kn.example.vn"
		}
		f.request(email)
	}
	for _, email := range []string{"can.bo.g@kn.example.vn", "khong.ton.tai@kn.example.vn"} {
		if st, raw := f.post("/v1/public/password-reset", map[string]any{"email": email}); st != http.StatusTooManyRequests {
			t.Fatalf("vượt giới hạn IP (%s): %d %s", email, st, raw)
		}
	}
	f.clk.Advance(time.Minute)
	f.request("khong.ton.tai@kn.example.vn")
}
