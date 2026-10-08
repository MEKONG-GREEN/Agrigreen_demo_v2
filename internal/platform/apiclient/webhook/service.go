// Package webhook: webhook MỎNG + ngắt mạch của cổng tích hợp (W5.7 — R-INT-07, R-INT-09; ADR-0033 §4), chủ
// pf/apiclient (schema pf_apiclient, pf 000130).
//
//   - Đăng ký (Admin API của api-platform, MFA, api_client_admin): URL https qua Guard (chặn SSRF), feed phải có
//     scope feed:<tên>:read đã duyệt bốn mắt, ngưỡng ngắt N (mặc định 5). Bí mật dẫn xuất từ khóa chủ, hiện MỘT lần;
//     xoay có chồng lấn 24 giờ (ký cả hai).
//   - Giao (Dispatcher, job apiclient_webhook_dispatch ở leader cmd/worker): thân CHỈ {feed, cursor, occurred_at},
//     ký HMAC (dấu thời gian + thân); lỗi → lùi hàm mũ có nhiễu; N lỗi liên tiếp → mở mạch, thử half-open sau thời
//     gian chờ; mọi lần giao vào sổ webhook_delivery; metric ag.apiclient.webhook.*.
//   - Trạng thái (kể cả ngắt mạch) đọc qua Admin API và GET /v1/webhooks của chính đối tác.
package webhook

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/app"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/store"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Mã lỗi (docs/errors.md, mục pf/apiclient — webhook).
var (
	ErrNotFound      = problem.New(http.StatusNotFound, "apiclient.webhook_not_found", "Không tìm thấy webhook của khách hàng API này")
	ErrURLRejected   = problem.New(http.StatusUnprocessableEntity, "apiclient.webhook_url_rejected", "URL webhook bị từ chối")
	ErrTooMany       = problem.New(http.StatusConflict, "apiclient.too_many_webhooks", "Khách hàng đã có tối đa số webhook đang hoạt động")
	ErrFeedNotScoped = problem.New(http.StatusUnprocessableEntity, "apiclient.webhook_feed_not_subscribed",
		"Feed không có hợp đồng hoặc khách hàng chưa có scope feed:<tên>:read đã duyệt")
)

// Giới hạn.
const (
	MaxActivePerClient = 5
	MaxFeeds           = 20
	// SecretOverlap là thời gian bí mật cũ còn ký sau khi xoay.
	SecretOverlap = 24 * time.Hour
)

