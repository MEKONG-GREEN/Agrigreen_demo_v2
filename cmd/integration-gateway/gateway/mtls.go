package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
)

// MachineIdentity là định danh máy của đối tác mà cổng cần (ADR-0033 §1) — interface do BÊN TIÊU THỤ (cổng)
// khai. v4.1 đặt chủ định danh máy ở k/access (pf/accounts, chuyển ở W7.2); tới đó khóa API vẫn ở pf/apiclient
// (port.Authenticator) và gắn chứng chỉ do vận hành cấp qua tệp (StaticIdentity). W7.2 thay bộ điều hợp bằng
// cổng của pf/accounts mà không sửa cổng.
type MachineIdentity interface {
	// ClientForCert trả khách hàng API gắn với chứng chỉ client có vân tay fingerprint (SHA-256 của DER lá,
	// hex chữ thường). ok = false: chứng chỉ không gắn với khách hàng nào.
	ClientForCert(ctx context.Context, fingerprint string) (clientID uuid.UUID, ok bool, err error)
	// RequiresMTLS: khách hàng là đối tác ghi khối lượng lớn — mọi lời gọi phải mang chứng chỉ gắn với nó.
	RequiresMTLS(ctx context.Context, clientID uuid.UUID) (bool, error)
}

// Binding là một dòng của tệp gắn (AG_GATEWAY_MTLS_BINDINGS).
type Binding struct {
	ClientID uuid.UUID `json:"client_id"`
	// CertSHA256: vân tay SHA-256 DER của các chứng chỉ client đang hiệu lực (nhiều = kỳ xoay vòng); chấp nhận
	// hex có hoặc không dấu ":".
	CertSHA256  []string `json:"cert_sha256"`
	RequireMTLS bool     `json:"require_mtls"`
	Note        string   `json:"note,omitempty"` // tên tổ chức (không phải người) — chỉ cho vận hành đọc
}

// BindingsFile là định dạng tệp gắn.
type BindingsFile struct {
	Version  int       `json:"version"`
	Bindings []Binding `json:"bindings"`
}

// StaticIdentity là MachineIdentity đọc từ tệp gắn (kho bí mật → Secret → tệp). Bất biến sau khi nạp; xoay
// chứng chỉ = sửa tệp ở kho bí mật, Reloader khởi động lại Pod.
type StaticIdentity struct {
	byCert  map[string]uuid.UUID
	require map[uuid.UUID]bool
}

var _ MachineIdentity = (*StaticIdentity)(nil)

// NormalizeFingerprint đưa vân tay về hex chữ thường không dấu ":"; lỗi nếu không phải 32 byte.
func NormalizeFingerprint(s string) (string, error) {
	h := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(s), ":", ""))
	b, err := hex.DecodeString(h)
	if err != nil || len(b) != sha256.Size {
		return "", fmt.Errorf("vân tay %q không phải SHA-256 hex", s)
	}
	return h, nil
}

// Fingerprint trả vân tay SHA-256 (hex chữ thường) của chứng chỉ.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// NewStaticIdentity kiểm và dựng StaticIdentity: client_id khác Nil và không trùng; mỗi vân tay hợp lệ và gắn
// đúng MỘT khách hàng; require_mtls phải có ít nhất một chứng chỉ (nếu không khách hàng không gọi được).
func NewStaticIdentity(f BindingsFile) (*StaticIdentity, error) {
	if f.Version != 1 {
		return nil, fmt.Errorf("integration-gateway: tệp gắn chứng chỉ version %d không hỗ trợ (cần 1)", f.Version)
	}
	s := &StaticIdentity{byCert: map[string]uuid.UUID{}, require: map[uuid.UUID]bool{}}
	var errs []error
	seen := map[uuid.UUID]bool{}
	for i, b := range f.Bindings {
		if b.ClientID == uuid.Nil {
			errs = append(errs, fmt.Errorf("bindings[%d]: thiếu client_id", i))
			continue
		}
		if seen[b.ClientID] {
			errs = append(errs, fmt.Errorf("bindings[%d]: client_id %s lặp", i, b.ClientID))
			continue
		}
		seen[b.ClientID] = true
		if b.RequireMTLS && len(b.CertSHA256) == 0 {
			errs = append(errs, fmt.Errorf("bindings[%d]: require_mtls nhưng không có cert_sha256", i))
		}
		for _, raw := range b.CertSHA256 {
			fp, err := NormalizeFingerprint(raw)
			if err != nil {
				errs = append(errs, fmt.Errorf("bindings[%d]: %w", i, err))
				continue
			}
			if other, dup := s.byCert[fp]; dup && other != b.ClientID {
				errs = append(errs, fmt.Errorf("bindings[%d]: chứng chỉ %s đã gắn với khách hàng %s", i, fp, other))
				continue
			}
			s.byCert[fp] = b.ClientID
		}
		s.require[b.ClientID] = b.RequireMTLS
	}
	if err := errors.Join(errs...); err != nil {
		return nil, fmt.Errorf("integration-gateway: tệp gắn chứng chỉ không hợp lệ:\n%w", err)
	}
	return s, nil
}

