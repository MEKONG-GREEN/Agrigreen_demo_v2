package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/gateway"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/integration-gateway/internal/testpki"
	apiclientport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/platform/apiclient/port"
)

// W5.2 (ADR-0033 §1, §41.2): cổng THẬT trên DB thật — khách hàng API phát qua Admin API, gắn chứng chỉ với
// require_mtls (đối tác ghi lớn, như MKG):
//   - listener mTLS + chứng chỉ gắn + khóa HMAC thật → 200 và đúng MỘT dòng sổ đo lường (metering giữ nguyên);
//   - cùng khóa qua listener HTTP (không chứng chỉ) → 401, không dòng sổ;
//   - chứng chỉ gắn nhưng khóa sai → 401, không dòng sổ (HMAC vẫn bắt buộc).
func TestGatewayMTLSRealWiring_W52Integration(t *testing.T) {
	e := newIntEnv(t)
	clientID, _, key := e.onboard(100)
	partnerCA, serverCA := testpki.NewCA(t, "partner-ca"), testpki.NewCA(t, "server-ca")
	cert, leaf, _, _ := partnerCA.Issue(t, "mkg-batch", false)
	id, err := gateway.NewStaticIdentity(gateway.BindingsFile{Version: 1, Bindings: []gateway.Binding{
		{ClientID: uuid.MustParse(clientID), CertSHA256: []string{gateway.Fingerprint(leaf)}, RequireMTLS: true, Note: "đối tác ghi lớn (thử)"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	h := gateway.NewHandler(e.mod, e.deps, id, e.log)
	plain := httptest.NewServer(h)
	t.Cleanup(plain.Close)
	certFile, keyFile, caFile := testpki.ServerFiles(t, t.TempDir(), serverCA, partnerCA)
	tlsCfg, err := gateway.ServerTLS(certFile, keyFile, caFile)
	if err != nil {
		t.Fatal(err)
	}
	mtls := httptest.NewUnstartedServer(h)
	mtls.TLS = tlsCfg
	mtls.StartTLS()
	t.Cleanup(mtls.Close)

	usage := func() int { return e.count(e.operator, `SELECT count(*) FROM pf_metering.usage_event`) }
	get := func(c *http.Client, base, k string) int {
		t.Helper()
		req, _ := http.NewRequest(http.MethodGet, base+"/v1/data-products", nil)
		req.Header.Set(apiclientport.KeyHeader, k)
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("gọi %s: %v", base, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	before := usage()
	if got := get(testpki.Client(serverCA, &cert), mtls.URL, key); got != http.StatusOK {
		t.Fatalf("mTLS + chứng chỉ gắn + khóa: muốn 200, nhận %d", got)
	}
	if after := usage(); after != before+1 {
		t.Fatalf("lời gọi qua mTLS phải ghi đúng một dòng sổ đo lường: %d → %d", before, after)
	}
	before = usage()
	if got := get(http.DefaultClient, plain.URL, key); got != http.StatusUnauthorized {
		t.Fatalf("require_mtls qua HTTP: muốn 401, nhận %d", got)
	}
	if got := get(testpki.Client(serverCA, &cert), mtls.URL, key[:len(key)-2]+"AA"); got != http.StatusUnauthorized {
		t.Fatalf("chứng chỉ gắn + khóa sai: muốn 401, nhận %d", got)
	}
	if after := usage(); after != before {
		t.Fatalf("lời gọi bị từ chối không ghi sổ: %d → %d", before, after)
	}
}
