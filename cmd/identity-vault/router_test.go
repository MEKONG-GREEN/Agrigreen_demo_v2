package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/envelope"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/httpx"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/pgtest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/problem"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/vault/identity/client"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

type fixture struct {
	t   *testing.T
	srv *httptest.Server
	iss *authztest.FakeIssuer
	log *syncBuffer
}

// syncBuffer gom log của server để kiểm không có dữ liệu cá nhân (R-14).
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
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

func newFixture(t *testing.T) *fixture {
	env := pgtest.Get(t)
	iss := authztest.New(t)
	pool := env.Pool(t, migrations.Vault, identity.DBRole)
	keys, err := identity.NewLocalKMS("test-1", bytes.Repeat([]byte{9}, 32))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := identity.New(identity.Config{DB: pool, Clock: clock.System{}, KMS: keys, BlindKey: bytes.Repeat([]byte{5}, 32)})
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	srv := httptest.NewServer(NewRouter(Deps{Log: telemetry.NewLogger("test", logs), Verifier: iss.Verifier(), Module: mod, DB: pool}))
	t.Cleanup(srv.Close)
	return &fixture{t: t, srv: srv, iss: iss, log: logs}
}

type user struct {
	f     *fixture
	token string
	tid   uuid.UUID
}

func (f *fixture) user(tid uuid.UUID, mfa bool, roles ...string) user {
	return user{f: f, tid: tid, token: f.iss.Sign(f.t, authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles, MFA: mfa})}
}

func (u user) ctx() context.Context { return authz.WithRawToken(context.Background(), u.token) }

func (u user) do(method, path string, hdr map[string]string, body any) (*http.Response, map[string]any) {
	u.f.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, u.f.srv.URL+path, rd)
	if u.token != "" {
		req.Header.Set("Authorization", "Bearer "+u.token)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		u.f.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

var everything = []string{client.RoleRegistrar, client.RoleEvidenceViewer, client.RoleEraser}

func createBody(phone string) map[string]any {
	return map[string]any{"kind": "household", "full_name": "Lê Thị Mai", "phone": phone, "address": "Ấp 1", "area_code": "86-29500"}
}

func TestHealthAndAuth(t *testing.T) {
	f := newFixture(t)
	if resp, _ := (user{f: f}).do("GET", "/healthz", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("healthz: %d", resp.StatusCode)
	}
	if resp, _ := (user{f: f}).do("GET", "/readyz", nil, nil); resp.StatusCode != 200 {
		t.Fatalf("readyz: %d", resp.StatusCode)
	}
	resp, body := (user{f: f}).do("GET", "/v1/subjects/"+ids.New().String()+"/display", map[string]string{"X-Purpose": "extension.contact"}, nil)
	if resp.StatusCode != 401 || body["code"] != "unauthenticated" {
		t.Fatalf("không token: %d %v", resp.StatusCode, body)
	}
	// Token của issuer khác (R-54).
	other := authztest.New(t)
	bad := user{f: f, token: other.Sign(t, authztest.Token{Subject: ids.New(), TenantID: ids.New(), Roles: everything})}
	if resp, _ := bad.do("GET", "/v1/pubkeys?area=x", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("token chữ ký lạ: %d", resp.StatusCode)
	}
}

// R-42, R-51 qua HTTP.
func TestPurposeAndIdempotencyHTTP(t *testing.T) {
	f := newFixture(t)
	u := f.user(ids.New(), false, client.RoleRegistrar)
	purpose := map[string]string{"X-Purpose": "registry.admin"}

	resp, pb := u.do("POST", "/v1/subjects", purpose, createBody("0902000001"))
	if resp.StatusCode != 400 || pb["code"] != "idempotency_key_required" {
		t.Fatalf("thiếu Idempotency-Key: %d %v", resp.StatusCode, pb)
	}
	resp, pb = u.do("POST", "/v1/subjects", map[string]string{"Idempotency-Key": "k-" + ids.New().String()}, createBody("0902000001"))
	if resp.StatusCode != 400 || pb["code"] != client.CodePurposeRequired {
		t.Fatalf("tạo thiếu mục đích: %d %v", resp.StatusCode, pb)
	}
	key := "k-" + ids.New().String()
	h := map[string]string{"Idempotency-Key": key, "X-Purpose": "registry.admin"}
	r1, b1 := u.do("POST", "/v1/subjects", h, createBody("0902000001"))
	if r1.StatusCode != 201 || b1["ref"] == nil || b1["public_key"] == nil {
		t.Fatalf("tạo: %d %v", r1.StatusCode, b1)
	}
	r2, b2 := u.do("POST", "/v1/subjects", h, createBody("0902000001"))
	if r2.StatusCode != 201 || r2.Header.Get("Idempotent-Replayed") != "true" || b2["ref"] != b1["ref"] {
		t.Fatalf("gọi lặp: %d %v", r2.StatusCode, b2)
	}
	ref := b1["ref"].(string)

	// Đọc thiếu mục đích → 400 vault.purpose_required.
	for _, c := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/v1/subjects/" + ref + "/display", nil},
		{"POST", "/v1/subjects/" + ref + "/resolve", map[string]any{}},
		{"POST", "/v1/subjects/lookup", map[string]any{"value": "0902000001"}},
		{"POST", "/v1/subjects/" + ref + "/unwrap", map[string]any{"wrapped_dek": make([]byte, 80)}},
	} {
		resp, pb := u.do(c.method, c.path, nil, c.body)
		if resp.StatusCode != 400 || pb["code"] != client.CodePurposeRequired {
			t.Errorf("%s %s thiếu mục đích: %d %v", c.method, c.path, resp.StatusCode, pb)
		}
	}
	// Mục đích trong thân cũng được.
	resp, pb = u.do("POST", "/v1/subjects/"+ref+"/resolve", nil, map[string]any{"purpose": "registry.admin"})
	if resp.StatusCode != 200 || pb["full_name"] != "Lê Thị Mai" || resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("resolve có mục đích trong thân: %d %v", resp.StatusCode, pb)
	}
	// Trùng SĐT → 409 kèm ref cũ.
	resp, pb = u.do("POST", "/v1/subjects", map[string]string{"Idempotency-Key": "k-" + ids.New().String(), "X-Purpose": "registry.dedup"}, createBody("+84902000001"))
	if resp.StatusCode != 409 || pb["code"] != client.CodeDuplicate || pb["ref"] != ref {
		t.Fatalf("trùng: %d %v", resp.StatusCode, pb)
	}
}