// Hành động sổ.
const (
	ActRegistered    = "registered"
	ActSecretRotated = "secret_rotated"
	ActDisabled      = "disabled"
	ActEnabled       = "enabled"
	ActBreakerReset  = "breaker_reset"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// Service là use case quản trị và đọc webhook.
type Service struct {
	DB   tenant.Beginner // pool bọc vai r_apiclient
	Keys *Keyring        // nil → đăng ký / xoay trả 503
	// Guard: chính sách URL (nil-giá trị = mặc định production).
	Guard Guard
	// OperatorTenant: tenant đơn vị vận hành; Nil → tenant người gọi Admin API.
	OperatorTenant uuid.UUID
	Clock          clock.Clock
	Rand           io.Reader
	Log            *slog.Logger
}

func (s *Service) now() time.Time {
	if s.Clock == nil {
		return time.Now()
	}
	return s.Clock.Now()
}

func (s *Service) operator(ctx context.Context, roles ...string) (authz.Principal, error) {
	p, err := authz.Require(ctx, roles...)
	if err != nil {
		return p, err
	}
	if s.OperatorTenant != uuid.Nil && p.TenantID != s.OperatorTenant {
		return p, app.ErrNotOperator
	}
	return p, nil
}

func (s *Service) nonce() (string, error) {
	r := s.Rand
	if r == nil {
		r = rand.Reader
	}
	b := make([]byte, 16)
	if _, err := io.ReadFull(r, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

var feedRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,47}$`)

// RegisterInput là dữ liệu đăng ký.
type RegisterInput struct {
	URL       string
	Feeds     []string
	Threshold int // 0 → DefaultThreshold
	IdemKey   string
}

// Issued là kết quả đăng ký / xoay: Secret CHỈ có ở lời gọi đầu (gọi lặp cùng Idempotency-Key → rỗng).
type Issued struct {
	Registration
	Secret string
}

func reasonOK(r string) bool {
	n := len([]rune(r))
	return n >= 10 && n <= 500
}

// Register đăng ký webhook cho khách hàng active (api_client_admin).
func (s *Service) Register(ctx context.Context, clientID uuid.UUID, in RegisterInput) (Issued, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return Issued{}, err
	}
	if in.IdemKey == "" {
		return Issued{}, problem.IdempotencyRequired
	}
	if s.Keys == nil {
		return Issued{}, app.ErrDependencyUnavailable.WithDetail("chưa cấu hình khóa chủ webhook (" + EnvKeyPrefix + "<n>)")
	}
	threshold := in.Threshold
	if threshold == 0 {
		threshold = DefaultThreshold
	}
	if threshold < 1 || threshold > 100 {
		return Issued{}, problem.Validation.WithField("failure_threshold", "1–100")
	}
	feeds := slices.Clone(in.Feeds)
	slices.Sort(feeds)
	feeds = slices.Compact(feeds)
	if len(feeds) == 0 || len(feeds) > MaxFeeds {
		return Issued{}, problem.Validation.WithField("feeds", "1–20 tên feed")
	}
	for _, f := range feeds {
		if !feedRe.MatchString(f) {
			return Issued{}, problem.Validation.WithField("feeds", "tên feed [a-z][a-z0-9_]{2,47}")
		}
		if _, err := app.FeedContract(f, 0); err != nil {
			return Issued{}, ErrFeedNotScoped.WithDetail("Feed " + f + " không có hợp đồng tích hợp")
		}
	}
	u, err := s.Guard.ValidateURL(ctx, in.URL)
	if err != nil {
		return Issued{}, urlProblem(err)
	}
	nonce, err := s.nonce()
	if err != nil {
		return Issued{}, err
	}
	now := s.now().UTC()
	id := ids.New()
	keyV, secretV := s.Keys.Current, 1
	idem := "webhook:register:" + clientID.String() + ":" + in.IdemKey
	var out Issued
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := webhookIDByIdem(ctx, tx, idem); err != nil || prev != uuid.Nil {
			if err != nil {
				return err
			}
			r, err := regByID(ctx, tx, clientID, prev, false)
			if err == nil && r != nil {
				out = Issued{Registration: *r}
			}
			return err
		}
		c, err := store.ClientByID(ctx, tx, clientID, true)
		if err != nil {
			return err
		}
		if c == nil {
			return app.ErrClientNotFound
		}
		if c.Status != string(port.ClientActive) {
			return app.ErrState.WithDetail("Khách hàng API chưa active")
		}
		scopes, err := store.ActiveScopes(ctx, tx, clientID)
		if err != nil {
			return err
		}
		for _, f := range feeds {
			if !slices.Contains(scopes, port.FeedScope(f)) {
				return ErrFeedNotScoped.WithDetail("Thiếu scope " + port.FeedScope(f) + " đã duyệt")
			}
		}
		n, err := countActive(ctx, tx, clientID)
		if err != nil {
			return err
		}
		if n >= MaxActivePerClient {
			return ErrTooMany
		}
		url := u.String()
		e := event{ID: ids.New(), TenantID: p.TenantID, WebhookID: id, ClientID: clientID, Action: ActRegistered, URL: &url, Feeds: feeds,
			Threshold: &threshold, SecretVersion: &secretV, KeyVersion: &keyV, Nonce: &nonce, At: now, Actor: p.ActorRef, IdemKey: idem}
		if err := insertEvent(ctx, tx, &e); err != nil {
			return err
		}
		r := Registration{TenantID: p.TenantID, ID: id, ClientID: clientID, URL: url, Host: strings.ToLower(u.Hostname()), Feeds: feeds,
			Status: StatusActive, Threshold: threshold, SecretVersion: secretV, KeyVersion: keyV, Nonce: nonce, CreatedBy: p.ActorRef,
			CreatedAt: now, LastEventID: e.ID, LastSeq: e.Seq, Breaker: Breaker{State: StateClosed}}
		if err := putReg(ctx, tx, r); err != nil {
			return err
		}
		if err := putBreaker(ctx, tx, p.TenantID, id, r.Breaker); err != nil {
			return err
		}
		raw, _ := s.Keys.Secret(keyV, id, secretV, nonce)
		out = Issued{Registration: r, Secret: FormatSecret(raw)}
		return nil
	})
	if err == nil && out.Secret != "" {
		s.log().InfoContext(ctx, "webhook: đăng ký", "event", "apiclient.webhook_registered", "webhook_id", id, "client_id", clientID,
			"host", out.Host, "feeds", feeds, "outcome", "ok")
	}
	return out, err
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

func urlProblem(err error) error {
	var re *RejectedError
	if errors.As(err, &re) {
		return ErrURLRejected.WithDetail("URL webhook bị từ chối: " + re.Reason)
	}
	return ErrURLRejected.WithCause(err)
}

// RotateSecret xoay bí mật (api_client_admin): bí mật cũ còn ký thêm SecretOverlap.
func (s *Service) RotateSecret(ctx context.Context, clientID, webhookID uuid.UUID, idemKey string) (Issued, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return Issued{}, err
	}
	if idemKey == "" {
		return Issued{}, problem.IdempotencyRequired
	}
	if s.Keys == nil {
		return Issued{}, app.ErrDependencyUnavailable.WithDetail("chưa cấu hình khóa chủ webhook (" + EnvKeyPrefix + "<n>)")
	}
	nonce, err := s.nonce()
	if err != nil {
		return Issued{}, err
	}
	now := s.now().UTC()
	idem := "webhook:rotate:" + webhookID.String() + ":" + idemKey
	var out Issued
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		if prev, err := webhookIDByIdem(ctx, tx, idem); err != nil || prev != uuid.Nil {
			if err != nil {
				return err
			}
			r, err := regByID(ctx, tx, clientID, prev, false)
			if err == nil && r != nil {
				out = Issued{Registration: *r}
			}
			return err
		}
		r, err := regByID(ctx, tx, clientID, webhookID, true)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrNotFound
		}
		if r.Status != StatusActive {
			return app.ErrState.WithDetail("Webhook không active")
		}
		keyV, secretV, until := s.Keys.Current, r.SecretVersion+1, now.Add(SecretOverlap)
		e := event{ID: ids.New(), TenantID: p.TenantID, WebhookID: webhookID, ClientID: clientID, Action: ActSecretRotated,
			SecretVersion: &secretV, KeyVersion: &keyV, Nonce: &nonce, PrevValidUntil: &until, At: now, Actor: p.ActorRef, IdemKey: idem}
		if err := insertEvent(ctx, tx, &e); err != nil {
			return err
		}
		pv, pk, pn := r.SecretVersion, r.KeyVersion, r.Nonce
		r.PrevVersion, r.PrevKeyVersion, r.PrevNonce, r.PrevValidUntil = &pv, &pk, &pn, &until
		r.SecretVersion, r.KeyVersion, r.Nonce, r.LastEventID, r.LastSeq = secretV, keyV, nonce, e.ID, e.Seq
		if err := putReg(ctx, tx, *r); err != nil {
			return err
		}
		raw, _ := s.Keys.Secret(keyV, webhookID, secretV, nonce)
		out = Issued{Registration: *r, Secret: FormatSecret(raw)}
		return nil
	})
	return out, err
}

// Change áp disable (lý do bắt buộc), enable (đóng mạch) hoặc reset_breaker (lý do bắt buộc) — api_client_admin.
func (s *Service) Change(ctx context.Context, clientID, webhookID uuid.UUID, op, reason, idemKey string) (Registration, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin)
	if err != nil {
		return Registration{}, err
	}
	reason = strings.TrimSpace(reason)
	var action string
	switch op {
	case "disable":
		action = ActDisabled
	case "enable":
		action = ActEnabled
	case "reset_breaker":
		action = ActBreakerReset
	default:
		return Registration{}, problem.NotFound
	}
	var rp *string
	if action != ActEnabled {
		if !reasonOK(reason) {
			return Registration{}, problem.Validation.WithField("reason", "bắt buộc, 10–500 ký tự")
		}
		rp = &reason
	}
	now := s.now().UTC()
	var out Registration
	err = tenant.WithTenantTx(ctx, s.DB, p.TenantID, func(tx pgx.Tx) error {
		r, err := regByID(ctx, tx, clientID, webhookID, true)
		if err != nil {
			return err
		}
		if r == nil {
			return ErrNotFound
		}
		switch {
		case action == ActDisabled && r.Status != StatusActive, action == ActEnabled && r.Status != StatusDisabled:
			return app.ErrState.WithDetail("Webhook đang " + r.Status)
		}
		if action == ActEnabled {
			n, err := countActive(ctx, tx, clientID)
			if err != nil {
				return err
			}
			if n >= MaxActivePerClient {
				return ErrTooMany
			}
		}
		idem := "webhook:" + op + ":" + webhookID.String() + ":" + idemKey
		if idemKey == "" {
			idem = "webhook:" + op + ":" + webhookID.String() + ":" + ids.New().String()
		}
		e := event{ID: ids.New(), TenantID: p.TenantID, WebhookID: webhookID, ClientID: clientID, Action: action, Reason: rp, At: now,
			Actor: p.ActorRef, IdemKey: idem}
		if err := insertEvent(ctx, tx, &e); err != nil {
			return err
		}
		r.LastEventID, r.LastSeq = e.ID, e.Seq
		switch action {
		case ActDisabled:
			r.Status, r.StatusReason = StatusDisabled, rp
		case ActEnabled:
			r.Status, r.StatusReason = StatusActive, nil
			r.Breaker.Reset()
		case ActBreakerReset:
			r.Breaker.Reset()
		}
		if err := putReg(ctx, tx, *r); err != nil {
			return err
		}
		if err := putBreaker(ctx, tx, p.TenantID, webhookID, r.Breaker); err != nil {
			return err
		}
		out = *r
		return nil
	})
	return out, err
}

// View là một webhook kèm cursor đã báo và vài lần giao gần nhất.
type View struct {
	Registration
	Cursors    []Cursor
	Deliveries []Delivery
}

// List liệt kê webhook của khách hàng (api_client_admin hoặc api_client_approver).
func (s *Service) List(ctx context.Context, clientID uuid.UUID) ([]View, error) {
	p, err := s.operator(ctx, port.RoleAPIClientAdmin, port.RoleAPIClientApprover)
	if err != nil {
		return nil, err
	}
	return s.list(ctx, p.TenantID, clientID, 10)
}

// ListForClient liệt kê webhook của CHÍNH khách hàng đã xác thực (Public API; tenant đơn vị vận hành).
func (s *Service) ListForClient(ctx context.Context, operator, clientID uuid.UUID) ([]View, error) {
	return s.list(ctx, operator, clientID, 0)
}

func (s *Service) list(ctx context.Context, tid, clientID uuid.UUID, deliveries int) ([]View, error) {
	var out []View
	err := tenant.WithTenantTx(ctx, s.DB, tid, func(tx pgx.Tx) error {
		regs, err := regsOf(ctx, tx, clientID)
		if err != nil {
			return err
		}
		out = make([]View, 0, len(regs))
		for _, r := range regs {
			v := View{Registration: r}
			if v.Cursors, err = cursorsOf(ctx, tx, r.ID); err != nil {
				return err
			}
			if deliveries > 0 {
				if v.Deliveries, err = deliveriesOf(ctx, tx, r.ID, deliveries); err != nil {
					return err
				}
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}
