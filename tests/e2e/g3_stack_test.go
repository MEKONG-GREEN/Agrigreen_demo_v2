package e2e

// Dựng phía MKG của kịch bản G3 trong tiến trình, cạnh nền tảng của harness:
//   - API nội bộ MKG THẬT (cmd/api-internal/assembly: hub + measurement + fieldops, adapter
//     mkgwire), nguồn thửa/HTX và luồng phản hồi HTX gọi Application API công khai của
//     harness bằng tài khoản dịch vụ (client credentials giả, token do issuer giả ký);
//   - ingest THẬT qua mTLS: CA thiết bị kiểu scripts/gen-dev-certs (CA → chứng thư máy chủ +
//     chứng thư client SAN = mã thiết bị), cấu hình TLS máy chủ bằng measurement.IngestServerTLS
//     như cmd/ingest (RequireAndVerifyClientCert), vòng tích phân IngestLoop.RunOnce;
//   - "bơm" outbox thay cho outbox-publisher + RabbitMQ: sự kiện nội bộ MKG (kind internal)
//     → consumer của cmd/worker-internal/assembly, van MKG (kind platform) và sự kiện nền tảng
//     → consumer liên module của cmd/api-platform/assembly, mỗi consumer qua shared.inbox trên
//     vai DB của module nhận (R-61) — cùng sổ đăng ký với tiến trình thật.
//
// Hai cụm DB (pf, mkg) + cụm ts là database riêng của pgtest; không có kết nối chéo cụm.

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	internalapi "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-internal/assembly"
	platformasm "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/api-platform/assembly"
	winternal "github.com/MEKONG-GREEN/Agrigreen_demo_v2/cmd/worker-internal/assembly"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement"
	mport "github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/mkg/measurement/port/edgewire"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/authz/authztest"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/clock"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/dbadmin"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/ids"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/inbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/outbox"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/internal/shared/telemetry"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/migrations"
	"github.com/MEKONG-GREEN/Agrigreen_demo_v2/tests/e2e/harness"
)

// mkgStack là API nội bộ MKG + ingest mTLS đang chạy trong tiến trình.
type mkgStack struct {
	App, Admin, Ingest *httptest.Server
	Mods               internalapi.Modules
	IngestHTTP         *http.Client
	ClientCertSHA      string // SHA-256 (hex) chứng thư client — cert_sha256 của sổ thiết bị
	Loop               *measurement.IngestLoop
}

// newMKGStack dựng phía MKG (gọi ở mức kiểm thử gốc — server sống suốt kịch bản). saToken trả
// token tài khoản dịch vụ tenant MKG mà endpoint token giả cấp cho client credentials của
// mkg/fieldops; grantor là tenant chương trình (AG_PARTNER_GRANTOR_TENANT, ADR-0020).
func newMKGStack(t *testing.T, s *harness.Stack, saToken func() string, grantor uuid.UUID, devices ...string) *mkgStack {
	t.Helper()
	ctx := context.Background()
	clk := clock.System{}
	tok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.ParseForm() != nil || r.PostForm.Get("grant_type") != "client_credentials" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": saToken(), "expires_in": 3600})
	}))
	t.Cleanup(tok.Close)
	t.Setenv("AG_PLATFORM_API_URL", s.App.URL)
	t.Setenv("AG_PLATFORM_TOKEN_URL", tok.URL)
	t.Setenv("AG_MKG_SA_CLIENT_ID", "agrigreen-mkg-sync")
	t.Setenv("AG_MKG_SA_CLIENT_SECRET", "dev-only")
	t.Setenv("AG_PARTNER_GRANTOR_TENANT", grantor.String())
	open := func(_ context.Context, role string) (*pgxpool.Pool, error) {
		return s.Env.Pool(t, migrations.MKG, role), nil
	}
	mods, ops, pools, err := internalapi.Build(ctx, open, clk)
	if err != nil {
		t.Fatal(err)
	}
	log := telemetry.NewLogger("api-internal", s.Logs)
	d := internalapi.Deps{Log: log, Verifier: s.Issuer.Verifier(), Ops: ops, Modules: mods.List(), Pools: pools}
	app, adm := httptest.NewServer(internalapi.NewRouter(d)), httptest.NewServer(internalapi.NewAdminRouter(d))
	t.Cleanup(app.Close)
	t.Cleanup(adm.Close)

	// Ingest như cmd/ingest: vai ingest_mkg (cụm mkg), ingest_ts / ts_reader (cụm ts).
	ports := measurement.NewIngestPorts(s.Env.Pool(t, migrations.MKG, mport.IngestRole), clk)
	store := &measurement.IngestTSStore{Writer: s.Env.Pool(t, migrations.TS, mport.IngestTSRole), Reader: s.Env.Pool(t, migrations.TS, mport.TSReaderRole)}
	lg := slog.New(slog.NewTextHandler(s.Logs, nil))
	met := measurement.NewIngestMetrics(nil)
	rv := &measurement.IngestReceiver{Devices: ports.Devices, Store: store, Clock: clk, Log: lg, Metrics: met}
	pki := newDevPKI(t, devices...)
	tlsCfg, err := measurement.IngestServerTLS(pki.serverCert, pki.serverKey, pki.caFile)
	if err != nil {
		t.Fatal(err)
	}
	ing := httptest.NewUnstartedServer(rv.NewEngine())
	ing.TLS = tlsCfg
	ing.StartTLS()
	t.Cleanup(ing.Close)
	ingestHTTP := &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		Certificates: []tls.Certificate{pki.client}, RootCAs: pki.roots, MinVersion: tls.VersionTLS12}}}
	loop := &measurement.IngestLoop{Sessions: ports.Sessions, Integrator: &measurement.IngestIntegrator{Windows: store, Tare: ports.Tare},
		Tickets: ports.Tickets, Clock: clk, Log: lg, Metrics: met}
	sum := sha256.Sum256(pki.client.Certificate[0])
	return &mkgStack{App: app, Admin: adm, Ingest: ing, Mods: mods, IngestHTTP: ingestHTTP, ClientCertSHA: hex.EncodeToString(sum[:]), Loop: loop}
}