// Client HTTP: chuyển tiếp token người dùng, đi đủ vòng đời chủ thể.
func TestHTTPClientEndToEnd(t *testing.T) {
	f := newFixture(t)
	tid := ids.New()
	u := f.user(tid, true, everything...)
	vc := client.NewHTTP(f.srv.URL, f.srv.Client())
	ctx := u.ctx()

	if _, err := vc.Create(context.Background(), client.PurposeRegistryAdmin, client.Identity{}); !errors.Is(err, client.ErrNoToken) {
		t.Fatalf("không có token trong ctx: %v", err)
	}
	c, err := vc.Create(ctx, client.PurposeRegistryAdmin, client.Identity{
		Kind: client.SubjectHousehold, FullName: "Phạm Văn Hùng", Phone: "0903 000 001", NationalID: "086123456789", Address: "Ấp 2", AreaCode: "86-29500",
	})
	if err != nil {
		t.Fatal(err)
	}
	dup, err := vc.Create(ctx, client.PurposeDedup, client.Identity{Kind: client.SubjectPerson, FullName: "Ai đó", NationalID: "086123456789", AreaCode: "86-29500"})
	if !errors.Is(err, client.ErrDuplicate) || dup.Ref != c.Ref || !bytes.Equal(dup.PublicKey, c.PublicKey) {
		t.Fatalf("trùng qua client: %v %v", dup.Ref, err)
	}
	if _, err := vc.Display(ctx, "", c.Ref); !errors.Is(err, client.ErrNoPurpose) {
		t.Fatalf("client chặn thiếu mục đích: %v", err)
	}
	d, err := vc.Display(ctx, client.PurposeContact, c.Ref)
	if err != nil || d.Label != "Phạm V. H." || d.MaskedPhone != "09xx xxx 001" {
		t.Fatalf("display: %+v %v", d, err)
	}
	id, err := vc.Resolve(ctx, client.PurposeRegistryAdmin, c.Ref)
	if err != nil || id.FullName != "Phạm Văn Hùng" || id.Phone != "84903000001" || id.NationalID != "086123456789" || id.Kind != client.SubjectHousehold {
		t.Fatalf("resolve: %v", err)
	}
	if ref, err := vc.Lookup(ctx, client.PurposeDedup, "+84903000001"); err != nil || ref != c.Ref {
		t.Fatalf("lookup: %v", err)
	}
	if _, err := vc.Lookup(ctx, client.PurposeDedup, "0903999999"); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("lookup không có: %v", err)
	}
	pub, _ := envelope.Key32(c.PublicKey)
	sealed, _ := envelope.Seal([]byte("ảnh ruộng"), pub)
	dek, err := vc.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, sealed.WrappedDEK)
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := envelope.DecryptWithDEK(dek, sealed.Nonce, sealed.Ciphertext); err != nil || string(pt) != "ảnh ruộng" {
		t.Fatal("DEK qua HTTP phải mở được ảnh")
	}
	keys, err := vc.PubKeys(ctx, []string{"86-29500"})
	if err != nil || len(keys) != 1 || keys[0].Ref != c.Ref {
		t.Fatalf("pubkeys: %v %v", keys, err)
	}

	// Tenant khác, cùng vai: không thấy (R-16: tenant chỉ từ token).
	other := f.user(ids.New(), true, everything...)
	if _, err := vc.Resolve(other.ctx(), client.PurposeRegistryAdmin, c.Ref); !errors.Is(err, client.ErrNotFound) {
		t.Fatalf("chéo tenant: %v", err)
	}
	// Xóa: cần MFA.
	noMFA := f.user(tid, false, client.RoleEraser)
	err = vc.Erase(noMFA.ctx(), c.Ref, "Chủ hộ yêu cầu xóa dữ liệu")
	var p *problem.Problem
	if !errors.As(err, &p) || p.Code != "mfa_required" {
		t.Fatalf("xóa không MFA: %v", err)
	}
	if err := vc.Erase(ctx, c.Ref, "Chủ hộ yêu cầu xóa dữ liệu"); err != nil {
		t.Fatal(err)
	}
	if _, err := vc.Unwrap(ctx, client.PurposeMRVEvidence, c.Ref, sealed.WrappedDEK); !errors.Is(err, client.ErrErased) {
		t.Fatalf("unwrap sau xóa: %v", err)
	}
	if _, err := vc.Resolve(ctx, client.PurposeRegistryAdmin, c.Ref); !errors.Is(err, client.ErrErased) {
		t.Fatalf("resolve sau xóa: %v", err)
	}
	if d, err := vc.Display(ctx, client.PurposeContact, c.Ref); !errors.Is(err, client.ErrErased) || d.Label != client.ErasedLabel {
		t.Fatalf("display sau xóa: %+v %v", d, err)
	}
	if keys, err := vc.PubKeys(ctx, []string{"86-29500"}); err != nil || len(keys) != 0 {
		t.Fatalf("pubkeys sau xóa: %v %v", keys, err)
	}
	// R-14: log truy cập của kho không chứa giá trị định danh hay token.
	logs := f.log.String()
	if !strings.Contains(logs, "http.request") {
		t.Fatal("phải có log truy cập")
	}
	for _, pii := range []string{"Phạm", "84903000001", "086123456789", "Ấp 2", u.token} {
		if strings.Contains(logs, pii) {
			t.Fatalf("log chứa dữ liệu cá nhân hoặc token: %q", pii)
		}
	}
}

