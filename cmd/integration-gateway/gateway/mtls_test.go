package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/internal/testpki"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
)

// fakeKeys là port.Authenticator giả: khóa → khách hàng (thay HMAC thật của pf/apiclient, đã có kiểm thử riêng
// và kiểm thử tích hợp của cổng).
type fakeKeys map[string]uuid.UUID

func (f fakeKeys) Authenticate(_ context.Context, presented string, at time.Time) (port.Caller, error) {
	id, ok := f[presented]
	if !ok {
		return port.Caller{}, port.ErrUnauthenticated
	}
	return port.Caller{Client: port.Client{ID: id}, KeyPrefix: "agk_test", ReceivedAt: at}, nil
}

type failingIdentity struct{}

func (failingIdentity) ClientForCert(context.Context, string) (uuid.UUID, bool, error) {
	return uuid.Nil, false, errors.New("kho định danh máy lỗi")
}
func (failingIdentity) RequiresMTLS(context.Context, uuid.UUID) (bool, error) {
	return false, errors.New("kho định danh máy lỗi")
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// authHandler: handler tối thiểu gọi Authenticator như bề mặt Public (401 khi lỗi).
func authHandler(a port.Authenticator) http.Handler {
	return WithPeerCert(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := a.Authenticate(r.Context(), r.Header.Get(port.KeyHeader), time.Now())
		if err != nil {
			if errors.Is(err, port.ErrUnauthenticated) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, c.Client.ID.String())
	}))
}