// devPKI là bộ chứng thư dev kiểu scripts/gen-dev-certs: một CA thiết bị, chứng thư máy chủ
// ingest (127.0.0.1/localhost) và một chứng thư client có SAN = các mã thiết bị (ADR-0019).
type devPKI struct {
	serverCert, serverKey, caFile string
	client                        tls.Certificate
	roots                         *x509.CertPool
}

func newDevPKI(t *testing.T, devices ...string) devPKI {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	caTpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "AgriGreen dev device CA (e2e G3)"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTpl, caTpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := x509.ParseCertificate(caDER)
	issue := func(serial int64, tpl *x509.Certificate) ([]byte, *ecdsa.PrivateKey) {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		tpl.SerialNumber, tpl.NotBefore, tpl.NotAfter = big.NewInt(serial), now.Add(-time.Hour), now.Add(24*time.Hour)
		tpl.KeyUsage = x509.KeyUsageDigitalSignature
		der, err := x509.CreateCertificate(rand.Reader, tpl, ca, &k.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return der, k
	}
	writePEM := func(name, typ string, b []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: b}), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	srvDER, srvKey := issue(2, &x509.Certificate{Subject: pkix.Name{CommonName: "ingest"}, DNSNames: []string{"localhost"},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.IPv6loopback}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	srvKeyDER, err := x509.MarshalECPrivateKey(srvKey)
	if err != nil {
		t.Fatal(err)
	}
	cliDER, cliKey := issue(3, &x509.Certificate{Subject: pkix.Name{CommonName: devices[0]}, DNSNames: devices,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	return devPKI{
		serverCert: writePEM("ingest.pem", "CERTIFICATE", srvDER), serverKey: writePEM("ingest-key.pem", "EC PRIVATE KEY", srvKeyDER),
		caFile: writePEM("device-ca.pem", "CERTIFICATE", caDER),
		client: tls.Certificate{Certificate: [][]byte{cliDER}, PrivateKey: cliKey}, roots: roots,
	}
}

// ---------- gọi API MKG ----------

// mkgUser là người dùng tenant MKG (token do issuer giả của harness ký).
type mkgUser struct {
	g   *g3 // bước hiện hành (g.t) và stack MKG (g.m)
	tok string
}

func (u mkgUser) app(method, path string, body any) harness.Resp {
	u.g.t.Helper()
	return mkgDo(u.g.t, u.g.m.App.URL, u.tok, method, path, body)
}

func (u mkgUser) adm(method, path string, body any) harness.Resp {
	u.g.t.Helper()
	return mkgDo(u.g.t, u.g.m.Admin.URL, u.tok, method, path, body)
}

// mkgToken là token người dùng tenant MKG (MFA — bề mặt quản trị cần).
func mkgToken(tid uuid.UUID, roles []string) authztest.Token {
	return authztest.Token{Subject: ids.New(), TenantID: tid, Roles: roles, MFA: true, TTL: 2 * time.Hour}
}

// mkgDo gọi một route; POST luôn mang Idempotency-Key mới (R-51).
func mkgDo(t *testing.T, base, tok, method, path string, body any) harness.Resp {
	t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, base+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost {
		req.Header.Set("Idempotency-Key", harness.NewIdem())
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return harness.Resp{Status: resp.StatusCode, Header: resp.Header, Raw: raw, Body: m}
}

// ---------- thiết bị cân biên ----------

// edgeScale là một thiết bị cân tổng hợp: mã, điểm cân, khóa ký nguồn Ed25519 và bộ đếm.
type edgeScale struct {
	ID    string
	Point string
	Key   ed25519.PrivateKey
	next  int64
}

func newEdgeScale(id, point string) *edgeScale {
	seed := sha256.Sum256([]byte("agrigreen-e2e-g3-edge-key:" + id))
	return &edgeScale{ID: id, Point: point, Key: ed25519.NewKeyFromSeed(seed[:]), next: 1}
}

func (d *edgeScale) publicKey() string {
	return base64.StdEncoding.EncodeToString([]byte(d.Key.Public().(ed25519.PublicKey)))
}

// scaleReading là một số đo thô của agent biên.
type scaleReading struct {
	kg     int64
	stable bool
}

// weighReadings: dao động lúc chất hàng, năm số ổn định đối xứng quanh gross (trung vị = gross,
// weigh.integrate.v1 tạm — C-6), một số sau cửa sổ (xe kế tiếp lên cân).
func weighReadings(gross, tare int64) []scaleReading {
	const j = 17
	return []scaleReading{{tare, false}, {tare + (gross-tare)*2/3, false}, {gross + j, false},
		{gross, true}, {gross - j, true}, {gross + j, true}, {gross, true}, {gross, true}, {gross + 2000 + j, false}}
}

// batches ký các lô edge (≤ 4 số đo, bộ đếm liên tiếp) cho một phiên; trả lô và cửa sổ [from, to].
func (d *edgeScale) batches(t *testing.T, rs []scaleReading) (out []edgewire.Batch, from, to int64) {
	t.Helper()
	from = d.next
	off := 0
	t0 := time.Now().UTC().Add(-time.Hour) // đồng hồ thiết bị: ghi nhận, không dùng làm thứ tự (ADR-0019)
	for i := 0; i < len(rs); i += 4 {
		end := min(i+4, len(rs))
		b := edgewire.Batch{Format: edgewire.Format, DeviceID: d.ID, Epoch: 1, CounterFrom: from + int64(i), CounterTo: from + int64(end) - 1, NTPOffsetMs: &off}
		for k := i; k < end; k++ {
			c := from + int64(k)
			b.Readings = append(b.Readings, edgewire.Reading{Counter: c, DeviceTime: edgewire.FormatDeviceTime(t0.Add(time.Duration(c) * time.Second)),
				ValueKg: fmt.Sprintf("%d.000", rs[k].kg), Stable: rs[k].stable, Unit: edgewire.UnitKg})
		}
		if err := edgewire.Sign(d.Key, &b); err != nil {
			t.Fatal(err)
		}
		out = append(out, b)
	}
	d.next = from + int64(len(rs))
	return out, from, from + int64(len(rs)) - 2 // số cuối ngoài cửa sổ
}

// push gửi một lô edge tới ingest qua mTLS; trả mã HTTP.
func (m *mkgStack) push(t *testing.T, b edgewire.Batch) (int, string) {
	t.Helper()
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, m.Ingest.URL+"/v1/ingest/batches", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := m.IngestHTTP.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	return res.StatusCode, string(body)
}

// ---------- bơm outbox → consumer (thay outbox-publisher + RabbitMQ) ----------

type delivery struct {
	cluster  migrations.Cluster
	role     string
	consumer string
	env      outbox.Envelope
	handler  inbox.Handler
}

// pump giữ trạng thái giao sự kiện của các tenant của kịch bản (ADR-0020: tenant chương trình và
// tenant MKG là HAI tenant — van MKG mang tenant MKG, consumer nền tảng ghi vào tenant bên cấp).
type pump struct {
	t         *testing.T
	s         *harness.Stack
	tids      []uuid.UUID
	seen      map[uuid.UUID]bool
	delivered []delivery
	// byType đếm lượt xử lý theo (consumer) — kiểm luồng đi đúng consumer.
	byConsumer map[string]int
}

func newPump(t *testing.T, s *harness.Stack, tids ...uuid.UUID) *pump {
	return &pump{t: t, s: s, tids: tids, seen: map[uuid.UUID]bool{}, byConsumer: map[string]int{}}
}

type outboxRow struct {
	env  outbox.Envelope
	kind string
}

func (p *pump) rows(c migrations.Cluster) []outboxRow {
	p.t.Helper()
	ctx := context.Background()
	pool := p.s.Env.Pool(p.t, c, dbadmin.OutboxPublisher)
	tids := make([]string, 0, len(p.tids))
	for _, t := range p.tids {
		tids = append(tids, t.String())
	}
	rs, err := pool.Query(ctx, `SELECT id, tenant_id, event_type, schema_version, occurred_at, recorded_at, producer, payload, kind
	  FROM shared.outbox WHERE tenant_id = ANY($1::text[]::uuid[]) ORDER BY id`, tids)
	if err != nil {
		p.t.Fatal(err)
	}
	defer rs.Close()
	var out []outboxRow
	for rs.Next() {
		var r outboxRow
		if err := rs.Scan(&r.env.EventID, &r.env.TenantID, &r.env.EventType, &r.env.SchemaVersion, &r.env.OccurredAt, &r.env.RecordedAt,
			&r.env.Producer, &r.env.Payload, &r.kind); err != nil {
			p.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		p.t.Fatal(err)
	}
	return out
}

// run giao mọi sự kiện chưa giao của hai cụm tới khi không còn sự kiện mới (consumer có thể
// phát thêm sự kiện). Trả số lượt xử lý.
func (p *pump) run() int {
	p.t.Helper()
	n := 0
	for {
		progressed := false
		for _, c := range []migrations.Cluster{migrations.MKG, migrations.PF} {
			for _, r := range p.rows(c) {
				if p.seen[r.env.EventID] {
					continue
				}
				p.seen[r.env.EventID] = true
				progressed = true
				n += p.deliver(c, r)
			}
		}
		if !progressed {
			return n
		}
	}
}

func (p *pump) deliver(c migrations.Cluster, r outboxRow) int {
	p.t.Helper()
	var ds []delivery
	if c == migrations.MKG && r.kind == string(outbox.KindInternal) {
		// Vhost mkg.internal: consumer của cmd/worker-internal (không bao giờ tới nền tảng).
		poolFor := func(role string) (*pgxpool.Pool, error) { return p.s.Env.Pool(p.t, migrations.MKG, role), nil }
		for _, wc := range winternal.Consumers {
			if !slices.Contains(wc.Events, r.env.EventType) {
				continue
			}
			h, err := wc.Handler(clock.System{}, poolFor)
			if err != nil {
				p.t.Fatal(err)
			}
			role := wc.Role
			if role == "" {
				role = winternal.OpsRole
			}
			ds = append(ds, delivery{cluster: migrations.MKG, role: role, consumer: wc.Name, env: r.env, handler: h})
		}
	} else {
		// Vhost nền tảng: van MKG (kind platform) và sự kiện pf → consumer liên module của worker.
		for _, pc := range platformasm.Consumers {
			if slices.Contains(pc.Events, r.env.EventType) {
				ds = append(ds, delivery{cluster: migrations.PF, role: pc.Role, consumer: pc.Name, env: r.env, handler: pc.Handler(p.s.Mods)})
			}
		}
	}
	n := 0
	for _, d := range ds {
		ok, err := inbox.Process(context.Background(), p.s.Env.Pool(p.t, d.cluster, d.role), d.consumer, d.env, d.handler)
		if err != nil {
			p.t.Fatalf("consumer %s xử lý %s (%s): %v", d.consumer, d.env.EventType, d.env.EventID, err)
		}
		if ok {
			n++
			p.byConsumer[d.consumer]++
		}
		p.delivered = append(p.delivered, d)
	}
	return n
}

// redeliverAll giao lại MỌI sự kiện đã giao (RabbitMQ giao ít nhất một lần): inbox phải bỏ qua (R-61).
func (p *pump) redeliverAll() {
	p.t.Helper()
	for _, d := range p.delivered {
		ok, err := inbox.Process(context.Background(), p.s.Env.Pool(p.t, d.cluster, d.role), d.consumer, d.env, d.handler)
		if err != nil || ok {
			p.t.Fatalf("R-61: giao lặp %s → %s phải bị bỏ qua: processed=%v err=%v", d.env.EventType, d.consumer, ok, err)
		}
	}
}