// LoadStaticIdentity đọc tệp gắn JSON (khóa lạ → lỗi).
func LoadStaticIdentity(path string) (*StaticIdentity, error) {
	raw, err := os.ReadFile(path) //nolint:gosec // đường dẫn do vận hành cấu hình
	if err != nil {
		return nil, fmt.Errorf("integration-gateway: đọc %s: %w", EnvMTLSBindings, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var f BindingsFile
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("integration-gateway: %s: %w", EnvMTLSBindings, err)
	}
	return NewStaticIdentity(f)
}

// ClientForCert hiện thực MachineIdentity.
func (s *StaticIdentity) ClientForCert(_ context.Context, fingerprint string) (uuid.UUID, bool, error) {
	id, ok := s.byCert[fingerprint]
	return id, ok, nil
}

// RequiresMTLS hiện thực MachineIdentity.
func (s *StaticIdentity) RequiresMTLS(_ context.Context, clientID uuid.UUID) (bool, error) {
	return s.require[clientID], nil
}

// ServerTLS dựng cấu hình listener mTLS: chứng chỉ client do CA đối tác ký là BẮT BUỘC
// (RequireAndVerifyClientCert, kiểm hạn và EKU clientAuth); TLS ≥ 1.2.
func ServerTLS(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" || clientCAFile == "" {
		return nil, fmt.Errorf("integration-gateway: mTLS cần %s, %s và %s", EnvTLSCert, EnvTLSKey, EnvClientCA)
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("integration-gateway: nạp chứng thư máy chủ: %w", err)
	}
	caPEM, err := os.ReadFile(clientCAFile) //nolint:gosec // đường dẫn do vận hành cấu hình
	if err != nil {
		return nil, fmt.Errorf("integration-gateway: đọc %s: %w", EnvClientCA, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("integration-gateway: %s không chứa chứng thư PEM hợp lệ", EnvClientCA)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
	}, nil
}

type peerCertKey struct{}

// WithPeerCert gắn vân tay chứng chỉ client ĐÃ KIỂM (r.TLS.VerifiedChains) vào context. Không bao giờ đọc
// header (X-Forwarded-Client-Cert…): chứng chỉ chỉ tin khi chính tiến trình này bắt tay TLS.
func WithPeerCert(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.TLS != nil && len(r.TLS.VerifiedChains) > 0 && len(r.TLS.VerifiedChains[0]) > 0 {
			r = r.WithContext(context.WithValue(r.Context(), peerCertKey{}, Fingerprint(r.TLS.VerifiedChains[0][0])))
		}
		next.ServeHTTP(w, r)
	})
}

// PeerCertFrom trả vân tay chứng chỉ client đã kiểm của request (nếu có).
func PeerCertFrom(ctx context.Context) (string, bool) {
	fp, ok := ctx.Value(peerCertKey{}).(string)
	return fp, ok
}

// MTLSAuthenticator bọc port.Authenticator (khóa API HMAC của pf/apiclient): khóa luôn bắt buộc; thêm:
//   - có chứng chỉ client → phải gắn với ĐÚNG khách hàng của khóa (chứng chỉ lạ hoặc của khách hàng khác → 401);
//   - không chứng chỉ → khách hàng require_mtls bị từ chối (401).
//
// Từ chối trả port.ErrUnauthenticated nên đi đúng nhánh 401 của bề mặt Public: KHÔNG ghi sổ đo lường.
type MTLSAuthenticator struct {
	Inner    port.Authenticator
	Identity MachineIdentity
	Log      *slog.Logger
}

// Authenticate hiện thực port.Authenticator.
func (a MTLSAuthenticator) Authenticate(ctx context.Context, presented string, receivedAt time.Time) (port.Caller, error) {
	c, err := a.Inner.Authenticate(ctx, presented, receivedAt)
	if err != nil {
		return c, err
	}
	if fp, ok := PeerCertFrom(ctx); ok {
		id, bound, err := a.Identity.ClientForCert(ctx, fp)
		if err != nil {
			return port.Caller{}, err
		}
		if !bound || id != c.Client.ID {
			reason := "cert_unbound"
			if bound {
				reason = "cert_mismatch"
			}
			return port.Caller{}, a.reject(ctx, c, reason)
		}
		return c, nil
	}
	req, err := a.Identity.RequiresMTLS(ctx, c.Client.ID)
	if err != nil {
		return port.Caller{}, err
	}
	if req {
		return port.Caller{}, a.reject(ctx, c, "mtls_required")
	}
	return c, nil
}

func (a MTLSAuthenticator) reject(ctx context.Context, c port.Caller, reason string) error {
	log := a.Log
	if log == nil {
		log = slog.Default()
	}
	log.WarnContext(ctx, "cổng tích hợp: từ chối mTLS", "event", "gateway.mtls_rejected", "reason", reason,
		"key_prefix", c.KeyPrefix, "client_id", c.Client.ID.String(), "trace_id", telemetry.TraceID(ctx), "outcome", "rejected")
	return port.ErrUnauthenticated
}