// Module gọi ở api-platform: httpx.Authenticate gắn token gốc, client.NewHTTP chuyển tiếp.
func TestRawTokenForwarding(t *testing.T) {
	f := newFixture(t)
	vc := client.NewHTTP(f.srv.URL, nil)
	r := httpx.NewEngine(telemetry.NewLogger("test", io.Discard), 1<<16)
	r.GET("/probe", httpx.Authenticate(f.iss.Verifier()), func(c *gin.Context) {
		_, err := vc.PubKeys(c.Request.Context(), []string{"x"})
		if err != nil {
			httpx.Fail(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	up := httptest.NewServer(r)
	defer up.Close()
	for _, tc := range []struct {
		roles []string
		want  int
	}{{[]string{client.RolePubKeySync}, 204}, {nil, 403}} {
		u := f.user(ids.New(), false, tc.roles...)
		req, _ := http.NewRequest("GET", up.URL+"/probe", nil)
		req.Header.Set("Authorization", "Bearer "+u.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Fatalf("vai %v: %d, muốn %d", tc.roles, resp.StatusCode, tc.want)
		}
	}
}

// mTLS: có AG_TLS_CLIENT_CA thì client không có chứng thư bị từ chối.
func TestMTLS(t *testing.T) {
	dir := t.TempDir()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ag-test-ca"}, NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, _ := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	caCert, _ := x509.ParseCertificate(caDER)
	issue := func(name string, usage x509.ExtKeyUsage) (certPEM, keyPEM []byte) {
		k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tpl := &x509.Certificate{
			SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: name},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			ExtKeyUsage: []x509.ExtKeyUsage{usage}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		}
		der, _ := x509.CreateCertificate(rand.Reader, tpl, caCert, &k.PublicKey, caKey)
		kb, _ := x509.MarshalECPrivateKey(k)
		return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	}
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, b, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	sc, sk := issue("identity-vault", x509.ExtKeyUsageServerAuth)
	cc, ck := issue("api-platform", x509.ExtKeyUsageClientAuth)
	t.Setenv("AG_TLS_CERT", write("s.crt", sc))
	t.Setenv("AG_TLS_KEY", write("s.key", sk))
	t.Setenv("AG_TLS_CLIENT_CA", write("ca.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})))
	cfg, err := TLSConfigFromEnv()
	if err != nil || cfg == nil || cfg.ClientAuth != tls.RequireAndVerifyClientCert {
		t.Fatalf("cấu hình mTLS: %v", err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	srv.TLS = cfg
	srv.StartTLS()
	defer srv.Close()
	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	pair, _ := tls.X509KeyPair(cc, ck)
	withCert := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}}}
	resp, err := withCert.Get(srv.URL)
	if err != nil {
		t.Fatalf("client có chứng thư: %v", err)
	}
	_ = resp.Body.Close()
	noCert := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}}}
	if resp, err := noCert.Get(srv.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("client không chứng thư phải bị từ chối")
	}
	t.Setenv("AG_TLS_CERT", "")
	t.Setenv("AG_TLS_KEY", "")
	if _, err := TLSConfigFromEnv(); err == nil {
		t.Fatal("chỉ có CA mà không có chứng thư server phải lỗi")
	}
}
