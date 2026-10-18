// Package idempotency thực hiện R-51: mọi POST tạo tài nguyên bắt buộc header
// Idempotency-Key; gọi lặp trả lại ĐÚNG phản hồi lần đầu. Khóa lưu 48 giờ theo
// (tenant, key) trong shared.idempotency_key.
//
// Hai lớp an toàn: (1) bảng này chặn chạy lại handler; (2) handler dùng
// KeyFromContext làm idem_key khi ghi sổ, nên kể cả khi tiến trình chết giữa
// chừng và request được chạy lại, sổ vẫn không ghi trùng.
package idempotency

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/tenant"
)

// Header là tên header.
const Header = "Idempotency-Key"

// ReplayedHeader đánh dấu phản hồi được phát lại.
const ReplayedHeader = "Idempotent-Replayed"

// Giới hạn lưu phản hồi.
const maxStoredBody = 1 << 20

var keyRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,200}$`)

type ctxKey struct{}

// KeyFromContext trả Idempotency-Key của request hiện tại.
func KeyFromContext(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	return s
}

// storedBodyKey là khóa gin.Context của thân thay thế sẽ được LƯU (SetStoredBody).
const storedBodyKey = "idempotency.stored_body"

// SetStoredBody đặt thân phản hồi được LƯU để phát lại, thay cho thân thật đã gửi
// cho client. Dùng khi phản hồi mang bí mật chỉ được trao MỘT lần (vd liên kết kích
// hoạt tài khoản chứa action token): bản lưu 48 giờ ở shared.idempotency_key không
// được chứa bí mật đó. Gọi lặp cùng khóa nhận bản đã lược bí mật.
func SetStoredBody(c *gin.Context, body []byte) { c.Set(storedBodyKey, body) }

type capture struct {
	gin.ResponseWriter
	buf bytes.Buffer
}

func (w *capture) Write(b []byte) (int, error) {
	if w.buf.Len() < maxStoredBody {
		w.buf.Write(b)
	}
	return w.ResponseWriter.Write(b)
}

type stored struct {
	actor       uuid.UUID
	route       string
	hash        []byte
	status      *int
	body        []byte
	contentType *string
	location    *string
	stale       bool
}

type authorizationScopeKey struct{}

// WithAuthorizationScope gắn dấu phạm vi do middleware cấp quyền đã giải ở server.
// Hạ tầng không diễn giải nội dung nghiệp vụ; không lấy dấu này từ header client.
// Gọi trước Middleware, cập nhật mỗi request để cache không sống vượt quyền hiện tại.
func WithAuthorizationScope(ctx context.Context, scope []byte) context.Context {
	return context.WithValue(ctx, authorizationScopeKey{}, bytes.Clone(scope))
}

// requestHash v2 ràng buộc tài nguyên thật, truy vấn và quyền hiện tại; không chỉ mẫu route.
func requestHash(ctx context.Context, p authz.Principal, r *http.Request, body []byte) []byte {
	roles := slices.Clone(p.Roles)
	slices.Sort(roles)
	scope, _ := ctx.Value(authorizationScopeKey{}).([]byte)
	binding, _ := json.Marshal(struct {
		Org   uuid.UUID
		Roles []string
		MFA   bool
		Scope []byte
	}{p.OrgRef, roles, p.MFA, scope})
	h := sha256.New()
	h.Write([]byte("v2\n" + r.Method + "\n" + r.URL.EscapedPath() + "\n" + r.URL.Query().Encode() + "\n"))
	h.Write(binding)
	h.Write([]byte("\n"))
	h.Write(body)
	return h.Sum(nil)
}

// Middleware áp cho các route POST. db là pool của vai có quyền trên shared.idempotency_key.
func Middleware(db tenant.Beginner) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}
		key := c.GetHeader(Header)
		if key == "" {
			httpx.Fail(c, problem.IdempotencyRequired)
			return
		}
		if !keyRe.MatchString(key) {
			httpx.Fail(c, problem.BadRequest.WithDetail("Idempotency-Key phải 8–200 ký tự [A-Za-z0-9._:-]"))
			return
		}
		ctx := c.Request.Context()
		p, ok := authz.FromContext(ctx)
		if !ok {
			httpx.Fail(c, problem.Unauthenticated)
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		route := c.FullPath()
		hash := requestHash(ctx, p, c.Request, body)

		var prev *stored
		err = tenant.WithTenantTx(ctx, db, p.TenantID, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `DELETE FROM shared.idempotency_key
			    WHERE tenant_id = $1 AND idem_key = $2 AND expires_at < now()`, p.TenantID, key); err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `INSERT INTO shared.idempotency_key (tenant_id, idem_key, actor_ref, method, route, request_hash)
			    VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, p.TenantID, key, p.ActorRef, c.Request.Method, route, hash)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 1 {
				return nil
			}
			s := &stored{}
			if err := tx.QueryRow(ctx, `SELECT actor_ref, route, request_hash, status, response_body, content_type, location,
			      status IS NULL AND created_at < now() - interval '2 minutes'
			    FROM shared.idempotency_key WHERE tenant_id = $1 AND idem_key = $2 FOR UPDATE`, p.TenantID, key).
				Scan(&s.actor, &s.route, &s.hash, &s.status, &s.body, &s.contentType, &s.location, &s.stale); err != nil {
				return err
			}
			if s.stale && bytes.Equal(s.hash, hash) && s.actor == p.ActorRef {
				// Lần chạy trước chết giữa chừng: nhận lại quyền chạy. Sổ vẫn chống trùng bằng idem_key.
				_, err := tx.Exec(ctx, "UPDATE shared.idempotency_key SET created_at = now() WHERE tenant_id = $1 AND idem_key = $2", p.TenantID, key)
				return err
			}
			prev = s
			return nil
		})
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		if prev != nil {
			switch {
			case prev.actor != p.ActorRef || prev.route != route || !bytes.Equal(prev.hash, hash):
				httpx.Fail(c, problem.IdempotencyConflict)
			case prev.status == nil:
				httpx.Fail(c, problem.IdempotencyInFlight)
			default:
				replay(c, prev)
			}
			return
		}

		c.Request = c.Request.WithContext(context.WithValue(ctx, ctxKey{}, key))
		w := &capture{ResponseWriter: c.Writer}
		c.Writer = w
		c.Next()

		status := w.Status()
		finish := context.WithoutCancel(ctx)
		err = tenant.WithTenantTx(finish, db, p.TenantID, func(tx pgx.Tx) error {
			if status >= 500 {
				// Lỗi hệ thống: xóa khóa để client thử lại được.
				_, err := tx.Exec(finish, "DELETE FROM shared.idempotency_key WHERE tenant_id = $1 AND idem_key = $2", p.TenantID, key)
				return err
			}
			var ct, loc *string
			if v := w.Header().Get("Content-Type"); v != "" {
				ct = &v
			}
			if v := w.Header().Get("Location"); v != "" {
				loc = &v
			}
			body := w.buf.Bytes()
			if v, ok := c.Get(storedBodyKey); ok {
				if b, ok := v.([]byte); ok {
					body = b
				}
			}
			_, err := tx.Exec(finish, `UPDATE shared.idempotency_key SET status = $3, response_body = $4, content_type = $5, location = $6
			    WHERE tenant_id = $1 AND idem_key = $2`, p.TenantID, key, status, body, ct, loc)
			return err
		})
		if err != nil {
			_ = c.Error(errors.Join(errors.New("idempotency: không lưu được phản hồi"), err))
		}
	}
}

func replay(c *gin.Context, s *stored) {
	if s.contentType != nil {
		c.Header("Content-Type", *s.contentType)
	}
	if s.location != nil {
		c.Header("Location", *s.location)
	}
	c.Header(ReplayedHeader, "true")
	c.Status(*s.status)
	_, _ = c.Writer.Write(s.body)
	c.Abort()
}