// W5.2 (ADR-0033 §1, §41.2): mTLS cho đối tác ghi lớn — chứng chỉ gắn với khách hàng API, kiểm CẢ khóa lẫn
// chứng chỉ; HMAC vẫn bắt buộc ở cả hai listener.
func TestMTLSAcceptReject_W52(t *testing.T) {
	partnerCA, serverCA, foreignCA := testpki.NewCA(t, "partner-ca"), testpki.NewCA(t, "server-ca"), testpki.NewCA(t, "foreign-ca")
	mkg, third := ids.New(), ids.New()
	mkgCert, mkgLeaf, _, _ := partnerCA.Issue(t, "mkg-batch", false)
	thirdCert, thirdLeaf, _, _ := partnerCA.Issue(t, "third", false)
	unboundCert, _, _, _ := partnerCA.Issue(t, "unbound", false)
	foreignCert, _, _, _ := foreignCA.Issue(t, "foreign", false)

	id, err := NewStaticIdentity(BindingsFile{Version: 1, Bindings: []Binding{
		{ClientID: mkg, CertSHA256: []string{Fingerprint(mkgLeaf)}, RequireMTLS: true, Note: "MKG"},
		{ClientID: third, CertSHA256: []string{Fingerprint(thirdLeaf)}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	auth := MTLSAuthenticator{Inner: fakeKeys{"key-mkg": mkg, "key-third": third}, Identity: id, Log: quiet()}
	h := authHandler(auth)

	plain := httptest.NewServer(h)
	t.Cleanup(plain.Close)
	certFile, keyFile, caFile := testpki.ServerFiles(t, t.TempDir(), serverCA, partnerCA)
	tlsCfg, err := ServerTLS(certFile, keyFile, caFile)
	if err != nil {
		t.Fatal(err)
	}
	mtls := httptest.NewUnstartedServer(h)
	mtls.TLS = tlsCfg
	mtls.StartTLS()
	t.Cleanup(mtls.Close)

	call := func(c *http.Client, url, key string) (int, error) {
		req, _ := http.NewRequest(http.MethodGet, url+"/v1/x", nil)
		if key != "" {
			req.Header.Set(port.KeyHeader, key)
		}
		resp, err := c.Do(req)
		if err != nil {
			return 0, err
		}
		_ = resp.Body.Close()
		return resp.StatusCode, nil
	}
	cases := []struct {
		name   string
		client *http.Client
		url    string
		key    string
		want   int // 0 = bắt tay TLS phải hỏng
	}{
		{"HMAC thường qua listener HTTP vẫn chạy", http.DefaultClient, plain.URL, "key-third", 200},
		{"khóa sai qua HTTP → 401", http.DefaultClient, plain.URL, "key-bad", 401},
		{"đối tác require_mtls qua HTTP (không chứng chỉ) → 401", http.DefaultClient, plain.URL, "key-mkg", 401},
		{"đối tác require_mtls + chứng chỉ của nó → 200", testpki.Client(serverCA, &mkgCert), mtls.URL, "key-mkg", 200},
		{"chứng chỉ đúng nhưng khóa sai → 401 (HMAC vẫn bắt buộc)", testpki.Client(serverCA, &mkgCert), mtls.URL, "key-bad", 401},
		{"chứng chỉ đúng, không khóa → 401", testpki.Client(serverCA, &mkgCert), mtls.URL, "", 401},
		{"khóa MKG + chứng chỉ của khách hàng khác → 401", testpki.Client(serverCA, &thirdCert), mtls.URL, "key-mkg", 401},
		{"khóa MKG + chứng chỉ chưa gắn (cùng CA) → 401", testpki.Client(serverCA, &unboundCert), mtls.URL, "key-mkg", 401},
		{"khách hàng không bắt buộc mTLS dùng chứng chỉ của mình → 200", testpki.Client(serverCA, &thirdCert), mtls.URL, "key-third", 200},
		{"chứng chỉ của CA lạ → bắt tay hỏng", testpki.Client(serverCA, &foreignCert), mtls.URL, "key-mkg", 0},
		{"listener mTLS không chứng chỉ → bắt tay hỏng", testpki.Client(serverCA, nil), mtls.URL, "key-third", 0},
	}
	for _, c := range cases {
		got, err := call(c.client, c.url, c.key)
		if c.want == 0 {
			if err == nil {
				t.Errorf("%s: muốn bắt tay TLS hỏng, nhận %d", c.name, got)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s: muốn %d, nhận %d (%v)", c.name, c.want, got, err)
		}
	}
}

// Chứng chỉ chỉ tin khi CHÍNH tiến trình bắt tay TLS: header giả không gắn được chứng chỉ.
func TestPeerCertIgnoresHeaders_W52(t *testing.T) {
	h := WithPeerCert(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := PeerCertFrom(r.Context()); ok {
			w.WriteHeader(http.StatusTeapot)
		}
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/x", nil)
	req.Header.Set("X-Forwarded-Client-Cert", "Hash=deadbeef")
	req.Header.Set("Ssl-Client-Cert", "-----BEGIN CERTIFICATE-----")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code == http.StatusTeapot {
		t.Fatal("chứng chỉ không được lấy từ header")
	}
}

// Lỗi kho định danh máy không bao giờ thành 200 (đóng khi lỗi), và không giả làm 401.
func TestMTLSIdentityErrorFailsClosed_W52(t *testing.T) {
	a := MTLSAuthenticator{Inner: fakeKeys{"k": ids.New()}, Identity: failingIdentity{}, Log: quiet()}
	if _, err := a.Authenticate(context.Background(), "k", time.Now()); err == nil || errors.Is(err, port.ErrUnauthenticated) {
		t.Fatalf("muốn lỗi hạ tầng, nhận %v", err)
	}
	ctx := context.WithValue(context.Background(), peerCertKey{}, strings.Repeat("a", 64))
	if _, err := a.Authenticate(ctx, "k", time.Now()); err == nil {
		t.Fatal("lỗi kho định danh máy phải từ chối")
	}
}

func TestStaticIdentityValidation_W52(t *testing.T) {
	a, b := ids.New(), ids.New()
	fp := strings.Repeat("ab", 32)
	bad := []BindingsFile{
		{Version: 2},
		{Version: 1, Bindings: []Binding{{CertSHA256: []string{fp}}}},
		{Version: 1, Bindings: []Binding{{ClientID: a}, {ClientID: a}}},
		{Version: 1, Bindings: []Binding{{ClientID: a, RequireMTLS: true}}},
		{Version: 1, Bindings: []Binding{{ClientID: a, CertSHA256: []string{"zz"}}}},
		{Version: 1, Bindings: []Binding{{ClientID: a, CertSHA256: []string{fp}}, {ClientID: b, CertSHA256: []string{fp}}}},
	}
	for i, f := range bad {
		if _, err := NewStaticIdentity(f); err == nil {
			t.Errorf("tệp gắn #%d phải bị từ chối", i)
		}
	}
	s, err := NewStaticIdentity(BindingsFile{Version: 1, Bindings: []Binding{{ClientID: a, CertSHA256: []string{"AB:" + strings.Repeat("AB:", 30) + "AB"}, RequireMTLS: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if id, ok, _ := s.ClientForCert(context.Background(), fp); !ok || id != a {
		t.Fatalf("vân tay có dấu ':' / chữ hoa phải chuẩn hóa: %v %v", id, ok)
	}
	if req, _ := s.RequiresMTLS(context.Background(), b); req {
		t.Fatal("khách hàng không gắn không bắt buộc mTLS")
	}
}

func TestLoadStaticIdentityRejectsUnknownFields_W52(t *testing.T) {
	dir := t.TempDir()
	raw, _ := json.Marshal(map[string]any{"version": 1, "bindings": []any{map[string]any{"client_id": ids.New().String(), "cert_sha256": []string{}, "secret": "x"}}})
	if _, err := LoadStaticIdentity(testpki.Write(t, dir, "b.json", raw)); err == nil {
		t.Fatal("khóa lạ phải bị từ chối")
	}
	if _, err := LoadStaticIdentity(dir + "/khong-co.json"); err == nil {
		t.Fatal("thiếu tệp phải lỗi")
	}
}

func TestServerTLSErrors_W52(t *testing.T) {
	if _, err := ServerTLS("", "", ""); err == nil {
		t.Fatal("thiếu tệp phải lỗi")
	}
	dir := t.TempDir()
	ca := testpki.NewCA(t, "ca")
	cert, key, _ := testpki.ServerFiles(t, dir, ca, ca)
	if _, err := ServerTLS(cert, key, testpki.Write(t, dir, "rong.pem", []byte("không phải PEM"))); err == nil {
		t.Fatal("CA không phải PEM phải lỗi")
	}
}
